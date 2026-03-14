package resp

import (
	"github.com/gin-gonic/gin"
)

type WrapperOption func(g *gin.RouterGroup)

// WithMiddlewares
// 携带中间件处理
func WithMiddlewares(middles ...gin.HandlerFunc) WrapperOption {
	return func(g *gin.RouterGroup) {
		g.Use(middles...)
	}
}

type Handler struct {
	Method       string
	RelativePath string
	Func         gin.HandlerFunc
	Options      []WrapperOption
}

func NewHandler(method, relativePath string, fun gin.HandlerFunc, opts ...WrapperOption) *Handler {
	return &Handler{
		Method:       method,
		RelativePath: relativePath,
		Func:         fun,
		Options:      opts,
	}
}

func (h *Handler) handle(g *gin.RouterGroup) {
	gg := g.Group("")
	for _, option := range h.Options {
		option(gg)
	}
	if h.Func != nil {
		gg.Handle(h.Method, h.RelativePath, h.Func)
	}
}

type HandleFunc func() (string, string, []gin.HandlerFunc)

// HandlerWrapper
// 处理器封装
func HandlerWrapper(g *gin.RouterGroup, relativePath string, handlers []*Handler, opts ...WrapperOption) {
	gg := g.Group(relativePath)

	for _, opt := range opts {
		opt(gg)
	}

	for _, h := range handlers {
		h.handle(gg)
	}
}
