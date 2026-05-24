package workspaces

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEnsureHomeWorkspaceSeedsTemplateFilesOnce(t *testing.T) {
	templateRoot := t.TempDir()
	workspacesRoot := t.TempDir()
	writeFile(t, templateRoot, "AGENTS.md", "# Workspace rules\n")
	writeFile(t, templateRoot, filepath.Join("skills", "review", "SKILL.md"), "# Review skill\n")
	writeFile(t, templateRoot, "ignored.txt", "do not copy")

	manager := newTestManager(t, templateRoot, workspacesRoot)

	home, err := manager.EnsureHomeWorkspace(context.Background(), "42")
	if err != nil {
		t.Fatalf("EnsureHomeWorkspace() error = %v", err)
	}

	wantHomeRoot := filepath.Join(workspacesRoot, "users", "42", "home")
	if home.Root != wantHomeRoot {
		t.Fatalf("home.Root = %q, want %q", home.Root, wantHomeRoot)
	}
	assertFileContent(t, home.Root, "AGENTS.md", "# Workspace rules\n")
	assertFileContent(t, home.Root, filepath.Join("skills", "review", "SKILL.md"), "# Review skill\n")
	assertPathMissing(t, home.Root, "ignored.txt")

	writeFile(t, templateRoot, "AGENTS.md", "# Changed template\n")
	writeFile(t, home.Root, "AGENTS.md", "# User customized\n")
	if _, err := manager.EnsureHomeWorkspace(context.Background(), "42"); err != nil {
		t.Fatalf("second EnsureHomeWorkspace() error = %v", err)
	}
	assertFileContent(t, home.Root, "AGENTS.md", "# User customized\n")
}

func TestEnsureHomeWorkspaceRepairsPartiallySeededHome(t *testing.T) {
	templateRoot := t.TempDir()
	workspacesRoot := t.TempDir()
	writeFile(t, templateRoot, "AGENTS.md", "# Workspace rules\n")
	writeFile(t, templateRoot, filepath.Join("skills", "review", "SKILL.md"), "# Review skill\n")
	writeFile(t, filepath.Join(workspacesRoot, "users", "42", "home"), "notes.md", "keep me")
	manager := newTestManager(t, templateRoot, workspacesRoot)

	home, err := manager.EnsureHomeWorkspace(context.Background(), "42")
	if err != nil {
		t.Fatalf("EnsureHomeWorkspace() error = %v", err)
	}

	assertFileContent(t, home.Root, "AGENTS.md", "# Workspace rules\n")
	assertFileContent(t, home.Root, filepath.Join("skills", "review", "SKILL.md"), "# Review skill\n")
	assertFileContent(t, home.Root, "notes.md", "keep me")
}

func TestEnsureHomeWorkspaceRepairsMissingNestedSkillsWithoutOverwritingExistingFiles(t *testing.T) {
	templateRoot := t.TempDir()
	workspacesRoot := t.TempDir()
	writeFile(t, templateRoot, "AGENTS.md", "# Workspace rules\n")
	writeFile(t, templateRoot, filepath.Join("skills", "review", "SKILL.md"), "# Template review\n")
	writeFile(t, templateRoot, filepath.Join("skills", "debugging", "SKILL.md"), "# Debugging skill\n")
	manager := newTestManager(t, templateRoot, workspacesRoot)

	homeRoot := filepath.Join(workspacesRoot, "users", "42", "home")
	writeFile(t, homeRoot, "AGENTS.md", "# Custom workspace rules\n")
	writeFile(t, homeRoot, filepath.Join("skills", "review", "SKILL.md"), "# User review notes\n")

	home, err := manager.EnsureHomeWorkspace(context.Background(), "42")
	if err != nil {
		t.Fatalf("EnsureHomeWorkspace() error = %v", err)
	}

	assertFileContent(t, home.Root, "AGENTS.md", "# Custom workspace rules\n")
	assertFileContent(t, home.Root, filepath.Join("skills", "review", "SKILL.md"), "# User review notes\n")
	assertFileContent(t, home.Root, filepath.Join("skills", "debugging", "SKILL.md"), "# Debugging skill\n")
}

func TestCreateTaskWorkspaceCopiesCurrentHomeAndWritesActiveState(t *testing.T) {
	templateRoot := t.TempDir()
	workspacesRoot := t.TempDir()
	writeFile(t, templateRoot, "AGENTS.md", "# Workspace rules\n")
	manager := newTestManager(t, templateRoot, workspacesRoot)

	home, err := manager.EnsureHomeWorkspace(context.Background(), "42")
	if err != nil {
		t.Fatalf("EnsureHomeWorkspace() error = %v", err)
	}
	writeFile(t, home.Root, "notes.md", "current home")

	task, err := manager.CreateTaskWorkspace(context.Background(), "42", "tsk_123", ModeMutable)
	if err != nil {
		t.Fatalf("CreateTaskWorkspace() error = %v", err)
	}

	wantTaskRoot := filepath.Join(workspacesRoot, "users", "42", "tasks", "tsk_123")
	if task.Root != wantTaskRoot {
		t.Fatalf("task.Root = %q, want %q", task.Root, wantTaskRoot)
	}
	assertFileContent(t, task.Root, "notes.md", "current home")
	state := readState(t, task.Root)
	if state.TaskID != "tsk_123" || state.UserID != "42" || state.Mode != ModeMutable || state.State != StateActive {
		t.Fatalf("state identifiers = %#v, want mutable active for user/task", state)
	}
	if state.HomeRoot != filepath.Join(workspacesRoot, "users", "42", "home") {
		t.Fatalf("state.HomeRoot = %q", state.HomeRoot)
	}
	if state.TaskRoot != task.Root {
		t.Fatalf("state.TaskRoot = %q, want %q", state.TaskRoot, task.Root)
	}
	if state.BackupRoot != "" {
		t.Fatalf("state.BackupRoot = %q, want empty before confirm", state.BackupRoot)
	}
	if state.CreatedAt.IsZero() || state.UpdatedAt.IsZero() {
		t.Fatalf("state timestamps should be set: %#v", state)
	}
}

func TestNewManagerCreatesDefaultWorkspacesRoot(t *testing.T) {
	baseDir := t.TempDir()
	t.Chdir(baseDir)
	templateRoot := filepath.Join(baseDir, "template")
	writeFile(t, templateRoot, "AGENTS.md", "# Workspace rules\n")

	manager, err := NewManager(Config{TemplateRoot: templateRoot})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	home, err := manager.EnsureHomeWorkspace(context.Background(), "42")
	if err != nil {
		t.Fatalf("EnsureHomeWorkspace() error = %v", err)
	}
	wantHomeRoot := filepath.Join("data", "workspaces", "users", "42", "home")
	if home.Root != wantHomeRoot {
		t.Fatalf("home.Root = %q, want %q", home.Root, wantHomeRoot)
	}
	assertFileContent(t, home.Root, "AGENTS.md", "# Workspace rules\n")
}

func TestCompleteTaskWorkspaceMarksReadonlyTaskCompleted(t *testing.T) {
	templateRoot := t.TempDir()
	workspacesRoot := t.TempDir()
	writeFile(t, templateRoot, "AGENTS.md", "# Workspace rules\n")
	manager := newTestManager(t, templateRoot, workspacesRoot)

	if _, err := manager.EnsureHomeWorkspace(context.Background(), "42"); err != nil {
		t.Fatalf("EnsureHomeWorkspace() error = %v", err)
	}
	task, err := manager.CreateTaskWorkspace(context.Background(), "42", "tsk_ro", ModeReadonly)
	if err != nil {
		t.Fatalf("CreateTaskWorkspace() error = %v", err)
	}

	state := readState(t, task.Root)
	if state.State != StateActive {
		t.Fatalf("readonly initial state.State = %q, want %q", state.State, StateActive)
	}
	completed, err := manager.CompleteTaskWorkspace(context.Background(), "42", "tsk_ro", "")
	if err != nil {
		t.Fatalf("CompleteTaskWorkspace() error = %v", err)
	}
	if completed.State != StateCompleted {
		t.Fatalf("completed.State = %q, want %q", completed.State, StateCompleted)
	}
}

func TestConfirmTaskWorkspaceBacksUpHomeReplacesContentsAndIsIdempotent(t *testing.T) {
	templateRoot := t.TempDir()
	workspacesRoot := t.TempDir()
	writeFile(t, templateRoot, "AGENTS.md", "# Workspace rules\n")
	manager := newTestManager(t, templateRoot, workspacesRoot)

	home, err := manager.EnsureHomeWorkspace(context.Background(), "42")
	if err != nil {
		t.Fatalf("EnsureHomeWorkspace() error = %v", err)
	}
	writeFile(t, home.Root, "home-only.txt", "preserve in backup")
	task, err := manager.CreateTaskWorkspace(context.Background(), "42", "tsk_123", ModeMutable)
	if err != nil {
		t.Fatalf("CreateTaskWorkspace() error = %v", err)
	}
	writeFile(t, task.Root, "task-only.txt", "merged into home")
	writeFile(t, home.Root, "late-home-only.txt", "must be removed by replacement")
	removePath(t, home.Root, "AGENTS.md")
	if _, err := manager.MarkTaskWorkspacePendingMerge(context.Background(), "42", "tsk_123"); err != nil {
		t.Fatalf("MarkTaskWorkspacePendingMerge() error = %v", err)
	}

	merged, err := manager.ConfirmTaskWorkspace(context.Background(), "42", "tsk_123")
	if err != nil {
		t.Fatalf("ConfirmTaskWorkspace() error = %v", err)
	}

	assertFileContent(t, home.Root, "task-only.txt", "merged into home")
	assertFileContent(t, merged.BackupRoot, "home-only.txt", "preserve in backup")
	assertPathMissing(t, home.Root, "late-home-only.txt")
	assertPathMissing(t, home.Root, StateFileName)
	state := readState(t, task.Root)
	if state.State != StateMerged {
		t.Fatalf("state.State = %q, want %q", state.State, StateMerged)
	}
	if state.BackupRoot == "" || state.MergedAt == nil {
		t.Fatalf("confirmed state should include backup_root and merged_at: %#v", state)
	}

	second, err := manager.ConfirmTaskWorkspace(context.Background(), "42", "tsk_123")
	if err != nil {
		t.Fatalf("second ConfirmTaskWorkspace() error = %v", err)
	}
	if second.BackupRoot != merged.BackupRoot {
		t.Fatalf("second backup root = %q, want original %q", second.BackupRoot, merged.BackupRoot)
	}
}

func TestConfirmTaskWorkspaceRestoresBackupWhenReplacementFails(t *testing.T) {
	templateRoot := t.TempDir()
	workspacesRoot := t.TempDir()
	writeFile(t, templateRoot, "AGENTS.md", "# Workspace rules\n")
	manager := newTestManager(t, templateRoot, workspacesRoot)

	home, err := manager.EnsureHomeWorkspace(context.Background(), "42")
	if err != nil {
		t.Fatalf("EnsureHomeWorkspace() error = %v", err)
	}
	writeFile(t, home.Root, "home-only.txt", "restore me")
	task, err := manager.CreateTaskWorkspace(context.Background(), "42", "tsk_restore", ModeMutable)
	if err != nil {
		t.Fatalf("CreateTaskWorkspace() error = %v", err)
	}
	externalRoot := t.TempDir()
	if err := os.Symlink(externalRoot, filepath.Join(task.Root, "bad-link")); err != nil {
		if errors.Is(err, os.ErrPermission) {
			t.Skipf("symlink creation not permitted: %v", err)
		}
		t.Fatalf("os.Symlink() error = %v", err)
	}
	if _, err := manager.MarkTaskWorkspacePendingMerge(context.Background(), "42", "tsk_restore"); err != nil {
		t.Fatalf("MarkTaskWorkspacePendingMerge() error = %v", err)
	}

	_, err = manager.ConfirmTaskWorkspace(context.Background(), "42", "tsk_restore")
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("ConfirmTaskWorkspace() error = %v, want symlink copy failure", err)
	}
	assertFileContent(t, home.Root, "home-only.txt", "restore me")
	assertPathMissing(t, home.Root, "bad-link")
}

func TestConfirmTaskWorkspaceRebuildsExistingBackupBeforeMerge(t *testing.T) {
	templateRoot := t.TempDir()
	workspacesRoot := t.TempDir()
	writeFile(t, templateRoot, "AGENTS.md", "# Workspace rules\n")
	manager := newTestManager(t, templateRoot, workspacesRoot)

	home, err := manager.EnsureHomeWorkspace(context.Background(), "42")
	if err != nil {
		t.Fatalf("EnsureHomeWorkspace() error = %v", err)
	}
	writeFile(t, home.Root, "home-only.txt", "complete backup")
	task, err := manager.CreateTaskWorkspace(context.Background(), "42", "tsk_partial_backup", ModeMutable)
	if err != nil {
		t.Fatalf("CreateTaskWorkspace() error = %v", err)
	}
	if _, err := manager.MarkTaskWorkspacePendingMerge(context.Background(), "42", "tsk_partial_backup"); err != nil {
		t.Fatalf("MarkTaskWorkspacePendingMerge() error = %v", err)
	}
	partialBackupRoot := filepath.Join(workspacesRoot, "users", "42", "backups", "partial")
	if err := os.MkdirAll(partialBackupRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(partial backup) error = %v", err)
	}
	setState(t, task.Root, func(state *WorkspaceStateFile) {
		state.BackupRoot = partialBackupRoot
	})

	if _, err := manager.ConfirmTaskWorkspace(context.Background(), "42", "tsk_partial_backup"); err != nil {
		t.Fatalf("ConfirmTaskWorkspace() error = %v", err)
	}
	assertFileContent(t, partialBackupRoot, "home-only.txt", "complete backup")
}

func TestConfirmTaskWorkspaceIgnoresTamperedStatePaths(t *testing.T) {
	templateRoot := t.TempDir()
	workspacesRoot := t.TempDir()
	writeFile(t, templateRoot, "AGENTS.md", "# Workspace rules\n")
	manager := newTestManager(t, templateRoot, workspacesRoot)

	victimHome, err := manager.EnsureHomeWorkspace(context.Background(), "victim")
	if err != nil {
		t.Fatalf("EnsureHomeWorkspace(victim) error = %v", err)
	}
	writeFile(t, victimHome.Root, "victim.txt", "do not touch")
	home, err := manager.EnsureHomeWorkspace(context.Background(), "42")
	if err != nil {
		t.Fatalf("EnsureHomeWorkspace(42) error = %v", err)
	}
	writeFile(t, home.Root, "owner.txt", "replace me")
	task, err := manager.CreateTaskWorkspace(context.Background(), "42", "tsk_tampered", ModeMutable)
	if err != nil {
		t.Fatalf("CreateTaskWorkspace() error = %v", err)
	}
	writeFile(t, task.Root, "owner.txt", "merged owner data")
	if _, err := manager.MarkTaskWorkspacePendingMerge(context.Background(), "42", "tsk_tampered"); err != nil {
		t.Fatalf("MarkTaskWorkspacePendingMerge() error = %v", err)
	}
	setState(t, task.Root, func(state *WorkspaceStateFile) {
		state.UserID = "victim"
		state.TaskID = "evil"
		state.HomeRoot = victimHome.Root
		state.TaskRoot = filepath.Join(workspacesRoot, "users", "victim", "tasks", "evil")
		state.BackupRoot = filepath.Join(workspacesRoot, "users", "victim", "backups", "evil")
	})

	merged, err := manager.ConfirmTaskWorkspace(context.Background(), "42", "tsk_tampered")
	if err != nil {
		t.Fatalf("ConfirmTaskWorkspace() error = %v", err)
	}

	if merged.UserID != "42" || merged.TaskID != "tsk_tampered" {
		t.Fatalf("merged identifiers = %#v, want service-derived user/task", merged)
	}
	if merged.HomeRoot != home.Root || merged.TaskRoot != task.Root {
		t.Fatalf("merged paths = home %q task %q, want %q %q", merged.HomeRoot, merged.TaskRoot, home.Root, task.Root)
	}
	if strings.Contains(merged.BackupRoot, "victim") {
		t.Fatalf("merged.BackupRoot = %q, want owner backup path", merged.BackupRoot)
	}
	assertFileContent(t, home.Root, "owner.txt", "merged owner data")
	assertFileContent(t, victimHome.Root, "victim.txt", "do not touch")
}

func TestConfirmAndDiscardTaskWorkspaceActionsAreSerializedPerUser(t *testing.T) {
	templateRoot := t.TempDir()
	workspacesRoot := t.TempDir()
	writeFile(t, templateRoot, "AGENTS.md", "# Workspace rules\n")
	manager := newTestManager(t, templateRoot, workspacesRoot)

	if _, err := manager.EnsureHomeWorkspace(context.Background(), "42"); err != nil {
		t.Fatalf("EnsureHomeWorkspace() error = %v", err)
	}
	task, err := manager.CreateTaskWorkspace(context.Background(), "42", "tsk_race", ModeMutable)
	if err != nil {
		t.Fatalf("CreateTaskWorkspace() error = %v", err)
	}
	writeFile(t, task.Root, "task-output.txt", "merged data")
	if _, err := manager.MarkTaskWorkspacePendingMerge(context.Background(), "42", "tsk_race"); err != nil {
		t.Fatalf("MarkTaskWorkspacePendingMerge() error = %v", err)
	}

	confirmPaused := make(chan struct{})
	releaseConfirm := make(chan struct{})
	var nowCalls int
	manager.now = func() time.Time {
		nowCalls++
		if nowCalls == 1 {
			close(confirmPaused)
			<-releaseConfirm
		}
		return time.Date(2026, 5, 23, 12, nowCalls, 0, 0, time.UTC)
	}

	confirmResult := make(chan *WorkspaceStateFile, 1)
	confirmErr := make(chan error, 1)
	go func() {
		state, err := manager.ConfirmTaskWorkspace(context.Background(), "42", "tsk_race")
		confirmResult <- state
		confirmErr <- err
	}()

	select {
	case <-confirmPaused:
	case <-time.After(2 * time.Second):
		t.Fatal("ConfirmTaskWorkspace did not pause after reading pending state")
	}

	discardResult := make(chan *WorkspaceStateFile, 1)
	discardErr := make(chan error, 1)
	go func() {
		state, err := manager.DiscardTaskWorkspace(context.Background(), "42", "tsk_race")
		discardResult <- state
		discardErr <- err
	}()

	select {
	case state := <-discardResult:
		close(releaseConfirm)
		if err := <-discardErr; err != nil {
			t.Fatalf("DiscardTaskWorkspace() early error = %v", err)
		}
		if err := <-confirmErr; err != nil {
			t.Fatalf("ConfirmTaskWorkspace() after early discard error = %v", err)
		}
		<-confirmResult
		t.Fatalf("DiscardTaskWorkspace completed while confirm was in progress with state %#v", state)
	case <-time.After(100 * time.Millisecond):
	}

	close(releaseConfirm)
	if err := <-confirmErr; err != nil {
		t.Fatalf("ConfirmTaskWorkspace() error = %v", err)
	}
	if state := <-confirmResult; state.State != StateMerged {
		t.Fatalf("confirm state = %q, want %q", state.State, StateMerged)
	}
	if err := <-discardErr; err != nil {
		t.Fatalf("DiscardTaskWorkspace() error = %v", err)
	}
	if state := <-discardResult; state.State != StateMerged {
		t.Fatalf("discard state = %q, want serialized action to observe merged", state.State)
	}
}

func TestDiscardTaskWorkspacePreservesTaskWorkspaceAndMarksDiscarded(t *testing.T) {
	templateRoot := t.TempDir()
	workspacesRoot := t.TempDir()
	writeFile(t, templateRoot, "AGENTS.md", "# Workspace rules\n")
	manager := newTestManager(t, templateRoot, workspacesRoot)

	if _, err := manager.EnsureHomeWorkspace(context.Background(), "42"); err != nil {
		t.Fatalf("EnsureHomeWorkspace() error = %v", err)
	}
	task, err := manager.CreateTaskWorkspace(context.Background(), "42", "tsk_123", ModeMutable)
	if err != nil {
		t.Fatalf("CreateTaskWorkspace() error = %v", err)
	}
	writeFile(t, task.Root, "task-output.txt", "keep me")
	if _, err := manager.MarkTaskWorkspacePendingMerge(context.Background(), "42", "tsk_123"); err != nil {
		t.Fatalf("MarkTaskWorkspacePendingMerge() error = %v", err)
	}

	discarded, err := manager.DiscardTaskWorkspace(context.Background(), "42", "tsk_123")
	if err != nil {
		t.Fatalf("DiscardTaskWorkspace() error = %v", err)
	}

	if discarded.State != StateDiscarded || discarded.DiscardedAt == nil {
		t.Fatalf("discarded state = %#v, want discarded with timestamp", discarded)
	}
	assertFileContent(t, task.Root, "task-output.txt", "keep me")
}

func TestPublicAPIUsesPlannedNamesAndSignatures(t *testing.T) {
	templateRoot := t.TempDir()
	workspacesRoot := t.TempDir()
	writeFile(t, templateRoot, "AGENTS.md", "# Workspace rules\n")

	manager, err := NewManager(Config{
		TemplateRoot: templateRoot,
		Root:         workspacesRoot,
		Now:          time.Now,
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	if _, err := manager.CreateTaskWorkspace(context.Background(), "42", "tsk_123", ModeMutable); err != nil {
		t.Fatalf("CreateTaskWorkspace() error = %v", err)
	}
	if _, err := manager.MarkTaskWorkspacePendingMerge(context.Background(), "42", "tsk_123"); err != nil {
		t.Fatalf("MarkTaskWorkspacePendingMerge() error = %v", err)
	}
	if _, err := manager.ConfirmTaskWorkspace(context.Background(), "42", "tsk_123"); err != nil {
		t.Fatalf("ConfirmTaskWorkspace() error = %v", err)
	}
	if _, err := manager.DiscardTaskWorkspace(context.Background(), "42", "tsk_123"); err != nil {
		t.Fatalf("DiscardTaskWorkspace() error = %v", err)
	}

	var _ Mode = ModeReadonly
	var _ State = StatePendingMerge
	var _ State = StateMerged
	var _ State = StateDiscarded
	var _ State = StateActive
	var _ State = StateCompleted
}

func TestEnsureHomeWorkspaceFailsWhenTemplateAgentsMissing(t *testing.T) {
	templateRoot := t.TempDir()
	workspacesRoot := t.TempDir()
	manager := newTestManager(t, templateRoot, workspacesRoot)

	_, err := manager.EnsureHomeWorkspace(context.Background(), "42")
	if err == nil || !strings.Contains(err.Error(), "AGENTS.md") {
		t.Fatalf("EnsureHomeWorkspace() error = %v, want missing AGENTS.md error", err)
	}
}

func TestManagerRejectsSymlinksAndPathEscapes(t *testing.T) {
	templateRoot := t.TempDir()
	workspacesRoot := t.TempDir()
	writeFile(t, templateRoot, "AGENTS.md", "# Workspace rules\n")
	manager := newTestManager(t, templateRoot, workspacesRoot)

	if _, err := manager.EnsureHomeWorkspace(context.Background(), ".."+string(os.PathSeparator)+"escape"); err == nil {
		t.Fatal("EnsureHomeWorkspace() error = nil, want path escape rejection")
	}

	if err := os.Symlink(filepath.Join(templateRoot, "AGENTS.md"), filepath.Join(templateRoot, "skills")); err != nil {
		if errors.Is(err, os.ErrPermission) {
			t.Skipf("symlink creation not permitted: %v", err)
		}
		t.Fatalf("os.Symlink() error = %v", err)
	}
	_, err := manager.EnsureHomeWorkspace(context.Background(), "42")
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("EnsureHomeWorkspace() error = %v, want symlink rejection", err)
	}
}

func TestConfirmTaskWorkspaceRejectsSymlinkedHomeAncestorBeforeReplacing(t *testing.T) {
	templateRoot := t.TempDir()
	workspacesRoot := t.TempDir()
	writeFile(t, templateRoot, "AGENTS.md", "# Workspace rules\n")
	manager := newTestManager(t, templateRoot, workspacesRoot)

	home, err := manager.EnsureHomeWorkspace(context.Background(), "42")
	if err != nil {
		t.Fatalf("EnsureHomeWorkspace() error = %v", err)
	}
	task, err := manager.CreateTaskWorkspace(context.Background(), "42", "tsk_123", ModeMutable)
	if err != nil {
		t.Fatalf("CreateTaskWorkspace() error = %v", err)
	}
	externalRoot := t.TempDir()
	if err := os.RemoveAll(home.Root); err != nil {
		t.Fatalf("RemoveAll(home) error = %v", err)
	}
	if err := os.Symlink(externalRoot, home.Root); err != nil {
		if errors.Is(err, os.ErrPermission) {
			t.Skipf("symlink creation not permitted: %v", err)
		}
		t.Fatalf("os.Symlink() error = %v", err)
	}
	writeFile(t, externalRoot, "outside.txt", "must not touch")
	backupRoot := filepath.Join(workspacesRoot, "backup-done")
	writeFile(t, backupRoot, "AGENTS.md", "# Backup\n")
	setState(t, task.Root, func(state *WorkspaceStateFile) {
		state.State = StatePendingMerge
		state.BackupRoot = backupRoot
	})

	_, err = manager.ConfirmTaskWorkspace(context.Background(), "42", "tsk_123")
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("ConfirmTaskWorkspace() error = %v, want symlink rejection", err)
	}
	assertFileContent(t, externalRoot, "outside.txt", "must not touch")
	assertFileContent(t, task.Root, "AGENTS.md", "# Workspace rules\n")
}

func TestCopyDirectoryContentsRejectsNestedDestinationSymlinkBeforeMkdirAll(t *testing.T) {
	sourceRoot := t.TempDir()
	destinationRoot := t.TempDir()
	writeFile(t, sourceRoot, filepath.Join("nested", "file.txt"), "content")
	externalRoot := t.TempDir()
	if err := os.Symlink(externalRoot, filepath.Join(destinationRoot, "nested")); err != nil {
		if errors.Is(err, os.ErrPermission) {
			t.Skipf("symlink creation not permitted: %v", err)
		}
		t.Fatalf("os.Symlink() error = %v", err)
	}

	err := copyDirectoryContents(sourceRoot, destinationRoot, nil)
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("copyDirectoryContents() error = %v, want symlink rejection", err)
	}
	assertPathMissing(t, externalRoot, "file.txt")
}

func TestCopyFileRejectsDestinationSymlink(t *testing.T) {
	sourceRoot := t.TempDir()
	destinationRoot := t.TempDir()
	externalRoot := t.TempDir()
	writeFile(t, sourceRoot, "source.txt", "safe content")
	writeFile(t, externalRoot, "outside.txt", "outside content")
	if err := os.Symlink(filepath.Join(externalRoot, "outside.txt"), filepath.Join(destinationRoot, "target.txt")); err != nil {
		if errors.Is(err, os.ErrPermission) {
			t.Skipf("symlink creation not permitted: %v", err)
		}
		t.Fatalf("os.Symlink() error = %v", err)
	}

	err := copyFile(filepath.Join(sourceRoot, "source.txt"), filepath.Join(destinationRoot, "target.txt"), 0o644)
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("copyFile() error = %v, want symlink rejection", err)
	}
	assertFileContent(t, externalRoot, "outside.txt", "outside content")
}

func newTestManager(t *testing.T, templateRoot string, workspacesRoot string) *Manager {
	t.Helper()
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	manager, err := NewManager(Config{
		TemplateRoot: templateRoot,
		Root:         workspacesRoot,
		Now: func() time.Time {
			now = now.Add(time.Minute)
			return now
		},
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	return manager
}

func writeFile(t *testing.T, root string, rel string, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func assertFileContent(t *testing.T, root string, rel string, want string) {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", filepath.Join(root, rel), err)
	}
	if got := string(content); got != want {
		t.Fatalf("%s content = %q, want %q", rel, got, want)
	}
}

func assertPathMissing(t *testing.T, root string, rel string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(root, rel)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s exists or stat failed with %v, want missing", filepath.Join(root, rel), err)
	}
}

func readState(t *testing.T, root string) WorkspaceStateFile {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, StateFileName))
	if err != nil {
		t.Fatalf("ReadFile(state) error = %v", err)
	}
	var state WorkspaceStateFile
	if err := json.Unmarshal(content, &state); err != nil {
		t.Fatalf("json.Unmarshal(state) error = %v", err)
	}
	return state
}

func setState(t *testing.T, root string, mutate func(*WorkspaceStateFile)) {
	t.Helper()
	state := readState(t, root)
	mutate(&state)
	state.UpdatedAt = state.UpdatedAt.Add(time.Minute)
	content, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("json.Marshal(state) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, StateFileName), content, 0o644); err != nil {
		t.Fatalf("WriteFile(state) error = %v", err)
	}
}

func removePath(t *testing.T, root string, rel string) {
	t.Helper()
	if err := os.Remove(filepath.Join(root, rel)); err != nil {
		t.Fatalf("Remove(%q) error = %v", filepath.Join(root, rel), err)
	}
}
