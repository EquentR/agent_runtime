package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/EquentR/agent_runtime/app/logics"
	"github.com/EquentR/agent_runtime/app/models"
	coreworkspaces "github.com/EquentR/agent_runtime/core/workspaces"
	resp "github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/gin-gonic/gin"
)

type AdminWorkspaceHandler struct {
	workspaces  *coreworkspaces.Manager
	audit       *logics.AdminAuditLogic
	middlewares []gin.HandlerFunc
}

func NewAdminWorkspaceHandler(workspaces *coreworkspaces.Manager, audit *logics.AdminAuditLogic, middlewares ...gin.HandlerFunc) *AdminWorkspaceHandler {
	return &AdminWorkspaceHandler{workspaces: workspaces, audit: audit, middlewares: middlewares}
}

func (h *AdminWorkspaceHandler) Register(rg *gin.RouterGroup) {
	if h.workspaces == nil {
		return
	}
	options := []resp.WrapperOption{}
	if len(h.middlewares) > 0 {
		options = append(options, resp.WithMiddlewares(h.middlewares...))
	}
	resp.HandlerWrapper(rg, "admin/workspaces", []*resp.Handler{
		resp.NewJsonOptionsHandler(h.handleGetUserWorkspaceSummary),
	}, options...)
}

// handleGetUserWorkspaceSummary returns an admin-only workspace summary route.
//
// @Summary 查看用户工作区摘要
// @Description 管理员查看指定用户 home/task workspace 状态，并写入后台审计记录。
// @Tags admin-workspaces
// @Produce json
// @Param user_id path string true "用户 ID"
// @Success 200 {object} UserWorkspaceSummarySwaggerResponse
// @Failure 400 {object} ErrorSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Failure 403 {object} ErrorSwaggerResponse
// @Router /admin/workspaces/users/{user_id} [get]
func (h *AdminWorkspaceHandler) handleGetUserWorkspaceSummary() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "/users/:user_id", func(c *gin.Context) (any, []resp.ResOpt, error) {
		userID := strings.TrimSpace(c.Param("user_id"))
		if userID == "" {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, fmt.Errorf("user_id is required")
		}
		summary, err := h.workspaces.SummarizeUserWorkspaces(c.Request.Context(), userID)
		if err != nil {
			return nil, workspaceSummaryErrorOptions(err), err
		}
		actor := currentAuthUser(c)
		if actor == nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, logics.ErrUnauthorized
		}
		if err := h.recordAudit(c, *actor, userID, summary); err != nil {
			return nil, nil, err
		}
		return summary, nil, nil
	}, nil
}

func (h *AdminWorkspaceHandler) recordAudit(c *gin.Context, actor models.User, userID string, summary *coreworkspaces.UserWorkspaceSummary) error {
	if h.audit == nil {
		return fmt.Errorf("admin audit logic is not configured")
	}
	return h.audit.Record(c.Request.Context(), logics.RecordAdminAuditInput{
		Actor:      actor,
		TargetKind: "workspace",
		TargetID:   userID,
		Action:     "admin.workspaces.inspect",
		After:      summary,
		IPAddress:  c.ClientIP(),
		UserAgent:  c.Request.UserAgent(),
	})
}

func workspaceSummaryErrorOptions(err error) []resp.ResOpt {
	if err == nil {
		return nil
	}
	message := err.Error()
	if strings.Contains(message, "path element") || strings.Contains(message, "absolute paths") || strings.Contains(message, "escapes workspace") {
		return []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}
	}
	return nil
}
