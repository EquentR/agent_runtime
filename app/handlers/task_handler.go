package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	coreagent "github.com/EquentR/agent_runtime/core/agent"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	resp "github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/gin-gonic/gin"
)

var errConversationAccessDenied = errors.New("无权访问该会话")

// TaskHandler 提供任务创建、查询、取消、重试与事件订阅接口。
type TaskHandler struct {
	manager       *coretasks.Manager
	conversations *coreagent.ConversationStore
	middlewares   []gin.HandlerFunc
	authRequired  bool
}

// CreateTaskRequest 描述创建任务接口的请求体。
type CreateTaskRequest struct {
	TaskType       string         `json:"task_type"`
	Input          map[string]any `json:"input"`
	Config         map[string]any `json:"config"`
	Metadata       map[string]any `json:"metadata"`
	CreatedBy      string         `json:"created_by"`
	IdempotencyKey string         `json:"idempotency_key"`
}

// NewTaskHandler 创建任务接口处理器。
func NewTaskHandler(manager *coretasks.Manager, conversations *coreagent.ConversationStore, middlewares ...gin.HandlerFunc) *TaskHandler {
	return &TaskHandler{manager: manager, conversations: conversations, middlewares: middlewares, authRequired: len(middlewares) > 0}
}

// Register 注册任务相关 REST 路由。
func (h *TaskHandler) Register(rg *gin.RouterGroup) {
	options := []resp.WrapperOption{}
	if len(h.middlewares) > 0 {
		options = append(options, resp.WithMiddlewares(h.middlewares...))
	}
	resp.HandlerWrapper(rg, "tasks", []*resp.Handler{
		resp.NewJsonOptionsHandler(h.handleCreateTask),
		resp.NewJsonHandler(h.handleFindRunningTask),
		resp.NewJsonHandler(h.handleGetTask),
		resp.NewJsonHandler(h.handleCancelTask),
		resp.NewJsonHandler(h.handleRetryTask),
		resp.NewHandler(http.MethodGet, "/:id/events", h.handleEvents),
	}, options...)
}

// handleCreateTask 返回创建任务接口的路由定义。
//
// @Summary 创建任务
// @Description 创建一个新的后台任务，并立即返回任务快照。
// @Tags tasks
// @Accept json
// @Produce json
// @Param request body CreateTaskRequest true "创建任务请求"
// @Success 200 {object} TaskSwaggerResponse
// @Failure 400 {object} ErrorSwaggerResponse
// @Router /tasks [post]
func (h *TaskHandler) handleCreateTask() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodPost, "", func(c *gin.Context) (any, []resp.ResOpt, error) {
		if h.manager == nil {
			return nil, nil, fmt.Errorf("task manager is not configured")
		}

		var request CreateTaskRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			return nil, nil, err
		}
		if request.Input == nil {
			request.Input = map[string]any{}
		}
		request.CreatedBy = h.resolveCreatedBy(c, request.CreatedBy)
		h.canonicalizeTaskInputCreatedBy(request.Input, request.CreatedBy)
		if err := h.ensureAgentRunConversation(c.Request.Context(), &request); err != nil {
			return nil, nil, err
		}
		if err := h.ensureConversationOwnership(c, request.Input); err != nil {
			if errors.Is(err, errConversationAccessDenied) {
				return gin.H{}, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, err
			}
			return nil, nil, err
		}

		task, err := h.manager.CreateTask(c.Request.Context(), coretasks.CreateTaskInput{
			TaskType:       request.TaskType,
			Input:          request.Input,
			Config:         request.Config,
			Metadata:       request.Metadata,
			CreatedBy:      request.CreatedBy,
			IdempotencyKey: request.IdempotencyKey,
			ConcurrencyKey: conversationConcurrencyKey(request.Input),
		})
		if err != nil {
			return nil, nil, err
		}
		return task, nil, nil
	}, nil
}

func (h *TaskHandler) ensureAgentRunConversation(ctx context.Context, request *CreateTaskRequest) error {
	if h == nil || request == nil || h.conversations == nil || request.TaskType != "agent.run" {
		return nil
	}
	if request.Input == nil {
		request.Input = map[string]any{}
	}
	if conversationID, ok := request.Input["conversation_id"].(string); ok && strings.TrimSpace(conversationID) != "" {
		request.Input["conversation_id"] = strings.TrimSpace(conversationID)
		return nil
	}

	providerID, _ := request.Input["provider_id"].(string)
	modelID, _ := request.Input["model_id"].(string)
	conversation, err := h.conversations.CreateConversation(ctx, coreagent.CreateConversationInput{
		ProviderID: providerID,
		ModelID:    modelID,
		CreatedBy:  request.CreatedBy,
	})
	if err != nil {
		return err
	}
	request.Input["conversation_id"] = conversation.ID
	return nil
}

func (h *TaskHandler) canonicalizeTaskInputCreatedBy(input map[string]any, createdBy string) {
	if len(input) == 0 || createdBy == "" {
		return
	}
	input["created_by"] = createdBy
	input["CreatedBy"] = createdBy
}

func conversationConcurrencyKey(input map[string]any) string {
	if len(input) == 0 {
		return ""
	}
	rawConversationID, ok := input["conversation_id"]
	if !ok {
		return ""
	}
	conversationID, ok := rawConversationID.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(conversationID)
}

func (h *TaskHandler) ensureConversationOwnership(c *gin.Context, input map[string]any) error {
	if !h.authRequired || h.conversations == nil {
		return nil
	}
	rawConversationID, ok := input["conversation_id"]
	if !ok {
		return nil
	}
	conversationID, ok := rawConversationID.(string)
	if !ok || conversationID == "" {
		return nil
	}
	conversation, err := h.conversations.GetConversation(c.Request.Context(), conversationID)
	if err != nil {
		return err
	}
	return ensureConversationOwnedByCurrentUser(c, conversation)
}

// handleFindRunningTask 返回按 conversation id 查询运行中任务接口的路由定义。
//
// @Summary 查询会话运行中任务
// @Description 根据 conversation id 返回最近一个非终态任务；若不存在则返回 null。
// @Tags tasks
// @Produce json
// @Param conversation_id query string true "会话 ID"
// @Success 200 {object} TaskSwaggerResponse
// @Router /tasks/running [get]
func (h *TaskHandler) handleFindRunningTask() (method, relativePath string, wrapper resp.JsonResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "/running", func(c *gin.Context) (any, error) {
		if h.manager == nil {
			return nil, fmt.Errorf("task manager is not configured")
		}

		conversationID := c.Query("conversation_id")
		if conversationID == "" {
			return nil, nil
		}

		task, err := h.manager.FindLatestActiveTaskByConversation(c.Request.Context(), conversationID)
		if err != nil {
			return nil, err
		}
		if err := h.ensureTaskAccess(c, task); err != nil {
			return nil, err
		}
		return task, nil
	}, nil
}

// handleGetTask 返回查询任务快照接口的路由定义。
//
// @Summary 获取任务详情
// @Description 根据任务 id 返回当前任务快照。
// @Tags tasks
// @Produce json
// @Param id path string true "任务 ID"
// @Success 200 {object} TaskSwaggerResponse
// @Failure 404 {object} ErrorSwaggerResponse
// @Router /tasks/{id} [get]
func (h *TaskHandler) handleGetTask() (method, relativePath string, wrapper resp.JsonResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "/:id", func(c *gin.Context) (any, error) {
		if h.manager == nil {
			return nil, fmt.Errorf("task manager is not configured")
		}
		task, err := h.manager.GetTask(c.Request.Context(), c.Param("id"))
		if err != nil {
			return nil, err
		}
		if err := h.ensureTaskAccess(c, task); err != nil {
			return nil, err
		}
		return task, nil
	}, nil
}

// handleCancelTask 返回取消任务接口的路由定义。
//
// @Summary 取消任务
// @Description 发起任务取消请求；若任务尚未执行，会直接进入 cancelled。
// @Tags tasks
// @Produce json
// @Param id path string true "任务 ID"
// @Success 200 {object} TaskSwaggerResponse
// @Failure 404 {object} ErrorSwaggerResponse
// @Router /tasks/{id}/cancel [post]
func (h *TaskHandler) handleCancelTask() (method, relativePath string, wrapper resp.JsonResultWrapper, opts []resp.WrapperOption) {
	return http.MethodPost, "/:id/cancel", func(c *gin.Context) (any, error) {
		if h.manager == nil {
			return nil, fmt.Errorf("task manager is not configured")
		}
		task, err := h.manager.GetTask(c.Request.Context(), c.Param("id"))
		if err != nil {
			return nil, err
		}
		if err := h.ensureTaskAccess(c, task); err != nil {
			return nil, err
		}
		return h.manager.CancelTask(c.Request.Context(), c.Param("id"))
	}, nil
}

// handleRetryTask 返回重试任务接口的路由定义。
//
// @Summary 重试任务
// @Description 基于一个已存在任务创建新的重试任务，并返回新的任务快照。
// @Tags tasks
// @Produce json
// @Param id path string true "任务 ID"
// @Success 200 {object} TaskSwaggerResponse
// @Failure 404 {object} ErrorSwaggerResponse
// @Router /tasks/{id}/retry [post]
func (h *TaskHandler) handleRetryTask() (method, relativePath string, wrapper resp.JsonResultWrapper, opts []resp.WrapperOption) {
	return http.MethodPost, "/:id/retry", func(c *gin.Context) (any, error) {
		if h.manager == nil {
			return nil, fmt.Errorf("task manager is not configured")
		}
		task, err := h.manager.GetTask(c.Request.Context(), c.Param("id"))
		if err != nil {
			return nil, err
		}
		if err := h.ensureTaskAccess(c, task); err != nil {
			return nil, err
		}
		return h.manager.RetryTask(c.Request.Context(), c.Param("id"))
	}, nil
}

// handleEvents 以 SSE 形式输出指定任务的历史事件与实时事件。
//
// @Summary 订阅任务事件
// @Description 先补发 after_seq 之后的历史事件，再持续推送实时事件。
// @Tags tasks
// @Produce text/event-stream
// @Param id path string true "任务 ID"
// @Param after_seq query int false "仅返回该序号之后的事件"
// @Success 200 {string} string "SSE event stream"
// @Failure 404 {string} string "task not found"
// @Router /tasks/{id}/events [get]
func (h *TaskHandler) handleEvents(c *gin.Context) {
	if h.manager == nil {
		c.String(http.StatusServiceUnavailable, "task manager is not configured")
		return
	}

	taskID := c.Param("id")
	task, err := h.manager.GetTask(c.Request.Context(), taskID)
	if err != nil {
		c.String(http.StatusNotFound, err.Error())
		return
	}
	if err := h.ensureTaskAccess(c, task); err != nil {
		c.String(http.StatusUnauthorized, err.Error())
		return
	}

	afterSeq, err := strconv.ParseInt(c.DefaultQuery("after_seq", "0"), 10, 64)
	if err != nil {
		c.String(http.StatusBadRequest, "invalid after_seq")
		return
	}

	writer := c.Writer
	flusher, ok := writer.(http.Flusher)
	if !ok {
		c.String(http.StatusInternalServerError, "streaming is not supported")
		return
	}

	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Connection", "keep-alive")

	ch, unsubscribe := h.manager.Subscribe(taskID)
	defer unsubscribe()

	history, err := h.manager.ListEvents(c.Request.Context(), taskID, afterSeq, 0)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	lastSeq := afterSeq
	for _, event := range history {
		if err := writeSSEEvent(writer, event); err != nil {
			return
		}
		lastSeq = event.Seq
		if event.EventType == coretasks.EventTaskFinished {
			flusher.Flush()
			return
		}
	}
	flusher.Flush()

	keepAlive := time.NewTicker(15 * time.Second)
	defer keepAlive.Stop()

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			if event.Seq <= lastSeq {
				continue
			}
			if err := writeSSEEvent(writer, event); err != nil {
				return
			}
			lastSeq = event.Seq
			flusher.Flush()
			if event.EventType == coretasks.EventTaskFinished {
				return
			}
		case <-keepAlive.C:
			if _, err := writer.Write([]byte(": keepalive\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (h *TaskHandler) resolveCreatedBy(c *gin.Context, fallback string) string {
	if user := currentAuthUser(c); user != nil && user.Username != "" {
		return user.Username
	}
	return fallback
}

func (h *TaskHandler) ensureTaskAccess(c *gin.Context, task *coretasks.Task) error {
	if !h.authRequired || task == nil {
		return nil
	}
	if user := currentAuthUser(c); user != nil && user.Username == task.CreatedBy {
		return nil
	}
	return fmt.Errorf("无权访问该任务")
}

// writeSSEEvent 将单个任务事件编码成标准 SSE 帧。
func writeSSEEvent(writer http.ResponseWriter, event coretasks.TaskEvent) error {
	payload, err := json.Marshal(struct {
		TaskID  string          `json:"task_id"`
		Seq     int64           `json:"seq"`
		Type    string          `json:"type"`
		TS      time.Time       `json:"ts"`
		Payload json.RawMessage `json:"payload"`
	}{
		TaskID:  event.TaskID,
		Seq:     event.Seq,
		Type:    event.EventType,
		TS:      event.CreatedAt,
		Payload: event.PayloadJSON,
	})
	if err != nil {
		return err
	}
	if _, err := writer.Write([]byte("id: " + strconv.FormatInt(event.Seq, 10) + "\n")); err != nil {
		return err
	}
	if _, err := writer.Write([]byte("event: " + event.EventType + "\n")); err != nil {
		return err
	}
	if _, err := writer.Write([]byte("data: " + string(payload) + "\n\n")); err != nil {
		return err
	}
	return nil
}
