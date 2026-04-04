package openai_responses

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	model "github.com/EquentR/agent_runtime/core/providers/types"
)

func TestOpenAiResponsesClient_HonorsConfiguredRequestTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			http.NotFound(w, r)
			return
		}
		time.Sleep(150 * time.Millisecond)
	}))
	defer server.Close()

	client := NewOpenAiResponsesClient("test-key", server.URL, 50*time.Millisecond)
	_, err := client.Chat(context.Background(), model.ChatRequest{
		Model:    "gpt-5.4",
		Messages: []model.Message{{Role: model.RoleUser, Content: "hello"}},
	})
	if err == nil {
		t.Fatal("Chat() error = nil, want timeout error")
	}
	lower := strings.ToLower(err.Error())
	if !strings.Contains(lower, "timeout") && !strings.Contains(lower, "deadline") {
		t.Fatalf("Chat() error = %v, want timeout/deadline error", err)
	}
}

func TestOpenAiResponsesClient_TimesOutWhenHeadersArriveButFirstEventDoesNot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
			time.Sleep(150 * time.Millisecond)
		}
	}))
	defer server.Close()

	client := NewOpenAiResponsesClient("test-key", server.URL, 50*time.Millisecond)
	_, err := client.Chat(context.Background(), model.ChatRequest{
		Model:    "gpt-5.4",
		Messages: []model.Message{{Role: model.RoleUser, Content: "hello"}},
	})
	if err == nil {
		t.Fatal("Chat() error = nil, want timeout error")
	}
	lower := strings.ToLower(err.Error())
	if !strings.Contains(lower, "timeout") && !strings.Contains(lower, "deadline") {
		t.Fatalf("Chat() error = %v, want timeout/deadline error", err)
	}
}

func TestOpenAiResponsesClient_DoesNotTimeoutActiveLongStreamAfterFirstEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if flusher, ok := w.(http.Flusher); ok {
			_, _ = w.Write([]byte("data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"message\",\"id\":\"msg_1\"}}\n\n"))
			_, _ = w.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"content_index\":0,\"delta\":\"hel\"}\n\n"))
			flusher.Flush()
			time.Sleep(120 * time.Millisecond)
			_, _ = w.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"content_index\":0,\"delta\":\"lo\"}\n\n"))
			_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"id\":\"msg_1\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello\"}]}],\"usage\":{\"input_tokens\":1,\"output_tokens\":2,\"total_tokens\":3}}}\n\n"))
			flusher.Flush()
		}
	}))
	defer server.Close()

	client := NewOpenAiResponsesClient("test-key", server.URL, 50*time.Millisecond)
	resp, err := client.Chat(context.Background(), model.ChatRequest{
		Model:    "gpt-5.4",
		Messages: []model.Message{{Role: model.RoleUser, Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Content != "hello" || resp.Message.Content != "hello" {
		t.Fatalf("response content/message = %#v, want hello", resp)
	}
}
