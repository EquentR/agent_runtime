package skills

import (
	"testing"

	model "github.com/EquentR/agent_runtime/core/providers/types"
)

func TestBuildSelectedSkillSummaryMessageUsesNamesAndDescriptionsOnly(t *testing.T) {
	message := BuildSelectedSkillSummaryMessage([]ResolvedSkill{{
		Name:        "debugging",
		Description: "systematic debugging guide",
		SourceRef:   "skills/debugging/SKILL.md",
		Content:     "# Debugging\n\nfull body",
		RuntimeOnly: true,
	}, {
		Name:        "review",
		Description: "risk-focused review guide",
		SourceRef:   "skills/review/SKILL.md",
		Content:     "# Review\n\nfull body",
		RuntimeOnly: true,
	}})
	if message == nil {
		t.Fatal("BuildSelectedSkillSummaryMessage() = nil, want summary message")
	}
	if message.Role != model.RoleUser {
		t.Fatalf("message.Role = %q, want %q", message.Role, model.RoleUser)
	}
	want := "Selected workspace skills are available as optional guides for this request.\nUse using_skills(name) only when a selected skill is actually relevant.\n\n- debugging: systematic debugging guide\n- review: risk-focused review guide"
	if message.Content != want {
		t.Fatalf("message.Content = %q, want %q", message.Content, want)
	}
}
