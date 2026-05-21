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
	authHandlerOptions := []handlers.AuthHandlerOption{}
	if deps.AuthSettings != nil {
		authHandlerOptions = append(authHandlerOptions, handlers.WithAuthHandlerSettings(deps.AuthSettings))
	}
	if deps.EmailVerification != nil {
		authHandlerOptions = append(authHandlerOptions, handlers.WithAuthHandlerEmailVerification(deps.EmailVerification))
	}
	if deps.TurnstileVerifier != nil {
		authHandlerOptions = append(authHandlerOptions, handlers.WithAuthHandlerTurnstileVerifier(deps.TurnstileVerifier))
	}
	activeUser := authMiddleware.RequireActiveUser()
	adminUser := authMiddleware.RequireAdmin()
	registers := []Register{
		handlers.NewAuthHandler(deps.AuthLogic, authHandlerOptions...),
		handlers.NewExampleHandler(),
		handlers.NewSettingsHandler(deps.AuthSettings),
		handlers.NewUserHandler(deps.UserDB, deps.EmailVerification, authMiddleware.RequireSession()).WithTurnstile(deps.AuthSettings, deps.TurnstileVerifier),
		handlers.NewAdminUserHandler(deps.UserDB, deps.AdminAuditLogic, deps.EmailVerification, adminUser),
		handlers.NewAdminSettingsHandler(deps.AuthSettings, deps.AdminAuditLogic, deps.AdminSMTPTester, adminUser),
		handlers.NewAdminAuditEventHandler(deps.AdminAuditLogic, adminUser),
		handlers.NewAdminModelHandler(deps.ModelLogic, deps.AdminAuditLogic, deps.ModelTester, adminUser),
		handlers.NewUserModelHandler(deps.ModelLogic, deps.ModelTester, activeUser),
		handlers.NewModelCatalogHandler(deps.ModelResolver, activeUser).WithModelLogic(deps.ModelLogic),
		handlers.NewAttachmentHandler(deps.AttachmentStore, deps.AttachmentStorage, deps.AttachmentDraftTTL, activeUser),
		handlers.NewSkillHandler(deps.SkillLoader, activeUser),
		handlers.NewPromptHandler(deps.PromptStore, activeUser),
		handlers.NewTaskHandler(deps.TaskManager, deps.ConversationStore, activeUser).WithModelLogic(deps.ModelLogic),
		handlers.NewInteractionHandler(deps.TaskManager, deps.InteractionStore, deps.ConversationStore, activeUser),
		handlers.NewApprovalHandler(deps.TaskManager, deps.ApprovalStore, deps.ConversationStore, activeUser).WithInteractionStore(deps.InteractionStore),
		handlers.NewConversationHandler(deps.ConversationStore, deps.AuditStore, activeUser),
		handlers.NewAuditHandler(deps.AuditStore, activeUser).WithConversationStore(deps.ConversationStore),
		handlers.NewSwaggerHandler(),
	}
	InitRouter(e, registers, baseUrl, staticPath)
}
