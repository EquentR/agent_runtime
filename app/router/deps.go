package router

import coretasks "github.com/EquentR/agent_runtime/core/tasks"

// Dependencies 汇总路由层需要的跨模块依赖。
type Dependencies struct {
	TaskManager *coretasks.Manager
}
