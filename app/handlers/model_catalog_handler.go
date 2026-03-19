package handlers

import (
	"fmt"
	"net/http"

	coreagent "github.com/EquentR/agent_runtime/core/agent"
	resp "github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/gin-gonic/gin"
)

type ModelCatalogHandler struct {
	resolver     *coreagent.ModelResolver
	middlewares  []gin.HandlerFunc
	authRequired bool
}

func NewModelCatalogHandler(resolver *coreagent.ModelResolver, middlewares ...gin.HandlerFunc) *ModelCatalogHandler {
	return &ModelCatalogHandler{resolver: resolver, middlewares: middlewares, authRequired: len(middlewares) > 0}
}

func (h *ModelCatalogHandler) Register(rg *gin.RouterGroup) {
	if h.resolver == nil {
		return
	}
	options := []resp.WrapperOption{}
	if len(h.middlewares) > 0 {
		options = append(options, resp.WithMiddlewares(h.middlewares...))
	}
	resp.HandlerWrapper(rg, "models", []*resp.Handler{
		resp.NewJsonOptionsHandler(h.handleGetModelCatalog),
	}, options...)
}

// handleGetModelCatalog 返回当前已配置的 provider/model 目录。
//
// @Summary 获取模型目录
// @Description 返回当前服务启动时注入的全部 LLM provider/model 配置，以及前端可直接使用的默认 provider/model。
// @Tags models
// @Produce json
// @Success 200 {object} ModelCatalogSwaggerResponse
// @Failure 400 {object} ErrorSwaggerResponse
// @Router /models [get]
func (h *ModelCatalogHandler) handleGetModelCatalog() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "", func(c *gin.Context) (any, []resp.ResOpt, error) {
		if h.resolver == nil {
			return nil, nil, fmt.Errorf("model resolver is not configured")
		}
		return h.resolver.Catalog(), nil, nil
	}, nil
}
