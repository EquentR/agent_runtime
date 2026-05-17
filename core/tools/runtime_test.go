package tools

import (
	"context"
	"testing"
)

func TestRuntimeRoundTripFromContext(t *testing.T) {
	ctx := WithRuntime(context.Background(), &Runtime{
		TaskID: "tsk_123",
		StepID: "step_1",
		Actor:  "runner-1",
	})

	runtime, ok := RuntimeFromContext(ctx)
	if !ok {
		t.Fatal("RuntimeFromContext() ok = false, want true")
	}
	if runtime.TaskID != "tsk_123" || runtime.StepID != "step_1" || runtime.Actor != "runner-1" {
		t.Fatalf("runtime = %#v, want task/step/actor metadata", runtime)
	}
}

func TestRuntimeFromContextCarriesToolCallAndEmitter(t *testing.T) {
	var emitted struct {
		eventType string
		level     string
		payload   any
	}
	ctx := WithRuntime(context.Background(), &Runtime{
		TaskID:     "tsk_123",
		StepID:     "step_1",
		Actor:      "runner-1",
		ToolCallID: "call_123",
		ToolName:   "generate_image",
		Metadata:   map[string]string{"conversation_id": "conv_123", "created_by": "user_1"},
		Emit: func(_ context.Context, eventType string, level string, payload any) error {
			emitted.eventType = eventType
			emitted.level = level
			emitted.payload = payload
			return nil
		},
	})

	runtime, ok := RuntimeFromContext(ctx)
	if !ok {
		t.Fatal("RuntimeFromContext() ok = false, want true")
	}
	if runtime.ToolCallID != "call_123" || runtime.ToolName != "generate_image" {
		t.Fatalf("runtime tool call = %q/%q, want call_123/generate_image", runtime.ToolCallID, runtime.ToolName)
	}
	if runtime.Metadata["conversation_id"] != "conv_123" || runtime.Metadata["created_by"] != "user_1" {
		t.Fatalf("runtime.Metadata = %#v, want conversation_id and created_by", runtime.Metadata)
	}
	if runtime.Emit == nil {
		t.Fatal("runtime.Emit = nil, want emitter")
	}
	if err := runtime.Emit(ctx, "log.image_partial", "info", map[string]any{"index": 1}); err != nil {
		t.Fatalf("runtime.Emit() error = %v", err)
	}
	if emitted.eventType != "log.image_partial" || emitted.level != "info" {
		t.Fatalf("emitted = %#v, want log.image_partial/info", emitted)
	}
}

func TestRegistryExecutePreservesRuntimeOnHandlerContext(t *testing.T) {
	registry := NewRegistry()

	if err := registry.Register(Tool{
		Name: "capture_runtime",
		Handler: func(ctx context.Context, arguments map[string]interface{}) (string, error) {
			runtime, ok := RuntimeFromContext(ctx)
			if !ok {
				t.Fatal("RuntimeFromContext() ok = false, want true")
			}
			if runtime.TaskID != "tsk_123" || runtime.StepID != "step_1" {
				t.Fatalf("runtime = %#v, want task and step ids", runtime)
			}
			return "ok", nil
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	ctx := WithRuntime(context.Background(), &Runtime{TaskID: "tsk_123", StepID: "step_1"})
	result, err := registry.Execute(ctx, "capture_runtime", nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Content != "ok" {
		t.Fatalf("result.Content = %q, want %q", result.Content, "ok")
	}
	if result.Ephemeral {
		t.Fatal("result.Ephemeral = true, want false")
	}
}

func TestRegistryExecuteReturnsStructuredEphemeralResult(t *testing.T) {
	registry := NewRegistry()

	if err := registry.Register(Tool{
		Name: "ephemeral_tool",
		ResultHandler: func(_ context.Context, _ map[string]interface{}) (Result, error) {
			return Result{Content: "secret", Ephemeral: true}, nil
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	result, err := registry.Execute(context.Background(), "ephemeral_tool", nil)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Content != "secret" {
		t.Fatalf("result.Content = %q, want %q", result.Content, "secret")
	}
	if !result.Ephemeral {
		t.Fatal("result.Ephemeral = false, want true")
	}
}
