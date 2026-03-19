package router

import (
	"github.com/EquentR/agent_runtime/app/logics"
	coreagent "github.com/EquentR/agent_runtime/core/agent"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
)

// Dependencies 汇总路由层需要的跨模块依赖。
type Dependencies struct {
	TaskManager       *coretasks.Manager
	ConversationStore *coreagent.ConversationStore
	AuthLogic         *logics.AuthLogic
}
