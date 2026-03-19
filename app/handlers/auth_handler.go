package handlers

import (
	"errors"
	"net/http"

	"github.com/EquentR/agent_runtime/app/logics"
	"github.com/EquentR/agent_runtime/app/models"
	resp "github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/gin-gonic/gin"
)

type AuthHandler struct {
	logic      *logics.AuthLogic
	middleware *AuthMiddleware
}

type registerRequest struct {
	Username        string `json:"username"`
	Password        string `json:"password"`
	ConfirmPassword string `json:"confirm_password"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func NewAuthHandler(logic *logics.AuthLogic) *AuthHandler {
	return &AuthHandler{logic: logic, middleware: NewAuthMiddleware(logic)}
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
	})
}

func (h *AuthHandler) handleRegister() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodPost, "/register", func(c *gin.Context) (any, []resp.ResOpt, error) {
		var request registerRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			return nil, []resp.ResOpt{resp.WithCode(http.StatusBadRequest)}, err
		}
		user, err := h.logic.Register(c.Request.Context(), request.Username, request.Password, request.ConfirmPassword)
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(authStatusCode(err, http.StatusBadRequest))}, err
		}
		return authUserResponse(user), nil, nil
	}, nil
}

func (h *AuthHandler) handleLogin() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodPost, "/login", func(c *gin.Context) (any, []resp.ResOpt, error) {
		var request loginRequest
		if err := c.ShouldBindJSON(&request); err != nil {
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

func (h *AuthHandler) handleCurrentUser() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "/me", func(c *gin.Context) (any, []resp.ResOpt, error) {
		user, _, err := h.middleware.resolve(c)
		if err != nil {
			return nil, []resp.ResOpt{resp.WithCode(authStatusCode(err, http.StatusUnauthorized))}, err
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
	return gin.H{"id": user.ID, "username": user.Username}
}

func authStatusCode(err error, fallback int) int {
	switch {
	case errors.Is(err, logics.ErrUnauthorized), errors.Is(err, logics.ErrInvalidCredentials):
		return http.StatusUnauthorized
	case errors.Is(err, logics.ErrUsernameTaken):
		return http.StatusConflict
	default:
		return fallback
	}
}
