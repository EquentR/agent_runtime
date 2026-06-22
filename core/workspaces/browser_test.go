package workspaces

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
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
	if !browserTreeContains(snapshot.Tree, "docs") {
		t.Fatalf("tree = %#v, want docs", snapshot.Tree)
	}
	if browserTreeContains(snapshot.Tree, filepath.ToSlash(filepath.Join("docs", "guide.md"))) {
		t.Fatalf("tree = %#v, did not want unloaded descendant docs/guide.md", snapshot.Tree)
	}
	if browserTreeContains(snapshot.Tree, "stale.txt") {
		t.Fatalf("tree = %#v, did not want stale.txt from older task", snapshot.Tree)
	}
}

func TestBrowserSnapshotLazilyListsVisibleDirectChildren(t *testing.T) {
	env := newBrowserTestEnv(t)
	ctx := context.Background()

	home, err := env.workspaces.EnsureHomeWorkspace(ctx, "42")
	if err != nil {
		t.Fatalf("EnsureHomeWorkspace() error = %v", err)
	}
	writeFile(t, home.Root, "README.md", "same\n")
	writeFile(t, home.Root, filepath.Join("docs", "guide.md"), "home guide\n")
	writeFile(t, home.Root, filepath.Join("docs", "nested", "deep.md"), "deep\n")
	writeFile(t, home.Root, filepath.Join("skills", "review", "SKILL.md"), "# hidden skill\n")

	task := env.createTask(t, "conv_1", "42", ModeReadonly)
	workspace := env.createWorkspace(t, "42", task.ID, ModeReadonly)
	writeFile(t, workspace.Root, filepath.Join("docs", "guide.md"), "task guide\n")
	writeFile(t, workspace.Root, "AGENTS.md", "# hidden agents\n")
	writeFile(t, workspace.Root, filepath.Join("skills", "review", "SKILL.md"), "# hidden task skill\n")
	writeFile(t, workspace.Root, StateFileName, `{"hidden":true}`)

	rootSnapshot, err := env.browser.Snapshot(ctx, "conv_1", "")
	if err != nil {
		t.Fatalf("Snapshot(root) error = %v", err)
	}
	if rootSnapshot.Path != "" {
		t.Fatalf("root snapshot Path = %q, want empty", rootSnapshot.Path)
	}
	if rootSnapshot.Tree == nil {
		t.Fatal("root snapshot Tree = nil, want root tree")
	}
	if !rootSnapshot.Tree.ChildrenLoaded {
		t.Fatalf("root ChildrenLoaded = false, want true")
	}
	if len(rootSnapshot.Tree.Children) != 2 {
		t.Fatalf("root children = %#v, want README.md and docs only", rootSnapshot.Tree.Children)
	}
	if browserTreeFind(rootSnapshot.Tree, "AGENTS.md") != nil ||
		browserTreeFind(rootSnapshot.Tree, "skills") != nil ||
		browserTreeFind(rootSnapshot.Tree, StateFileName) != nil {
		t.Fatalf("root tree = %#v, want hidden paths excluded", rootSnapshot.Tree)
	}
	readme := browserTreeFind(rootSnapshot.Tree, "README.md")
	if readme == nil {
		t.Fatalf("root tree = %#v, want README.md", rootSnapshot.Tree)
	}
	if readme.Type != "file" || readme.HasDiff {
		t.Fatalf("README node = %#v, want unchanged file without diff", readme)
	}
	docs := browserTreeFind(rootSnapshot.Tree, "docs")
	if docs == nil {
		t.Fatalf("root tree = %#v, want docs", rootSnapshot.Tree)
	}
	if docs.Type != "dir" || docs.ChildrenLoaded || len(docs.Children) != 0 {
		t.Fatalf("docs node = %#v, want unloaded dir without children", docs)
	}
	if browserTreeFind(rootSnapshot.Tree, filepath.ToSlash(filepath.Join("docs", "guide.md"))) != nil ||
		browserTreeFind(rootSnapshot.Tree, filepath.ToSlash(filepath.Join("docs", "nested", "deep.md"))) != nil {
		t.Fatalf("root tree = %#v, did not want recursive descendants", rootSnapshot.Tree)
	}

	docsSnapshot, err := env.browser.Snapshot(ctx, "conv_1", "docs")
	if err != nil {
		t.Fatalf("Snapshot(docs) error = %v", err)
	}
	if docsSnapshot.Path != "docs" {
		t.Fatalf("docs snapshot Path = %q, want docs", docsSnapshot.Path)
	}
	if docsSnapshot.Tree == nil || docsSnapshot.Tree.Path != "docs" || docsSnapshot.Tree.Type != "dir" || !docsSnapshot.Tree.ChildrenLoaded {
		t.Fatalf("docs snapshot tree = %#v, want loaded docs dir", docsSnapshot.Tree)
	}
	if len(docsSnapshot.Tree.Children) != 2 {
		t.Fatalf("docs children = %#v, want guide.md and nested only", docsSnapshot.Tree.Children)
	}
	guide := browserTreeFind(docsSnapshot.Tree, filepath.ToSlash(filepath.Join("docs", "guide.md")))
	if guide == nil {
		t.Fatalf("docs tree = %#v, want docs/guide.md", docsSnapshot.Tree)
	}
	if guide.Type != "file" || !guide.HasDiff {
		t.Fatalf("guide node = %#v, want changed file with diff", guide)
	}
	nested := browserTreeFind(docsSnapshot.Tree, filepath.ToSlash(filepath.Join("docs", "nested")))
	if nested == nil {
		t.Fatalf("docs tree = %#v, want docs/nested", docsSnapshot.Tree)
	}
	if nested.Type != "dir" || nested.ChildrenLoaded || len(nested.Children) != 0 {
		t.Fatalf("nested node = %#v, want unloaded dir without children", nested)
	}
	if browserTreeFind(docsSnapshot.Tree, filepath.ToSlash(filepath.Join("docs", "nested", "deep.md"))) != nil {
		t.Fatalf("docs tree = %#v, did not want nested descendant", docsSnapshot.Tree)
	}

	encoded, err := json.Marshal(rootSnapshot)
	if err != nil {
		t.Fatalf("Marshal(rootSnapshot) error = %v", err)
	}
	if strings.Contains(string(encoded), "home_root") || strings.Contains(string(encoded), "task_root") {
		t.Fatalf("snapshot JSON = %s, did not want serialized roots", string(encoded))
	}
}

func TestBrowserRejectsHiddenBrowserPaths(t *testing.T) {
	env := newBrowserTestEnv(t)
	ctx := context.Background()

	task := env.createTask(t, "conv_1", "42", ModeReadonly)
	workspace := env.createWorkspace(t, "42", task.ID, ModeReadonly)
	writeFile(t, workspace.Root, "visible.md", "visible\n")
	writeFile(t, workspace.Root, "AGENTS.md", "# hidden agents\n")
	writeFile(t, workspace.Root, filepath.Join("skills", "review", "SKILL.md"), "# hidden skill\n")
	writeFile(t, workspace.Root, filepath.Join(".attachments", "att_1", "notes.txt"), "hidden attachment\n")
	writeFile(t, workspace.Root, StateFileName, `{"hidden":true}`)
	writeFile(t, workspace.Root, BaselineFileName, `{"hidden":true}`)
	writeFile(t, workspace.Root, filepath.Join("docs", "guide.md"), "guide\n")
	writeFile(t, workspace.Root, filepath.Join("docs", "AGENTS.md"), "# hidden nested agents\n")
	writeFile(t, workspace.Root, filepath.Join("docs", "skills", "review", "SKILL.md"), "# hidden nested skill\n")
	writeFile(t, workspace.Root, filepath.Join("docs", ".attachments", "att_1", "notes.txt"), "hidden nested attachment\n")
	writeFile(t, workspace.Root, filepath.Join("docs", StateFileName), `{"hidden":true}`)
	writeFile(t, workspace.Root, filepath.Join("docs", BaselineFileName), `{"hidden":true}`)

	snapshot, err := env.browser.Snapshot(ctx, "conv_1", "")
	if err != nil {
		t.Fatalf("Snapshot(root) error = %v", err)
	}
	for _, path := range []string{"AGENTS.md", "skills", ".attachments", StateFileName, BaselineFileName} {
		if browserTreeFind(snapshot.Tree, path) != nil {
			t.Fatalf("root tree = %#v, did not want hidden path %q", snapshot.Tree, path)
		}
	}
	docsSnapshot, err := env.browser.Snapshot(ctx, "conv_1", "docs")
	if err != nil {
		t.Fatalf("Snapshot(docs) error = %v", err)
	}
	for _, path := range []string{
		filepath.ToSlash(filepath.Join("docs", "AGENTS.md")),
		filepath.ToSlash(filepath.Join("docs", "skills")),
		filepath.ToSlash(filepath.Join("docs", ".attachments")),
		filepath.ToSlash(filepath.Join("docs", StateFileName)),
		filepath.ToSlash(filepath.Join("docs", BaselineFileName)),
	} {
		if browserTreeFind(docsSnapshot.Tree, path) != nil {
			t.Fatalf("docs tree = %#v, did not want hidden path %q", docsSnapshot.Tree, path)
		}
	}
	if browserTreeFind(docsSnapshot.Tree, filepath.ToSlash(filepath.Join("docs", "guide.md"))) == nil {
		t.Fatalf("docs tree = %#v, want visible docs/guide.md", docsSnapshot.Tree)
	}

	for _, path := range []string{
		"AGENTS.md",
		filepath.ToSlash(filepath.Join("skills", "review", "SKILL.md")),
		filepath.ToSlash(filepath.Join(".attachments", "att_1", "notes.txt")),
		StateFileName,
		BaselineFileName,
		filepath.ToSlash(filepath.Join("docs", "AGENTS.md")),
		filepath.ToSlash(filepath.Join("docs", ".attachments", "att_1", "notes.txt")),
	} {
		if _, err := env.browser.File(ctx, "conv_1", path); err == nil || !strings.Contains(strings.ToLower(err.Error()), "not found") {
			t.Fatalf("File(%q) error = %v, want not found", path, err)
		}
	}
	for _, path := range []string{
		"AGENTS.md",
		filepath.ToSlash(filepath.Join("skills", "review", "SKILL.md")),
		filepath.ToSlash(filepath.Join(".attachments", "att_1", "notes.txt")),
		StateFileName,
		BaselineFileName,
		filepath.ToSlash(filepath.Join("docs", StateFileName)),
		filepath.ToSlash(filepath.Join("docs", BaselineFileName)),
	} {
		if _, err := env.browser.Diff(ctx, "conv_1", path); err == nil || !strings.Contains(strings.ToLower(err.Error()), "not found") {
			t.Fatalf("Diff(%q) error = %v, want not found", path, err)
		}
	}
	if _, err := env.browser.Snapshot(ctx, "conv_1", "skills"); err == nil || !strings.Contains(strings.ToLower(err.Error()), "not found") {
		t.Fatalf("Snapshot(skills) error = %v, want not found", err)
	}
	if _, err := env.browser.Download(ctx, "conv_1", filepath.ToSlash(filepath.Join("docs", "skills"))); err == nil || !strings.Contains(strings.ToLower(err.Error()), "not found") {
		t.Fatalf("Download(docs/skills) error = %v, want not found", err)
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

	download, err := env.browser.Download(ctx, "conv_1", "notes.md")
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if download.Reader == nil {
		t.Fatal("download Reader = nil, want file reader")
	}
	defer download.Reader.Close()
	body, err := io.ReadAll(download.Reader)
	if err != nil {
		t.Fatalf("ReadAll(download.Reader) error = %v", err)
	}
	if string(body) != "task line\n" {
		t.Fatalf("download body = %q, want task line", string(body))
	}
	if download.FileName != "notes.md" {
		t.Fatalf("download fileName = %q, want notes.md", download.FileName)
	}
	if download.ContentType != "application/octet-stream" {
		t.Fatalf("download ContentType = %q, want application/octet-stream", download.ContentType)
	}
}

func TestBrowserDownloadDirectoryReturnsVisibleZip(t *testing.T) {
	env := newBrowserTestEnv(t)
	ctx := context.Background()

	task := env.createTask(t, "conv_1", "42", ModeReadonly)
	workspace := env.createWorkspace(t, "42", task.ID, ModeReadonly)
	writeFile(t, workspace.Root, filepath.Join("docs", "guide.md"), "guide\n")
	writeFile(t, workspace.Root, filepath.Join("docs", "nested", "deep.md"), "deep\n")
	writeFile(t, workspace.Root, filepath.Join("docs", "AGENTS.md"), "# hidden agents\n")
	writeFile(t, workspace.Root, filepath.Join("docs", "skills", "review", "SKILL.md"), "# hidden skill\n")
	writeFile(t, workspace.Root, filepath.Join("docs", ".attachments", "att_1", "notes.txt"), "hidden attachment\n")
	writeFile(t, workspace.Root, filepath.Join("docs", StateFileName), `{"hidden":true}`)
	writeFile(t, workspace.Root, filepath.Join("docs", BaselineFileName), `{"hidden":true}`)

	download, err := env.browser.Download(ctx, "conv_1", "docs")
	if err != nil {
		t.Fatalf("Download(docs) error = %v", err)
	}
	if download.FileName != "docs.zip" {
		t.Fatalf("download FileName = %q, want docs.zip", download.FileName)
	}
	if download.ContentType != "application/zip" {
		t.Fatalf("download ContentType = %q, want application/zip", download.ContentType)
	}
	if download.Reader == nil {
		t.Fatal("download Reader = nil, want zip reader")
	}
	if download.Data != nil && len(download.Data) != 0 {
		t.Fatalf("download Data = %q, want empty for streamed zip", string(download.Data))
	}
	if download.ContentLength != -1 {
		t.Fatalf("download ContentLength = %d, want -1 for streamed zip", download.ContentLength)
	}
	defer download.Reader.Close()
	data, err := io.ReadAll(download.Reader)
	if err != nil {
		t.Fatalf("ReadAll(download.Reader) error = %v", err)
	}
	entries := readZipEntries(t, data)
	for path, want := range map[string]string{
		filepath.ToSlash(filepath.Join("docs", "guide.md")):          "guide\n",
		filepath.ToSlash(filepath.Join("docs", "nested", "deep.md")): "deep\n",
	} {
		got, ok := entries[path]
		if !ok {
			t.Fatalf("zip entries = %#v, want %q", entries, path)
		}
		if got != want {
			t.Fatalf("zip entry %q = %q, want %q", path, got, want)
		}
	}
	for _, hiddenPath := range []string{
		filepath.ToSlash(filepath.Join("docs", "AGENTS.md")),
		filepath.ToSlash(filepath.Join("docs", "skills", "review", "SKILL.md")),
		filepath.ToSlash(filepath.Join("docs", ".attachments", "att_1", "notes.txt")),
		filepath.ToSlash(filepath.Join("docs", StateFileName)),
		filepath.ToSlash(filepath.Join("docs", BaselineFileName)),
	} {
		if _, ok := entries[hiddenPath]; ok {
			t.Fatalf("zip entries = %#v, did not want hidden path %q", entries, hiddenPath)
		}
	}

	rootDownload, err := env.browser.Download(ctx, "conv_1", "")
	if err != nil {
		t.Fatalf("Download(root) error = %v", err)
	}
	if rootDownload.Reader == nil {
		t.Fatal("root download Reader = nil, want zip reader")
	}
	if rootDownload.Data != nil && len(rootDownload.Data) != 0 {
		t.Fatalf("root download Data = %q, want empty for streamed zip", string(rootDownload.Data))
	}
	if rootDownload.ContentLength != -1 {
		t.Fatalf("root download ContentLength = %d, want -1 for streamed zip", rootDownload.ContentLength)
	}
	defer rootDownload.Reader.Close()
	rootData, err := io.ReadAll(rootDownload.Reader)
	if err != nil {
		t.Fatalf("ReadAll(root download.Reader) error = %v", err)
	}
	rootEntries := readZipEntries(t, rootData)
	for _, visiblePath := range []string{
		filepath.ToSlash(filepath.Join("docs", "guide.md")),
		filepath.ToSlash(filepath.Join("docs", "nested", "deep.md")),
	} {
		if _, ok := rootEntries[visiblePath]; !ok {
			t.Fatalf("root zip entries = %#v, want visible path %q", rootEntries, visiblePath)
		}
	}
	for _, hiddenPath := range []string{
		filepath.ToSlash(filepath.Join("docs", "AGENTS.md")),
		filepath.ToSlash(filepath.Join("docs", "skills", "review", "SKILL.md")),
		filepath.ToSlash(filepath.Join("docs", ".attachments", "att_1", "notes.txt")),
		filepath.ToSlash(filepath.Join("docs", StateFileName)),
		filepath.ToSlash(filepath.Join("docs", BaselineFileName)),
	} {
		if _, ok := rootEntries[hiddenPath]; ok {
			t.Fatalf("root zip entries = %#v, did not want hidden path %q", rootEntries, hiddenPath)
		}
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
	if _, err := env.browser.Download(ctx, "conv_1", "../secret.txt"); err == nil {
		t.Fatal("Download() error = nil, want path escape rejection")
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
	if _, err := env.browser.Download(ctx, "conv_1", "linked.txt"); err == nil || !strings.Contains(strings.ToLower(err.Error()), "symlink") {
		t.Fatalf("Download(symlink) error = %v, want symlink rejection", err)
	}
	if err := os.MkdirAll(filepath.Join(workspace.Root, "docs"), 0o755); err != nil {
		t.Fatalf("MkdirAll(docs) error = %v", err)
	}
	nestedLinkPath := filepath.Join(workspace.Root, "docs", "linked.txt")
	if err := os.Symlink(outside, nestedLinkPath); err != nil {
		t.Skipf("nested symlink creation not permitted: %v", err)
	}
	if _, err := env.browser.Download(ctx, "conv_1", "docs"); err == nil || !strings.Contains(strings.ToLower(err.Error()), "symlink") {
		t.Fatalf("Download(docs with symlink) error = %v, want symlink rejection", err)
	}
}

type browserTestEnv struct {
	db             *gorm.DB
	tasks          *coretasks.Store
	workspaces     *Manager
	browser        *Browser
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

func browserTreeFind(node *WorkspaceTreeNode, path string) *WorkspaceTreeNode {
	if node == nil {
		return nil
	}
	if node.Path == path {
		return node
	}
	for _, child := range node.Children {
		if found := browserTreeFind(child, path); found != nil {
			return found
		}
	}
	return nil
}

func readZipEntries(t *testing.T, data []byte) map[string]string {
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
