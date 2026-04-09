package builtin

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestUsingSkillsReturnsFullSkillPayloadAsEphemeralResult(t *testing.T) {
	workspace := t.TempDir()
	mustWriteFile(t, filepath.Join(workspace, "skills", "debugging", "SKILL.md"), "---\nname: debugging\ndescription: Debug skill\n---\n\n# Debugging\n\nDebug carefully.\n")
	mustWriteFile(t, filepath.Join(workspace, "skills", "debugging", "checklist.md"), "step 1")
	mustWriteFile(t, filepath.Join(workspace, "skills", "debugging", "examples", "sample.txt"), "example")
	mustWriteFile(t, filepath.Join(workspace, "skills", "debugging", ".hidden.txt"), "secret")

	registry := newBuiltinRegistry(t, Options{WorkspaceRoot: workspace})
	result, err := registry.Execute(context.Background(), "using_skills", map[string]any{"name": "debugging"})
	if err != nil {
		t.Fatalf("Execute(using_skills) error = %v", err)
	}
	if !result.Ephemeral {
		t.Fatal("result.Ephemeral = false, want true")
	}
	var payload struct {
		Name         string   `json:"name"`
		Description  string   `json:"description"`
		SourceRef    string   `json:"source_ref"`
		Directory    string   `json:"directory"`
		ResourceRefs []string `json:"resource_refs"`
		Content      string   `json:"content"`
	}
	if err := json.Unmarshal([]byte(result.Content), &payload); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", result.Content, err)
	}
	if payload.Name != "debugging" || payload.Description != "Debug skill" {
		t.Fatalf("payload = %#v, want debugging summary metadata", payload)
	}
	if payload.SourceRef != "skills/debugging/SKILL.md" {
		t.Fatalf("payload.SourceRef = %q, want skills/debugging/SKILL.md", payload.SourceRef)
	}
	if !strings.HasSuffix(strings.ReplaceAll(payload.Directory, "\\", "/"), "/skills/debugging") {
		t.Fatalf("payload.Directory = %q, want absolute debugging directory", payload.Directory)
	}
	if payload.Content != "# Debugging\n\nDebug carefully.\n" {
		t.Fatalf("payload.Content = %q, want markdown body", payload.Content)
	}
	joined := strings.Join(payload.ResourceRefs, ",")
	if !strings.Contains(joined, "skills/debugging/SKILL.md") || !strings.Contains(joined, "skills/debugging/checklist.md") || !strings.Contains(joined, "skills/debugging/examples/sample.txt") {
		t.Fatalf("payload.ResourceRefs = %#v, want visible skill resources", payload.ResourceRefs)
	}
	if strings.Contains(joined, ".hidden") {
		t.Fatalf("payload.ResourceRefs = %#v, want hidden files excluded", payload.ResourceRefs)
	}
}

func TestUsingSkillsRejectsMissingOrInvalidSkillNames(t *testing.T) {
	registry := newBuiltinRegistry(t, Options{WorkspaceRoot: t.TempDir()})
	for _, tc := range []struct {
		name string
		args map[string]any
		want string
	}{
		{name: "missing", args: map[string]any{"name": "missing"}, want: "not found"},
		{name: "path escape", args: map[string]any{"name": "../debugging"}, want: "invalid skill name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := registry.Execute(context.Background(), "using_skills", tc.args)
			if err == nil {
				t.Fatal("Execute(using_skills) error = nil, want non-nil")
			}
			if !strings.Contains(strings.ToLower(err.Error()), tc.want) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tc.want)
			}
		})
	}
}
