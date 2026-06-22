package workspaces

import (
	"archive/zip"
	"bytes"
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
	Path           string               `json:"path"`
	Name           string               `json:"name"`
	Type           string               `json:"type"`
	Size           int64                `json:"size,omitempty"`
	Binary         bool                 `json:"binary,omitempty"`
	HasDiff        bool                 `json:"has_diff,omitempty"`
	ChildrenLoaded bool                 `json:"children_loaded,omitempty"`
	Children       []*WorkspaceTreeNode `json:"children,omitempty"`
}

type WorkspaceSnapshot struct {
	ConversationID string             `json:"conversation_id"`
	TaskID         string             `json:"task_id"`
	UserID         string             `json:"user_id"`
	Path           string             `json:"path,omitempty"`
	HomeRoot       string             `json:"-"`
	TaskRoot       string             `json:"-"`
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

type WorkspaceDownload struct {
	Data        []byte
	FileName    string
	ContentType string
}

func NewBrowser(deps BrowserDeps) *Browser {
	return &Browser{deps: deps}
}

func (b *Browser) Snapshot(ctx context.Context, conversationID string, filterPath string) (*WorkspaceSnapshot, error) {
	resolved, err := b.resolve(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	cleanPath, err := normalizeBrowserPath(filterPath)
	if err != nil {
		return nil, err
	}
	if isHiddenBrowserPath(cleanPath) {
		return nil, browserPathNotFoundError(cleanPath)
	}
	tree, err := b.buildTree(resolved.TaskRoot, resolved.HomeRoot, cleanPath)
	if err != nil {
		return nil, err
	}
	return &WorkspaceSnapshot{
		ConversationID: resolved.ConversationID,
		TaskID:         resolved.Task.ID,
		UserID:         resolved.UserID,
		Path:           cleanPath,
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
	if isHiddenBrowserPath(cleanPath) {
		return nil, browserPathNotFoundError(cleanPath)
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
	if isHiddenBrowserPath(cleanPath) {
		return nil, browserPathNotFoundError(cleanPath)
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

func (b *Browser) Download(ctx context.Context, conversationID string, filePath string) (*WorkspaceDownload, error) {
	resolved, err := b.resolve(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	cleanPath, err := normalizeBrowserPath(filePath)
	if err != nil {
		return nil, err
	}
	if isHiddenBrowserPath(cleanPath) {
		return nil, browserPathNotFoundError(cleanPath)
	}
	taskPath, err := resolveBrowserPath(resolved.TaskRoot, cleanPath)
	if err != nil {
		return nil, err
	}
	_, data, err := readBrowserFile(taskPath)
	if err == nil {
		return &WorkspaceDownload{
			Data:        data,
			FileName:    filepath.Base(cleanPath),
			ContentType: "application/octet-stream",
		}, nil
	}
	if !infoIsDirectory(taskPath, err) {
		return nil, err
	}
	data, err = zipBrowserDirectory(resolved.TaskRoot, cleanPath)
	if err != nil {
		return nil, err
	}
	return &WorkspaceDownload{
		Data:        data,
		FileName:    browserDownloadZipFileName(cleanPath),
		ContentType: "application/zip",
	}, nil
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

func (b *Browser) buildTree(taskRoot string, homeRoot string, filterPath string) (*WorkspaceTreeNode, error) {
	if err := ensureNoSymlink(taskRoot); err != nil {
		return nil, err
	}
	currentRoot, err := resolveBrowserPath(taskRoot, filterPath)
	if err != nil {
		return nil, err
	}
	currentInfo, err := os.Lstat(currentRoot)
	if err != nil {
		return nil, err
	}
	if currentInfo.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("symlink paths are not supported: %s", currentRoot)
	}
	if !currentInfo.IsDir() {
		return nil, fmt.Errorf("workspace path is not a directory: %s", currentRoot)
	}

	rootNode := &WorkspaceTreeNode{
		Path:           filterPath,
		Name:           filepath.Base(filterPath),
		Type:           "dir",
		ChildrenLoaded: true,
	}
	if filterPath == "" {
		rootNode.Name = ""
	}
	entries, err := os.ReadDir(currentRoot)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		slashPath := browserChildPath(filterPath, entry.Name())
		if isHiddenBrowserPath(slashPath) {
			continue
		}
		currentPath := filepath.Join(currentRoot, entry.Name())
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("symlink paths are not supported: %s", currentPath)
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
				return nil, err
			}
			hasDiff, err := browserFileHasDiff(homeRoot, slashPath, data)
			if err != nil {
				return nil, err
			}
			node.Type = "file"
			node.Size = info.Size()
			node.Binary = !utf8.Valid(data)
			node.HasDiff = hasDiff
		}
		rootNode.Children = append(rootNode.Children, node)
	}
	sort.Slice(rootNode.Children, func(i, j int) bool {
		if rootNode.Children[i].Type != rootNode.Children[j].Type {
			return rootNode.Children[i].Type == "dir"
		}
		return rootNode.Children[i].Name < rootNode.Children[j].Name
	})
	return rootNode, nil
}

func browserChildPath(parentPath string, childName string) string {
	if parentPath == "" {
		return filepath.ToSlash(childName)
	}
	return parentPath + "/" + filepath.ToSlash(childName)
}

func browserFileHasDiff(homeRoot string, filePath string, taskData []byte) (bool, error) {
	homePath, err := resolveBrowserPath(homeRoot, filePath)
	if err != nil {
		return false, err
	}
	_, homeData, err := readBrowserFile(homePath)
	if err != nil {
		if os.IsNotExist(err) || strings.Contains(err.Error(), "workspace path is a directory") {
			return true, nil
		}
		return false, err
	}
	return !bytes.Equal(homeData, taskData), nil
}

func zipBrowserDirectory(taskRoot string, basePath string) ([]byte, error) {
	if err := ensureNoSymlink(taskRoot); err != nil {
		return nil, err
	}
	directoryPath, err := resolveBrowserPath(taskRoot, basePath)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(directoryPath)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("symlink paths are not supported: %s", directoryPath)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("workspace path is not a directory: %s", directoryPath)
	}

	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	walkErr := filepath.WalkDir(directoryPath, func(currentPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if currentPath == directoryPath {
			return nil
		}
		relativeToBase, err := filepath.Rel(directoryPath, currentPath)
		if err != nil {
			return err
		}
		relativeToBase = filepath.ToSlash(relativeToBase)
		entryPath := browserChildPath(basePath, relativeToBase)
		if isHiddenBrowserZipPath(basePath, entryPath) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink paths are not supported: %s", currentPath)
		}
		if entry.IsDir() {
			return nil
		}
		info, data, err := readBrowserFileMustStayInWorkspace(taskRoot, entryPath)
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(entryPath)
		header.Method = zip.Deflate
		fileWriter, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		_, err = fileWriter.Write(data)
		return err
	})
	closeErr := writer.Close()
	if walkErr != nil {
		return nil, walkErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return buffer.Bytes(), nil
}

func readBrowserFileMustStayInWorkspace(taskRoot string, relativePath string) (fs.FileInfo, []byte, error) {
	path, err := resolveBrowserPath(taskRoot, relativePath)
	if err != nil {
		return nil, nil, err
	}
	return readBrowserFile(path)
}

func browserDownloadZipFileName(cleanPath string) string {
	if strings.TrimSpace(cleanPath) == "" {
		return "workspace.zip"
	}
	return filepath.Base(cleanPath) + ".zip"
}

func infoIsDirectory(path string, readErr error) bool {
	if readErr == nil {
		return false
	}
	info, statErr := os.Lstat(path)
	return statErr == nil && info.IsDir()
}

func isHiddenBrowserZipPath(basePath string, entryPath string) bool {
	if isHiddenBrowserPath(entryPath) {
		return true
	}
	trimmedBase := strings.Trim(filepath.ToSlash(strings.TrimSpace(basePath)), "/")
	trimmedEntry := strings.Trim(filepath.ToSlash(strings.TrimSpace(entryPath)), "/")
	if trimmedBase == "" {
		return false
	}
	if trimmedEntry == trimmedBase {
		return isHiddenBrowserPath("")
	}
	prefix := trimmedBase + "/"
	if !strings.HasPrefix(trimmedEntry, prefix) {
		return false
	}
	relativeToBase := strings.TrimPrefix(trimmedEntry, prefix)
	return pathHasHiddenBrowserSuffix(relativeToBase)
}

func pathHasHiddenBrowserSuffix(relativePath string) bool {
	slashPath := strings.Trim(filepath.ToSlash(strings.TrimSpace(relativePath)), "/")
	for slashPath != "" {
		if isHiddenBrowserPath(slashPath) {
			return true
		}
		index := strings.Index(slashPath, "/")
		if index < 0 {
			break
		}
		slashPath = slashPath[index+1:]
	}
	return false
}

func isHiddenBrowserPath(relativePath string) bool {
	slashPath := filepath.ToSlash(strings.TrimSpace(relativePath))
	if slashPath == "" {
		return false
	}
	if isWorkspaceSidecar(slashPath) {
		return true
	}
	return slashPath == "AGENTS.md" || slashPath == "skills" || strings.HasPrefix(slashPath, "skills/")
}

func browserPathNotFoundError(relativePath string) error {
	if strings.TrimSpace(relativePath) == "" {
		return fmt.Errorf("workspace path not found: %w", os.ErrNotExist)
	}
	return fmt.Errorf("workspace path not found: %s: %w", relativePath, os.ErrNotExist)
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
