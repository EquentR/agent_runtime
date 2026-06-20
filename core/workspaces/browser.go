package workspaces

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	coreaudit "github.com/EquentR/agent_runtime/core/audit"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	"github.com/pmezard/go-difflib/difflib"
)

type BrowserDeps struct {
	TaskStore        BrowserTaskStore
	AuditStore       BrowserAuditStore
	WorkspaceManager BrowserWorkspaceResolver
}

type BrowserTaskStore interface {
	GetTask(ctx context.Context, id string) (*coretasks.Task, error)
	FindLatestTaskByConversation(ctx context.Context, conversationID string) (*coretasks.Task, error)
}

type BrowserAuditStore interface {
	GetLatestRunByConversationID(ctx context.Context, conversationID string) (*coreaudit.Run, error)
}

type BrowserWorkspaceResolver interface {
	homeRoot(userID string) (string, error)
	taskRoot(userID string, taskID string) (string, error)
}

type Browser struct {
	deps BrowserDeps
}

type WorkspaceTreeNode struct {
	Path     string             `json:"path"`
	Name     string             `json:"name"`
	Type     string             `json:"type"`
	Size     int64              `json:"size,omitempty"`
	Binary   bool               `json:"binary,omitempty"`
	Children []*WorkspaceTreeNode `json:"children,omitempty"`
}

type WorkspaceSnapshot struct {
	ConversationID string             `json:"conversation_id"`
	TaskID         string             `json:"task_id"`
	UserID         string             `json:"user_id"`
	HomeRoot       string             `json:"home_root"`
	TaskRoot       string             `json:"task_root"`
	Tree           *WorkspaceTreeNode `json:"tree,omitempty"`
}

type WorkspaceFileDetail struct {
	ConversationID string `json:"conversation_id"`
	TaskID         string `json:"task_id"`
	UserID         string `json:"user_id"`
	Path           string `json:"path"`
	Name           string `json:"name"`
	Size           int64  `json:"size"`
	Binary         bool   `json:"binary"`
	Content        string `json:"content,omitempty"`
}

type WorkspaceDiffResult struct {
	ConversationID string `json:"conversation_id"`
	TaskID         string `json:"task_id"`
	UserID         string `json:"user_id"`
	Path           string `json:"path"`
	Binary         bool   `json:"binary"`
	UnifiedDiff    string `json:"unified_diff,omitempty"`
	HomeContent    string `json:"home_content,omitempty"`
	TaskContent    string `json:"task_content,omitempty"`
}

func NewBrowser(deps BrowserDeps) *Browser {
	return &Browser{deps: deps}
}

func (b *Browser) Snapshot(ctx context.Context, conversationID string, filterPath string) (*WorkspaceSnapshot, error) {
	resolved, err := b.resolve(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	tree, err := b.buildTree(resolved.TaskRoot, filterPath)
	if err != nil {
		return nil, err
	}
	return &WorkspaceSnapshot{
		ConversationID: resolved.ConversationID,
		TaskID:         resolved.Task.ID,
		UserID:         resolved.UserID,
		HomeRoot:       resolved.HomeRoot,
		TaskRoot:       resolved.TaskRoot,
		Tree:           tree,
	}, nil
}

func (b *Browser) File(ctx context.Context, conversationID string, filePath string) (*WorkspaceFileDetail, error) {
	resolved, err := b.resolve(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	cleanPath, err := normalizeBrowserPath(filePath)
	if err != nil {
		return nil, err
	}
	taskPath, err := resolveBrowserPath(resolved.TaskRoot, cleanPath)
	if err != nil {
		return nil, err
	}
	info, data, err := readBrowserFile(taskPath)
	if err != nil {
		return nil, err
	}
	detail := &WorkspaceFileDetail{
		ConversationID: resolved.ConversationID,
		TaskID:         resolved.Task.ID,
		UserID:         resolved.UserID,
		Path:           cleanPath,
		Name:           filepath.Base(cleanPath),
		Size:           info.Size(),
		Binary:         !utf8.Valid(data),
	}
	if !detail.Binary {
		detail.Content = string(data)
	}
	return detail, nil
}

func (b *Browser) Diff(ctx context.Context, conversationID string, filePath string) (*WorkspaceDiffResult, error) {
	resolved, err := b.resolve(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	cleanPath, err := normalizeBrowserPath(filePath)
	if err != nil {
		return nil, err
	}
	taskPath, err := resolveBrowserPath(resolved.TaskRoot, cleanPath)
	if err != nil {
		return nil, err
	}
	homePath, err := resolveBrowserPath(resolved.HomeRoot, cleanPath)
	if err != nil {
		return nil, err
	}
	taskInfo, taskData, err := readBrowserFile(taskPath)
	if err != nil {
		return nil, err
	}
	homeInfo, homeData, homeErr := readBrowserFile(homePath)
	homeMissing := false
	if homeErr != nil {
		if os.IsNotExist(homeErr) {
			homeMissing = true
		} else {
			return nil, homeErr
		}
	}
	taskBinary := !utf8.Valid(taskData)
	homeBinary := !homeMissing && !utf8.Valid(homeData)
	if taskBinary || homeBinary {
		return &WorkspaceDiffResult{
			ConversationID: resolved.ConversationID,
			TaskID:         resolved.Task.ID,
			UserID:         resolved.UserID,
			Path:           cleanPath,
			Binary:         true,
		}, nil
	}
	diffText, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(homeData)),
		FromFile: "home/" + cleanPath,
		B:        difflib.SplitLines(string(taskData)),
		ToFile:   "task/" + cleanPath,
		Context:  3,
	})
	if err != nil {
		return nil, err
	}
	_ = taskInfo
	_ = homeInfo
	return &WorkspaceDiffResult{
		ConversationID: resolved.ConversationID,
		TaskID:         resolved.Task.ID,
		UserID:         resolved.UserID,
		Path:           cleanPath,
		Binary:         false,
		UnifiedDiff:    diffText,
		HomeContent:    string(homeData),
		TaskContent:    string(taskData),
	}, nil
}

func (b *Browser) DiffFile(ctx context.Context, conversationID string, filePath string) (*WorkspaceDiffResult, error) {
	return b.Diff(ctx, conversationID, filePath)
}

func (b *Browser) Download(ctx context.Context, conversationID string, filePath string) ([]byte, string, error) {
	resolved, err := b.resolve(ctx, conversationID)
	if err != nil {
		return nil, "", err
	}
	cleanPath, err := normalizeBrowserPath(filePath)
	if err != nil {
		return nil, "", err
	}
	taskPath, err := resolveBrowserPath(resolved.TaskRoot, cleanPath)
	if err != nil {
		return nil, "", err
	}
	_, data, err := readBrowserFile(taskPath)
	if err != nil {
		return nil, "", err
	}
	return data, filepath.Base(cleanPath), nil
}

type browserResolution struct {
	ConversationID string
	UserID         string
	WorkspaceID    string
	HomeRoot       string
	TaskRoot       string
	Task           *coretasks.Task
}

func (b *Browser) resolve(ctx context.Context, conversationID string) (*browserResolution, error) {
	if b.deps.WorkspaceManager == nil {
		return nil, fmt.Errorf("workspace manager is not configured")
	}
	task, err := b.resolveLatestTask(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, fmt.Errorf("task not found for conversation: %s", strings.TrimSpace(conversationID))
	}

	input := map[string]any{}
	if len(task.InputJSON) > 0 {
		if err := json.Unmarshal(task.InputJSON, &input); err != nil {
			return nil, fmt.Errorf("decode task input: %w", err)
		}
	}
	workspaceID := browserWorkspaceID(task, input)
	userID := firstBrowserString(
		getString(input, "workspace_user_id"),
		getString(input, "user_id"),
		task.CreatedBy,
	)
	if userID == "" {
		return nil, fmt.Errorf("workspace user id is missing")
	}
	homeRoot, err := b.deps.WorkspaceManager.homeRoot(userID)
	if err != nil {
		return nil, err
	}
	taskRoot, err := b.deps.WorkspaceManager.taskRoot(userID, workspaceID)
	if err != nil {
		return nil, err
	}
	return &browserResolution{
		ConversationID: strings.TrimSpace(conversationID),
		UserID:         userID,
		WorkspaceID:    workspaceID,
		HomeRoot:       homeRoot,
		TaskRoot:       taskRoot,
		Task:           task,
	}, nil
}

func (b *Browser) resolveLatestTask(ctx context.Context, conversationID string) (*coretasks.Task, error) {
	if b.deps.TaskStore != nil {
		task, err := b.deps.TaskStore.FindLatestTaskByConversation(ctx, conversationID)
		if err != nil {
			return nil, err
		}
		if task != nil {
			return task, nil
		}
	}
	if b.deps.AuditStore != nil && b.deps.TaskStore != nil {
		run, err := b.deps.AuditStore.GetLatestRunByConversationID(ctx, conversationID)
		if err != nil {
			return nil, err
		}
		if run == nil {
			return nil, nil
		}
		return b.deps.TaskStore.GetTask(ctx, run.TaskID)
	}
	if b.deps.TaskStore == nil {
		return nil, fmt.Errorf("task store is not configured")
	}
	return nil, nil
}

func (b *Browser) buildTree(root string, filterPath string) (*WorkspaceTreeNode, error) {
	if err := ensureNoSymlink(root); err != nil {
		return nil, err
	}
	filterPath = strings.TrimSpace(filterPath)
	if filterPath != "" {
		normalized, err := normalizeBrowserPath(filterPath)
		if err != nil {
			return nil, err
		}
		filterPath = normalized
	}
	rootNode := &WorkspaceTreeNode{Type: "dir"}
	nodes := map[string]*WorkspaceTreeNode{"": rootNode}
	if err := filepath.WalkDir(root, func(currentPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if currentPath == root {
			return nil
		}
		rel, err := filepath.Rel(root, currentPath)
		if err != nil {
			return err
		}
		slashPath := filepath.ToSlash(rel)
		if isWorkspaceSidecar(slashPath) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if filterPath != "" && slashPath != filterPath {
			descendant := strings.HasPrefix(slashPath, filterPath+"/")
			ancestor := strings.HasPrefix(filterPath, slashPath+"/")
			if !descendant && !ancestor {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink paths are not supported: %s", currentPath)
		}
		parentKey := filepath.ToSlash(filepath.Dir(slashPath))
		if parentKey == "." {
			parentKey = ""
		}
		parent := nodes[parentKey]
		if parent == nil {
			parent = rootNode
		}
		node := &WorkspaceTreeNode{
			Path: slashPath,
			Name: filepath.Base(slashPath),
		}
		if entry.IsDir() {
			node.Type = "dir"
		} else {
			info, data, err := readBrowserFile(currentPath)
			if err != nil {
				return err
			}
			node.Type = "file"
			node.Size = info.Size()
			node.Binary = !utf8.Valid(data)
		}
		parent.Children = append(parent.Children, node)
		sort.Slice(parent.Children, func(i, j int) bool {
			if parent.Children[i].Type != parent.Children[j].Type {
				return parent.Children[i].Type == "dir"
			}
			return parent.Children[i].Name < parent.Children[j].Name
		})
		if entry.IsDir() {
			nodes[slashPath] = node
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return rootNode, nil
}

func resolveBrowserPath(root string, rel string) (string, error) {
	trimmed := strings.TrimSpace(rel)
	if trimmed == "" {
		return root, nil
	}
	normalized, err := normalizeBrowserPath(trimmed)
	if err != nil {
		return "", err
	}
	resolved := filepath.Clean(filepath.Join(root, filepath.FromSlash(normalized)))
	cleanRoot := filepath.Clean(root)
	relPath, err := filepath.Rel(cleanRoot, resolved)
	if err != nil {
		return "", err
	}
	if relPath == ".." || strings.HasPrefix(relPath, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes workspace root: %s", rel)
	}
	if err := ensureNoSymlink(resolved); err != nil {
		return "", err
	}
	return resolved, nil
}

func normalizeBrowserPath(rel string) (string, error) {
	trimmed := strings.TrimSpace(rel)
	if trimmed == "" {
		return "", nil
	}
	normalized := filepath.ToSlash(filepath.Clean(filepath.FromSlash(trimmed)))
	if normalized == "." {
		return "", nil
	}
	if filepath.IsAbs(normalized) || normalized == ".." || strings.HasPrefix(normalized, "../") {
		return "", fmt.Errorf("path escapes workspace: %s", rel)
	}
	if strings.Contains(normalized, ":") || strings.Contains(normalized, `\`) {
		return "", fmt.Errorf("path escapes workspace: %s", rel)
	}
	return strings.TrimPrefix(normalized, "./"), nil
}

func readBrowserFile(path string) (fs.FileInfo, []byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, fmt.Errorf("symlink paths are not supported: %s", path)
	}
	if info.IsDir() {
		return info, nil, fmt.Errorf("workspace path is a directory: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	return info, data, nil
}

func getString(input map[string]any, key string) string {
	if input == nil {
		return ""
	}
	raw, _ := input[key].(string)
	return strings.TrimSpace(raw)
}

func firstBrowserString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func browserWorkspaceID(task *coretasks.Task, input map[string]any) string {
	if task == nil {
		return ""
	}
	mode := strings.TrimSpace(getString(input, "workspace_mode"))
	if mode == "" {
		mode = string(ModeMutable)
	}
	if mode == string(ModeReadonly) {
		return strings.TrimSpace(task.ID)
	}
	return firstBrowserString(getString(input, "conversation_id"), task.ID)
}
