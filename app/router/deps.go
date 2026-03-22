package router

import (
	"github.com/EquentR/agent_runtime/app/logics"
	coreagent "github.com/EquentR/agent_runtime/core/agent"
	coreaudit "github.com/EquentR/agent_runtime/core/audit"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
)

// Dependencies 汇总路由层需要的跨模块依赖。
type Dependencies struct {
	TaskManager       *coretasks.Manager
	ConversationStore *coreagent.ConversationStore
	AuditStore        *coreaudit.Store
	ModelResolver     *coreagent.ModelResolver
	AuthLogic         *logics.AuthLogic
}
