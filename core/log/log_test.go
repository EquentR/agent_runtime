package log

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

type spyLogger struct {
	entries []spyEntry
}

type spyEntry struct {
	level  string
	msg    string
	fields []Field
}

func (s *spyLogger) Debug(msg string, fields ...Field) { s.entries = append(s.entries, spyEntry{level: "debug", msg: msg, fields: append([]Field(nil), fields...)}) }
func (s *spyLogger) Info(msg string, fields ...Field)  { s.entries = append(s.entries, spyEntry{level: "info", msg: msg, fields: append([]Field(nil), fields...)}) }
func (s *spyLogger) Warn(msg string, fields ...Field)  { s.entries = append(s.entries, spyEntry{level: "warn", msg: msg, fields: append([]Field(nil), fields...)}) }
func (s *spyLogger) Error(msg string, fields ...Field) { s.entries = append(s.entries, spyEntry{level: "error", msg: msg, fields: append([]Field(nil), fields...)}) }

func TestSetLoggerRoutesFacadeCallsToInjectedLogger(t *testing.T) {
	original := SetLogger(&fallbackLogger{})
	defer SetLogger(original)

	spy := &spyLogger{}
	SetLogger(spy)

	Info("task started", String("task_id", "task-1"), Int("attempt", 2))

	if len(spy.entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(spy.entries))
	}
	entry := spy.entries[0]
	if entry.level != "info" {
		t.Fatalf("level = %q, want info", entry.level)
	}
	if entry.msg != "task started" {
		t.Fatalf("msg = %q, want task started", entry.msg)
	}
	if len(entry.fields) != 2 {
		t.Fatalf("len(fields) = %d, want 2", len(entry.fields))
	}
}

func TestInfofFormatsMessageBeforeDelegating(t *testing.T) {
	original := SetLogger(&fallbackLogger{})
	defer SetLogger(original)

	spy := &spyLogger{}
	SetLogger(spy)

	Infof("task %s attempt %d", "task-1", 3)

	if len(spy.entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(spy.entries))
	}
	if spy.entries[0].msg != "task task-1 attempt 3" {
		t.Fatalf("formatted msg = %q, want %q", spy.entries[0].msg, "task task-1 attempt 3")
	}
}

func TestFallbackLoggerWritesPrettierStdoutOutput(t *testing.T) {
	originalStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = originalStdout
	}()

	original := SetLogger(&fallbackLogger{})
	defer SetLogger(original)

	Warn("tool failed", String("tool_name", "http_request"), Bool("timed_out", true), Duration("elapsed", 1500*time.Millisecond))

	if err := w.Close(); err != nil {
		t.Fatalf("stdout Close() error = %v", err)
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("ReadFrom() error = %v", err)
	}
	output := buf.String()
	for _, want := range []string{"[", "] WARN tool failed", " | ", "tool_name=http_request", "timed_out=true", "elapsed=1.5s"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output = %q, want substring %q", output, want)
		}
	}
}

func TestContextCompilesForFutureLoggerUsage(t *testing.T) {
	_ = context.Background()
}
