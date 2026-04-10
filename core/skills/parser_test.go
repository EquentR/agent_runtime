package skills

import (
	"errors"
	"testing"
)

func TestParseSkillDocumentParsesFrontmatterMetadata(t *testing.T) {
	skill, err := parseSkillDocument("debugging", "skills/debugging/SKILL.md", `---
name: debugging
description: Systematic debugging skill
tools:
  - grep
  - read
tags:
  - debugging
  - analysis
version: v1
hidden: true
---

# Debugging

Follow a structured debugging workflow.
`)
	if err != nil {
		t.Fatalf("parseSkillDocument() error = %v", err)
	}
	if skill.Name != "debugging" {
		t.Fatalf("skill.Name = %q, want %q", skill.Name, "debugging")
	}
	if skill.Description != "Systematic debugging skill" {
		t.Fatalf("skill.Description = %q, want %q", skill.Description, "Systematic debugging skill")
	}
	if len(skill.Tools) != 2 || skill.Tools[0] != "grep" || skill.Tools[1] != "read" {
		t.Fatalf("skill.Tools = %#v, want [grep read]", skill.Tools)
	}
	if len(skill.Tags) != 2 || skill.Tags[0] != "debugging" || skill.Tags[1] != "analysis" {
		t.Fatalf("skill.Tags = %#v, want [debugging analysis]", skill.Tags)
	}
	if skill.Version != "v1" {
		t.Fatalf("skill.Version = %q, want %q", skill.Version, "v1")
	}
	if !skill.Hidden {
		t.Fatal("skill.Hidden = false, want true")
	}
	if skill.SourceRef != "skills/debugging/SKILL.md" {
		t.Fatalf("skill.SourceRef = %q, want %q", skill.SourceRef, "skills/debugging/SKILL.md")
	}
	if skill.Directory != "" {
		t.Fatalf("skill.Directory = %q, want empty", skill.Directory)
	}
	if skill.Content != "# Debugging\n\nFollow a structured debugging workflow.\n" {
		t.Fatalf("skill.Content = %q, want markdown body without frontmatter", skill.Content)
	}
}

func TestParseSkillDocumentLeavesDescriptionEmptyWithoutFrontmatterDescription(t *testing.T) {
	skill, err := parseSkillDocument("debugging", "skills/debugging/SKILL.md", `# Debugging

Follow a structured debugging workflow.

Use real data first.
`)
	if err != nil {
		t.Fatalf("parseSkillDocument() error = %v", err)
	}
	if skill.Name != "debugging" {
		t.Fatalf("skill.Name = %q, want %q", skill.Name, "debugging")
	}
	if skill.Description != "" {
		t.Fatalf("skill.Description = %q, want empty", skill.Description)
	}
	if skill.Hidden {
		t.Fatal("skill.Hidden = true, want false")
	}
	if len(skill.Tools) != 0 {
		t.Fatalf("skill.Tools = %#v, want empty", skill.Tools)
	}
	if len(skill.Tags) != 0 {
		t.Fatalf("skill.Tags = %#v, want empty", skill.Tags)
	}
}

func TestParseSkillDocumentIgnoresBodyTitleAndParagraphForSummary(t *testing.T) {
	skill, err := parseSkillDocument("debugging", "skills/debugging/SKILL.md", `---
name: debugging
description: debug guide
---

# Debugging

body text
`)
	if err != nil {
		t.Fatalf("parseSkillDocument() error = %v", err)
	}
	if skill.Name != "debugging" {
		t.Fatalf("skill.Name = %q, want %q", skill.Name, "debugging")
	}
	if skill.Description != "debug guide" {
		t.Fatalf("skill.Description = %q, want %q", skill.Description, "debug guide")
	}
}

func TestParseSkillDocumentRejectsFrontmatterOnlyDocumentClosedAtEOF(t *testing.T) {
	_, err := parseSkillDocument("debugging", "skills/debugging/SKILL.md", "---\nname: debugging\ndescription: Systematic debugging skill\n---")
	if !errors.Is(err, ErrInvalidSkillDocument) {
		t.Fatalf("parseSkillDocument() error = %v, want ErrInvalidSkillDocument", err)
	}
}

func TestParseSkillDocumentRejectsFrontmatterNameMismatch(t *testing.T) {
	_, err := parseSkillDocument("debugging", "skills/debugging/SKILL.md", `---
name: review
---

# Debugging

Follow a structured debugging workflow.
`)
	if !errors.Is(err, ErrInvalidSkillDocument) {
		t.Fatalf("parseSkillDocument() error = %v, want ErrInvalidSkillDocument", err)
	}
}

func TestParseSkillDocumentRejectsInvalidFrontmatterYAML(t *testing.T) {
	_, err := parseSkillDocument("debugging", "skills/debugging/SKILL.md", `---
name: [debugging
---

# Debugging

Follow a structured debugging workflow.
`)
	if !errors.Is(err, ErrInvalidSkillDocument) {
		t.Fatalf("parseSkillDocument() error = %v, want ErrInvalidSkillDocument", err)
	}
}

func TestParseSkillDocumentRejectsWhitespaceOnlyBody(t *testing.T) {
	_, err := parseSkillDocument("debugging", "skills/debugging/SKILL.md", "   \n\t  \n")
	if !errors.Is(err, ErrInvalidSkillDocument) {
		t.Fatalf("parseSkillDocument() error = %v, want ErrInvalidSkillDocument", err)
	}
}

func TestParseSkillDocumentRejectsEmptyDirectoryName(t *testing.T) {
	_, err := parseSkillDocument("   ", "skills/debugging/SKILL.md", "# Debugging\n\nFollow a structured debugging workflow.\n")
	if !errors.Is(err, ErrInvalidSkillDocument) {
		t.Fatalf("parseSkillDocument() error = %v, want ErrInvalidSkillDocument", err)
	}
}

func TestParseSkillDocumentRejectsEmptySourceRef(t *testing.T) {
	_, err := parseSkillDocument("debugging", "   ", "# Debugging\n\nFollow a structured debugging workflow.\n")
	if !errors.Is(err, ErrInvalidSkillDocument) {
		t.Fatalf("parseSkillDocument() error = %v, want ErrInvalidSkillDocument", err)
	}
}

func TestParseSkillDocumentNormalizesTagsAndTools(t *testing.T) {
	skill, err := parseSkillDocument("debugging", "skills/debugging/SKILL.md", `---
name: debugging
tools:
  - grep
  - ''
  - read
  - grep
tags:
  - debugging
  - ''
  - analysis
  - debugging
---

# Debugging

Follow a structured debugging workflow.
`)
	if err != nil {
		t.Fatalf("parseSkillDocument() error = %v", err)
	}
	if len(skill.Tools) != 2 || skill.Tools[0] != "grep" || skill.Tools[1] != "read" {
		t.Fatalf("skill.Tools = %#v, want [grep read]", skill.Tools)
	}
	if len(skill.Tags) != 2 || skill.Tags[0] != "debugging" || skill.Tags[1] != "analysis" {
		t.Fatalf("skill.Tags = %#v, want [debugging analysis]", skill.Tags)
	}
}
