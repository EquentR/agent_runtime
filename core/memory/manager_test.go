package memory

import (
	"context"
	"strings"
	"testing"

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
	})

	got := mgr.ShortTermMessages()
	got[0].Attachments[0].Data[0] = 'x'
	got[0].ReasoningItems[0].Summary[0].Text = "mutated"
	got[0].ToolCalls[0].ThoughtSignature[0] = 'x'

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
