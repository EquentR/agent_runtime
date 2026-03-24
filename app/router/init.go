package router

import (
	"github.com/EquentR/agent_runtime/app/handlers"
	"github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/gin-gonic/gin"
)

// Init 根据当前应用依赖装配全部 HTTP handler。
func Init(e *gin.Engine, baseUrl string, staticPath []rest.Static, deps Dependencies) {
	// 统一在这里汇总路由注册器，保持启动链清晰。
	authMiddleware := handlers.NewAuthMiddleware(deps.AuthLogic)
	registers := []Register{
		handlers.NewAuthHandler(deps.AuthLogic),
		handlers.NewExampleHandler(),
		handlers.NewModelCatalogHandler(deps.ModelResolver, authMiddleware.RequireSession()),
		handlers.NewPromptHandler(deps.PromptStore, authMiddleware.RequireSession()),
		handlers.NewTaskHandler(deps.TaskManager, deps.ConversationStore, authMiddleware.RequireSession()),
		handlers.NewConversationHandler(deps.ConversationStore, deps.AuditStore, authMiddleware.RequireSession()),
		handlers.NewAuditHandler(deps.AuditStore, authMiddleware.RequireSession()),
		handlers.NewSwaggerHandler(),
	}
	InitRouter(e, registers, baseUrl, staticPath)
}
