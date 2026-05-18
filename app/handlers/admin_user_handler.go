package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/EquentR/agent_runtime/app/logics"
	"github.com/EquentR/agent_runtime/app/models"
	resp "github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

var (
	errAdminUserNotFound  = errors.New("admin user not found")
	errInvalidAdminUserID = errors.New("invalid admin user id")
)

type AdminUserHandler struct {
	db                *gorm.DB
	audit             *logics.AdminAuditLogic
	emailVerification *logics.EmailVerificationLogic
	middlewares       []gin.HandlerFunc
}

type createAdminUserRequest struct {
	Username    string `json:"username"`
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

type updateAdminUserRequest struct {
	Role                *string `json:"role"`
	Status              *string `json:"status"`
	Email               *string `json:"email"`
	DisplayName         *string `json:"display_name"`
	EmailVerified       *bool   `json:"email_verified"`
	EmailVerifiedAt     *string `json:"email_verified_at"`
	ForcePasswordChange *bool   `json:"force_password_change"`
}

type resetAdminUserPasswordRequest struct {
	Password string `json:"password"`
}

func NewAdminUserHandler(db *gorm.DB, audit *logics.AdminAuditLogic, emailVerification *logics.EmailVerificationLogic, middlewares ...gin.HandlerFunc) *AdminUserHandler {
	return &AdminUserHandler{db: db, audit: audit, emailVerification: emailVerification, middlewares: middlewares}
}

func (h *AdminUserHandler) Register(rg *gin.RouterGroup) {
	if h.db == nil {
		return
	}
	options := []resp.WrapperOption{}
	if len(h.middlewares) > 0 {
		options = append(options, resp.WithMiddlewares(h.middlewares...))
	}
	resp.HandlerWrapper(rg, "admin/users", []*resp.Handler{
		resp.NewJsonOptionsHandler(h.handleListUsers),
		resp.NewJsonOptionsHandler(h.handleGetUser),
		resp.NewJsonOptionsHandler(h.handleCreateUser),
		resp.NewJsonOptionsHandler(h.handleUpdateUser),
		resp.NewJsonOptionsHandler(h.handleResetPassword),
		resp.NewJsonOptionsHandler(h.handleResendVerification),
	}, options...)
}

func (h *AdminUserHandler) handleListUsers() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "", func(c *gin.Context) (any, []resp.ResOpt, error) {
		query := h.db.WithContext(c.Request.Context()).Model(&models.User{})
		if q := strings.TrimSpace(c.Query("q")); q != "" {
			like := "%" + q + "%"
			query = query.Where("username LIKE ? OR email LIKE ? OR display_name LIKE ?", like, like, like)
		}
		if role := strings.TrimSpace(c.Query("role")); role != "" {
			query = query.Where("role = ?", role)
		}
		if status := strings.TrimSpace(c.Query("status")); status != "" {
			query = query.Where("status = ?", status)
		}
		var users []models.User
		if err := query.Order("id asc").Find(&users).Error; err != nil {
			return nil, nil, err
		}
		result := make([]gin.H, 0, len(users))
		for idx := range users {
			result = append(result, authUserResponse(&users[idx]))
		}
		return result, nil, nil
	}, nil
}

func (h *AdminUserHandler) handleGetUser() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "/:id", func(c *gin.Context) (any, []resp.ResOpt, error) {
		user, resOpts, err := h.loadUser(c, c.Param("id"))
		if err != nil {
			return nil, resOpts, err
		}
		return authUserResponse(&user), nil, nil
	}, nil
}

func (h *AdminUserHandler) handleCreateUser() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodPost, "", func(c *gin.Context) (any, []resp.ResOpt, error) {
		actor, err := h.requireActor(c)
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, err
		}
		var request createAdminUserRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}
		username := strings.TrimSpace(request.Username)
		email := normalizeAdminUserEmail(request.Email)
		password := request.Password
		if username == "" {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, fmt.Errorf("username is required")
		}
		if email == "" {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, logics.ErrEmailRequired
		}
		if len(password) < 6 {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, fmt.Errorf("password must be at least 6 characters")
		}
		if err := h.ensureUserIdentityAvailable(c, 0, username, email); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusConflict)}, err
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return nil, nil, err
		}
		displayName := strings.TrimSpace(request.DisplayName)
		if displayName == "" {
			displayName = username
		}
		user := models.User{
			Username:            username,
			Email:               email,
			DisplayName:         displayName,
			PasswordHash:        string(hash),
			Role:                models.UserRoleUser,
			Status:              models.UserStatusPendingEmailVerification,
			ForcePasswordChange: true,
		}
		if err := h.db.WithContext(c.Request.Context()).Create(&user).Error; err != nil {
			return nil, adminUserWriteErrorOptions(err), err
		}
		if err := h.recordAudit(c, *actor, "user", strconv.FormatUint(user.ID, 10), "admin.users.create", nil, user); err != nil {
			return nil, nil, err
		}
		return authUserResponse(&user), nil, nil
	}, nil
}

func (h *AdminUserHandler) handleUpdateUser() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodPatch, "/:id", func(c *gin.Context) (any, []resp.ResOpt, error) {
		actor, err := h.requireActor(c)
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, err
		}
		user, resOpts, err := h.loadUser(c, c.Param("id"))
		if err != nil {
			return nil, resOpts, err
		}
		before := user
		var request updateAdminUserRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}
		if request.Role != nil {
			role := strings.TrimSpace(*request.Role)
			if !isValidAdminUserRole(role) {
				return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, fmt.Errorf("invalid user role")
			}
			user.Role = role
		}
		if request.Status != nil {
			status := strings.TrimSpace(*request.Status)
			if !isValidAdminUserStatus(status) {
				return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, fmt.Errorf("invalid user status")
			}
			user.Status = status
		}
		if request.Email != nil {
			email := normalizeAdminUserEmail(*request.Email)
			if email == "" {
				return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, logics.ErrEmailRequired
			}
			if err := h.ensureUserIdentityAvailable(c, user.ID, user.Username, email); err != nil {
				return nil, []resp.ResOpt{resp.WithCode(http.StatusConflict)}, err
			}
			user.Email = email
		}
		if request.DisplayName != nil {
			user.DisplayName = strings.TrimSpace(*request.DisplayName)
		}
		if request.EmailVerifiedAt != nil {
			verifiedAt, err := parseAdminUserVerifiedAt(*request.EmailVerifiedAt)
			if err != nil {
				return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
			}
			user.EmailVerifiedAt = verifiedAt
		}
		if request.EmailVerified != nil {
			if *request.EmailVerified {
				if user.EmailVerifiedAt == nil {
					now := time.Now().UTC()
					user.EmailVerifiedAt = &now
				}
			} else {
				user.EmailVerifiedAt = nil
			}
		}
		if request.ForcePasswordChange != nil {
			user.ForcePasswordChange = *request.ForcePasswordChange
		}
		if err := h.db.WithContext(c.Request.Context()).Save(&user).Error; err != nil {
			return nil, adminUserWriteErrorOptions(err), err
		}
		if err := h.recordAudit(c, *actor, "user", strconv.FormatUint(user.ID, 10), "admin.users.update", before, user); err != nil {
			return nil, nil, err
		}
		return authUserResponse(&user), nil, nil
	}, nil
}

func (h *AdminUserHandler) handleResetPassword() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodPost, "/:id/reset-password", func(c *gin.Context) (any, []resp.ResOpt, error) {
		actor, err := h.requireActor(c)
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, err
		}
		user, resOpts, err := h.loadUser(c, c.Param("id"))
		if err != nil {
			return nil, resOpts, err
		}
		before := user
		var request resetAdminUserPasswordRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}
		if len(request.Password) < 6 {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, fmt.Errorf("password must be at least 6 characters")
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(request.Password), bcrypt.DefaultCost)
		if err != nil {
			return nil, nil, err
		}
		user.PasswordHash = string(hash)
		user.ForcePasswordChange = true
		if err := h.db.WithContext(c.Request.Context()).Save(&user).Error; err != nil {
			return nil, nil, err
		}
		if err := h.recordAudit(c, *actor, "user", strconv.FormatUint(user.ID, 10), "admin.users.reset_password", before, user); err != nil {
			return nil, nil, err
		}
		return authUserResponse(&user), nil, nil
	}, nil
}

func (h *AdminUserHandler) handleResendVerification() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodPost, "/:id/resend-verification", func(c *gin.Context) (any, []resp.ResOpt, error) {
		actor, err := h.requireActor(c)
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, err
		}
		if h.emailVerification == nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusServiceUnavailable)}, logics.ErrMailServiceUnavailable
		}
		user, resOpts, err := h.loadUser(c, c.Param("id"))
		if err != nil {
			return nil, resOpts, err
		}
		if err := h.emailVerification.Send(c.Request.Context(), logics.SendEmailVerificationInput{
			UserID:  user.ID,
			Email:   user.Email,
			Purpose: logics.EmailVerificationPurposeRegistration,
		}); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(authStatusCode(err, http.StatusBadRequest))}, err
		}
		after := gin.H{"sent": true, "email": user.Email}
		if err := h.recordAudit(c, *actor, "user", strconv.FormatUint(user.ID, 10), "admin.users.resend_verification", user, after); err != nil {
			return nil, nil, err
		}
		return after, nil, nil
	}, nil
}

func (h *AdminUserHandler) loadUser(c *gin.Context, rawID string) (models.User, []resp.ResOpt, error) {
	id, err := strconv.ParseUint(strings.TrimSpace(rawID), 10, 64)
	if err != nil || id == 0 {
		return models.User{}, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, errInvalidAdminUserID
	}
	var user models.User
	if err := h.db.WithContext(c.Request.Context()).First(&user, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.User{}, []resp.ResOpt{resp.WithCode(resp.NotFound)}, errAdminUserNotFound
		}
		return models.User{}, nil, err
	}
	return user, nil, nil
}

func (h *AdminUserHandler) requireActor(c *gin.Context) (*models.User, error) {
	actor := currentAuthUser(c)
	if actor == nil {
		return nil, logics.ErrUnauthorized
	}
	return actor, nil
}

func (h *AdminUserHandler) recordAudit(c *gin.Context, actor models.User, targetKind string, targetID string, action string, before any, after any) error {
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

func normalizeAdminUserEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func (h *AdminUserHandler) ensureUserIdentityAvailable(c *gin.Context, userID uint64, username string, email string) error {
	username = strings.TrimSpace(username)
	email = normalizeAdminUserEmail(email)
	if username != "" {
		query := h.db.WithContext(c.Request.Context()).Where("username = ? AND id <> ?", username, userID)
		var existing models.User
		err := query.Take(&existing).Error
		if err == nil {
			return logics.ErrUsernameTaken
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}
	if email == "" {
		return nil
	}
	var existing models.User
	err := h.db.WithContext(c.Request.Context()).Where("email = ? AND id <> ?", email, userID).Take(&existing).Error
	if err == nil {
		return logics.ErrEmailTaken
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	err = h.db.WithContext(c.Request.Context()).Where("LOWER(username) = ? AND id <> ?", email, userID).Take(&existing).Error
	if err == nil {
		return logics.ErrEmailTaken
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	usernameAsEmail := normalizeAdminUserEmail(username)
	if usernameAsEmail == "" {
		return nil
	}
	err = h.db.WithContext(c.Request.Context()).Where("email = ? AND id <> ?", usernameAsEmail, userID).Take(&existing).Error
	if err == nil {
		return logics.ErrUsernameTaken
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return nil
}

func isValidAdminUserRole(role string) bool {
	return role == models.UserRoleAdmin || role == models.UserRoleUser
}

func isValidAdminUserStatus(status string) bool {
	switch status {
	case models.UserStatusPendingEmailVerification,
		models.UserStatusActive,
		models.UserStatusDisabled,
		models.UserStatusNeedsEmailBinding:
		return true
	default:
		return false
	}
}

func parseAdminUserVerifiedAt(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, err
	}
	parsed = parsed.UTC()
	return &parsed, nil
}

func adminUserWriteErrorOptions(err error) []resp.ResOpt {
	if err == nil {
		return nil
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "unique") || strings.Contains(lower, "duplicate") {
		return []resp.ResOpt{resp.WithCode(http.StatusConflict)}
	}
	return nil
}
