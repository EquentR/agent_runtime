package openai_official

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
	"github.com/openai/openai-go/responses"
)

func TestStreamToolCallAccumulator_AppendAndAssemble(t *testing.T) {
	acc := newStreamToolCallAccumulator()

	acc.AddOutputItem(responses.ResponseOutputItemUnion{
		Type:   "function_call",
		CallID: "call_1",
		Name:   "lookup_weather",
	})
	acc.AppendArgumentsDelta("call_1", "{\"city\":")
	acc.AppendArgumentsDelta("call_1", "\"Beijing\"}")

	acc.AddOutputItem(responses.ResponseOutputItemUnion{
		Type:      "function_call",
		CallID:    "call_2",
		Name:      "lookup_time",
		Arguments: `{"city":"Beijing"}`,
	})

	got := acc.ToolCalls()
	if len(got) != 2 {
		t.Fatalf("len(tool calls) = %d, want 2", len(got))
	}
	if got[0].ID != "call_1" || got[0].Name != "lookup_weather" || got[0].Arguments != `{"city":"Beijing"}` {
		t.Fatalf("tool call[0] = %#v, want call_1/lookup_weather/{\"city\":\"Beijing\"}", got[0])
	}
	if got[1].ID != "call_2" || got[1].Name != "lookup_time" {
		t.Fatalf("tool call[1] = %#v, want call_2/lookup_time", got[1])
	}
}

func TestResolveStreamResponseType(t *testing.T) {
	tests := []struct {
		name         string
		finishReason string
		toolCalls    []types.ToolCall
		want         model.StreamResponseType
	}{
		{name: "tool calls by finish reason", finishReason: "tool_calls", want: model.StreamResponseToolCall},
		{name: "tool calls by payload", toolCalls: []types.ToolCall{{Name: "lookup_weather"}}, want: model.StreamResponseToolCall},
		{name: "text", finishReason: "stop", want: model.StreamResponseText},
		{name: "unknown", want: model.StreamResponseUnknown},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveStreamResponseType(tc.finishReason, tc.toolCalls); got != tc.want {
				t.Fatalf("resolveStreamResponseType() = %q, want %q", got, tc.want)
			}
		})
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

	emit := func(s string) {
		chunks = append(chunks, s)
	}
	setErr := func(error) {}

	applyStreamEvent(responses.ResponseStreamEventUnion{Type: "response.output_item.added", OutputIndex: 0, Item: responses.ResponseOutputItemUnion{Type: "function_call", ID: "item_1", CallID: "call_1", Name: "lookup_weather"}}, acc, &outputItems, reasoningItems, stats, &once, start, splitter, &reasoning, func(string) {}, emit, func(model.StreamEvent) {}, func(string) {}, setErr)
	applyStreamEvent(responses.ResponseStreamEventUnion{Type: "response.output_item.added", OutputIndex: 1, Item: responses.ResponseOutputItemUnion{Type: "reasoning", ID: "rs_1"}}, acc, &outputItems, reasoningItems, stats, &once, start, splitter, &reasoning, func(string) {}, emit, func(model.StreamEvent) {}, func(string) {}, setErr)
	applyStreamEvent(responses.ResponseStreamEventUnion{Type: "response.output_item.added", OutputIndex: 2, Item: responses.ResponseOutputItemUnion{Type: "message", ID: "msg_1"}}, acc, &outputItems, reasoningItems, stats, &once, start, splitter, &reasoning, func(string) {}, emit, func(model.StreamEvent) {}, func(string) {}, setErr)
	applyStreamEvent(responses.ResponseStreamEventUnion{Type: "response.function_call_arguments.delta", ItemID: "item_1", Delta: responses.ResponseStreamEventUnionDelta{OfString: "{\"city\":"}}, acc, &outputItems, reasoningItems, stats, &once, start, splitter, &reasoning, func(string) {}, emit, func(model.StreamEvent) {}, func(string) {}, setErr)
	applyStreamEvent(responses.ResponseStreamEventUnion{Type: "response.function_call_arguments.delta", ItemID: "item_1", Delta: responses.ResponseStreamEventUnionDelta{OfString: "\"Beijing\"}"}}, acc, &outputItems, reasoningItems, stats, &once, start, splitter, &reasoning, func(string) {}, emit, func(model.StreamEvent) {}, func(string) {}, setErr)
	applyStreamEvent(responses.ResponseStreamEventUnion{Type: "response.reasoning_summary_text.delta", OutputIndex: 1, Delta: responses.ResponseStreamEventUnionDelta{OfString: "plan first"}}, acc, &outputItems, reasoningItems, stats, &once, start, splitter, &reasoning, func(string) {}, emit, func(model.StreamEvent) {}, func(string) {}, setErr)
	applyStreamEvent(responses.ResponseStreamEventUnion{Type: "response.output_text.delta", OutputIndex: 2, ContentIndex: 0, Delta: responses.ResponseStreamEventUnionDelta{OfString: "<think>shadow</think>hello"}}, acc, &outputItems, reasoningItems, stats, &once, start, splitter, &reasoning, func(string) {}, emit, func(model.StreamEvent) {}, func(string) {}, setErr)
	applyStreamEvent(responses.ResponseStreamEventUnion{Type: "response.completed", Response: responses.Response{Status: "completed", Usage: responses.ResponseUsage{InputTokens: 3, OutputTokens: 4, TotalTokens: 7}}}, acc, &outputItems, reasoningItems, stats, &once, start, splitter, &reasoning, func(string) {}, emit, func(model.StreamEvent) {}, func(string) {}, setErr)

	if len(chunks) != 1 || chunks[0] != "hello" {
		t.Fatalf("chunks = %#v, want [hello]", chunks)
	}
	if reasoning.String() != "plan first" {
		t.Fatalf("reasoning = %q, want %q", reasoning.String(), "plan first")
	}
	if stats.TTFT <= 0 {
		t.Fatalf("stats.TTFT = %v, want > 0", stats.TTFT)
	}
	if stats.Usage.TotalTokens != 7 || stats.Usage.PromptTokens != 3 || stats.Usage.CompletionTokens != 4 {
		t.Fatalf("usage = %#v, want {3,4,7}", stats.Usage)
	}
	if stats.FinishReason != "tool_calls" {
		t.Fatalf("finish reason = %q, want tool_calls", stats.FinishReason)
	}
	toolCalls := acc.ToolCalls()
	if len(toolCalls) != 1 || toolCalls[0].Arguments != `{"city":"Beijing"}` {
		t.Fatalf("tool calls = %#v, want single assembled call", toolCalls)
	}
	if len(outputItems) != 3 {
		t.Fatalf("len(outputItems) = %d, want 3", len(outputItems))
	}
	if outputItems[1].Type != "reasoning" || len(outputItems[1].Summary) != 1 || outputItems[1].Summary[0].Text != "plan first" {
		t.Fatalf("reasoning output item = %#v", outputItems[1])
	}
	if outputItems[2].Type != "message" || len(outputItems[2].Content) != 1 || outputItems[2].Content[0].Text != "<think>shadow</think>hello" {
		t.Fatalf("message output item = %#v", outputItems[2])
	}
	compact := compactReasoningItemsByOutputIndex(reasoningItems)
	if len(compact) != 1 || compact[0].Summary[0].Text != "plan first" {
		t.Fatalf("reasoningItems = %#v", compact)
	}
}

func TestApplyStreamEvent_PreservesOutputOrderByIndex(t *testing.T) {
	stats := &model.StreamStats{}
	acc := newStreamToolCallAccumulator()
	outputItems := make([]responses.ResponseOutputItemUnion, 0)
	reasoningItems := make(map[int64]model.ReasoningItem)
	var once sync.Once

	applyStreamEvent(responses.ResponseStreamEventUnion{Type: "response.output_item.added", OutputIndex: 2, Item: responses.ResponseOutputItemUnion{Type: "message", ID: "msg_1"}}, acc, &outputItems, reasoningItems, stats, &once, time.Now(), model.NewLeadingThinkStreamSplitter(), &strings.Builder{}, func(string) {}, func(string) {}, func(model.StreamEvent) {}, func(string) {}, func(error) {})
	applyStreamEvent(responses.ResponseStreamEventUnion{Type: "response.output_item.added", OutputIndex: 0, Item: responses.ResponseOutputItemUnion{Type: "reasoning", ID: "rs_1"}}, acc, &outputItems, reasoningItems, stats, &once, time.Now(), model.NewLeadingThinkStreamSplitter(), &strings.Builder{}, func(string) {}, func(string) {}, func(model.StreamEvent) {}, func(string) {}, func(error) {})
	applyStreamEvent(responses.ResponseStreamEventUnion{Type: "response.output_item.added", OutputIndex: 1, Item: responses.ResponseOutputItemUnion{Type: "function_call", ID: "fc_1", CallID: "call_1", Name: "lookup_weather"}}, acc, &outputItems, reasoningItems, stats, &once, time.Now(), model.NewLeadingThinkStreamSplitter(), &strings.Builder{}, func(string) {}, func(string) {}, func(model.StreamEvent) {}, func(string) {}, func(error) {})

	if len(outputItems) != 3 {
		t.Fatalf("len(outputItems) = %d, want 3", len(outputItems))
	}
	if outputItems[0].Type != "reasoning" || outputItems[1].Type != "function_call" || outputItems[2].Type != "message" {
		t.Fatalf("output item order = %#v", outputItems)
	}
}

func TestApplyStreamEvent_EmitsUpdatedToolCallForMatchingEvent(t *testing.T) {
	stats := &model.StreamStats{}
	acc := newStreamToolCallAccumulator()
	outputItems := make([]responses.ResponseOutputItemUnion, 0)
	reasoningItems := make(map[int64]model.ReasoningItem)
	var once sync.Once
	var emitted []types.ToolCall

	emitEvent := func(event model.StreamEvent) {
		if event.Type == model.StreamEventToolCallDelta {
			emitted = append(emitted, event.ToolCall)
		}
	}

	applyStreamEvent(responses.ResponseStreamEventUnion{Type: "response.output_item.added", OutputIndex: 0, Item: responses.ResponseOutputItemUnion{Type: "function_call", ID: "item_1", CallID: "call_1", Name: "lookup_weather"}}, acc, &outputItems, reasoningItems, stats, &once, time.Now(), model.NewLeadingThinkStreamSplitter(), &strings.Builder{}, func(string) {}, func(string) {}, emitEvent, func(string) {}, func(error) {})
	applyStreamEvent(responses.ResponseStreamEventUnion{Type: "response.output_item.added", OutputIndex: 1, Item: responses.ResponseOutputItemUnion{Type: "function_call", ID: "item_2", CallID: "call_2", Name: "lookup_time"}}, acc, &outputItems, reasoningItems, stats, &once, time.Now(), model.NewLeadingThinkStreamSplitter(), &strings.Builder{}, func(string) {}, func(string) {}, emitEvent, func(string) {}, func(error) {})
	applyStreamEvent(responses.ResponseStreamEventUnion{Type: "response.function_call_arguments.delta", ItemID: "item_1", Delta: responses.ResponseStreamEventUnionDelta{OfString: "{\"city\":"}}, acc, &outputItems, reasoningItems, stats, &once, time.Now(), model.NewLeadingThinkStreamSplitter(), &strings.Builder{}, func(string) {}, func(string) {}, emitEvent, func(string) {}, func(error) {})
	applyStreamEvent(responses.ResponseStreamEventUnion{Type: "response.function_call_arguments.delta", ItemID: "item_2", Delta: responses.ResponseStreamEventUnionDelta{OfString: "{\"zone\":"}}, acc, &outputItems, reasoningItems, stats, &once, time.Now(), model.NewLeadingThinkStreamSplitter(), &strings.Builder{}, func(string) {}, func(string) {}, emitEvent, func(string) {}, func(error) {})

	if len(emitted) < 4 {
		t.Fatalf("len(emitted) = %d, want at least 4", len(emitted))
	}
	if emitted[2].ID != "call_1" || emitted[2].Arguments != `{"city":` {
		t.Fatalf("emitted[2] = %#v, want updated call_1", emitted[2])
	}
	if emitted[3].ID != "call_2" || emitted[3].Arguments != `{"zone":` {
		t.Fatalf("emitted[3] = %#v, want updated call_2", emitted[3])
	}
}

func TestApplyStreamEvent_ReasoningItemsRemainCompactForNonZeroOutputIndex(t *testing.T) {
	stats := &model.StreamStats{}
	acc := newStreamToolCallAccumulator()
	outputItems := make([]responses.ResponseOutputItemUnion, 0)
	reasoningItems := make(map[int64]model.ReasoningItem)
	var once sync.Once
	var reasoning strings.Builder

	applyStreamEvent(
		responses.ResponseStreamEventUnion{
			Type:        "response.output_item.added",
			OutputIndex: 2,
			Item: responses.ResponseOutputItemUnion{
				Type:             "reasoning",
				ID:               "rs_2",
				EncryptedContent: "enc_2",
			},
		},
		acc,
		&outputItems,
		reasoningItems,
		stats,
		&once,
		time.Now(),
		model.NewLeadingThinkStreamSplitter(),
		&reasoning,
		func(string) {},
		func(string) {},
		func(model.StreamEvent) {},
		func(string) {},
		func(error) {},
	)
	applyStreamEvent(
		responses.ResponseStreamEventUnion{
			Type:        "response.reasoning_summary_text.delta",
			OutputIndex: 2,
			Delta:       responses.ResponseStreamEventUnionDelta{OfString: "plan later"},
		},
		acc,
		&outputItems,
		reasoningItems,
		stats,
		&once,
		time.Now(),
		model.NewLeadingThinkStreamSplitter(),
		&reasoning,
		func(string) {},
		func(string) {},
		func(model.StreamEvent) {},
		func(string) {},
		func(error) {},
	)

	compact := compactReasoningItemsByOutputIndex(reasoningItems)
	if len(compact) != 1 {
		t.Fatalf("len(reasoningItems) = %d, want 1", len(compact))
	}
	if compact[0].ID != "rs_2" {
		t.Fatalf("reasoningItems[0].ID = %q, want rs_2", compact[0].ID)
	}
	if compact[0].EncryptedContent != "enc_2" {
		t.Fatalf("reasoningItems[0].EncryptedContent = %q, want enc_2", compact[0].EncryptedContent)
	}
	if len(compact[0].Summary) != 1 || compact[0].Summary[0].Text != "plan later" {
		t.Fatalf("reasoningItems[0].Summary = %#v", compact[0].Summary)
	}
}

func TestResponseStreamFinalMessage_PreservesProviderStateOutputSequence(t *testing.T) {
	state, err := providerStateFromOutputItems("resp_1", []responses.ResponseOutputItemUnion{
		{
			Type:             "reasoning",
			ID:               "rs_1",
			EncryptedContent: "enc_123",
			Summary: []responses.ResponseReasoningItemSummary{{
				Text: "plan first",
			}},
		},
		{
			Type:    "message",
			ID:      "msg_1",
			Content: []responses.ResponseOutputMessageContentUnion{{Type: "output_text", Text: "hello"}},
		},
		{
			Type:      "function_call",
			ID:        "fc_1",
			CallID:    "call_1",
			Name:      "lookup_weather",
			Arguments: `{"city":"Beijing"}`,
		},
	})
	if err != nil {
		t.Fatalf("providerStateFromOutputItems() error = %v", err)
	}

	s := &responseStream{
		ctx:    context.Background(),
		cancel: func() {},
		stats:  &model.StreamStats{},
	}
	s.setFinalMessage(finalAssistantMessageFromResponse(
		"hello",
		"plan",
		[]model.ReasoningItem{{
			ID:               "rs_1",
			EncryptedContent: "enc_123",
			Summary: []model.ReasoningSummary{{
				Text: "plan first",
			}},
		}},
		[]types.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: `{"city":"Beijing"}`}},
		state,
	))

	msg, err := s.FinalMessage()
	if err != nil {
		t.Fatalf("FinalMessage() error = %v", err)
	}
	if msg.ProviderState == nil {
		t.Fatal("FinalMessage().ProviderState = nil, want provider state")
	}
	if msg.ProviderState.Provider != "openai_responses" {
		t.Fatalf("provider = %q, want %q", msg.ProviderState.Provider, "openai_responses")
	}
	if msg.ProviderState.Format != "openai_response_state.v1" {
		t.Fatalf("format = %q, want %q", msg.ProviderState.Format, "openai_response_state.v1")
	}

	var replayed struct {
		ResponseID string                              `json:"response_id"`
		Output     []responses.ResponseOutputItemUnion `json:"output"`
	}
	if err := json.Unmarshal(msg.ProviderState.Payload, &replayed); err != nil {
		t.Fatalf("unmarshal provider state payload: %v", err)
	}
	if replayed.ResponseID != "resp_1" {
		t.Fatalf("response_id = %q, want resp_1", replayed.ResponseID)
	}
	if len(replayed.Output) != 3 {
		t.Fatalf("len(replayed.output) = %d, want 3", len(replayed.Output))
	}
	if replayed.Output[0].Type != "reasoning" || replayed.Output[1].Type != "message" || replayed.Output[2].Type != "function_call" {
		t.Fatalf("replayed output sequence = %#v", replayed.Output)
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].ID != "call_1" {
		t.Fatalf("FinalMessage().ToolCalls = %#v", msg.ToolCalls)
	}
	if len(msg.ReasoningItems) != 1 || msg.ReasoningItems[0].ID != "rs_1" {
		t.Fatalf("FinalMessage().ReasoningItems = %#v", msg.ReasoningItems)
	}
	if msg.Content != "hello" || msg.Reasoning != "plan" {
		t.Fatalf("FinalMessage() = %#v", msg)
	}
}

func TestApplyStreamEvent_FailedWithoutMessageSetsGenericError(t *testing.T) {
	stats := &model.StreamStats{}
	acc := newStreamToolCallAccumulator()
	var once sync.Once
	start := time.Now()

	var gotErr error
	outputItems := make([]responses.ResponseOutputItemUnion, 0)
	reasoningItems := make(map[int64]model.ReasoningItem)
	applyStreamEvent(
		responses.ResponseStreamEventUnion{
			Type:     "response.failed",
			Response: responses.Response{Status: "failed"},
		},
		acc,
		&outputItems,
		reasoningItems,
		stats,
		&once,
		start,
		model.NewLeadingThinkStreamSplitter(),
		&strings.Builder{},
		func(string) {},
		func(string) {},
		func(model.StreamEvent) {},
		func(string) {},
		func(err error) { gotErr = err },
	)

	if gotErr == nil {
		t.Fatal("expected error to be set for response.failed")
	}
}

func TestApplyStreamEvent_ErrorWithoutMessageSetsGenericError(t *testing.T) {
	stats := &model.StreamStats{}
	acc := newStreamToolCallAccumulator()
	var once sync.Once
	start := time.Now()

	var gotErr error
	outputItems := make([]responses.ResponseOutputItemUnion, 0)
	reasoningItems := make(map[int64]model.ReasoningItem)
	applyStreamEvent(
		responses.ResponseStreamEventUnion{Type: "error"},
		acc,
		&outputItems,
		reasoningItems,
		stats,
		&once,
		start,
		model.NewLeadingThinkStreamSplitter(),
		&strings.Builder{},
		func(string) {},
		func(string) {},
		func(model.StreamEvent) {},
		func(string) {},
		func(err error) { gotErr = err },
	)

	if gotErr == nil {
		t.Fatal("expected error to be set for error event")
	}
}

func TestResponseStreamRecv_FiltersTextDeltaEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan model.StreamEvent, 3)
	events <- model.StreamEvent{Type: model.StreamEventReasoningDelta, Reasoning: "plan"}
	events <- model.StreamEvent{Type: model.StreamEventTextDelta, Text: "hello"}
	close(events)

	s := &responseStream{
		ctx:    ctx,
		cancel: cancel,
		events: events,
		stats:  &model.StreamStats{},
	}

	got, err := s.Recv()
	if err != nil {
		t.Fatalf("Recv() error = %v", err)
	}
	if got != "hello" {
		t.Fatalf("Recv() = %q, want hello", got)
	}
}

func TestResponseStreamFinalMessage_ReturnsErrorOnCanceledStream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := &responseStream{
		ctx:    ctx,
		cancel: func() {},
		stats:  &model.StreamStats{},
	}

	_, err := s.FinalMessage()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("FinalMessage() error = %v, want %v", err, context.Canceled)
	}
}
