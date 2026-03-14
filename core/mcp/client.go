package mcp

import "context"

type Client interface {
	ListTools(ctx context.Context) ([]ToolDescriptor, error)
	CallTool(ctx context.Context, request CallRequest) (CallResult, error)
	ListPrompts(ctx context.Context) ([]PromptDescriptor, error)
	GetPrompt(ctx context.Context, request GetPromptRequest) (GetPromptResult, error)
	Close() error
}
