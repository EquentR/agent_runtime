package handlers

import (
	"net/http"
	"time"

	coreupdater "github.com/EquentR/agent_runtime/core/updater"
	"github.com/gin-gonic/gin"
)

type HealthHandler struct {
	build coreupdater.BuildInfo
	store *coreupdater.HealthHandshakeStore
}

func NewHealthHandler(build coreupdater.BuildInfo, stores ...*coreupdater.HealthHandshakeStore) *HealthHandler {
	var store *coreupdater.HealthHandshakeStore
	if len(stores) > 0 {
		store = stores[0]
	}
	return &HealthHandler{build: build.Normalized(), store: store}
}

func (h *HealthHandler) Register(group *gin.RouterGroup) {
	group.GET("/health", func(c *gin.Context) {
		if token := c.GetHeader(coreupdater.UpdateHealthTokenHeader); h.store != nil && token != "" {
			if err := h.store.Verify(token, h.build.Version, time.Now().UTC()); err != nil {
				c.AbortWithStatusJSON(http.StatusServiceUnavailable, coreupdater.HealthResponse{Ready: false, Version: h.build.Version})
				return
			}
		}
		c.JSON(http.StatusOK, coreupdater.HealthResponse{Ready: true, Version: h.build.Version})
	})
}
