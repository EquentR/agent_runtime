package skills

import (
	"strings"

	model "github.com/EquentR/agent_runtime/core/providers/types"
)

const selectedSkillsSummaryPrefix = "Selected workspace skills are available as optional guides for this request.\nUse using_skills(name) only when a selected skill is actually relevant."

func BuildSelectedSkillSummaryMessage(skills []ResolvedSkill) *model.Message {
	if len(skills) == 0 {
		return nil
	}
	lines := make([]string, 0, len(skills)+2)
	lines = append(lines, selectedSkillsSummaryPrefix, "")
	for _, skill := range skills {
		line := "- " + skill.Name + ":"
		if description := strings.TrimSpace(skill.Description); description != "" {
			line += " " + description
		}
		lines = append(lines, line)
	}
	return &model.Message{Role: model.RoleUser, Content: strings.Join(lines, "\n")}
}
