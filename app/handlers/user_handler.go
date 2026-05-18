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
	Email string `json:"email"`
}

type confirmUserEmailVerificationRequest struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

func NewUserHandler(db *gorm.DB, emailVerification *logics.EmailVerificationLogic, middlewares ...gin.HandlerFunc) *UserHandler {
	return &UserHandler{db: db, emailVerification: emailVerification, middlewares: middlewares}
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

func (h *UserHandler) handleGetProfile() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "", func(c *gin.Context) (any, []resp.ResOpt, error) {
		user, err := h.currentUser(c)
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, err
		}
		return userProfileResponse(user), nil, nil
	}, nil
}

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
		if err := h.emailVerification.Send(c.Request.Context(), logics.SendEmailVerificationInput{
			UserID:  user.ID,
			Email:   request.Email,
			Purpose: logics.EmailVerificationPurposeEmailBinding,
		}); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(authStatusCode(err, http.StatusBadRequest))}, err
		}
		return gin.H{"sent": true}, nil, nil
	}, nil
}

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
		verified, err := h.emailVerification.Verify(c.Request.Context(), logics.VerifyEmailInput{
			UserID:  user.ID,
			Email:   request.Email,
			Purpose: logics.EmailVerificationPurposeEmailBinding,
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
