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
		user, err := h.logic.Register(c.Request.Context(), request.Username, request.Password, request.ConfirmPassword)
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

func (h *AuthHandler) setSessionCookie(c *gin.Context, session *models.UserSession) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(h.logic.CookieName(), session.ID, int(h.logic.SessionTTL().Seconds()), "/", "", false, true)
}

func (h *AuthHandler) clearSessionCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(h.logic.CookieName(), "", -1, "/", "", false, true)
}

func authUserResponse(user *models.User) gin.H {
	return gin.H{"id": user.ID, "username": user.Username, "role": user.Role}
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
