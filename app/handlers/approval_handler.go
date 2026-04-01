package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	coreagent "github.com/EquentR/agent_runtime/core/agent"
	"github.com/EquentR/agent_runtime/core/approvals"
	"github.com/EquentR/agent_runtime/core/interactions"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	resp "github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/gin-gonic/gin"
)

type ApprovalHandler struct {
	manager       *coretasks.Manager
	approvals     *approvals.Store
	interactions  *interactions.Store
	conversations *coreagent.ConversationStore
	middlewares   []gin.HandlerFunc
	authRequired  bool
}

type ApprovalDecisionRequest struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

func NewApprovalHandler(manager *coretasks.Manager, approvalStore *approvals.Store, conversations *coreagent.ConversationStore, middlewares ...gin.HandlerFunc) *ApprovalHandler {
	return &ApprovalHandler{
		manager:       manager,
		approvals:     approvalStore,
		interactions:  nil,
		conversations: conversations,
		middlewares:   middlewares,
		authRequired:  len(middlewares) > 0,
	}
}

func (h *ApprovalHandler) WithInteractionStore(store *interactions.Store) *ApprovalHandler {
	if h == nil {
		return nil
	}
	h.interactions = store
	return h
}

func (h *ApprovalHandler) Register(rg *gin.RouterGroup) {
	options := []resp.WrapperOption{}
	if len(h.middlewares) > 0 {
		options = append(options, resp.WithMiddlewares(h.middlewares...))
	}
	resp.HandlerWrapper(rg, "tasks", []*resp.Handler{
		resp.NewJsonOptionsHandler(h.handleListTaskApprovals),
		resp.NewJsonOptionsHandler(h.handleDecideApproval),
	}, options...)
}

// @Summary 查询任务审批列表
// @Description 返回指定任务的工具审批记录。
// @Tags tasks
// @Produce json
// @Param id path string true "任务 ID"
// @Success 200 {object} ApprovalListSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Failure 404 {object} ErrorSwaggerResponse
// @Router /tasks/{id}/approvals [get]
func (h *ApprovalHandler) handleListTaskApprovals() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "/:id/approvals", func(c *gin.Context) (any, []resp.ResOpt, error) {
		task, opts, err := h.loadAccessibleTask(c)
		if err != nil {
			return nil, opts, err
		}
		if h.interactions != nil {
			listed, err := h.interactions.ListTaskInteractions(c.Request.Context(), task.ID)
			if err != nil {
				return nil, nil, err
			}
			return mapApprovalInteractions(listed), nil, nil
		}
		listed, err := h.approvals.ListTaskApprovals(c.Request.Context(), task.ID)
		return listed, nil, err
	}, nil
}

func mapApprovalInteractions(listed []interactions.Interaction) []approvals.ToolApproval {
	approvalsList := make([]approvals.ToolApproval, 0, len(listed))
	for _, interaction := range listed {
		if interaction.Kind != interactions.KindApproval {
			continue
		}
		approval := approvals.ToolApproval{
			ID:             interaction.ID,
			TaskID:         interaction.TaskID,
			ConversationID: interaction.ConversationID,
			StepIndex:      interaction.StepIndex,
			ToolCallID:     interaction.ToolCallID,
			Status:         approvals.Status(interaction.Status),
			DecisionBy:     interaction.RespondedBy,
			DecisionAt:     interaction.RespondedAt,
			CreatedAt:      interaction.CreatedAt,
			UpdatedAt:      interaction.UpdatedAt,
		}
		var request map[string]any
		_ = json.Unmarshal(interaction.RequestJSON, &request)
		approval.ToolName = stringValue(request["tool_name"])
		approval.ArgumentsSummary = stringValue(request["arguments_summary"])
		approval.RiskLevel = stringValue(request["risk_level"])
		approval.Reason = stringValue(request["reason"])
		var response map[string]any
		_ = json.Unmarshal(interaction.ResponseJSON, &response)
		approval.DecisionReason = stringValue(response["reason"])
		switch stringValue(response["decision"]) {
		case string(approvals.DecisionApprove):
			approval.Status = approvals.StatusApproved
		case string(approvals.DecisionReject):
			if approval.Status == approvals.StatusExpired {
				approval.Status = approvals.StatusExpired
			} else {
				approval.Status = approvals.StatusRejected
			}
		}
		approvalsList = append(approvalsList, approval)
	}
	return approvalsList
}

func stringValue(value any) string {
	return strings.TrimSpace(fmt.Sprint(value))
}

// @Summary 审批工具调用
// @Description 对指定任务下的待审批工具调用做 approve 或 reject 决策。
// @Tags tasks
// @Accept json
// @Produce json
// @Param id path string true "任务 ID"
// @Param approvalID path string true "审批 ID"
// @Param request body ApprovalDecisionSwaggerRequest true "审批决策请求"
// @Success 200 {object} ApprovalSwaggerResponse
// @Failure 400 {object} ErrorSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Failure 404 {object} ErrorSwaggerResponse
// @Router /tasks/{id}/approvals/{approvalID}/decision [post]
func (h *ApprovalHandler) handleDecideApproval() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodPost, "/:id/approvals/:approvalID/decision", func(c *gin.Context) (any, []resp.ResOpt, error) {
		task, opts, err := h.loadAccessibleTask(c)
		if err != nil {
			return nil, opts, err
		}

		var request ApprovalDecisionRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			return nil, nil, err
		}

		decision := approvals.Decision(strings.TrimSpace(request.Decision))
		if decision != approvals.DecisionApprove && decision != approvals.DecisionReject {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, fmt.Errorf("decision must be approve or reject")
		}

		resolved, err := h.manager.ResolveTaskApproval(c.Request.Context(), task.ID, c.Param("approvalID"), approvals.ResolveApprovalInput{
			Decision:   decision,
			Reason:     strings.TrimSpace(request.Reason),
			DecisionBy: h.resolveDecisionBy(c, task),
		})
		if err != nil {
			if errors.Is(err, approvals.ErrApprovalNotFound) {
				return nil, []resp.ResOpt{resp.WithCode(http.StatusNotFound)}, err
			}
			return nil, nil, err
		}
		return resolved, opts, nil
	}, nil
}

func (h *ApprovalHandler) loadAccessibleTask(c *gin.Context) (*coretasks.Task, []resp.ResOpt, error) {
	if h == nil || h.manager == nil {
		return nil, nil, fmt.Errorf("task manager is not configured")
	}
	if h.approvals == nil {
		return nil, nil, fmt.Errorf("approval store is not configured")
	}
	task, err := h.manager.GetTask(c.Request.Context(), c.Param("id"))
	if err != nil {
		if errors.Is(err, coretasks.ErrTaskNotFound) {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusNotFound)}, err
		}
		return nil, nil, err
	}
	if err := h.ensureTaskAccess(c, task); err != nil {
		if errors.Is(err, errConversationAccessDenied) || err.Error() == "无权访问该任务" {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, err
		}
		return nil, nil, err
	}
	return task, nil, nil
}

func (h *ApprovalHandler) ensureTaskAccess(c *gin.Context, task *coretasks.Task) error {
	if !h.authRequired || task == nil {
		return nil
	}
	if user := currentAuthUser(c); user != nil && user.Username == task.CreatedBy {
		return nil
	}
	return fmt.Errorf("无权访问该任务")
}

func (h *ApprovalHandler) resolveDecisionBy(c *gin.Context, task *coretasks.Task) string {
	if user := currentAuthUser(c); user != nil && user.Username != "" {
		return user.Username
	}
	if task == nil {
		return ""
	}
	return task.CreatedBy
}
