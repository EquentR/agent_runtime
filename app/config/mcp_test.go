package config

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	mark3mcp "github.com/mark3labs/mcp-go/mcp"
	mark3server "github.com/mark3labs/mcp-go/server"
	"gopkg.in/yaml.v3"
)

func TestMCPConfig_UnmarshalYAML(t *testing.T) {
	t.Parallel()

	data := []byte(`servers:
  - name: docs
    enabled: true
    transport: streamable_http
    url: http://example.com/mcp
    headers:
      Authorization: Bearer token
    host: docs.internal
    timeout: 5s
    enableTools: true
    enablePrompts: true
    toolPrefix: docs
    promptPrefix: docs.prompt
`)

	var cfg MCP
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}
	if len(cfg.Servers) != 1 {
		t.Fatalf("servers length = %d, want 1", len(cfg.Servers))
	}
	server := cfg.Servers[0]
	if server.Name != "docs" || server.Transport != TransportStreamableHTTP {
		t.Fatalf("server = %#v, want docs streamable_http", server)
	}
	if server.Timeout != 5*time.Second {
		t.Fatalf("timeout = %v, want %v", server.Timeout, 5*time.Second)
	}
	if !server.EnableTools || !server.EnablePrompts {
		t.Fatalf("server flags = %#v, want tools/prompts enabled", server)
	}
	if server.ToolPrefix != "docs" || server.PromptPrefix != "docs.prompt" {
		t.Fatalf("prefixes = %#v, want docs/docs.prompt", server)
	}
	if server.Headers["Authorization"] != "Bearer token" {
		t.Fatalf("headers = %#v, want authorization header", server.Headers)
	}
}

func TestMCPServerConfig_NewClientStreamableHTTP(t *testing.T) {
	t.Parallel()

	server := newTestMCPServer()
	httpServer := mark3server.NewTestStreamableHTTPServer(server)
	defer httpServer.Close()

	cfg := MCPServerConfig{
		Transport:     TransportStreamableHTTP,
		URL:           httpServer.URL,
		EnableTools:   true,
		EnablePrompts: true,
	}

	client, err := cfg.NewClient()
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer client.Close()

	prompts, err := client.ListPrompts(context.Background())
	if err != nil {
		t.Fatalf("ListPrompts() error = %v", err)
	}
	if len(prompts) != 1 {
		t.Fatalf("prompts length = %d, want 1", len(prompts))
	}
}

func TestMCPServerConfig_NewClientSSE(t *testing.T) {
	t.Parallel()

	server := newTestMCPServer()
	sseServer := mark3server.NewTestServer(server)
	defer sseServer.Close()

	cfg := MCPServerConfig{Transport: TransportSSE, URL: sseServer.URL + "/sse"}
	client, err := cfg.NewClient()
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer client.Close()

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("tools length = %d, want 1", len(tools))
	}
}

func TestMCPServerConfig_NewClientSSEIgnoresTimeout(t *testing.T) {
	t.Parallel()

	server := newTestMCPServer()
	sseServer := mark3server.NewTestServer(server)
	defer sseServer.Close()

	cfg := MCPServerConfig{
		Transport: TransportSSE,
		URL:       sseServer.URL + "/sse",
		Timeout:   10 * time.Millisecond,
	}
	client, err := cfg.NewClient()
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer client.Close()

	time.Sleep(50 * time.Millisecond)

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("tools length = %d, want 1", len(tools))
	}
}

func TestMCPServerConfig_RegistrationOptions(t *testing.T) {
	t.Parallel()

	cfg := MCPServerConfig{
		ToolPrefix:   "docs",
		PromptPrefix: "docs.prompt",
	}

	options := cfg.RegistrationOptions()
	if options.Prefix != "docs" {
		t.Fatalf("options.Prefix = %q, want %q", options.Prefix, "docs")
	}
	if options.PromptPrefix != "docs.prompt" {
		t.Fatalf("options.PromptPrefix = %q, want %q", options.PromptPrefix, "docs.prompt")
	}
}

func TestMCPServerConfig_NewClientStdio(t *testing.T) {
	t.Parallel()

	cfg := MCPServerConfig{
		Transport: TransportStdio,
		Command:   os.Args[0],
		Env:       []string{"GO_WANT_APP_CONFIG_MCP_STDIO_HELPER=1"},
		Args:      []string{"-test.run=TestHelperProcessAppConfigMCPStdioServer"},
	}

	client, err := cfg.NewClient()
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer client.Close()

	prompts, err := client.ListPrompts(context.Background())
	if err != nil {
		t.Fatalf("ListPrompts() error = %v", err)
	}
	if len(prompts) != 1 {
		t.Fatalf("prompts length = %d, want 1", len(prompts))
	}
}

func TestHelperProcessAppConfigMCPStdioServer(t *testing.T) {
	if os.Getenv("GO_WANT_APP_CONFIG_MCP_STDIO_HELPER") != "1" {
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
	server := mark3server.NewMCPServer(
		"test-server",
		"1.0.0",
		mark3server.WithToolCapabilities(false),
		mark3server.WithPromptCapabilities(false),
	)
	server.AddTool(mark3mcp.NewTool(
		"search_docs",
		mark3mcp.WithDescription("Search project docs"),
		mark3mcp.WithString("query", mark3mcp.Required(), mark3mcp.Description("Search query")),
	), func(_ context.Context, request mark3mcp.CallToolRequest) (*mark3mcp.CallToolResult, error) {
		return &mark3mcp.CallToolResult{Content: []mark3mcp.Content{mark3mcp.TextContent{Type: "text", Text: "matched: " + request.GetString("query", "")}}}, nil
	})
	server.AddPrompt(mark3mcp.NewPrompt(
		"compose_release",
		mark3mcp.WithPromptDescription("Compose release notes"),
		mark3mcp.WithArgument("topic", mark3mcp.ArgumentDescription("Release topic"), mark3mcp.RequiredArgument()),
	), func(_ context.Context, request mark3mcp.GetPromptRequest) (*mark3mcp.GetPromptResult, error) {
		return &mark3mcp.GetPromptResult{
			Description: "Compose release notes",
			Messages: []mark3mcp.PromptMessage{{
				Role: mark3mcp.RoleUser,
				Content: mark3mcp.TextContent{
					Type: "text",
					Text: "Draft release notes for " + request.Params.Arguments["topic"],
				},
			}},
		}, nil
	})

	return server
}
