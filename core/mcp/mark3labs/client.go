package mark3labs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	coremcp "github.com/EquentR/agent_runtime/core/mcp"
	mark3client "github.com/mark3labs/mcp-go/client"
	mark3transport "github.com/mark3labs/mcp-go/client/transport"
	mark3mcp "github.com/mark3labs/mcp-go/mcp"
)

var errNoTextContent = errors.New("tool result did not contain text content")

const (
	defaultClientName    = "agent_runtime"
	defaultClientVersion = "dev"
)

type Client struct {
	raw mark3client.MCPClient
}

func NewStdioClient(command string, env []string, args ...string) (*Client, error) {
	raw, err := mark3client.NewStdioMCPClient(command, env, args...)
	if err != nil {
		return nil, fmt.Errorf("create stdio mcp client: %w", err)
	}

	return wrapInitializedClient(raw)
}

func NewSSEClient(baseURL string, options ...mark3transport.ClientOption) (*Client, error) {
	raw, err := mark3client.NewSSEMCPClient(baseURL, options...)
	if err != nil {
		return nil, fmt.Errorf("create sse mcp client: %w", err)
	}

	return wrapInitializedClient(raw)
}

func NewStreamableHTTPClient(baseURL string, options ...mark3transport.StreamableHTTPCOption) (*Client, error) {
	raw, err := mark3client.NewStreamableHttpClient(baseURL, options...)
	if err != nil {
		return nil, fmt.Errorf("create streamable http mcp client: %w", err)
	}

	return wrapInitializedClient(raw)
}

func NewClient(raw mark3client.MCPClient) (*Client, error) {
	if isNil(raw) {
		return nil, errors.New("mcp client cannot be nil")
	}
	return &Client{raw: raw}, nil
}

func (c *Client) ListTools(ctx context.Context) ([]coremcp.ToolDescriptor, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	result, err := c.raw.ListTools(ctx, mark3mcp.ListToolsRequest{})
	if err != nil {
		return nil, fmt.Errorf("list tools: %w", err)
	}
	if result == nil || len(result.Tools) == 0 {
		return []coremcp.ToolDescriptor{}, nil
	}

	tools := make([]coremcp.ToolDescriptor, 0, len(result.Tools))
	for _, tool := range result.Tools {
		tools = append(tools, coremcp.ToolDescriptor{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: toSchema(tool.InputSchema),
		})
	}

	return tools, nil
}

func (c *Client) CallTool(ctx context.Context, request coremcp.CallRequest) (coremcp.CallResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	result, err := c.raw.CallTool(ctx, mark3mcp.CallToolRequest{
		Params: mark3mcp.CallToolParams{
			Name:      request.Name,
			Arguments: request.Arguments,
		},
	})
	if err != nil {
		return coremcp.CallResult{}, fmt.Errorf("call tool %q: %w", request.Name, err)
	}

	text, err := extractText(result)
	if result != nil && result.IsError {
		if err != nil {
			return coremcp.CallResult{}, fmt.Errorf("call tool %q failed", request.Name)
		}
		return coremcp.CallResult{}, fmt.Errorf("call tool %q failed: %s", request.Name, text)
	}
	if err != nil {
		return coremcp.CallResult{}, fmt.Errorf("call tool %q: %w", request.Name, err)
	}

	return coremcp.CallResult{Text: text, Raw: result}, nil
}

func (c *Client) ListPrompts(ctx context.Context) ([]coremcp.PromptDescriptor, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	result, err := c.raw.ListPrompts(ctx, mark3mcp.ListPromptsRequest{})
	if err != nil {
		return nil, fmt.Errorf("list prompts: %w", err)
	}
	if result == nil || len(result.Prompts) == 0 {
		return []coremcp.PromptDescriptor{}, nil
	}

	prompts := make([]coremcp.PromptDescriptor, 0, len(result.Prompts))
	for _, prompt := range result.Prompts {
		prompts = append(prompts, coremcp.PromptDescriptor{
			Name:        prompt.Name,
			Description: prompt.Description,
			Arguments:   toPromptArguments(prompt.Arguments),
		})
	}

	return prompts, nil
}

func (c *Client) GetPrompt(ctx context.Context, request coremcp.GetPromptRequest) (coremcp.GetPromptResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	result, err := c.raw.GetPrompt(ctx, mark3mcp.GetPromptRequest{
		Params: mark3mcp.GetPromptParams{
			Name:      request.Name,
			Arguments: request.Arguments,
		},
	})
	if err != nil {
		return coremcp.GetPromptResult{}, fmt.Errorf("get prompt %q: %w", request.Name, err)
	}
	if result == nil {
		return coremcp.GetPromptResult{}, fmt.Errorf("get prompt %q: nil result", request.Name)
	}

	messages := toPromptMessages(result.Messages)
	return coremcp.GetPromptResult{
		Description: result.Description,
		Messages:    messages,
		Text:        renderPromptText(result.Description, messages),
		Raw:         result,
	}, nil
}

func (c *Client) Close() error {
	return c.raw.Close()
}

func wrapInitializedClient(raw *mark3client.Client) (*Client, error) {
	if raw == nil {
		return nil, errors.New("mcp client cannot be nil")
	}

	ctx := context.Background()
	if err := raw.Start(ctx); err != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("start mcp client: %w", err)
	}

	_, err := raw.Initialize(ctx, mark3mcp.InitializeRequest{
		Params: mark3mcp.InitializeParams{
			ProtocolVersion: mark3mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: mark3mcp.Implementation{
				Name:    defaultClientName,
				Version: defaultClientVersion,
			},
		},
	})
	if err != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("initialize mcp client: %w", err)
	}

	return NewClient(raw)
}

func toSchema(schema mark3mcp.ToolInputSchema) coremcp.Schema {
	properties := make(map[string]coremcp.SchemaProperty, len(schema.Properties))
	for name, raw := range schema.Properties {
		properties[name] = toSchemaProperty(raw)
	}

	required := append([]string(nil), schema.Required...)
	if required == nil {
		required = []string{}
	}

	typeName := schema.Type
	if typeName == "" {
		typeName = "object"
	}

	return coremcp.Schema{
		Type:       typeName,
		Properties: properties,
		Required:   required,
	}
}

func toSchemaProperty(raw any) coremcp.SchemaProperty {
	propertyMap, ok := raw.(map[string]any)
	if !ok {
		return coremcp.SchemaProperty{}
	}

	property := coremcp.SchemaProperty{}
	if typeName, ok := propertyMap["type"].(string); ok {
		property.Type = typeName
	}
	if description, ok := propertyMap["description"].(string); ok {
		property.Description = description
	}
	property.Enum = toStringSlice(propertyMap["enum"])

	return property
}

func toStringSlice(raw any) []string {
	switch values := raw.(type) {
	case nil:
		return nil
	case []string:
		return append([]string(nil), values...)
	case []any:
		result := make([]string, 0, len(values))
		for _, value := range values {
			if str, ok := value.(string); ok {
				result = append(result, str)
			}
		}
		if len(result) == 0 {
			return nil
		}
		return result
	default:
		return nil
	}
}

func toPromptArguments(arguments []mark3mcp.PromptArgument) []coremcp.PromptArgument {
	if len(arguments) == 0 {
		return nil
	}

	out := make([]coremcp.PromptArgument, 0, len(arguments))
	for _, argument := range arguments {
		out = append(out, coremcp.PromptArgument{
			Name:        argument.Name,
			Description: argument.Description,
			Required:    argument.Required,
		})
	}

	return out
}

func toPromptMessages(messages []mark3mcp.PromptMessage) []coremcp.PromptMessage {
	if len(messages) == 0 {
		return nil
	}

	out := make([]coremcp.PromptMessage, 0, len(messages))
	for _, message := range messages {
		out = append(out, coremcp.PromptMessage{
			Role:    string(message.Role),
			Content: renderPromptContent(message.Content),
		})
	}

	return out
}

func renderPromptText(description string, messages []coremcp.PromptMessage) string {
	parts := make([]string, 0, len(messages)+1)
	if strings.TrimSpace(description) != "" {
		parts = append(parts, "description: "+description)
	}

	for _, message := range messages {
		content := strings.TrimSpace(message.Content)
		if content == "" {
			continue
		}
		role := strings.TrimSpace(message.Role)
		if role == "" {
			role = "message"
		}
		parts = append(parts, role+":\n"+content)
	}

	return strings.Join(parts, "\n\n")
}

func renderPromptContent(content mark3mcp.Content) string {
	switch value := content.(type) {
	case mark3mcp.TextContent:
		return value.Text
	case *mark3mcp.TextContent:
		if value == nil {
			return ""
		}
		return value.Text
	case mark3mcp.ImageContent:
		return fmt.Sprintf("[image:%s]", value.MIMEType)
	case *mark3mcp.ImageContent:
		if value == nil {
			return ""
		}
		return fmt.Sprintf("[image:%s]", value.MIMEType)
	case mark3mcp.AudioContent:
		return fmt.Sprintf("[audio:%s]", value.MIMEType)
	case *mark3mcp.AudioContent:
		if value == nil {
			return ""
		}
		return fmt.Sprintf("[audio:%s]", value.MIMEType)
	case mark3mcp.ResourceLink:
		return fmt.Sprintf("[resource:%s]", value.URI)
	case *mark3mcp.ResourceLink:
		if value == nil {
			return ""
		}
		return fmt.Sprintf("[resource:%s]", value.URI)
	case mark3mcp.EmbeddedResource:
		return renderEmbeddedResource(value.Resource)
	case *mark3mcp.EmbeddedResource:
		if value == nil {
			return ""
		}
		return renderEmbeddedResource(value.Resource)
	default:
		bytes, err := json.Marshal(content)
		if err != nil {
			return ""
		}
		return string(bytes)
	}
}

func renderEmbeddedResource(resource mark3mcp.ResourceContents) string {
	switch value := resource.(type) {
	case mark3mcp.TextResourceContents:
		return value.Text
	case *mark3mcp.TextResourceContents:
		if value == nil {
			return ""
		}
		return value.Text
	case mark3mcp.BlobResourceContents:
		return fmt.Sprintf("[resource:%s]", value.URI)
	case *mark3mcp.BlobResourceContents:
		if value == nil {
			return ""
		}
		return fmt.Sprintf("[resource:%s]", value.URI)
	default:
		bytes, err := json.Marshal(resource)
		if err != nil {
			return ""
		}
		return string(bytes)
	}
}

func extractText(result *mark3mcp.CallToolResult) (string, error) {
	if result == nil {
		return "", errors.New("tool result cannot be nil")
	}

	parts := make([]string, 0, len(result.Content))
	for _, content := range result.Content {
		switch value := content.(type) {
		case mark3mcp.TextContent:
			if strings.TrimSpace(value.Text) != "" {
				parts = append(parts, value.Text)
			}
		case *mark3mcp.TextContent:
			if value != nil && strings.TrimSpace(value.Text) != "" {
				parts = append(parts, value.Text)
			}
		}
	}

	if len(parts) > 0 {
		return strings.Join(parts, "\n"), nil
	}

	if result.StructuredContent != nil {
		bytes, err := json.Marshal(result.StructuredContent)
		if err != nil {
			return "", fmt.Errorf("marshal structured content: %w", err)
		}
		return string(bytes), nil
	}

	return "", errNoTextContent
}

func isNil(value any) bool {
	if value == nil {
		return true
	}

	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}
