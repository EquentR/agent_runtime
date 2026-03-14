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

func TestClient_ListToolsMapsSchema(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	server := mark3server.NewMCPServer("test-server", "1.0.0", mark3server.WithToolCapabilities(false))
	server.AddTool(mark3mcp.NewTool(
		"search_docs",
		mark3mcp.WithDescription("Search project docs"),
		mark3mcp.WithString("query", mark3mcp.Required(), mark3mcp.Description("Search query")),
	), func(context.Context, mark3mcp.CallToolRequest) (*mark3mcp.CallToolResult, error) {
		return &mark3mcp.CallToolResult{Content: []mark3mcp.Content{mark3mcp.TextContent{Type: "text", Text: "ok"}}}, nil
	})

	rawClient, err := mark3client.NewInProcessClient(server)
	if err != nil {
		t.Fatalf("NewInProcessClient() error = %v", err)
	}
	defer rawClient.Close()

	if err := rawClient.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	_, err = rawClient.Initialize(ctx, mark3mcp.InitializeRequest{
		Params: mark3mcp.InitializeParams{
			ProtocolVersion: mark3mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: mark3mcp.Implementation{
				Name:    "agent-runtime-test",
				Version: "1.0.0",
			},
		},
	})
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	client, err := NewClient(rawClient)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("ListTools() length = %d, want 1", len(tools))
	}
	if tools[0].Name != "search_docs" {
		t.Fatalf("tool name = %q, want %q", tools[0].Name, "search_docs")
	}
	if tools[0].InputSchema.Type != "object" {
		t.Fatalf("schema type = %q, want %q", tools[0].InputSchema.Type, "object")
	}
	if tools[0].InputSchema.Properties["query"].Description != "Search query" {
		t.Fatalf("query description = %q, want %q", tools[0].InputSchema.Properties["query"].Description, "Search query")
	}
	if len(tools[0].InputSchema.Required) != 1 || tools[0].InputSchema.Required[0] != "query" {
		t.Fatalf("required = %#v, want []string{\"query\"}", tools[0].InputSchema.Required)
	}
	if tools[0].InputSchema.Properties["query"].Type != "string" {
		t.Fatalf("query type = %q, want %q", tools[0].InputSchema.Properties["query"].Type, "string")
	}
}

func TestClient_CallToolReturnsText(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	server := mark3server.NewMCPServer("test-server", "1.0.0", mark3server.WithToolCapabilities(false))
	server.AddTool(mark3mcp.NewTool(
		"search_docs",
		mark3mcp.WithString("query", mark3mcp.Required()),
	), func(_ context.Context, request mark3mcp.CallToolRequest) (*mark3mcp.CallToolResult, error) {
		return &mark3mcp.CallToolResult{Content: []mark3mcp.Content{mark3mcp.TextContent{Type: "text", Text: "matched: " + request.GetString("query", "")}}}, nil
	})

	rawClient, err := mark3client.NewInProcessClient(server)
	if err != nil {
		t.Fatalf("NewInProcessClient() error = %v", err)
	}
	defer rawClient.Close()

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

	client, err := NewClient(rawClient)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	result, err := client.CallTool(ctx, coremcp.CallRequest{
		Name: "search_docs",
		Arguments: map[string]any{
			"query": "registry",
		},
	})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if result.Text != "matched: registry" {
		t.Fatalf("result.Text = %q, want %q", result.Text, "matched: registry")
	}
}

func TestClient_CallToolReturnsErrorWhenRemoteToolFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	server := mark3server.NewMCPServer("test-server", "1.0.0", mark3server.WithToolCapabilities(false))
	server.AddTool(mark3mcp.NewTool("search_docs"), func(context.Context, mark3mcp.CallToolRequest) (*mark3mcp.CallToolResult, error) {
		return &mark3mcp.CallToolResult{
			IsError: true,
			Content: []mark3mcp.Content{mark3mcp.TextContent{Type: "text", Text: "upstream failure"}},
		}, nil
	})

	rawClient, err := mark3client.NewInProcessClient(server)
	if err != nil {
		t.Fatalf("NewInProcessClient() error = %v", err)
	}
	defer rawClient.Close()

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

	client, err := NewClient(rawClient)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	_, err = client.CallTool(ctx, coremcp.CallRequest{Name: "search_docs"})
	if err == nil {
		t.Fatal("CallTool() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "upstream failure") {
		t.Fatalf("CallTool() error = %v, want contains %q", err, "upstream failure")
	}
}
