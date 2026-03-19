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

type AuthMiddleware struct {
	logic *logics.AuthLogic
}

func NewAuthMiddleware(logic *logics.AuthLogic) *AuthMiddleware {
	return &AuthMiddleware{logic: logic}
}

func (m *AuthMiddleware) RequireSession() gin.HandlerFunc {
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
		c.Next()
	}
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
