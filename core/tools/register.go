package tools

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	coremcp "github.com/EquentR/agent_runtime/core/mcp"
	"github.com/EquentR/agent_runtime/core/types"
)

var ErrToolNotFound = errors.New("tool not found")

type Result struct {
	Content   string
	Ephemeral bool
}

// Handler 定义本地工具的执行签名。
type Handler func(ctx context.Context, arguments map[string]interface{}) (string, error)

// ResultHandler 定义支持结构化结果的工具执行签名。
type ResultHandler func(ctx context.Context, arguments map[string]interface{}) (Result, error)

// Tool 表示一个可注册、可执行的本地工具。
type Tool struct {
	Name              string
	Description       string
	Parameters        types.JSONSchema
	ApprovalMode      types.ToolApprovalMode
	ApprovalEvaluator ApprovalEvaluator
	Handler           Handler
	ResultHandler     ResultHandler
	Source            string
}

// MCPRegistrationOptions 控制 MCP 工具注册时的命名行为。
type MCPRegistrationOptions struct {
	Prefix       string
	PromptPrefix string
}

// Registry 维护工具定义与执行器。
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewRegistry 创建一个空的工具注册器。
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
	}
}

// Register 注册一个或多个本地工具。
func (r *Registry) Register(tools ...Tool) error {
	if len(tools) == 0 {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// 先完整校验整批工具，再统一写入注册表，避免中途失败时留下部分注册成功、
	// 部分失败的半完成状态。
	seen := make(map[string]struct{}, len(tools))
	for _, tool := range tools {
		if err := tool.Validate(); err != nil {
			return fmt.Errorf("invalid tool %q: %w", tool.Name, err)
		}
		if _, ok := r.tools[tool.Name]; ok {
			return fmt.Errorf("tool %q already registered", tool.Name)
		}
		if _, ok := seen[tool.Name]; ok {
			return fmt.Errorf("duplicate tool %q in batch", tool.Name)
		}
		seen[tool.Name] = struct{}{}
	}

	for _, tool := range tools {
		tool.Parameters = normalizeSchema(tool.Parameters)
		r.tools[tool.Name] = tool
	}

	return nil
}

// RegisterMCPClient 将一个 MCP client 暴露的工具批量注册到本地注册器中。
func (r *Registry) RegisterMCPClient(client coremcp.Client, options MCPRegistrationOptions) error {
	if client == nil {
		return fmt.Errorf("mcp client cannot be nil")
	}

	mcpTools, err := client.ListTools(context.Background())
	if err != nil {
		return fmt.Errorf("list mcp tools: %w", err)
	}

	tools := make([]Tool, 0, len(mcpTools))
	for _, remoteTool := range mcpTools {
		remoteTool := remoteTool
		name := qualifyToolName(options.Prefix, remoteTool.Name)
		tools = append(tools, Tool{
			Name:        name,
			Description: remoteTool.Description,
			Parameters:  remoteTool.InputSchema,
			Source:      "mcp",
			ResultHandler: func(ctx context.Context, arguments map[string]interface{}) (Result, error) {
				result, err := client.CallTool(ctx, coremcp.CallRequest{
					Name:      remoteTool.Name,
					Arguments: arguments,
				})
				if err != nil {
					return Result{}, err
				}
				return Result{Content: result.Text}, nil
			},
		})
	}

	return r.Register(tools...)
}

// RegisterMCPPrompts 将一个 MCP client 暴露的 prompts 包装成可调用的本地工具。
func (r *Registry) RegisterMCPPrompts(client coremcp.Client, options MCPRegistrationOptions) error {
	if client == nil {
		return fmt.Errorf("mcp client cannot be nil")
	}

	prompts, err := client.ListPrompts(context.Background())
	if err != nil {
		return fmt.Errorf("list mcp prompts: %w", err)
	}

	tools := make([]Tool, 0, len(prompts))
	for _, prompt := range prompts {
		prompt := prompt
		tools = append(tools, Tool{
			Name:        qualifyPromptName(options, prompt.Name),
			Description: prompt.Description,
			Parameters:  promptArgumentsToSchema(prompt.Arguments),
			Source:      "mcp_prompt",
			ResultHandler: func(ctx context.Context, arguments map[string]interface{}) (Result, error) {
				result, err := client.GetPrompt(ctx, coremcp.GetPromptRequest{
					Name:      prompt.Name,
					Arguments: stringifyArguments(arguments),
				})
				if err != nil {
					return Result{}, err
				}
				return Result{Content: result.Text}, nil
			},
		})
	}

	return r.Register(tools...)
}

// List 返回可暴露给 LLM 的工具定义列表。
func (r *Registry) List() []types.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)

	result := make([]types.Tool, 0, len(names))
	for _, name := range names {
		result = append(result, r.tools[name].Definition())
	}

	return result
}

// Execute 调用指定工具。
func (r *Registry) Execute(ctx context.Context, name string, arguments map[string]interface{}) (Result, error) {
	r.mu.RLock()
	tool, ok := r.tools[name]
	r.mu.RUnlock()
	if !ok {
		return Result{}, fmt.Errorf("%w: %s", ErrToolNotFound, name)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if arguments == nil {
		arguments = map[string]interface{}{}
	}
	if tool.ResultHandler != nil {
		return tool.ResultHandler(ctx, arguments)
	}
	content, err := tool.Handler(ctx, arguments)
	if err != nil {
		return Result{}, err
	}
	return Result{Content: content}, nil
}

// ApprovalPolicy 返回工具对应的审批策略。
func (r *Registry) ApprovalPolicy(name string) (ApprovalPolicy, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	tool, ok := r.tools[name]
	if !ok {
		return ApprovalPolicy{}, false
	}

	return ApprovalPolicy{
		Mode:      tool.ApprovalMode,
		Evaluator: tool.ApprovalEvaluator,
	}, true
}

// Validate 校验工具定义是否合法。
func (t Tool) Validate() error {
	if t.Name == "" {
		return fmt.Errorf("tool name cannot be empty")
	}
	if !IsValidApprovalMode(t.ApprovalMode) {
		return fmt.Errorf("invalid approval mode %q", t.ApprovalMode)
	}
	if t.ApprovalMode == types.ToolApprovalModeConditional && t.ApprovalEvaluator == nil {
		return fmt.Errorf("conditional approval requires evaluator")
	}
	if t.Handler == nil && t.ResultHandler == nil {
		return fmt.Errorf("tool handler cannot be nil")
	}
	return nil
}

// Definition 提取工具的 LLM 描述信息。
func (t Tool) Definition() types.Tool {
	return types.Tool{
		Name:         t.Name,
		Description:  t.Description,
		Parameters:   normalizeSchema(t.Parameters),
		ApprovalMode: t.ApprovalMode,
	}
}

func normalizeSchema(schema types.JSONSchema) types.JSONSchema {
	// 下游各家 LLM 适配层都默认这里拿到的是完整 object schema，因此在注册阶段
	// 统一补齐空 map / slice，避免后面重复做 nil 判断。
	if schema.Type == "" {
		schema.Type = "object"
	}
	if schema.Properties == nil {
		schema.Properties = map[string]types.SchemaProperty{}
	}
	if schema.Required == nil {
		schema.Required = []string{}
	}
	return schema
}

func qualifyToolName(prefix string, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

func qualifyPromptName(options MCPRegistrationOptions, name string) string {
	prefix := options.PromptPrefix
	if prefix == "" {
		if options.Prefix == "" {
			prefix = "prompt"
		} else {
			prefix = options.Prefix + ".prompt"
		}
	}
	return qualifyToolName(prefix, name)
}

func promptArgumentsToSchema(arguments []coremcp.PromptArgument) types.JSONSchema {
	schema := types.JSONSchema{
		Type:       "object",
		Properties: map[string]types.SchemaProperty{},
		Required:   []string{},
	}

	for _, argument := range arguments {
		schema.Properties[argument.Name] = types.SchemaProperty{
			Type:        "string",
			Description: argument.Description,
		}
		if argument.Required {
			schema.Required = append(schema.Required, argument.Name)
		}
	}

	return schema
}

func stringifyArguments(arguments map[string]interface{}) map[string]string {
	if len(arguments) == 0 {
		return map[string]string{}
	}

	out := make(map[string]string, len(arguments))
	for key, value := range arguments {
		out[key] = fmt.Sprint(value)
	}

	return out
}
