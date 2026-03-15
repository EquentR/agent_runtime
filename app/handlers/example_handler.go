package handlers

import (
	"net/http"

	"github.com/EquentR/agent_runtime/app/logics"
	resp "github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/gin-gonic/gin"
)

// ExampleHandler 提供示例接口，用于验证应用骨架是否工作正常。
type ExampleHandler struct {
	l *logics.ExampleLogic
}

// NewExampleHandler 创建示例 handler。
func NewExampleHandler() *ExampleHandler {
	return &ExampleHandler{}
}

// Register 注册示例接口路由。
func (h *ExampleHandler) Register(rg *gin.RouterGroup) {
	resp.HandlerWrapper(rg, "hello",
		[]*resp.Handler{
			resp.NewJsonHandler(h.handleSayHello),
		})
	h.l = &logics.ExampleLogic{}
}

// handleSayHello 返回示例问候接口的路由定义。
//
// @Summary 示例问候接口
// @Description 返回一个简单的问候语，用于确认 HTTP 服务已经启动。
// @Tags example
// @Produce json
// @Param name query string false "名称"
// @Success 200 {object} resp.Result{data=string}
// @Router /hello/say [get]
func (h *ExampleHandler) handleSayHello() (method, relativePath string, wrapper resp.JsonResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "say", func(c *gin.Context) (any, error) {
		name := c.Query("name")
		return h.l.SayHello(name), nil
	}, nil
}
