package commands

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/EquentR/agent_runtime/app/router"
	coreagent "github.com/EquentR/agent_runtime/core/agent"
	coreaudit "github.com/EquentR/agent_runtime/core/audit"
	model "github.com/EquentR/agent_runtime/core/providers/types"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	coretypes "github.com/EquentR/agent_runtime/core/types"
	"github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type serveRouterEnvelope struct {
	Code int  `json:"code"`
	OK   bool `json:"ok"`
}

func TestBuildRouterDependenciesExposesAuditRoutes(t *testing.T) {
	db := newServeTestDB(t)
	auditStore := coreaudit.NewStore(db)
	if err := auditStore.AutoMigrate(); err != nil {
		t.Fatalf("audit store migrate error = %v", err)
	}

	engine := rest.Init()
	router.Init(engine, "/api/v1", nil, buildRouterDependencies(nil, nil, auditStore, nil, nil))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/audit/runs/run_1", nil)
	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 so the route is registered", recorder.Code)
	}
	var envelope serveRouterEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("Unmarshal() envelope error = %v, body = %s", err, recorder.Body.String())
	}
	if envelope.OK {
		t.Fatal("envelope.OK = true, want unauthorized failure for anonymous request")
	}
	if envelope.Code != http.StatusUnauthorized {
		t.Fatalf("envelope.Code = %d, want %d", envelope.Code, http.StatusUnauthorized)
	}
}

func TestInitAuditRuntimeSharesRecorderAcrossTaskAndAgentPaths(t *testing.T) {
	db := newServeTestDB(t)
	runtime, err := initAuditRuntime(db)
	if err != nil {
		t.Fatalf("initAuditRuntime() error = %v", err)
	}
	if runtime.Store == nil {
		t.Fatal("runtime.Store = nil, want audit store")
	}
	if runtime.RunRecorder == nil {
		t.Fatal("runtime.RunRecorder = nil, want audit recorder")
	}
	if runtime.TaskRecorder == nil {
		t.Fatal("runtime.TaskRecorder = nil, want task audit recorder")
	}
	taskRecorder, ok := runtime.TaskRecorder.(*taskAuditRecorder)
	if !ok {
		t.Fatalf("runtime.TaskRecorder type = %T, want *taskAuditRecorder", runtime.TaskRecorder)
	}
	if taskRecorder.recorder != runtime.RunRecorder {
		t.Fatal("task and agent audit paths should share one recorder instance")
	}
}

func TestRegisterAgentRunExecutorWiresExecutorAuditRecorder(t *testing.T) {
	db := newServeTestDB(t)
	taskStore := coretasks.NewStore(db)
	if err := taskStore.AutoMigrate(); err != nil {
		t.Fatalf("task store migrate error = %v", err)
	}
	conversationStore := coreagent.NewConversationStore(db)
	if err := conversationStore.AutoMigrate(); err != nil {
		t.Fatalf("conversation store migrate error = %v", err)
	}
	auditStore := coreaudit.NewStore(db)
	if err := auditStore.AutoMigrate(); err != nil {
		t.Fatalf("audit store migrate error = %v", err)
	}

	recorder := coreaudit.NewRecorder(auditStore)
	manager := coretasks.NewManager(taskStore, coretasks.ManagerOptions{
		RunnerID:          "serve-test",
		PollInterval:      5 * time.Millisecond,
		LeaseDuration:     100 * time.Millisecond,
		HeartbeatInterval: 20 * time.Millisecond,
		AuditRecorder:     newTaskAuditRecorder(recorder),
	})
	if err := registerAgentRunExecutor(manager, &coreagent.ModelResolver{Providers: []coretypes.LLMProvider{{
		BaseProvider: coretypes.BaseProvider{Name: "openai"},
		Models:       []coretypes.LLMModel{{BaseModel: coretypes.BaseModel{ID: "gpt-5.4", Name: "GPT 5.4"}, Type: coretypes.LLMTypeOpenAIResponses}},
	}}}, conversationStore, nil, func(*coretypes.LLMProvider, *coretypes.LLMModel) (model.LlmClient, error) {
		return &serveStubClient{answer: "hello"}, nil
	}, recorder); err != nil {
		t.Fatalf("registerAgentRunExecutor() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.Start(ctx)

	created, err := manager.CreateTask(ctx, coretasks.CreateTaskInput{
		TaskType:  "agent.run",
		CreatedBy: "tester",
		Input: coreagent.RunTaskInput{
			ConversationID: "conv_1",
			ProviderID:     "openai",
			ModelID:        "gpt-5.4",
			Message:        "hi",
			CreatedBy:      "tester",
		},
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	waitForServeTestTaskStatus(t, ctx, manager, created.ID, coretasks.StatusSucceeded)

	run := waitForServeTestAuditRun(t, ctx, auditStore, created.ID)
	eventTypes := waitForServeTestAuditEvents(t, db, created.ID, "conversation.loaded", "user_message.appended", "messages.persisted")
	if len(eventTypes) < 3 {
		t.Fatalf("audit event types = %v, want executor audit events", eventTypes)
	}

	var artifacts []coreaudit.Artifact
	if err := db.WithContext(ctx).Where("run_id = ?", run.ID).Find(&artifacts).Error; err != nil {
		t.Fatalf("load artifacts error = %v", err)
	}
	if !hasServeTestArtifactKind(artifacts, coreaudit.ArtifactKindRequestMessages) {
		t.Fatalf("artifact kinds = %#v, want %q", artifacts, coreaudit.ArtifactKindRequestMessages)
	}
}

type serveStubClient struct{ answer string }

func (s *serveStubClient) Chat(context.Context, model.ChatRequest) (model.ChatResponse, error) {
	panic("unexpected Chat call")
}

func (s *serveStubClient) ChatStream(context.Context, model.ChatRequest) (model.Stream, error) {
	return &serveStubStream{answer: s.answer}, nil
}

type serveStubStream struct{ answer string }

func (s *serveStubStream) Recv() (string, error) { return "", nil }

func (s *serveStubStream) RecvEvent() (model.StreamEvent, error) {
	if s.answer == "" {
		return model.StreamEvent{}, nil
	}
	answer := s.answer
	s.answer = ""
	return model.StreamEvent{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: answer}}, nil
}

func (s *serveStubStream) FinalMessage() (model.Message, error) {
	return model.Message{Role: model.RoleAssistant, Content: "hello"}, nil
}

func (s *serveStubStream) Close() error                    { return nil }
func (s *serveStubStream) Context() context.Context        { return context.Background() }
func (s *serveStubStream) Stats() *model.StreamStats       { return &model.StreamStats{} }
func (s *serveStubStream) ToolCalls() []coretypes.ToolCall { return nil }
func (s *serveStubStream) ResponseType() model.StreamResponseType {
	return model.StreamResponseUnknown
}
func (s *serveStubStream) FinishReason() string { return "stop" }
func (s *serveStubStream) Reasoning() string    { return "" }

func newServeTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open db error = %v", err)
	}
	return db
}

func waitForServeTestTaskStatus(t *testing.T, ctx context.Context, manager *coretasks.Manager, taskID string, want coretasks.Status) *coretasks.Task {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, err := manager.GetTask(ctx, taskID)
		if err == nil && task != nil && task.Status == want {
			return task
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("task %s did not reach status %q", taskID, want)
	return nil
}

func waitForServeTestAuditRun(t *testing.T, ctx context.Context, store *coreaudit.Store, taskID string) *coreaudit.Run {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		run, err := store.GetRunByTaskID(ctx, taskID)
		if err == nil && run != nil {
			return run
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("task %s did not create audit run", taskID)
	return nil
}

func waitForServeTestAuditEvents(t *testing.T, db *gorm.DB, taskID string, want ...string) []string {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var events []coreaudit.Event
		if err := db.Where("task_id = ?", taskID).Order("seq asc").Find(&events).Error; err != nil {
			t.Fatalf("load audit events error = %v", err)
		}
		got := make([]string, 0, len(events))
		for _, event := range events {
			got = append(got, event.EventType)
		}
		if containsServeTestEventTypes(got, want...) {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("task %s did not record audit events %v", taskID, want)
	return nil
}

func containsServeTestEventTypes(got []string, want ...string) bool {
	for _, target := range want {
		found := false
		for _, candidate := range got {
			if candidate == target {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func hasServeTestArtifactKind(artifacts []coreaudit.Artifact, kind coreaudit.ArtifactKind) bool {
	for _, artifact := range artifacts {
		if artifact.Kind == kind {
			return true
		}
	}
	return false
}
