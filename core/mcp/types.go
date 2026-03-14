package mcp

import coretypes "github.com/EquentR/agent_runtime/core/types"

type Schema = coretypes.JSONSchema

type SchemaProperty = coretypes.SchemaProperty

type ToolDescriptor struct {
	Name        string
	Description string
	InputSchema Schema
}

type CallRequest struct {
	Name      string
	Arguments map[string]any
}

type CallResult struct {
	Text string
	Raw  any
}

type PromptDescriptor struct {
	Name        string
	Description string
	Arguments   []PromptArgument
}

type PromptArgument struct {
	Name        string
	Description string
	Required    bool
}

type GetPromptRequest struct {
	Name      string
	Arguments map[string]string
}

type PromptMessage struct {
	Role    string
	Content string
}

type GetPromptResult struct {
	Description string
	Messages    []PromptMessage
	Text        string
	Raw         any
}
