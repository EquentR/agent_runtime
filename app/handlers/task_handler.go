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

	"github.com/EquentR/agent_runtime/app/logics"
	coreagent "github.com/EquentR/agent_runtime/core/agent"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	coreworkspaces "github.com/EquentR/agent_runtime/core/workspaces"
	resp "github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/gin-gonic/gin"
)

var errConversationAccessDenied = errors.New("无权访问该会话")

// TaskHandler 提供任务创建、查询、取消、重试与事件订阅接口。
type taskWorkspaceManager interface {
	ConfirmTaskWorkspace(ctx context.Context, userID string, taskID string) (*coreworkspaces.WorkspaceStateFile, error)
	DiscardTaskWorkspace(ctx context.Context, userID string, taskID string) (*coreworkspaces.WorkspaceStateFile, error)
}

type taskWorkspaceActionInput struct {
	UserID      string
	WorkspaceID string
}

type TaskHandler struct {
	manager       *coretasks.Manager
	conversations *coreagent.ConversationStore
	modelLogic    *logics.ModelLogic
	workspaces    taskWorkspaceManager
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
	WorkspaceMode  string         `json:"workspace_mode"`
}

// NewTaskHandler 创建任务接口处理器。
func NewTaskHandler(manager *coretasks.Manager, conversations *coreagent.ConversationStore, middlewares ...gin.HandlerFunc) *TaskHandler {
	return &TaskHandler{manager: manager, conversations: conversations, middlewares: middlewares, authRequired: len(middlewares) > 0}
}

func (h *TaskHandler) WithModelLogic(modelLogic *logics.ModelLogic) *TaskHandler {
	h.modelLogic = modelLogic
	return h
}

func (h *TaskHandler) WithWorkspaceManager(workspaces taskWorkspaceManager) *TaskHandler {
	h.workspaces = workspaces
	return h
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
		resp.NewJsonOptionsHandler(h.handleConfirmTaskWorkspace),
		resp.NewJsonOptionsHandler(h.handleDiscardTaskWorkspace),
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
		workspaceMode, err := h.resolveWorkspaceMode(&request)
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}
		request.CreatedBy = h.resolveCreatedBy(c, request.CreatedBy)
		h.canonicalizeTaskInputCreatedBy(request.Input, request.CreatedBy)
		h.canonicalizeTaskInputUserID(c, request.Input)
		workspaceUserID := h.resolveWorkspaceUserID(c, request.CreatedBy)
		h.canonicalizeTaskInputWorkspace(request.Input, workspaceUserID, workspaceMode)
		if err := h.ensureModelAuthorized(c, &request); err != nil {
			return gin.H{}, modelAuthorizationErrorOptions(err), err
		}
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
			ConcurrencyKey: workspaceConcurrencyKey(request.Input, workspaceUserID, workspaceMode),
		})
		if err != nil {
			return nil, nil, err
		}
		return task, nil, nil
	}, nil
}

func (h *TaskHandler) resolveWorkspaceMode(request *CreateTaskRequest) (coreworkspaces.Mode, error) {
	if request == nil {
		return coreworkspaces.ModeMutable, nil
	}
	raw := strings.TrimSpace(request.WorkspaceMode)
	if raw == "" && request.Input != nil {
		if value, ok := request.Input["workspace_mode"].(string); ok {
			raw = strings.TrimSpace(value)
		}
	}
	if raw == "" {
		return coreworkspaces.ModeMutable, nil
	}
	switch coreworkspaces.Mode(raw) {
	case coreworkspaces.ModeMutable:
		return coreworkspaces.ModeMutable, nil
	case coreworkspaces.ModeReadonly:
		return coreworkspaces.ModeReadonly, nil
	default:
		return "", fmt.Errorf("invalid workspace_mode: %s", raw)
	}
}

func (h *TaskHandler) ensureModelAuthorized(c *gin.Context, request *CreateTaskRequest) error {
	if h == nil || h.modelLogic == nil || request == nil || request.TaskType != "agent.run" {
		return nil
	}
	user := currentAuthUser(c)
	if user == nil {
		return logics.ErrUnauthorized
	}
	providerID, _ := request.Input["provider_id"].(string)
	modelID, _ := request.Input["model_id"].(string)
	_, err := h.modelLogic.ResolveForUse(c.Request.Context(), *user, providerID, modelID)
	return err
}

func modelAuthorizationErrorOptions(err error) []resp.ResOpt {
	switch {
	case errors.Is(err, logics.ErrUnauthorized), errors.Is(err, logics.ErrModelUnauthorized):
		return []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}
	case errors.Is(err, logics.ErrModelNotFound):
		return []resp.ResOpt{resp.WithCode(http.StatusNotFound)}
	default:
		return nil
	}
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

func (h *TaskHandler) canonicalizeTaskInputUserID(c *gin.Context, input map[string]any) {
	if len(input) == 0 {
		return
	}
	delete(input, "UserID")
	if user := currentAuthUser(c); user != nil && user.ID != 0 {
		input["user_id"] = strconv.FormatUint(user.ID, 10)
		return
	}
	delete(input, "user_id")
}

func (h *TaskHandler) canonicalizeTaskInputWorkspace(input map[string]any, workspaceUserID string, mode coreworkspaces.Mode) {
	if input == nil {
		return
	}
	delete(input, "WorkspaceUserID")
	delete(input, "WorkspaceMode")
	delete(input, "workspace_task_root")
	delete(input, "WorkspaceTaskRoot")
	delete(input, "task_workspace_root")
	delete(input, "workspace_root")
	if workspaceUserID != "" {
		input["workspace_user_id"] = workspaceUserID
	}
	input["workspace_mode"] = string(mode)
}

func (h *TaskHandler) resolveWorkspaceUserID(c *gin.Context, createdBy string) string {
	if user := currentAuthUser(c); user != nil && user.ID != 0 {
		return strconv.FormatUint(user.ID, 10)
	}
	if trimmed := strings.TrimSpace(createdBy); trimmed != "" {
		return trimmed
	}
	return "local"
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

func workspaceConcurrencyKey(input map[string]any, workspaceUserID string, mode coreworkspaces.Mode) string {
	if mode == coreworkspaces.ModeMutable && strings.TrimSpace(workspaceUserID) != "" {
		return "workspace:" + strings.TrimSpace(workspaceUserID) + ":mutable"
	}
	return conversationConcurrencyKey(input)
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

// handleConfirmTaskWorkspace returns the route definition for confirming task workspace merge.
//
// @Summary 确认合并任务工作区
// @Description 将 pending_merge 状态的 mutable task workspace 备份并整目录回写到用户 home workspace。
// @Tags tasks
// @Produce json
// @Param id path string true "任务 ID"
// @Success 200 {object} WorkspaceStateSwaggerResponse
// @Failure 400 {object} ErrorSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Failure 404 {object} ErrorSwaggerResponse
// @Failure 409 {object} ErrorSwaggerResponse
// @Router /tasks/{id}/workspace/confirm [post]
func (h *TaskHandler) handleConfirmTaskWorkspace() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodPost, "/:id/workspace/confirm", func(c *gin.Context) (any, []resp.ResOpt, error) {
		if h.workspaces == nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusServiceUnavailable)}, fmt.Errorf("workspace manager is not configured")
		}
		task, resOpts, err := loadTaskForOwnerMutation(c, h.manager, h.authRequired)
		if err != nil {
			return nil, resOpts, err
		}
		input, err := h.taskWorkspaceInput(c, task)
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}
		state, err := h.workspaces.ConfirmTaskWorkspace(c.Request.Context(), input.UserID, input.WorkspaceID)
		if err != nil {
			data, opts := workspaceActionResponseOptions(err, input.WorkspaceID, workspaceActionErrorOptions)
			return data, opts, err
		}
		return state, nil, nil
	}, nil
}

// handleDiscardTaskWorkspace returns the route definition for discarding task workspace changes.
//
// @Summary 放弃任务工作区变更
// @Description 将 task workspace 标记为 discarded，保留目录但不回写用户 home workspace。
// @Tags tasks
// @Produce json
// @Param id path string true "任务 ID"
// @Success 200 {object} WorkspaceStateSwaggerResponse
// @Failure 400 {object} ErrorSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Failure 404 {object} ErrorSwaggerResponse
// @Router /tasks/{id}/workspace/discard [post]
func (h *TaskHandler) handleDiscardTaskWorkspace() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodPost, "/:id/workspace/discard", func(c *gin.Context) (any, []resp.ResOpt, error) {
		if h.workspaces == nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusServiceUnavailable)}, fmt.Errorf("workspace manager is not configured")
		}
		task, resOpts, err := loadTaskForOwnerMutation(c, h.manager, h.authRequired)
		if err != nil {
			return nil, resOpts, err
		}
		input, err := h.taskWorkspaceInput(c, task)
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}
		state, err := h.workspaces.DiscardTaskWorkspace(c.Request.Context(), input.UserID, input.WorkspaceID)
		if err != nil {
			data, opts := workspaceActionResponseOptions(err, input.WorkspaceID, workspaceActionErrorOptions)
			return data, opts, err
		}
		return state, nil, nil
	}, nil
}

func (h *TaskHandler) taskWorkspaceInput(c *gin.Context, task *coretasks.Task) (taskWorkspaceActionInput, error) {
	if task == nil {
		return taskWorkspaceActionInput{}, fmt.Errorf("task is required")
	}
	userID, err := workspaceUserIDFromTask(c, task)
	if err != nil {
		return taskWorkspaceActionInput{}, err
	}
	workspaceID, err := workspaceIDFromTask(task)
	if err != nil {
		return taskWorkspaceActionInput{}, err
	}
	return taskWorkspaceActionInput{UserID: userID, WorkspaceID: workspaceID}, nil
}

func workspaceUserIDFromTask(c *gin.Context, task *coretasks.Task) (string, error) {
	if user := currentAuthUser(c); user != nil && user.ID != 0 {
		return strconv.FormatUint(user.ID, 10), nil
	}
	input, err := decodeTaskInputMap(task)
	if err != nil {
		return "", err
	}
	if raw, ok := input["workspace_user_id"].(string); ok && strings.TrimSpace(raw) != "" {
		return strings.TrimSpace(raw), nil
	}
	if raw, ok := input["user_id"].(string); ok && strings.TrimSpace(raw) != "" {
		return strings.TrimSpace(raw), nil
	}
	if trimmed := strings.TrimSpace(task.CreatedBy); trimmed != "" {
		return trimmed, nil
	}
	return "", fmt.Errorf("workspace_user_id is missing")
}

func workspaceIDFromTask(task *coretasks.Task) (string, error) {
	if task == nil {
		return "", fmt.Errorf("task is required")
	}
	input, err := decodeTaskInputMap(task)
	if err != nil {
		return "", err
	}
	if raw, ok := input["conversation_id"].(string); ok && strings.TrimSpace(raw) != "" {
		return strings.TrimSpace(raw), nil
	}
	if strings.TrimSpace(task.ID) != "" {
		return strings.TrimSpace(task.ID), nil
	}
	return "", fmt.Errorf("workspace id is missing")
}

func decodeTaskInputMap(task *coretasks.Task) (map[string]any, error) {
	input := map[string]any{}
	if task == nil || len(task.InputJSON) == 0 {
		return input, nil
	}
	if err := json.Unmarshal(task.InputJSON, &input); err != nil {
		return nil, fmt.Errorf("decode task input: %w", err)
	}
	return input, nil
}

func workspaceActionErrorOptions(err error) []resp.ResOpt {
	if err == nil {
		return nil
	}
	message := err.Error()
	switch {
	case errors.Is(err, coreworkspaces.ErrWorkspaceHomeChanged):
		return []resp.ResOpt{resp.WithCode(http.StatusConflict)}
	case strings.Contains(message, "not found"):
		return []resp.ResOpt{resp.WithCode(http.StatusNotFound)}
	case strings.Contains(message, "not ready"), strings.Contains(message, "invalid"):
		return []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}
	default:
		return nil
	}
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
