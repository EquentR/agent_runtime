package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/EquentR/agent_runtime/app/logics"
	"github.com/EquentR/agent_runtime/app/models"
	resp "github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const (
	userRequiredActionVerifyEmail    = "verify_email"
	userRequiredActionBindEmail      = "bind_email"
	userRequiredActionChangePassword = "change_password"
)

type UserHandler struct {
	db                *gorm.DB
	emailVerification *logics.EmailVerificationLogic
	settings          AuthHandlerSettingsReader
	turnstileVerifier logics.TurnstileVerifier
	middlewares       []gin.HandlerFunc
}

type updateUserProfileRequest struct {
	DisplayName *string `json:"display_name"`
}

type changeUserPasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	Password        string `json:"password"`
	ConfirmPassword string `json:"confirm_password"`
}

type startUserEmailVerificationRequest struct {
	Email          string `json:"email"`
	TurnstileToken string `json:"turnstile_token"`
}

type confirmUserEmailVerificationRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

func NewUserHandler(db *gorm.DB, emailVerification *logics.EmailVerificationLogic, middlewares ...gin.HandlerFunc) *UserHandler {
	return &UserHandler{db: db, emailVerification: emailVerification, middlewares: middlewares}
}

func (h *UserHandler) WithTurnstile(settings AuthHandlerSettingsReader, verifier logics.TurnstileVerifier) *UserHandler {
	h.settings = settings
	h.turnstileVerifier = verifier
	return h
}

func (h *UserHandler) Register(rg *gin.RouterGroup) {
	if h.db == nil {
		return
	}
	options := []resp.WrapperOption{}
	if len(h.middlewares) > 0 {
		options = append(options, resp.WithMiddlewares(h.middlewares...))
	}
	resp.HandlerWrapper(rg, "users/me", []*resp.Handler{
		resp.NewJsonOptionsHandler(h.handleGetProfile),
		resp.NewJsonOptionsHandler(h.handleUpdateProfile),
		resp.NewJsonOptionsHandler(h.handleChangePassword),
		resp.NewJsonOptionsHandler(h.handleStartEmailVerification),
		resp.NewJsonOptionsHandler(h.handleConfirmEmailVerification),
	}, options...)
}

// @Summary 获取当前用户资料
// @Description 返回当前 session 用户资料、邮箱验证状态和 required actions。
// @Tags users
// @Produce json
// @Success 200 {object} AuthUserSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Router /users/me [get]
func (h *UserHandler) handleGetProfile() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "", func(c *gin.Context) (any, []resp.ResOpt, error) {
		user, err := h.currentUser(c)
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, err
		}
		return userProfileResponse(user), nil, nil
	}, nil
}

// @Summary 更新当前用户资料
// @Description 更新当前用户 display_name。
// @Tags users
// @Accept json
// @Produce json
// @Param request body UserProfileUpdateSwaggerRequest true "用户资料更新"
// @Success 200 {object} AuthUserSwaggerResponse
// @Failure 400 {object} ErrorSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Failure 403 {object} ErrorSwaggerResponse
// @Router /users/me [patch]
func (h *UserHandler) handleUpdateProfile() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodPatch, "", func(c *gin.Context) (any, []resp.ResOpt, error) {
		user, err := h.currentUser(c)
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, err
		}
		if err := ensureProfileUserCanMutate(user); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(authStatusCode(err, http.StatusForbidden))}, err
		}
		var request updateUserProfileRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}
		if request.DisplayName != nil {
			displayName := strings.TrimSpace(*request.DisplayName)
			if displayName == "" {
				displayName = user.Username
			}
			user.DisplayName = displayName
		}
		if err := h.db.WithContext(c.Request.Context()).Save(user).Error; err != nil {
			return nil, nil, err
		}
		return userProfileResponse(user), nil, nil
	}, nil
}

// @Summary 当前用户修改密码
// @Description 校验当前密码后修改密码，并清除 force_password_change。
// @Tags users
// @Accept json
// @Produce json
// @Param request body UserPasswordChangeSwaggerRequest true "修改密码请求"
// @Success 200 {object} AuthUserSwaggerResponse
// @Failure 400 {object} ErrorSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Failure 403 {object} ErrorSwaggerResponse
// @Router /users/me/password [post]
func (h *UserHandler) handleChangePassword() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodPost, "/password", func(c *gin.Context) (any, []resp.ResOpt, error) {
		user, err := h.currentUser(c)
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, err
		}
		if err := ensureProfileUserCanMutate(user); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(authStatusCode(err, http.StatusForbidden))}, err
		}
		var request changeUserPasswordRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}
		if err := validateUserPasswordChange(user, request); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(authStatusCode(err, http.StatusBadRequest))}, err
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(request.Password), bcrypt.DefaultCost)
		if err != nil {
			return nil, nil, err
		}
		user.PasswordHash = string(hash)
		user.ForcePasswordChange = false
		if err := h.db.WithContext(c.Request.Context()).Save(user).Error; err != nil {
			return nil, nil, err
		}
		return userProfileResponse(user), nil, nil
	}, nil
}

// @Summary 当前用户发送邮箱验证码
// @Description 为当前用户发送邮箱绑定或注册验证验证码。
// @Tags users
// @Accept json
// @Produce json
// @Param request body UserEmailVerificationStartSwaggerRequest true "发送验证码请求"
// @Success 200 {object} AuthEmailVerificationSentSwaggerResponse
// @Failure 400 {object} ErrorSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Failure 403 {object} ErrorSwaggerResponse
// @Failure 429 {object} ErrorSwaggerResponse
// @Failure 503 {object} ErrorSwaggerResponse
// @Router /users/me/email-verification [post]
func (h *UserHandler) handleStartEmailVerification() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodPost, "/email-verification", func(c *gin.Context) (any, []resp.ResOpt, error) {
		user, err := h.currentUser(c)
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, err
		}
		if h.emailVerification == nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusServiceUnavailable)}, logics.ErrMailServiceUnavailable
		}
		if err := ensureProfileUserCanMutate(user); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(authStatusCode(err, http.StatusForbidden))}, err
		}
		var request startUserEmailVerificationRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}
		if err := h.verifyTurnstile(c, request.TurnstileToken); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}
		email := profileEmailVerificationEmail(user, request.Email)
		if err := h.emailVerification.Send(c.Request.Context(), logics.SendEmailVerificationInput{
			UserID:  user.ID,
			Email:   email,
			Purpose: profileEmailVerificationPurpose(user),
		}); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(authStatusCode(err, http.StatusBadRequest))}, err
		}
		return gin.H{"sent": true}, nil, nil
	}, nil
}

// @Summary 当前用户确认邮箱验证码
// @Description 校验当前用户邮箱绑定或注册验证验证码。
// @Tags users
// @Accept json
// @Produce json
// @Param request body UserEmailVerificationConfirmSwaggerRequest true "确认验证码请求"
// @Success 200 {object} AuthUserSwaggerResponse
// @Failure 400 {object} ErrorSwaggerResponse
// @Failure 401 {object} ErrorSwaggerResponse
// @Failure 403 {object} ErrorSwaggerResponse
// @Failure 429 {object} ErrorSwaggerResponse
// @Failure 503 {object} ErrorSwaggerResponse
// @Router /users/me/email-verification/confirm [post]
func (h *UserHandler) handleConfirmEmailVerification() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodPost, "/email-verification/confirm", func(c *gin.Context) (any, []resp.ResOpt, error) {
		user, err := h.currentUser(c)
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, err
		}
		if h.emailVerification == nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusServiceUnavailable)}, logics.ErrMailServiceUnavailable
		}
		if err := ensureProfileUserCanMutate(user); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(authStatusCode(err, http.StatusForbidden))}, err
		}
		var request confirmUserEmailVerificationRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}
		email := profileEmailVerificationEmail(user, request.Email)
		verified, err := h.emailVerification.Verify(c.Request.Context(), logics.VerifyEmailInput{
			UserID:  user.ID,
			Email:   email,
			Purpose: profileEmailVerificationPurpose(user),
			Code:    request.Code,
		})
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(authStatusCode(err, http.StatusBadRequest))}, err
		}
		return userProfileResponse(verified), nil, nil
	}, nil
}

func (h *UserHandler) currentUser(c *gin.Context) (*models.User, error) {
	user := currentAuthUser(c)
	if user == nil {
		return nil, logics.ErrUnauthorized
	}
	var reloaded models.User
	if err := h.db.WithContext(c.Request.Context()).First(&reloaded, user.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, logics.ErrUnauthorized
		}
		return nil, err
	}
	return &reloaded, nil
}

func (h *UserHandler) verifyTurnstile(c *gin.Context, token string) error {
	if h == nil || h.settings == nil {
		return nil
	}
	settings, err := h.settings.GetTurnstile(c.Request.Context())
	if err != nil {
		return err
	}
	if !settings.Enabled || !settings.ProtectVerification {
		return nil
	}
	if h.turnstileVerifier == nil {
		return fmt.Errorf("turnstile verifier is not configured")
	}
	return h.turnstileVerifier.Verify(c.Request.Context(), token, c.ClientIP())
}

func validateUserPasswordChange(user *models.User, request changeUserPasswordRequest) error {
	if user == nil {
		return logics.ErrUnauthorized
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(request.CurrentPassword)); err != nil {
		return logics.ErrInvalidCredentials
	}
	if len(request.Password) < 6 {
		return fmt.Errorf("password must be at least 6 characters")
	}
	if request.Password != request.ConfirmPassword {
		return fmt.Errorf("password confirmation does not match")
	}
	return nil
}

func ensureProfileUserCanMutate(user *models.User) error {
	if user == nil {
		return logics.ErrUnauthorized
	}
	if user.Status == models.UserStatusDisabled {
		return logics.ErrUserDisabled
	}
	return nil
}

func profileEmailVerificationPurpose(user *models.User) string {
	if user != nil && user.Status == models.UserStatusPendingEmailVerification {
		return logics.EmailVerificationPurposeRegistration
	}
	return logics.EmailVerificationPurposeEmailBinding
}

func profileEmailVerificationEmail(user *models.User, requestedEmail string) string {
	if user != nil && user.Status == models.UserStatusPendingEmailVerification && strings.TrimSpace(requestedEmail) == "" {
		return user.Email
	}
	return requestedEmail
}

func userProfileResponse(user *models.User) gin.H {
	emailVerified := user != nil && user.EmailVerifiedAt != nil
	return gin.H{
		"id":                    user.ID,
		"username":              user.Username,
		"email":                 user.Email,
		"display_name":          displayNameOrUsername(user),
		"role":                  user.Role,
		"status":                user.Status,
		"email_verified":        emailVerified,
		"email_verified_at":     user.EmailVerifiedAt,
		"force_password_change": user.ForcePasswordChange,
		"required_actions":      userRequiredActions(user),
	}
}

func displayNameOrUsername(user *models.User) string {
	if user == nil {
		return ""
	}
	displayName := strings.TrimSpace(user.DisplayName)
	if displayName == "" {
		return user.Username
	}
	return displayName
}

func userRequiredActions(user *models.User) []string {
	if user == nil || user.Status == models.UserStatusDisabled {
		return []string{}
	}
	actions := make([]string, 0, 3)
	if user.Status == models.UserStatusPendingEmailVerification {
		actions = append(actions, userRequiredActionVerifyEmail)
	}
	if user.Status == models.UserStatusNeedsEmailBinding || strings.TrimSpace(user.Email) == "" {
		actions = append(actions, userRequiredActionBindEmail)
	}
	if user.ForcePasswordChange {
		actions = append(actions, userRequiredActionChangePassword)
	}
	return actions
}
