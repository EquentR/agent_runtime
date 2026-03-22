package agent

import (
	"context"
	"fmt"
	"strings"

	coreaudit "github.com/EquentR/agent_runtime/core/audit"
	coretools "github.com/EquentR/agent_runtime/core/tools"
	coretypes "github.com/EquentR/agent_runtime/core/types"
)

type runnerToolArgumentsArtifact struct {
	ToolCallID string `json:"tool_call_id,omitempty"`
	ToolName   string `json:"tool_name"`
	Arguments  string `json:"arguments,omitempty"`
}

type runnerToolOutputArtifact struct {
	ToolCallID string `json:"tool_call_id,omitempty"`
	ToolName   string `json:"tool_name"`
	Output     string `json:"output,omitempty"`
	Error      string `json:"error,omitempty"`
}

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
	} else {
		_ = r.options.EventSink.OnStepStart(ctx, StepEvent{Step: step, Title: title, Metadata: cloneMetadata(r.options.Metadata)})
	}
	r.appendAuditEvent(ctx, step, coreaudit.PhaseStep, "step.started", map[string]any{"title": title}, "")
}

func (r *Runner) emitStepFinish(ctx context.Context, step int, title string, payload any) {
	if r != nil && r.options.EventSink != nil {
		metadata := cloneMetadata(r.options.Metadata)
		if payload != nil {
			if metadata == nil {
				metadata = map[string]any{}
			}
			metadata["payload"] = payload
		}
		_ = r.options.EventSink.OnStepFinish(ctx, StepEvent{Step: step, Title: title, Metadata: metadata})
	}
	r.appendAuditEvent(ctx, step, coreaudit.PhaseStep, "step.finished", mergeAuditPayload(map[string]any{"title": title}, payload), "")
}

func (r *Runner) emitToolStart(ctx context.Context, step int, call coretypes.ToolCall) {
	if r != nil && r.options.EventSink != nil {
		_ = r.options.EventSink.OnToolStart(ctx, ToolEvent{
			Step:       step,
			ToolCallID: call.ID,
			ToolName:   call.Name,
			Arguments:  call.Arguments,
			Metadata:   cloneMetadata(r.options.Metadata),
		})
	}
	artifactID := r.attachAuditArtifact(ctx, coreaudit.ArtifactKindToolArguments, runnerToolArgumentsArtifact{
		ToolCallID: call.ID,
		ToolName:   call.Name,
		Arguments:  call.Arguments,
	})
	r.appendAuditEvent(ctx, step, coreaudit.PhaseTool, "tool.started", map[string]any{
		"tool_call_id": call.ID,
		"tool_name":    call.Name,
	}, artifactID)
}

func (r *Runner) emitToolFinish(ctx context.Context, step int, call coretypes.ToolCall, output string, err error) {
	if r != nil && r.options.EventSink != nil {
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
	artifact := runnerToolOutputArtifact{ToolCallID: call.ID, ToolName: call.Name, Output: output}
	payload := map[string]any{
		"tool_call_id": call.ID,
		"tool_name":    call.Name,
	}
	if err != nil {
		artifact.Error = err.Error()
		payload["error"] = err.Error()
	} else {
		payload["output_length"] = len(output)
	}
	artifactID := r.attachAuditArtifact(ctx, coreaudit.ArtifactKindToolOutput, artifact)
	r.appendAuditEvent(ctx, step, coreaudit.PhaseTool, "tool.finished", payload, artifactID)
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
		TaskID:   strings.TrimSpace(r.options.TaskID),
		StepID:   fmt.Sprintf("step-%d", step),
		Actor:    r.options.Actor,
		Metadata: cloneStringMap(r.options.Metadata),
	})
}

func (r *Runner) appendAuditEvent(ctx context.Context, step int, phase coreaudit.Phase, eventType string, payload any, refArtifactID string) {
	runID := r.auditRunID()
	if r == nil || r.options.AuditRecorder == nil || runID == "" {
		return
	}
	_, _ = r.options.AuditRecorder.AppendEvent(ctx, runID, coreaudit.AppendEventInput{
		Phase:         phase,
		EventType:     eventType,
		StepIndex:     step,
		RefArtifactID: refArtifactID,
		Payload:       payload,
	})
}

func (r *Runner) attachAuditArtifact(ctx context.Context, kind coreaudit.ArtifactKind, body any) string {
	runID := r.auditRunID()
	if r == nil || r.options.AuditRecorder == nil || runID == "" {
		return ""
	}
	artifact, err := r.options.AuditRecorder.AttachArtifact(ctx, runID, coreaudit.CreateArtifactInput{
		Kind:     kind,
		MimeType: "application/json",
		Encoding: "utf-8",
		Body:     body,
	})
	if err != nil || artifact == nil {
		return ""
	}
	return artifact.ID
}

func (r *Runner) auditRunID() string {
	if r == nil {
		return ""
	}
	return strings.TrimSpace(r.options.AuditRunID)
}

func mergeAuditPayload(base map[string]any, extra any) map[string]any {
	payload := make(map[string]any, len(base)+1)
	for key, value := range base {
		payload[key] = value
	}
	switch typed := extra.(type) {
	case nil:
	case map[string]any:
		for key, value := range typed {
			payload[key] = value
		}
	default:
		payload["value"] = typed
	}
	return payload
}
