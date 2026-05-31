package openai_chat

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/types"
)

func TestBuildChatCompletionParamsForwardsPromptCacheOptionsWithoutUser(t *testing.T) {
	params, _, err := buildChatCompletionParams(model.ChatRequest{
		Model:                "gpt-5.5",
		Messages:             []model.Message{{Role: model.RoleUser, Content: "hello"}},
		MaxTokens:            256,
		PromptCacheKey:       " agent-runtime-cache-key ",
		PromptCacheRetention: "24h",
	})
	if err != nil {
		t.Fatalf("buildChatCompletionParams() error = %v", err)
	}

	payload := marshalParamsMap(t, params)
	if payload["prompt_cache_key"] != "agent-runtime-cache-key" {
		t.Fatalf("prompt_cache_key = %#v, want agent-runtime-cache-key", payload["prompt_cache_key"])
	}
	if payload["prompt_cache_retention"] != "24h" {
		t.Fatalf("prompt_cache_retention = %#v, want 24h", payload["prompt_cache_retention"])
	}
	if _, ok := payload["user"]; ok {
		t.Fatalf("user field present = %#v, want omitted", payload["user"])
	}
	if payload["max_completion_tokens"] != float64(256) {
		t.Fatalf("max_completion_tokens = %#v, want 256", payload["max_completion_tokens"])
	}
	streamOptions, ok := payload["stream_options"].(map[string]any)
	if !ok {
		t.Fatalf("stream_options = %#v, want object", payload["stream_options"])
	}
	if streamOptions["include_usage"] != true {
		t.Fatalf("stream_options.include_usage = %#v, want true", streamOptions["include_usage"])
	}
}

func TestBuildChatCompletionParamsMapsToolsAndToolChoice(t *testing.T) {
	params, _, err := buildChatCompletionParams(model.ChatRequest{
		Model:    "gpt-5.5",
		Messages: []model.Message{{Role: model.RoleUser, Content: "check weather"}},
		Tools: []types.Tool{{
			Name:        "lookup_weather",
			Description: "query weather",
			Parameters: types.JSONSchema{
				Type: "object",
				Properties: map[string]types.SchemaProperty{
					"city": {Type: "string", Description: "city name"},
				},
				Required: []string{"city"},
			},
		}},
		ToolChoice: types.ToolChoice{Type: types.ToolForce, Name: "lookup_weather"},
	})
	if err != nil {
		t.Fatalf("buildChatCompletionParams() error = %v", err)
	}

	payload := marshalParamsMap(t, params)
	rawTools, ok := payload["tools"].([]any)
	if !ok || len(rawTools) != 1 {
		t.Fatalf("tools = %#v, want one tool", payload["tools"])
	}
	tool := rawTools[0].(map[string]any)
	if tool["type"] != "function" {
		t.Fatalf("tool.type = %#v, want function", tool["type"])
	}
	function := tool["function"].(map[string]any)
	if function["name"] != "lookup_weather" || function["description"] != "query weather" {
		t.Fatalf("tool function = %#v", function)
	}
	parameters := function["parameters"].(map[string]any)
	if parameters["type"] != "object" {
		t.Fatalf("parameters.type = %#v, want object", parameters["type"])
	}

	choice := payload["tool_choice"].(map[string]any)
	if choice["type"] != "function" {
		t.Fatalf("tool_choice.type = %#v, want function", choice["type"])
	}
	choiceFunction := choice["function"].(map[string]any)
	if choiceFunction["name"] != "lookup_weather" {
		t.Fatalf("tool_choice.function.name = %#v, want lookup_weather", choiceFunction["name"])
	}
}

func TestClientChatStreamSendsPromptCacheKeyAndBuildsFinalMessage(t *testing.T) {
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		writeSSE(t, w, `{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"gpt-5.5","choices":[{"index":0,"delta":{"role":"assistant","content":"hel"}}]}`)
		writeSSE(t, w, `{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"gpt-5.5","choices":[{"index":0,"delta":{"content":"lo"},"finish_reason":"stop"}]}`)
		writeSSE(t, w, `{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"gpt-5.5","choices":[],"usage":{"prompt_tokens":1200,"completion_tokens":2,"total_tokens":1202,"prompt_tokens_details":{"cached_tokens":1024}}}`)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	client := NewOpenAIChatClient("test-key", server.URL+"/v1", time.Minute)
	stream, err := client.ChatStream(context.Background(), model.ChatRequest{
		Model:          "gpt-5.5",
		Messages:       []model.Message{{Role: model.RoleUser, Content: "hello"}},
		PromptCacheKey: "agent-runtime-cache-key",
	})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	defer stream.Close()

	var content string
	var completed model.Message
	for {
		event, err := stream.RecvEvent()
		if err != nil {
			t.Fatalf("RecvEvent() error = %v", err)
		}
		if event.Type == "" {
			break
		}
		if event.Type == model.StreamEventTextDelta {
			content += event.Text
		}
		if event.Type == model.StreamEventCompleted {
			completed = event.Message
		}
	}

	if payload["prompt_cache_key"] != "agent-runtime-cache-key" {
		t.Fatalf("prompt_cache_key = %#v, want agent-runtime-cache-key", payload["prompt_cache_key"])
	}
	if _, ok := payload["user"]; ok {
		t.Fatalf("user field present = %#v, want omitted", payload["user"])
	}
	streamOptions := payload["stream_options"].(map[string]any)
	if streamOptions["include_usage"] != true {
		t.Fatalf("stream_options.include_usage = %#v, want true", streamOptions["include_usage"])
	}
	if content != "hello" {
		t.Fatalf("streamed content = %q, want hello", content)
	}
	stats := stream.Stats()
	if stats.Usage.CachedPromptTokens != 1024 {
		t.Fatalf("CachedPromptTokens = %d, want 1024", stats.Usage.CachedPromptTokens)
	}
	final, err := stream.FinalMessage()
	if err != nil {
		t.Fatalf("FinalMessage() error = %v", err)
	}
	if completed.Content != "hello" || final.Content != "hello" {
		t.Fatalf("final messages = completed:%#v final:%#v, want hello", completed, final)
	}
	if final.ProviderState == nil {
		t.Fatal("final.ProviderState = nil, want provider state")
	}
	if final.ProviderState.Provider != "openai_chat" {
		t.Fatalf("provider = %q, want openai_chat", final.ProviderState.Provider)
	}
	if final.ProviderState.Format != "openai_chat_message.v1" {
		t.Fatalf("format = %q, want openai_chat_message.v1", final.ProviderState.Format)
	}
}

func TestClientChatRejectsHTMLResponseWithoutCompletionChunks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><body>API console</body></html>`))
	}))
	defer server.Close()

	client := NewOpenAIChatClient("test-key", server.URL, time.Minute)
	_, err := client.Chat(context.Background(), model.ChatRequest{
		Model:    "gpt-5.5",
		Messages: []model.Message{{Role: model.RoleUser, Content: "hello"}},
	})
	if err == nil {
		t.Fatal("Chat() error = nil, want malformed stream error")
	}
	if !strings.Contains(err.Error(), "no completion chunks") {
		t.Fatalf("Chat() error = %v, want no completion chunks error", err)
	}
}

func TestClientChatStreamPreservesRefusal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		writeSSE(t, w, `{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1,"model":"gpt-5.5","choices":[{"index":0,"delta":{"role":"assistant","refusal":"I can't help with that."},"finish_reason":"stop"}]}`)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	client := NewOpenAIChatClient("test-key", server.URL+"/v1", time.Minute)
	resp, err := client.Chat(context.Background(), model.ChatRequest{
		Model:    "gpt-5.5",
		Messages: []model.Message{{Role: model.RoleUser, Content: "unsafe request"}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	if resp.Message.Content != "I can't help with that." {
		t.Fatalf("resp.Message.Content = %q, want refusal text", resp.Message.Content)
	}
	if resp.Message.ProviderState == nil {
		t.Fatal("ProviderState = nil, want refusal preserved")
	}
	var state chatMessageState
	if err := json.Unmarshal(resp.Message.ProviderState.Payload, &state); err != nil {
		t.Fatalf("unmarshal provider state: %v", err)
	}
	if state.Refusal != "I can't help with that." {
		t.Fatalf("state.Refusal = %q, want refusal text", state.Refusal)
	}
}

func TestClientChatStreamTimesOutWaitingForFirstEvent(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		select {
		case <-release:
		case <-r.Context().Done():
		case <-time.After(500 * time.Millisecond):
		}
	}))
	defer server.Close()
	defer close(release)

	client := NewOpenAIChatClient("test-key", server.URL+"/v1", 20*time.Millisecond)
	stream, err := client.ChatStream(context.Background(), model.ChatRequest{
		Model:    "gpt-5.5",
		Messages: []model.Message{{Role: model.RoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("ChatStream() error = %v", err)
	}
	defer stream.Close()

	start := time.Now()
	_, err = stream.RecvEvent()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("RecvEvent() error = %v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("RecvEvent() elapsed = %s, want request timeout before server release", elapsed)
	}
}

func TestBuildOpenAIChatMessagesReplaysProviderStateWithoutReasoningContent(t *testing.T) {
	state := &model.ProviderState{
		Provider: "openai_chat",
		Format:   "openai_chat_message.v1",
		Version:  "v1",
		Payload:  json.RawMessage(`{"role":"assistant","content":"state answer","reasoning_content":"state reasoning","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup_weather","arguments":"{\"city\":\"Shanghai\"}"}}]}`),
	}

	params, _, err := buildChatCompletionParams(model.ChatRequest{
		Model: "gpt-5.5",
		Messages: []model.Message{{
			Role:          model.RoleAssistant,
			Content:       "normalized answer should not win",
			Reasoning:     "normalized reasoning should not win",
			ToolCalls:     []types.ToolCall{{ID: "call_normalized", Name: "wrong", Arguments: `{}`}},
			ProviderState: state,
		}},
	})
	if err != nil {
		t.Fatalf("buildChatCompletionParams() error = %v", err)
	}

	payload := marshalParamsMap(t, params)
	messages := payload["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(messages))
	}
	message := messages[0].(map[string]any)
	if message["content"] != "state answer" {
		t.Fatalf("message.content = %#v, want state answer", message["content"])
	}
	if _, ok := message["reasoning_content"]; ok {
		t.Fatalf("message.reasoning_content = %#v, want omitted for official Chat Completions", message["reasoning_content"])
	}
	toolCalls := message["tool_calls"].([]any)
	call := toolCalls[0].(map[string]any)
	if call["id"] != "call_1" {
		t.Fatalf("tool call id = %#v, want call_1", call["id"])
	}
	function := call["function"].(map[string]any)
	if function["name"] != "lookup_weather" || function["arguments"] != `{"city":"Shanghai"}` {
		t.Fatalf("function = %#v", function)
	}
}

func marshalParamsMap(t *testing.T, params any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal params: %v\nraw=%s", err, raw)
	}
	return payload
}

func writeSSE(t *testing.T, w http.ResponseWriter, payload string) {
	t.Helper()
	if _, err := w.Write([]byte("data: " + payload + "\n\n")); err != nil {
		t.Fatalf("write SSE: %v", err)
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}
