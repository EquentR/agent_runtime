package mcp_config

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/EquentR/agent_runtime/core/mcp"
	mark3labs "github.com/EquentR/agent_runtime/core/mcp/mark3labs"
	coretools "github.com/EquentR/agent_runtime/core/tools"
	mark3transport "github.com/mark3labs/mcp-go/client/transport"
)

const (
	TransportStdio          = "stdio"
	TransportSSE            = "sse"
	TransportStreamableHTTP = "streamable_http"
)

type MCP struct {
	Servers []MCPServerConfig `yaml:"servers"`
}

type MCPServerConfig struct {
	Name          string            `yaml:"name"`
	Enabled       bool              `yaml:"enabled"`
	Transport     string            `yaml:"transport"`
	URL           string            `yaml:"url"`
	Command       string            `yaml:"command"`
	Args          []string          `yaml:"args"`
	Env           []string          `yaml:"env"`
	Headers       map[string]string `yaml:"headers"`
	Host          string            `yaml:"host"`
	Timeout       time.Duration     `yaml:"timeout"`
	EnableTools   bool              `yaml:"enableTools"`
	EnablePrompts bool              `yaml:"enablePrompts"`
	ToolPrefix    string            `yaml:"toolPrefix"`
	PromptPrefix  string            `yaml:"promptPrefix"`
}

func (c MCPServerConfig) NewClient() (mcp.Client, error) {
	transport := strings.TrimSpace(c.Transport)
	switch transport {
	case TransportStdio:
		if strings.TrimSpace(c.Command) == "" {
			return nil, fmt.Errorf("mcp stdio command cannot be empty")
		}
		return mark3labs.NewStdioClient(c.Command, c.Env, c.Args...)
	case TransportSSE:
		if strings.TrimSpace(c.URL) == "" {
			return nil, fmt.Errorf("mcp sse url cannot be empty")
		}
		return mark3labs.NewSSEClient(c.URL, c.sseOptions()...)
	case TransportStreamableHTTP:
		if strings.TrimSpace(c.URL) == "" {
			return nil, fmt.Errorf("mcp streamable http url cannot be empty")
		}
		return mark3labs.NewStreamableHTTPClient(c.URL, c.streamableHTTPOptions()...)
	default:
		return nil, fmt.Errorf("unsupported mcp transport: %s", c.Transport)
	}
}

func (c MCPServerConfig) RegistrationOptions() coretools.MCPRegistrationOptions {
	return coretools.MCPRegistrationOptions{
		Prefix:       c.ToolPrefix,
		PromptPrefix: c.PromptPrefix,
	}
}

func (c MCPServerConfig) sseOptions() []mark3transport.ClientOption {
	options := make([]mark3transport.ClientOption, 0, 3)
	if len(c.Headers) > 0 {
		options = append(options, mark3transport.WithHeaders(c.Headers))
	}
	if strings.TrimSpace(c.Host) != "" {
		options = append(options, mark3transport.WithHTTPHost(c.Host))
	}
	return options
}

func (c MCPServerConfig) streamableHTTPOptions() []mark3transport.StreamableHTTPCOption {
	options := make([]mark3transport.StreamableHTTPCOption, 0, 3)
	if len(c.Headers) > 0 {
		options = append(options, mark3transport.WithHTTPHeaders(c.Headers))
	}
	if strings.TrimSpace(c.Host) != "" {
		options = append(options, mark3transport.WithStreamableHTTPHost(c.Host))
	}
	if client := c.httpClient(); client != nil {
		options = append(options, mark3transport.WithHTTPBasicClient(client))
	}
	return options
}

func (c MCPServerConfig) httpClient() *http.Client {
	if c.Timeout <= 0 {
		return nil
	}
	return &http.Client{Timeout: c.Timeout}
}
