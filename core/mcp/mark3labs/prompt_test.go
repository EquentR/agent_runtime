package mark3labs

import (
	"context"
	"strings"
	"testing"

	coremcp "github.com/EquentR/agent_runtime/core/mcp"
	mark3client "github.com/mark3labs/mcp-go/client"
	mark3mcp "github.com/mark3labs/mcp-go/mcp"
	mark3server "github.com/mark3labs/mcp-go/server"
)

func TestClient_ListPromptsMapsArguments(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rawClient := newInitializedPromptClient(t, ctx)
	defer rawClient.Close()

	client, err := NewClient(rawClient)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	prompts, err := client.ListPrompts(ctx)
	if err != nil {
		t.Fatalf("ListPrompts() error = %v", err)
	}
	if len(prompts) != 1 {
		t.Fatalf("ListPrompts() length = %d, want 1", len(prompts))
	}
	if prompts[0].Name != "compose_release" {
		t.Fatalf("prompt name = %q, want %q", prompts[0].Name, "compose_release")
	}
	if prompts[0].Description != "Compose release notes" {
		t.Fatalf("prompt description = %q, want %q", prompts[0].Description, "Compose release notes")
	}
	if len(prompts[0].Arguments) != 2 {
		t.Fatalf("arguments length = %d, want 2", len(prompts[0].Arguments))
	}
	if prompts[0].Arguments[0].Name != "topic" || !prompts[0].Arguments[0].Required {
		t.Fatalf("first argument = %#v, want required topic", prompts[0].Arguments[0])
	}
	if prompts[0].Arguments[1].Name != "tone" || prompts[0].Arguments[1].Required {
		t.Fatalf("second argument = %#v, want optional tone", prompts[0].Arguments[1])
	}
}

func TestClient_GetPromptReturnsTranscript(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rawClient := newInitializedPromptClient(t, ctx)
	defer rawClient.Close()

	client, err := NewClient(rawClient)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	result, err := client.GetPrompt(ctx, coremcp.GetPromptRequest{
		Name: "compose_release",
		Arguments: map[string]string{
			"topic": "registry",
			"tone":  "concise",
		},
	})
	if err != nil {
		t.Fatalf("GetPrompt() error = %v", err)
	}
	if result.Description != "Compose release notes" {
		t.Fatalf("description = %q, want %q", result.Description, "Compose release notes")
	}
	if len(result.Messages) != 2 {
		t.Fatalf("messages length = %d, want 2", len(result.Messages))
	}
	if !strings.Contains(result.Text, "user:") {
		t.Fatalf("result.Text = %q, want contains %q", result.Text, "user:")
	}
	if !strings.Contains(result.Text, "Draft release notes for registry in concise tone") {
		t.Fatalf("result.Text = %q, want rendered user content", result.Text)
	}
	if !strings.Contains(result.Text, "assistant:") {
		t.Fatalf("result.Text = %q, want contains %q", result.Text, "assistant:")
	}
}

func newInitializedPromptClient(t *testing.T, ctx context.Context) *mark3client.Client {
	t.Helper()

	server := mark3server.NewMCPServer("test-server", "1.0.0", mark3server.WithPromptCapabilities(false))
	server.AddPrompt(mark3mcp.NewPrompt(
		"compose_release",
		mark3mcp.WithPromptDescription("Compose release notes"),
		mark3mcp.WithArgument("topic", mark3mcp.ArgumentDescription("Release topic"), mark3mcp.RequiredArgument()),
		mark3mcp.WithArgument("tone", mark3mcp.ArgumentDescription("Writing tone")),
	), func(_ context.Context, request mark3mcp.GetPromptRequest) (*mark3mcp.GetPromptResult, error) {
		return &mark3mcp.GetPromptResult{
			Description: "Compose release notes",
			Messages: []mark3mcp.PromptMessage{
				{
					Role: mark3mcp.RoleUser,
					Content: mark3mcp.TextContent{
						Type: "text",
						Text: "Draft release notes for " + request.Params.Arguments["topic"] + " in " + request.Params.Arguments["tone"] + " tone",
					},
				},
				{
					Role: mark3mcp.RoleAssistant,
					Content: mark3mcp.TextContent{
						Type: "text",
						Text: "Focus on user-facing changes.",
					},
				},
			},
		}, nil
	})

	rawClient, err := mark3client.NewInProcessClient(server)
	if err != nil {
		t.Fatalf("NewInProcessClient() error = %v", err)
	}
	if err := rawClient.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	_, err = rawClient.Initialize(ctx, mark3mcp.InitializeRequest{
		Params: mark3mcp.InitializeParams{
			ProtocolVersion: mark3mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo:      mark3mcp.Implementation{Name: "agent-runtime-test", Version: "1.0.0"},
		},
	})
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	return rawClient
}
