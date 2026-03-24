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

	coreagent "github.com/EquentR/agent_runtime/core/agent"
	coreprompt "github.com/EquentR/agent_runtime/core/prompt"
	model "github.com/EquentR/agent_runtime/core/providers/types"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	coretypes "github.com/EquentR/agent_runtime/core/types"
	"github.com/EquentR/agent_runtime/pkg/rest"
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

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	store := coretasks.NewStore(db)
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
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
	handler := NewTaskHandler(manager, nil)
	handler.Register(engine.Group("/api/v1"))

	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	return manager, server
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
