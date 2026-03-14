package openai_official

import (
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
	var chunks []string
	splitter := model.NewLeadingThinkStreamSplitter()
	var reasoning strings.Builder
	var once sync.Once
	start := time.Now().Add(-5 * time.Millisecond)

	emit := func(s string) {
		chunks = append(chunks, s)
	}
	setErr := func(error) {}

	applyStreamEvent(responses.ResponseStreamEventUnion{Type: "response.output_item.added", Item: responses.ResponseOutputItemUnion{Type: "function_call", ID: "item_1", CallID: "call_1", Name: "lookup_weather"}}, acc, stats, &once, start, splitter, &reasoning, func(string) {}, emit, setErr)
	applyStreamEvent(responses.ResponseStreamEventUnion{Type: "response.function_call_arguments.delta", ItemID: "item_1", Delta: responses.ResponseStreamEventUnionDelta{OfString: "{\"city\":"}}, acc, stats, &once, start, splitter, &reasoning, func(string) {}, emit, setErr)
	applyStreamEvent(responses.ResponseStreamEventUnion{Type: "response.function_call_arguments.delta", ItemID: "item_1", Delta: responses.ResponseStreamEventUnionDelta{OfString: "\"Beijing\"}"}}, acc, stats, &once, start, splitter, &reasoning, func(string) {}, emit, setErr)
	applyStreamEvent(responses.ResponseStreamEventUnion{Type: "response.reasoning_summary_text.delta", Delta: responses.ResponseStreamEventUnionDelta{OfString: "plan first"}}, acc, stats, &once, start, splitter, &reasoning, func(string) {}, emit, setErr)
	applyStreamEvent(responses.ResponseStreamEventUnion{Type: "response.output_text.delta", Delta: responses.ResponseStreamEventUnionDelta{OfString: "<think>shadow</think>hello"}}, acc, stats, &once, start, splitter, &reasoning, func(string) {}, emit, setErr)
	applyStreamEvent(responses.ResponseStreamEventUnion{Type: "response.completed", Response: responses.Response{Status: "completed", Usage: responses.ResponseUsage{InputTokens: 3, OutputTokens: 4, TotalTokens: 7}}}, acc, stats, &once, start, splitter, &reasoning, func(string) {}, emit, setErr)

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
}

func TestApplyStreamEvent_FailedWithoutMessageSetsGenericError(t *testing.T) {
	stats := &model.StreamStats{}
	acc := newStreamToolCallAccumulator()
	var once sync.Once
	start := time.Now()

	var gotErr error
	applyStreamEvent(
		responses.ResponseStreamEventUnion{
			Type:     "response.failed",
			Response: responses.Response{Status: "failed"},
		},
		acc,
		stats,
		&once,
		start,
		model.NewLeadingThinkStreamSplitter(),
		&strings.Builder{},
		func(string) {},
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
	applyStreamEvent(
		responses.ResponseStreamEventUnion{Type: "error"},
		acc,
		stats,
		&once,
		start,
		model.NewLeadingThinkStreamSplitter(),
		&strings.Builder{},
		func(string) {},
		func(string) {},
		func(err error) { gotErr = err },
	)

	if gotErr == nil {
		t.Fatal("expected error to be set for error event")
	}
}
