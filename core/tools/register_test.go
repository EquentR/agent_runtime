package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"

	coremcp "github.com/EquentR/agent_runtime/core/mcp"
	"github.com/EquentR/agent_runtime/core/types"
)

type fakeMCPClient struct {
	tools        []coremcp.ToolDescriptor
	prompts      []coremcp.PromptDescriptor
	callName     string
	callArgs     map[string]any
	result       coremcp.CallResult
	promptName   string
	promptArgs   map[string]string
	promptResult coremcp.GetPromptResult
	err          error
}

func (f *fakeMCPClient) ListTools(_ context.Context) ([]coremcp.ToolDescriptor, error) {
	return append([]coremcp.ToolDescriptor(nil), f.tools...), nil
}

func (f *fakeMCPClient) CallTool(_ context.Context, request coremcp.CallRequest) (coremcp.CallResult, error) {
	f.callName = request.Name
	f.callArgs = request.Arguments
	return f.result, f.err
}

func (f *fakeMCPClient) ListPrompts(_ context.Context) ([]coremcp.PromptDescriptor, error) {
	return append([]coremcp.PromptDescriptor(nil), f.prompts...), nil
}

func (f *fakeMCPClient) GetPrompt(_ context.Context, request coremcp.GetPromptRequest) (coremcp.GetPromptResult, error) {
	f.promptName = request.Name
	f.promptArgs = request.Arguments
	return f.promptResult, f.err
}

func (f *fakeMCPClient) Close() error {
	return nil
}

func TestRegistry_IsEmptyByDefault(t *testing.T) {
	registry := NewRegistry()
	if got := registry.List(); len(got) != 0 {
		t.Fatalf("List() length = %d, want 0", len(got))
	}
}

func TestRegistry_RegisterMCPClient(t *testing.T) {
	registry := NewRegistry()
	fakeClient := &fakeMCPClient{
		tools: []coremcp.ToolDescriptor{
			{
				Name:        "search_docs",
				Description: "Search project docs",
				InputSchema: coremcp.Schema{
					Type: "object",
					Properties: map[string]coremcp.SchemaProperty{
						"query": {
							Type:        "string",
							Description: "Search query",
						},
					},
					Required: []string{"query"},
				},
			},
		},
		result: coremcp.CallResult{Text: "matched docs"},
	}

	if err := registry.RegisterMCPClient(fakeClient, MCPRegistrationOptions{Prefix: "docs"}); err != nil {
		t.Fatalf("RegisterMCPClient() error = %v", err)
	}

	tools := registry.List()
	if len(tools) != 1 {
		t.Fatalf("List() length = %d, want 1", len(tools))
	}
	if tools[0].Name != "docs.search_docs" {
		t.Fatalf("tool name = %q, want %q", tools[0].Name, "docs.search_docs")
	}
	if tools[0].Parameters.Type != "object" {
		t.Fatalf("schema type = %q, want %q", tools[0].Parameters.Type, "object")
	}
	if tools[0].Parameters.Properties["query"].Type != "string" {
		t.Fatalf("query type = %q, want %q", tools[0].Parameters.Properties["query"].Type, "string")
	}

	result, err := registry.Execute(context.Background(), "docs.search_docs", map[string]any{
		"query": "registry",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Content != "matched docs" {
		t.Fatalf("result.Content = %q, want %q", result.Content, "matched docs")
	}
	if result.Ephemeral {
		t.Fatal("result.Ephemeral = true, want false")
	}
	if fakeClient.callName != "search_docs" {
		t.Fatalf("call name = %q, want %q", fakeClient.callName, "search_docs")
	}
	if got := fmt.Sprint(fakeClient.callArgs["query"]); got != "registry" {
		t.Fatalf("call args query = %q, want %q", got, "registry")
	}
}

func TestRegistry_RegisterMCPPrompts(t *testing.T) {
	registry := NewRegistry()
	fakeClient := &fakeMCPClient{
		prompts: []coremcp.PromptDescriptor{
			{
				Name:        "compose_release",
				Description: "Compose release notes",
				Arguments: []coremcp.PromptArgument{
					{Name: "topic", Description: "Release topic", Required: true},
					{Name: "tone", Description: "Writing tone"},
				},
			},
		},
		promptResult: coremcp.GetPromptResult{
			Description: "Compose release notes",
			Text:        "user:\nDraft release notes for registry\n\nassistant:\nFocus on user-facing changes.",
		},
	}

	if err := registry.RegisterMCPPrompts(fakeClient, MCPRegistrationOptions{Prefix: "docs"}); err != nil {
		t.Fatalf("RegisterMCPPrompts() error = %v", err)
	}

	tools := registry.List()
	if len(tools) != 1 {
		t.Fatalf("List() length = %d, want 1", len(tools))
	}
	if tools[0].Name != "docs.prompt.compose_release" {
		t.Fatalf("tool name = %q, want %q", tools[0].Name, "docs.prompt.compose_release")
	}
	if tools[0].Parameters.Properties["topic"].Type != "string" {
		t.Fatalf("topic type = %q, want %q", tools[0].Parameters.Properties["topic"].Type, "string")
	}
	if tools[0].Parameters.Properties["tone"].Description != "Writing tone" {
		t.Fatalf("tone description = %q, want %q", tools[0].Parameters.Properties["tone"].Description, "Writing tone")
	}

	result, err := registry.Execute(context.Background(), "docs.prompt.compose_release", map[string]any{
		"topic": "registry",
		"tone":  "concise",
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Content != fakeClient.promptResult.Text {
		t.Fatalf("result.Content = %q, want %q", result.Content, fakeClient.promptResult.Text)
	}
	if fakeClient.promptName != "compose_release" {
		t.Fatalf("prompt name = %q, want %q", fakeClient.promptName, "compose_release")
	}
	if fakeClient.promptArgs["topic"] != "registry" {
		t.Fatalf("prompt args topic = %q, want %q", fakeClient.promptArgs["topic"], "registry")
	}
	if fakeClient.promptArgs["tone"] != "concise" {
		t.Fatalf("prompt args tone = %q, want %q", fakeClient.promptArgs["tone"], "concise")
	}
}

func TestRegisterStoresApprovalPolicyAndDefinition(t *testing.T) {
	registry := NewRegistry()

	if err := registry.Register(Tool{
		Name:         "delete_file",
		Description:  "Delete a file",
		Parameters:   types.JSONSchema{},
		ApprovalMode: types.ToolApprovalModeConditional,
		ApprovalEvaluator: func(arguments map[string]any) ApprovalRequirement {
			if fmt.Sprint(arguments["path"]) == "danger.txt" {
				return ApprovalRequirement{
					Required:         true,
					RiskLevel:        RiskLevelHigh,
					ArgumentsSummary: "path=danger.txt",
					Reason:           "deletes danger.txt",
				}
			}
			return ApprovalRequirement{}
		},
		Handler: func(_ context.Context, _ map[string]any) (string, error) {
			return "ok", nil
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	definitions := registry.List()
	if len(definitions) != 1 {
		t.Fatalf("List() length = %d, want 1", len(definitions))
	}
	if definitions[0].ApprovalMode != types.ToolApprovalModeConditional {
		t.Fatalf("ApprovalMode = %q, want %q", definitions[0].ApprovalMode, types.ToolApprovalModeConditional)
	}

	policy, ok := registry.ApprovalPolicy("delete_file")
	if !ok {
		t.Fatal("ApprovalPolicy(delete_file) ok = false, want true")
	}
	requirement := policy.Evaluate(map[string]any{"path": "danger.txt"})
	if !requirement.Required {
		t.Fatal("policy.Evaluate(danger.txt) Required = false, want true")
	}
	if requirement.RiskLevel != RiskLevelHigh {
		t.Fatalf("policy.Evaluate(danger.txt) RiskLevel = %q, want %q", requirement.RiskLevel, RiskLevelHigh)
	}
	if requirement.ArgumentsSummary != "path=danger.txt" {
		t.Fatalf("policy.Evaluate(danger.txt) ArgumentsSummary = %q, want %q", requirement.ArgumentsSummary, "path=danger.txt")
	}
	if requirement.Reason != "deletes danger.txt" {
		t.Fatalf("policy.Evaluate(danger.txt) Reason = %q, want %q", requirement.Reason, "deletes danger.txt")
	}

	if _, ok := registry.ApprovalPolicy("missing"); ok {
		t.Fatal("ApprovalPolicy(missing) ok = true, want false")
	}
}

func TestRegisterRejectsInvalidApprovalMode(t *testing.T) {
	registry := NewRegistry()
	err := registry.Register(Tool{
		Name:         "invalid_tool",
		ApprovalMode: types.ToolApprovalMode("bogus"),
		Handler: func(_ context.Context, _ map[string]any) (string, error) {
			return "ok", nil
		},
	})
	if err == nil {
		t.Fatal("Register() error = nil, want invalid approval mode error")
	}
	if !strings.Contains(err.Error(), "approval mode") {
		t.Fatalf("Register() error = %q, want approval mode message", err)
	}
}
