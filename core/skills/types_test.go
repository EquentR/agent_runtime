package skills

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestSkillJSONShapeIsStable(t *testing.T) {
	skill := Skill{
		Name:         "debugging",
		Title:        "Debugging",
		Description:  "Systematic debugging skill",
		Tags:         []string{"debugging", "analysis"},
		Tools:        []string{"grep", "read"},
		Version:      "v1",
		Hidden:       true,
		SourceRef:    "skills/debugging/SKILL.md",
		Directory:    "E:/workspace/skills/debugging",
		Content:      "# Debugging\nUse a structured debugging flow.",
		ResourceRefs: []string{"skills/debugging/examples.md"},
	}

	payload, err := json.Marshal(skill)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	got := string(payload)
	for _, want := range []string{
		`"name":"debugging"`,
		`"title":"Debugging"`,
		`"description":"Systematic debugging skill"`,
		`"tags":["debugging","analysis"]`,
		`"tools":["grep","read"]`,
		`"version":"v1"`,
		`"hidden":true`,
		`"source_ref":"skills/debugging/SKILL.md"`,
		`"content":"# Debugging\nUse a structured debugging flow."`,
		`"resource_refs":["skills/debugging/examples.md"]`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("marshaled json = %s, want substring %s", got, want)
		}
	}
	if strings.Contains(got, `"Directory"`) || strings.Contains(got, `"directory"`) {
		t.Fatalf("marshaled json = %s, want no directory field", got)
	}
}

func TestSkillListItemJSONShapeIsStable(t *testing.T) {
	skill := SkillListItem{
		Name:        "debugging",
		Title:       "Debugging",
		Description: "Systematic debugging skill",
		Tags:        []string{"debugging", "analysis"},
		Tools:       []string{"grep", "read"},
		Version:     "v1",
		Hidden:      true,
		SourceRef:   "skills/debugging/SKILL.md",
	}

	payload, err := json.Marshal(skill)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	got := string(payload)
	for _, want := range []string{
		`"name":"debugging"`,
		`"title":"Debugging"`,
		`"description":"Systematic debugging skill"`,
		`"tags":["debugging","analysis"]`,
		`"tools":["grep","read"]`,
		`"version":"v1"`,
		`"hidden":true`,
		`"source_ref":"skills/debugging/SKILL.md"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("marshaled json = %s, want substring %s", got, want)
		}
	}
	if strings.Contains(got, `"content"`) {
		t.Fatalf("marshaled json = %s, want no content field", got)
	}
}

func TestSkillErrorsSupportErrorsIs(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want error
	}{
		{name: "not found", err: fmt.Errorf("resolve skill %q: %w", "debugging", ErrSkillNotFound), want: ErrSkillNotFound},
		{name: "invalid name", err: fmt.Errorf("resolve skill %q: %w", "../bad", ErrInvalidSkillName), want: ErrInvalidSkillName},
		{name: "invalid document", err: fmt.Errorf("parse skill %q: %w", "review", ErrInvalidSkillDocument), want: ErrInvalidSkillDocument},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !errors.Is(tc.err, tc.want) {
				t.Fatalf("errors.Is(%v, %v) = false, want true", tc.err, tc.want)
			}
		})
	}
}
