package google

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/types"
	genai "google.golang.org/genai"
)

func TestGenAIStreamRecv_ReturnsStreamErrorWhenChannelClosed(t *testing.T) {
	ch := make(chan string)
	close(ch)

	s := &genAIStream{
		ctx: context.Background(),
		events: func() <-chan model.StreamEvent {
			events := make(chan model.StreamEvent)
			close(events)
			return events
		}(),
		stats: &model.StreamStats{},
	}
	streamErr := errors.New("stream failed")
	s.setStreamError(streamErr)

	_, err := s.Recv()
	if !errors.Is(err, streamErr) {
		t.Fatalf("Recv() error = %v, want %v", err, streamErr)
	}
}

func TestGenAIStreamRecv_FiltersTextDeltaEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan model.StreamEvent, 3)
	events <- model.StreamEvent{Type: model.StreamEventReasoningDelta, Reasoning: "plan"}
	events <- model.StreamEvent{Type: model.StreamEventTextDelta, Text: "hello"}
	close(events)

	s := &genAIStream{
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

func TestGenAIStreamFinalMessage_ReturnsErrorOnCanceledStream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := &genAIStream{
		ctx:    ctx,
		cancel: func() {},
		stats:  &model.StreamStats{},
	}

	_, err := s.FinalMessage()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("FinalMessage() error = %v, want %v", err, context.Canceled)
	}
}

func TestEmitStreamEvent_ReturnsFalseWhenContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if ok := emitStreamEvent(ctx, make(chan model.StreamEvent), model.StreamEvent{Type: model.StreamEventCompleted}); ok {
		t.Fatal("emitStreamEvent() = true, want false when context is canceled")
	}
}

func TestGenAIStreamFinalMessage_PreservesProviderState(t *testing.T) {
	s := &genAIStream{
		ctx:   context.Background(),
		stats: &model.StreamStats{},
	}
	msg := model.Message{
		Role:      model.RoleAssistant,
		Content:   "hello",
		Reasoning: "plan",
		ToolCalls: []types.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: `{"city":"Beijing"}`}},
		ProviderState: &model.ProviderState{
			Provider: "google_genai",
			Format:   "google_genai_content.v1",
			Version:  "v1",
			Payload:  json.RawMessage(`{"role":"model","parts":[{"text":"plan","thought":true},{"functionCall":{"id":"call_1","name":"lookup_weather","args":{"city":"Beijing"}}},{"text":"hello"}]}`),
		},
	}
	s.setFinalMessage(msg)

	final, err := s.FinalMessage()
	if err != nil {
		t.Fatalf("FinalMessage() error = %v", err)
	}
	if final.ProviderState == nil {
		t.Fatal("final.ProviderState = nil, want provider state")
	}
	if string(final.ProviderState.Payload) != string(msg.ProviderState.Payload) {
		t.Fatalf("provider state payload = %s, want %s", string(final.ProviderState.Payload), string(msg.ProviderState.Payload))
	}
	final.ProviderState.Payload[0] = 'x'
	if string(s.final().ProviderState.Payload) != string(msg.ProviderState.Payload) {
		t.Fatal("mutating final message provider state should not mutate stored stream final message")
	}
}

func TestStreamFinalMessageFromObservedContent_PreservesOriginalParts(t *testing.T) {
	observed := &genai.Content{
		Role: genai.RoleModel,
		Parts: []*genai.Part{
			{Text: "plan", Thought: true, ThoughtSignature: []byte{1, 2, 3}},
			{FunctionCall: &genai.FunctionCall{ID: "call_1", Name: "lookup_weather", Args: map[string]any{"city": "Beijing"}}, ThoughtSignature: []byte{4, 5, 6}},
			{Text: "<think>plan</think>Final answer"},
			{ExecutableCode: &genai.ExecutableCode{Language: "python", Code: "print(1)"}},
		},
	}

	final, err := finalAssistantMessageFromObservedContent(observed)
	if err != nil {
		t.Fatalf("finalAssistantMessageFromObservedContent() error = %v", err)
	}
	if final.Content != "Final answer" {
		t.Fatalf("final.Content = %q, want Final answer", final.Content)
	}
	if final.Reasoning != "plan" {
		t.Fatalf("final.Reasoning = %q, want plan", final.Reasoning)
	}
	if len(final.ToolCalls) != 1 || final.ToolCalls[0].Name != "lookup_weather" {
		t.Fatalf("final.ToolCalls = %#v", final.ToolCalls)
	}
	if final.ProviderState == nil {
		t.Fatal("final.ProviderState = nil, want provider state")
	}

	replayed, ok, err := contentFromProviderState(final.ProviderState)
	if err != nil {
		t.Fatalf("contentFromProviderState() error = %v", err)
	}
	if !ok {
		t.Fatal("contentFromProviderState() ok = false, want true")
	}
	if len(replayed.Parts) != 4 {
		t.Fatalf("len(replayed.Parts) = %d, want 4", len(replayed.Parts))
	}
	if replayed.Parts[2].Text != "<think>plan</think>Final answer" {
		t.Fatalf("replayed text part = %q, want original streamed text", replayed.Parts[2].Text)
	}
	if replayed.Parts[3].ExecutableCode == nil || replayed.Parts[3].ExecutableCode.Code != "print(1)" {
		t.Fatalf("replayed executable code part = %#v", replayed.Parts[3])
	}
	for _, part := range replayed.Parts {
		if part != nil && part.Text == "Final answer" {
			t.Fatal("provider state should preserve original streamed parts, not reconstructed flattened text parts")
		}
	}
}

func TestObservedStreamPartsAccumulator_PreservesTextAndExecutableCode(t *testing.T) {
	finalContent := &genai.Content{Role: genai.RoleModel}
	toolCallAccumulator := newStreamToolCallAccumulator()
	parts := []*genai.Part{
		{Text: "plain answer"},
		{ExecutableCode: &genai.ExecutableCode{Language: "python", Code: "print(1)"}},
	}

	for _, part := range parts {
		accumulateObservedStreamPart(finalContent, toolCallAccumulator, part)
	}

	if len(finalContent.Parts) != 2 {
		t.Fatalf("len(finalContent.Parts) = %d, want 2", len(finalContent.Parts))
	}
	if finalContent.Parts[0].Text != "plain answer" {
		t.Fatalf("finalContent.Parts[0].Text = %q, want plain answer", finalContent.Parts[0].Text)
	}
	if finalContent.Parts[1].ExecutableCode == nil || finalContent.Parts[1].ExecutableCode.Code != "print(1)" {
		t.Fatalf("finalContent.Parts[1] = %#v, want executable code part preserved", finalContent.Parts[1])
	}
}
