package runtimeprompt

const (
	PhaseSession      = "session"
	PhaseStepPreModel = "step_pre_model"
	PhaseToolResult   = "tool_result"

	SourceTypeForcedBlock    = "forced_block"
	SourceTypeMemorySummary  = "memory_summary"
	SourceTypeResolvedPrompt = "resolved_prompt"

	RoleSystem = "system"
)

type Segment struct {
	SourceType   string `json:"source_type"`
	SourceKey    string `json:"source_key"`
	Phase        string `json:"phase"`
	Order        int    `json:"order"`
	Role         string `json:"role"`
	Content      string `json:"content"`
	RuntimeOnly  bool   `json:"runtime_only,omitempty"`
	AuditVisible bool   `json:"audit_visible,omitempty"`
}

type Envelope struct {
	Segments []Segment `json:"segments"`
}
