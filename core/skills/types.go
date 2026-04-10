package skills

// Skill describes a workspace skill document and its resolved metadata.
type Skill struct {
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	Tools        []string `json:"tools,omitempty"`
	Version      string   `json:"version,omitempty"`
	Hidden       bool     `json:"hidden,omitempty"`
	SourceRef    string   `json:"source_ref"`
	Directory    string   `json:"-"`
	Content      string   `json:"content,omitempty"`
	ResourceRefs []string `json:"resource_refs,omitempty"`
}

// SkillListItem describes the readonly list payload for a workspace skill.
type SkillListItem struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Tools       []string `json:"tools,omitempty"`
	Version     string   `json:"version,omitempty"`
	Hidden      bool     `json:"hidden,omitempty"`
	SourceRef   string   `json:"source_ref"`
}

// ResolvedSkill is the runtime-only representation injected into the prompt.
type ResolvedSkill struct {
	Name        string
	Description string
	SourceRef   string
	Content     string
	RuntimeOnly bool
}
