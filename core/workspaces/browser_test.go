package workspaces

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestBrowserSnapshotUsesLatestTaskWorkspaceAndBuildsTree(t *testing.T) {
	env := newBrowserTestEnv(t)
	ctx := context.Background()



	older := env.createTask(t, "conv_1", "42", ModeReadonly)
	olderWorkspace := env.createWorkspace(t, "42", older.ID, ModeReadonly)
	writeFile(t, olderWorkspace.Root, "stale.txt", "stale\n")

	newer := env.createTask(t, "conv_1", "42", ModeReadonly)
	newerWorkspace := env.createWorkspace(t, "42", newer.ID, ModeReadonly)
	writeFile(t, newerWorkspace.Root, "fresh.txt", "fresh\n")
	writeFile(t, newerWorkspace.Root, filepath.Join("docs", "guide.md"), "guide\n")

	snapshot, err := env.browser.Snapshot(ctx, "conv_1", "")
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if snapshot.TaskID != newer.ID {
		t.Fatalf("snapshot.TaskID = %q, want %q", snapshot.TaskID, newer.ID)
	}
	if !browserTreeContains(snapshot.Tree, "fresh.txt") {
		t.Fatalf("tree = %#v, want fresh.txt", snapshot.Tree)
	}
	if !browserTreeContains(snapshot.Tree, filepath.ToSlash(filepath.Join("docs", "guide.md"))) {
		t.Fatalf("tree = %#v, want docs/guide.md", snapshot.Tree)
	}
	if browserTreeContains(snapshot.Tree, "stale.txt") {
		t.Fatalf("tree = %#v, did not want stale.txt from older task", snapshot.Tree)
	}
}

func TestBrowserFileDiffAndDownloadFollowWorkspaceContents(t *testing.T) {
	env := newBrowserTestEnv(t)
	ctx := context.Background()

	if _, err := env.workspaces.EnsureHomeWorkspace(ctx, "42"); err != nil {
		t.Fatalf("EnsureHomeWorkspace() error = %v", err)
	}
	writeFile(t, filepath.Join(env.workspacesRoot, "users", "42", "home"), "notes.md", "home line\n")

	task := env.createTask(t, "conv_1", "42", ModeReadonly)
	workspace := env.createWorkspace(t, "42", task.ID, ModeReadonly)
	writeFile(t, workspace.Root, "notes.md", "task line\n")
	writeFile(t, workspace.Root, "bin.dat", string([]byte{0xff, 0x00, 0x01}))

	fileDetail, err := env.browser.File(ctx, "conv_1", "notes.md")
	if err != nil {
		t.Fatalf("File() error = %v", err)
	}
	if fileDetail.Path != "notes.md" || fileDetail.Name != "notes.md" || fileDetail.Content != "task line\n" || fileDetail.Binary {
		t.Fatalf("file detail = %#v, want text notes.md", fileDetail)
	}

	binaryDetail, err := env.browser.File(ctx, "conv_1", "bin.dat")
	if err != nil {
		t.Fatalf("File(binary) error = %v", err)
	}
	if !binaryDetail.Binary || binaryDetail.Content != "" {
		t.Fatalf("binary detail = %#v, want binary metadata only", binaryDetail)
	}

	diff, err := env.browser.Diff(ctx, "conv_1", "notes.md")
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	if diff.Binary {
		t.Fatalf("diff = %#v, want text diff", diff)
	}
	if !strings.Contains(diff.UnifiedDiff, "-home line") || !strings.Contains(diff.UnifiedDiff, "+task line") {
		t.Fatalf("UnifiedDiff = %q, want home removal and task addition", diff.UnifiedDiff)
	}

	data, fileName, err := env.browser.Download(ctx, "conv_1", "notes.md")
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if string(data) != "task line\n" {
		t.Fatalf("download body = %q, want task line", string(data))
	}
	if fileName != "notes.md" {
		t.Fatalf("download fileName = %q, want notes.md", fileName)
	}
}

func TestBrowserRejectsEscapingPathsAndSymlinks(t *testing.T) {
	env := newBrowserTestEnv(t)
	ctx := context.Background()
	task := env.createTask(t, "conv_1", "42", ModeReadonly)
	workspace := env.createWorkspace(t, "42", task.ID, ModeReadonly)
	writeFile(t, workspace.Root, "notes.md", "task line\n")

	if _, err := env.browser.File(ctx, "conv_1", "../secret.txt"); err == nil {
		t.Fatal("File() error = nil, want path escape rejection")
	}

	outside := filepath.Join(env.workspacesRoot, "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatalf("WriteFile(outside) error = %v", err)
	}
	linkPath := filepath.Join(workspace.Root, "linked.txt")
	if err := os.Symlink(outside, linkPath); err != nil {
		t.Skipf("symlink creation not permitted: %v", err)
	}
	if _, err := env.browser.File(ctx, "conv_1", "linked.txt"); err == nil || !strings.Contains(strings.ToLower(err.Error()), "symlink") {
		t.Fatalf("File(symlink) error = %v, want symlink rejection", err)
	}
}

type browserTestEnv struct {
	db            *gorm.DB
	tasks         *coretasks.Store
	workspaces    *Manager
	browser       *Browser
	workspacesRoot string
}

func newBrowserTestEnv(t *testing.T) *browserTestEnv {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"-browser?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	taskStore := coretasks.NewStore(db)
	if err := taskStore.AutoMigrate(); err != nil {
		t.Fatalf("taskStore.AutoMigrate() error = %v", err)
	}
	templateRoot := t.TempDir()
	workspacesRoot := t.TempDir()
	writeFile(t, templateRoot, "AGENTS.md", "# Workspace rules\n")
	manager := newTestManager(t, templateRoot, workspacesRoot)
	browser := NewBrowser(BrowserDeps{
		TaskStore:        taskStore,
		WorkspaceManager: manager,
	})
	return &browserTestEnv{
		db:             db,
		tasks:          taskStore,
		workspaces:     manager,
		browser:        browser,
		workspacesRoot: workspacesRoot,
	}
}

func (e *browserTestEnv) createTask(t *testing.T, conversationID string, workspaceUserID string, mode Mode) *coretasks.Task {
	t.Helper()
	task, _, err := e.tasks.CreateTask(context.Background(), coretasks.CreateTaskInput{
		TaskType:  "agent.run",
		CreatedBy: "alice",
		Input: map[string]any{
			"conversation_id":   conversationID,
			"workspace_user_id": workspaceUserID,
			"workspace_mode":    string(mode),
		},
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	return task
}

func (e *browserTestEnv) createWorkspace(t *testing.T, userID string, workspaceID string, mode Mode) *Workspace {
	t.Helper()
	workspace, err := e.workspaces.CreateTaskWorkspace(context.Background(), userID, workspaceID, mode)
	if err != nil {
		t.Fatalf("CreateTaskWorkspace(%q) error = %v", workspaceID, err)
	}
	return workspace
}

func browserTreeContains(node *WorkspaceTreeNode, path string) bool {
	if node == nil {
		return false
	}
	if node.Path == path {
		return true
	}
	for _, child := range node.Children {
		if browserTreeContains(child, path) {
			return true
		}
	}
	return false
}
