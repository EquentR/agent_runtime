package tasks

import (
	"context"

	coretools "github.com/EquentR/agent_runtime/core/tools"
)

// Runtime 为单个任务执行过程提供事件写入与上下文桥接能力。
type Runtime struct {
	manager *Manager
	taskID  string
}

// newRuntime 为被领取的任务创建运行时封装。
func newRuntime(manager *Manager, taskID string) *Runtime {
	return &Runtime{manager: manager, taskID: taskID}
}

// TaskID 返回当前运行时绑定的任务 id。
func (r *Runtime) TaskID() string {
	if r == nil {
		return ""
	}
	return r.taskID
}

// StartStep 将任务推进到新的步骤，并发布 step.started 事件。
func (r *Runtime) StartStep(ctx context.Context, key string, title string) error {
	_, events, err := r.manager.store.StartStep(ctx, r.taskID, key, title)
	if err != nil {
		return err
	}
	r.manager.publish(events...)
	return nil
}

// FinishStep 为当前步骤写入结束事件。
func (r *Runtime) FinishStep(ctx context.Context, payload any) error {
	_, events, err := r.manager.store.FinishStep(ctx, r.taskID, payload)
	if err != nil {
		return err
	}
	r.manager.publish(events...)
	return nil
}

// Emit 追加一个自定义任务事件并广播给实时订阅者。
func (r *Runtime) Emit(ctx context.Context, eventType string, level string, payload any) error {
	event, err := r.manager.store.AppendEvent(ctx, r.taskID, eventType, level, payload)
	if err != nil {
		return err
	}
	r.manager.publish(event)
	return nil
}

// ToolContext 构造一个附带工具运行元数据的子上下文。
//
// 这样后续真正接入 agent executor 时，工具层可以无缝拿到
// `task_id`、`step_id` 与 `actor` 等任务级信息。
func (r *Runtime) ToolContext(ctx context.Context, stepID string) context.Context {
	return coretools.WithRuntime(ctx, &coretools.Runtime{
		TaskID: r.taskID,
		StepID: stepID,
		Actor:  r.manager.runnerID,
	})
}
