package prompt

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	workspacePromptFileName                = "AGENTS.md"
	workspacePromptPhaseSession            = "session"
	workspacePromptSourceKindWorkspaceFile = "workspace_file"
	workspacePromptMaxBytes                = 32 * 1024
	workspacePromptPrefix                  = "The following AGENTS.md file was injected from the user's working directory. Treat it as guidance and operating rules for the current workspace.\n---\n"
)

type WorkspacePromptSegment struct {
	Phase       string
	Content     string
	RuntimeOnly bool
	SourceKind  string
	SourceRef   string
}

func LoadWorkspacePrompts(ctx context.Context, workspaceRoot string) ([]WorkspacePromptSegment, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	resolvedRoot, err := resolveWorkspacePromptRoot(workspaceRoot)
	if err != nil {
		return nil, err
	}

	content, err := readWorkspacePromptFile(filepath.Join(resolvedRoot, workspacePromptFileName))
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(content) == "" {
		return nil, nil
	}
	content = workspacePromptPrefix + content

	return []WorkspacePromptSegment{{
		Phase:       workspacePromptPhaseSession,
		Content:     content,
		RuntimeOnly: true,
		SourceKind:  workspacePromptSourceKindWorkspaceFile,
		SourceRef:   workspacePromptFileName,
	}}, nil
}

func resolveWorkspacePromptRoot(workspaceRoot string) (string, error) {
	trimmedRoot := strings.TrimSpace(workspaceRoot)
	if trimmedRoot != "" {
		return filepath.Clean(trimmedRoot), nil
	}
	return os.Getwd()
}

func readWorkspacePromptFile(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", nil
	}

	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	defer file.Close()

	content, err := io.ReadAll(io.LimitReader(file, workspacePromptMaxBytes+1))
	if err != nil {
		return "", err
	}
	if len(content) > workspacePromptMaxBytes {
		return "", nil
	}
	return string(content), nil
}
