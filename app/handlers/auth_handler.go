package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/EquentR/agent_runtime/app/logics"
	"github.com/EquentR/agent_runtime/app/models"
	resp "github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/gin-gonic/gin"
)

type AuthHandler struct {
	logic             *logics.AuthLogic
	middleware        *AuthMiddleware
	settings          AuthHandlerSettingsReader
	emailVerification *logics.EmailVerificationLogic
	turnstileVerifier logics.TurnstileVerifier
}

type registerRequest struct {
	Username        string `json:"username"`
	Email           string `json:"email"`
	Password        string `json:"password"`
	ConfirmPassword string `json:"confirm_password"`
	TurnstileToken  string `json:"turnstile_token"`
}

type loginRequest struct {
	Username       string `json:"username"`
	Password       string `json:"password"`
	TurnstileToken string `json:"turnstile_token"`
}

type emailVerificationSendRequest struct {
	UserID         uint64 `json:"user_id"`
	Email          string `json:"email"`
	Purpose        string `json:"purpose"`
	TurnstileToken string `json:"turnstile_token"`
}

type emailVerificationVerifyRequest struct {
	UserID  uint64 `json:"user_id"`
	Email   string `json:"email"`
	Purpose string `json:"purpose"`
	Code    string `json:"code"`
}

type AuthHandlerSettingsReader interface {
	GetTurnstile(ctx context.Context) (logics.TurnstileSettings, error)
}

type AuthHandlerOption func(*AuthHandler)

func WithAuthHandlerSettings(settings AuthHandlerSettingsReader) AuthHandlerOption {
	return func(h *AuthHandler) {
		h.settings = settings
	}
}

func WithAuthHandlerEmailVerification(verification *logics.EmailVerificationLogic) AuthHandlerOption {
	return func(h *AuthHandler) {
		h.emailVerification = verification
	}
}

func WithAuthHandlerTurnstileVerifier(verifier logics.TurnstileVerifier) AuthHandlerOption {
	return func(h *AuthHandler) {
		h.turnstileVerifier = verifier
	}
}

func NewAuthHandler(logic *logics.AuthLogic, opts ...AuthHandlerOption) *AuthHandler {
	handler := &AuthHandler{logic: logic, middleware: NewAuthMiddleware(logic)}
	if logic != nil {
		handler.emailVerification = logic.EmailVerification()
	}
	for _, opt := range opts {
		if opt != nil {
			opt(handler)
		}
	}
	return handler
}

func (h *AuthHandler) Register(rg *gin.RouterGroup) {
	if h.logic == nil {
		return
	}
	resp.HandlerWrapper(rg, "auth", []*resp.Handler{
		resp.NewJsonOptionsHandler(h.handleRegister),
		resp.NewJsonOptionsHandler(h.handleLogin),
		resp.NewJsonOptionsHandler(h.handleLogout),
		resp.NewJsonOptionsHandler(h.handleCurrentUser),
		resp.NewJsonOptionsHandler(h.handleSendEmailVerification),
		resp.NewJsonOptionsHandler(h.handleVerifyEmail),
	})
}

// handleRegister 返回用户注册接口的路由定义。
//
// @Summary 注册账号
// @Description 使用用户名和密码创建账号，注册成功后返回用户基础信息。
// @Tags auth
// @Accept json
// @Produce json
// @Param body body AuthRegisterSwaggerRequest true "注册请求"
// @Success 200 {object} AuthUserSwaggerResponse
// @Failure 400 {object} ErrorSwaggerResponse
// @Failure 409 {object} ErrorSwaggerResponse
// @Router /auth/register [post]
func (h *AuthHandler) handleRegister() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodPost, "/register", func(c *gin.Context) (any, []resp.ResOpt, error) {
		var request registerRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}
		if err := h.verifyTurnstile(c, request.TurnstileToken, func(settings logics.TurnstileSettings) bool {
			return settings.ProtectRegistration
		}); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}
		user, err := h.logic.RegisterWithInput(c.Request.Context(), logics.RegisterInput{
			Username:        request.Username,
			Email:           request.Email,
			Password:        request.Password,
			ConfirmPassword: request.ConfirmPassword,
			TurnstileToken:  request.TurnstileToken,
		})
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(authStatusCode(err, http.StatusBadRequest))}, err
		}
		return authUserResponse(user), nil, nil
	}, nil
}

// handleLogin 返回用户登录接口的路由定义。
//
// @Summary 用户登录
// @Description 校验用户名密码并写入 session cookie，成功后返回当前用户信息。
// @Tags auth
// @Accept json
// @Produce json
// @Param body body AuthLoginSwaggerRequest true "登录请求"
// @Success 200 {object} AuthUserSwaggerResponse
// @Failure 400 {object} ErrorSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Router /auth/login [post]
func (h *AuthHandler) handleLogin() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodPost, "/login", func(c *gin.Context) (any, []resp.ResOpt, error) {
		var request loginRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}
		if err := h.verifyTurnstile(c, request.TurnstileToken, func(settings logics.TurnstileSettings) bool {
			return settings.ProtectLogin
		}); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}
		user, session, err := h.logic.Login(c.Request.Context(), request.Username, request.Password)
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(authStatusCode(err, http.StatusUnauthorized))}, err
		}
		h.setSessionCookie(c, session)
		return authUserResponse(user), nil, nil
	}, nil
}

// handleLogout 返回退出登录接口的路由定义。
//
// @Summary 退出登录
// @Description 删除当前 session 并清理浏览器中的 session cookie。
// @Tags auth
// @Accept json
// @Produce json
// @Success 200 {object} AuthLogoutSwaggerResponse
// @Failure 500 {object} ErrorSwaggerResponse
// @Router /auth/logout [post]
func (h *AuthHandler) handleLogout() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodPost, "/logout", func(c *gin.Context) (any, []resp.ResOpt, error) {
		cookie, _ := c.Cookie(h.logic.CookieName())
		if err := h.logic.Logout(c.Request.Context(), cookie); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusInternalServerError)}, err
		}
		h.clearSessionCookie(c)
		return gin.H{"logged_out": true}, nil, nil
	}, nil
}

// handleCurrentUser 返回当前登录用户接口的路由定义。
//
// @Summary 获取当前用户
// @Description 校验 session cookie 后返回当前登录用户的基础信息。
// @Tags auth
// @Produce json
// @Success 200 {object} AuthUserSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Router /auth/me [get]
func (h *AuthHandler) handleCurrentUser() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "/me", func(c *gin.Context) (any, []resp.ResOpt, error) {
		user, ok := h.middleware.CurrentUser(c)
		if !ok || user == nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, logics.ErrUnauthorized
		}
		return authUserResponse(user), nil, nil
	}, []resp.WrapperOption{h.middleware.RequireSessionOption()}
}

// handleSendEmailVerification 返回发送邮箱验证码接口的路由定义。
//
// @Summary 发送邮箱验证码
// @Description 根据邮箱或用户 ID 发送邮箱验证码，可用于注册验证或邮箱绑定。
// @Tags auth
// @Accept json
// @Produce json
// @Param body body AuthEmailVerificationSendSwaggerRequest true "发送邮箱验证码请求"
// @Success 200 {object} AuthEmailVerificationSentSwaggerResponse
// @Failure 400 {object} ErrorSwaggerResponse
// @Failure 404 {object} ErrorSwaggerResponse
// @Failure 429 {object} ErrorSwaggerResponse
// @Failure 503 {object} ErrorSwaggerResponse
// @Router /auth/email-verification/send [post]
func (h *AuthHandler) handleSendEmailVerification() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodPost, "/email-verification/send", func(c *gin.Context) (any, []resp.ResOpt, error) {
		var request emailVerificationSendRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}
		if err := h.verifyTurnstile(c, request.TurnstileToken, func(settings logics.TurnstileSettings) bool {
			return settings.ProtectVerification
		}); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}
		if h.emailVerification == nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusServiceUnavailable)}, logics.ErrMailServiceUnavailable
		}
		if err := h.emailVerification.SendByEmail(c.Request.Context(), logics.SendEmailVerificationInput{
			UserID:  request.UserID,
			Email:   request.Email,
			Purpose: request.Purpose,
		}); err != nil {
			if isIndistinguishableEmailVerificationSendError(err) {
				return gin.H{"sent": true}, nil, nil
			}
			return nil, []resp.ResOpt{resp.WithCode(authStatusCode(err, http.StatusBadRequest))}, err
		}
		return gin.H{"sent": true}, nil, nil
	}, nil
}

// handleVerifyEmail 返回校验邮箱验证码接口的路由定义。
//
// @Summary 校验邮箱验证码
// @Description 校验邮箱验证码并返回更新后的用户信息。
// @Tags auth
// @Accept json
// @Produce json
// @Param body body AuthEmailVerificationVerifySwaggerRequest true "校验邮箱验证码请求"
// @Success 200 {object} AuthEmailVerificationVerifySwaggerResponse
// @Failure 400 {object} ErrorSwaggerResponse
// @Failure 403 {object} ErrorSwaggerResponse
// @Failure 404 {object} ErrorSwaggerResponse
// @Failure 429 {object} ErrorSwaggerResponse
// @Failure 503 {object} ErrorSwaggerResponse
// @Router /auth/email-verification/verify [post]
func (h *AuthHandler) handleVerifyEmail() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodPost, "/email-verification/verify", func(c *gin.Context) (any, []resp.ResOpt, error) {
		var request emailVerificationVerifyRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}
		if h.emailVerification == nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusServiceUnavailable)}, logics.ErrMailServiceUnavailable
		}
		user, err := h.emailVerification.Verify(c.Request.Context(), logics.VerifyEmailInput{
			UserID:  request.UserID,
			Email:   request.Email,
			Purpose: request.Purpose,
			Code:    request.Code,
		})
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(authStatusCode(err, http.StatusBadRequest))}, err
		}
		return authUserResponse(user), nil, nil
	}, nil
}

func (h *AuthHandler) setSessionCookie(c *gin.Context, session *models.UserSession) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(h.logic.CookieName(), session.ID, int(h.logic.SessionTTL().Seconds()), "/", "", false, true)
}

func (h *AuthHandler) clearSessionCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(h.logic.CookieName(), "", -1, "/", "", false, true)
}

func authUserResponse(user *models.User) gin.H {
	return gin.H{
		"id":                    user.ID,
		"username":              user.Username,
		"email":                 user.Email,
		"display_name":          user.DisplayName,
		"role":                  user.Role,
		"status":                user.Status,
		"email_verified_at":     user.EmailVerifiedAt,
		"force_password_change": user.ForcePasswordChange,
	}
}

func (h *AuthHandler) verifyTurnstile(c *gin.Context, token string, protected func(logics.TurnstileSettings) bool) error {
	if h == nil || h.settings == nil || protected == nil {
		return nil
	}
	settings, err := h.settings.GetTurnstile(c.Request.Context())
	if err != nil {
		return err
	}
	if !settings.Enabled || !protected(settings) {
		return nil
	}
	if h.turnstileVerifier == nil {
		return fmt.Errorf("turnstile verifier is not configured")
	}
	return h.turnstileVerifier.Verify(c.Request.Context(), token, c.ClientIP())
}

func isIndistinguishableEmailVerificationSendError(err error) bool {
	return errors.Is(err, logics.ErrEmailVerificationNotFound) ||
		errors.Is(err, logics.ErrEmailVerificationInvalidState) ||
		errors.Is(err, logics.ErrEmailVerificationCooldown) ||
		errors.Is(err, logics.ErrEmailVerificationTooManyAttempts)
}

func authStatusCode(err error, fallback int) int {
	switch {
	case errors.Is(err, logics.ErrUnauthorized), errors.Is(err, logics.ErrInvalidCredentials):
		return http.StatusUnauthorized
	case errors.Is(err, logics.ErrUsernameTaken), errors.Is(err, logics.ErrEmailTaken):
		return http.StatusConflict
	case errors.Is(err, logics.ErrPublicRegistrationDisabled),
		errors.Is(err, logics.ErrUserDisabled),
		errors.Is(err, logics.ErrEmailVerificationRequired),
		errors.Is(err, logics.ErrEmailBindingRequired),
		errors.Is(err, logics.ErrPasswordChangeRequired),
		errors.Is(err, logics.ErrEmailVerificationInvalidState):
		return http.StatusForbidden
	case errors.Is(err, logics.ErrMailServiceUnavailable):
		return http.StatusServiceUnavailable
	case errors.Is(err, logics.ErrEmailVerificationCooldown), errors.Is(err, logics.ErrEmailVerificationTooManyAttempts):
		return http.StatusTooManyRequests
	case errors.Is(err, logics.ErrEmailVerificationNotFound):
		return http.StatusNotFound
	default:
		return fallback
	}
}
