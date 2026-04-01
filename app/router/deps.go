package router

import (
	"github.com/EquentR/agent_runtime/app/logics"
	coreagent "github.com/EquentR/agent_runtime/core/agent"
	"github.com/EquentR/agent_runtime/core/approvals"
	coreaudit "github.com/EquentR/agent_runtime/core/audit"
	"github.com/EquentR/agent_runtime/core/interactions"
	coreprompt "github.com/EquentR/agent_runtime/core/prompt"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
)

// Dependencies 汇总路由层需要的跨模块依赖。
type Dependencies struct {
	TaskManager       *coretasks.Manager
	ApprovalStore     *approvals.Store
	InteractionStore  *interactions.Store
	ConversationStore *coreagent.ConversationStore
	AuditStore        *coreaudit.Store
	ModelResolver     *coreagent.ModelResolver
	PromptStore       *coreprompt.Store
	PromptResolver    *coreprompt.Resolver
	AuthLogic         *logics.AuthLogic
}
