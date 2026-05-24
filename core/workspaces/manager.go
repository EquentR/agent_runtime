package workspaces

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const defaultWorkspacesRoot = "data/workspaces"

type Manager struct {
	templateRoot string
	root         string
	now          func() time.Time
	locksMu      sync.Mutex
	userLocks    map[string]*sync.Mutex
}

func NewManager(options Config) (*Manager, error) {
	templateRoot := normalizeRoot(options.TemplateRoot)
	if templateRoot == "" {
		return nil, fmt.Errorf("template root is required")
	}
	root := normalizeRoot(options.Root)
	if root == "" {
		root = defaultWorkspacesRoot
	}

	if err := ensureExistingDir(templateRoot); err != nil {
		return nil, fmt.Errorf("validate template root: %w", err)
	}
	if err := ensurePreparedDir(root); err != nil {
		return nil, fmt.Errorf("prepare workspaces root: %w", err)
	}

	now := options.Now
	if now == nil {
		now = time.Now
	}

	return &Manager{
		templateRoot: templateRoot,
		root:         root,
		now:          now,
		userLocks:    map[string]*sync.Mutex{},
	}, nil
}

func (m *Manager) EnsureHomeWorkspace(ctx context.Context, userID string) (*Workspace, error) {
	unlock := m.lockUserWorkspace(userID)
	defer unlock()
	return m.ensureHomeWorkspace(ctx, userID)
}

func (m *Manager) ensureHomeWorkspace(ctx context.Context, userID string) (*Workspace, error) {
	_ = ctx
	homeRoot, err := m.homeRoot(userID)
	if err != nil {
		return nil, err
	}
	if err := ensureNoSymlink(homeRoot); err != nil {
		return nil, err
	}
	if _, err := os.Stat(homeRoot); err == nil {
		if err := m.seedMissingHomeWorkspaceFiles(homeRoot); err != nil {
			return nil, err
		}
		return &Workspace{UserID: strings.TrimSpace(userID), Root: homeRoot}, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	if err := os.RemoveAll(homeRoot); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(homeRoot, 0o755); err != nil {
		return nil, err
	}
	if err := ensureNoSymlink(homeRoot); err != nil {
		return nil, err
	}

	if err := m.seedMissingHomeWorkspaceFiles(homeRoot); err != nil {
		return nil, err
	}

	return &Workspace{UserID: strings.TrimSpace(userID), Root: homeRoot}, nil
}

func (m *Manager) seedMissingHomeWorkspaceFiles(homeRoot string) error {
	agentsSource := filepath.Join(m.templateRoot, "AGENTS.md")
	if _, err := os.Lstat(agentsSource); err != nil {
		return fmt.Errorf("seed AGENTS.md: %w", err)
	}
	agentsDestination := filepath.Join(homeRoot, "AGENTS.md")
	if _, err := os.Stat(agentsDestination); errors.Is(err, os.ErrNotExist) {
		if err := copyPath(agentsSource, agentsDestination, nil); err != nil {
			return fmt.Errorf("seed AGENTS.md: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("seed AGENTS.md: %w", err)
	}

	if err := m.seedMissingSkills(homeRoot); err != nil {
		return err
	}

	return nil
}

func (m *Manager) seedMissingSkills(homeRoot string) error {
	skillsSource := filepath.Join(m.templateRoot, "skills")
	if err := ensureNoSymlink(skillsSource); err != nil {
		return fmt.Errorf("seed skills: %w", err)
	}
	sourceInfo, err := os.Stat(skillsSource)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !sourceInfo.IsDir() {
		return fmt.Errorf("seed skills: %s is not a directory", skillsSource)
	}

	skillsDestination := filepath.Join(homeRoot, "skills")
	if err := ensureNoSymlink(skillsDestination); err != nil {
		return fmt.Errorf("seed skills: %w", err)
	}
	if destInfo, err := os.Lstat(skillsDestination); err == nil {
		if !destInfo.IsDir() {
			return fmt.Errorf("seed skills: %s exists and is not a directory", skillsDestination)
		}
	} else if os.IsNotExist(err) {
		if err := copyPath(skillsSource, skillsDestination, nil); err != nil {
			return fmt.Errorf("seed skills: %w", err)
		}
		return nil
	} else {
		return fmt.Errorf("seed skills: %w", err)
	}

	return filepath.WalkDir(skillsSource, func(currentPath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if currentPath == skillsSource {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink paths are not supported: %s", currentPath)
		}

		relativePath, err := filepath.Rel(skillsSource, currentPath)
		if err != nil {
			return err
		}
		destinationPath := filepath.Join(skillsDestination, relativePath)

		if entry.IsDir() {
			if info, err := os.Lstat(destinationPath); err == nil {
				if !info.IsDir() {
					return fmt.Errorf("seed skills: %s exists and is not a directory", destinationPath)
				}
				return nil
			} else if !os.IsNotExist(err) {
				return err
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			return os.MkdirAll(destinationPath, info.Mode().Perm())
		}

		if info, err := os.Lstat(destinationPath); err == nil {
			if info.IsDir() {
				return fmt.Errorf("seed skills: %s exists and is a directory", destinationPath)
			}
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}
		return copyFile(currentPath, destinationPath, info.Mode().Perm())
	})
}

func (m *Manager) CreateTaskWorkspace(ctx context.Context, userID string, taskID string, mode Mode) (*Workspace, error) {
	unlock := m.lockUserWorkspace(userID)
	defer unlock()
	home, err := m.ensureHomeWorkspace(ctx, userID)
	if err != nil {
		return nil, err
	}
	taskRoot, err := m.taskRoot(userID, taskID)
	if err != nil {
		return nil, err
	}

	if err := ensureNoSymlink(taskRoot); err != nil {
		return nil, err
	}
	if _, err := os.Stat(taskRoot); err == nil {
		if state, ok, err := m.loadState(taskRoot); err != nil {
			return nil, err
		} else if ok {
			return &Workspace{UserID: strings.TrimSpace(userID), TaskID: strings.TrimSpace(taskID), Root: taskRoot, State: state.State}, nil
		}
		if err := os.RemoveAll(taskRoot); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	if err := os.MkdirAll(taskRoot, 0o755); err != nil {
		return nil, err
	}
	if err := ensureNoSymlink(taskRoot); err != nil {
		return nil, err
	}

	filter := func(relativePath string, entry os.DirEntry) bool {
		_ = entry
		return filepath.ToSlash(relativePath) == StateFileName
	}
	if err := copyDirectoryContents(home.Root, taskRoot, filter); err != nil {
		return nil, fmt.Errorf("copy home workspace: %w", err)
	}

	now := m.nowUTC()
	state := WorkspaceStateFile{
		TaskID:    strings.TrimSpace(taskID),
		UserID:    strings.TrimSpace(userID),
		Mode:      mode,
		State:     workspaceStateForMode(mode),
		HomeRoot:  home.Root,
		TaskRoot:  taskRoot,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := m.saveState(taskRoot, state); err != nil {
		return nil, err
	}

	return &Workspace{UserID: state.UserID, TaskID: state.TaskID, Root: taskRoot, State: state.State}, nil
}

func (m *Manager) ConfirmTaskWorkspace(ctx context.Context, userID string, taskID string) (*WorkspaceStateFile, error) {
	_ = ctx
	unlock := m.lockUserWorkspace(userID)
	defer unlock()
	taskRoot, err := m.taskRoot(userID, taskID)
	if err != nil {
		return nil, err
	}
	state, ok, err := m.loadState(taskRoot)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("workspace state not found: %s", taskRoot)
	}
	if err := m.normalizeLoadedState(userID, taskID, taskRoot, &state); err != nil {
		return nil, err
	}
	if state.State == StateMerged || state.State == StateDiscarded {
		return &state, nil
	}
	if state.State != StatePendingMerge {
		return nil, fmt.Errorf("workspace is not ready for confirmation: %s", state.State)
	}

	backupRoot := state.BackupRoot
	if !m.isValidBackupRoot(state.UserID, backupRoot) {
		backupRoot = filepath.Join(m.root, "users", state.UserID, "backups", fmt.Sprintf("%s-%s", state.TaskID, m.nowUTC().Format("20060102T150405Z")))
		state.BackupRoot = backupRoot
		state.UpdatedAt = m.nowUTC()
		if err := m.saveState(taskRoot, state); err != nil {
			return nil, err
		}
	}

	if err := ensureNoSymlink(filepath.Dir(backupRoot)); err != nil {
		return nil, err
	}
	if err := replaceDirectoryContents(state.HomeRoot, backupRoot, nil); err != nil {
		return nil, fmt.Errorf("create backup: %w", err)
	}

	if err := replaceDirectoryContents(taskRoot, state.HomeRoot, func(relativePath string, entry os.DirEntry) bool {
		_ = entry
		return filepath.ToSlash(relativePath) == StateFileName
	}); err != nil {
		if restoreErr := replaceDirectoryContents(backupRoot, state.HomeRoot, nil); restoreErr != nil {
			return nil, fmt.Errorf("replace home workspace: %w (restore backup failed: %v)", err, restoreErr)
		}
		return nil, fmt.Errorf("replace home workspace: %w", err)
	}

	now := m.nowUTC()
	state.State = StateMerged
	state.MergedAt = &now
	state.UpdatedAt = now
	if err := m.saveState(taskRoot, state); err != nil {
		return nil, err
	}
	return &state, nil
}

func (m *Manager) MarkTaskWorkspacePendingMerge(ctx context.Context, userID string, taskID string) (*WorkspaceStateFile, error) {
	_ = ctx
	unlock := m.lockUserWorkspace(userID)
	defer unlock()
	taskRoot, err := m.taskRoot(userID, taskID)
	if err != nil {
		return nil, err
	}
	state, ok, err := m.loadState(taskRoot)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("workspace state not found: %s", taskRoot)
	}
	if err := m.normalizeLoadedState(userID, taskID, taskRoot, &state); err != nil {
		return nil, err
	}
	if state.State == StatePendingMerge || state.State == StateMerged || state.State == StateDiscarded || state.State == StateCompleted {
		return &state, nil
	}
	if state.Mode != ModeMutable {
		return nil, fmt.Errorf("workspace mode cannot enter pending merge: %s", state.Mode)
	}
	if state.State != StateActive {
		return nil, fmt.Errorf("workspace is not active: %s", state.State)
	}
	now := m.nowUTC()
	state.State = StatePendingMerge
	state.UpdatedAt = now
	if err := m.saveState(taskRoot, state); err != nil {
		return nil, err
	}
	return &state, nil
}

func (m *Manager) CompleteTaskWorkspace(ctx context.Context, userID string, taskID string, errorMessage string) (*WorkspaceStateFile, error) {
	_ = ctx
	unlock := m.lockUserWorkspace(userID)
	defer unlock()
	taskRoot, err := m.taskRoot(userID, taskID)
	if err != nil {
		return nil, err
	}
	state, ok, err := m.loadState(taskRoot)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("workspace state not found: %s", taskRoot)
	}
	if err := m.normalizeLoadedState(userID, taskID, taskRoot, &state); err != nil {
		return nil, err
	}
	if state.State == StateMerged || state.State == StateDiscarded || state.State == StateCompleted {
		return &state, nil
	}
	now := m.nowUTC()
	state.State = StateCompleted
	state.ErrorMessage = strings.TrimSpace(errorMessage)
	state.UpdatedAt = now
	if err := m.saveState(taskRoot, state); err != nil {
		return nil, err
	}
	return &state, nil
}

func (m *Manager) DiscardTaskWorkspace(ctx context.Context, userID string, taskID string) (*WorkspaceStateFile, error) {
	_ = ctx
	unlock := m.lockUserWorkspace(userID)
	defer unlock()
	taskRoot, err := m.taskRoot(userID, taskID)
	if err != nil {
		return nil, err
	}
	state, ok, err := m.loadState(taskRoot)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("workspace state not found: %s", taskRoot)
	}
	if err := m.normalizeLoadedState(userID, taskID, taskRoot, &state); err != nil {
		return nil, err
	}
	if state.State == StateDiscarded || state.State == StateMerged {
		return &state, nil
	}
	now := m.nowUTC()
	state.State = StateDiscarded
	state.DiscardedAt = &now
	state.UpdatedAt = now
	if err := m.saveState(taskRoot, state); err != nil {
		return nil, err
	}
	return &state, nil
}

func (m *Manager) SummarizeUserWorkspaces(ctx context.Context, userID string) (*UserWorkspaceSummary, error) {
	_ = ctx
	unlock := m.lockUserWorkspace(userID)
	defer unlock()
	homeRoot, err := m.homeRoot(userID)
	if err != nil {
		return nil, err
	}
	if err := ensureNoSymlink(homeRoot); err != nil {
		return nil, err
	}

	tasksRoot, err := m.resolveWorkspacePath("users", userID, "tasks")
	if err != nil {
		return nil, err
	}
	if err := ensureNoSymlink(tasksRoot); err != nil {
		return nil, err
	}

	summary := &UserWorkspaceSummary{
		UserID:   strings.TrimSpace(userID),
		HomeRoot: homeRoot,
		Tasks:    []TaskWorkspaceSummary{},
	}
	entries, err := os.ReadDir(tasksRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return summary, nil
		}
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		taskRoot := filepath.Join(tasksRoot, entry.Name())
		if err := ensureNoSymlink(taskRoot); err != nil {
			return nil, err
		}
		state, ok, err := m.loadState(taskRoot)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		summary.Tasks = append(summary.Tasks, TaskWorkspaceSummary{
			TaskID:      state.TaskID,
			Mode:        state.Mode,
			State:       state.State,
			TaskRoot:    state.TaskRoot,
			BackupRoot:  state.BackupRoot,
			CreatedAt:   state.CreatedAt,
			UpdatedAt:   state.UpdatedAt,
			MergedAt:    state.MergedAt,
			DiscardedAt: state.DiscardedAt,
		})
	}
	sort.Slice(summary.Tasks, func(i, j int) bool {
		return summary.Tasks[i].TaskID < summary.Tasks[j].TaskID
	})
	return summary, nil
}

func (m *Manager) lockUserWorkspace(userID string) func() {
	key := strings.TrimSpace(userID)
	if key == "" {
		key = "local"
	}
	m.locksMu.Lock()
	if m.userLocks == nil {
		m.userLocks = map[string]*sync.Mutex{}
	}
	lock := m.userLocks[key]
	if lock == nil {
		lock = &sync.Mutex{}
		m.userLocks[key] = lock
	}
	m.locksMu.Unlock()

	lock.Lock()
	return lock.Unlock
}

func (m *Manager) homeRoot(userID string) (string, error) {
	return m.resolveWorkspacePath("users", userID, "home")
}

func (m *Manager) taskRoot(userID string, taskID string) (string, error) {
	return m.resolveWorkspacePath("users", userID, "tasks", taskID)
}

func (m *Manager) resolveWorkspacePath(parts ...string) (string, error) {
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		normalized, err := normalizePathElement(part)
		if err != nil {
			return "", err
		}
		segments = append(segments, normalized)
	}
	resolved := filepath.Clean(filepath.Join(append([]string{m.root}, segments...)...))
	rel, err := filepath.Rel(m.root, resolved)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes workspace root: %s", resolved)
	}
	return resolved, nil
}

func (m *Manager) nowUTC() time.Time {
	return m.now().UTC()
}

func (m *Manager) loadState(taskRoot string) (WorkspaceStateFile, bool, error) {
	statePath := filepath.Join(taskRoot, StateFileName)
	if err := ensureNoSymlink(statePath); err != nil {
		return WorkspaceStateFile{}, false, err
	}
	content, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return WorkspaceStateFile{}, false, nil
		}
		return WorkspaceStateFile{}, false, err
	}
	var state WorkspaceStateFile
	if err := json.Unmarshal(content, &state); err != nil {
		return WorkspaceStateFile{}, false, err
	}
	return state, true, nil
}

func (m *Manager) saveState(taskRoot string, state WorkspaceStateFile) error {
	statePath := filepath.Join(taskRoot, StateFileName)
	if err := ensureNoSymlink(statePath); err != nil {
		return err
	}
	content, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return os.WriteFile(statePath, content, 0o644)
}

func (m *Manager) normalizeLoadedState(userID string, taskID string, taskRoot string, state *WorkspaceStateFile) error {
	if state == nil {
		return fmt.Errorf("workspace state is required")
	}
	homeRoot, err := m.homeRoot(userID)
	if err != nil {
		return err
	}
	canonicalTaskRoot, err := m.taskRoot(userID, taskID)
	if err != nil {
		return err
	}
	if filepath.Clean(taskRoot) != filepath.Clean(canonicalTaskRoot) {
		return fmt.Errorf("workspace task root mismatch: %s", taskRoot)
	}
	state.UserID = strings.TrimSpace(userID)
	state.TaskID = strings.TrimSpace(taskID)
	state.HomeRoot = homeRoot
	state.TaskRoot = canonicalTaskRoot
	if !m.isValidBackupRoot(state.UserID, state.BackupRoot) {
		state.BackupRoot = ""
	}
	if state.Mode != ModeMutable && state.Mode != ModeReadonly {
		state.Mode = ModeMutable
	}
	return nil
}

func (m *Manager) isValidBackupRoot(userID string, backupRoot string) bool {
	if strings.TrimSpace(backupRoot) == "" {
		return false
	}
	backupsRoot, err := m.resolveWorkspacePath("users", userID, "backups")
	if err != nil {
		return false
	}
	cleanBackupRoot := filepath.Clean(backupRoot)
	rel, err := filepath.Rel(backupsRoot, cleanBackupRoot)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func replaceDirectoryContents(sourceRoot string, destinationRoot string, filter func(relativePath string, entry os.DirEntry) bool) error {
	if err := ensureNoSymlink(destinationRoot); err != nil {
		return err
	}
	if err := os.RemoveAll(destinationRoot); err != nil {
		return err
	}
	if err := os.MkdirAll(destinationRoot, 0o755); err != nil {
		return err
	}
	return copyDirectoryContents(sourceRoot, destinationRoot, filter)
}

func ensureExistingDir(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("path is not a directory: %s", path)
	}
	return ensureNoSymlink(path)
}

func ensurePreparedDir(path string) error {
	if err := ensureNoSymlink(path); err != nil {
		return err
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}
	return ensureExistingDir(path)
}

func normalizeRoot(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	return filepath.Clean(filepath.FromSlash(trimmed))
}

func normalizePathElement(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", fmt.Errorf("path element is required")
	}
	normalized := filepath.Clean(filepath.FromSlash(trimmed))
	if filepath.IsAbs(normalized) {
		return "", fmt.Errorf("absolute paths are not allowed: %s", path)
	}
	if normalized == "." || normalized == ".." {
		return "", fmt.Errorf("path element escapes workspace: %s", path)
	}
	if normalized != filepath.Base(normalized) {
		return "", fmt.Errorf("path element escapes workspace: %s", path)
	}
	if strings.ContainsAny(normalized, `:/\`) {
		return "", fmt.Errorf("path element escapes workspace: %s", path)
	}
	return normalized, nil
}

func workspaceStateForMode(mode Mode) State {
	_ = mode
	return StateActive
}
