package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/EquentR/agent_runtime/app/logics"
	"github.com/EquentR/agent_runtime/app/models"
	coreupdater "github.com/EquentR/agent_runtime/core/updater"
	resp "github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/gin-gonic/gin"
)

const updateCSRFCookie = "ice_art_update_csrf"
const updateCSRFHeader = "X-Ice-Art-Update-CSRF"

type AdminUpdateService interface {
	Status(context.Context) (logics.UpdateStatus, error)
	Check(context.Context) (logics.UpdateStatus, error)
	Authorize(context.Context, *models.User, *models.UserSession, string, string, string) (string, time.Time, error)
	Install(context.Context, *models.User, *models.UserSession, string, string, string, coreupdater.BackupMode) (coreupdater.OperationState, error)
	ForceInstall(context.Context, *models.User, *models.UserSession, string, string, string, coreupdater.BackupMode) (coreupdater.OperationState, error)
	Rollback(context.Context, *models.User, *models.UserSession, string, string, string) (coreupdater.OperationState, error)
}

type AdminUpdateHandler struct {
	service     AdminUpdateService
	auth        *AuthMiddleware
	audit       *logics.AdminAuditLogic
	middlewares []gin.HandlerFunc
}

type updateAuthorizeRequest struct {
	Password string `json:"password"`
	Action   string `json:"action"`
	Target   string `json:"target"`
}

type updateInstallRequest struct {
	AuthorizationToken string                 `json:"authorization_token"`
	OperationID        string                 `json:"operation_id"`
	Target             string                 `json:"target"`
	BackupMode         coreupdater.BackupMode `json:"backup_mode"`
}

func NewAdminUpdateHandler(service AdminUpdateService, auth *AuthMiddleware, audit *logics.AdminAuditLogic, middlewares ...gin.HandlerFunc) *AdminUpdateHandler {
	return &AdminUpdateHandler{service: service, auth: auth, audit: audit, middlewares: middlewares}
}

func (h *AdminUpdateHandler) Register(group *gin.RouterGroup) {
	if h.service == nil {
		return
	}
	options := []resp.WrapperOption{}
	if len(h.middlewares) > 0 {
		options = append(options, resp.WithMiddlewares(h.middlewares...))
	}
	resp.HandlerWrapper(group, "admin/updates", []*resp.Handler{
		newUpdateJSONOptionsHandler(h.status),
		newUpdateJSONOptionsHandler(h.check),
		newUpdateJSONOptionsHandler(h.authorize),
		newUpdateJSONOptionsHandler(h.install),
		newUpdateJSONOptionsHandler(h.forceInstall),
		newUpdateJSONOptionsHandler(h.rollback),
	}, options...)
}

// @Summary 获取自升级状态
// @Tags admin-updates
// @Produce json
// @Success 200 {object} AdminUpdateStatusSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Router /admin/updates/status [get]
func (h *AdminUpdateHandler) status() (string, string, resp.JsonOptionsResultWrapper, []resp.WrapperOption) {
	return http.MethodGet, "/status", func(c *gin.Context) (any, []resp.ResOpt, error) {
		status, err := h.service.Status(c.Request.Context())
		if err != nil {
			return nil, nil, err
		}
		csrf, err := ensureUpdateCSRF(c)
		if err != nil {
			return nil, nil, err
		}
		return gin.H{"status": status, "csrf_token": csrf}, nil, nil
	}, nil
}

// @Summary 立即检查稳定版更新
// @Tags admin-updates
// @Produce json
// @Success 200 {object} AdminUpdateCheckSwaggerResponse
// @Failure 403 {object} ErrorSwaggerResponse
// @Failure 409 {object} ErrorSwaggerResponse
// @Router /admin/updates/check [post]
func (h *AdminUpdateHandler) check() (string, string, resp.JsonOptionsResultWrapper, []resp.WrapperOption) {
	return http.MethodPost, "/check", func(c *gin.Context) (any, []resp.ResOpt, error) {
		if err := validateUpdateMutation(c); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusForbidden)}, err
		}
		status, err := h.service.Check(c.Request.Context())
		target := status.Current.Version
		if status.Latest != nil {
			target = status.Latest.TagName
		}
		h.recordUpdate(c, "admin.updates.check", target, "", status.State, err)
		if err != nil {
			return status, updateErrorOptions(err), err
		}
		return status, nil, nil
	}, nil
}

// @Summary 复核管理员密码并签发一次性升级授权
// @Tags admin-updates
// @Accept json
// @Produce json
// @Param request body AdminUpdateAuthorizeSwaggerRequest true "授权动作"
// @Success 200 {object} AdminUpdateAuthorizationSwaggerResponse
// @Failure 400 {object} ErrorSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Failure 403 {object} ErrorSwaggerResponse
// @Router /admin/updates/authorize [post]
func (h *AdminUpdateHandler) authorize() (string, string, resp.JsonOptionsResultWrapper, []resp.WrapperOption) {
	return http.MethodPost, "/authorize", func(c *gin.Context) (any, []resp.ResOpt, error) {
		if err := validateUpdateMutation(c); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusForbidden)}, err
		}
		var request updateAuthorizeRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}
		if request.Action != "install" && request.Action != "force_install" && request.Action != "rollback" {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, fmt.Errorf("unsupported update action")
		}
		user, session, err := h.actor(c)
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, err
		}
		token, expiresAt, err := h.service.Authorize(c.Request.Context(), user, session, request.Password, request.Action, strings.TrimSpace(request.Target))
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, err
		}
		return gin.H{"authorization_token": token, "expires_at": expiresAt}, nil, nil
	}, nil
}

// @Summary 下载并安装已检查的发行版
// @Tags admin-updates
// @Accept json
// @Produce json
// @Param request body AdminUpdateInstallSwaggerRequest true "升级参数"
// @Success 202 {object} AdminUpdateOperationSwaggerResponse
// @Failure 400 {object} ErrorSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Failure 403 {object} ErrorSwaggerResponse
// @Failure 409 {object} ErrorSwaggerResponse
// @Failure 503 {object} ErrorSwaggerResponse
// @Router /admin/updates/install [post]
func (h *AdminUpdateHandler) install() (string, string, resp.JsonOptionsResultWrapper, []resp.WrapperOption) {
	return http.MethodPost, "/install", func(c *gin.Context) (any, []resp.ResOpt, error) {
		if err := validateUpdateMutation(c); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusForbidden)}, err
		}
		var request updateInstallRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}
		user, session, err := h.actor(c)
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, err
		}
		state, err := h.service.Install(c.Request.Context(), user, session, request.AuthorizationToken, request.OperationID, request.Target, request.BackupMode)
		h.recordUpdate(c, "admin.updates.install", request.Target, request.BackupMode, state, err)
		if err != nil {
			return state, updateErrorOptions(err), err
		}
		return state, []resp.ResOpt{resp.WithCode(http.StatusAccepted)}, nil
	}, nil
}

// @Summary 在排空超时后强制继续升级
// @Tags admin-updates
// @Accept json
// @Produce json
// @Param request body AdminUpdateInstallSwaggerRequest true "强制升级参数"
// @Success 202 {object} AdminUpdateOperationSwaggerResponse
// @Failure 400 {object} ErrorSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Failure 403 {object} ErrorSwaggerResponse
// @Failure 409 {object} ErrorSwaggerResponse
// @Router /admin/updates/force-install [post]
func (h *AdminUpdateHandler) forceInstall() (string, string, resp.JsonOptionsResultWrapper, []resp.WrapperOption) {
	return http.MethodPost, "/force-install", func(c *gin.Context) (any, []resp.ResOpt, error) {
		if err := validateUpdateMutation(c); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusForbidden)}, err
		}
		var request updateInstallRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}
		user, session, err := h.actor(c)
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, err
		}
		state, err := h.service.ForceInstall(c.Request.Context(), user, session, request.AuthorizationToken, request.OperationID, request.Target, request.BackupMode)
		h.recordUpdate(c, "admin.updates.force_install", request.Target, request.BackupMode, state, err)
		if err != nil {
			return state, updateErrorOptions(err), err
		}
		return state, []resp.ResOpt{resp.WithCode(http.StatusAccepted)}, nil
	}, nil
}

// @Summary 回滚最近一次受保护备份
// @Tags admin-updates
// @Accept json
// @Produce json
// @Param request body AdminUpdateInstallSwaggerRequest true "回滚参数"
// @Success 202 {object} AdminUpdateOperationSwaggerResponse
// @Failure 400 {object} ErrorSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Failure 403 {object} ErrorSwaggerResponse
// @Failure 409 {object} ErrorSwaggerResponse
// @Router /admin/updates/rollback [post]
func (h *AdminUpdateHandler) rollback() (string, string, resp.JsonOptionsResultWrapper, []resp.WrapperOption) {
	return http.MethodPost, "/rollback", func(c *gin.Context) (any, []resp.ResOpt, error) {
		if err := validateUpdateMutation(c); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusForbidden)}, err
		}
		var request updateInstallRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}
		user, session, err := h.actor(c)
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, err
		}
		state, err := h.service.Rollback(c.Request.Context(), user, session, request.AuthorizationToken, request.OperationID, request.Target)
		h.recordUpdate(c, "admin.updates.rollback", request.Target, "", state, err)
		if err != nil {
			return state, updateErrorOptions(err), err
		}
		return state, []resp.ResOpt{resp.WithCode(http.StatusAccepted)}, nil
	}, nil
}

func updateErrorOptions(err error) []resp.ResOpt {
	code := http.StatusInternalServerError
	switch {
	case errors.Is(err, coreupdater.ErrOperationAuthorization):
		code = http.StatusUnauthorized
	case errors.Is(err, logics.ErrUpdateOperationConflict):
		code = http.StatusConflict
	case errors.Is(err, logics.ErrInvalidUpdateRequest):
		code = http.StatusBadRequest
	case errors.Is(err, logics.ErrUpdateUnavailable):
		code = http.StatusServiceUnavailable
	case errors.Is(err, logics.ErrUpdateDrainTimeout):
		code = http.StatusConflict
	}
	return []resp.ResOpt{resp.WithCode(code)}
}

func newUpdateJSONOptionsHandler(factory func() (string, string, resp.JsonOptionsResultWrapper, []resp.WrapperOption)) *resp.Handler {
	method, path, wrapper, options := factory()
	return resp.NewHandler(method, path, func(c *gin.Context) {
		data, resultOptions, err := wrapper(c)
		result := resp.NewResult()
		if err == nil {
			result.SetCode(http.StatusOK)
			result.Data = data
		} else {
			result.SetCode(http.StatusInternalServerError)
			result.Data = data
			result.SetMessage(updatePublicError(err))
		}
		for _, option := range resultOptions {
			option(result)
		}
		c.JSON(result.Code, result)
	}, options...)
}

func updatePublicError(err error) string {
	switch {
	case errors.Is(err, coreupdater.ErrOperationAuthorization):
		return "administrator authorization failed"
	case errors.Is(err, logics.ErrUpdateOperationConflict):
		return "another update operation is active"
	case errors.Is(err, logics.ErrInvalidUpdateRequest):
		return err.Error()
	case errors.Is(err, logics.ErrUpdateUnavailable):
		return err.Error()
	case errors.Is(err, logics.ErrUpdateDrainTimeout):
		return "任务未能排空，需要重新授权后强制继续"
	default:
		return "update operation failed"
	}
}

func (h *AdminUpdateHandler) actor(c *gin.Context) (*models.User, *models.UserSession, error) {
	user, userOK := h.auth.CurrentUser(c)
	session, sessionOK := h.auth.CurrentSession(c)
	if !userOK || !sessionOK {
		return nil, nil, fmt.Errorf("administrator session is required")
	}
	return user, session, nil
}

func (h *AdminUpdateHandler) record(c *gin.Context, action, target string, before, after any) {
	if h.audit == nil {
		return
	}
	user, ok := h.auth.CurrentUser(c)
	if !ok {
		return
	}
	_ = h.audit.Record(c.Request.Context(), logics.RecordAdminAuditInput{Actor: *user, TargetKind: "update", TargetID: target, Action: action, Before: before, After: after, IPAddress: c.ClientIP(), UserAgent: c.Request.UserAgent()})
}

func (h *AdminUpdateHandler) recordUpdate(c *gin.Context, action, target string, backupMode coreupdater.BackupMode, state coreupdater.OperationState, operationErr error) {
	result := "success"
	if operationErr != nil {
		result = "failure"
	}
	runtimeMode := ""
	if status, err := h.service.Status(c.Request.Context()); err == nil {
		runtimeMode = status.RuntimeMode
	}
	h.record(c, action, target, nil, gin.H{
		"result":       result,
		"operation_id": state.OperationID,
		"backup_mode":  backupMode,
		"runtime_mode": runtimeMode,
		"phase":        state.Phase,
	})
}

func ensureUpdateCSRF(c *gin.Context) (string, error) {
	if value, err := c.Cookie(updateCSRFCookie); err == nil && value != "" {
		return value, nil
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(updateCSRFCookie, token, 600, "/", "", c.Request.TLS != nil, false)
	return token, nil
}

func validateUpdateMutation(c *gin.Context) error {
	origin := strings.TrimSpace(c.GetHeader("Origin"))
	parsed, err := url.Parse(origin)
	if err != nil || origin == "" || !strings.EqualFold(parsed.Host, c.Request.Host) || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("update request origin is not allowed")
	}
	cookie, err := c.Cookie(updateCSRFCookie)
	if err != nil || cookie == "" || !hmac.Equal([]byte(cookie), []byte(c.GetHeader(updateCSRFHeader))) {
		return fmt.Errorf("update CSRF token is invalid")
	}
	return nil
}
