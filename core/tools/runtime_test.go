package tools

import (
	"context"
	"testing"
)

// TestRuntimeRoundTripFromContext 验证工具运行时信息可以在上下文中往返传递。
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

// TestRegistryExecutePreservesRuntimeOnHandlerContext 验证注册器执行时不会丢失运行时元数据。
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
	if result != "ok" {
		t.Fatalf("result = %q, want %q", result, "ok")
	}
}
