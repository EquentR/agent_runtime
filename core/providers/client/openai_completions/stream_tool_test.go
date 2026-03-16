package openai

import (
	"context"
	"encoding/json"
	"errors"
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

func TestOpenAIStreamRecv_FiltersTextDeltaEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan model.StreamEvent, 3)
	events <- model.StreamEvent{Type: model.StreamEventReasoningDelta, Reasoning: "plan"}
	events <- model.StreamEvent{Type: model.StreamEventTextDelta, Text: "hello"}
	close(events)

	s := &openAIStream{
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

	got, err = s.Recv()
	if err != nil {
		t.Fatalf("Recv() second error = %v", err)
	}
	if got != "" {
		t.Fatalf("Recv() second = %q, want empty end-of-stream sentinel", got)
	}
}

func TestOpenAIStreamFinalMessage_ReturnsErrorOnCanceledStream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := &openAIStream{
		ctx:    ctx,
		cancel: func() {},
		stats:  &model.StreamStats{},
	}

	_, err := s.FinalMessage()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("FinalMessage() error = %v, want %v", err, context.Canceled)
	}
}

func TestOpenAIStreamFinalMessage_ReturnsStreamErrorOnFailedStream(t *testing.T) {
	s := &openAIStream{
		ctx:    context.Background(),
		cancel: func() {},
		stats:  &model.StreamStats{},
	}
	streamErr := errors.New("stream failed")
	s.setStreamError(streamErr)

	_, err := s.FinalMessage()
	if !errors.Is(err, streamErr) {
		t.Fatalf("FinalMessage() error = %v, want %v", err, streamErr)
	}
}

func TestOpenAIStreamFinalMessage_ReturnsReplayableAssistantMessage(t *testing.T) {
	state := &model.ProviderState{
		Provider: "openai_completions",
		Format:   "openai_chat_message.v1",
		Version:  "v1",
		Payload:  json.RawMessage(`{"role":"assistant","content":"hello","reasoning_content":"plan","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup_weather","arguments":"{}"}}]}`),
	}

	s := &openAIStream{
		ctx:    context.Background(),
		cancel: func() {},
		stats:  &model.StreamStats{},
	}
	s.setFinalMessage(model.Message{
		Role:          model.RoleAssistant,
		Content:       "hello",
		Reasoning:     "plan",
		ToolCalls:     []types.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: `{}`}},
		ProviderState: state,
	})

	msg, err := s.FinalMessage()
	if err != nil {
		t.Fatalf("FinalMessage() error = %v", err)
	}
	if msg.Role != model.RoleAssistant || msg.Content != "hello" || msg.Reasoning != "plan" {
		t.Fatalf("FinalMessage() = %#v", msg)
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].ID != "call_1" {
		t.Fatalf("FinalMessage().ToolCalls = %#v", msg.ToolCalls)
	}
	if msg.ProviderState == nil {
		t.Fatal("FinalMessage().ProviderState = nil, want provider state")
	}
	if string(msg.ProviderState.Payload) != string(state.Payload) {
		t.Fatalf("FinalMessage().ProviderState.Payload = %s, want %s", string(msg.ProviderState.Payload), string(state.Payload))
	}
}

func TestFinalAssistantMessageFromNativeMessage_PreservesNativePayloadShape(t *testing.T) {
	native := goopenai.ChatCompletionMessage{
		Role:             model.RoleAssistant,
		Content:          "hello",
		ReasoningContent: "plan",
		ToolCalls: []goopenai.ToolCall{{
			ID:   "call_1",
			Type: goopenai.ToolTypeFunction,
			Function: goopenai.FunctionCall{
				Name:      "lookup_weather",
				Arguments: `{"city":"Shanghai"}`,
			},
		}},
	}

	msg, err := finalAssistantMessageFromNativeMessage(native)
	if err != nil {
		t.Fatalf("finalAssistantMessageFromNativeMessage() error = %v", err)
	}
	if msg.Content != "hello" || msg.Reasoning != "plan" {
		t.Fatalf("message = %#v", msg)
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].ID != "call_1" {
		t.Fatalf("message tool calls = %#v", msg.ToolCalls)
	}
	if msg.ProviderState == nil {
		t.Fatal("message.ProviderState = nil, want provider state")
	}
	var replayed goopenai.ChatCompletionMessage
	if err := json.Unmarshal(msg.ProviderState.Payload, &replayed); err != nil {
		t.Fatalf("unmarshal provider state payload: %v", err)
	}
	if replayed.Refusal != "" {
		t.Fatalf("replayed.Refusal = %q, want empty", replayed.Refusal)
	}
	if replayed.ReasoningContent != "plan" || replayed.Content != "hello" {
		t.Fatalf("replayed payload = %#v", replayed)
	}
	if len(replayed.ToolCalls) != 1 || replayed.ToolCalls[0].Function.Arguments != `{"city":"Shanghai"}` {
		t.Fatalf("replayed tool calls = %#v", replayed.ToolCalls)
	}
}

func TestFinalAssistantMessageFromNativeMessage_FallsBackToLegacyFunctionCall(t *testing.T) {
	native := goopenai.ChatCompletionMessage{
		Role:    model.RoleAssistant,
		Content: "",
		FunctionCall: &goopenai.FunctionCall{
			Name:      "lookup_weather",
			Arguments: `{"city":"Shanghai"}`,
		},
	}

	msg, err := finalAssistantMessageFromNativeMessage(native)
	if err != nil {
		t.Fatalf("finalAssistantMessageFromNativeMessage() error = %v", err)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("len(msg.ToolCalls) = %d, want 1", len(msg.ToolCalls))
	}
	if msg.ToolCalls[0].Name != "lookup_weather" || msg.ToolCalls[0].Arguments != `{"city":"Shanghai"}` {
		t.Fatalf("msg.ToolCalls[0] = %#v", msg.ToolCalls[0])
	}
	var replayed goopenai.ChatCompletionMessage
	if err := json.Unmarshal(msg.ProviderState.Payload, &replayed); err != nil {
		t.Fatalf("unmarshal provider state payload: %v", err)
	}
	if replayed.FunctionCall == nil || replayed.FunctionCall.Name != "lookup_weather" {
		t.Fatalf("replayed.FunctionCall = %#v", replayed.FunctionCall)
	}
	if len(replayed.ToolCalls) != 0 {
		t.Fatalf("replayed.ToolCalls = %#v, want empty legacy payload preserved", replayed.ToolCalls)
	}
}

func TestEmitStreamEvent_ReturnsFalseWhenContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if ok := emitStreamEvent(ctx, make(chan model.StreamEvent), model.StreamEvent{Type: model.StreamEventCompleted}); ok {
		t.Fatal("emitStreamEvent() = true, want false when context is canceled")
	}
}
