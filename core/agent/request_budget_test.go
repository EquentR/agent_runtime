package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/EquentR/agent_runtime/core/forcedprompt"
	"github.com/EquentR/agent_runtime/core/memory"
	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/runtimeprompt"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
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
		MaxContextTokens: 120,
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

func TestBuildBudgetedRequestEmitsAuthoritativeMemoryContextStateAndCompressionTotals(t *testing.T) {
	original := []model.Message{
		{Role: model.RoleUser, Content: strings.Repeat("a", 60)},
		{Role: model.RoleAssistant, Content: strings.Repeat("b", 60)},
	}
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
	mgr.AddMessages(original)

	runtime := &recordingTaskRuntime{}
	runner, err := NewRunner(&stubClient{}, nil, Options{
		Model:                "test-model",
		LLMModel:             &coretypes.LLMModel{Context: coretypes.LLMContextConfig{Max: 100}},
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
		t.Fatalf("budget decision = %#v, want compression success", decision)
	}

	runtimeContext, err := mgr.RuntimeContext(context.Background())
	if err != nil {
		t.Fatalf("RuntimeContext() error = %v", err)
	}
	contextState := mgr.ContextState()
	wantAfterShort := memory.CountMessageTokens(fakeTokenCounter{}, runtimeContext.Tail)
	wantAfterRenderedSummary := contextState.RenderedSummaryTokens
	wantAfterTotal := contextState.TotalTokens
	wantAfterSummary := int64(fakeTokenCounter{}.Count(mgr.Summary()))
	wantBeforeShort := memory.CountMessageTokens(fakeTokenCounter{}, original)

	compressedPayload := requireRecordedEmitPayloadMap(t, runtime.emits, coretasks.EventMemoryCompressed)
	if got := requireRecordedEmitInt64(t, compressedPayload, "tokens_before"); got != wantBeforeShort {
		t.Fatalf("memory.compressed tokens_before = %d, want %d", got, wantBeforeShort)
	}
	if got := requireRecordedEmitInt64(t, compressedPayload, "tokens_after"); got != wantAfterShort {
		t.Fatalf("memory.compressed tokens_after = %d, want %d", got, wantAfterShort)
	}
	if got := requireRecordedEmitInt64(t, compressedPayload, "short_term_tokens_before"); got != wantBeforeShort {
		t.Fatalf("memory.compressed short_term_tokens_before = %d, want %d", got, wantBeforeShort)
	}
	if got := requireRecordedEmitInt64(t, compressedPayload, "short_term_tokens_after"); got != wantAfterShort {
		t.Fatalf("memory.compressed short_term_tokens_after = %d, want %d", got, wantAfterShort)
	}
	if got := requireRecordedEmitInt64(t, compressedPayload, "summary_tokens_before"); got != 0 {
		t.Fatalf("memory.compressed summary_tokens_before = %d, want 0", got)
	}
	if got := requireRecordedEmitInt64(t, compressedPayload, "summary_tokens_after"); got != wantAfterSummary {
		t.Fatalf("memory.compressed summary_tokens_after = %d, want %d", got, wantAfterSummary)
	}
	if got := requireRecordedEmitInt64(t, compressedPayload, "rendered_summary_tokens_before"); got != 0 {
		t.Fatalf("memory.compressed rendered_summary_tokens_before = %d, want 0", got)
	}
	if got := requireRecordedEmitInt64(t, compressedPayload, "rendered_summary_tokens_after"); got != wantAfterRenderedSummary {
		t.Fatalf("memory.compressed rendered_summary_tokens_after = %d, want %d", got, wantAfterRenderedSummary)
	}
	if got := requireRecordedEmitInt64(t, compressedPayload, "total_tokens_before"); got != wantBeforeShort {
		t.Fatalf("memory.compressed total_tokens_before = %d, want %d", got, wantBeforeShort)
	}
	if got := requireRecordedEmitInt64(t, compressedPayload, "total_tokens_after"); got != wantAfterTotal {
		t.Fatalf("memory.compressed total_tokens_after = %d, want %d", got, wantAfterTotal)
	}

	statePayload := requireRecordedEmitPayloadMap(t, runtime.emits, coretasks.EventMemoryContextState)
	if got := requireRecordedEmitInt64(t, statePayload, "short_term_tokens"); got != wantAfterShort {
		t.Fatalf("memory.context_state short_term_tokens = %d, want %d", got, wantAfterShort)
	}
	if got := requireRecordedEmitInt64(t, statePayload, "summary_tokens"); got != wantAfterSummary {
		t.Fatalf("memory.context_state summary_tokens = %d, want %d", got, wantAfterSummary)
	}
	if got := requireRecordedEmitInt64(t, statePayload, "rendered_summary_tokens"); got != wantAfterRenderedSummary {
		t.Fatalf("memory.context_state rendered_summary_tokens = %d, want %d", got, wantAfterRenderedSummary)
	}
	if got := requireRecordedEmitInt64(t, statePayload, "total_tokens"); got != wantAfterTotal {
		t.Fatalf("memory.context_state total_tokens = %d, want %d", got, wantAfterTotal)
	}
}

func TestBuildBudgetedRequestUsesValidatedRenderedSummaryBudgetSemantics(t *testing.T) {
	mgr, err := memory.NewManager(memory.Options{
		MaxContextTokens: 33,
		Counter:          fakeTokenCounter{},
		SummaryTemplate:  "%s",
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	mgr.LoadSummary(strings.Repeat("s", 10))

	runtime := &recordingTaskRuntime{}
	runner, err := NewRunner(&stubClient{}, nil, Options{
		Model:                "test-model",
		LLMModel:             &coretypes.LLMModel{Context: coretypes.LLMContextConfig{Max: 33}},
		Memory:               mgr,
		RuntimePromptBuilder: runtimeprompt.NewBuilder(nil),
		EventSink:            &taskRuntimeSink{runtime: runtime},
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	_, _, _, err = runner.buildBudgetedRequest(context.Background(), nil, false)
	if err != nil {
		t.Fatalf("buildBudgetedRequest() error = %v", err)
	}

	statePayload := requireRecordedEmitPayloadMap(t, runtime.emits, coretasks.EventMemoryContextState)
	if got := requireRecordedEmitInt64(t, statePayload, "summary_limit"); got != 10 {
		t.Fatalf("memory.context_state summary_limit = %d, want 10", got)
	}
	if got := requireRecordedEmitInt64(t, statePayload, "summary_tokens"); got != 10 {
		t.Fatalf("memory.context_state summary_tokens = %d, want 10", got)
	}
	if got := requireRecordedEmitInt64(t, statePayload, "rendered_summary_tokens"); got != 10 {
		t.Fatalf("memory.context_state rendered_summary_tokens = %d, want 10 under validated summary budget semantics", got)
	}
	if got := requireRecordedEmitInt64(t, statePayload, "total_tokens"); got != 10 {
		t.Fatalf("memory.context_state total_tokens = %d, want 10 under validated summary budget semantics", got)
	}
}

func TestBuildBudgetedRequestEmitsMemoryEventsWhenFirstPassCompressionPrecedesBlockedReservePath(t *testing.T) {
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
	mgr.AddMessages([]model.Message{
		{Role: model.RoleUser, Content: strings.Repeat("a", 60)},
		{Role: model.RoleAssistant, Content: strings.Repeat("b", 60)},
	})

	runtime := &recordingTaskRuntime{}
	runner, err := NewRunner(&stubClient{}, nil, Options{
		Model:                "test-model",
		LLMModel:             &coretypes.LLMModel{Context: coretypes.LLMContextConfig{Max: 120}},
		Memory:               mgr,
		RuntimePromptBuilder: runtimeprompt.NewBuilder(nil),
		SystemPrompt:         strings.Repeat("p", 100),
		Now:                  func() time.Time { return time.Date(2026, time.April, 6, 9, 0, 0, 0, time.UTC) },
		EventSink:            &taskRuntimeSink{runtime: runtime},
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	_, _, _, err = runner.buildBudgetedRequest(context.Background(), nil, false)
	if err == nil {
		t.Fatal("buildBudgetedRequest() error = nil, want blocked request after first-pass compression")
	}

	compressedEmits := 0
	contextStateEmits := 0
	for _, emit := range runtime.emits {
		switch emit.eventType {
		case coretasks.EventMemoryCompressed:
			compressedEmits++
		case coretasks.EventMemoryContextState:
			contextStateEmits++
		}
	}
	if compressedEmits != 1 {
		t.Fatalf("memory.compressed emit count = %d, want 1 after first-pass compression; emits = %#v", compressedEmits, runtime.emits)
	}
	if contextStateEmits != 1 {
		t.Fatalf("memory.context_state emit count = %d, want 1 after first-pass compression; emits = %#v", contextStateEmits, runtime.emits)
	}
}

func TestBuildBudgetedRequestEmitsMemoryEventsWhenReserveAwareCompressionStillEndsBlocked(t *testing.T) {
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
	// First pass stays below short-term limit, so compression only happens on the
	// reserve-aware second call after request overhead is accounted for.
	mgr.AddMessages([]model.Message{
		{Role: model.RoleUser, Content: strings.Repeat("a", 25)},
		{Role: model.RoleAssistant, Content: strings.Repeat("b", 25)},
	})

	runtime := &recordingTaskRuntime{}
	runner, err := NewRunner(&stubClient{}, nil, Options{
		Model:                "test-model",
		LLMModel:             &coretypes.LLMModel{Context: coretypes.LLMContextConfig{Max: 100}},
		Memory:               mgr,
		RuntimePromptBuilder: runtimeprompt.NewBuilder(nil),
		SystemPrompt:         strings.Repeat("p", 90),
		Now:                  func() time.Time { return time.Date(2026, time.April, 6, 9, 0, 0, 0, time.UTC) },
		EventSink:            &taskRuntimeSink{runtime: runtime},
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	_, _, decision, err := runner.buildBudgetedRequest(context.Background(), nil, false)
	if !errors.Is(err, ErrContextBudgetExceeded) {
		t.Fatalf("buildBudgetedRequest() error = %v, want ErrContextBudgetExceeded", err)
	}
	if !decision.CompressionAttempted || !decision.CompressionSucceeded {
		t.Fatalf("budget decision = %#v, want reserve-aware compression before blocked result", decision)
	}

	compressedEmits := 0
	contextStateEmits := 0
	for _, emit := range runtime.emits {
		switch emit.eventType {
		case coretasks.EventMemoryCompressed:
			compressedEmits++
		case coretasks.EventMemoryContextState:
			contextStateEmits++
		}
	}
	if compressedEmits != 1 {
		t.Fatalf("memory.compressed emit count = %d, want 1 after reserve-aware blocked compression; emits = %#v", compressedEmits, runtime.emits)
	}
	if contextStateEmits != 1 {
		t.Fatalf("memory.context_state emit count = %d, want 1 after reserve-aware blocked compression; emits = %#v", contextStateEmits, runtime.emits)
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

func requireRecordedEmitPayloadMap(t *testing.T, emits []recordedEmit, eventType string) map[string]any {
	t.Helper()

	for _, emit := range emits {
		if emit.eventType != eventType {
			continue
		}
		payload, ok := emit.payload.(map[string]any)
		if !ok {
			t.Fatalf("emit %q payload type = %T, want map[string]any", eventType, emit.payload)
		}
		return payload
	}

	t.Fatalf("recorded emits = %#v, want %q", emits, eventType)
	return nil
}

func requireRecordedEmitInt64(t *testing.T, payload map[string]any, key string) int64 {
	t.Helper()

	value, ok := payload[key]
	if !ok {
		t.Fatalf("payload = %#v, want key %q", payload, key)
	}
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case int32:
		return int64(typed)
	case float64:
		return int64(typed)
	default:
		t.Fatalf("payload[%q] type = %T, want numeric value (value=%v)", key, value, fmt.Sprintf("%v", value))
		return 0
	}
}
