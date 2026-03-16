package agent

import (
	"context"
	"fmt"

	coretasks "github.com/EquentR/agent_runtime/core/tasks"
)

type taskRuntime interface {
	StartStep(ctx context.Context, key string, title string) error
	FinishStep(ctx context.Context, payload any) error
	Emit(ctx context.Context, eventType string, level string, payload any) error
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
