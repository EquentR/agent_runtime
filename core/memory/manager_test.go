package memory

import (
	"context"
	"strings"
	"testing"

	corelog "github.com/EquentR/agent_runtime/core/log"
	model "github.com/EquentR/agent_runtime/core/providers/types"
	coretypes "github.com/EquentR/agent_runtime/core/types"
)

func TestNewManagerDefaultsToHundredKContextWindow(t *testing.T) {
	mgr, err := NewManager(Options{Counter: fakeTokenCounter{}})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	if got := mgr.MaxContextTokens(); got != 100_000 {
		t.Fatalf("MaxContextTokens() = %d, want 100000", got)
	}
	if got := mgr.ShortTermLimitTokens(); got != 70_000 {
		t.Fatalf("ShortTermLimitTokens() = %d, want 70000", got)
	}
	if got := mgr.SummaryLimitTokens(); got != 30_000 {
		t.Fatalf("SummaryLimitTokens() = %d, want 30000", got)
	}
}

func TestNewManagerUsesProviderContextWindow(t *testing.T) {
	mgr, err := NewManager(Options{
		Model: &coretypes.LLMModel{
			BaseModel: coretypes.BaseModel{ID: "gpt54", Name: "gpt-5.4"},
			Type:      "openai_responses",
			Context:   coretypes.LLMContextConfig{Max: 32_000},
		},
		Counter: fakeTokenCounter{},
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	if got := mgr.MaxContextTokens(); got != 32_000 {
		t.Fatalf("MaxContextTokens() = %d, want 32000", got)
	}
	if got := mgr.ShortTermLimitTokens(); got != 22_400 {
		t.Fatalf("ShortTermLimitTokens() = %d, want 22400", got)
	}
	if got := mgr.SummaryLimitTokens(); got != 9_600 {
		t.Fatalf("SummaryLimitTokens() = %d, want 9600", got)
	}
}

func TestNewManagerUsesProviderInputBudgetWhenOutputReserved(t *testing.T) {
	mgr, err := NewManager(Options{
		Model: &coretypes.LLMModel{
			BaseModel: coretypes.BaseModel{ID: "gpt54", Name: "gpt-5.4"},
			Type:      "openai_responses",
			Context:   coretypes.LLMContextConfig{Max: 128_000, Output: 8_000},
		},
		Counter: fakeTokenCounter{},
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	if got := mgr.MaxContextTokens(); got != 120_000 {
		t.Fatalf("MaxContextTokens() = %d, want 120000 effective input budget", got)
	}
	if got := mgr.ShortTermLimitTokens(); got != 84_000 {
		t.Fatalf("ShortTermLimitTokens() = %d, want 84000", got)
	}
	if got := mgr.SummaryLimitTokens(); got != 36_000 {
		t.Fatalf("SummaryLimitTokens() = %d, want 36000", got)
	}
}

func TestContextMessagesSkipsCompressionBelowThreshold(t *testing.T) {
	compressCalls := 0
	mgr, err := NewManager(Options{
		MaxContextTokens: 100,
		Counter:          fakeTokenCounter{},
		Compressor: func(_ context.Context, request CompressionRequest) (string, error) {
			compressCalls++
			return "unused", nil
		},
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	mgr.AddMessage(model.Message{Role: model.RoleUser, Content: strings.Repeat("a", 20)})

	got, err := mgr.ContextMessages(context.Background())
	if err != nil {
		t.Fatalf("ContextMessages() error = %v", err)
	}
	if compressCalls != 0 {
		t.Fatalf("compressor called %d times, want 0", compressCalls)
	}
	if len(got) != 1 || got[0].Content != strings.Repeat("a", 20) {
		t.Fatalf("ContextMessages() = %#v, want untouched short-term message", got)
	}
}

func TestContextMessagesCompressesShortTermWhenThresholdExceeded(t *testing.T) {
	var seen CompressionRequest
	mgr, err := NewManager(Options{
		MaxContextTokens: 100,
		Counter:          fakeTokenCounter{},
		Compressor: func(_ context.Context, request CompressionRequest) (string, error) {
			seen = request
			return "compressed memory", nil
		},
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	mgr.AddMessage(model.Message{Role: model.RoleUser, Content: strings.Repeat("a", 60)})
	mgr.AddMessage(model.Message{Role: model.RoleAssistant, Content: strings.Repeat("b", 60)})

	got, err := mgr.ContextMessages(context.Background())
	if err != nil {
		t.Fatalf("ContextMessages() error = %v", err)
	}
	if seen.Instruction == "" {
		t.Fatal("CompressionRequest.Instruction = empty, want default instruction")
	}
	if seen.TargetSummaryTokens != 24 {
		t.Fatalf("CompressionRequest.TargetSummaryTokens = %d, want 24", seen.TargetSummaryTokens)
	}
	if seen.MaxSummaryTokens != 30 {
		t.Fatalf("CompressionRequest.MaxSummaryTokens = %d, want 30", seen.MaxSummaryTokens)
	}
	if len(seen.Messages) != 2 {
		t.Fatalf("len(CompressionRequest.Messages) = %d, want 2", len(seen.Messages))
	}
	if gotSummary := mgr.Summary(); gotSummary != "compressed memory" {
		t.Fatalf("Summary() = %q, want %q", gotSummary, "compressed memory")
	}
	if short := mgr.ShortTermMessages(); len(short) != 0 {
		t.Fatalf("ShortTermMessages() len = %d, want 0 after compression", len(short))
	}
	if len(got) != 1 {
		t.Fatalf("len(ContextMessages()) = %d, want 1 summary message", len(got))
	}
	if got[0].Role != model.RoleSystem {
		t.Fatalf("summary role = %q, want system", got[0].Role)
	}
	if !strings.Contains(got[0].Content, "compressed memory") {
		t.Fatalf("summary content = %q, want to include compressed memory", got[0].Content)
	}
}

func TestContextMessagesCompressionUsesRollingPreviousSummary(t *testing.T) {
	var seen []CompressionRequest
	mgr, err := NewManager(Options{
		MaxContextTokens: 100,
		Counter:          fakeTokenCounter{},
		Compressor: func(_ context.Context, request CompressionRequest) (string, error) {
			seen = append(seen, request)
			if request.PreviousSummary == "" {
				return "first summary", nil
			}
			return "second summary", nil
		},
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	mgr.AddMessages([]model.Message{
		{Role: model.RoleUser, Content: strings.Repeat("a", 60)},
		{Role: model.RoleAssistant, Content: strings.Repeat("b", 60)},
	})
	if _, err := mgr.ContextMessages(context.Background()); err != nil {
		t.Fatalf("first ContextMessages() error = %v", err)
	}

	mgr.AddMessages([]model.Message{
		{Role: model.RoleUser, Content: strings.Repeat("c", 60)},
		{Role: model.RoleAssistant, Content: strings.Repeat("d", 60)},
	})
	if _, err := mgr.ContextMessages(context.Background()); err != nil {
		t.Fatalf("second ContextMessages() error = %v", err)
	}

	if len(seen) != 2 {
		t.Fatalf("compressor call count = %d, want 2", len(seen))
	}
	if seen[0].PreviousSummary != "" {
		t.Fatalf("first PreviousSummary = %q, want empty", seen[0].PreviousSummary)
	}
	if seen[1].PreviousSummary != "first summary" {
		t.Fatalf("second PreviousSummary = %q, want first summary", seen[1].PreviousSummary)
	}
	if mgr.Summary() != "second summary" {
		t.Fatalf("Summary() = %q, want second summary", mgr.Summary())
	}
}

func TestRuntimeContextReturnsSummaryAndReplayableBodySeparately(t *testing.T) {
	mgr, err := NewManager(Options{
		MaxContextTokens: 100,
		Counter:          fakeTokenCounter{},
		Compressor: func(_ context.Context, request CompressionRequest) (string, error) {
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

	got, err := mgr.RuntimeContext(context.Background())
	if err != nil {
		t.Fatalf("RuntimeContext() error = %v", err)
	}
	if got.Summary == nil {
		t.Fatal("Summary = nil, want rendered compressed summary")
	}
	if got.Summary.Role != model.RoleSystem || !strings.Contains(got.Summary.Content, "compressed memory") {
		t.Fatalf("Summary = %#v, want rendered compressed summary", got.Summary)
	}
	if len(got.Body) != 0 {
		t.Fatalf("len(Body) = %d, want 0 after compression", len(got.Body))
	}
}

func TestContextMessagesWrapsRuntimeContextIntoLegacySliceShape(t *testing.T) {
	mgr, err := NewManager(Options{
		MaxContextTokens: 100,
		Counter:          fakeTokenCounter{},
		Compressor: func(_ context.Context, request CompressionRequest) (string, error) {
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
	if _, err := mgr.RuntimeContext(context.Background()); err != nil {
		t.Fatalf("RuntimeContext() error = %v", err)
	}

	mgr.AddMessage(model.Message{Role: model.RoleUser, Content: "follow up"})

	runtimeContext, err := mgr.RuntimeContext(context.Background())
	if err != nil {
		t.Fatalf("RuntimeContext() error = %v", err)
	}
	got, err := mgr.ContextMessages(context.Background())
	if err != nil {
		t.Fatalf("ContextMessages() error = %v", err)
	}

	if runtimeContext.Summary == nil {
		t.Fatal("Summary = nil, want rendered summary")
	}
	if len(runtimeContext.Body) != 1 {
		t.Fatalf("len(Body) = %d, want 1", len(runtimeContext.Body))
	}
	if len(got) != 2 {
		t.Fatalf("len(ContextMessages()) = %d, want 2", len(got))
	}
	if got[0].Content != runtimeContext.Summary.Content {
		t.Fatalf("ContextMessages()[0].Content = %q, want %q", got[0].Content, runtimeContext.Summary.Content)
	}
	if got[1].Role != runtimeContext.Body[0].Role || got[1].Content != runtimeContext.Body[0].Content {
		t.Fatalf("ContextMessages()[1] = %#v, want %#v", got[1], runtimeContext.Body[0])
	}
}

func TestContextMessagesKeepsOriginalStateWhenCompressorFails(t *testing.T) {
	original := []model.Message{
		{Role: model.RoleUser, Content: strings.Repeat("a", 60)},
		{Role: model.RoleAssistant, Content: strings.Repeat("b", 60)},
	}
	mgr, err := NewManager(Options{
		MaxContextTokens: 100,
		Counter:          fakeTokenCounter{},
		Compressor: func(_ context.Context, request CompressionRequest) (string, error) {
			return "", context.DeadlineExceeded
		},
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	mgr.AddMessages(original)

	_, err = mgr.ContextMessages(context.Background())
	if err == nil {
		t.Fatal("ContextMessages() error = nil, want compressor failure")
	}
	if mgr.Summary() != "" {
		t.Fatalf("Summary() = %q, want empty after failed compression", mgr.Summary())
	}
	short := mgr.ShortTermMessages()
	if len(short) != len(original) {
		t.Fatalf("len(ShortTermMessages()) = %d, want %d", len(short), len(original))
	}
	for i := range original {
		if short[i].Role != original[i].Role || short[i].Content != original[i].Content {
			t.Fatalf("ShortTermMessages()[%d] = %#v, want %#v", i, short[i], original[i])
		}
	}
}

func TestRuntimeContextWithReserveTriggersCompressionWhenPromptOverheadPushesRequestOverBudget(t *testing.T) {
	compressCalls := 0
	mgr, err := NewManager(Options{
		MaxContextTokens: 100,
		Counter:          fakeTokenCounter{},
		Compressor: func(_ context.Context, request CompressionRequest) (string, error) {
			compressCalls++
			return "compressed memory", nil
		},
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	mgr.AddMessages([]model.Message{
		{Role: model.RoleUser, Content: strings.Repeat("a", 30)},
		{Role: model.RoleAssistant, Content: strings.Repeat("b", 30)},
	})

	state, trace, err := mgr.RuntimeContextWithReserve(context.Background(), 50)
	if err != nil {
		t.Fatalf("RuntimeContextWithReserve() error = %v", err)
	}
	if compressCalls != 1 {
		t.Fatalf("compressor called %d times, want 1", compressCalls)
	}
	if !trace.Attempted || !trace.Succeeded {
		t.Fatalf("trace = %#v, want attempted and succeeded", trace)
	}
	if state.Summary == nil || !strings.Contains(state.Summary.Content, "compressed memory") {
		t.Fatalf("summary = %#v, want rendered compressed summary", state.Summary)
	}
}

func TestContextMessagesCountsTextAttachmentsTowardBudget(t *testing.T) {
	compressCalls := 0
	mgr, err := NewManager(Options{
		MaxContextTokens: 50,
		Counter:          fakeTokenCounter{},
		Compressor: func(_ context.Context, request CompressionRequest) (string, error) {
			compressCalls++
			return "compressed", nil
		},
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	mgr.AddMessage(model.Message{
		Role: model.RoleUser,
		Attachments: []model.Attachment{{
			FileName: "notes.txt",
			MimeType: "text/plain",
			Data:     []byte(strings.Repeat("a", 60)),
		}},
	})

	got, err := mgr.ContextMessages(context.Background())
	if err != nil {
		t.Fatalf("ContextMessages() error = %v", err)
	}
	if compressCalls != 1 {
		t.Fatalf("compressor called %d times, want 1 for attachment-heavy message", compressCalls)
	}
	if len(got) != 1 || !strings.Contains(got[0].Content, "compressed") {
		t.Fatalf("ContextMessages() = %#v, want compressed summary message", got)
	}
}

func TestContextMessagesCountsProviderStateTowardBudget(t *testing.T) {
	compressCalls := 0
	mgr, err := NewManager(Options{
		MaxContextTokens: 250,
		Counter:          fakeTokenCounter{},
		Compressor: func(_ context.Context, request CompressionRequest) (string, error) {
			compressCalls++
			return "compressed", nil
		},
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	mgr.AddMessage(model.Message{Role: model.RoleUser, Content: "prefix"})
	mgr.AddMessage(model.Message{
		Role:    model.RoleAssistant,
		Content: "ok",
		ProviderState: &model.ProviderState{
			Provider: "openai_completions",
			Format:   "openai_chat_message.v1",
			Version:  "v1",
			Payload:  []byte(`{"role":"assistant","content":"` + strings.Repeat("x", 60) + `"}`),
		},
	})

	got, err := mgr.ContextMessages(context.Background())
	if err != nil {
		t.Fatalf("ContextMessages() error = %v", err)
	}
	if compressCalls != 1 {
		t.Fatalf("compressor called %d times, want 1 when provider state pushes context over budget", compressCalls)
	}
	if len(got) != 2 {
		t.Fatalf("len(ContextMessages()) = %d, want 2 summary+preserved assistant", len(got))
	}
	if !strings.Contains(got[0].Content, "compressed") {
		t.Fatalf("summary message = %#v, want compressed summary", got[0])
	}
	if got[1].ProviderState == nil {
		t.Fatal("got[1].ProviderState = nil, want replayable assistant preserved")
	}
}

func TestContextMessagesPrependsSummaryBeforeNewShortTermMessages(t *testing.T) {
	mgr, err := NewManager(Options{
		MaxContextTokens: 100,
		Counter:          fakeTokenCounter{},
		Compressor: func(_ context.Context, request CompressionRequest) (string, error) {
			return "compressed memory", nil
		},
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	mgr.AddMessage(model.Message{Role: model.RoleUser, Content: strings.Repeat("a", 60)})
	mgr.AddMessage(model.Message{Role: model.RoleAssistant, Content: strings.Repeat("b", 60)})
	if _, err := mgr.ContextMessages(context.Background()); err != nil {
		t.Fatalf("initial ContextMessages() error = %v", err)
	}

	mgr.AddMessage(model.Message{Role: model.RoleUser, Content: "follow up"})

	got, err := mgr.ContextMessages(context.Background())
	if err != nil {
		t.Fatalf("ContextMessages() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(ContextMessages()) = %d, want 2", len(got))
	}
	if got[0].Role != model.RoleSystem || !strings.Contains(got[0].Content, "compressed memory") {
		t.Fatalf("summary message = %#v, want prepended compressed summary", got[0])
	}
	if got[1].Role != model.RoleUser || got[1].Content != "follow up" {
		t.Fatalf("latest short-term message = %#v, want preserved follow up", got[1])
	}
}

func TestContextMessagesCompressionPreservesReplayableTail(t *testing.T) {
	var seen CompressionRequest
	mgr, err := NewManager(Options{
		MaxContextTokens: 260,
		Counter:          fakeTokenCounter{},
		Compressor: func(_ context.Context, request CompressionRequest) (string, error) {
			seen = request
			return "compressed history", nil
		},
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	mgr.AddMessages([]model.Message{
		{Role: model.RoleUser, Content: strings.Repeat("a", 60)},
		{Role: model.RoleAssistant, Content: strings.Repeat("b", 60)},
		{
			Role:    model.RoleAssistant,
			Content: "normalized text",
			ProviderState: &model.ProviderState{
				Provider: "openai_completions",
				Format:   "openai_chat_message.v1",
				Version:  "v1",
				Payload:  []byte(`{"role":"assistant","content":"provider text"}`),
			},
		},
		{Role: model.RoleTool, ToolCallId: "call_1", Content: `{"temp":23}`},
		{Role: model.RoleUser, Content: "follow up"},
	})

	got, err := mgr.ContextMessages(context.Background())
	if err != nil {
		t.Fatalf("ContextMessages() error = %v", err)
	}
	if len(seen.Messages) != 2 {
		t.Fatalf("len(CompressionRequest.Messages) = %d, want 2 compressible prefix messages", len(seen.Messages))
	}
	if len(got) != 4 {
		t.Fatalf("len(ContextMessages()) = %d, want 4", len(got))
	}
	if got[0].Role != model.RoleSystem || !strings.Contains(got[0].Content, "compressed history") {
		t.Fatalf("summary message = %#v, want compressed summary", got[0])
	}
	if got[1].ProviderState == nil {
		t.Fatal("got[1].ProviderState = nil, want replayable assistant tail preserved")
	}
	if got[1].Role != model.RoleAssistant || got[2].Role != model.RoleTool || got[3].Role != model.RoleUser {
		t.Fatalf("preserved tail roles = %#v, want assistant/tool/user suffix", []string{got[1].Role, got[2].Role, got[3].Role})
	}
	short := mgr.ShortTermMessages()
	if len(short) != 3 {
		t.Fatalf("len(ShortTermMessages()) = %d, want 3 preserved tail messages", len(short))
	}
	if short[0].ProviderState == nil {
		t.Fatal("ShortTermMessages()[0].ProviderState = nil, want preserved provider state")
	}
	if string(short[0].ProviderState.Payload) != `{"role":"assistant","content":"provider text"}` {
		t.Fatalf("preserved provider state payload = %q, want original payload", string(short[0].ProviderState.Payload))
	}
}

func TestContextMessagesErrorsWhenReplayableTailAloneExceedsBudget(t *testing.T) {
	mgr, err := NewManager(Options{
		MaxContextTokens: 20,
		Counter:          fakeTokenCounter{},
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	mgr.AddMessage(model.Message{
		Role:    model.RoleAssistant,
		Content: "ok",
		ProviderState: &model.ProviderState{
			Provider: "openai_completions",
			Format:   "openai_chat_message.v1",
			Version:  "v1",
			Payload:  []byte(`{"role":"assistant","content":"` + strings.Repeat("x", 80) + `"}`),
		},
	})

	_, err = mgr.ContextMessages(context.Background())
	if err == nil {
		t.Fatal("ContextMessages() error = nil, want budget validation error")
	}
	if !strings.Contains(err.Error(), "preserved replayable tail exceeds short-term budget") {
		t.Fatalf("ContextMessages() error = %v, want replayable tail budget error", err)
	}
}

func TestContextMessagesErrorsWhenCompressedResultStillExceedsBudget(t *testing.T) {
	compressCalls := 0
	mgr, err := NewManager(Options{
		MaxContextTokens: 40,
		Counter:          fakeTokenCounter{},
		Compressor: func(_ context.Context, request CompressionRequest) (string, error) {
			compressCalls++
			return "sum", nil
		},
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	mgr.AddMessages([]model.Message{
		{Role: model.RoleUser, Content: "prefix"},
		{
			Role:    model.RoleAssistant,
			Content: "ok",
			ProviderState: &model.ProviderState{
				Provider: "openai_completions",
				Format:   "openai_chat_message.v1",
				Version:  "v1",
				Payload:  []byte(`{"role":"assistant","content":"` + strings.Repeat("x", 80) + `"}`),
			},
		},
	})

	_, err = mgr.ContextMessages(context.Background())
	if err == nil {
		t.Fatal("ContextMessages() error = nil, want budget validation error")
	}
	if compressCalls != 1 {
		t.Fatalf("compressor called %d times, want 1", compressCalls)
	}
	if !strings.Contains(err.Error(), "preserved replayable tail exceeds short-term budget") {
		t.Fatalf("ContextMessages() error = %v, want replayable tail budget error", err)
	}
	if mgr.Summary() != "" {
		t.Fatalf("Summary() = %q, want unchanged empty summary on failed validation", mgr.Summary())
	}
	if short := mgr.ShortTermMessages(); len(short) != 2 {
		t.Fatalf("len(ShortTermMessages()) = %d, want original messages retained on failure", len(short))
	}
}

func TestShortTermMessagesReturnsDeepCopy(t *testing.T) {
	mgr, err := NewManager(Options{Counter: fakeTokenCounter{}})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	mgr.AddMessage(model.Message{
		Role: model.RoleAssistant,
		Attachments: []model.Attachment{{
			FileName: "note.txt",
			MimeType: "text/plain",
			Data:     []byte("hello"),
		}},
		ReasoningItems: []model.ReasoningItem{{
			ID:      "rs_1",
			Summary: []model.ReasoningSummary{{Text: "plan first"}},
		}},
		ToolCalls: []coretypes.ToolCall{{
			ID:               "call_1",
			Name:             "search",
			Arguments:        `{"q":"golang"}`,
			ThoughtSignature: []byte("sig"),
		}},
		ProviderState: &model.ProviderState{
			Provider:   "openai_completions",
			Format:     "openai_chat_message.v1",
			Version:    "v1",
			ResponseID: "resp_1",
			Payload:    []byte(`{"content":"hello"}`),
		},
		ProviderData: map[string]any{
			"type":        "openai_responses.output.v1",
			"response_id": "resp_1",
			"output_json": `[]`,
		},
	})

	got := mgr.ShortTermMessages()
	got[0].Attachments[0].Data[0] = 'x'
	got[0].ReasoningItems[0].Summary[0].Text = "mutated"
	got[0].ToolCalls[0].ThoughtSignature[0] = 'x'
	got[0].ProviderState.Payload[2] = 'X'
	got[0].ProviderState.ResponseID = "changed"
	providerData := got[0].ProviderData.(map[string]any)
	providerData["response_id"] = "changed"

	again := mgr.ShortTermMessages()
	if string(again[0].Attachments[0].Data) != "hello" {
		t.Fatalf("attachment data = %q, want hello", string(again[0].Attachments[0].Data))
	}
	if again[0].ReasoningItems[0].Summary[0].Text != "plan first" {
		t.Fatalf("reasoning summary = %q, want plan first", again[0].ReasoningItems[0].Summary[0].Text)
	}
	if string(again[0].ToolCalls[0].ThoughtSignature) != "sig" {
		t.Fatalf("thought signature = %q, want sig", string(again[0].ToolCalls[0].ThoughtSignature))
	}
	if string(again[0].ProviderState.Payload) != `{"content":"hello"}` {
		t.Fatalf("provider state payload = %q, want original payload", string(again[0].ProviderState.Payload))
	}
	if again[0].ProviderState.ResponseID != "resp_1" {
		t.Fatalf("provider state response id = %q, want resp_1", again[0].ProviderState.ResponseID)
	}
	if again[0].ProviderData == nil {
		t.Fatal("provider data = nil, want preserved provider data")
	}
	againProviderData := again[0].ProviderData.(map[string]any)
	if againProviderData["response_id"] != "resp_1" {
		t.Fatalf("provider data response_id = %#v, want resp_1", againProviderData["response_id"])
	}
}

type memoryLogSpy struct {
	entries []memoryLogEntry
}

type memoryLogEntry struct {
	level  string
	msg    string
	fields map[string]any
}

func (s *memoryLogSpy) Debug(msg string, fields ...corelog.Field) { s.entries = append(s.entries, newMemoryLogEntry("debug", msg, fields...)) }
func (s *memoryLogSpy) Info(msg string, fields ...corelog.Field)  { s.entries = append(s.entries, newMemoryLogEntry("info", msg, fields...)) }
func (s *memoryLogSpy) Warn(msg string, fields ...corelog.Field)  { s.entries = append(s.entries, newMemoryLogEntry("warn", msg, fields...)) }
func (s *memoryLogSpy) Error(msg string, fields ...corelog.Field) { s.entries = append(s.entries, newMemoryLogEntry("error", msg, fields...)) }

func newMemoryLogEntry(level string, msg string, fields ...corelog.Field) memoryLogEntry {
	mapped := make(map[string]any, len(fields))
	for _, field := range fields {
		mapped[field.Key] = field.Value
	}
	return memoryLogEntry{level: level, msg: msg, fields: mapped}
}

func assertMemoryLogContains(t *testing.T, entries []memoryLogEntry, level string, msg string) {
	t.Helper()
	for _, entry := range entries {
		if entry.level == level && entry.msg == msg {
			return
		}
	}
	t.Fatalf("memory log entry not found: level=%s msg=%s entries=%#v", level, msg, entries)
}

func TestContextMessagesLogsCompressionTriggeredAndFailure(t *testing.T) {
	spy := &memoryLogSpy{}
	original := corelog.SetLogger(spy)
	defer corelog.SetLogger(original)

	mgr, err := NewManager(Options{
		MaxContextTokens: 100,
		Counter:          fakeTokenCounter{},
		Compressor: func(_ context.Context, request CompressionRequest) (string, error) {
			return "", context.DeadlineExceeded
		},
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	mgr.AddMessages([]model.Message{
		{Role: model.RoleUser, Content: strings.Repeat("a", 60)},
		{Role: model.RoleAssistant, Content: strings.Repeat("b", 60)},
	})
	_, err = mgr.ContextMessages(context.Background())
	if err == nil {
		t.Fatal("ContextMessages() error = nil, want compressor failure")
	}
	assertMemoryLogContains(t, spy.entries, "info", "memory compression triggered")
	assertMemoryLogContains(t, spy.entries, "error", "memory compression failed")
}
type fakeTokenCounter struct{}

func (fakeTokenCounter) Count(text string) int {
	return len([]rune(text))
}

func (fakeTokenCounter) CountMessages(messages []string) int {
	total := 0
	for _, message := range messages {
		total += len([]rune(message))
	}
	return total
}
