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
	})
}

func (h *SettingsHandler) handleGetRegistration() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "/registration", func(c *gin.Context) (any, []resp.ResOpt, error) {
		settings, err := h.settings.GetPublicRegistration(c.Request.Context())
		if err != nil {
			return nil, nil, err
		}
		return settings, nil, nil
	}, nil
}
