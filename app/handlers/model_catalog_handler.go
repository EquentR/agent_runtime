package handlers

import (
	"fmt"
	"net/http"

	"github.com/EquentR/agent_runtime/app/logics"
	coreagent "github.com/EquentR/agent_runtime/core/agent"
	resp "github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/gin-gonic/gin"
)

type ModelCatalogHandler struct {
	resolver     *coreagent.ModelResolver
	modelLogic   *logics.ModelLogic
	middlewares  []gin.HandlerFunc
	authRequired bool
}

func NewModelCatalogHandler(resolver *coreagent.ModelResolver, middlewares ...gin.HandlerFunc) *ModelCatalogHandler {
	return &ModelCatalogHandler{resolver: resolver, middlewares: middlewares, authRequired: len(middlewares) > 0}
}

func (h *ModelCatalogHandler) WithModelLogic(modelLogic *logics.ModelLogic) *ModelCatalogHandler {
	h.modelLogic = modelLogic
	return h
}

func (h *ModelCatalogHandler) Register(rg *gin.RouterGroup) {
	if h.resolver == nil && h.modelLogic == nil {
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
// @Description 返回当前用户可用的 LLM provider/model 目录，以及前端可直接使用的默认 provider/model。
// @Tags models
// @Produce json
// @Success 200 {object} ModelCatalogSwaggerResponse
// @Failure 400 {object} ErrorSwaggerResponse
// @Router /models [get]
func (h *ModelCatalogHandler) handleGetModelCatalog() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "", func(c *gin.Context) (any, []resp.ResOpt, error) {
		if h.modelLogic != nil {
			user := currentAuthUser(c)
			if user == nil {
				return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, logics.ErrUnauthorized
			}
			catalog, err := h.modelLogic.CatalogForUser(c.Request.Context(), *user)
			return catalog, nil, err
		}
		if h.resolver == nil {
			return nil, nil, fmt.Errorf("model resolver is not configured")
		}
		return h.resolver.Catalog(), nil, nil
	}, nil
}
