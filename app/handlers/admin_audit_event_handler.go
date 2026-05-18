package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/EquentR/agent_runtime/app/logics"
	"github.com/EquentR/agent_runtime/app/models"
	resp "github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/gin-gonic/gin"
)

type AdminAuditEventHandler struct {
	audit       *logics.AdminAuditLogic
	middlewares []gin.HandlerFunc
}

type adminAuditEventResponse struct {
	ID            uint64          `json:"id"`
	ActorID       uint64          `json:"actor_id"`
	ActorUsername string          `json:"actor_username"`
	ActorEmail    string          `json:"actor_email"`
	TargetKind    string          `json:"target_kind"`
	TargetID      string          `json:"target_id"`
	Action        string          `json:"action"`
	BeforeJSON    json.RawMessage `json:"before_json,omitempty"`
	AfterJSON     json.RawMessage `json:"after_json,omitempty"`
	IPAddress     string          `json:"ip_address"`
	UserAgent     string          `json:"user_agent"`
	CreatedAt     time.Time       `json:"created_at"`
}

func NewAdminAuditEventHandler(audit *logics.AdminAuditLogic, middlewares ...gin.HandlerFunc) *AdminAuditEventHandler {
	return &AdminAuditEventHandler{audit: audit, middlewares: middlewares}
}

func (h *AdminAuditEventHandler) Register(rg *gin.RouterGroup) {
	if h.audit == nil {
		return
	}
	options := []resp.WrapperOption{}
	if len(h.middlewares) > 0 {
		options = append(options, resp.WithMiddlewares(h.middlewares...))
	}
	resp.HandlerWrapper(rg, "admin/audit-events", []*resp.Handler{
		resp.NewJsonOptionsHandler(h.handleListEvents),
	}, options...)
}

func (h *AdminAuditEventHandler) handleListEvents() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "", func(c *gin.Context) (any, []resp.ResOpt, error) {
		actorID, err := parseOptionalUintQuery(c.Query("actor_id"))
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}
		limit, err := parseOptionalIntQuery(c.Query("limit"))
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}
		events, err := h.audit.List(c.Request.Context(), logics.AdminAuditFilter{
			ActorID:       actorID,
			ActorUsername: c.Query("actor_username"),
			TargetKind:    c.Query("target_kind"),
			TargetID:      c.Query("target_id"),
			Action:        c.Query("action"),
			Limit:         limit,
		})
		if err != nil {
			return nil, nil, err
		}
		result := make([]adminAuditEventResponse, 0, len(events))
		for _, event := range events {
			result = append(result, toAdminAuditEventResponse(event))
		}
		return result, nil, nil
	}, nil
}

func toAdminAuditEventResponse(event models.AdminAuditEvent) adminAuditEventResponse {
	return adminAuditEventResponse{
		ID:            event.ID,
		ActorID:       event.ActorID,
		ActorUsername: event.ActorUsername,
		ActorEmail:    event.ActorEmail,
		TargetKind:    event.TargetKind,
		TargetID:      event.TargetID,
		Action:        event.Action,
		BeforeJSON:    json.RawMessage(event.BeforeJSON),
		AfterJSON:     json.RawMessage(event.AfterJSON),
		IPAddress:     event.IPAddress,
		UserAgent:     event.UserAgent,
		CreatedAt:     event.CreatedAt,
	}
}

func parseOptionalUintQuery(raw string) (uint64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	return strconv.ParseUint(raw, 10, 64)
}

func parseOptionalIntQuery(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	return strconv.Atoi(raw)
}
