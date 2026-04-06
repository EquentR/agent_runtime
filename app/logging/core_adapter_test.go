package logging

import (
	"testing"

	corelog "github.com/EquentR/agent_runtime/core/log"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestCoreAdapterWritesZapEntriesAndFields(t *testing.T) {
	core, recorded := observer.New(zapcore.DebugLevel)
	adapter := NewCoreAdapter(zap.New(core))

	adapter.Info("task started", corelog.String("task_id", "task-1"), corelog.Int("attempt", 2), corelog.Bool("waiting", false))

	entries := recorded.All()
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	entry := entries[0]
	if entry.Level != zapcore.InfoLevel {
		t.Fatalf("entry.Level = %v, want %v", entry.Level, zapcore.InfoLevel)
	}
	if entry.Message != "task started" {
		t.Fatalf("entry.Message = %q, want task started", entry.Message)
	}
	ctx := entry.ContextMap()
	if ctx["task_id"] != "task-1" {
		t.Fatalf("task_id = %#v, want task-1", ctx["task_id"])
	}
	if got, ok := ctx["attempt"].(int64); !ok || got != 2 {
		t.Fatalf("attempt = %#v, want int64(2)", ctx["attempt"])
	}
	if got, ok := ctx["waiting"].(bool); !ok || got {
		t.Fatalf("waiting = %#v, want false", ctx["waiting"])
	}
}
