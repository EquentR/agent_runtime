package tasks

import (
	"context"
	"fmt"
	"sync"

	coretools "github.com/EquentR/agent_runtime/core/tools"
)

// Runtime 为单个任务执行过程提供事件写入与上下文桥接能力。
type Runtime struct {
	manager *Manager
	taskID  string

	mu        sync.RWMutex
	suspended bool
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

// UpdateMetadata 用新的元数据快照替换当前任务的 MetadataJSON。
func (r *Runtime) UpdateMetadata(ctx context.Context, metadata any) error {
	if r == nil || r.manager == nil || r.manager.store == nil {
		return fmt.Errorf("task runtime is not configured")
	}
	_, err := r.manager.store.UpdateTaskMetadata(ctx, r.taskID, metadata)
	return err
}

// CreateChildTask 基于当前任务创建一个子任务，并继承根任务关联。
func (r *Runtime) CreateChildTask(ctx context.Context, input CreateTaskInput) (*Task, error) {
	if r == nil || r.manager == nil || r.manager.store == nil {
		return nil, fmt.Errorf("task runtime is not configured")
	}
	parent, err := r.manager.store.GetTask(ctx, r.taskID)
	if err != nil {
		return nil, err
	}
	if input.RootTaskID == "" {
		input.RootTaskID = parent.RootTaskID
	}
	if input.ParentTaskID == "" {
		input.ParentTaskID = parent.ID
	}
	return r.manager.CreateTask(ctx, input)
}

// ListChildTasks 返回当前任务下的所有子任务快照。
func (r *Runtime) ListChildTasks(ctx context.Context) ([]Task, error) {
	if r == nil || r.manager == nil || r.manager.store == nil {
		return nil, fmt.Errorf("task runtime is not configured")
	}
	return r.manager.store.ListChildTasks(ctx, r.taskID)
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

// Suspend 将当前运行中的任务切换为 waiting，并广播挂起事件。
func (r *Runtime) Suspend(ctx context.Context, reason string) error {
	task, events, err := r.manager.store.MarkWaiting(ctx, r.taskID, reason)
	if err != nil {
		return err
	}
	if task != nil && task.Status == StatusWaiting {
		r.mu.Lock()
		r.suspended = true
		r.mu.Unlock()
	}
	if len(events) > 0 {
		r.manager.recordTaskWaiting(task)
	}
	r.manager.publish(events...)
	return nil
}

func (r *Runtime) isSuspended() bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.suspended
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
