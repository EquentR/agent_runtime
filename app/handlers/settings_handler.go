package handlers

import (
	"net/http"

	"github.com/EquentR/agent_runtime/app/logics"
	resp "github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/gin-gonic/gin"
)

type SettingsHandler struct {
	settings *logics.SettingsLogic
}

func NewSettingsHandler(settings *logics.SettingsLogic) *SettingsHandler {
	return &SettingsHandler{settings: settings}
}

func (h *SettingsHandler) Register(rg *gin.RouterGroup) {
	if h.settings == nil {
		return
	}
	resp.HandlerWrapper(rg, "settings", []*resp.Handler{
		resp.NewJsonOptionsHandler(h.handleGetRegistration),
		resp.NewJsonOptionsHandler(h.handleGetTurnstile),
	})
}

// @Summary 获取公开注册配置
// @Description 返回无需登录即可读取的公开注册开关。
// @Tags settings
// @Produce json
// @Success 200 {object} PublicRegistrationSettingsSwaggerResponse
// @Failure 500 {object} ErrorSwaggerResponse
// @Router /settings/registration [get]
func (h *SettingsHandler) handleGetRegistration() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "/registration", func(c *gin.Context) (any, []resp.ResOpt, error) {
		settings, err := h.settings.GetPublicRegistration(c.Request.Context())
		if err != nil {
			return nil, nil, err
		}
		return settings, nil, nil
	}, nil
}

// @Summary 获取公开 Turnstile 配置
// @Description 返回无需登录即可读取的 Cloudflare Turnstile 站点密钥和保护范围，不包含 secret。
// @Tags settings
// @Produce json
// @Success 200 {object} PublicTurnstileSettingsSwaggerResponse
// @Failure 500 {object} ErrorSwaggerResponse
// @Router /settings/turnstile [get]
func (h *SettingsHandler) handleGetTurnstile() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "/turnstile", func(c *gin.Context) (any, []resp.ResOpt, error) {
		settings, err := h.settings.GetPublicTurnstile(c.Request.Context())
		if err != nil {
			return nil, nil, err
		}
		return settings, nil, nil
	}, nil
}
