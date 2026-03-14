package mark3labs

import (
	"context"
	"fmt"
	"os"
	"testing"

	mark3mcp "github.com/mark3labs/mcp-go/mcp"
	mark3server "github.com/mark3labs/mcp-go/server"
)

func TestNewStreamableHTTPClient_InitializesAndListsTools(t *testing.T) {
	t.Parallel()

	server := newTestMCPServer()
	httpServer := mark3server.NewTestStreamableHTTPServer(server)
	defer httpServer.Close()

	client, err := NewStreamableHTTPClient(httpServer.URL)
	if err != nil {
		t.Fatalf("NewStreamableHTTPClient() error = %v", err)
	}
	defer client.Close()

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("ListTools() length = %d, want 1", len(tools))
	}
	if tools[0].Name != "search_docs" {
		t.Fatalf("tool name = %q, want %q", tools[0].Name, "search_docs")
	}
}

func TestNewSSEClient_InitializesAndListsTools(t *testing.T) {
	t.Parallel()

	server := newTestMCPServer()
	sseServer := mark3server.NewTestServer(server)
	defer sseServer.Close()

	client, err := NewSSEClient(sseServer.URL + "/sse")
	if err != nil {
		t.Fatalf("NewSSEClient() error = %v", err)
	}
	defer client.Close()

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("ListTools() length = %d, want 1", len(tools))
	}
	if tools[0].Name != "search_docs" {
		t.Fatalf("tool name = %q, want %q", tools[0].Name, "search_docs")
	}
	if tools[0].Description != "Search project docs" {
		t.Fatalf("tool description = %q, want %q", tools[0].Description, "Search project docs")
	}
}

func TestNewStdioClient_InitializesAndListsTools(t *testing.T) {
	t.Parallel()

	client, err := NewStdioClient(
		os.Args[0],
		[]string{"GO_WANT_MCP_STDIO_HELPER=1"},
		"-test.run=TestHelperProcessStdioServer",
	)
	if err != nil {
		t.Fatalf("NewStdioClient() error = %v", err)
	}
	defer client.Close()

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("ListTools() length = %d, want 1", len(tools))
	}
	if tools[0].Name != "search_docs" {
		t.Fatalf("tool name = %q, want %q", tools[0].Name, "search_docs")
	}
	if tools[0].Description != "Search project docs" {
		t.Fatalf("tool description = %q, want %q", tools[0].Description, "Search project docs")
	}
}

func TestHelperProcessStdioServer(t *testing.T) {
	if os.Getenv("GO_WANT_MCP_STDIO_HELPER") != "1" {
		return
	}

	server := newTestMCPServer()
	if err := mark3server.ServeStdio(server); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func newTestMCPServer() *mark3server.MCPServer {
	server := mark3server.NewMCPServer("test-server", "1.0.0", mark3server.WithToolCapabilities(false))
	server.AddTool(mark3mcp.NewTool(
		"search_docs",
		mark3mcp.WithDescription("Search project docs"),
		mark3mcp.WithString("query", mark3mcp.Required(), mark3mcp.Description("Search query")),
	), func(_ context.Context, request mark3mcp.CallToolRequest) (*mark3mcp.CallToolResult, error) {
		return &mark3mcp.CallToolResult{Content: []mark3mcp.Content{mark3mcp.TextContent{Type: "text", Text: "matched: " + request.GetString("query", "")}}}, nil
	})

	return server
}
