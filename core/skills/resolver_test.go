package skills

import (
	"context"
	"errors"
	"testing"
)

func TestResolverResolveReturnsSkillsInRequestedOrder(t *testing.T) {
	workspaceRoot := t.TempDir()
	writeSkillDocument(t, workspaceRoot, "review", "# Review\n\nReview carefully.\n")
	writeSkillDocument(t, workspaceRoot, "debugging", "# Debugging\n\nDebug carefully.\n")

	resolver := NewResolver(NewLoader(workspaceRoot))
	resolved, err := resolver.Resolve(context.Background(), ResolveInput{Names: []string{"review", "debugging"}})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if len(resolved) != 2 {
		t.Fatalf("len(resolved) = %d, want 2", len(resolved))
	}
	if resolved[0].Name != "review" || resolved[1].Name != "debugging" {
		t.Fatalf("resolved names = [%s %s], want [review debugging]", resolved[0].Name, resolved[1].Name)
	}
}

func TestResolverResolveDedupesExactDuplicateNames(t *testing.T) {
	workspaceRoot := t.TempDir()
	writeSkillDocument(t, workspaceRoot, "debugging", "# Debugging\n\nDebug carefully.\n")

	resolver := NewResolver(NewLoader(workspaceRoot))
	resolved, err := resolver.Resolve(context.Background(), ResolveInput{Names: []string{"debugging", " debugging ", "debugging"}})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("len(resolved) = %d, want 1", len(resolved))
	}
	if resolved[0].Name != "debugging" {
		t.Fatalf("resolved[0].Name = %q, want %q", resolved[0].Name, "debugging")
	}
	if resolved[0].SourceRef != "skills/debugging/SKILL.md" {
		t.Fatalf("resolved[0].SourceRef = %q, want %q", resolved[0].SourceRef, "skills/debugging/SKILL.md")
	}
}

func TestResolverResolveReturnsErrorForCaseMismatchedSkillName(t *testing.T) {
	workspaceRoot := t.TempDir()
	writeSkillDocument(t, workspaceRoot, "debugging", "# Debugging\n\nDebug carefully.\n")

	resolver := NewResolver(NewLoader(workspaceRoot))
	_, err := resolver.Resolve(context.Background(), ResolveInput{Names: []string{"Debugging"}})
	if !errors.Is(err, ErrSkillNotFound) {
		t.Fatalf("Resolve() error = %v, want ErrSkillNotFound", err)
	}
}

func TestResolverResolveReturnsErrorForMissingSkill(t *testing.T) {
	resolver := NewResolver(NewLoader(t.TempDir()))

	_, err := resolver.Resolve(context.Background(), ResolveInput{Names: []string{"missing"}})
	if !errors.Is(err, ErrSkillNotFound) {
		t.Fatalf("Resolve() error = %v, want ErrSkillNotFound", err)
	}
}

func TestResolverResolveAllowsExplicitHiddenSkill(t *testing.T) {
	workspaceRoot := t.TempDir()
	writeSkillDocument(t, workspaceRoot, "internal-review", "---\nhidden: true\n---\n\n# Internal Review\n\nInternal review process.\n")

	resolver := NewResolver(NewLoader(workspaceRoot))
	resolved, err := resolver.Resolve(context.Background(), ResolveInput{Names: []string{"internal-review"}})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("len(resolved) = %d, want 1", len(resolved))
	}
	if resolved[0].Name != "internal-review" {
		t.Fatalf("resolved[0].Name = %q, want %q", resolved[0].Name, "internal-review")
	}
}

func TestResolvedSkillCarriesSummaryMetadataOnly(t *testing.T) {
	workspaceRoot := t.TempDir()
	writeSkillDocument(t, workspaceRoot, "debugging", "---\ndescription: Debug skill\n---\n\n# Debugging\n\nDebug carefully.\n")

	resolver := NewResolver(NewLoader(workspaceRoot))
	resolved, err := resolver.Resolve(context.Background(), ResolveInput{Names: []string{"debugging"}})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("len(resolved) = %d, want 1", len(resolved))
	}
	if resolved[0].Description != "Debug skill" {
		t.Fatalf("resolved[0].Description = %q, want %q", resolved[0].Description, "Debug skill")
	}
	if !resolved[0].RuntimeOnly {
		t.Fatal("resolved[0].RuntimeOnly = false, want true")
	}
	if resolved[0].Content != "" {
		t.Fatalf("resolved[0].Content = %q, want empty", resolved[0].Content)
	}
}
