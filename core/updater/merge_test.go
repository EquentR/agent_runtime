package updater

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApplyManagedFilesUpdatesUnmodifiedAndPreservesModifiedFiles(t *testing.T) {
	installed := t.TempDir()
	staged := t.TempDir()
	writeUpdaterTestFile(t, installed, "workspace/AGENTS.md", "old official")
	writeUpdaterTestFile(t, installed, "workspace/skills/custom/SKILL.md", "user modified")
	writeUpdaterTestFile(t, staged, "workspace/AGENTS.md", "new official")
	writeUpdaterTestFile(t, staged, "workspace/skills/custom/SKILL.md", "new custom")
	writeUpdaterTestFile(t, staged, "workspace/skills/new/SKILL.md", "new skill")
	previous, err := BuildManagedManifest("v1.0.0", installed, []string{"workspace/AGENTS.md", "workspace/skills/custom/SKILL.md"})
	if err != nil {
		t.Fatalf("BuildManagedManifest(previous) error = %v", err)
	}
	previous.Files["workspace/skills/custom/SKILL.md"] = string(make([]byte, 64))
	next, err := BuildManagedManifest("v1.1.0", staged, []string{"workspace/AGENTS.md", "workspace/skills/custom/SKILL.md", "workspace/skills/new/SKILL.md"})
	if err != nil {
		t.Fatalf("BuildManagedManifest(next) error = %v", err)
	}
	result, err := ApplyManagedFiles(installed, staged, previous, next)
	if err != nil {
		t.Fatalf("ApplyManagedFiles() error = %v", err)
	}
	assertUpdaterTestFile(t, installed, "workspace/AGENTS.md", "new official")
	assertUpdaterTestFile(t, installed, "workspace/skills/custom/SKILL.md", "user modified")
	assertUpdaterTestFile(t, installed, "workspace/skills/custom/SKILL.md.new", "new custom")
	assertUpdaterTestFile(t, installed, "workspace/skills/new/SKILL.md", "new skill")
	if len(result.Updated) != 1 || len(result.Conflicts) != 1 || len(result.Added) != 1 {
		t.Fatalf("MergeResult = %#v", result)
	}
}

func TestApplyManagedFilesFirstUpgradePreservesExistingAndRemovesOnlyUnmodifiedDeletedFiles(t *testing.T) {
	installed := t.TempDir()
	staged := t.TempDir()
	writeUpdaterTestFile(t, installed, "workspace/AGENTS.md", "user rules")
	writeUpdaterTestFile(t, staged, "workspace/AGENTS.md", "official rules")
	next, _ := BuildManagedManifest("v1.1.0", staged, []string{"workspace/AGENTS.md"})
	result, err := ApplyManagedFiles(installed, staged, ManagedManifest{}, next)
	if err != nil {
		t.Fatalf("ApplyManagedFiles(first) error = %v", err)
	}
	assertUpdaterTestFile(t, installed, "workspace/AGENTS.md", "user rules")
	assertUpdaterTestFile(t, installed, "workspace/AGENTS.md.new", "official rules")
	if len(result.Conflicts) != 1 {
		t.Fatalf("first MergeResult = %#v", result)
	}

	removedPath := "workspace/skills/removed/SKILL.md"
	writeUpdaterTestFile(t, installed, removedPath, "old official")
	previous, _ := BuildManagedManifest("v1.0.0", installed, []string{removedPath})
	result, err = ApplyManagedFiles(installed, staged, previous, ManagedManifest{Version: "v1.1.0", Files: map[string]string{}})
	if err != nil {
		t.Fatalf("ApplyManagedFiles(remove) error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(installed, filepath.FromSlash(removedPath))); !os.IsNotExist(err) {
		t.Fatalf("removed file still exists: %v", err)
	}
	if len(result.Removed) != 1 {
		t.Fatalf("remove MergeResult = %#v", result)
	}
}

func writeUpdaterTestFile(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func assertUpdaterTestFile(t *testing.T, root, relative, want string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil || string(raw) != want {
		t.Fatalf("file %s = %q, %v, want %q", relative, raw, err, want)
	}
}
