package agent

import (
	"context"
	"fmt"

	coretools "github.com/EquentR/agent_runtime/core/tools"
	coretypes "github.com/EquentR/agent_runtime/core/types"
)

type StepEvent struct {
	Step     int
	Title    string
	Metadata map[string]any
}

type ToolEvent struct {
	Step       int
	ToolCallID string
	ToolName   string
	Arguments  string
	Output     string
	Err        error
	Metadata   map[string]any
}

type LogEvent struct {
	Level    string
	Message  string
	Metadata map[string]any
}

type EventSink interface {
	OnStepStart(ctx context.Context, event StepEvent) error
	OnStepFinish(ctx context.Context, event StepEvent) error
	OnToolStart(ctx context.Context, event ToolEvent) error
	OnToolFinish(ctx context.Context, event ToolEvent) error
	OnLog(ctx context.Context, event LogEvent) error
}

type StreamEventSink interface {
	OnStreamEvent(ctx context.Context, event RunStreamEvent) error
}

func cloneMetadata(input map[string]string) map[string]any {
	if len(input) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

func cloneTools(input []coretypes.Tool) []coretypes.Tool {
	if len(input) == 0 {
		return nil
	}
	cloned := make([]coretypes.Tool, len(input))
	copy(cloned, input)
	return cloned
}

func mergeTools(primary []coretypes.Tool, secondary []coretypes.Tool) []coretypes.Tool {
	if len(primary) == 0 {
		return cloneTools(secondary)
	}
	if len(secondary) == 0 {
		return cloneTools(primary)
	}

	merged := make([]coretypes.Tool, 0, len(primary)+len(secondary))
	seen := make(map[string]struct{}, len(primary)+len(secondary))
	for _, tool := range primary {
		if _, ok := seen[tool.Name]; ok {
			continue
		}
		merged = append(merged, tool)
		seen[tool.Name] = struct{}{}
	}
	for _, tool := range secondary {
		if _, ok := seen[tool.Name]; ok {
			continue
		}
		merged = append(merged, tool)
		seen[tool.Name] = struct{}{}
	}
	return cloneTools(merged)
}

func (r *Runner) emitStepStart(ctx context.Context, step int, title string) {
	if r == nil || r.options.EventSink == nil {
		return
	}
	_ = r.options.EventSink.OnStepStart(ctx, StepEvent{Step: step, Title: title, Metadata: cloneMetadata(r.options.Metadata)})
}

func (r *Runner) emitStepFinish(ctx context.Context, step int, title string, payload any) {
	if r == nil || r.options.EventSink == nil {
		return
	}
	metadata := cloneMetadata(r.options.Metadata)
	if payload != nil {
		if metadata == nil {
			metadata = map[string]any{}
		}
		metadata["payload"] = payload
	}
	_ = r.options.EventSink.OnStepFinish(ctx, StepEvent{Step: step, Title: title, Metadata: metadata})
}

func (r *Runner) emitToolStart(ctx context.Context, step int, call coretypes.ToolCall) {
	if r == nil || r.options.EventSink == nil {
		return
	}
	_ = r.options.EventSink.OnToolStart(ctx, ToolEvent{
		Step:       step,
		ToolCallID: call.ID,
		ToolName:   call.Name,
		Arguments:  call.Arguments,
		Metadata:   cloneMetadata(r.options.Metadata),
	})
}

func (r *Runner) emitToolFinish(ctx context.Context, step int, call coretypes.ToolCall, output string, err error) {
	if r == nil || r.options.EventSink == nil {
		return
	}
	_ = r.options.EventSink.OnToolFinish(ctx, ToolEvent{
		Step:       step,
		ToolCallID: call.ID,
		ToolName:   call.Name,
		Arguments:  call.Arguments,
		Output:     output,
		Err:        err,
		Metadata:   cloneMetadata(r.options.Metadata),
	})
}

func (r *Runner) emitLog(ctx context.Context, level string, message string, fields map[string]any) {
	if r == nil || r.options.EventSink == nil {
		return
	}
	metadata := cloneMetadata(r.options.Metadata)
	if len(fields) > 0 {
		if metadata == nil {
			metadata = map[string]any{}
		}
		for key, value := range fields {
			metadata[key] = value
		}
	}
	_ = r.options.EventSink.OnLog(ctx, LogEvent{Level: level, Message: message, Metadata: metadata})
}

func (r *Runner) emitStreamEvent(ctx context.Context, event RunStreamEvent) {
	if r == nil || r.options.EventSink == nil {
		return
	}
	sink, ok := r.options.EventSink.(StreamEventSink)
	if !ok {
		return
	}
	_ = sink.OnStreamEvent(ctx, event)
}

func (r *Runner) toolContext(ctx context.Context, step int) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return coretools.WithRuntime(ctx, &coretools.Runtime{
		StepID:   fmt.Sprintf("step-%d", step),
		Actor:    r.options.Actor,
		Metadata: cloneStringMap(r.options.Metadata),
	})
}
