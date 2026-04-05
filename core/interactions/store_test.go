package interactions

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestStoreCreateInteractionPersistsQuestionRequest(t *testing.T) {
	store := newInteractionStoreForTest(t)

	created, err := store.CreateInteraction(context.Background(), CreateInteractionInput{
		ID:             "interaction_1",
		TaskID:         "task_1",
		ConversationID: "conv_1",
		ToolCallID:     "call_1",
		Kind:           KindQuestion,
		Request: QuestionRequest{
			Prompt:      "Which environment?",
			Options:     []Option{{ID: "staging", Label: "Staging"}},
			AllowCustom: true,
		},
	})
	if err != nil {
		t.Fatalf("CreateInteraction() error = %v", err)
	}
	if created.Kind != KindQuestion {
		t.Fatalf("created.Kind = %q, want %q", created.Kind, KindQuestion)
	}
	if created.Status != StatusPending {
		t.Fatalf("created.Status = %q, want %q", created.Status, StatusPending)
	}

	var request QuestionRequest
	if err := json.Unmarshal(created.RequestJSON, &request); err != nil {
		t.Fatalf("json.Unmarshal(created.RequestJSON) error = %v", err)
	}
	if request.Prompt != "Which environment?" {
		t.Fatalf("request.Prompt = %q, want %q", request.Prompt, "Which environment?")
	}
	if len(request.Options) != 1 {
		t.Fatalf("len(request.Options) = %d, want 1", len(request.Options))
	}
	if request.Options[0].ID != "staging" {
		t.Fatalf("request.Options[0].ID = %q, want %q", request.Options[0].ID, "staging")
	}
	if !request.AllowCustom {
		t.Fatal("request.AllowCustom = false, want true")
	}

	loaded, err := store.GetInteraction(context.Background(), "task_1", "interaction_1")
	if err != nil {
		t.Fatalf("GetInteraction() error = %v", err)
	}
	if string(loaded.RequestJSON) != string(created.RequestJSON) {
		t.Fatalf("loaded.RequestJSON = %s, want %s", string(loaded.RequestJSON), string(created.RequestJSON))
	}
}

func TestStoreCreateInteractionRejectsUnsupportedKindAndStatus(t *testing.T) {
	store := newInteractionStoreForTest(t)

	_, err := store.CreateInteraction(context.Background(), CreateInteractionInput{
		TaskID:  "task_1",
		Kind:    Kind("weird"),
		Request: map[string]any{"prompt": "hello"},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported interaction kind") {
		t.Fatalf("CreateInteraction() error = %v, want unsupported interaction kind", err)
	}

	_, err = store.CreateInteraction(context.Background(), CreateInteractionInput{
		TaskID:  "task_1",
		Kind:    KindQuestion,
		Status:  Status("mystery"),
		Request: map[string]any{"prompt": "hello"},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported interaction status") {
		t.Fatalf("CreateInteraction() error = %v, want unsupported interaction status", err)
	}
}

func TestStoreCreateInteractionRejectsPendingResponseMetadata(t *testing.T) {
	store := newInteractionStoreForTest(t)
	now := time.Now().UTC()

	_, err := store.CreateInteraction(context.Background(), CreateInteractionInput{
		TaskID:      "task_1",
		Kind:        KindQuestion,
		Status:      StatusPending,
		Request:     map[string]any{"prompt": "hello"},
		Response:    map[string]any{"selected_option_id": "a"},
		RespondedBy: "alice",
		RespondedAt: &now,
	})
	if err == nil || !strings.Contains(err.Error(), "pending interaction cannot include response metadata") {
		t.Fatalf("CreateInteraction() error = %v, want pending response metadata rejection", err)
	}
}

func TestStoreCreateInteractionPersistsResponseMetadataForResolvedStatus(t *testing.T) {
	store := newInteractionStoreForTest(t)
	now := time.Date(2026, time.March, 31, 12, 0, 0, 0, time.UTC)

	created, err := store.CreateInteraction(context.Background(), CreateInteractionInput{
		TaskID:      "task_1",
		Kind:        KindQuestion,
		Status:      StatusResponded,
		Request:     map[string]any{"prompt": "Which environment?"},
		Response:    map[string]any{"selected_option_id": "staging"},
		RespondedBy: "alice",
		RespondedAt: &now,
	})
	if err != nil {
		t.Fatalf("CreateInteraction() error = %v", err)
	}
	if created.RespondedBy != "alice" {
		t.Fatalf("created.RespondedBy = %q, want %q", created.RespondedBy, "alice")
	}
	if created.RespondedAt == nil || !created.RespondedAt.Equal(now) {
		t.Fatalf("created.RespondedAt = %v, want %v", created.RespondedAt, now)
	}
	if string(created.ResponseJSON) == "" || string(created.ResponseJSON) == "null" {
		t.Fatalf("created.ResponseJSON = %s, want non-empty JSON", string(created.ResponseJSON))
	}
}

func TestStoreCreateInteractionTreatsRawNullResponseAsEmpty(t *testing.T) {
	store := newInteractionStoreForTest(t)
	now := time.Date(2026, time.April, 5, 12, 0, 0, 0, time.UTC)

	created, err := store.CreateInteraction(context.Background(), CreateInteractionInput{
		TaskID:      "task_1",
		Kind:        KindQuestion,
		Status:      StatusResponded,
		Request:     map[string]any{"prompt": "Which environment?"},
		Response:    json.RawMessage(" null "),
		RespondedBy: "alice",
		RespondedAt: &now,
	})
	if err != nil {
		t.Fatalf("CreateInteraction() error = %v", err)
	}
	if len(created.ResponseJSON) != 0 {
		t.Fatalf("created.ResponseJSON = %s, want empty response bytes", string(created.ResponseJSON))
	}
}

func TestStoreGetInteractionRequiresTaskScope(t *testing.T) {
	store := newInteractionStoreForTest(t)
	_, err := store.GetInteraction(context.Background(), "", "interaction_1")
	if err == nil || err.Error() != "task id cannot be empty" {
		t.Fatalf("GetInteraction() error = %v, want task id cannot be empty", err)
	}
}

func TestStoreGetInteractionReturnsNotFound(t *testing.T) {
	store := newInteractionStoreForTest(t)
	_, err := store.GetInteraction(context.Background(), "task_1", "missing")
	if err == nil || !strings.Contains(err.Error(), ErrInteractionNotFound.Error()) {
		t.Fatalf("GetInteraction() error = %v, want ErrInteractionNotFound", err)
	}
}

func newInteractionStoreForTest(t *testing.T) *Store {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}

	store := NewStore(db)
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	return store
}
