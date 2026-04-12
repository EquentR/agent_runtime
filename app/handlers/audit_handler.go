package handlers

import (
	"errors"
	"fmt"
	"net/http"

	coreaudit "github.com/EquentR/agent_runtime/core/audit"
	resp "github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/gin-gonic/gin"
)

// AuditHandler 提供审计运行、事件与回放只读查询接口。
type AuditHandler struct {
	store        *coreaudit.Store
	middlewares  []gin.HandlerFunc
	authRequired bool
}

// NewAuditHandler 创建审计查询接口处理器。
func NewAuditHandler(store *coreaudit.Store, middlewares ...gin.HandlerFunc) *AuditHandler {
	return &AuditHandler{store: store, middlewares: middlewares, authRequired: len(middlewares) > 0}
}

// Register 注册审计只读接口路由。
func (h *AuditHandler) Register(rg *gin.RouterGroup) {
	if h.store == nil {
		return
	}
	options := []resp.WrapperOption{}
	if len(h.middlewares) > 0 {
		options = append(options, resp.WithMiddlewares(h.middlewares...))
	}
	resp.HandlerWrapper(rg, "audit", []*resp.Handler{
		resp.NewJsonOptionsHandler(h.handleGetRun),
		resp.NewJsonOptionsHandler(h.handleGetRunEvents),
		resp.NewJsonOptionsHandler(h.handleGetRunReplay),
		resp.NewJsonOptionsHandler(h.handleListConversationRuns),
		resp.NewJsonOptionsHandler(h.handleListConversationEvents),
	}, options...)
}

// handleGetRun 返回单个审计运行详情接口的路由定义。
//
// @Summary 获取审计运行
// @Description 根据审计运行 id 返回运行快照，供调试页展示运行级元数据。
// @Tags audit
// @Produce json
// @Param id path string true "审计运行 ID"
// @Success 200 {object} AuditRunSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Failure 404 {object} ErrorSwaggerResponse
// @Router /audit/runs/{id} [get]
func (h *AuditHandler) handleGetRun() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "/runs/:id", func(c *gin.Context) (any, []resp.ResOpt, error) {
		run, resOpts, err := h.loadAuthorizedRun(c)
		if err != nil {
			return nil, resOpts, err
		}
		return run, nil, nil
	}, nil
}

// handleGetRunEvents 返回指定审计运行的事件列表接口定义。
//
// @Summary 获取审计运行事件
// @Description 根据审计运行 id 返回按 seq 升序排列的审计事件列表。
// @Tags audit
// @Produce json
// @Param id path string true "审计运行 ID"
// @Success 200 {object} AuditEventsSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Failure 404 {object} ErrorSwaggerResponse
// @Router /audit/runs/{id}/events [get]
func (h *AuditHandler) handleGetRunEvents() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "/runs/:id/events", func(c *gin.Context) (any, []resp.ResOpt, error) {
		run, resOpts, err := h.loadAuthorizedRun(c)
		if err != nil {
			return nil, resOpts, err
		}
		events, err := h.store.ListEvents(c.Request.Context(), run.ID)
		if err != nil {
			return nil, nil, err
		}
		return events, nil, nil
	}, nil
}

// handleGetRunReplay 返回指定审计运行的回放包接口定义。
//
// @Summary 获取审计运行回放包
// @Description 根据审计运行 id 返回已组装的回放包，包含时间线与调试所需工件摘要。
// @Tags audit
// @Produce json
// @Param id path string true "审计运行 ID"
// @Success 200 {object} AuditReplaySwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Failure 404 {object} ErrorSwaggerResponse
// @Failure 409 {object} ErrorSwaggerResponse
// @Router /audit/runs/{id}/replay [get]
func (h *AuditHandler) handleGetRunReplay() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "/runs/:id/replay", func(c *gin.Context) (any, []resp.ResOpt, error) {
		run, resOpts, err := h.loadAuthorizedRun(c)
		if err != nil {
			return nil, resOpts, err
		}
		bundle, err := coreaudit.BuildReplayBundle(c.Request.Context(), h.store, run.ID)
		switch {
		case errors.Is(err, coreaudit.ErrRunNotFound):
			return nil, []resp.ResOpt{resp.WithCode(resp.NotFound)}, err
		case errors.Is(err, coreaudit.ErrReplayNotReplayable), errors.Is(err, coreaudit.ErrReplayUnsupportedSchemaVersion), errors.Is(err, coreaudit.ErrReplayRunNotFinished):
			return nil, []resp.ResOpt{resp.WithCode(http.StatusConflict)}, err
		case err != nil:
			return nil, nil, err
		default:
			return bundle, nil, nil
		}
	}, nil
}

// handleListConversationRuns 返回指定 conversation 下所有审计运行列表的接口定义。
//
// @Summary 按会话列出审计运行
// @Description 按 conversation_id 返回该会话下所有审计运行，按创建时间升序排列。
// @Tags audit
// @Produce json
// @Param conversation_id path string true "会话 ID"
// @Success 200 {object} AuditRunListSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Router /audit/conversations/{conversation_id}/runs [get]
func (h *AuditHandler) handleListConversationRuns() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "/conversations/:conversation_id/runs", func(c *gin.Context) (any, []resp.ResOpt, error) {
		if h.store == nil {
			return nil, nil, fmt.Errorf("audit store is not configured")
		}
		conversationID := c.Param("conversation_id")
		runs, err := h.store.ListRunsByConversationID(c.Request.Context(), conversationID)
		if err != nil {
			return nil, nil, err
		}
		if len(runs) > 0 {
			if err := h.ensureRunAccess(c, &runs[0]); err != nil {
				return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, err
			}
		}
		return runs, nil, nil
	}, nil
}

// handleListConversationEvents 返回指定 conversation 下所有审计事件的聚合列表接口定义。
//
// @Summary 按会话列出审计事件
// @Description 按 conversation_id 返回该会话下跨审计运行的所有事件，按时间和序列升序排列。
// @Tags audit
// @Produce json
// @Param conversation_id path string true "会话 ID"
// @Success 200 {object} AuditEventListSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Router /audit/conversations/{conversation_id}/events [get]
func (h *AuditHandler) handleListConversationEvents() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "/conversations/:conversation_id/events", func(c *gin.Context) (any, []resp.ResOpt, error) {
		if h.store == nil {
			return nil, nil, fmt.Errorf("audit store is not configured")
		}
		conversationID := c.Param("conversation_id")
		runs, err := h.store.ListRunsByConversationID(c.Request.Context(), conversationID)
		if err != nil {
			return nil, nil, err
		}
		if len(runs) > 0 {
			if err := h.ensureRunAccess(c, &runs[0]); err != nil {
				return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, err
			}
		}
		events, err := h.store.ListEventsByConversationID(c.Request.Context(), conversationID)
		if err != nil {
			return nil, nil, err
		}
		return events, nil, nil
	}, nil
}

func (h *AuditHandler) loadAuthorizedRun(c *gin.Context) (*coreaudit.Run, []resp.ResOpt, error) {
	if h.store == nil {
		return nil, nil, fmt.Errorf("audit store is not configured")
	}
	run, err := h.store.GetRun(c.Request.Context(), c.Param("id"))
	if errors.Is(err, coreaudit.ErrRunNotFound) {
		return nil, []resp.ResOpt{resp.WithCode(resp.NotFound)}, err
	}
	if err != nil {
		return nil, nil, err
	}
	if err := h.ensureRunAccess(c, run); err != nil {
		return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, err
	}
	return run, nil, nil
}

func (h *AuditHandler) ensureRunAccess(c *gin.Context, run *coreaudit.Run) error {
	if !h.authRequired {
		return nil
	}
	if run == nil {
		return nil
	}
	return ensureOwnerReadableByCurrentUser(c, run.CreatedBy, "无权访问该审计记录")
}
