package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/EquentR/agent_runtime/app/logics"
	"github.com/EquentR/agent_runtime/app/models"
	"github.com/EquentR/agent_runtime/pkg/mail"
	resp "github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/gin-gonic/gin"
)

type AdminSMTPTargetSender interface {
	Send(ctx context.Context, message mail.Message) error
}

type AdminSettingsHandler struct {
	settings    *logics.SettingsLogic
	audit       *logics.AdminAuditLogic
	smtpTester  AdminSMTPTargetSender
	middlewares []gin.HandlerFunc
}

type updateAdminSMTPSettingsRequest struct {
	Enabled       bool   `json:"enabled"`
	Host          string `json:"host"`
	Port          int    `json:"port"`
	Username      string `json:"username"`
	Password      string `json:"password"`
	ClearPassword bool   `json:"clear_password"`
	From          string `json:"from"`
	UseTLS        bool   `json:"use_tls"`
	UseStartTLS   bool   `json:"use_start_tls"`
}

type testAdminSMTPSettingsRequest struct {
	To string `json:"to"`
}

type updateAdminTurnstileSettingsRequest struct {
	Enabled             bool   `json:"enabled"`
	SiteKey             string `json:"site_key"`
	Secret              string `json:"secret"`
	ClearSecret         bool   `json:"clear_secret"`
	ProtectLogin        bool   `json:"protect_login"`
	ProtectRegistration bool   `json:"protect_registration"`
	ProtectVerification bool   `json:"protect_verification"`
}

type updateAdminRegistrationSettingsRequest struct {
	Enabled bool `json:"enabled"`
}

func NewAdminSettingsHandler(settings *logics.SettingsLogic, audit *logics.AdminAuditLogic, smtpTester AdminSMTPTargetSender, middlewares ...gin.HandlerFunc) *AdminSettingsHandler {
	return &AdminSettingsHandler{settings: settings, audit: audit, smtpTester: smtpTester, middlewares: middlewares}
}

func (h *AdminSettingsHandler) Register(rg *gin.RouterGroup) {
	if h.settings == nil {
		return
	}
	options := []resp.WrapperOption{}
	if len(h.middlewares) > 0 {
		options = append(options, resp.WithMiddlewares(h.middlewares...))
	}
	resp.HandlerWrapper(rg, "admin/settings", []*resp.Handler{
		resp.NewJsonOptionsHandler(h.handleGetSMTP),
		resp.NewJsonOptionsHandler(h.handleUpdateSMTP),
		resp.NewJsonOptionsHandler(h.handleTestSMTP),
		resp.NewJsonOptionsHandler(h.handleGetTurnstile),
		resp.NewJsonOptionsHandler(h.handleUpdateTurnstile),
		resp.NewJsonOptionsHandler(h.handleGetRegistration),
		resp.NewJsonOptionsHandler(h.handleUpdateRegistration),
	}, options...)
}

func (h *AdminSettingsHandler) handleGetSMTP() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "/smtp", func(c *gin.Context) (any, []resp.ResOpt, error) {
		settings, err := h.settings.GetSMTP(c.Request.Context())
		if err != nil {
			return nil, nil, err
		}
		return settings, nil, nil
	}, nil
}

func (h *AdminSettingsHandler) handleUpdateSMTP() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodPut, "/smtp", func(c *gin.Context) (any, []resp.ResOpt, error) {
		actor, err := requireAdminSettingsActor(c)
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, err
		}
		before, err := h.settings.GetSMTP(c.Request.Context())
		if err != nil {
			return nil, nil, err
		}
		var request updateAdminSMTPSettingsRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}
		after, err := h.settings.UpdateSMTP(c.Request.Context(), logics.UpdateSMTPInput{
			Enabled:       request.Enabled,
			Host:          request.Host,
			Port:          request.Port,
			Username:      request.Username,
			Password:      request.Password,
			ClearPassword: request.ClearPassword,
			From:          request.From,
			UseTLS:        request.UseTLS,
			UseStartTLS:   request.UseStartTLS,
			UpdatedBy:     actor.Username,
		})
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}
		if err := h.recordAudit(c, *actor, "setting", "smtp", "admin.settings.smtp.update", before, after); err != nil {
			return nil, nil, err
		}
		return after, nil, nil
	}, nil
}

func (h *AdminSettingsHandler) handleTestSMTP() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodPost, "/smtp/test", func(c *gin.Context) (any, []resp.ResOpt, error) {
		if h.smtpTester == nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusServiceUnavailable)}, logics.ErrMailServiceUnavailable
		}
		var request testAdminSMTPSettingsRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}
		to := strings.TrimSpace(request.To)
		if to == "" {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, fmt.Errorf("smtp test recipient is required")
		}
		if err := h.smtpTester.Send(c.Request.Context(), mail.Message{
			To:      to,
			Subject: "Agent Runtime SMTP test",
			Body:    "This is a test email from Agent Runtime.",
		}); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusServiceUnavailable)}, err
		}
		return gin.H{"sent": true}, nil, nil
	}, nil
}

func (h *AdminSettingsHandler) handleGetTurnstile() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "/turnstile", func(c *gin.Context) (any, []resp.ResOpt, error) {
		settings, err := h.settings.GetTurnstile(c.Request.Context())
		if err != nil {
			return nil, nil, err
		}
		return settings, nil, nil
	}, nil
}

func (h *AdminSettingsHandler) handleUpdateTurnstile() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodPut, "/turnstile", func(c *gin.Context) (any, []resp.ResOpt, error) {
		actor, err := requireAdminSettingsActor(c)
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, err
		}
		before, err := h.settings.GetTurnstile(c.Request.Context())
		if err != nil {
			return nil, nil, err
		}
		var request updateAdminTurnstileSettingsRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}
		after, err := h.settings.UpdateTurnstile(c.Request.Context(), logics.UpdateTurnstileInput{
			Enabled:             request.Enabled,
			SiteKey:             request.SiteKey,
			Secret:              request.Secret,
			ClearSecret:         request.ClearSecret,
			ProtectLogin:        request.ProtectLogin,
			ProtectRegistration: request.ProtectRegistration,
			ProtectVerification: request.ProtectVerification,
			UpdatedBy:           actor.Username,
		})
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}
		if err := h.recordAudit(c, *actor, "setting", "turnstile", "admin.settings.turnstile.update", before, after); err != nil {
			return nil, nil, err
		}
		return after, nil, nil
	}, nil
}

func (h *AdminSettingsHandler) handleGetRegistration() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "/registration", func(c *gin.Context) (any, []resp.ResOpt, error) {
		settings, err := h.settings.GetPublicRegistration(c.Request.Context())
		if err != nil {
			return nil, nil, err
		}
		return settings, nil, nil
	}, nil
}

func (h *AdminSettingsHandler) handleUpdateRegistration() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodPut, "/registration", func(c *gin.Context) (any, []resp.ResOpt, error) {
		actor, err := requireAdminSettingsActor(c)
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, err
		}
		before, err := h.settings.GetPublicRegistration(c.Request.Context())
		if err != nil {
			return nil, nil, err
		}
		var request updateAdminRegistrationSettingsRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}
		after, err := h.settings.UpdatePublicRegistration(c.Request.Context(), logics.UpdatePublicRegistrationInput{
			Enabled:   request.Enabled,
			UpdatedBy: actor.Username,
		})
		if err != nil {
			return nil, nil, err
		}
		if err := h.recordAudit(c, *actor, "setting", "registration", "admin.settings.registration.update", before, after); err != nil {
			return nil, nil, err
		}
		return after, nil, nil
	}, nil
}

func requireAdminSettingsActor(c *gin.Context) (*models.User, error) {
	actor := currentAuthUser(c)
	if actor == nil {
		return nil, logics.ErrUnauthorized
	}
	return actor, nil
}

func (h *AdminSettingsHandler) recordAudit(c *gin.Context, actor models.User, targetKind string, targetID string, action string, before any, after any) error {
	if h.audit == nil {
		return fmt.Errorf("admin audit logic is not configured")
	}
	return h.audit.Record(c.Request.Context(), logics.RecordAdminAuditInput{
		Actor:      actor,
		TargetKind: targetKind,
		TargetID:   targetID,
		Action:     action,
		Before:     before,
		After:      after,
		IPAddress:  c.ClientIP(),
		UserAgent:  c.Request.UserAgent(),
	})
}
