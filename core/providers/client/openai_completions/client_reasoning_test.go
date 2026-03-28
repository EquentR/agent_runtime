package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
		stats: &model.StreamStats{Usage: model.TokenUsage{TotalTokens: 7}},
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

func TestChatResponseFromStream_UsesFinalMessageProjection(t *testing.T) {
	state := &model.ProviderState{
		Provider: "openai_completions",
		Format:   "openai_chat_message.v1",
		Version:  "v1",
		Payload:  json.RawMessage(`{"role":"assistant","content":"hello","reasoning_content":"plan"}`),
	}

	resp, err := chatResponseFromStream(time.Now(), &fakeChatStream{
		ctx:  context.Background(),
		recv: []string{"hel", "lo"},
		final: model.Message{
			Role:          model.RoleAssistant,
			Content:       "hello",
			Reasoning:     "plan",
			ToolCalls:     []types.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: `{}`}},
			ProviderState: state,
		},
		stats: &model.StreamStats{Usage: model.TokenUsage{TotalTokens: 7}},
	})
	if err != nil {
		t.Fatalf("chatResponseFromStream() error = %v", err)
	}
	if resp.Message.ProviderState == nil {
		t.Fatal("resp.Message.ProviderState = nil, want provider state")
	}
	if resp.Message.ProviderState.Provider != "openai_completions" {
		t.Fatalf("resp.Message.ProviderState.Provider = %q, want %q", resp.Message.ProviderState.Provider, "openai_completions")
	}
	if resp.Content != "hello" || resp.Reasoning != "plan" {
		t.Fatalf("mirrored response fields = %#v", resp)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].ID != "call_1" {
		t.Fatalf("mirrored tool calls = %#v", resp.ToolCalls)
	}
	if string(resp.Message.ProviderState.Payload) != string(state.Payload) {
		t.Fatalf("provider state payload = %s, want %s", string(resp.Message.ProviderState.Payload), string(state.Payload))
	}
}

func TestClientChat_PreservesReasoningContentFromStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4o-mini\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"reasoning_content\":\"Need the weather tool.\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4o-mini\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"lookup_weather\",\"arguments\":\"{\\\"city\\\":\\\"Shanghai\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	client := NewOpenAiCompletionsClient(server.URL+"/v1", "test-key")
	resp, err := client.Chat(context.Background(), model.ChatRequest{
		Model: "gpt-4o-mini",
		Messages: []model.Message{{
			Role:    model.RoleUser,
			Content: "What is the weather in Shanghai?",
		}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Reasoning != "Need the weather tool." {
		t.Fatalf("resp.Reasoning = %q, want %q", resp.Reasoning, "Need the weather tool.")
	}
	if resp.Message.Role != model.RoleAssistant {
		t.Fatalf("resp.Message.Role = %q, want %q", resp.Message.Role, model.RoleAssistant)
	}
	if resp.Message.Reasoning != "Need the weather tool." {
		t.Fatalf("resp.Message.Reasoning = %q, want %q", resp.Message.Reasoning, "Need the weather tool.")
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("len(resp.ToolCalls) = %d, want 1", len(resp.ToolCalls))
	}
	if len(resp.Message.ToolCalls) != 1 {
		t.Fatalf("len(resp.Message.ToolCalls) = %d, want 1", len(resp.Message.ToolCalls))
	}
	if resp.ToolCalls[0].ID != "call_1" || resp.ToolCalls[0].Name != "lookup_weather" {
		t.Fatalf("tool call = %#v, want call_1/lookup_weather", resp.ToolCalls[0])
	}
	if resp.Message.ToolCalls[0].ID != "call_1" || resp.Message.ToolCalls[0].Name != "lookup_weather" {
		t.Fatalf("message tool call = %#v, want call_1/lookup_weather", resp.Message.ToolCalls[0])
	}
	if resp.ToolCalls[0].Arguments != `{"city":"Shanghai"}` {
		t.Fatalf("tool call arguments = %q, want %q", resp.ToolCalls[0].Arguments, `{"city":"Shanghai"}`)
	}
}

func TestClientChat_StreamProviderStateRoundTripsIntoReplay(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4o-mini\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"reasoning_content\":\"Need the weather tool.\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4o-mini\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"lookup_weather\",\"arguments\":\"{\\\"city\\\":\"}}]}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4o-mini\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Shanghai weather is 23C.\",\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"Shanghai\\\"}\"}}]}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4o-mini\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	client := NewOpenAiCompletionsClient(server.URL+"/v1", "test-key")
	resp, err := client.Chat(context.Background(), model.ChatRequest{
		Model: "gpt-4o-mini",
		Messages: []model.Message{{
			Role:    model.RoleUser,
			Content: "What is the weather in Shanghai?",
		}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Message.ProviderState == nil {
		t.Fatal("resp.Message.ProviderState = nil, want provider state")
	}

	replayed, _, err := buildOpenAIMessages([]model.Message{{
		Role:          model.RoleAssistant,
		Content:       resp.Message.Content,
		Reasoning:     resp.Message.Reasoning,
		ToolCalls:     resp.Message.ToolCalls,
		ProviderState: resp.Message.ProviderState,
	}})
	if err != nil {
		t.Fatalf("buildOpenAIMessages() error = %v", err)
	}
	if len(replayed) != 1 {
		t.Fatalf("len(replayed) = %d, want 1", len(replayed))
	}
	if replayed[0].ReasoningContent != "Need the weather tool." {
		t.Fatalf("replayed[0].ReasoningContent = %q, want %q", replayed[0].ReasoningContent, "Need the weather tool.")
	}
	if replayed[0].Content != "Shanghai weather is 23C." {
		t.Fatalf("replayed[0].Content = %q, want %q", replayed[0].Content, "Shanghai weather is 23C.")
	}
	if len(replayed[0].ToolCalls) != 1 {
		t.Fatalf("len(replayed[0].ToolCalls) = %d, want 1", len(replayed[0].ToolCalls))
	}
	if replayed[0].ToolCalls[0].ID != "call_1" || replayed[0].ToolCalls[0].Function.Name != "lookup_weather" {
		t.Fatalf("replayed tool call = %#v", replayed[0].ToolCalls[0])
	}
	if replayed[0].ToolCalls[0].Function.Arguments != `{"city":"Shanghai"}` {
		t.Fatalf("replayed tool call arguments = %q, want %q", replayed[0].ToolCalls[0].Function.Arguments, `{"city":"Shanghai"}`)
	}
	if resp.Message.ProviderState.Version != "v1" {
		t.Fatalf("provider state version = %q, want %q", resp.Message.ProviderState.Version, "v1")
	}
	if !strings.Contains(string(resp.Message.ProviderState.Payload), `"tool_calls"`) {
		t.Fatalf("provider state payload = %s, want tool_calls preserved", string(resp.Message.ProviderState.Payload))
	}
}

func TestClientChatStream_ToolCallDeltaKeepsIdentityAcrossArgumentChunks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4o-mini\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"lookup_weather\",\"arguments\":\"{\\\"city\\\":\"}}]}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4o-mini\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"Shanghai\\\"}\"}}]}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4o-mini\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":3,\"total_tokens\":13}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	client := NewOpenAiCompletionsClient(server.URL+"/v1", "test-key")
	stream, err := client.ChatStream(context.Background(), model.ChatRequest{
		Model: "gpt-4o-mini",
		Messages: []model.Message{{
			Role:    model.RoleUser,
			Content: "What is the weather in Shanghai?",
		}},
	})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	defer stream.Close()

	var toolEvents []model.StreamEvent
	for {
		event, err := stream.RecvEvent()
		if err != nil {
			t.Fatalf("RecvEvent() error = %v", err)
		}
		if event.Type == "" {
			break
		}
		if event.Type == model.StreamEventToolCallDelta {
			toolEvents = append(toolEvents, event)
		}
	}

	if len(toolEvents) != 2 {
		t.Fatalf("len(toolEvents) = %d, want 2", len(toolEvents))
	}
	if toolEvents[0].ToolCall.ID != "call_1" || toolEvents[0].ToolCall.Name != "lookup_weather" {
		t.Fatalf("first tool delta = %#v, want stable id/name", toolEvents[0].ToolCall)
	}
	if toolEvents[1].ToolCall.ID != "call_1" || toolEvents[1].ToolCall.Name != "lookup_weather" {
		t.Fatalf("second tool delta = %#v, want stable id/name", toolEvents[1].ToolCall)
	}
	if toolEvents[1].ToolCall.Arguments != `{"city":"Shanghai"}` {
		t.Fatalf("second tool delta arguments = %q, want %q", toolEvents[1].ToolCall.Arguments, `{"city":"Shanghai"}`)
	}
}
