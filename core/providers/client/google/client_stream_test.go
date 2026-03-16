package google

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/types"
)

type fakeChatStream struct {
	recv  []string
	final model.Message
	stats *model.StreamStats
	idx   int
	ctx   context.Context
}

func (f *fakeChatStream) Recv() (string, error) {
	if f.idx >= len(f.recv) {
		return "", nil
	}
	chunk := f.recv[f.idx]
	f.idx++
	return chunk, nil
}

func (f *fakeChatStream) RecvEvent() (model.StreamEvent, error) { return model.StreamEvent{}, nil }
func (f *fakeChatStream) FinalMessage() (model.Message, error)  { return f.final, nil }
func (f *fakeChatStream) Close() error                          { return nil }
func (f *fakeChatStream) Context() context.Context              { return f.ctx }
func (f *fakeChatStream) Stats() *model.StreamStats             { return f.stats }
func (f *fakeChatStream) ToolCalls() []types.ToolCall           { return nil }
func (f *fakeChatStream) ResponseType() model.StreamResponseType {
	return model.StreamResponseText
}
func (f *fakeChatStream) FinishReason() string { return "stop" }
func (f *fakeChatStream) Reasoning() string    { return "" }

func TestChatResponseFromStream_UsesFinalMessage(t *testing.T) {
	resp, err := chatResponseFromStream(time.Now(), &fakeChatStream{
		ctx:  context.Background(),
		recv: []string{"hel", "lo"},
		final: model.Message{
			Role:      model.RoleAssistant,
			Content:   "hello",
			Reasoning: "plan",
			ToolCalls: []types.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: `{}`}},
		},
		stats: &model.StreamStats{Usage: model.TokenUsage{TotalTokens: 5}},
	})
	if err != nil {
		t.Fatalf("chatResponseFromStream() error = %v", err)
	}
	if resp.Message.Content != "hello" || resp.Content != "hello" {
		t.Fatalf("response content/message = %#v", resp)
	}
	if resp.Message.Reasoning != "plan" || resp.Reasoning != "plan" {
		t.Fatalf("response reasoning/message = %#v", resp)
	}
	if len(resp.Message.ToolCalls) != 1 || len(resp.ToolCalls) != 1 {
		t.Fatalf("response tool calls = %#v", resp)
	}
}

func TestChatResponseFromStream_MirrorsProviderStateFromFinalMessage(t *testing.T) {
	state := &model.ProviderState{
		Provider: "google_genai",
		Format:   "google_genai_content.v1",
		Version:  "v1",
		Payload:  json.RawMessage(`{"role":"model","parts":[{"text":"plan","thought":true},{"text":"hello"}]}`),
	}

	resp, err := chatResponseFromStream(time.Now(), &fakeChatStream{
		ctx:  context.Background(),
		recv: []string{"he", "llo"},
		final: model.Message{
			Role:          model.RoleAssistant,
			Content:       "hello",
			Reasoning:     "plan",
			ProviderState: state,
		},
		stats: &model.StreamStats{Usage: model.TokenUsage{TotalTokens: 2}},
	})
	if err != nil {
		t.Fatalf("chatResponseFromStream() error = %v", err)
	}
	if resp.Message.ProviderState == nil {
		t.Fatal("resp.Message.ProviderState = nil, want provider state")
	}
	if resp.Message.ProviderState.Provider != "google_genai" {
		t.Fatalf("provider = %q, want google_genai", resp.Message.ProviderState.Provider)
	}
	if resp.Content != "hello" || resp.Reasoning != "plan" {
		t.Fatalf("flattened fields not mirrored from Message: %#v", resp)
	}
	resp.Message.ProviderState.Payload[0] = 'x'
	if string(state.Payload) != `{"role":"model","parts":[{"text":"plan","thought":true},{"text":"hello"}]}` {
		t.Fatal("chatResponseFromStream should deep-copy final message provider state")
	}
}
