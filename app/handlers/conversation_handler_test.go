package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	coreagent "github.com/EquentR/agent_runtime/core/agent"
	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestConversationHandlerGetConversation(t *testing.T) {
	store, server := newConversationHandlerTestServer(t)
	conversation, err := store.CreateConversation(context.Background(), coreagent.CreateConversationInput{
		ID:         "conv_1",
		ProviderID: "openai",
		ModelID:    "gpt-5.4",
		Title:      "Hello",
		CreatedBy:  "tester",
	})
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}

	resp, err := http.Get(server.URL + "/api/v1/conversations/" + conversation.ID)
	if err != nil {
		t.Fatalf("http.Get() error = %v", err)
	}
	defer resp.Body.Close()
	got := decodeConversationResponse(t, resp.Body)
	if got.ID != "conv_1" || got.ProviderID != "openai" || got.ModelID != "gpt-5.4" {
		t.Fatalf("conversation = %#v, want persisted conversation", got)
	}
}

func TestConversationHandlerGetConversationMessages(t *testing.T) {
	store, server := newConversationHandlerTestServer(t)
	_, err := store.CreateConversation(context.Background(), coreagent.CreateConversationInput{ID: "conv_1", ProviderID: "openai", ModelID: "gpt-5.4"})
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	if err := store.AppendMessages(context.Background(), "conv_1", "task_1", []model.Message{{Role: model.RoleUser, Content: "hello"}, {Role: model.RoleAssistant, Content: "hi"}}); err != nil {
		t.Fatalf("AppendMessages() error = %v", err)
	}

	resp, err := http.Get(server.URL + "/api/v1/conversations/conv_1/messages")
	if err != nil {
		t.Fatalf("http.Get() error = %v", err)
	}
	defer resp.Body.Close()
	got := decodeConversationMessagesResponse(t, resp.Body)
	if len(got) != 2 {
		t.Fatalf("len(messages) = %d, want 2", len(got))
	}
	if got[0].Role != model.RoleUser || got[1].Role != model.RoleAssistant {
		t.Fatalf("messages = %#v, want ordered user/assistant messages", got)
	}
}

func TestConversationHandlerReturnsNotFoundForMissingConversation(t *testing.T) {
	_, server := newConversationHandlerTestServer(t)
	resp, err := http.Get(server.URL + "/api/v1/conversations/missing")
	if err != nil {
		t.Fatalf("http.Get() error = %v", err)
	}
	defer resp.Body.Close()
	var envelope taskTestResponse
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if envelope.OK {
		t.Fatal("response ok = true, want false for missing conversation")
	}
}

func TestConversationHandlerListConversations(t *testing.T) {
	store, server := newConversationHandlerTestServer(t)
	_, err := store.CreateConversation(context.Background(), coreagent.CreateConversationInput{ID: "conv_old", ProviderID: "openai", ModelID: "gpt-5.4"})
	if err != nil {
		t.Fatalf("CreateConversation(old) error = %v", err)
	}
	_, err = store.CreateConversation(context.Background(), coreagent.CreateConversationInput{ID: "conv_new", ProviderID: "openai", ModelID: "gpt-5.4"})
	if err != nil {
		t.Fatalf("CreateConversation(new) error = %v", err)
	}
	if err := store.AppendMessages(context.Background(), "conv_old", "task_old", []model.Message{{Role: model.RoleUser, Content: "Old topic"}}); err != nil {
		t.Fatalf("AppendMessages(old) error = %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := store.AppendMessages(context.Background(), "conv_new", "task_new", []model.Message{{Role: model.RoleUser, Content: "New topic"}, {Role: model.RoleAssistant, Content: "Latest answer"}}); err != nil {
		t.Fatalf("AppendMessages(new) error = %v", err)
	}

	resp, err := http.Get(server.URL + "/api/v1/conversations")
	if err != nil {
		t.Fatalf("http.Get() error = %v", err)
	}
	defer resp.Body.Close()
	got := decodeConversationListResponse(t, resp.Body)
	if len(got) < 2 {
		t.Fatalf("len(conversations) = %d, want at least 2", len(got))
	}
	if got[0].ID != "conv_new" {
		t.Fatalf("first conversation = %q, want conv_new", got[0].ID)
	}
	if got[0].Title == "" || got[0].LastMessage != "Latest answer" || got[0].MessageCount != 2 {
		t.Fatalf("conversation summary = %#v, want title/last_message/message_count", got[0])
	}
}

func newConversationHandlerTestServer(t *testing.T) (*coreagent.ConversationStore, *httptest.Server) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	store := coreagent.NewConversationStore(db)
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	engine := rest.Init()
	NewConversationHandler(store).Register(engine.Group("/api/v1"))
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	return store, server
}

func decodeConversationResponse(t *testing.T, body io.Reader) coreagent.Conversation {
	t.Helper()
	var envelope taskTestResponse
	if err := json.NewDecoder(body).Decode(&envelope); err != nil {
		t.Fatalf("Decode() envelope error = %v", err)
	}
	if !envelope.OK {
		t.Fatalf("response ok = false, raw data = %s", string(envelope.Data))
	}
	var got coreagent.Conversation
	if err := json.Unmarshal(envelope.Data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	return got
}

func decodeConversationMessagesResponse(t *testing.T, body io.Reader) []model.Message {
	t.Helper()
	var envelope taskTestResponse
	if err := json.NewDecoder(body).Decode(&envelope); err != nil {
		t.Fatalf("Decode() envelope error = %v", err)
	}
	if !envelope.OK {
		t.Fatalf("response ok = false, raw data = %s", string(envelope.Data))
	}
	var got []model.Message
	if err := json.Unmarshal(envelope.Data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	return got
}

func decodeConversationListResponse(t *testing.T, body io.Reader) []coreagent.Conversation {
	t.Helper()
	var envelope taskTestResponse
	if err := json.NewDecoder(body).Decode(&envelope); err != nil {
		t.Fatalf("Decode() envelope error = %v", err)
	}
	if !envelope.OK {
		t.Fatalf("response ok = false, raw data = %s", string(envelope.Data))
	}
	var got []coreagent.Conversation
	if err := json.Unmarshal(envelope.Data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	return got
}
