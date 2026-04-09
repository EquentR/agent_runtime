package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/EquentR/agent_runtime/core/forcedprompt"
	"github.com/EquentR/agent_runtime/core/memory"
	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/runtimeprompt"
	coretypes "github.com/EquentR/agent_runtime/core/types"
)

// fixedCounter returns a fixed token count per string, used to make compression
// thresholds predictable in tests.
type fixedCounter struct{ count int }

func (f *fixedCounter) Count(_ string) int { return f.count }
func (f *fixedCounter) CountMessages(msgs []string) int {
	return f.count * len(msgs)
}

func TestBuildBudgetedRequestRetriesAfterReserveCompression(t *testing.T) {
	mgr, err := memory.NewManager(memory.Options{
		MaxContextTokens: 120,
		Counter:          fakeTokenCounter{},
		Compressor: func(context.Context, memory.CompressionRequest) (string, error) {
			return "compressed memory", nil
		},
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	mgr.AddMessages([]model.Message{{Role: model.RoleUser, Content: strings.Repeat("a", 70)}, {Role: model.RoleAssistant, Content: strings.Repeat("b", 70)}})

	runner, err := NewRunner(&stubClient{}, nil, Options{
		Model:                "test-model",
		LLMModel:             &coretypes.LLMModel{Context: coretypes.LLMContextConfig{Max: 120}},
		Memory:               mgr,
		RuntimePromptBuilder: runtimeprompt.NewBuilder(nil),
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	_, messages, decision, err := runner.buildBudgetedRequest(context.Background(), nil, false)
	if err != nil {
		t.Fatalf("buildBudgetedRequest() error = %v", err)
	}
	if !decision.CompressionAttempted || decision.FinalPath != "compressed" {
		t.Fatalf("budget decision = %#v, want successful compression path", decision)
	}
	if len(messages) == 0 {
		t.Fatal("messages = empty, want rebuilt request")
	}
}

func TestBuildBudgetedRequestTrimsOldToolMessagesWhenCompressionIsInsufficient(t *testing.T) {
	messages := []model.Message{
		{Role: model.RoleUser, Content: strings.Repeat("u", 30)},
		{Role: model.RoleTool, ToolCallId: "call_1", Content: strings.Repeat("tool", 40)},
		{Role: model.RoleAssistant, Content: strings.Repeat("assistant", 20)},
	}
	trimmed, changed := trimMessagesForBudget(messages, 60, fakeTokenCounter{})
	if !changed {
		t.Fatal("trimMessagesForBudget() changed = false, want true")
	}
	if trimmed[1].Role != model.RoleTool || !strings.Contains(trimmed[1].Content, "trimmed tool output") {
		t.Fatalf("trimmed tool message = %#v, want summarized tool output", trimmed[1])
	}
}

func TestBuildBudgetedRequestReturnsErrContextBudgetExceededWhenStillUnsafe(t *testing.T) {
	runner, err := NewRunner(&stubClient{}, nil, Options{
		Model:    "test-model",
		LLMModel: &coretypes.LLMModel{Context: coretypes.LLMContextConfig{Max: 20}},
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	_, _, _, err = runner.buildBudgetedRequestFromContext(context.Background(), memory.RuntimeContext{Tail: []model.Message{{Role: model.RoleUser, Content: strings.Repeat("x", 200)}}}, false)
	if !errors.Is(err, ErrContextBudgetExceeded) {
		t.Fatalf("buildBudgetedRequestFromContext() error = %v, want ErrContextBudgetExceeded", err)
	}
}

func TestBuildBudgetedRequestFromContextCountsRenderedPromptOverhead(t *testing.T) {
	runner, err := NewRunner(&stubClient{}, nil, Options{
		Model:                "test-model",
		LLMModel:             &coretypes.LLMModel{Context: coretypes.LLMContextConfig{Max: 60}},
		RuntimePromptBuilder: runtimeprompt.NewBuilder(forcedprompt.NewProvider()),
		Now:                  func() time.Time { return time.Date(2026, time.April, 6, 9, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	_, _, _, err = runner.buildBudgetedRequestFromContext(context.Background(), memory.RuntimeContext{Tail: []model.Message{{Role: model.RoleUser, Content: "hi"}}}, false)
	if !errors.Is(err, ErrContextBudgetExceeded) {
		t.Fatalf("buildBudgetedRequestFromContext() error = %v, want ErrContextBudgetExceeded from rendered prompt overhead", err)
	}
}

func TestBuildBudgetedRequestUsesReserveAwareCompressionBeforeTrim(t *testing.T) {
	mgr, err := memory.NewManager(memory.Options{
		MaxContextTokens: 100,
		Counter:          fakeTokenCounter{},
		Compressor: func(context.Context, memory.CompressionRequest) (string, error) {
			return "compressed memory", nil
		},
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	mgr.AddMessages([]model.Message{{Role: model.RoleUser, Content: strings.Repeat("a", 30)}, {Role: model.RoleAssistant, Content: strings.Repeat("b", 30)}})

	runner, err := NewRunner(&stubClient{}, nil, Options{
		Model:                "test-model",
		LLMModel:             &coretypes.LLMModel{Context: coretypes.LLMContextConfig{Max: 100}},
		Memory:               mgr,
		RuntimePromptBuilder: runtimeprompt.NewBuilder(nil),
		SystemPrompt:         strings.Repeat("p", 50),
		Now:                  func() time.Time { return time.Date(2026, time.April, 6, 9, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	_, messages, decision, err := runner.buildBudgetedRequest(context.Background(), nil, false)
	if err != nil {
		t.Fatalf("buildBudgetedRequest() error = %v", err)
	}
	if !decision.CompressionAttempted || !decision.CompressionSucceeded {
		t.Fatalf("budget decision = %#v, want reserve-aware compression attempt and success", decision)
	}
	if decision.TrimApplied {
		t.Fatalf("budget decision = %#v, want compression path before deterministic trim", decision)
	}
	if decision.FinalPath != "compressed" {
		t.Fatalf("budget decision = %#v, want compressed final path", decision)
	}
	if len(messages) == 0 {
		t.Fatal("messages = empty, want rebuilt request")
	}
}

// TestBuildBudgetedRequestEmitsMemoryCompressedOnPathA verifies that
// emitMemoryCompressed is called when first-pass compression succeeds (Path A):
// memory exceeds shortTermLimitTokens, compression fires on RuntimeContextWithReserve(ctx,0),
// and the resulting context fits within the model's Max budget.
func TestBuildBudgetedRequestEmitsMemoryCompressedOnPathA(t *testing.T) {
	// fixedCounter returns 10 per string, so 20 messages × 10 = 200 tokens total.
	// MaxContextTokens=300 → shortTermLimitTokens = 300*70/100 = 210.
	// 20 messages × 10 = 200 ≤ 210, so we need more tokens to exceed it.
	// Use 25 messages × 10 = 250 > 210 → compression triggers on first call.
	// After compression the tail shrinks to fit, and Max=300 gives enough headroom.
	counter := &fixedCounter{count: 10}
	mgr, err := memory.NewManager(memory.Options{
		MaxContextTokens: 300,
		Counter:          counter,
		Compressor: func(_ context.Context, _ memory.CompressionRequest) (string, error) {
			return "summary", nil
		},
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	// Add 25 user messages so shortTerm token estimate = 25*10 = 250 > 210 (shortTermLimit).
	for i := 0; i < 25; i++ {
		mgr.AddMessage(model.Message{Role: model.RoleUser, Content: "hello"})
	}

	runtime := &recordingTaskRuntime{}
	runner, err := NewRunner(&stubClient{}, nil, Options{
		Model:                "test-model",
		LLMModel:             &coretypes.LLMModel{Context: coretypes.LLMContextConfig{Max: 300}},
		Memory:               mgr,
		RuntimePromptBuilder: runtimeprompt.NewBuilder(nil),
		EventSink:            &taskRuntimeSink{runtime: runtime},
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	_, _, decision, err := runner.buildBudgetedRequest(context.Background(), nil, false)
	if err != nil {
		t.Fatalf("buildBudgetedRequest() error = %v", err)
	}
	if !decision.CompressionSucceeded {
		t.Fatalf("budget decision = %#v, want compression succeeded on path A", decision)
	}

	compressedEmits := 0
	for _, emit := range runtime.emits {
		if emit.eventType == "memory.compressed" {
			compressedEmits++
		}
	}
	if compressedEmits != 1 {
		t.Fatalf("memory.compressed emit count = %d, want 1; emits = %#v", compressedEmits, runtime.emits)
	}
}

// TestBuildBudgetedRequestEmitsMemoryCompressedOnPathB verifies that
// emitMemoryCompressed is called when second-pass (reserve-aware) compression
// succeeds (Path B): first pass returns ErrContextBudgetExceeded, then
// RuntimeContextWithReserve(ctx, reserve) compresses and the rebuilt request fits.
func TestBuildBudgetedRequestEmitsMemoryCompressedOnPathB(t *testing.T) {
	mgr, err := memory.NewManager(memory.Options{
		MaxContextTokens: 100,
		Counter:          fakeTokenCounter{},
		Compressor: func(context.Context, memory.CompressionRequest) (string, error) {
			return "compressed memory", nil
		},
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	// Two messages of 30 chars each = 60 total, within shortTermLimit (70) for first pass,
	// but the system prompt (50 chars) pushes the request over Max=100 → ErrContextBudgetExceeded.
	// The reserve-aware second call then triggers compression.
	mgr.AddMessages([]model.Message{
		{Role: model.RoleUser, Content: strings.Repeat("a", 30)},
		{Role: model.RoleAssistant, Content: strings.Repeat("b", 30)},
	})

	runtime := &recordingTaskRuntime{}
	runner, err := NewRunner(&stubClient{}, nil, Options{
		Model:                "test-model",
		LLMModel:             &coretypes.LLMModel{Context: coretypes.LLMContextConfig{Max: 100}},
		Memory:               mgr,
		RuntimePromptBuilder: runtimeprompt.NewBuilder(nil),
		SystemPrompt:         strings.Repeat("p", 50),
		Now:                  func() time.Time { return time.Date(2026, time.April, 6, 9, 0, 0, 0, time.UTC) },
		EventSink:            &taskRuntimeSink{runtime: runtime},
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	_, _, decision, err := runner.buildBudgetedRequest(context.Background(), nil, false)
	if err != nil {
		t.Fatalf("buildBudgetedRequest() error = %v", err)
	}
	if !decision.CompressionAttempted || !decision.CompressionSucceeded {
		t.Fatalf("budget decision = %#v, want compression succeeded on path B", decision)
	}

	compressedEmits := 0
	for _, emit := range runtime.emits {
		if emit.eventType == "memory.compressed" {
			compressedEmits++
		}
	}
	if compressedEmits != 1 {
		t.Fatalf("memory.compressed emit count = %d, want 1; emits = %#v", compressedEmits, runtime.emits)
	}
}
