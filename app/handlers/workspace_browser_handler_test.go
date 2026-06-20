package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EquentR/agent_runtime/app/logics"
	coreagent "github.com/EquentR/agent_runtime/core/agent"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	coreworkspaces "github.com/EquentR/agent_runtime/core/workspaces"
	"github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestConversationWorkspaceBrowserListsFilesFromLatestRelatedTask(t *testing.T) {
	env := newWorkspaceBrowserHandlerTestEnv(t)
	ctx := context.Background()
	if _, err := env.conversations.CreateConversation(ctx, coreagent.CreateConversationInput{ID: "conv_1", ProviderID: "openai", ModelID: "gpt-5.4", CreatedBy: "alice"}); err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}

	active := env.createTask(t, "conv_1", "42")
	activeWorkspace := env.createWorkspace(t, "42", active.ID, coreworkspaces.ModeReadonly)
	writeWorkspaceBrowserTestFile(t, activeWorkspace.Root, "stale.txt", "active only")

	latest := env.createTask(t, "conv_1", "42")
	latestWorkspace := env.createWorkspace(t, "42", latest.ID, coreworkspaces.ModeReadonly)
	writeWorkspaceBrowserTestFile(t, latestWorkspace.Root, "fresh.txt", "latest file")
	if _, _, err := env.taskStore.ClaimNextTask(ctx, "runner-latest", time.Minute); err != nil {
		t.Fatalf("ClaimNextTask(latest) error = %v", err)
	}
	if _, _, err := env.taskStore.MarkSucceeded(ctx, latest.ID, map[string]any{"conversation_id": "conv_1"}); err != nil {
		t.Fatalf("MarkSucceeded(latest) error = %v", err)
	}

	response := workspaceBrowserGet(t, env, env.server.URL+"/api/v1/conversations/conv_1/workspace/files", nil)
	defer response.Body.Close()
	got := decodeWorkspaceSnapshotResponse(t, response.Body)

	if got.TaskID != latest.ID {
		t.Fatalf("snapshot.TaskID = %q, want latest related task %q", got.TaskID, latest.ID)
	}
	if !workspaceTreeContains(got.Tree, "fresh.txt") {
		t.Fatalf("tree = %#v, want fresh.txt from latest task workspace", got.Tree)
	}
	if workspaceTreeContains(got.Tree, "stale.txt") {
		t.Fatalf("tree = %#v, did not want stale.txt from active task workspace", got.Tree)
	}
}

func TestConversationWorkspaceBrowserReturnsFileDetail(t *testing.T) {
	env := newWorkspaceBrowserHandlerTestEnv(t)
	env.seedConversationTaskWorkspace(t, "conv_1", "42", "notes.md", "hello workspace\n")

	response := workspaceBrowserGet(t, env, env.server.URL+"/api/v1/conversations/conv_1/workspace/file", map[string]string{"path": "notes.md"})
	defer response.Body.Close()
	got := decodeWorkspaceFileResponse(t, response.Body)

	if got.Path != "notes.md" || got.Name != "notes.md" || got.Content != "hello workspace\n" || got.Binary {
		t.Fatalf("file detail = %#v, want text notes.md content", got)
	}
}

func TestConversationWorkspaceBrowserReturnsDiffAgainstHomeWorkspace(t *testing.T) {
	env := newWorkspaceBrowserHandlerTestEnv(t)
	home, err := env.workspaces.EnsureHomeWorkspace(context.Background(), "42")
	if err != nil {
		t.Fatalf("EnsureHomeWorkspace() error = %v", err)
	}
	writeWorkspaceBrowserTestFile(t, home.Root, "notes.md", "home line\n")
	env.seedConversationTaskWorkspace(t, "conv_1", "42", "notes.md", "task line\n")

	response := workspaceBrowserGet(t, env, env.server.URL+"/api/v1/conversations/conv_1/workspace/diff", map[string]string{"path": "notes.md"})
	defer response.Body.Close()
	got := decodeWorkspaceDiffResponse(t, response.Body)

	if got.Path != "notes.md" || got.Binary {
		t.Fatalf("diff = %#v, want text diff for notes.md", got)
	}
	if !strings.Contains(got.UnifiedDiff, "-home line") || !strings.Contains(got.UnifiedDiff, "+task line") {
		t.Fatalf("UnifiedDiff = %q, want home removal and task addition", got.UnifiedDiff)
	}
}

func TestConversationWorkspaceBrowserDownloadsFileContent(t *testing.T) {
	env := newWorkspaceBrowserHandlerTestEnv(t)
	env.seedConversationTaskWorkspace(t, "conv_1", "42", "notes.md", "download me\n")

	response := workspaceBrowserGet(t, env, env.server.URL+"/api/v1/conversations/conv_1/workspace/download", map[string]string{"path": "notes.md"})
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll(download) error = %v", err)
	}

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", response.StatusCode, string(body))
	}
	if string(body) != "download me\n" {
		t.Fatalf("download body = %q, want file content", string(body))
	}
	if disposition := response.Header.Get("Content-Disposition"); !strings.Contains(disposition, "notes.md") {
		t.Fatalf("Content-Disposition = %q, want filename", disposition)
	}
}

type workspaceBrowserHandlerTestEnv struct {
	db            *gorm.DB
	conversations *coreagent.ConversationStore
	taskStore     *coretasks.Store
	taskManager   *coretasks.Manager
	workspaces    *coreworkspaces.Manager
	authLogic     *logics.AuthLogic
	server        *httptest.Server
}

type workspaceBrowserSnapshotResponse struct {
	ConversationID string                         `json:"conversation_id"`
	TaskID         string                         `json:"task_id"`
	UserID         string                         `json:"user_id"`
	Tree           *workspaceBrowserTreeNodeEntry `json:"tree"`
}

type workspaceBrowserTreeNodeEntry struct {
	Path     string                          `json:"path"`
	Name     string                          `json:"name"`
	Type     string                          `json:"type"`
	Children []workspaceBrowserTreeNodeEntry `json:"children"`
}

type workspaceBrowserFileResponse struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Binary  bool   `json:"binary"`
	Content string `json:"content"`
}

type workspaceBrowserDiffResponse struct {
	Path        string `json:"path"`
	Binary      bool   `json:"binary"`
	UnifiedDiff string `json:"unified_diff"`
}

func newWorkspaceBrowserHandlerTestEnv(t *testing.T) *workspaceBrowserHandlerTestEnv {
	t.Helper()

	dsn := fmt.Sprintf("file:%s-workspace-browser?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	conversations := coreagent.NewConversationStore(db)
	if err := conversations.AutoMigrate(); err != nil {
		t.Fatalf("conversation AutoMigrate() error = %v", err)
	}
	taskStore := coretasks.NewStore(db)
	if err := taskStore.AutoMigrate(); err != nil {
		t.Fatalf("task AutoMigrate() error = %v", err)
	}
	templateRoot := t.TempDir()
	workspacesRoot := t.TempDir()
	writeWorkspaceBrowserTestFile(t, templateRoot, "AGENTS.md", "# Workspace rules\n")
	workspaces, err := coreworkspaces.NewManager(coreworkspaces.Config{TemplateRoot: templateRoot, Root: workspacesRoot})
	if err != nil {
		t.Fatalf("workspaces.NewManager() error = %v", err)
	}
	taskManager := coretasks.NewManager(taskStore, coretasks.ManagerOptions{RunnerID: "workspace-browser-test"})
	authLogic := newAuthLogicForTest(t, db)
	authMiddleware := NewAuthMiddleware(authLogic)

	engine := rest.Init()
	NewWorkspaceBrowserHandler(conversations, taskManager, workspaces, authMiddleware.RequireSession()).Register(engine.Group("/api/v1"))
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)

	registerActiveAuthUserForTest(t, authLogic, "alice", "secret-123")
	return &workspaceBrowserHandlerTestEnv{
		db:            db,
		conversations: conversations,
		taskStore:     taskStore,
		taskManager:   taskManager,
		workspaces:    workspaces,
		authLogic:     authLogic,
		server:        server,
	}
}

func (e *workspaceBrowserHandlerTestEnv) createTask(t *testing.T, conversationID string, workspaceUserID string) *coretasks.Task {
	t.Helper()
	task, err := e.taskManager.CreateTask(context.Background(), coretasks.CreateTaskInput{
		TaskType:  "agent.run",
		CreatedBy: "alice",
		Input: map[string]any{
			"conversation_id":    conversationID,
			"workspace_user_id": workspaceUserID,
			"workspace_mode":    string(coreworkspaces.ModeReadonly),
		},
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	return task
}

func (e *workspaceBrowserHandlerTestEnv) createWorkspace(t *testing.T, userID string, workspaceID string, mode coreworkspaces.Mode) *coreworkspaces.Workspace {
	t.Helper()
	workspace, err := e.workspaces.CreateTaskWorkspace(context.Background(), userID, workspaceID, mode)
	if err != nil {
		t.Fatalf("CreateTaskWorkspace(%q) error = %v", workspaceID, err)
	}
	return workspace
}

func (e *workspaceBrowserHandlerTestEnv) seedConversationTaskWorkspace(t *testing.T, conversationID string, workspaceUserID string, path string, content string) *coretasks.Task {
	t.Helper()
	if _, err := e.conversations.CreateConversation(context.Background(), coreagent.CreateConversationInput{ID: conversationID, ProviderID: "openai", ModelID: "gpt-5.4", CreatedBy: "alice"}); err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	task := e.createTask(t, conversationID, workspaceUserID)
	workspace := e.createWorkspace(t, workspaceUserID, task.ID, coreworkspaces.ModeReadonly)
	writeWorkspaceBrowserTestFile(t, workspace.Root, path, content)
	if _, _, err := e.taskStore.ClaimNextTask(context.Background(), "runner", time.Minute); err != nil {
		t.Fatalf("ClaimNextTask() error = %v", err)
	}
	if _, _, err := e.taskStore.MarkSucceeded(context.Background(), task.ID, map[string]any{"conversation_id": conversationID}); err != nil {
		t.Fatalf("MarkSucceeded() error = %v", err)
	}
	return task
}

func workspaceBrowserGet(t *testing.T, env *workspaceBrowserHandlerTestEnv, rawURL string, query map[string]string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	values := request.URL.Query()
	for key, value := range query {
		values.Set(key, value)
	}
	request.URL.RawQuery = values.Encode()
	request.AddCookie(newConversationHandlerSessionCookie(t, env.authLogic, "alice"))
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	return response
}

func decodeWorkspaceSnapshotResponse(t *testing.T, body io.Reader) workspaceBrowserSnapshotResponse {
	t.Helper()
	var envelope taskTestResponse
	if err := json.NewDecoder(body).Decode(&envelope); err != nil {
		t.Fatalf("Decode(snapshot envelope) error = %v", err)
	}
	if !envelope.OK {
		t.Fatalf("snapshot response ok = false, data = %s", string(envelope.Data))
	}
	var got workspaceBrowserSnapshotResponse
	if err := json.Unmarshal(envelope.Data, &got); err != nil {
		t.Fatalf("Unmarshal(snapshot) error = %v", err)
	}
	return got
}

func decodeWorkspaceFileResponse(t *testing.T, body io.Reader) workspaceBrowserFileResponse {
	t.Helper()
	var envelope taskTestResponse
	if err := json.NewDecoder(body).Decode(&envelope); err != nil {
		t.Fatalf("Decode(file envelope) error = %v", err)
	}
	if !envelope.OK {
		t.Fatalf("file response ok = false, data = %s", string(envelope.Data))
	}
	var got workspaceBrowserFileResponse
	if err := json.Unmarshal(envelope.Data, &got); err != nil {
		t.Fatalf("Unmarshal(file) error = %v", err)
	}
	return got
}

func decodeWorkspaceDiffResponse(t *testing.T, body io.Reader) workspaceBrowserDiffResponse {
	t.Helper()
	var envelope taskTestResponse
	if err := json.NewDecoder(body).Decode(&envelope); err != nil {
		t.Fatalf("Decode(diff envelope) error = %v", err)
	}
	if !envelope.OK {
		t.Fatalf("diff response ok = false, data = %s", string(envelope.Data))
	}
	var got workspaceBrowserDiffResponse
	if err := json.Unmarshal(envelope.Data, &got); err != nil {
		t.Fatalf("Unmarshal(diff) error = %v", err)
	}
	return got
}

func workspaceTreeContains(node *workspaceBrowserTreeNodeEntry, path string) bool {
	if node == nil {
		return false
	}
	if node.Path == path {
		return true
	}
	for index := range node.Children {
		if workspaceTreeContains(&node.Children[index], path) {
			return true
		}
	}
	return false
}

func writeWorkspaceBrowserTestFile(t *testing.T, root string, relativePath string, content string) {
	t.Helper()
	fullPath := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(fullPath), err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", fullPath, err)
	}
}
