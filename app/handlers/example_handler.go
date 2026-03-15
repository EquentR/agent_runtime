package handlers

import (
	"net/http"

	"github.com/EquentR/agent_runtime/app/logics"
	resp "github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/gin-gonic/gin"
)

type ExampleHandler struct {
	l *logics.ExampleLogic
}

func NewExampleHandler() *ExampleHandler {
	return &ExampleHandler{}
}

func (h *ExampleHandler) Register(rg *gin.RouterGroup) {
	resp.HandlerWrapper(rg, "hello",
		[]*resp.Handler{
			resp.NewJsonHandler(h.handleSayHello),
		})
	h.l = &logics.ExampleLogic{}
}

func (h *ExampleHandler) handleSayHello() (method, relativePath string, wrapper resp.JsonResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "say", func(c *gin.Context) (any, error) {
		name := c.Query("name")
		return h.l.SayHello(name), nil
	}, nil
}
