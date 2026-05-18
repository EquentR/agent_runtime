package handlers

import (
	"errors"
	"net/http"

	"github.com/EquentR/agent_runtime/app/logics"
	"github.com/EquentR/agent_runtime/app/models"
	resp "github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/gin-gonic/gin"
)

const authSessionCookieName = logics.DefaultAuthSessionCookieName

const (
	authUserContextKey    = "auth.user"
	authSessionContextKey = "auth.session"
)

var errAdminRequired = errors.New("需要管理员权限")

type AuthMiddleware struct {
	logic *logics.AuthLogic
}

func NewAuthMiddleware(logic *logics.AuthLogic) *AuthMiddleware {
	return &AuthMiddleware{logic: logic}
}

func (m *AuthMiddleware) RequireSession() gin.HandlerFunc {
	return m.require(nil)
}

func (m *AuthMiddleware) RequireActiveUser() gin.HandlerFunc {
	return m.require(requireActiveUserState)
}

func (m *AuthMiddleware) RequireAdmin() gin.HandlerFunc {
	return m.require(func(user *models.User) error {
		if err := requireActiveUserState(user); err != nil {
			return err
		}
		if user.Role != models.UserRoleAdmin {
			return errAdminRequired
		}
		return nil
	})
}

func (m *AuthMiddleware) RequireActiveUserOption() resp.WrapperOption {
	return resp.WithMiddlewares(m.RequireActiveUser())
}

func (m *AuthMiddleware) RequireAdminOption() resp.WrapperOption {
	return resp.WithMiddlewares(m.RequireAdmin())
}

func (m *AuthMiddleware) require(check func(*models.User) error) gin.HandlerFunc {
	return func(c *gin.Context) {
		user, session, err := m.resolve(c)
		if err != nil {
			status := http.StatusUnauthorized
			if !errors.Is(err, logics.ErrUnauthorized) {
				status = http.StatusInternalServerError
			}
			resp.BadJson(c, nil, err, resp.WithCode(status))
			c.Abort()
			return
		}
		c.Set(authUserContextKey, user)
		c.Set(authSessionContextKey, session)
		if check != nil {
			if err := check(user); err != nil {
				resp.BadJson(c, nil, err, resp.WithCode(authStatusCode(err, http.StatusForbidden)))
				c.Abort()
				return
			}
		}
		c.Next()
	}
}

func (m *AuthMiddleware) RequireSessionOption() resp.WrapperOption {
	return resp.WithMiddlewares(m.RequireSession())
}

func (m *AuthMiddleware) CurrentUser(c *gin.Context) (*models.User, bool) {
	value, ok := c.Get(authUserContextKey)
	if !ok {
		return nil, false
	}
	user, ok := value.(*models.User)
	return user, ok
}

func (m *AuthMiddleware) resolve(c *gin.Context) (*models.User, *models.UserSession, error) {
	if m == nil || m.logic == nil {
		return nil, nil, logics.ErrUnauthorized
	}
	cookie, err := c.Cookie(m.logic.CookieName())
	if err != nil {
		return nil, nil, logics.ErrUnauthorized
	}
	return m.logic.CurrentUser(c.Request.Context(), cookie)
}

func requireActiveUserState(user *models.User) error {
	if user == nil {
		return logics.ErrUnauthorized
	}
	switch user.Status {
	case models.UserStatusDisabled:
		return logics.ErrUserDisabled
	case models.UserStatusNeedsEmailBinding:
		return logics.ErrEmailBindingRequired
	case models.UserStatusPendingEmailVerification:
		return logics.ErrEmailVerificationRequired
	}
	if user.ForcePasswordChange {
		return logics.ErrPasswordChangeRequired
	}
	if user.Email == "" {
		return logics.ErrEmailBindingRequired
	}
	if user.EmailVerifiedAt == nil {
		return logics.ErrEmailVerificationRequired
	}
	if user.Status != "" && user.Status != models.UserStatusActive {
		return logics.ErrEmailVerificationRequired
	}
	return nil
}
