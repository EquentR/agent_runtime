package router

import (
	"github.com/EquentR/agent_runtime/app/handlers"
	"github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/gin-gonic/gin"
)

// Init 根据当前应用依赖装配全部 HTTP handler。
func Init(e *gin.Engine, baseUrl string, staticPath []rest.Static, deps Dependencies) {
	// 统一在这里汇总路由注册器，保持启动链清晰。
	registers := []Register{
		handlers.NewExampleHandler(),
		handlers.NewTaskHandler(deps.TaskManager),
		handlers.NewConversationHandler(deps.ConversationStore),
		handlers.NewSwaggerHandler(),
	}
	InitRouter(e, registers, baseUrl, staticPath)
}
