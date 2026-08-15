package agent

import (
	"context"
	"fmt"

	"github.com/EquentR/agent_runtime/core/approvals"
	"github.com/EquentR/agent_runtime/core/interactions"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	coretypes "github.com/EquentR/agent_runtime/core/types"
)

type taskRuntime interface {
	StartStep(ctx context.Context, key string, title string) error
	FinishStep(ctx context.Context, payload any) error
	Emit(ctx context.Context, eventType string, level string, payload any) error
	EmitLive(ctx context.Context, eventType string, level string, payload any) error
	TaskID() string
	GetTask(ctx context.Context) (*coretasks.Task, error)
	UpdateMetadata(ctx context.Context, metadata any) error
	Suspend(ctx context.Context, reason string) error
	CreateApproval(ctx context.Context, input approvals.CreateApprovalInput) (*approvals.ToolApproval, error)
	CreateInteraction(ctx context.Context, input interactions.CreateInteractionInput) (*interactions.Interaction, error)
	GetApproval(ctx context.Context, approvalID string) (*approvals.ToolApproval, error)
	ExpireApproval(ctx context.Context, approvalID string, reason string) (*approvals.ToolApproval, error)
	ToolContext(ctx context.Context, stepID string, metadata map[string]string, call coretypes.ToolCall) context.Context
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
	persistent := map[string]any{
		"tool_call_id":     event.ToolCallID,
		"tool_name":        event.ToolName,
		"arguments_length": len(event.Arguments),
	}
	return s.runtime.Emit(ctx, coretasks.EventToolStarted, "info", coretasks.NewRuntimeEventPayload(persistent, toolEventLivePayload(event, false)))
}

func (s *taskRuntimeSink) OnToolFinish(ctx context.Context, event ToolEvent) error {
	level := "info"
	if event.Err != nil {
		level = "error"
	}
	persistent := map[string]any{
		"tool_call_id":     event.ToolCallID,
		"tool_name":        event.ToolName,
		"arguments_length": len(event.Arguments),
		"output_length":    len(event.Output),
	}
	if event.Err != nil {
		persistent["error"] = event.Err.Error()
	}
	return s.runtime.Emit(ctx, coretasks.EventToolFinished, level, coretasks.NewRuntimeEventPayload(persistent, toolEventLivePayload(event, true)))
}

func toolEventLivePayload(event ToolEvent, includeOutput bool) map[string]any {
	live := map[string]any{
		"Step":       event.Step,
		"ToolCallID": event.ToolCallID,
		"ToolName":   event.ToolName,
		"Arguments":  event.Arguments,
		"Metadata":   event.Metadata,
	}
	if includeOutput {
		live["Output"] = event.Output
	}
	if event.Err != nil {
		live["Err"] = event.Err.Error()
	}
	return live
}

func (s *taskRuntimeSink) OnLog(ctx context.Context, event LogEvent) error {
	return s.runtime.Emit(ctx, coretasks.EventLogMessage, event.Level, event)
}

func (s *taskRuntimeSink) OnStreamEvent(ctx context.Context, event RunStreamEvent) error {
	return s.runtime.EmitLive(ctx, coretasks.EventLogMessage, "info", event)
}
