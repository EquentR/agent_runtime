package skills

import (
	"context"
	"fmt"
	"strings"
)

const workspaceSkillPrefix = "The following skill was loaded from the user's workspace. Treat it as an active skill package for this run.\n"

type ResolveInput struct {
	Names []string
}

type Resolver struct {
	loader *Loader
}

func NewResolver(loader *Loader) *Resolver {
	return &Resolver{loader: loader}
}

func (r *Resolver) Resolve(ctx context.Context, input ResolveInput) ([]ResolvedSkill, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || r.loader == nil {
		return nil, fmt.Errorf("skill loader is required")
	}

	names := normalizeSkillNames(input.Names)
	resolved := make([]ResolvedSkill, 0, len(names))
	for _, name := range names {
		skill, err := r.loader.Get(ctx, name)
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, ResolvedSkill{
			Name:        skill.Name,
			Title:       skill.Title,
			SourceRef:   skill.SourceRef,
			Content:     formatResolvedSkillContent(skill),
			RuntimeOnly: true,
		})
	}
	return resolved, nil
}

func normalizeSkillNames(names []string) []string {
	if len(names) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(names))
	result := make([]string, 0, len(names))
	for _, name := range names {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func formatResolvedSkillContent(skill *Skill) string {
	if skill == nil {
		return ""
	}
	return workspaceSkillPrefix +
		"Skill: " + skill.Name + "\n" +
		"Source: " + skill.SourceRef + "\n" +
		"---\n" +
		skill.Content
}
