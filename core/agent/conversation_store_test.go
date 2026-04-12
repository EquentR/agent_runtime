package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
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

func TestConversationStoreListMessagesBackfillsOnlyFinalAssistantReply(t *testing.T) {
	store := newConversationStoreForTest(t)
	if err := store.db.AutoMigrate(&coretasks.Task{}); err != nil {
		t.Fatalf("AutoMigrate(tasks) error = %v", err)
	}
	_, err := store.CreateConversation(context.Background(), CreateConversationInput{
		ID:         "conv_1",
		ProviderID: "openai",
		ModelID:    "gpt-5.4",
	})
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}

	firstAssistantRaw := []byte(`{"version":"v1","message":{"Role":"assistant","Content":"planning tool call"}}`)
	finalAssistantRaw := []byte(`{"version":"v1","message":{"Role":"assistant","Content":"final answer","Usage":{"PromptTokens":123,"CompletionTokens":45,"TotalTokens":168}}}`)
	if err := store.db.Create(&ConversationMessage{ConversationID: "conv_1", Seq: 1, Role: model.RoleAssistant, Content: "planning tool call", MessageJSON: firstAssistantRaw, TaskID: "task_1"}).Error; err != nil {
		t.Fatalf("insert first assistant error = %v", err)
	}
	if err := store.db.Create(&ConversationMessage{ConversationID: "conv_1", Seq: 2, Role: model.RoleTool, Content: "tool output", MessageJSON: []byte(`{"version":"v1","message":{"Role":"tool","Content":"tool output"}}`), TaskID: "task_1"}).Error; err != nil {
		t.Fatalf("insert tool error = %v", err)
	}
	if err := store.db.Create(&ConversationMessage{ConversationID: "conv_1", Seq: 3, Role: model.RoleAssistant, Content: "final answer", MessageJSON: finalAssistantRaw, TaskID: "task_1"}).Error; err != nil {
		t.Fatalf("insert final assistant error = %v", err)
	}
	if err := store.db.Create(&coretasks.Task{
		ID:            "task_1",
		TaskType:      "agent.run",
		Status:        coretasks.StatusSucceeded,
		InputJSON:     []byte(`{}`),
		ConfigJSON:    []byte(`{}`),
		MetadataJSON:  []byte(`{}`),
		ResultJSON:    []byte(`{"usage":{"PromptTokens":123,"CompletionTokens":45,"TotalTokens":168}}`),
		ExecutionMode: coretasks.ExecutionModeSerial,
		RootTaskID:    "task_1",
	}).Error; err != nil {
		t.Fatalf("insert task error = %v", err)
	}

	got, err := store.ListMessages(context.Background(), "conv_1")
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len(messages) = %d, want 3", len(got))
	}
	if got[0].Usage != nil {
		t.Fatalf("got[0].Usage = %#v, want nil on non-final assistant", got[0].Usage)
	}
	if got[2].Usage == nil || got[2].Usage.TotalTokens != 168 {
		t.Fatalf("got[2].Usage = %#v, want persisted final usage", got[2].Usage)
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

func TestConversationStoreEnsureConversationUpdatesLatestModelSelection(t *testing.T) {
	store := newConversationStoreForTest(t)
	_, err := store.CreateConversation(context.Background(), CreateConversationInput{
		ID:         "conv_1",
		ProviderID: "openai",
		ModelID:    "gpt-5.4",
	})
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}

	conversation, err := store.EnsureConversation(context.Background(), EnsureConversationInput{
		ID:         "conv_1",
		ProviderID: "google",
		ModelID:    "gemini-2.5-flash",
	})
	if err != nil {
		t.Fatalf("EnsureConversation() error = %v", err)
	}
	if conversation.ProviderID != "google" || conversation.ModelID != "gemini-2.5-flash" {
		t.Fatalf("conversation = %#v, want latest provider/model persisted", conversation)
	}

	reloaded, err := store.GetConversation(context.Background(), "conv_1")
	if err != nil {
		t.Fatalf("GetConversation() error = %v", err)
	}
	if reloaded.ProviderID != "google" || reloaded.ModelID != "gemini-2.5-flash" {
		t.Fatalf("reloaded = %#v, want updated provider/model", reloaded)
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

func TestConversationStoreGetAndSetMemorySummary(t *testing.T) {
	store := newConversationStoreForTest(t)
	_, err := store.CreateConversation(context.Background(), CreateConversationInput{
		ID:         "conv_1",
		ProviderID: "openai",
		ModelID:    "gpt-5.4",
	})
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}

	// Before any summary is persisted, GetMemorySummary should return "", nil.
	got, err := store.GetMemorySummary(context.Background(), "conv_1")
	if err != nil {
		t.Fatalf("GetMemorySummary() error = %v, want nil", err)
	}
	if got != "" {
		t.Fatalf("GetMemorySummary() = %q, want empty string before any summary is saved", got)
	}

	// Persist a summary.
	wantSummary := "task goal: summarize repo; progress: done"
	if err := store.SetMemorySummary(context.Background(), "conv_1", wantSummary); err != nil {
		t.Fatalf("SetMemorySummary() error = %v", err)
	}

	// Now GetMemorySummary should return the persisted value.
	got, err = store.GetMemorySummary(context.Background(), "conv_1")
	if err != nil {
		t.Fatalf("GetMemorySummary() after set error = %v", err)
	}
	if got != wantSummary {
		t.Fatalf("GetMemorySummary() = %q, want %q", got, wantSummary)
	}

	if err := store.SetMemorySummary(context.Background(), "conv_1", ""); err != nil {
		t.Fatalf("SetMemorySummary(clear) error = %v", err)
	}
	got, err = store.GetMemorySummary(context.Background(), "conv_1")
	if err != nil {
		t.Fatalf("GetMemorySummary() after clear error = %v", err)
	}
	if got != "" {
		t.Fatalf("GetMemorySummary() after clear = %q, want empty string", got)
	}
}

func TestConversationStorePersistsMemorySnapshotFields(t *testing.T) {
	store := newConversationStoreForTest(t)
	_, err := store.CreateConversation(context.Background(), CreateConversationInput{
		ID:         "conv_1",
		ProviderID: "openai",
		ModelID:    "gpt-5.4",
	})
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}

	wantContext := &MemoryContextSnapshot{
		ShortTermTokens:       18,
		SummaryTokens:         7,
		RenderedSummaryTokens: 11,
		TotalTokens:           29,
		ShortTermLimit:        70,
		SummaryLimit:          30,
		MaxContextTokens:      100,
		HasSummary:            true,
	}
	wantCompression := &MemoryCompressionSnapshot{
		TokensBefore:                44,
		TokensAfter:                 19,
		ShortTermTokensBefore:       44,
		ShortTermTokensAfter:        8,
		SummaryTokensBefore:         0,
		SummaryTokensAfter:          7,
		RenderedSummaryTokensBefore: 0,
		RenderedSummaryTokensAfter:  11,
		TotalTokensBefore:           44,
		TotalTokensAfter:            19,
	}
	if err := store.SetMemorySnapshots(context.Background(), "conv_1", wantContext, wantCompression); err != nil {
		t.Fatalf("SetMemorySnapshots() error = %v", err)
	}

	wantSummary := "task goal: summarize repo; progress: done"
	if err := store.SetMemorySummary(context.Background(), "conv_1", wantSummary); err != nil {
		t.Fatalf("SetMemorySummary() error = %v", err)
	}

	conversation, err := store.GetConversation(context.Background(), "conv_1")
	if err != nil {
		t.Fatalf("GetConversation() error = %v", err)
	}
	if !reflect.DeepEqual(conversation.MemoryContext, wantContext) {
		t.Fatalf("conversation.MemoryContext = %#v, want %#v", conversation.MemoryContext, wantContext)
	}
	if !reflect.DeepEqual(conversation.MemoryCompression, wantCompression) {
		t.Fatalf("conversation.MemoryCompression = %#v, want %#v", conversation.MemoryCompression, wantCompression)
	}

	gotSummary, err := store.GetMemorySummary(context.Background(), "conv_1")
	if err != nil {
		t.Fatalf("GetMemorySummary() error = %v", err)
	}
	if gotSummary != wantSummary {
		t.Fatalf("GetMemorySummary() = %q, want %q", gotSummary, wantSummary)
	}

	conversations, err := store.ListConversations(context.Background())
	if err != nil {
		t.Fatalf("ListConversations() error = %v", err)
	}
	if len(conversations) != 1 {
		t.Fatalf("len(conversations) = %d, want 1", len(conversations))
	}
	if !reflect.DeepEqual(conversations[0].MemoryContext, wantContext) {
		t.Fatalf("conversations[0].MemoryContext = %#v, want %#v", conversations[0].MemoryContext, wantContext)
	}
	if !reflect.DeepEqual(conversations[0].MemoryCompression, wantCompression) {
		t.Fatalf("conversations[0].MemoryCompression = %#v, want %#v", conversations[0].MemoryCompression, wantCompression)
	}
}
