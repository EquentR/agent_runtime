package tasks

import (
	"encoding/json"
	"time"
)

// Status 表示任务在持久化状态机中的当前阶段。
type Status string

const (
	// StatusQueued 表示任务已入队，等待后台 runner 领取。
	StatusQueued Status = "queued"
	// StatusRunning 表示任务已经被 runner 领取并正在执行。
	StatusRunning Status = "running"
	// StatusWaiting 表示任务已暂停，等待外部条件满足后再继续入队。
	StatusWaiting Status = "waiting"
	// StatusCancelRequested 表示外部已经发起取消，请求正在传递到执行流。
	StatusCancelRequested Status = "cancel_requested"
	// StatusCancelled 表示任务已被协作式取消并完成收尾。
	StatusCancelled Status = "cancelled"
	// StatusSucceeded 表示任务执行成功并写入最终结果。
	StatusSucceeded Status = "succeeded"
	// StatusFailed 表示任务执行失败并写入错误结果。
	StatusFailed Status = "failed"
)

// IsTerminal 判断当前状态是否已经进入终态。
func (s Status) IsTerminal() bool {
	switch s {
	case StatusCancelled, StatusSucceeded, StatusFailed:
		return true
	default:
		return false
	}
}

// ExecutionMode 表示任务内部的执行模式。
type ExecutionMode string

const (
	// ExecutionModeSerial 表示任务内部严格串行执行。
	ExecutionModeSerial ExecutionMode = "serial"
)

const (
	// EventTaskCreated 表示任务记录已经落库创建。
	EventTaskCreated = "task.created"
	// EventTaskStarted 表示任务已开始执行。
	EventTaskStarted = "task.started"
	// EventTaskCancelRequested 表示任务已收到取消请求。
	EventTaskCancelRequested = "task.cancel_requested"
	// EventTaskWaiting 表示任务已暂停并释放当前执行租约。
	EventTaskWaiting = "task.waiting"
	// EventTaskResumed 表示等待中的任务已恢复为 queued。
	EventTaskResumed = "task.resumed"
	// EventTaskFinished 表示任务进入终态。
	EventTaskFinished = "task.finished"
	// EventStepStarted 表示任务内部某个步骤开始执行。
	EventStepStarted = "step.started"
	// EventStepFinished 表示任务内部某个步骤执行结束。
	EventStepFinished = "step.finished"
	// EventToolStarted 预留给后续工具调用开始事件。
	EventToolStarted = "tool.started"
	// EventToolFinished 预留给后续工具调用结束事件。
	EventToolFinished = "tool.finished"
	// EventApprovalRequested 表示工具调用已暂停并等待人工审批。
	EventApprovalRequested = "approval.requested"
	// EventApprovalResolved 表示人工审批已经完成。
	EventApprovalResolved = "approval.resolved"
	// EventInteractionRequested 表示任务请求人工问题交互。
	EventInteractionRequested = "interaction.requested"
	// EventInteractionResponded 表示人工问题交互已经完成响应。
	EventInteractionResponded = "interaction.responded"
	// EventLogMessage 表示任务运行过程中的日志事件。
	EventLogMessage = "log.message"
	// EventChildTaskSpawned 预留给未来父子任务模型。
	EventChildTaskSpawned = "child_task.spawned"
	// EventMemoryCompressed 表示 agent 记忆压缩已成功完成。
	EventMemoryCompressed = "memory.compressed"
)

// CreateTaskInput 描述创建任务时允许写入的初始输入与关联元数据。
type CreateTaskInput struct {
	TaskType        string
	Input           any
	Config          any
	Metadata        any
	CreatedBy       string
	IdempotencyKey  string
	ExecutionMode   ExecutionMode
	RootTaskID      string
	ParentTaskID    string
	ChildIndex      int
	RetryOfTaskID   string
	WaitingOnTaskID string
	SuspendReason   string
	ConcurrencyKey  string
}

// TaskResult 表示任务终态时的聚合结果快照。
type TaskResult struct {
	Result any
	Error  any
	Status Status
	At     time.Time
}

// Event 表示供上层消费的通用事件结构。
type Event struct {
	TaskID    string
	EventType string
	Level     string
	Payload   json.RawMessage
	CreatedAt time.Time
	Seq       int64
}
