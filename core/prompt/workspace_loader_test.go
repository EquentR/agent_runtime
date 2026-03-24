package prompt

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadWorkspacePromptsLoadsAgentsFileFromWorkspaceRoot(t *testing.T) {
	workspaceRoot := t.TempDir()
	content := "# Workspace Instructions\nAlways explain the command before running it.\n"
	writeWorkspaceFile(t, workspaceRoot, workspacePromptFileName, content)

	segments, err := LoadWorkspacePrompts(context.Background(), workspaceRoot)
	if err != nil {
		t.Fatalf("LoadWorkspacePrompts() error = %v", err)
	}
	if len(segments) != 1 {
		t.Fatalf("len(segments) = %d, want 1", len(segments))
	}
	if segments[0].Content != content {
		t.Fatalf("segment content = %q, want %q", segments[0].Content, content)
	}
}

func TestLoadWorkspacePromptsMissingFileReturnsEmptyResult(t *testing.T) {
	workspaceRoot := t.TempDir()

	segments, err := LoadWorkspacePrompts(context.Background(), workspaceRoot)
	if err != nil {
		t.Fatalf("LoadWorkspacePrompts() error = %v", err)
	}
	if len(segments) != 0 {
		t.Fatalf("len(segments) = %d, want 0", len(segments))
	}
}

func TestLoadWorkspacePromptsEmptyWorkspaceRootUsesCurrentWorkingDirectory(t *testing.T) {
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	workspaceRoot := t.TempDir()
	writeWorkspaceFile(t, workspaceRoot, workspacePromptFileName, "Use the working directory agents file.")
	if err := os.Chdir(workspaceRoot); err != nil {
		t.Fatalf("Chdir(%q) error = %v", workspaceRoot, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalWD); err != nil {
			t.Fatalf("restore working directory error = %v", err)
		}
	})

	segments, err := LoadWorkspacePrompts(context.Background(), "  ")
	if err != nil {
		t.Fatalf("LoadWorkspacePrompts() error = %v", err)
	}
	if len(segments) != 1 {
		t.Fatalf("len(segments) = %d, want 1", len(segments))
	}
	if segments[0].Content != "Use the working directory agents file." {
		t.Fatalf("segment content = %q, want %q", segments[0].Content, "Use the working directory agents file.")
	}
	if segments[0].SourceRef != workspacePromptFileName {
		t.Fatalf("segment source ref = %q, want %q", segments[0].SourceRef, workspacePromptFileName)
	}
	if !segments[0].RuntimeOnly {
		t.Fatal("segment RuntimeOnly = false, want true")
	}
	if segments[0].Phase != workspacePromptPhaseSession {
		t.Fatalf("segment phase = %q, want %q", segments[0].Phase, workspacePromptPhaseSession)
	}
	if segments[0].SourceKind != workspacePromptSourceKindWorkspaceFile {
		t.Fatalf("segment source kind = %q, want %q", segments[0].SourceKind, workspacePromptSourceKindWorkspaceFile)
	}
}

func TestLoadWorkspacePromptsWhitespaceOnlyFileReturnsEmptyResult(t *testing.T) {
	workspaceRoot := t.TempDir()
	writeWorkspaceFile(t, workspaceRoot, workspacePromptFileName, "  \n\t  \n")

	segments, err := LoadWorkspacePrompts(context.Background(), workspaceRoot)
	if err != nil {
		t.Fatalf("LoadWorkspacePrompts() error = %v", err)
	}
	if len(segments) != 0 {
		t.Fatalf("len(segments) = %d, want 0", len(segments))
	}
}

func TestLoadWorkspacePromptsOversizeFileReturnsEmptyResult(t *testing.T) {
	workspaceRoot := t.TempDir()
	writeWorkspaceFile(t, workspaceRoot, workspacePromptFileName, strings.Repeat("a", workspacePromptMaxBytes+1))

	segments, err := LoadWorkspacePrompts(context.Background(), workspaceRoot)
	if err != nil {
		t.Fatalf("LoadWorkspacePrompts() error = %v", err)
	}
	if len(segments) != 0 {
		t.Fatalf("len(segments) = %d, want 0", len(segments))
	}
}

func TestLoadWorkspacePromptsDirectoryReturnsEmptyResult(t *testing.T) {
	workspaceRoot := t.TempDir()
	path := filepath.Join(workspaceRoot, workspacePromptFileName)
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("Mkdir(%q) error = %v", path, err)
	}

	segments, err := LoadWorkspacePrompts(context.Background(), workspaceRoot)
	if err != nil {
		t.Fatalf("LoadWorkspacePrompts() error = %v", err)
	}
	if len(segments) != 0 {
		t.Fatalf("len(segments) = %d, want 0", len(segments))
	}
}

func TestLoadWorkspacePromptsSymlinkReturnsEmptyResult(t *testing.T) {
	workspaceRoot := t.TempDir()
	targetRoot := t.TempDir()
	targetPath := filepath.Join(targetRoot, "outside-agents.md")
	if err := os.WriteFile(targetPath, []byte("Outside workspace prompt"), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", targetPath, err)
	}
	linkPath := filepath.Join(workspaceRoot, workspacePromptFileName)
	if err := os.Symlink(targetPath, linkPath); err != nil {
		t.Skipf("symlink unsupported in this environment: %v", err)
	}

	segments, err := LoadWorkspacePrompts(context.Background(), workspaceRoot)
	if err != nil {
		t.Fatalf("LoadWorkspacePrompts() error = %v", err)
	}
	if len(segments) != 0 {
		t.Fatalf("len(segments) = %d, want 0", len(segments))
	}
}

func TestLoadWorkspacePromptsReturnsSessionWorkspaceMetadata(t *testing.T) {
	workspaceRoot := t.TempDir()
	writeWorkspaceFile(t, workspaceRoot, workspacePromptFileName, "Use concise answers.")

	segments, err := LoadWorkspacePrompts(context.Background(), workspaceRoot)
	if err != nil {
		t.Fatalf("LoadWorkspacePrompts() error = %v", err)
	}
	if len(segments) != 1 {
		t.Fatalf("len(segments) = %d, want 1", len(segments))
	}
	if segments[0].Phase != workspacePromptPhaseSession {
		t.Fatalf("segment phase = %q, want %q", segments[0].Phase, workspacePromptPhaseSession)
	}
	if segments[0].SourceKind != workspacePromptSourceKindWorkspaceFile {
		t.Fatalf("segment source kind = %q, want %q", segments[0].SourceKind, workspacePromptSourceKindWorkspaceFile)
	}
	if segments[0].SourceRef != workspacePromptFileName {
		t.Fatalf("segment source ref = %q, want %q", segments[0].SourceRef, workspacePromptFileName)
	}
	if !segments[0].RuntimeOnly {
		t.Fatal("segment RuntimeOnly = false, want true")
	}
}

func writeWorkspaceFile(t *testing.T, workspaceRoot string, name string, content string) {
	t.Helper()

	path := filepath.Join(workspaceRoot, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}
