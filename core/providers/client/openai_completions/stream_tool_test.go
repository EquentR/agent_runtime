package openai

import (
	"testing"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/types"
	goopenai "github.com/sashabaranov/go-openai"
)

func TestStreamToolCallAccumulator_Append(t *testing.T) {
	acc := newStreamToolCallAccumulator()
	idx0 := 0
	idx1 := 1

	acc.Append([]goopenai.ToolCall{{
		Index: &idx0,
		ID:    "call_1",
		Type:  goopenai.ToolTypeFunction,
		Function: goopenai.FunctionCall{
			Name:      "lookup_weather",
			Arguments: "{\"city\":",
		},
	}})

	acc.Append([]goopenai.ToolCall{{
		Index: &idx0,
		Function: goopenai.FunctionCall{
			Arguments: "\"Beijing\"}",
		},
	}})

	acc.Append([]goopenai.ToolCall{{
		Index: &idx1,
		ID:    "call_2",
		Type:  goopenai.ToolTypeFunction,
		Function: goopenai.FunctionCall{
			Name:      "lookup_time",
			Arguments: "{\"city\":\"Beijing\"}",
		},
	}})

	got := acc.ToolCalls()
	if len(got) != 2 {
		t.Fatalf("len(acc.ToolCalls()) = %d, want 2", len(got))
	}

	if got[0].ID != "call_1" || got[0].Name != "lookup_weather" || got[0].Arguments != `{"city":"Beijing"}` {
		t.Fatalf("tool call[0] = %#v, want id=call_1 name=lookup_weather args={\"city\":\"Beijing\"}", got[0])
	}
	if got[1].ID != "call_2" || got[1].Name != "lookup_time" {
		t.Fatalf("tool call[1] = %#v, want id=call_2 name=lookup_time", got[1])
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
		{name: "text response", finishReason: "stop", want: model.StreamResponseText},
		{name: "unknown response", want: model.StreamResponseUnknown},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveStreamResponseType(tc.finishReason, tc.toolCalls)
			if got != tc.want {
				t.Fatalf("resolveStreamResponseType() = %q, want %q", got, tc.want)
			}
		})
	}
}
