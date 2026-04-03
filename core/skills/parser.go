package skills

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

type skillFrontmatter struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Tools       []string `yaml:"tools"`
	Tags        []string `yaml:"tags"`
	Version     string   `yaml:"version"`
	Hidden      bool     `yaml:"hidden"`
}

func parseSkillDocument(directoryName string, sourceRef string, content string) (*Skill, error) {
	directoryName = strings.TrimSpace(directoryName)
	sourceRef = strings.TrimSpace(sourceRef)
	if directoryName == "" || sourceRef == "" || strings.TrimSpace(content) == "" {
		return nil, fmt.Errorf("parse skill %q: %w", directoryName, ErrInvalidSkillDocument)
	}

	frontmatter, body, err := splitSkillFrontmatter(content)
	if err != nil {
		return nil, fmt.Errorf("parse skill %q: %w", directoryName, ErrInvalidSkillDocument)
	}
	if strings.TrimSpace(body) == "" {
		return nil, fmt.Errorf("parse skill %q: %w", directoryName, ErrInvalidSkillDocument)
	}

	meta := skillFrontmatter{}
	if strings.TrimSpace(frontmatter) != "" {
		if err := yaml.Unmarshal([]byte(frontmatter), &meta); err != nil {
			return nil, fmt.Errorf("parse skill %q: %w", directoryName, ErrInvalidSkillDocument)
		}
		if name := strings.TrimSpace(meta.Name); name != "" && name != directoryName {
			return nil, fmt.Errorf("parse skill %q: %w", directoryName, ErrInvalidSkillDocument)
		}
	}

	title := extractSkillTitle(directoryName, body)
	description := strings.TrimSpace(meta.Description)
	if description == "" {
		description = extractFirstParagraph(body)
	}

	return &Skill{
		Name:         directoryName,
		Title:        title,
		Description:  description,
		Tags:         normalizeSkillStringList(meta.Tags),
		Tools:        normalizeSkillStringList(meta.Tools),
		Version:      strings.TrimSpace(meta.Version),
		Hidden:       meta.Hidden,
		SourceRef:    sourceRef,
		Content:      body,
		ResourceRefs: []string{},
	}, nil
}

func splitSkillFrontmatter(content string) (frontmatter string, body string, err error) {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return "", normalized, nil
	}
	rest := normalized[len("---\n"):]
	if strings.HasSuffix(rest, "\n---") {
		return rest[:len(rest)-len("\n---")], "", nil
	}
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		return "", "", fmt.Errorf("frontmatter closing delimiter not found")
	}
	frontmatter = rest[:end]
	body = rest[end+len("\n---\n"):]
	if strings.HasPrefix(body, "\n") {
		body = body[1:]
	}
	return frontmatter, body, nil
}

func extractSkillTitle(directoryName string, body string) string {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			title := strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
			if title != "" {
				return title
			}
			break
		}
	}
	return directoryName
}

func extractFirstParagraph(body string) string {
	paragraph := make([]string, 0)
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if len(paragraph) > 0 {
				break
			}
			continue
		}
		if strings.HasPrefix(trimmed, "# ") && len(paragraph) == 0 {
			continue
		}
		paragraph = append(paragraph, trimmed)
	}
	return strings.Join(paragraph, " ")
}

func normalizeSkillStringList(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
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
