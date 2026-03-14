package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	model "github.com/EquentR/agent_runtime/core/providers/types"
)

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
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("len(resp.ToolCalls) = %d, want 1", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].ID != "call_1" || resp.ToolCalls[0].Name != "lookup_weather" {
		t.Fatalf("tool call = %#v, want call_1/lookup_weather", resp.ToolCalls[0])
	}
	if resp.ToolCalls[0].Arguments != `{"city":"Shanghai"}` {
		t.Fatalf("tool call arguments = %q, want %q", resp.ToolCalls[0].Arguments, `{"city":"Shanghai"}`)
	}
}
