package openai

import (
	"errors"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/types"
	"github.com/sashabaranov/go-openai"
)

// buildChatCompletionStreamRequest 构建流式请求。
//
// 在普通聊天请求基础上开启 stream，并统一开启 usage 回传，
// 同时返回用于本地 token 统计的 prompt 文本切片。
func buildChatCompletionStreamRequest(req model.ChatRequest) (openai.ChatCompletionRequest, []string, error) {
	oaiReq, err := buildChatCompletionRequest(req)
	if err != nil {
		return openai.ChatCompletionRequest{}, nil, err
	}

	_, promptMessages, err := buildOpenAIMessages(req.Messages)
	if err != nil {
		return openai.ChatCompletionRequest{}, nil, err
	}

	oaiReq.Stream = true
	oaiReq.StreamOptions = &openai.StreamOptions{IncludeUsage: true}

	return oaiReq, promptMessages, nil
}

// buildChatCompletionRequest 将通用 ChatRequest 转换为 OpenAI ChatCompletionRequest。
//
// 该函数负责消息映射、采样参数映射、tools/tool_choice 映射，
// 供同步与流式两条链路复用。
func buildChatCompletionRequest(req model.ChatRequest) (openai.ChatCompletionRequest, error) {
	msgs, _, err := buildOpenAIMessages(req.Messages)
	if err != nil {
		return openai.ChatCompletionRequest{}, err
	}

	oaiReq := openai.ChatCompletionRequest{
		Model:               req.Model,
		Messages:            msgs,
		MaxCompletionTokens: int(req.MaxTokens),
		Tools:               modelToolsToOpenAI(req.Tools),
	}

	toolChoice, err := modelToolChoiceToOpenAI(req.ToolChoice)
	if err != nil {
		return openai.ChatCompletionRequest{}, err
	}
	if toolChoice != nil {
		oaiReq.ToolChoice = toolChoice
	}

	if req.Sampling.Temperature != nil {
		oaiReq.Temperature = *req.Sampling.Temperature
	}
	if req.Sampling.TopP != nil {
		oaiReq.TopP = *req.Sampling.TopP
	}

	return oaiReq, nil
}

// extractChatResponse 从 OpenAI 同步响应中提取统一 ChatResponse。
//
// 仅解析第一条 choice，并将 tool calls 转为 llm_core 的 ToolCall 结构。
func extractChatResponse(resp openai.ChatCompletionResponse) (model.ChatResponse, error) {
	if len(resp.Choices) == 0 {
		return model.ChatResponse{}, errors.New("openai chat completion returned no choices")
	}

	msg := resp.Choices[0].Message
	toolCalls := make([]types.ToolCall, 0, len(msg.ToolCalls))
	for _, tc := range msg.ToolCalls {
		if tc.Type != "" && tc.Type != openai.ToolTypeFunction {
			continue
		}
		toolCalls = append(toolCalls, types.ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}

	reasoning, answer := model.SplitLeadingThinkBlock(msg.Content)
	if msg.ReasoningContent != "" {
		reasoning = msg.ReasoningContent
	}

	return model.ChatResponse{
		Content:   answer,
		Reasoning: reasoning,
		ToolCalls: toolCalls,
		Usage: model.TokenUsage{
			PromptTokens:       int64(resp.Usage.PromptTokens),
			CachedPromptTokens: cachedPromptTokens(resp.Usage),
			CompletionTokens:   int64(resp.Usage.CompletionTokens),
			TotalTokens:        int64(resp.Usage.TotalTokens),
		},
	}, nil
}

func cachedPromptTokens(usage openai.Usage) int64 {
	if usage.PromptTokensDetails == nil {
		return 0
	}
	return int64(usage.PromptTokensDetails.CachedTokens)
}

// modelToolsToOpenAI 将内部 Tool 定义映射为 OpenAI 函数工具定义。
func modelToolsToOpenAI(tools []types.Tool) []openai.Tool {
	if len(tools) == 0 {
		return nil
	}

	result := make([]openai.Tool, 0, len(tools))
	for _, tool := range tools {
		result = append(result, openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters: map[string]any{
					"type":       tool.Parameters.Type,
					"properties": tool.Parameters.Properties,
					"required":   tool.Parameters.Required,
				},
			},
		})
	}

	return result
}

// modelToolChoiceToOpenAI 将内部 ToolChoice 映射为 OpenAI 可接受的 tool_choice。
//
// 返回值类型使用 any 是为了兼容 OpenAI 的三种形态：
// 1) "auto" / "none" / "required" 字符串
// 2) 指定函数名的结构体 ToolChoice
// 3) nil（未设置）
func modelToolChoiceToOpenAI(choice types.ToolChoice) (any, error) {
	switch choice.Type {
	case "":
		return nil, nil
	case types.ToolAuto:
		return "auto", nil
	case types.ToolNone:
		return "none", nil
	case types.ToolForce:
		if choice.Name == "" {
			return "required", nil
		}
		return openai.ToolChoice{
			Type: openai.ToolTypeFunction,
			Function: openai.ToolFunction{
				Name: choice.Name,
			},
		}, nil
	default:
		return nil, errors.New("unsupported tool choice type")
	}
}
