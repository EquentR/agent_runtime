package handlers

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
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
	if contentType := response.Header.Get("Content-Type"); !strings.Contains(contentType, "application/octet-stream") {
		t.Fatalf("Content-Type = %q, want application/octet-stream", contentType)
	}
	if disposition := response.Header.Get("Content-Disposition"); !strings.Contains(disposition, "notes.md") {
		t.Fatalf("Content-Disposition = %q, want filename", disposition)
	}
}

func TestConversationWorkspaceBrowserSnapshotDoesNotExposeBackendRoots(t *testing.T) {
	env := newWorkspaceBrowserHandlerTestEnv(t)
	env.seedConversationTaskWorkspace(t, "conv_1", "42", "notes.md", "hello workspace\n")

	response := workspaceBrowserGet(t, env, env.server.URL+"/api/v1/conversations/conv_1/workspace/files", nil)
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll(snapshot) error = %v", err)
	}

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", response.StatusCode, string(body))
	}
	for _, forbidden := range []string{
		"home_root",
		"task_root",
		env.workspacesRoot,
		filepath.ToSlash(env.workspacesRoot),
		strings.ReplaceAll(env.workspacesRoot, `\`, `\\`),
	} {
		if forbidden != "" && strings.Contains(string(body), forbidden) {
			t.Fatalf("snapshot body = %s, did not want %q", string(body), forbidden)
		}
	}
}

func TestConversationWorkspaceBrowserLoadsDirectorySnapshotByPath(t *testing.T) {
	env := newWorkspaceBrowserHandlerTestEnv(t)
	env.seedConversationTaskWorkspaceWithFiles(t, "conv_1", "42", map[string]string{
		filepath.Join("docs", "guide.md"):          "guide\n",
		filepath.Join("docs", "nested", "deep.md"): "deep\n",
	})

	response := workspaceBrowserGet(t, env, env.server.URL+"/api/v1/conversations/conv_1/workspace/files", map[string]string{"path": "docs"})
	defer response.Body.Close()
	got := decodeWorkspaceSnapshotResponse(t, response.Body)

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	if got.Path != "docs" {
		t.Fatalf("snapshot.Path = %q, want docs", got.Path)
	}
	if got.Tree == nil || got.Tree.Path != "docs" || got.Tree.Type != "dir" || !got.Tree.ChildrenLoaded {
		t.Fatalf("tree = %#v, want loaded docs directory snapshot", got.Tree)
	}
	if !workspaceTreeContains(got.Tree, filepath.ToSlash(filepath.Join("docs", "guide.md"))) {
		t.Fatalf("tree = %#v, want docs/guide.md", got.Tree)
	}
	if !workspaceTreeContains(got.Tree, filepath.ToSlash(filepath.Join("docs", "nested"))) {
		t.Fatalf("tree = %#v, want docs/nested", got.Tree)
	}
	if workspaceTreeContains(got.Tree, filepath.ToSlash(filepath.Join("docs", "nested", "deep.md"))) {
		t.Fatalf("tree = %#v, did not want unloaded nested descendant", got.Tree)
	}
}

func TestConversationWorkspaceBrowserDownloadsDirectoryZip(t *testing.T) {
	env := newWorkspaceBrowserHandlerTestEnv(t)
	env.seedConversationTaskWorkspace(t, "conv_1", "42", filepath.Join("docs", "guide.md"), "guide\n")

	response := workspaceBrowserGet(t, env, env.server.URL+"/api/v1/conversations/conv_1/workspace/download", map[string]string{"path": "docs"})
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll(download zip) error = %v", err)
	}

	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", response.StatusCode, string(body))
	}
	if contentType := response.Header.Get("Content-Type"); !strings.Contains(contentType, "application/zip") {
		t.Fatalf("Content-Type = %q, want application/zip", contentType)
	}
	if disposition := response.Header.Get("Content-Disposition"); !strings.Contains(disposition, "docs.zip") {
		t.Fatalf("Content-Disposition = %q, want docs.zip", disposition)
	}
	entries := readWorkspaceBrowserZipEntries(t, body)
	if got := entries[filepath.ToSlash(filepath.Join("docs", "guide.md"))]; got != "guide\n" {
		t.Fatalf("zip entries = %#v, want docs/guide.md with guide content", entries)
	}
}

func TestConversationWorkspaceBrowserFormatsDownloadDispositionSafely(t *testing.T) {
	env := newWorkspaceDownloadHandlerTestEnv(t, &staticWorkspaceBrowser{
		download: &coreworkspaces.WorkspaceDownload{
			Reader:        io.NopCloser(strings.NewReader("download me\n")),
			ContentLength: int64(len("download me\n")),
			FileName:      `bad"name-报告.txt`,
			ContentType:   "application/octet-stream",
		},
	})

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

	mediaType, params, err := mime.ParseMediaType(response.Header.Get("Content-Disposition"))
	if err != nil {
		t.Fatalf("ParseMediaType(Content-Disposition) error = %v", err)
	}
	if mediaType != "attachment" {
		t.Fatalf("Content-Disposition media type = %q, want attachment", mediaType)
	}
	if params["filename"] != `bad"name-报告.txt` {
		t.Fatalf("Content-Disposition filename = %q, want %q", params["filename"], `bad"name-报告.txt`)
	}
	if strings.Contains(response.Header.Get("Content-Disposition"), `filename="bad"name-报告.txt"`) {
		t.Fatalf("Content-Disposition = %q, want escaped or encoded filename", response.Header.Get("Content-Disposition"))
	}
}

type workspaceBrowserHandlerTestEnv struct {
	db             *gorm.DB
	conversations  *coreagent.ConversationStore
	taskStore      *coretasks.Store
	taskManager    *coretasks.Manager
	workspaces     *coreworkspaces.Manager
	workspacesRoot string
	authLogic      *logics.AuthLogic
	server         *httptest.Server
}

type workspaceBrowserSnapshotResponse struct {
	ConversationID string                         `json:"conversation_id"`
	TaskID         string                         `json:"task_id"`
	UserID         string                         `json:"user_id"`
	Path           string                         `json:"path"`
	Tree           *workspaceBrowserTreeNodeEntry `json:"tree"`
}

type workspaceBrowserTreeNodeEntry struct {
	Path           string                          `json:"path"`
	Name           string                          `json:"name"`
	Type           string                          `json:"type"`
	HasDiff        bool                            `json:"has_diff"`
	ChildrenLoaded bool                            `json:"children_loaded"`
	Children       []workspaceBrowserTreeNodeEntry `json:"children"`
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
		db:             db,
		conversations:  conversations,
		taskStore:      taskStore,
		taskManager:    taskManager,
		workspaces:     workspaces,
		workspacesRoot: workspacesRoot,
		authLogic:      authLogic,
		server:         server,
	}
}

func (e *workspaceBrowserHandlerTestEnv) createTask(t *testing.T, conversationID string, workspaceUserID string) *coretasks.Task {
	t.Helper()
	task, err := e.taskManager.CreateTask(context.Background(), coretasks.CreateTaskInput{
		TaskType:  "agent.run",
		CreatedBy: "alice",
		Input: map[string]any{
			"conversation_id":   conversationID,
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
	return e.seedConversationTaskWorkspaceWithFiles(t, conversationID, workspaceUserID, map[string]string{path: content})
}

func (e *workspaceBrowserHandlerTestEnv) seedConversationTaskWorkspaceWithFiles(t *testing.T, conversationID string, workspaceUserID string, files map[string]string) *coretasks.Task {
	t.Helper()
	if _, err := e.conversations.CreateConversation(context.Background(), coreagent.CreateConversationInput{ID: conversationID, ProviderID: "openai", ModelID: "gpt-5.4", CreatedBy: "alice"}); err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	task := e.createTask(t, conversationID, workspaceUserID)
	workspace := e.createWorkspace(t, workspaceUserID, task.ID, coreworkspaces.ModeReadonly)
	for path, content := range files {
		writeWorkspaceBrowserTestFile(t, workspace.Root, path, content)
	}
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

func readWorkspaceBrowserZipEntries(t *testing.T, data []byte) map[string]string {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip.NewReader() error = %v", err)
	}
	entries := map[string]string{}
	for _, file := range reader.File {
		handle, err := file.Open()
		if err != nil {
			t.Fatalf("Open(%q) error = %v", file.Name, err)
		}
		content := new(bytes.Buffer)
		if _, err := content.ReadFrom(handle); err != nil {
			_ = handle.Close()
			t.Fatalf("ReadFrom(%q) error = %v", file.Name, err)
		}
		if err := handle.Close(); err != nil {
			t.Fatalf("Close(%q) error = %v", file.Name, err)
		}
		entries[file.Name] = content.String()
	}
	return entries
}

type staticWorkspaceBrowser struct {
	download *coreworkspaces.WorkspaceDownload
}

func (b *staticWorkspaceBrowser) Snapshot(context.Context, string, string) (*coreworkspaces.WorkspaceSnapshot, error) {
	return nil, fmt.Errorf("unexpected Snapshot call")
}

func (b *staticWorkspaceBrowser) File(context.Context, string, string) (*coreworkspaces.WorkspaceFileDetail, error) {
	return nil, fmt.Errorf("unexpected File call")
}

func (b *staticWorkspaceBrowser) Diff(context.Context, string, string) (*coreworkspaces.WorkspaceDiffResult, error) {
	return nil, fmt.Errorf("unexpected Diff call")
}

func (b *staticWorkspaceBrowser) Download(context.Context, string, string) (*coreworkspaces.WorkspaceDownload, error) {
	return b.download, nil
}

func newWorkspaceDownloadHandlerTestEnv(t *testing.T, browser workspaceBrowser) *workspaceBrowserHandlerTestEnv {
	t.Helper()

	dsn := fmt.Sprintf("file:%s-workspace-download?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	conversations := coreagent.NewConversationStore(db)
	if err := conversations.AutoMigrate(); err != nil {
		t.Fatalf("conversation AutoMigrate() error = %v", err)
	}
	authLogic := newAuthLogicForTest(t, db)
	authMiddleware := NewAuthMiddleware(authLogic)

	handler := NewWorkspaceHandler(conversations, nil, nil, authMiddleware.RequireSession())
	handler.browser = browser

	engine := rest.Init()
	handler.Register(engine.Group("/api/v1"))
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)

	registerActiveAuthUserForTest(t, authLogic, "alice", "secret-123")
	if _, err := conversations.CreateConversation(context.Background(), coreagent.CreateConversationInput{
		ID:         "conv_1",
		ProviderID: "openai",
		ModelID:    "gpt-5.4",
		CreatedBy:  "alice",
	}); err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}

	return &workspaceBrowserHandlerTestEnv{
		db:            db,
		conversations: conversations,
		authLogic:     authLogic,
		server:        server,
	}
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
