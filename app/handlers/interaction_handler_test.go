package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/EquentR/agent_runtime/app/logics"
	coreagent "github.com/EquentR/agent_runtime/core/agent"
	"github.com/EquentR/agent_runtime/core/interactions"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	"github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestInteractionHandlerListsTaskInteractionsForOwner(t *testing.T) {
	deps, server := newInteractionHandlerTestServer(t)
	ownerCookie := registerAndLoginAuthUser(t, server.URL, "alice")
	task := createTaskAsUser(t, server.URL, ownerCookie, map[string]any{"task_type": "agent.run", "created_by": "alice"})
	if _, err := deps.interactionStore.CreateInteraction(context.Background(), interactions.CreateInteractionInput{
		ID:             "interaction_1",
		TaskID:         task.ID,
		ConversationID: "conv-1",
		Kind:           interactions.KindQuestion,
		Request:        map[string]any{"question": "Which environment?", "options": []string{"Staging", "Production"}},
	}); err != nil {
		t.Fatalf("CreateInteraction() error = %v", err)
	}
	envelope := getInteractionListEnvelope(t, server.URL, ownerCookie, task.ID)
	if !envelope.OK {
		t.Fatalf("list OK = false, message = %q", envelope.Message)
	}
	var listed []map[string]any
	if err := json.Unmarshal(envelope.Data, &listed); err != nil {
		t.Fatalf("json.Unmarshal(listed) error = %v", err)
	}
	if len(listed) != 1 || listed[0]["id"] != "interaction_1" {
		t.Fatalf("listed = %#v, want interaction_1", listed)
	}
	if _, ok := listed[0]["request_json"].(map[string]any); !ok {
		t.Fatalf("request_json = %#v, want decoded object", listed[0]["request_json"])
	}
}

func TestInteractionHandlerListReturnsDecodedInteractionJSONObjects(t *testing.T) {
	deps, server := newInteractionHandlerTestServer(t)
	ownerCookie := registerAndLoginAuthUser(t, server.URL, "alice")
	task := createTaskAsUser(t, server.URL, ownerCookie, map[string]any{"task_type": "agent.run", "created_by": "alice"})
	if _, err := deps.interactionStore.CreateInteraction(context.Background(), interactions.CreateInteractionInput{
		ID:             "interaction_2",
		TaskID:         task.ID,
		ConversationID: "conv-1",
		Kind:           interactions.KindQuestion,
		Request: map[string]any{
			"question": "Which environment?",
			"options":  []string{"Staging", "Production"},
		},
		Response: map[string]any{
			"selected_option_id": "Staging",
		},
		RespondedBy: "alice",
		RespondedAt: ptrTime(time.Now().UTC()),
		Status:      interactions.StatusResponded,
	}); err != nil {
		t.Fatalf("CreateInteraction() error = %v", err)
	}
	envelope := getInteractionListEnvelope(t, server.URL, ownerCookie, task.ID)
	if !envelope.OK {
		t.Fatalf("list OK = false, message = %q", envelope.Message)
	}
	var listed []map[string]any
	if err := json.Unmarshal(envelope.Data, &listed); err != nil {
		t.Fatalf("json.Unmarshal(listed) error = %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("len(listed) = %d, want 1", len(listed))
	}
	if _, ok := listed[0]["request_json"].(map[string]any); !ok {
		t.Fatalf("request_json = %#v, want decoded object", listed[0]["request_json"])
	}
	if _, ok := listed[0]["response_json"].(map[string]any); !ok {
		t.Fatalf("response_json = %#v, want decoded object", listed[0]["response_json"])
	}
}

func TestInteractionHandlerRespondsToQuestionInteraction(t *testing.T) {
	deps, server := newInteractionHandlerTestServer(t)
	ownerCookie := registerAndLoginAuthUser(t, server.URL, "alice")
	task, interaction := createWaitingTaskWithQuestionInteraction(t, deps, "alice")
	envelope := postInteractionResponseEnvelope(t, server.URL, ownerCookie, task.ID, interaction.ID, map[string]any{
		"selected_option_id": "staging",
		"custom_text":        "",
	})
	if !envelope.OK {
		t.Fatalf("respond OK = false, message = %q", envelope.Message)
	}
	var responded map[string]any
	if err := json.Unmarshal(envelope.Data, &responded); err != nil {
		t.Fatalf("json.Unmarshal(responded) error = %v", err)
	}
	if responded["status"] != string(interactions.StatusResponded) {
		t.Fatalf("responded.status = %#v, want %q", responded["status"], interactions.StatusResponded)
	}
	if responded["responded_by"] != "alice" {
		t.Fatalf("responded.responded_by = %#v, want alice", responded["responded_by"])
	}
	if _, ok := responded["response_json"].(map[string]any); !ok {
		t.Fatalf("responded.response_json = %#v, want decoded object", responded["response_json"])
	}
	waiting, err := deps.taskManager.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if waiting.Status != coretasks.StatusQueued {
		t.Fatalf("waiting.Status = %q, want queued after atomic respond+resume", waiting.Status)
	}
}

func TestInteractionHandlerRejectsEmptyQuestionResponse(t *testing.T) {
	deps, server := newInteractionHandlerTestServer(t)
	ownerCookie := registerAndLoginAuthUser(t, server.URL, "alice")
	task, interaction := createWaitingTaskWithQuestionInteraction(t, deps, "alice")
	envelope := postInteractionResponseEnvelope(t, server.URL, ownerCookie, task.ID, interaction.ID, map[string]any{
		"selected_option_id": "",
		"custom_text":        "   ",
	})
	if envelope.OK {
		t.Fatalf("respond OK = true, want false for empty response: %s", string(envelope.Data))
	}
	stored, err := deps.interactionStore.GetInteraction(context.Background(), task.ID, interaction.ID)
	if err != nil {
		t.Fatalf("GetInteraction() error = %v", err)
	}
	if stored.Status != interactions.StatusPending {
		t.Fatalf("stored.Status = %q, want pending", stored.Status)
	}
}

type interactionHandlerTestDeps struct {
	authLogic         *logics.AuthLogic
	conversationStore *coreagent.ConversationStore
	taskStore         *coretasks.Store
	taskManager       *coretasks.Manager
	interactionStore  *interactions.Store
}

func newInteractionHandlerTestServer(t *testing.T) (*interactionHandlerTestDeps, *httptest.Server) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s_interactions?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	conversationStore := coreagent.NewConversationStore(db)
	if err := conversationStore.AutoMigrate(); err != nil {
		t.Fatalf("conversation AutoMigrate() error = %v", err)
	}
	taskStore := coretasks.NewStore(db)
	if err := taskStore.AutoMigrate(); err != nil {
		t.Fatalf("task AutoMigrate() error = %v", err)
	}
	interactionStore := interactions.NewStore(db)
	if err := interactionStore.AutoMigrate(); err != nil {
		t.Fatalf("interaction AutoMigrate() error = %v", err)
	}
	taskManager := coretasks.NewManager(taskStore, coretasks.ManagerOptions{RunnerID: "interaction-handler-test", InteractionStore: interactionStore})
	authLogic := newAuthLogicForTest(t, db)
	authMiddleware := NewAuthMiddleware(authLogic)
	engine := rest.Init()
	group := engine.Group("/api/v1")
	NewAuthHandler(authLogic).Register(group)
	NewTaskHandler(taskManager, conversationStore, authMiddleware.RequireSession()).Register(group)
	NewInteractionHandler(taskManager, interactionStore, conversationStore, authMiddleware.RequireSession()).Register(group)
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	return &interactionHandlerTestDeps{authLogic: authLogic, conversationStore: conversationStore, taskStore: taskStore, taskManager: taskManager, interactionStore: interactionStore}, server
}

func createWaitingTaskWithQuestionInteraction(t *testing.T, deps *interactionHandlerTestDeps, createdBy string) (*coretasks.Task, *interactions.Interaction) {
	t.Helper()
	task, err := deps.taskManager.CreateTask(context.Background(), coretasks.CreateTaskInput{TaskType: "agent.run", CreatedBy: createdBy})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	claimed, _, err := deps.taskStore.ClaimNextTask(context.Background(), "interaction-handler-test", 30*time.Second)
	if err != nil {
		t.Fatalf("ClaimNextTask() error = %v", err)
	}
	if claimed == nil || claimed.ID != task.ID {
		t.Fatalf("claimed task = %#v, want %q", claimed, task.ID)
	}
	if _, _, err := deps.taskStore.MarkWaiting(context.Background(), task.ID, "waiting_for_interaction"); err != nil {
		t.Fatalf("MarkWaiting() error = %v", err)
	}
	interaction, err := deps.interactionStore.CreateInteraction(context.Background(), interactions.CreateInteractionInput{
		ID:             "interaction_question_1",
		TaskID:         task.ID,
		ConversationID: "conv-1",
		Kind:           interactions.KindQuestion,
		Request:        map[string]any{"question": "Which environment?", "options": []string{"staging", "production"}},
	})
	if err != nil {
		t.Fatalf("CreateInteraction() error = %v", err)
	}
	return task, interaction
}

func getInteractionListEnvelope(t *testing.T, baseURL string, cookie *http.Cookie, taskID string) taskTestResponse {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, baseURL+"/api/v1/tasks/"+taskID+"/interactions", nil)
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	request.AddCookie(cookie)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("http.DefaultClient.Do() error = %v", err)
	}
	defer response.Body.Close()
	return decodeEnvelope(t, response.Body)
}

func postInteractionResponseEnvelope(t *testing.T, baseURL string, cookie *http.Cookie, taskID string, interactionID string, payload map[string]any) taskTestResponse {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, baseURL+"/api/v1/tasks/"+taskID+"/interactions/"+interactionID+"/respond", bytes.NewReader(mustJSON(t, payload)))
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("http.DefaultClient.Do() error = %v", err)
	}
	defer response.Body.Close()
	return decodeEnvelope(t, response.Body)
}
