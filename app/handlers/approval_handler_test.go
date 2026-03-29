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
	"github.com/EquentR/agent_runtime/core/approvals"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	"github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestApprovalHandlerListApprovalsEnforcesOwnershipAndReturnsRecords(t *testing.T) {
	deps, server := newApprovalHandlerTestServer(t)
	ownerCookie := registerAndLoginAuthUser(t, server.URL, "alice")
	otherCookie := registerAndLoginAuthUser(t, server.URL, "bob")

	task := createTaskAsUser(t, server.URL, ownerCookie, map[string]any{
		"task_type":  "agent.run",
		"created_by": "alice",
	})
	_, err := deps.approvalStore.CreateApproval(context.Background(), approvals.CreateApprovalInput{
		TaskID:           task.ID,
		ConversationID:   "conv-1",
		StepIndex:        2,
		ToolCallID:       "call-1",
		ToolName:         "bash",
		ArgumentsSummary: "rm -rf /tmp/demo",
		RiskLevel:        "high",
		Reason:           "dangerous filesystem mutation",
	})
	if err != nil {
		t.Fatalf("CreateApproval() error = %v", err)
	}

	ownerEnvelope := getApprovalListEnvelope(t, server.URL, ownerCookie, task.ID)
	if !ownerEnvelope.OK {
		t.Fatalf("owner list OK = false, message = %q", ownerEnvelope.Message)
	}
	var listed []approvals.ToolApproval
	if err := json.Unmarshal(ownerEnvelope.Data, &listed); err != nil {
		t.Fatalf("json.Unmarshal(listed) error = %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("len(listed) = %d, want 1", len(listed))
	}
	if listed[0].ToolName != "bash" {
		t.Fatalf("tool_name = %q, want %q", listed[0].ToolName, "bash")
	}
	if listed[0].Status != approvals.StatusPending {
		t.Fatalf("status = %q, want %q", listed[0].Status, approvals.StatusPending)
	}

	otherEnvelope := getApprovalListEnvelope(t, server.URL, otherCookie, task.ID)
	if otherEnvelope.OK {
		t.Fatal("other user list unexpectedly succeeded")
	}
	if otherEnvelope.Code != http.StatusUnauthorized {
		t.Fatalf("other user code = %d, want %d", otherEnvelope.Code, http.StatusUnauthorized)
	}
}

func TestApprovalHandlerDecisionResolvesApprovalAndResumesWaitingTaskOnce(t *testing.T) {
	deps, server := newApprovalHandlerTestServer(t)
	ownerCookie := registerAndLoginAuthUser(t, server.URL, "alice")

	task, approval := createWaitingTaskWithApproval(t, deps, "alice")

	response := postApprovalDecisionEnvelope(t, server.URL, ownerCookie, task.ID, approval.ID, map[string]any{
		"decision": "approve",
		"reason":   "safe",
	})
	if !response.OK {
		t.Fatalf("decision OK = false, message = %q", response.Message)
	}

	var resolved approvals.ToolApproval
	if err := json.Unmarshal(response.Data, &resolved); err != nil {
		t.Fatalf("json.Unmarshal(resolved) error = %v", err)
	}
	if resolved.Status != approvals.StatusApproved {
		t.Fatalf("resolved status = %q, want %q", resolved.Status, approvals.StatusApproved)
	}
	if resolved.DecisionReason != "safe" {
		t.Fatalf("decision_reason = %q, want %q", resolved.DecisionReason, "safe")
	}
	if resolved.DecisionBy != "alice" {
		t.Fatalf("decision_by = %q, want %q", resolved.DecisionBy, "alice")
	}

	queued := waitForApprovalTaskState(t, deps.taskManager, task.ID, coretasks.StatusQueued)
	if queued.SuspendReason != "" {
		t.Fatalf("queued suspend_reason = %q, want empty", queued.SuspendReason)
	}

	secondResponse := postApprovalDecisionEnvelope(t, server.URL, ownerCookie, task.ID, approval.ID, map[string]any{
		"decision": "approve",
		"reason":   "still safe",
	})
	if !secondResponse.OK {
		t.Fatalf("second decision OK = false, message = %q", secondResponse.Message)
	}

	events, err := deps.taskManager.ListEvents(context.Background(), task.ID, 0, 20)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	resumedCount := 0
	approvalResolvedCount := 0
	for _, event := range events {
		switch event.EventType {
		case coretasks.EventTaskResumed:
			resumedCount++
		case coretasks.EventApprovalResolved:
			approvalResolvedCount++
		}
	}
	if resumedCount != 1 {
		t.Fatalf("task.resumed count = %d, want 1", resumedCount)
	}
	if approvalResolvedCount != 1 {
		t.Fatalf("approval.resolved count = %d, want 1", approvalResolvedCount)
	}
}

func TestApprovalHandlerListApprovalsMatchesTaskOneContract(t *testing.T) {
	deps, server := newApprovalHandlerTestServer(t)
	ownerCookie := registerAndLoginAuthUser(t, server.URL, "alice")

	task := createTaskAsUser(t, server.URL, ownerCookie, map[string]any{
		"task_type":  "agent.run",
		"created_by": "alice",
	})
	approval, err := deps.approvalStore.CreateApproval(context.Background(), approvals.CreateApprovalInput{
		TaskID:           task.ID,
		ConversationID:   "conv-1",
		StepIndex:        2,
		ToolCallID:       "call-1",
		ToolName:         "bash",
		ArgumentsSummary: "rm -rf /tmp/demo",
		RiskLevel:        "high",
		Reason:           "dangerous filesystem mutation",
	})
	if err != nil {
		t.Fatalf("CreateApproval() error = %v", err)
	}

	envelope := getApprovalListEnvelope(t, server.URL, ownerCookie, task.ID)
	if !envelope.OK {
		t.Fatalf("list OK = false, message = %q", envelope.Message)
	}

	var data []map[string]any
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatalf("json.Unmarshal(data) error = %v", err)
	}
	if len(data) != 1 {
		t.Fatalf("len(data) = %d, want 1", len(data))
	}
	assertExactMapKeys(t, data[0], "id", "task_id", "conversation_id", "step_index", "tool_call_id", "tool_name", "arguments_summary", "risk_level", "reason", "status", "decision_by", "decision_reason", "decision_at", "created_at", "updated_at")
	if data[0]["id"] != approval.ID {
		t.Fatalf("approval id = %#v, want %q", data[0]["id"], approval.ID)
	}
	if data[0]["reason"] != "dangerous filesystem mutation" {
		t.Fatalf("approval reason = %#v, want %q", data[0]["reason"], "dangerous filesystem mutation")
	}
}

func TestApprovalHandlerDecisionReturnsBadRequestForInvalidDecision(t *testing.T) {
	deps, server := newApprovalHandlerTestServer(t)
	ownerCookie := registerAndLoginAuthUser(t, server.URL, "alice")
	task, approval := createWaitingTaskWithApproval(t, deps, "alice")

	envelope := postApprovalDecisionEnvelope(t, server.URL, ownerCookie, task.ID, approval.ID, map[string]any{
		"decision": "maybe",
		"reason":   "unclear",
	})
	if envelope.OK {
		t.Fatal("invalid decision unexpectedly succeeded")
	}
	if envelope.Code != http.StatusBadRequest {
		t.Fatalf("envelope.Code = %d, want %d", envelope.Code, http.StatusBadRequest)
	}
}

func TestApprovalHandlerDecisionRejectsNonOwner(t *testing.T) {
	deps, server := newApprovalHandlerTestServer(t)
	registerAndLoginAuthUser(t, server.URL, "alice")
	otherCookie := registerAndLoginAuthUser(t, server.URL, "bob")
	task, approval := createWaitingTaskWithApproval(t, deps, "alice")

	envelope := postApprovalDecisionEnvelope(t, server.URL, otherCookie, task.ID, approval.ID, map[string]any{
		"decision": "reject",
		"reason":   "no",
	})
	if envelope.OK {
		t.Fatal("non-owner decision unexpectedly succeeded")
	}
	if envelope.Code != http.StatusUnauthorized {
		t.Fatalf("envelope.Code = %d, want %d", envelope.Code, http.StatusUnauthorized)
	}

	loaded, err := deps.approvalStore.GetApproval(context.Background(), task.ID, approval.ID)
	if err != nil {
		t.Fatalf("GetApproval() error = %v", err)
	}
	if loaded.Status != approvals.StatusPending {
		t.Fatalf("approval status = %q, want %q", loaded.Status, approvals.StatusPending)
	}
}

func TestApprovalHandlerListApprovalsReturnsNewestFirst(t *testing.T) {
	deps, server := newApprovalHandlerTestServer(t)
	ownerCookie := registerAndLoginAuthUser(t, server.URL, "alice")

	task := createTaskAsUser(t, server.URL, ownerCookie, map[string]any{
		"task_type":  "agent.run",
		"created_by": "alice",
	})
	first, err := deps.approvalStore.CreateApproval(context.Background(), approvals.CreateApprovalInput{TaskID: task.ID, ToolCallID: "call-1", ToolName: "bash"})
	if err != nil {
		t.Fatalf("CreateApproval() first error = %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	second, err := deps.approvalStore.CreateApproval(context.Background(), approvals.CreateApprovalInput{TaskID: task.ID, ToolCallID: "call-2", ToolName: "delete_file"})
	if err != nil {
		t.Fatalf("CreateApproval() second error = %v", err)
	}

	envelope := getApprovalListEnvelope(t, server.URL, ownerCookie, task.ID)
	if !envelope.OK {
		t.Fatalf("list OK = false, message = %q", envelope.Message)
	}
	var listed []approvals.ToolApproval
	if err := json.Unmarshal(envelope.Data, &listed); err != nil {
		t.Fatalf("json.Unmarshal(listed) error = %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("len(listed) = %d, want 2", len(listed))
	}
	if listed[0].ID != second.ID {
		t.Fatalf("listed[0].ID = %q, want %q", listed[0].ID, second.ID)
	}
	if listed[1].ID != first.ID {
		t.Fatalf("listed[1].ID = %q, want %q", listed[1].ID, first.ID)
	}
}

func TestApprovalHandlerListApprovalsReturnsNotFoundForMissingTask(t *testing.T) {
	_, server := newApprovalHandlerTestServer(t)
	ownerCookie := registerAndLoginAuthUser(t, server.URL, "alice")

	envelope := getApprovalListEnvelope(t, server.URL, ownerCookie, "task_missing")
	if envelope.OK {
		t.Fatal("missing task list unexpectedly succeeded")
	}
	if envelope.Code != http.StatusNotFound {
		t.Fatalf("envelope.Code = %d, want %d", envelope.Code, http.StatusNotFound)
	}
}

func TestApprovalHandlerDecisionReturnsNotFoundForMissingApproval(t *testing.T) {
	deps, server := newApprovalHandlerTestServer(t)
	ownerCookie := registerAndLoginAuthUser(t, server.URL, "alice")
	task, _ := createWaitingTaskWithApproval(t, deps, "alice")

	envelope := postApprovalDecisionEnvelope(t, server.URL, ownerCookie, task.ID, "approval_missing", map[string]any{
		"decision": "approve",
		"reason":   "safe",
	})
	if envelope.OK {
		t.Fatal("missing approval decision unexpectedly succeeded")
	}
	if envelope.Code != http.StatusNotFound {
		t.Fatalf("envelope.Code = %d, want %d", envelope.Code, http.StatusNotFound)
	}
}

type approvalHandlerTestDeps struct {
	authLogic         *logics.AuthLogic
	conversationStore *coreagent.ConversationStore
	taskStore         *coretasks.Store
	taskManager       *coretasks.Manager
	approvalStore     *approvals.Store
}

func newApprovalHandlerTestServer(t *testing.T) (*approvalHandlerTestDeps, *httptest.Server) {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
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
	approvalStore := approvals.NewStore(db)
	if err := approvalStore.AutoMigrate(); err != nil {
		t.Fatalf("approval AutoMigrate() error = %v", err)
	}

	taskManager := coretasks.NewManager(taskStore, coretasks.ManagerOptions{RunnerID: "approval-handler-test", ApprovalStore: approvalStore})
	authLogic := newAuthLogicForTest(t, db)
	authMiddleware := NewAuthMiddleware(authLogic)

	engine := rest.Init()
	group := engine.Group("/api/v1")
	NewAuthHandler(authLogic).Register(group)
	NewTaskHandler(taskManager, conversationStore, authMiddleware.RequireSession()).Register(group)
	NewApprovalHandler(taskManager, approvalStore, conversationStore, authMiddleware.RequireSession()).Register(group)

	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	return &approvalHandlerTestDeps{
		authLogic:         authLogic,
		conversationStore: conversationStore,
		taskStore:         taskStore,
		taskManager:       taskManager,
		approvalStore:     approvalStore,
	}, server
}

func registerAndLoginAuthUser(t *testing.T, baseURL string, username string) *http.Cookie {
	t.Helper()
	registerResponse := postAuthJSON(t, baseURL+"/api/v1/auth/register", map[string]any{
		"username":         username,
		"password":         "password123",
		"confirm_password": "password123",
	})
	registerResponse.Body.Close()

	loginResponse := postAuthJSON(t, baseURL+"/api/v1/auth/login", map[string]any{
		"username": username,
		"password": "password123",
	})
	defer loginResponse.Body.Close()
	return mustFindCookie(t, loginResponse.Cookies(), authSessionCookieName)
}

func createTaskAsUser(t *testing.T, baseURL string, cookie *http.Cookie, payload map[string]any) coretasks.Task {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, baseURL+"/api/v1/tasks", bytes.NewReader(mustJSON(t, payload)))
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
	return decodeTaskResponse(t, response.Body)
}

func getApprovalListEnvelope(t *testing.T, baseURL string, cookie *http.Cookie, taskID string) taskTestResponse {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, baseURL+"/api/v1/tasks/"+taskID+"/approvals", nil)
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

func postApprovalDecisionEnvelope(t *testing.T, baseURL string, cookie *http.Cookie, taskID string, approvalID string, payload map[string]any) taskTestResponse {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, baseURL+"/api/v1/tasks/"+taskID+"/approvals/"+approvalID+"/decision", bytes.NewReader(mustJSON(t, payload)))
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

func createWaitingTaskWithApproval(t *testing.T, deps *approvalHandlerTestDeps, createdBy string) (*coretasks.Task, *approvals.ToolApproval) {
	t.Helper()
	task, err := deps.taskManager.CreateTask(context.Background(), coretasks.CreateTaskInput{TaskType: "agent.run", CreatedBy: createdBy})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	claimed, _, err := deps.taskStore.ClaimNextTask(context.Background(), "approval-handler-test", 30*time.Second)
	if err != nil {
		t.Fatalf("ClaimNextTask() error = %v", err)
	}
	if claimed == nil || claimed.ID != task.ID {
		t.Fatalf("claimed task = %#v, want %q", claimed, task.ID)
	}
	if _, _, err := deps.taskStore.MarkWaiting(context.Background(), task.ID, "waiting_for_tool_approval"); err != nil {
		t.Fatalf("MarkWaiting() error = %v", err)
	}
	approval, err := deps.approvalStore.CreateApproval(context.Background(), approvals.CreateApprovalInput{
		TaskID:           task.ID,
		ConversationID:   "conv-1",
		StepIndex:        2,
		ToolCallID:       "call-1",
		ToolName:         "bash",
		ArgumentsSummary: "rm -rf /tmp/demo",
		RiskLevel:        "high",
		Reason:           "dangerous filesystem mutation",
	})
	if err != nil {
		t.Fatalf("CreateApproval() error = %v", err)
	}
	return task, approval
}

func waitForApprovalTaskState(t *testing.T, manager *coretasks.Manager, taskID string, want coretasks.Status) *coretasks.Task {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, err := manager.GetTask(context.Background(), taskID)
		if err == nil && task.Status == want {
			return task
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("task %s did not reach status %q", taskID, want)
	return nil
}

func assertExactMapKeys(t *testing.T, got map[string]any, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("map key count = %d, want %d; got = %#v", len(got), len(want), got)
	}
	for _, key := range want {
		if _, ok := got[key]; !ok {
			t.Fatalf("missing key %q in map %#v", key, got)
		}
	}
}
