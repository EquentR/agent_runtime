package openai_responses_new

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/types"
	"github.com/openai/openai-go/v3/responses"
)

func TestStreamToolCallAccumulator_AppendAndAssemble(t *testing.T) {
	acc := newStreamToolCallAccumulator()

	acc.AddOutputItem(responses.ResponseOutputItemUnion{Type: "function_call", CallID: "call_1", Name: "lookup_weather"})
	acc.AppendArgumentsDelta("call_1", "{\"city\":")
	acc.AppendArgumentsDelta("call_1", "\"Beijing\"}")

	got := acc.ToolCalls()
	if len(got) != 1 {
		t.Fatalf("len(tool calls) = %d, want 1", len(got))
	}
	if got[0].ID != "call_1" || got[0].Name != "lookup_weather" || got[0].Arguments != `{"city":"Beijing"}` {
		t.Fatalf("tool call[0] = %#v", got[0])
	}
}

func TestApplyStreamEvent_DeltaToolAndCompletion(t *testing.T) {
	stats := &model.StreamStats{ResponseType: model.StreamResponseUnknown}
	acc := newStreamToolCallAccumulator()
	outputItems := make([]responses.ResponseOutputItemUnion, 0)
	reasoningItems := make(map[int64]model.ReasoningItem)
	var chunks []string
	splitter := model.NewLeadingThinkStreamSplitter()
	var reasoning strings.Builder
	var once sync.Once
	start := time.Now().Add(-5 * time.Millisecond)

	applyStreamEvent(responses.ResponseStreamEventUnion{Type: "response.output_item.added", OutputIndex: 0, Item: responses.ResponseOutputItemUnion{Type: "function_call", ID: "item_1", CallID: "call_1", Name: "lookup_weather"}}, acc, &outputItems, reasoningItems, stats, &once, start, splitter, &reasoning, func(string) {}, func(s string) { chunks = append(chunks, s) }, func(model.StreamEvent) {}, func(string) {}, func(error) {})
	applyStreamEvent(responses.ResponseStreamEventUnion{Type: "response.output_item.added", OutputIndex: 1, Item: responses.ResponseOutputItemUnion{Type: "reasoning", ID: "rs_1"}}, acc, &outputItems, reasoningItems, stats, &once, start, splitter, &reasoning, func(string) {}, func(s string) { chunks = append(chunks, s) }, func(model.StreamEvent) {}, func(string) {}, func(error) {})
	applyStreamEvent(responses.ResponseStreamEventUnion{Type: "response.output_item.added", OutputIndex: 2, Item: responses.ResponseOutputItemUnion{Type: "message", ID: "msg_1"}}, acc, &outputItems, reasoningItems, stats, &once, start, splitter, &reasoning, func(string) {}, func(s string) { chunks = append(chunks, s) }, func(model.StreamEvent) {}, func(string) {}, func(error) {})
	applyStreamEvent(responses.ResponseStreamEventUnion{Type: "response.function_call_arguments.delta", ItemID: "item_1", Delta: "{\"city\":"}, acc, &outputItems, reasoningItems, stats, &once, start, splitter, &reasoning, func(string) {}, func(s string) { chunks = append(chunks, s) }, func(model.StreamEvent) {}, func(string) {}, func(error) {})
	applyStreamEvent(responses.ResponseStreamEventUnion{Type: "response.function_call_arguments.delta", ItemID: "item_1", Delta: "\"Beijing\"}"}, acc, &outputItems, reasoningItems, stats, &once, start, splitter, &reasoning, func(string) {}, func(s string) { chunks = append(chunks, s) }, func(model.StreamEvent) {}, func(string) {}, func(error) {})
	applyStreamEvent(responses.ResponseStreamEventUnion{Type: "response.reasoning_summary_text.delta", OutputIndex: 1, Delta: "plan first"}, acc, &outputItems, reasoningItems, stats, &once, start, splitter, &reasoning, func(string) {}, func(s string) { chunks = append(chunks, s) }, func(model.StreamEvent) {}, func(string) {}, func(error) {})
	applyStreamEvent(responses.ResponseStreamEventUnion{Type: "response.output_text.delta", OutputIndex: 2, ContentIndex: 0, Delta: "<think>shadow</think>hello"}, acc, &outputItems, reasoningItems, stats, &once, start, splitter, &reasoning, func(string) {}, func(s string) { chunks = append(chunks, s) }, func(model.StreamEvent) {}, func(string) {}, func(error) {})
	applyStreamEvent(responses.ResponseStreamEventUnion{Type: "response.completed", Response: responses.Response{Status: "completed", Usage: responses.ResponseUsage{InputTokens: 3, OutputTokens: 4, TotalTokens: 7}}}, acc, &outputItems, reasoningItems, stats, &once, start, splitter, &reasoning, func(string) {}, func(s string) { chunks = append(chunks, s) }, func(model.StreamEvent) {}, func(string) {}, func(error) {})

	if len(chunks) != 1 || chunks[0] != "hello" {
		t.Fatalf("chunks = %#v, want [hello]", chunks)
	}
	if reasoning.String() != "plan first" {
		t.Fatalf("reasoning = %q, want plan first", reasoning.String())
	}
	if stats.FinishReason != "tool_calls" {
		t.Fatalf("finish reason = %q, want tool_calls", stats.FinishReason)
	}
}

func TestResponseStreamFinalMessage_PreservesProviderStateOutputSequence(t *testing.T) {
	state, err := providerStateFromOutputItems("resp_1", []responses.ResponseOutputItemUnion{
		{Type: "reasoning", ID: "rs_1", EncryptedContent: "enc_123", Summary: []responses.ResponseReasoningItemSummary{{Text: "plan first"}}},
		{Type: "message", ID: "msg_1", Content: []responses.ResponseOutputMessageContentUnion{{Type: "output_text", Text: "hello"}}},
		{Type: "function_call", ID: "fc_1", CallID: "call_1", Name: "lookup_weather", Arguments: responses.ResponseOutputItemUnionArguments{OfString: `{"city":"Beijing"}`}},
	})
	if err != nil {
		t.Fatalf("providerStateFromOutputItems() error = %v", err)
	}

	s := &responseStream{ctx: context.Background(), cancel: func() {}, stats: &model.StreamStats{}}
	s.setFinalMessage(finalAssistantMessageFromResponse(
		"hello",
		"plan",
		[]model.ReasoningItem{{ID: "rs_1", EncryptedContent: "enc_123", Summary: []model.ReasoningSummary{{Text: "plan first"}}}},
		[]types.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: `{"city":"Beijing"}`}},
		state,
	))

	msg, err := s.FinalMessage()
	if err != nil {
		t.Fatalf("FinalMessage() error = %v", err)
	}
	if msg.ProviderState == nil {
		t.Fatal("FinalMessage().ProviderState = nil")
	}
	var replayed struct {
		ResponseID string                              `json:"response_id"`
		Output     []responses.ResponseOutputItemUnion `json:"output"`
	}
	if err := json.Unmarshal(msg.ProviderState.Payload, &replayed); err != nil {
		t.Fatalf("json.Unmarshal(provider state): %v", err)
	}
	if replayed.ResponseID != "resp_1" || len(replayed.Output) != 3 {
		t.Fatalf("replayed state = %#v", replayed)
	}
}

func TestResponseStreamFinalMessage_ReturnsErrorOnCanceledStream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := &responseStream{ctx: ctx, cancel: func() {}, stats: &model.StreamStats{}}
	_, err := s.FinalMessage()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("FinalMessage() error = %v, want %v", err, context.Canceled)
	}
}
