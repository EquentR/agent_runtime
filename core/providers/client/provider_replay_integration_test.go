package client_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	openaiclient "github.com/EquentR/agent_runtime/core/providers/client/openai_completions"
	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/types"
)

func TestOpenAICompletionsChat_SameProviderReplayPrefersProviderState(t *testing.T) {
	var requestBody map[string]any
	server := newOpenAICompletionsCaptureServer(t, &requestBody)
	defer server.Close()

	client := openaiclient.NewOpenAiCompletionsClient(server.URL+"/v1", "test-key", time.Minute)
	_, err := client.Chat(context.Background(), model.ChatRequest{
		Model: "gpt-4o-mini",
		Messages: []model.Message{{
			Role: model.RoleAssistant,
			ProviderState: &model.ProviderState{
				Provider: "openai_completions",
				Format:   "openai_chat_message.v1",
				Version:  "v1",
				Payload: []byte(`{
					"role":"assistant",
					"content":"provider text",
					"reasoning_content":"provider reasoning",
					"tool_calls":[{"id":"call_state","type":"function","function":{"name":"lookup_weather","arguments":"{\"city\":\"Beijing\"}"}}]
				}`),
			},
			Content:   "normalized text",
			Reasoning: "normalized reasoning",
			ToolCalls: []types.ToolCall{{ID: "call_norm", Name: "normalized_tool", Arguments: `{"city":"Shanghai"}`}},
		}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	message := requireSingleRequestMessage(t, requestBody)
	if got, _ := message["content"].(string); got != "provider text" {
		t.Fatalf("request content = %q, want provider text", got)
	}
	if got, _ := message["reasoning_content"].(string); got != "provider reasoning" {
		t.Fatalf("request reasoning_content = %q, want provider reasoning", got)
	}
	toolCalls := requireToolCalls(t, message)
	if len(toolCalls) != 1 {
		t.Fatalf("len(tool_calls) = %d, want 1", len(toolCalls))
	}
	fn, _ := toolCalls[0]["function"].(map[string]any)
	if got, _ := fn["name"].(string); got != "lookup_weather" {
		t.Fatalf("tool call name = %q, want lookup_weather", got)
	}
	if got, _ := fn["arguments"].(string); got != `{"city":"Beijing"}` {
		t.Fatalf("tool call arguments = %q, want provider args", got)
	}
}

func TestOpenAICompletionsChat_MissingProviderStateFallsBackToNormalizedFields(t *testing.T) {
	var requestBody map[string]any
	server := newOpenAICompletionsCaptureServer(t, &requestBody)
	defer server.Close()

	client := openaiclient.NewOpenAiCompletionsClient(server.URL+"/v1", "test-key", time.Minute)
	_, err := client.Chat(context.Background(), model.ChatRequest{
		Model: "gpt-4o-mini",
		Messages: []model.Message{{
			Role:      model.RoleAssistant,
			Content:   "normalized text",
			Reasoning: "normalized reasoning",
			ToolCalls: []types.ToolCall{{ID: "call_norm", Name: "lookup_weather", Arguments: `{"city":"Shanghai"}`}},
		}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	message := requireSingleRequestMessage(t, requestBody)
	if got, _ := message["content"].(string); got != "normalized text" {
		t.Fatalf("request content = %q, want normalized text", got)
	}
	if got, _ := message["reasoning_content"].(string); got != "normalized reasoning" {
		t.Fatalf("request reasoning_content = %q, want normalized reasoning", got)
	}
	toolCalls := requireToolCalls(t, message)
	if len(toolCalls) != 1 {
		t.Fatalf("len(tool_calls) = %d, want 1", len(toolCalls))
	}
	fn, _ := toolCalls[0]["function"].(map[string]any)
	if got, _ := fn["arguments"].(string); got != `{"city":"Shanghai"}` {
		t.Fatalf("tool call arguments = %q, want normalized args", got)
	}
}

func TestOpenAICompletionsChat_CrossProviderReplayIgnoresForeignStateAndFallsBack(t *testing.T) {
	var requestBody map[string]any
	server := newOpenAICompletionsCaptureServer(t, &requestBody)
	defer server.Close()

	client := openaiclient.NewOpenAiCompletionsClient(server.URL+"/v1", "test-key", time.Minute)
	resp, err := client.Chat(context.Background(), model.ChatRequest{
		Model: "gpt-4o-mini",
		Messages: []model.Message{{
			Role: model.RoleAssistant,
			ProviderState: &model.ProviderState{
				Provider: "openai_responses",
				Format:   "openai_response_output_items.v1",
				Version:  "v1",
				Payload:  []byte(`[{"type":"message","content":[{"type":"output_text","text":"foreign text"}]}]`),
			},
			Content:   "normalized text",
			Reasoning: "normalized reasoning",
			ToolCalls: []types.ToolCall{{ID: "call_norm", Name: "lookup_weather", Arguments: `{"city":"Shanghai"}`}},
		}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Content != "ok" || resp.Message.Content != "ok" {
		t.Fatalf("response content/message = %#v", resp)
	}

	message := requireSingleRequestMessage(t, requestBody)
	if got, _ := message["content"].(string); got != "normalized text" {
		t.Fatalf("request content = %q, want normalized text", got)
	}
	if got, _ := message["reasoning_content"].(string); got != "normalized reasoning" {
		t.Fatalf("request reasoning_content = %q, want normalized reasoning", got)
	}
	toolCalls := requireToolCalls(t, message)
	if len(toolCalls) != 1 {
		t.Fatalf("len(tool_calls) = %d, want 1", len(toolCalls))
	}
	fn, _ := toolCalls[0]["function"].(map[string]any)
	if got, _ := fn["name"].(string); got != "lookup_weather" {
		t.Fatalf("tool call name = %q, want lookup_weather", got)
	}
}

func TestOpenAICompletionsChat_RoundTripsClientProducedProviderState(t *testing.T) {
	var (
		mu       sync.Mutex
		requests []map[string]any
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll(r.Body) error = %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("json.Unmarshal(request body) error = %v, body=%s", err, string(body))
		}
		mu.Lock()
		requests = append(requests, payload)
		callIndex := len(requests)
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		if callIndex == 1 {
			_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4o-mini\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"reasoning_content\":\"provider reasoning\"}}]}\n\n"))
			_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4o-mini\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"provider text\",\"tool_calls\":[{\"index\":0,\"id\":\"call_state\",\"type\":\"function\",\"function\":{\"name\":\"lookup_weather\",\"arguments\":\"{\\\"city\\\":\\\"Beijing\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n"))
		} else {
			_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-2\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4o-mini\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\n"))
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	client := openaiclient.NewOpenAiCompletionsClient(server.URL+"/v1", "test-key", time.Minute)
	first, err := client.Chat(context.Background(), model.ChatRequest{
		Model:    "gpt-4o-mini",
		Messages: []model.Message{{Role: model.RoleUser, Content: "first"}},
	})
	if err != nil {
		t.Fatalf("first Chat() error = %v", err)
	}
	if first.Message.ProviderState == nil {
		t.Fatal("first.Message.ProviderState = nil, want provider state from streamed response")
	}

	_, err = client.Chat(context.Background(), model.ChatRequest{
		Model: "gpt-4o-mini",
		Messages: []model.Message{{
			Role:          model.RoleAssistant,
			Content:       "mutated normalized text",
			Reasoning:     "mutated normalized reasoning",
			ToolCalls:     []types.ToolCall{{ID: "call_norm", Name: "normalized_tool", Arguments: `{"city":"Shanghai"}`}},
			ProviderState: first.Message.ProviderState,
		}},
	})
	if err != nil {
		t.Fatalf("second Chat() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("len(requests) = %d, want 2", len(requests))
	}
	message := requireSingleRequestMessage(t, requests[1])
	if got, _ := message["content"].(string); got != "provider text" {
		t.Fatalf("second request content = %q, want replayed provider text", got)
	}
	if got, _ := message["reasoning_content"].(string); got != "provider reasoning" {
		t.Fatalf("second request reasoning_content = %q, want replayed provider reasoning", got)
	}
	toolCalls := requireToolCalls(t, message)
	if len(toolCalls) != 1 {
		t.Fatalf("len(second request tool_calls) = %d, want 1", len(toolCalls))
	}
	fn, _ := toolCalls[0]["function"].(map[string]any)
	if got, _ := fn["name"].(string); got != "lookup_weather" {
		t.Fatalf("second request tool call name = %q, want lookup_weather", got)
	}
}

func newOpenAICompletionsCaptureServer(t *testing.T, requestBody *map[string]any) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll(r.Body) error = %v", err)
		}
		if err := json.Unmarshal(body, requestBody); err != nil {
			t.Fatalf("json.Unmarshal(request body) error = %v, body=%s", err, string(body))
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-4o-mini\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
}

func requireSingleRequestMessage(t *testing.T, requestBody map[string]any) map[string]any {
	t.Helper()

	messages, ok := requestBody["messages"].([]any)
	if !ok {
		t.Fatalf("messages type = %T, want []any", requestBody["messages"])
	}
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(messages))
	}
	message, ok := messages[0].(map[string]any)
	if !ok {
		t.Fatalf("message type = %T, want map[string]any", messages[0])
	}
	return message
}

func requireToolCalls(t *testing.T, message map[string]any) []map[string]any {
	t.Helper()

	raw, ok := message["tool_calls"].([]any)
	if !ok {
		t.Fatalf("tool_calls type = %T, want []any", message["tool_calls"])
	}
	toolCalls := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		toolCall, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("tool_call type = %T, want map[string]any", item)
		}
		toolCalls = append(toolCalls, toolCall)
	}
	return toolCalls
}
