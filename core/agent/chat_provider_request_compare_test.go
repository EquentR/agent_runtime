package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	coreprompt "github.com/EquentR/agent_runtime/core/prompt"
	openaichat "github.com/EquentR/agent_runtime/core/providers/client/openai_chat"
	openaicompletions "github.com/EquentR/agent_runtime/core/providers/client/openai_completions"
	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/runtimeprompt"
)

func TestRunnerOfficialAndCompatibleChatRequestsKeepToolOutputsAdjacent(t *testing.T) {
	for _, tc := range []struct {
		name      string
		newClient func(baseURL string) model.LlmClient
	}{
		{
			name: "openai_chat",
			newClient: func(baseURL string) model.LlmClient {
				return openaichat.NewOpenAIChatClient("test-key", baseURL, time.Minute)
			},
		},
		{
			name: "openai_completions",
			newClient: func(baseURL string) model.LlmClient {
				return openaicompletions.NewOpenAiCompletionsClient(baseURL, "test-key", time.Minute)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests, server := newChatCompletionCaptureServer(t)
			defer server.Close()

			registry := newTestRegistry(t, map[string]func(context.Context, map[string]interface{}) (string, error){
				"lookup_weather": func(context.Context, map[string]interface{}) (string, error) {
					return "sunny", nil
				},
			})
			runner, err := NewRunner(tc.newClient(server.URL+"/v1"), registry, Options{
				Model:                "gpt-5.5",
				RuntimePromptBuilder: runtimeprompt.NewBuilder(nil),
				ResolvedPrompt: &coreprompt.ResolvedPrompt{Segments: []coreprompt.ResolvedPromptSegment{{
					Phase:   runtimeprompt.PhaseToolResult,
					Content: "Tool-result prompt",
				}}},
				Metadata: map[string]string{
					"conversation_id": "conv_chat_compare",
					"provider_id":     tc.name,
					"model_id":        "gpt-5.5",
				},
			})
			if err != nil {
				t.Fatalf("NewRunner() error = %v", err)
			}

			result, err := runner.Run(context.Background(), RunInput{
				Messages: []model.Message{{Role: model.RoleUser, Content: "weather?"}},
				Tools:    registry.List(),
			})
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if result.FinalMessage.Content != "done" {
				t.Fatalf("final message = %q, want done", result.FinalMessage.Content)
			}

			captured := requests.all()
			if len(captured) != 2 {
				t.Fatalf("captured request count = %d, want 2", len(captured))
			}
			assertPromptCachePayload(t, captured[0], captured[1])
			assertToolOutputsImmediatelyFollowToolCalls(t, captured[1])
			assertToolResultPromptAfterToolOutputs(t, captured[1])
		})
	}
}

type capturedChatCompletionRequests struct {
	mu       sync.Mutex
	payloads []map[string]any
}

func (r *capturedChatCompletionRequests) append(payload map[string]any) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.payloads = append(r.payloads, payload)
	return len(r.payloads)
}

func (r *capturedChatCompletionRequests) all() []map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]map[string]any, len(r.payloads))
	copy(out, r.payloads)
	return out
}

func newChatCompletionCaptureServer(t *testing.T) (*capturedChatCompletionRequests, *httptest.Server) {
	t.Helper()

	requests := &capturedChatCompletionRequests{}
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
		callIndex := requests.append(payload)

		w.Header().Set("Content-Type", "text/event-stream")
		if callIndex == 1 {
			_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-5.5\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"lookup_weather\",\"arguments\":\"{\\\"city\\\":\\\"Shanghai\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n"))
		} else {
			_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-2\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"gpt-5.5\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\n"))
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	return requests, server
}

func assertPromptCachePayload(t *testing.T, first map[string]any, second map[string]any) {
	t.Helper()

	firstKey, _ := first["prompt_cache_key"].(string)
	secondKey, _ := second["prompt_cache_key"].(string)
	if strings.TrimSpace(firstKey) == "" {
		t.Fatalf("first prompt_cache_key = %#v, want non-empty", first["prompt_cache_key"])
	}
	if firstKey != secondKey {
		t.Fatalf("prompt_cache_key mismatch: first=%q second=%q", firstKey, secondKey)
	}
	if first["prompt_cache_retention"] != "24h" || second["prompt_cache_retention"] != "24h" {
		t.Fatalf("prompt_cache_retention = %#v/%#v, want 24h", first["prompt_cache_retention"], second["prompt_cache_retention"])
	}
}

func assertToolOutputsImmediatelyFollowToolCalls(t *testing.T, payload map[string]any) {
	t.Helper()

	messages := chatRequestMessages(t, payload)
	assistantIndex := -1
	var toolCallIDs []string
	for i, message := range messages {
		rawToolCalls, ok := message["tool_calls"].([]any)
		if !ok || len(rawToolCalls) == 0 {
			continue
		}
		assistantIndex = i
		for _, rawToolCall := range rawToolCalls {
			toolCall, ok := rawToolCall.(map[string]any)
			if !ok {
				t.Fatalf("tool_call type = %T, want object", rawToolCall)
			}
			id, _ := toolCall["id"].(string)
			toolCallIDs = append(toolCallIDs, id)
		}
		break
	}
	if assistantIndex < 0 {
		t.Fatalf("messages = %#v, want assistant message with tool_calls", messages)
	}
	for offset, id := range toolCallIDs {
		messageIndex := assistantIndex + 1 + offset
		if messageIndex >= len(messages) {
			t.Fatalf("missing tool response for tool_call_id %q", id)
		}
		message := messages[messageIndex]
		if message["role"] != model.RoleTool || message["tool_call_id"] != id {
			t.Fatalf("message after tool_calls at index %d = %#v, want tool response for %q", messageIndex, message, id)
		}
	}
}

func assertToolResultPromptAfterToolOutputs(t *testing.T, payload map[string]any) {
	t.Helper()

	messages := chatRequestMessages(t, payload)
	for i, message := range messages {
		if message["role"] == model.RoleSystem && message["content"] == "Tool-result prompt" {
			if i == 0 || messages[i-1]["role"] != model.RoleTool {
				t.Fatalf("tool-result prompt index=%d previous=%#v, want previous message to be tool output", i, messages[i-1])
			}
			return
		}
	}
	t.Fatalf("messages = %#v, want tool-result prompt", messages)
}

func chatRequestMessages(t *testing.T, payload map[string]any) []map[string]any {
	t.Helper()

	rawMessages, ok := payload["messages"].([]any)
	if !ok {
		t.Fatalf("messages type = %T, want array", payload["messages"])
	}
	messages := make([]map[string]any, 0, len(rawMessages))
	for _, rawMessage := range rawMessages {
		message, ok := rawMessage.(map[string]any)
		if !ok {
			t.Fatalf("message type = %T, want object", rawMessage)
		}
		messages = append(messages, message)
	}
	return messages
}
