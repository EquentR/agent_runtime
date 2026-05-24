package handlers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/EquentR/agent_runtime/app/logics"
	"github.com/EquentR/agent_runtime/app/models"
	coreagent "github.com/EquentR/agent_runtime/core/agent"
	coreprompt "github.com/EquentR/agent_runtime/core/prompt"
	model "github.com/EquentR/agent_runtime/core/providers/types"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	coretypes "github.com/EquentR/agent_runtime/core/types"
	coreworkspaces "github.com/EquentR/agent_runtime/core/workspaces"
	"github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/EquentR/agent_runtime/pkg/secret"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// taskTestResponse 对应测试场景下的通用 REST 包装结构。
type taskTestResponse struct {
	Code    int             `json:"code"`
	OK      bool            `json:"ok"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// TestTaskHandlerCreateAndGetTask 验证任务创建与详情查询接口。
func TestTaskHandlerCreateAndGetTask(t *testing.T) {
	manager, server := newTaskHandlerTestServer(t, nil, false)
	_ = manager

	created := createTaskViaHTTP(t, server.URL, map[string]any{
		"task_type": "agent.run",
		"input":     map[string]any{"prompt": "hello"},
		"metadata":  map[string]any{"source": "web"},
	})
	if created.Status != coretasks.StatusQueued {
		t.Fatalf("created status = %q, want %q", created.Status, coretasks.StatusQueued)
	}

	got := getTaskViaHTTP(t, server.URL, created.ID)
	if got.ID != created.ID {
		t.Fatalf("task id = %q, want %q", got.ID, created.ID)
	}
	if got.Status != coretasks.StatusQueued {
		t.Fatalf("task status = %q, want %q", got.Status, coretasks.StatusQueued)
	}
}

// TestTaskHandlerCancelQueuedTask 验证排队任务可以通过接口直接取消。
func TestTaskHandlerCancelQueuedTask(t *testing.T) {
	_, server := newTaskHandlerTestServer(t, nil, false)

	created := createTaskViaHTTP(t, server.URL, map[string]any{
		"task_type": "agent.run",
	})

	cancelled := postTaskAction(t, server.URL, "/api/v1/tasks/"+created.ID+"/cancel")
	if cancelled.Status != coretasks.StatusCancelled {
		t.Fatalf("cancelled status = %q, want %q", cancelled.Status, coretasks.StatusCancelled)
	}
}

func TestTaskHandlerEventsClosesStreamAfterHistoricalTaskFinished(t *testing.T) {
	_, server := newTaskHandlerTestServer(t, nil, false)

	created := createTaskViaHTTP(t, server.URL, map[string]any{
		"task_type": "agent.run",
	})
	_ = postTaskAction(t, server.URL, "/api/v1/tasks/"+created.ID+"/cancel")

	request, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/tasks/"+created.ID+"/events?after_seq=0", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	content := string(body)
	if !strings.Contains(content, "event: task.finished") {
		t.Fatalf("events body = %q, want task.finished event", content)
	}
	if strings.Contains(content, ": keepalive") {
		t.Fatalf("events body = %q, want stream closed before keepalive", content)
	}
}

// TestTaskHandlerRetryCreatesNewQueuedTask 验证重试接口会返回新的任务。
func TestTaskHandlerRetryCreatesNewQueuedTask(t *testing.T) {
	_, server := newTaskHandlerTestServer(t, nil, false)

	original := createTaskViaHTTP(t, server.URL, map[string]any{
		"task_type": "agent.run",
	})
	_ = postTaskAction(t, server.URL, "/api/v1/tasks/"+original.ID+"/cancel")

	retried := postTaskAction(t, server.URL, "/api/v1/tasks/"+original.ID+"/retry")
	if retried.ID == original.ID {
		t.Fatalf("retried id = %q, want new task id", retried.ID)
	}
	if retried.RetryOfTaskID != original.ID {
		t.Fatalf("retry_of_task_id = %q, want %q", retried.RetryOfTaskID, original.ID)
	}
	if retried.Status != coretasks.StatusQueued {
		t.Fatalf("retry status = %q, want %q", retried.Status, coretasks.StatusQueued)
	}
}

// TestTaskHandlerEventsStreamsHistoricalAndLiveEvents 验证 SSE 接口会补发历史事件并继续推送实时事件。
func TestTaskHandlerEventsStreamsHistoricalAndLiveEvents(t *testing.T) {
	release := make(chan struct{})
	manager, server := newTaskHandlerTestServer(t, func(ctx context.Context, task *coretasks.Task, runtime *coretasks.Runtime) (any, error) {
		if err := runtime.StartStep(ctx, "prepare", "Prepare response"); err != nil {
			return nil, err
		}
		<-release
		if err := runtime.FinishStep(ctx, map[string]any{"ok": true}); err != nil {
			return nil, err
		}
		return map[string]any{"message": "done"}, nil
	}, true)

	created := createTaskViaHTTP(t, server.URL, map[string]any{
		"task_type": "agent.run",
	})

	waitForTaskState(t, manager, created.ID, coretasks.StatusRunning)

	request, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/tasks/"+created.ID+"/events?after_seq=1", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer response.Body.Close()

	lines := make(chan string, 32)
	go func() {
		scanner := bufio.NewScanner(response.Body)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()

	if !waitForLine(t, lines, "event: task.started", 2*time.Second) && !waitForLine(t, lines, "event: step.started", 2*time.Second) {
		t.Fatal("did not receive historical task.started or step.started event")
	}
	close(release)

	if !waitForLine(t, lines, "event: task.finished", 2*time.Second) {
		t.Fatal("did not receive live task.finished event")
	}
}

func TestTaskHandlerCreateAgentRunTaskWithConversationInput(t *testing.T) {
	_, server := newTaskHandlerTestServer(t, nil, false)
	created := createTaskViaHTTP(t, server.URL, map[string]any{
		"task_type": "agent.run",
		"input": map[string]any{
			"conversation_id": "conv_1",
			"provider_id":     "openai",
			"model_id":        "gpt-5.4",
			"message":         "hello",
		},
	})
	if created.Status != coretasks.StatusQueued {
		t.Fatalf("status = %q, want queued", created.Status)
	}
}

func TestTaskHandlerCreateAgentRunTaskCreatesConversationWhenMissing(t *testing.T) {
	manager, conversationStore, server := newTaskHandlerTestServerWithConversationStore(t, nil, false)

	created := createTaskViaHTTP(t, server.URL, map[string]any{
		"task_type":  "agent.run",
		"created_by": "demo-user",
		"input": map[string]any{
			"provider_id": "openai",
			"model_id":    "gpt-5.4",
			"message":     "hello",
		},
	})

	decodedInput := decodeJSONRaw(t, created.InputJSON)
	conversationID, ok := decodedInput["conversation_id"].(string)
	if !ok || conversationID == "" {
		t.Fatalf("input.conversation_id = %#v, want generated conversation id", decodedInput["conversation_id"])
	}
	if created.ConcurrencyKey != "workspace:demo-user:mutable" {
		t.Fatalf("created concurrency key = %q, want per-user mutable workspace lock", created.ConcurrencyKey)
	}

	persisted, err := manager.GetTask(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	persistedInput := decodeJSONRaw(t, persisted.InputJSON)
	if persistedInput["conversation_id"] != conversationID {
		t.Fatalf("persisted input.conversation_id = %#v, want %q", persistedInput["conversation_id"], conversationID)
	}

	conversation, err := conversationStore.GetConversation(context.Background(), conversationID)
	if err != nil {
		t.Fatalf("GetConversation() error = %v", err)
	}
	if conversation.ProviderID != "openai" {
		t.Fatalf("conversation provider = %q, want openai", conversation.ProviderID)
	}
	if conversation.ModelID != "gpt-5.4" {
		t.Fatalf("conversation model = %q, want gpt-5.4", conversation.ModelID)
	}
	if conversation.CreatedBy != "demo-user" {
		t.Fatalf("conversation created_by = %q, want demo-user", conversation.CreatedBy)
	}
}

func TestTaskHandlerRejectsUnauthorizedModelSelection(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.UserSession{}, &models.LLMModelOverride{}, &models.CustomLLMModel{}); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	authLogic, err := logics.NewAuthLogic(db, logics.AuthConfig{CookieName: authSessionCookieName})
	if err != nil {
		t.Fatalf("NewAuthLogic() error = %v", err)
	}
	owner := seedAdminHandlerUser(t, db, "model-owner", "model-owner@example.com", models.UserRoleUser, models.UserStatusActive, true)
	other := seedAdminHandlerUser(t, db, "model-other", "model-other@example.com", models.UserRoleUser, models.UserStatusActive, true)
	_, otherSession, err := authLogic.Login(context.Background(), other.Username, "secret-123")
	if err != nil {
		t.Fatalf("Login(other) error = %v", err)
	}
	codec, err := secret.NewCodec("test-secret")
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	modelLogic, err := logics.NewModelLogic(db, nil, codec)
	if err != nil {
		t.Fatalf("NewModelLogic() error = %v", err)
	}
	_, err = modelLogic.CreateCustomModel(context.Background(), logics.CreateCustomModelInput{
		OwnerUserID:      owner.ID,
		ProviderID:       "owner-provider",
		ModelID:          "owner-model",
		DisplayName:      "Owner Model",
		ProviderType:     coretypes.LLMTypeOpenAICompletions,
		APIKey:           "owner-secret",
		ContextMaxTokens: 32768,
	})
	if err != nil {
		t.Fatalf("CreateCustomModel() error = %v", err)
	}
	taskStore := coretasks.NewStore(db)
	if err := taskStore.AutoMigrate(); err != nil {
		t.Fatalf("task AutoMigrate() error = %v", err)
	}
	conversationStore := coreagent.NewConversationStore(db)
	if err := conversationStore.AutoMigrate(); err != nil {
		t.Fatalf("conversation AutoMigrate() error = %v", err)
	}
	manager := coretasks.NewManager(taskStore, coretasks.ManagerOptions{})
	engine := rest.Init()
	authMiddleware := NewAuthMiddleware(authLogic)
	NewTaskHandler(manager, conversationStore, authMiddleware.RequireActiveUser()).WithModelLogic(modelLogic).Register(engine.Group("/api/v1"))
	server := httptest.NewServer(engine)
	defer server.Close()

	payload := map[string]any{
		"task_type": "agent.run",
		"input": map[string]any{
			"provider_id": "owner-provider",
			"model_id":    "owner-model",
			"message":     "hello",
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/tasks", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: authSessionCookieName, Value: otherSession.ID})
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer response.Body.Close()

	var envelope taskTestResponse
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if envelope.OK {
		t.Fatalf("response OK = true, want unauthorized model selection rejected")
	}
	if envelope.Code != http.StatusUnauthorized {
		t.Fatalf("response code = %d, want %d", envelope.Code, http.StatusUnauthorized)
	}
}

func TestTaskHandlerOverridesSpoofedUserIDBeforePersistingModelSelection(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.UserSession{}, &models.LLMModelOverride{}, &models.CustomLLMModel{}); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	authLogic, err := logics.NewAuthLogic(db, logics.AuthConfig{CookieName: authSessionCookieName})
	if err != nil {
		t.Fatalf("NewAuthLogic() error = %v", err)
	}
	victim := seedAdminHandlerUser(t, db, "victim", "victim@example.com", models.UserRoleUser, models.UserStatusActive, true)
	attacker := seedAdminHandlerUser(t, db, "attacker", "attacker@example.com", models.UserRoleUser, models.UserStatusActive, true)
	_, attackerSession, err := authLogic.Login(context.Background(), attacker.Username, "secret-123")
	if err != nil {
		t.Fatalf("Login(attacker) error = %v", err)
	}
	codec, err := secret.NewCodec("test-secret")
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	modelLogic, err := logics.NewModelLogic(db, nil, codec)
	if err != nil {
		t.Fatalf("NewModelLogic() error = %v", err)
	}
	for _, owner := range []models.User{victim, attacker} {
		_, err = modelLogic.CreateCustomModel(context.Background(), logics.CreateCustomModelInput{
			OwnerUserID:      owner.ID,
			ProviderID:       "shared-provider",
			ModelID:          "shared-model",
			DisplayName:      owner.Username + " Model",
			ProviderType:     coretypes.LLMTypeOpenAICompletions,
			APIKey:           owner.Username + "-secret",
			ContextMaxTokens: 32768,
		})
		if err != nil {
			t.Fatalf("CreateCustomModel(%s) error = %v", owner.Username, err)
		}
	}
	taskStore := coretasks.NewStore(db)
	if err := taskStore.AutoMigrate(); err != nil {
		t.Fatalf("task AutoMigrate() error = %v", err)
	}
	conversationStore := coreagent.NewConversationStore(db)
	if err := conversationStore.AutoMigrate(); err != nil {
		t.Fatalf("conversation AutoMigrate() error = %v", err)
	}
	manager := coretasks.NewManager(taskStore, coretasks.ManagerOptions{})
	engine := rest.Init()
	authMiddleware := NewAuthMiddleware(authLogic)
	NewTaskHandler(manager, conversationStore, authMiddleware.RequireActiveUser()).WithModelLogic(modelLogic).Register(engine.Group("/api/v1"))
	server := httptest.NewServer(engine)
	defer server.Close()

	raw, err := json.Marshal(map[string]any{
		"task_type": "agent.run",
		"input": map[string]any{
			"provider_id": "shared-provider",
			"model_id":    "shared-model",
			"user_id":     fmt.Sprintf("%d", victim.ID),
			"message":     "hello",
		},
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/tasks", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: authSessionCookieName, Value: attackerSession.ID})
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer response.Body.Close()

	var envelope taskTestResponse
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if !envelope.OK {
		t.Fatalf("response OK = false, message = %s", envelope.Message)
	}
	var created coretasks.Task
	if err := json.Unmarshal(envelope.Data, &created); err != nil {
		t.Fatalf("Unmarshal task error = %v", err)
	}
	persisted, err := manager.GetTask(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	input := decodeJSONRaw(t, persisted.InputJSON)
	if input["user_id"] != fmt.Sprintf("%d", attacker.ID) {
		t.Fatalf("persisted input.user_id = %#v, want attacker id %d", input["user_id"], attacker.ID)
	}
	if _, exists := input["UserID"]; exists {
		t.Fatalf("persisted input.UserID exists = %#v, want removed", input["UserID"])
	}
}

func TestTaskHandlerCanonicalizesWorkspaceFieldsFromAuthenticatedUser(t *testing.T) {
	manager, conversationStore, server := newTaskHandlerTestServerWithAuthUser(t, &models.User{ID: 42, Username: "alice", Role: models.UserRoleUser}, nil)
	if _, err := conversationStore.CreateConversation(context.Background(), coreagent.CreateConversationInput{
		ID:         "conv_workspace",
		ProviderID: "openai",
		ModelID:    "gpt-5.4",
		CreatedBy:  "alice",
	}); err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}

	created := createTaskViaHTTP(t, server.URL, map[string]any{
		"task_type":      "agent.run",
		"workspace_mode": "readonly",
		"input": map[string]any{
			"conversation_id":     "conv_workspace",
			"workspace_user_id":   "999",
			"WorkspaceUserID":     "888",
			"workspace_mode":      "mutable",
			"WorkspaceMode":       "mutable",
			"user_id":             "777",
			"UserID":              "666",
			"message":             "hello",
			"workspace_task_root": "client-controlled",
		},
	})

	persisted, err := manager.GetTask(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	input := decodeJSONRaw(t, persisted.InputJSON)
	if input["workspace_user_id"] != "42" {
		t.Fatalf("workspace_user_id = %#v, want authenticated user id", input["workspace_user_id"])
	}
	if input["workspace_mode"] != string(coreworkspaces.ModeReadonly) {
		t.Fatalf("workspace_mode = %#v, want readonly", input["workspace_mode"])
	}
	if input["user_id"] != "42" {
		t.Fatalf("user_id = %#v, want authenticated user id", input["user_id"])
	}
	for _, key := range []string{"WorkspaceUserID", "WorkspaceMode", "UserID", "workspace_task_root"} {
		if _, exists := input[key]; exists {
			t.Fatalf("input[%q] exists = %#v, want removed", key, input[key])
		}
	}
	if persisted.ConcurrencyKey != "conv_workspace" {
		t.Fatalf("readonly concurrency key = %q, want conversation key", persisted.ConcurrencyKey)
	}
}

func TestTaskHandlerMutableWorkspaceModeUsesPerUserConcurrencyKey(t *testing.T) {
	manager, conversationStore, server := newTaskHandlerTestServerWithAuthUser(t, &models.User{ID: 42, Username: "alice", Role: models.UserRoleUser}, nil)
	if _, err := conversationStore.CreateConversation(context.Background(), coreagent.CreateConversationInput{
		ID:         "conv_1",
		ProviderID: "openai",
		ModelID:    "gpt-5.4",
		CreatedBy:  "alice",
	}); err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}

	created := createTaskViaHTTP(t, server.URL, map[string]any{
		"task_type": "agent.run",
		"input": map[string]any{
			"conversation_id": "conv_1",
			"message":         "hello",
		},
	})

	persisted, err := manager.GetTask(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if persisted.ConcurrencyKey != "workspace:42:mutable" {
		t.Fatalf("mutable concurrency key = %q, want per-user workspace lock", persisted.ConcurrencyKey)
	}
	input := decodeJSONRaw(t, persisted.InputJSON)
	if input["workspace_mode"] != string(coreworkspaces.ModeMutable) {
		t.Fatalf("workspace_mode = %#v, want mutable default", input["workspace_mode"])
	}
}

func TestTaskHandlerConfirmAndDiscardWorkspaceUseOwnedWorkspaceUser(t *testing.T) {
	manager, conversationStore, server := newTaskHandlerTestServerWithAuthUser(t, &models.User{ID: 42, Username: "alice", Role: models.UserRoleUser}, &recordingTaskWorkspaceManager{
		confirmState: coreworkspaces.WorkspaceStateFile{TaskID: "tsk_confirmed", UserID: "42", State: coreworkspaces.StateMerged},
		discardState: coreworkspaces.WorkspaceStateFile{TaskID: "tsk_discarded", UserID: "42", State: coreworkspaces.StateDiscarded},
	})
	_ = conversationStore

	created := createTaskViaHTTP(t, server.URL, map[string]any{
		"task_type": "agent.run",
		"input": map[string]any{
			"message": "hello",
		},
	})

	recorder := mustTaskWorkspaceManager(t, server)
	confirm := postWorkspaceAction(t, server.URL, "/api/v1/tasks/"+created.ID+"/workspace/confirm")
	if confirm.State != coreworkspaces.StateMerged {
		t.Fatalf("confirm state = %q, want merged", confirm.State)
	}
	if recorder.confirmUserID != "42" || recorder.confirmTaskID != created.ID {
		t.Fatalf("confirm input = user %q task %q, want authenticated user/task", recorder.confirmUserID, recorder.confirmTaskID)
	}

	discard := postWorkspaceAction(t, server.URL, "/api/v1/tasks/"+created.ID+"/workspace/discard")
	if discard.State != coreworkspaces.StateDiscarded {
		t.Fatalf("discard state = %q, want discarded", discard.State)
	}
	if recorder.discardUserID != "42" || recorder.discardTaskID != created.ID {
		t.Fatalf("discard input = user %q task %q, want authenticated user/task", recorder.discardUserID, recorder.discardTaskID)
	}

	if _, err := manager.GetTask(context.Background(), created.ID); err != nil {
		t.Fatalf("created task should remain readable after workspace actions: %v", err)
	}
}

func TestTaskHandlerWorkspaceActionsRejectAdminForAnotherUsersTask(t *testing.T) {
	recorder := &recordingTaskWorkspaceManager{
		confirmState: coreworkspaces.WorkspaceStateFile{State: coreworkspaces.StateMerged},
		discardState: coreworkspaces.WorkspaceStateFile{State: coreworkspaces.StateDiscarded},
	}
	manager, _, server := newTaskHandlerTestServerWithAuthUser(t, &models.User{ID: 1, Username: "root", Role: models.UserRoleAdmin}, recorder)
	created, err := manager.CreateTask(context.Background(), coretasks.CreateTaskInput{
		TaskType:  "agent.run",
		CreatedBy: "alice",
		Input: map[string]any{
			"workspace_user_id": "42",
			"message":           "hello",
		},
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	for _, path := range []string{
		"/api/v1/tasks/" + created.ID + "/workspace/confirm",
		"/api/v1/tasks/" + created.ID + "/workspace/discard",
	} {
		raw, err := json.Marshal(map[string]any{})
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}
		response, err := http.Post(server.URL+path, "application/json", bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("Post(%s) error = %v", path, err)
		}
		var envelope taskTestResponse
		if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
			response.Body.Close()
			t.Fatalf("Decode(%s) error = %v", path, err)
		}
		response.Body.Close()
		if envelope.OK || envelope.Code != http.StatusUnauthorized {
			t.Fatalf("%s response ok/code = %v/%d, want unauthorized", path, envelope.OK, envelope.Code)
		}
	}

	if recorder.confirmTaskID != "" || recorder.discardTaskID != "" {
		t.Fatalf("workspace manager was called: confirm task %q discard task %q", recorder.confirmTaskID, recorder.discardTaskID)
	}
}

func TestCreateTaskPersistsConversationConcurrencyKey(t *testing.T) {
	manager, server := newTaskHandlerTestServer(t, nil, false)

	created := createTaskViaHTTP(t, server.URL, map[string]any{
		"task_type":      "agent.run",
		"workspace_mode": "readonly",
		"input": map[string]any{
			"conversation_id": "  conv_1  ",
			"provider_id":     "openai",
			"model_id":        "gpt-5.4",
			"message":         "hello",
		},
	})

	if created.ConcurrencyKey != "conv_1" {
		t.Fatalf("created concurrency key = %q, want %q", created.ConcurrencyKey, "conv_1")
	}

	persisted, err := manager.GetTask(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if persisted.ConcurrencyKey != "conv_1" {
		t.Fatalf("persisted concurrency key = %q, want %q", persisted.ConcurrencyKey, "conv_1")
	}
}

func TestTaskHandlerCreateTaskTransparentlyPassesSkillsInInput(t *testing.T) {
	manager, server := newTaskHandlerTestServer(t, nil, false)

	created := createTaskViaHTTP(t, server.URL, map[string]any{
		"task_type": "agent.run",
		"input": map[string]any{
			"conversation_id": "conv_1",
			"provider_id":     "openai",
			"model_id":        "gpt-5.4",
			"message":         "hello",
			"skills":          []string{"debugging", "review"},
		},
	})

	persisted, err := manager.GetTask(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	persistedInput := decodeJSONRaw(t, persisted.InputJSON)
	rawSkills, ok := persistedInput["skills"].([]any)
	if !ok {
		t.Fatalf("persisted input.skills = %#v, want JSON array", persistedInput["skills"])
	}
	if len(rawSkills) != 2 || rawSkills[0] != "debugging" || rawSkills[1] != "review" {
		t.Fatalf("persisted input.skills = %#v, want [debugging review]", rawSkills)
	}
}

func TestCreateTaskAcceptsAttachmentIDsInAgentRunInput(t *testing.T) {
	manager, server := newTaskHandlerTestServer(t, nil, false)

	created := createTaskViaHTTP(t, server.URL, map[string]any{
		"task_type": "agent.run",
		"input": map[string]any{
			"conversation_id": "conv_1",
			"provider_id":     "openai",
			"model_id":        "gpt-5.4",
			"message":         "hello",
			"attachment_ids":  []string{"att_1", "att_2"},
		},
	})

	persisted, err := manager.GetTask(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	persistedInput := decodeJSONRaw(t, persisted.InputJSON)
	rawAttachmentIDs, ok := persistedInput["attachment_ids"].([]any)
	if !ok {
		t.Fatalf("persisted input.attachment_ids = %#v, want JSON array", persistedInput["attachment_ids"])
	}
	if len(rawAttachmentIDs) != 2 || rawAttachmentIDs[0] != "att_1" || rawAttachmentIDs[1] != "att_2" {
		t.Fatalf("persisted input.attachment_ids = %#v, want [att_1 att_2]", rawAttachmentIDs)
	}
}

func TestTaskHandlerAgentRunEndToEndAppendsConversationHistory(t *testing.T) {
	manager, server := newAgentRunTaskTestServer(t)
	first := createTaskViaHTTP(t, server.URL, map[string]any{
		"task_type": "agent.run",
		"input": map[string]any{
			"conversation_id": "conv_1",
			"provider_id":     "openai",
			"model_id":        "gpt-5.4",
			"message":         "first",
		},
	})
	firstDone := waitForTaskState(t, manager, first.ID, coretasks.StatusSucceeded)
	firstResult := decodeJSONRaw(t, firstDone.ResultJSON)
	if firstResult["conversation_id"] != "conv_1" {
		t.Fatalf("conversation_id = %#v, want conv_1", firstResult["conversation_id"])
	}

	second := createTaskViaHTTP(t, server.URL, map[string]any{
		"task_type": "agent.run",
		"input": map[string]any{
			"conversation_id": "conv_1",
			"provider_id":     "openai",
			"model_id":        "gpt-5.4",
			"message":         "second",
		},
	})
	secondDone := waitForTaskState(t, manager, second.ID, coretasks.StatusSucceeded)
	secondResult := decodeJSONRaw(t, secondDone.ResultJSON)
	if secondResult["messages_appended"] != float64(2) {
		t.Fatalf("messages_appended = %#v, want 2", secondResult["messages_appended"])
	}
}

func TestTaskHandlerFindsRunningTaskByConversation(t *testing.T) {
	_, server := newTaskHandlerTestServer(t, nil, false)

	older := createTaskViaHTTP(t, server.URL, map[string]any{
		"task_type": "agent.run",
		"input": map[string]any{
			"conversation_id": "conv_1",
		},
	})
	_ = older
	createTaskViaHTTP(t, server.URL, map[string]any{
		"task_type": "agent.run",
		"input": map[string]any{
			"conversation_id": "conv_other",
		},
	})
	newest := createTaskViaHTTP(t, server.URL, map[string]any{
		"task_type": "agent.run",
		"input": map[string]any{
			"conversation_id": "conv_1",
		},
	})

	request, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/tasks/running?conversation_id=conv_1", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer response.Body.Close()

	got := decodeTaskResponse(t, response.Body)
	if got.ID != newest.ID {
		t.Fatalf("task id = %q, want %q", got.ID, newest.ID)
	}
}

// newTaskHandlerTestServer 构造带任务路由的测试 HTTP 服务。
func newTaskHandlerTestServer(t *testing.T, executor coretasks.Executor, startManager bool) (*coretasks.Manager, *httptest.Server) {
	t.Helper()
	manager, _, server := newTaskHandlerTestServerWithConversationStore(t, executor, startManager)
	return manager, server
}

func newTaskHandlerTestServerWithConversationStore(t *testing.T, executor coretasks.Executor, startManager bool) (*coretasks.Manager, *coreagent.ConversationStore, *httptest.Server) {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	store := coretasks.NewStore(db)
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	conversationStore := coreagent.NewConversationStore(db)
	if err := conversationStore.AutoMigrate(); err != nil {
		t.Fatalf("conversation AutoMigrate() error = %v", err)
	}

	manager := coretasks.NewManager(store, coretasks.ManagerOptions{
		RunnerID:          "handler-test",
		PollInterval:      5 * time.Millisecond,
		LeaseDuration:     100 * time.Millisecond,
		HeartbeatInterval: 20 * time.Millisecond,
	})
	if executor != nil {
		if err := manager.RegisterExecutor("agent.run", executor); err != nil {
			t.Fatalf("RegisterExecutor() error = %v", err)
		}
	}
	if startManager {
		ctx, cancel := context.WithCancel(context.Background())
		manager.Start(ctx)
		t.Cleanup(cancel)
	}

	engine := rest.Init()
	handler := NewTaskHandler(manager, conversationStore)
	handler.Register(engine.Group("/api/v1"))

	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	return manager, conversationStore, server
}

func newTaskHandlerTestServerWithAuthUser(t *testing.T, user *models.User, workspaceManager taskWorkspaceManager) (*coretasks.Manager, *coreagent.ConversationStore, *httptest.Server) {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	store := coretasks.NewStore(db)
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	conversationStore := coreagent.NewConversationStore(db)
	if err := conversationStore.AutoMigrate(); err != nil {
		t.Fatalf("conversation AutoMigrate() error = %v", err)
	}
	manager := coretasks.NewManager(store, coretasks.ManagerOptions{})
	engine := rest.Init()
	authMiddleware := func(c *gin.Context) {
		c.Set(authUserContextKey, user)
		c.Next()
	}
	handler := NewTaskHandler(manager, conversationStore, authMiddleware)
	if workspaceManager != nil {
		handler.WithWorkspaceManager(workspaceManager)
	}
	handler.Register(engine.Group("/api/v1"))
	server := httptest.NewServer(engine)
	if recording, ok := workspaceManager.(*recordingTaskWorkspaceManager); ok {
		taskWorkspaceManagerByServer[server.URL] = recording
		t.Cleanup(func() {
			delete(taskWorkspaceManagerByServer, server.URL)
		})
	}
	t.Cleanup(server.Close)
	return manager, conversationStore, server
}

// createTaskViaHTTP 通过 HTTP 调用创建任务接口。
func createTaskViaHTTP(t *testing.T, baseURL string, payload map[string]any) coretasks.Task {
	t.Helper()
	return postTask(t, baseURL+"/api/v1/tasks", payload)
}

// postTaskAction 通过 HTTP 调用无请求体的任务动作接口。
func postTaskAction(t *testing.T, baseURL string, path string) coretasks.Task {
	t.Helper()
	return postTask(t, baseURL+path, map[string]any{})
}

func postWorkspaceAction(t *testing.T, baseURL string, path string) coreworkspaces.WorkspaceStateFile {
	t.Helper()
	raw, err := json.Marshal(map[string]any{})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	response, err := http.Post(baseURL+path, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("Post() error = %v", err)
	}
	defer response.Body.Close()
	var envelope taskTestResponse
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("Decode() envelope error = %v", err)
	}
	if !envelope.OK {
		t.Fatalf("response ok = false, message = %s, data = %s", envelope.Message, string(envelope.Data))
	}
	var state coreworkspaces.WorkspaceStateFile
	if err := json.Unmarshal(envelope.Data, &state); err != nil {
		t.Fatalf("Unmarshal workspace state error = %v", err)
	}
	return state
}

// postTask 统一发送 POST 请求并解析任务响应。
func postTask(t *testing.T, url string, payload map[string]any) coretasks.Task {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	response, err := http.Post(url, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("Post() error = %v", err)
	}
	defer response.Body.Close()
	return decodeTaskResponse(t, response.Body)
}

// getTaskViaHTTP 通过 HTTP 调用任务详情接口。
func getTaskViaHTTP(t *testing.T, baseURL string, id string) coretasks.Task {
	t.Helper()
	response, err := http.Get(baseURL + "/api/v1/tasks/" + id)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer response.Body.Close()
	return decodeTaskResponse(t, response.Body)
}

// decodeTaskResponse 解码外层 REST 包装并提取任务对象。
func decodeTaskResponse(t *testing.T, body io.Reader) coretasks.Task {
	t.Helper()
	var envelope taskTestResponse
	if err := json.NewDecoder(body).Decode(&envelope); err != nil {
		t.Fatalf("Decode() envelope error = %v", err)
	}
	if !envelope.OK {
		t.Fatalf("response ok = false, raw data = %s", string(envelope.Data))
	}
	var task coretasks.Task
	if err := json.Unmarshal(envelope.Data, &task); err != nil {
		t.Fatalf("Unmarshal() task error = %v", err)
	}
	return task
}

func decodeJSONRaw(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	return got
}

// waitForTaskState 轮询等待任务进入目标状态。
func waitForTaskState(t *testing.T, manager *coretasks.Manager, taskID string, want coretasks.Status) coretasks.Task {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, err := manager.GetTask(context.Background(), taskID)
		if err == nil && task.Status == want {
			return *task
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("task %s did not reach status %q", taskID, want)
	return coretasks.Task{}
}

// waitForLine 从 SSE 响应中等待某一行文本出现。
func waitForLine(t *testing.T, lines <-chan string, want string, timeout time.Duration) bool {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				return false
			}
			if strings.TrimSpace(line) == want {
				return true
			}
		case <-timer.C:
			return false
		}
	}
}

type recordingTaskWorkspaceManager struct {
	confirmUserID string
	confirmTaskID string
	discardUserID string
	discardTaskID string
	confirmState  coreworkspaces.WorkspaceStateFile
	discardState  coreworkspaces.WorkspaceStateFile
}

func (m *recordingTaskWorkspaceManager) ConfirmTaskWorkspace(_ context.Context, userID string, taskID string) (*coreworkspaces.WorkspaceStateFile, error) {
	m.confirmUserID = userID
	m.confirmTaskID = taskID
	state := m.confirmState
	state.TaskID = taskID
	state.UserID = userID
	return &state, nil
}

func (m *recordingTaskWorkspaceManager) DiscardTaskWorkspace(_ context.Context, userID string, taskID string) (*coreworkspaces.WorkspaceStateFile, error) {
	m.discardUserID = userID
	m.discardTaskID = taskID
	state := m.discardState
	state.TaskID = taskID
	state.UserID = userID
	return &state, nil
}

var taskWorkspaceManagerByServer = map[string]*recordingTaskWorkspaceManager{}

func mustTaskWorkspaceManager(t *testing.T, server *httptest.Server) *recordingTaskWorkspaceManager {
	t.Helper()
	manager, ok := taskWorkspaceManagerByServer[server.URL]
	if !ok {
		t.Fatalf("recording task workspace manager not registered for %s", server.URL)
	}
	return manager
}

func newAgentRunTaskTestServer(t *testing.T) (*coretasks.Manager, *httptest.Server) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	store := coretasks.NewStore(db)
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	conversationStore := coreagent.NewConversationStore(db)
	if err := conversationStore.AutoMigrate(); err != nil {
		t.Fatalf("conversation AutoMigrate() error = %v", err)
	}
	promptStore := coreprompt.NewStore(db)
	if db.Migrator().HasTable("prompt_documents") || db.Migrator().HasTable("prompt_bindings") {
		t.Fatal("prompt tables exist before explicit test migration, want newAgentRunTaskTestServer to prepare prompt schema explicitly")
	}
	if err := promptStore.AutoMigrate(); err != nil {
		t.Fatalf("prompt AutoMigrate() error = %v", err)
	}
	if !db.Migrator().HasTable("prompt_documents") || !db.Migrator().HasTable("prompt_bindings") {
		t.Fatal("prompt tables missing after explicit test migration")
	}
	manager := coretasks.NewManager(store, coretasks.ManagerOptions{
		RunnerID:          "handler-test",
		PollInterval:      5 * time.Millisecond,
		LeaseDuration:     100 * time.Millisecond,
		HeartbeatInterval: 20 * time.Millisecond,
	})
	resolver := &coreagent.ModelResolver{Providers: []coretypes.LLMProvider{{
		BaseProvider: coretypes.BaseProvider{Name: "openai"},
		Models:       []coretypes.LLMModel{{BaseModel: coretypes.BaseModel{ID: "gpt-5.4", Name: "GPT 5.4"}, Type: coretypes.LLMTypeOpenAIResponses}},
	}}}
	responses := []string{"first answer", "second answer"}
	if err := manager.RegisterExecutor("agent.run", coreagent.NewTaskExecutor(coreagent.ExecutorDependencies{
		Resolver:          resolver,
		ConversationStore: conversationStore,
		PromptResolver:    coreprompt.NewResolver(promptStore),
		ClientFactory: func(*coretypes.LLMProvider, *coretypes.LLMModel) (model.LlmClient, error) {
			answer := responses[0]
			responses = responses[1:]
			return &stubAgentClient{answer: answer}, nil
		},
	})); err != nil {
		t.Fatalf("RegisterExecutor() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	manager.Start(ctx)
	t.Cleanup(cancel)
	engine := rest.Init()
	handler := NewTaskHandler(manager, conversationStore)
	handler.Register(engine.Group("/api/v1"))
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	return manager, server
}

type stubAgentClient struct{ answer string }

func (s *stubAgentClient) Chat(context.Context, model.ChatRequest) (model.ChatResponse, error) {
	panic("unexpected Chat call")
}

func (s *stubAgentClient) ChatStream(context.Context, model.ChatRequest) (model.Stream, error) {
	return &stubHandlerStream{answer: s.answer}, nil
}

type stubHandlerStream struct{ answer string }

func (s *stubHandlerStream) Recv() (string, error) { return "", nil }
func (s *stubHandlerStream) RecvEvent() (model.StreamEvent, error) {
	if s.answer == "" {
		return model.StreamEvent{}, nil
	}
	answer := s.answer
	s.answer = ""
	return model.StreamEvent{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: answer}}, nil
}
func (s *stubHandlerStream) FinalMessage() (model.Message, error) {
	return model.Message{Role: model.RoleAssistant, Content: "done"}, nil
}
func (s *stubHandlerStream) Close() error                    { return nil }
func (s *stubHandlerStream) Context() context.Context        { return context.Background() }
func (s *stubHandlerStream) Stats() *model.StreamStats       { return &model.StreamStats{} }
func (s *stubHandlerStream) ToolCalls() []coretypes.ToolCall { return nil }
func (s *stubHandlerStream) ResponseType() model.StreamResponseType {
	return model.StreamResponseUnknown
}
func (s *stubHandlerStream) FinishReason() string { return "stop" }
func (s *stubHandlerStream) Reasoning() string    { return "" }
