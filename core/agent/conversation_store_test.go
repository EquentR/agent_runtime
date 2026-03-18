package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	coretypes "github.com/EquentR/agent_runtime/core/types"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestConversationStoreCreateConversation(t *testing.T) {
	store := newConversationStoreForTest(t)
	conversation, err := store.CreateConversation(context.Background(), CreateConversationInput{
		ID:         "conv_1",
		ProviderID: "openai",
		ModelID:    "gpt-5.4",
		CreatedBy:  "tester",
	})
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	if conversation.ID != "conv_1" || conversation.ProviderID != "openai" || conversation.ModelID != "gpt-5.4" {
		t.Fatalf("conversation = %#v, want persisted ids", conversation)
	}
}

func TestConversationStoreAppendAndListMessages(t *testing.T) {
	store := newConversationStoreForTest(t)
	_, err := store.CreateConversation(context.Background(), CreateConversationInput{
		ID:         "conv_1",
		ProviderID: "openai",
		ModelID:    "gpt-5.4",
	})
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	err = store.AppendMessages(context.Background(), "conv_1", "task_1", []model.Message{
		{Role: model.RoleUser, Content: "hello"},
		{Role: model.RoleAssistant, Content: "hi"},
	})
	if err != nil {
		t.Fatalf("AppendMessages() error = %v", err)
	}
	got, err := store.ListMessages(context.Background(), "conv_1")
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(messages) = %d, want 2", len(got))
	}
	if got[0].Role != model.RoleUser || got[1].Role != model.RoleAssistant {
		t.Fatalf("messages = %#v, want user/assistant order", got)
	}
}

func TestConversationStoreListMessagesPreservesReplayableAssistantState(t *testing.T) {
	store := newConversationStoreForTest(t)
	_, err := store.CreateConversation(context.Background(), CreateConversationInput{
		ID:         "conv_1",
		ProviderID: "openai",
		ModelID:    "gpt-5.4",
	})
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}

	want := model.Message{
		Role:      model.RoleAssistant,
		Content:   "hello",
		Reasoning: "plan first",
		ReasoningItems: []model.ReasoningItem{{
			ID:               "rs_1",
			EncryptedContent: "enc_1",
			Summary:          []model.ReasoningSummary{{Text: "plan first"}},
		}},
		ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: `{"city":"Beijing"}`}},
		ProviderState: &model.ProviderState{
			Provider:   "openai_responses",
			Format:     "openai_response_state.v1",
			Version:    "v1",
			ResponseID: "resp_1",
			Payload:    json.RawMessage(`{"response_id":"resp_1","output":[{"type":"message","id":"msg_1"}]}`),
		},
		ProviderData: map[string]any{
			"type":        "openai_responses.output.v1",
			"response_id": "resp_1",
			"output_json": `[{"type":"message","id":"msg_1"}]`,
		},
	}

	err = store.AppendMessages(context.Background(), "conv_1", "task_1", []model.Message{want})
	if err != nil {
		t.Fatalf("AppendMessages() error = %v", err)
	}

	got, err := store.ListMessages(context.Background(), "conv_1")
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(got))
	}
	if got[0].ProviderState == nil {
		t.Fatal("ProviderState = nil, want persisted replay state")
	}
	if got[0].ProviderState.ResponseID != "resp_1" {
		t.Fatalf("ProviderState.ResponseID = %q, want resp_1", got[0].ProviderState.ResponseID)
	}
	if string(got[0].ProviderState.Payload) != string(want.ProviderState.Payload) {
		t.Fatalf("ProviderState.Payload = %s, want %s", string(got[0].ProviderState.Payload), string(want.ProviderState.Payload))
	}
	if got[0].ProviderData == nil {
		t.Fatal("ProviderData = nil, want persisted replay snapshot")
	}
	providerData, ok := got[0].ProviderData.(map[string]any)
	if !ok {
		t.Fatalf("ProviderData type = %T, want map[string]any", got[0].ProviderData)
	}
	if providerData["response_id"] != "resp_1" {
		t.Fatalf("ProviderData.response_id = %#v, want resp_1", providerData["response_id"])
	}
}

func TestConversationStoreListMessagesReadsVersionedEnvelope(t *testing.T) {
	store := newConversationStoreForTest(t)
	_, err := store.CreateConversation(context.Background(), CreateConversationInput{
		ID:         "conv_1",
		ProviderID: "openai",
		ModelID:    "gpt-5.4",
	})
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}

	envelopeRaw, err := json.Marshal(persistedConversationMessage{
		Version: persistedConversationMessageVersion,
		Message: model.Message{
			Role:    model.RoleAssistant,
			Content: "hello",
			ProviderState: &model.ProviderState{
				Provider:   "openai_responses",
				Format:     "openai_response_state.v1",
				Version:    "v1",
				ResponseID: "resp_1",
				Payload:    json.RawMessage(`{"response_id":"resp_1"}`),
			},
		},
	})
	if err != nil {
		t.Fatalf("json.Marshal(envelope) error = %v", err)
	}
	if err := store.db.Create(&ConversationMessage{ConversationID: "conv_1", Seq: 1, Role: model.RoleAssistant, Content: "hello", MessageJSON: envelopeRaw}).Error; err != nil {
		t.Fatalf("insert envelope message error = %v", err)
	}

	got, err := store.ListMessages(context.Background(), "conv_1")
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(got))
	}
	if got[0].ProviderState == nil || got[0].ProviderState.ResponseID != "resp_1" {
		t.Fatalf("got[0].ProviderState = %#v, want ResponseID resp_1", got[0].ProviderState)
	}
}

func TestConversationStoreAppendMessagesUpdatesTitleSummaryAndCount(t *testing.T) {
	store := newConversationStoreForTest(t)
	_, err := store.CreateConversation(context.Background(), CreateConversationInput{
		ID:         "conv_1",
		ProviderID: "openai",
		ModelID:    "gpt-5.4",
	})
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	err = store.AppendMessages(context.Background(), "conv_1", "task_1", []model.Message{
		{Role: model.RoleUser, Content: "   Please help me summarize this repository structure in one sentence.   "},
		{Role: model.RoleAssistant, Content: "Here is a short summary."},
	})
	if err != nil {
		t.Fatalf("AppendMessages() error = %v", err)
	}
	conversation, err := store.GetConversation(context.Background(), "conv_1")
	if err != nil {
		t.Fatalf("GetConversation() error = %v", err)
	}
	if conversation.Title == "" {
		t.Fatal("Title = empty, want derived title")
	}
	if conversation.LastMessage != "Here is a short summary." {
		t.Fatalf("LastMessage = %q, want assistant tail", conversation.LastMessage)
	}
	if conversation.MessageCount != 2 {
		t.Fatalf("MessageCount = %d, want 2", conversation.MessageCount)
	}
}

func TestConversationStoreListConversationsReturnsMostRecentFirst(t *testing.T) {
	store := newConversationStoreForTest(t)
	_, err := store.CreateConversation(context.Background(), CreateConversationInput{ID: "conv_old", ProviderID: "openai", ModelID: "gpt-5.4"})
	if err != nil {
		t.Fatalf("CreateConversation(old) error = %v", err)
	}
	_, err = store.CreateConversation(context.Background(), CreateConversationInput{ID: "conv_new", ProviderID: "openai", ModelID: "gpt-5.4"})
	if err != nil {
		t.Fatalf("CreateConversation(new) error = %v", err)
	}
	if err := store.AppendMessages(context.Background(), "conv_old", "task_old", []model.Message{{Role: model.RoleUser, Content: "old"}}); err != nil {
		t.Fatalf("AppendMessages(old) error = %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := store.AppendMessages(context.Background(), "conv_new", "task_new", []model.Message{{Role: model.RoleUser, Content: "new"}}); err != nil {
		t.Fatalf("AppendMessages(new) error = %v", err)
	}
	conversations, err := store.ListConversations(context.Background())
	if err != nil {
		t.Fatalf("ListConversations() error = %v", err)
	}
	if len(conversations) < 2 {
		t.Fatalf("len(conversations) = %d, want at least 2", len(conversations))
	}
	if conversations[0].ID != "conv_new" {
		t.Fatalf("first conversation = %q, want conv_new", conversations[0].ID)
	}
}

func TestConversationStoreDeleteConversationRemovesMessages(t *testing.T) {
	store := newConversationStoreForTest(t)
	_, err := store.CreateConversation(context.Background(), CreateConversationInput{
		ID:         "conv_1",
		ProviderID: "openai",
		ModelID:    "gpt-5.4",
	})
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	if err := store.AppendMessages(context.Background(), "conv_1", "task_1", []model.Message{{Role: model.RoleUser, Content: "hello"}}); err != nil {
		t.Fatalf("AppendMessages() error = %v", err)
	}

	err = store.DeleteConversation(context.Background(), "conv_1")
	if err != nil {
		t.Fatalf("DeleteConversation() error = %v", err)
	}

	_, err = store.GetConversation(context.Background(), "conv_1")
	if err == nil || !errors.Is(err, ErrConversationNotFound) {
		t.Fatalf("GetConversation() error = %v, want ErrConversationNotFound", err)
	}

	messages, err := store.ListMessages(context.Background(), "conv_1")
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("len(messages) = %d, want 0 after delete", len(messages))
	}
}

func newConversationStoreForTest(t *testing.T) *ConversationStore {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	store := NewConversationStore(db)
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	return store
}
