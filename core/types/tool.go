package types

type Tool struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Parameters  JSONSchema `json:"parameters"`
}

type JSONSchema struct {
	Type       string                    `json:"type"`
	Properties map[string]SchemaProperty `json:"properties"`
	Required   []string                  `json:"required"`
}

type SchemaProperty struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

type ToolCall struct {
	ID        string `json:"id,omitempty"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // 保持 string，避免提前反序列化
	// ThoughtSignature 用于 Gemini tool calling 轮次续写，需原样回传。
	ThoughtSignature []byte `json:"thought_signature,omitempty"`
}

type ToolChoiceType string

const (
	ToolAuto  ToolChoiceType = "auto"
	ToolNone  ToolChoiceType = "none"
	ToolForce ToolChoiceType = "force"
)

type ToolChoice struct {
	Type ToolChoiceType `json:"type"`
	Name string         `json:"name,omitempty"`
}
