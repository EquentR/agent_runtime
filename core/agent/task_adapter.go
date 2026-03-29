package agent

import (
	"context"
	"fmt"

	"github.com/EquentR/agent_runtime/core/approvals"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
)

type taskRuntime interface {
	StartStep(ctx context.Context, key string, title string) error
	FinishStep(ctx context.Context, payload any) error
	Emit(ctx context.Context, eventType string, level string, payload any) error
	TaskID() string
	GetTask(ctx context.Context) (*coretasks.Task, error)
	UpdateMetadata(ctx context.Context, metadata any) error
	Suspend(ctx context.Context, reason string) error
	CreateApproval(ctx context.Context, input approvals.CreateApprovalInput) (*approvals.ToolApproval, error)
	GetApproval(ctx context.Context, approvalID string) (*approvals.ToolApproval, error)
	ExpireApproval(ctx context.Context, approvalID string, reason string) (*approvals.ToolApproval, error)
	ToolContext(ctx context.Context, stepID string) context.Context
}

type taskRuntimeBridge interface {
	TaskRuntime() taskRuntime
}

type taskRuntimeSink struct {
	runtime taskRuntime
}

func NewTaskRuntimeSink(runtime *coretasks.Runtime) EventSink {
	if runtime == nil {
		return nil
	}
	return &taskRuntimeSink{runtime: runtime}
}

func (s *taskRuntimeSink) TaskRuntime() taskRuntime {
	if s == nil {
		return nil
	}
	return s.runtime
}

func (s *taskRuntimeSink) OnStepStart(ctx context.Context, event StepEvent) error {
	return s.runtime.StartStep(ctx, fmt.Sprintf("agent.step.%d", event.Step), event.Title)
}

func (s *taskRuntimeSink) OnStepFinish(ctx context.Context, event StepEvent) error {
	return s.runtime.FinishStep(ctx, event.Metadata)
}

func (s *taskRuntimeSink) OnToolStart(ctx context.Context, event ToolEvent) error {
	return s.runtime.Emit(ctx, coretasks.EventToolStarted, "info", event)
}

func (s *taskRuntimeSink) OnToolFinish(ctx context.Context, event ToolEvent) error {
	level := "info"
	if event.Err != nil {
		level = "error"
	}
	return s.runtime.Emit(ctx, coretasks.EventToolFinished, level, event)
}

func (s *taskRuntimeSink) OnLog(ctx context.Context, event LogEvent) error {
	return s.runtime.Emit(ctx, coretasks.EventLogMessage, event.Level, event)
}

func (s *taskRuntimeSink) OnStreamEvent(ctx context.Context, event RunStreamEvent) error {
	return s.runtime.Emit(ctx, coretasks.EventLogMessage, "info", event)
}
