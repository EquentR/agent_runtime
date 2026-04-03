package skills

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoaderListReturnsEmptyWhenSkillsDirectoryMissing(t *testing.T) {
	loader := NewLoader(t.TempDir())

	items, err := loader.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("len(items) = %d, want 0", len(items))
	}
}

func TestLoaderListUsesCurrentWorkingDirectoryWhenWorkspaceRootEmpty(t *testing.T) {
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	workspaceRoot := t.TempDir()
	if err := os.Chdir(workspaceRoot); err != nil {
		t.Fatalf("Chdir(%q) error = %v", workspaceRoot, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalWD); err != nil {
			t.Fatalf("restore working directory error = %v", err)
		}
	})
	writeSkillDocument(t, workspaceRoot, "debugging", "# Debugging\nDebug carefully.")

	loader := NewLoader("   ")
	items, err := loader.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].Name != "debugging" {
		t.Fatalf("items[0].Name = %q, want %q", items[0].Name, "debugging")
	}
}

func TestLoaderListLoadsWorkspaceSkillsSortedByName(t *testing.T) {
	workspaceRoot := t.TempDir()
	writeSkillDocument(t, workspaceRoot, "review", "# Review\nReview carefully.")
	writeSkillDocument(t, workspaceRoot, "debugging", "# Debugging\nDebug carefully.")

	loader := NewLoader(workspaceRoot)
	items, err := loader.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	if items[0].Name != "debugging" || items[1].Name != "review" {
		t.Fatalf("items names = [%s %s], want [debugging review]", items[0].Name, items[1].Name)
	}
	if items[0].SourceRef != "skills/debugging/SKILL.md" {
		t.Fatalf("items[0].SourceRef = %q, want %q", items[0].SourceRef, "skills/debugging/SKILL.md")
	}
}

func TestLoaderListIgnoresDirectoriesWithoutSkillDocument(t *testing.T) {
	workspaceRoot := t.TempDir()
	writeSkillDocument(t, workspaceRoot, "debugging", "# Debugging\nDebug carefully.")
	missingDocDir := filepath.Join(workspaceRoot, "skills", "notes")
	if err := os.MkdirAll(missingDocDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", missingDocDir, err)
	}

	loader := NewLoader(workspaceRoot)
	items, err := loader.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].Name != "debugging" {
		t.Fatalf("items[0].Name = %q, want %q", items[0].Name, "debugging")
	}
}

func TestLoaderGetLoadsWorkspaceSkillDocument(t *testing.T) {
	workspaceRoot := t.TempDir()
	writeSkillDocument(t, workspaceRoot, "debugging", "# Debugging\nDebug carefully.")

	loader := NewLoader(workspaceRoot)
	skill, err := loader.Get(context.Background(), "debugging")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if skill.Name != "debugging" {
		t.Fatalf("skill.Name = %q, want %q", skill.Name, "debugging")
	}
	if skill.Title != "Debugging" {
		t.Fatalf("skill.Title = %q, want %q", skill.Title, "Debugging")
	}
	if skill.Description != "Debug carefully." {
		t.Fatalf("skill.Description = %q, want %q", skill.Description, "Debug carefully.")
	}
	if skill.SourceRef != "skills/debugging/SKILL.md" {
		t.Fatalf("skill.SourceRef = %q, want %q", skill.SourceRef, "skills/debugging/SKILL.md")
	}
	if skill.Directory != filepath.Join(workspaceRoot, "skills", "debugging") {
		t.Fatalf("skill.Directory = %q, want %q", skill.Directory, filepath.Join(workspaceRoot, "skills", "debugging"))
	}
	if skill.Content != "# Debugging\nDebug carefully." {
		t.Fatalf("skill.Content = %q, want %q", skill.Content, "# Debugging\nDebug carefully.")
	}
}

func TestLoaderGetRejectsOversizeSkillDocument(t *testing.T) {
	workspaceRoot := t.TempDir()
	writeSkillDocument(t, workspaceRoot, "debugging", strings.Repeat("a", skillDocumentMaxBytes+1))

	loader := NewLoader(workspaceRoot)
	_, err := loader.Get(context.Background(), "debugging")
	if !errors.Is(err, ErrInvalidSkillDocument) {
		t.Fatalf("Get() error = %v, want ErrInvalidSkillDocument", err)
	}
}

func TestLoaderGetRejectsInvalidSkillName(t *testing.T) {
	loader := NewLoader(t.TempDir())

	for _, name := range []string{"", ".", "..", "../bad", `bad\\name`, "nested/path"} {
		t.Run(name, func(t *testing.T) {
			_, err := loader.Get(context.Background(), name)
			if !errors.Is(err, ErrInvalidSkillName) {
				t.Fatalf("Get(%q) error = %v, want ErrInvalidSkillName", name, err)
			}
		})
	}
}

func TestLoaderListIgnoresSymlinkSkillDirectory(t *testing.T) {
	workspaceRoot := t.TempDir()
	targetRoot := t.TempDir()
	writeSkillDocument(t, targetRoot, "outside", "# Outside\nShould be ignored.")
	skillsRoot := filepath.Join(workspaceRoot, "skills")
	if err := os.MkdirAll(skillsRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", skillsRoot, err)
	}
	if err := os.Symlink(filepath.Join(targetRoot, "skills", "outside"), filepath.Join(skillsRoot, "outside")); err != nil {
		t.Skipf("symlink unsupported in this environment: %v", err)
	}

	loader := NewLoader(workspaceRoot)
	items, err := loader.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("len(items) = %d, want 0", len(items))
	}
}

func TestLoaderListIgnoresSymlinkSkillDocument(t *testing.T) {
	workspaceRoot := t.TempDir()
	skillsRoot := filepath.Join(workspaceRoot, "skills")
	skillDir := filepath.Join(skillsRoot, "debugging")
	target := filepath.Join(workspaceRoot, "outside.md")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", skillDir, err)
	}
	if err := os.WriteFile(target, []byte("# Outside\nIgnored."), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", target, err)
	}
	if err := os.Symlink(target, filepath.Join(skillDir, "SKILL.md")); err != nil {
		t.Skipf("symlink unsupported in this environment: %v", err)
	}

	loader := NewLoader(workspaceRoot)
	items, err := loader.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("len(items) = %d, want 0", len(items))
	}
}

func writeSkillDocument(t *testing.T, workspaceRoot, name, content string) {
	t.Helper()

	dir := filepath.Join(workspaceRoot, "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", dir, err)
	}
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}
