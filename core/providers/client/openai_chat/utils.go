package openai_chat

import (
	"fmt"
	"strings"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/types"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

func buildChatCompletionParams(req model.ChatRequest) (openai.ChatCompletionNewParams, []string, error) {
	messages, promptMessages, err := buildOpenAIChatMessages(req.Messages)
	if err != nil {
		return openai.ChatCompletionNewParams{}, nil, err
	}

	params := openai.ChatCompletionNewParams{
		Model:    shared.ChatModel(req.Model),
		Messages: messages,
		StreamOptions: openai.ChatCompletionStreamOptionsParam{
			IncludeUsage: openai.Bool(true),
		},
		Tools: modelToolsToOpenAIChat(req.Tools),
	}
	if req.MaxTokens > 0 {
		params.MaxCompletionTokens = openai.Int(req.MaxTokens)
	}
	if key := strings.TrimSpace(req.PromptCacheKey); key != "" {
		params.PromptCacheKey = openai.String(key)
	}
	if retention := strings.TrimSpace(req.PromptCacheRetention); retention != "" {
		params.PromptCacheRetention = openai.ChatCompletionNewParamsPromptCacheRetention(retention)
	}
	if req.Sampling.Temperature != nil {
		params.Temperature = openai.Float(float64(*req.Sampling.Temperature))
	}
	if req.Sampling.TopP != nil {
		params.TopP = openai.Float(float64(*req.Sampling.TopP))
	}

	toolChoice, err := modelToolChoiceToOpenAIChat(req.ToolChoice)
	if err != nil {
		return openai.ChatCompletionNewParams{}, nil, err
	}
	if toolChoice != nil {
		params.ToolChoice = *toolChoice
	}

	return params, promptMessages, nil
}

func modelToolsToOpenAIChat(tools []types.Tool) []openai.ChatCompletionToolUnionParam {
	if len(tools) == 0 {
		return nil
	}
	result := make([]openai.ChatCompletionToolUnionParam, 0, len(tools))
	for _, tool := range tools {
		parameters := shared.FunctionParameters{
			"type":       tool.Parameters.Type,
			"properties": tool.Parameters.Properties,
			"required":   tool.Parameters.Required,
		}
		result = append(result, openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
			Name:        tool.Name,
			Description: openai.String(tool.Description),
			Parameters:  parameters,
		}))
	}
	return result
}

func modelToolChoiceToOpenAIChat(choice types.ToolChoice) (*openai.ChatCompletionToolChoiceOptionUnionParam, error) {
	switch choice.Type {
	case "":
		return nil, nil
	case types.ToolAuto:
		value := openai.ChatCompletionToolChoiceOptionUnionParam{OfAuto: openai.String(string(openai.ChatCompletionToolChoiceOptionAutoAuto))}
		return &value, nil
	case types.ToolNone:
		value := openai.ChatCompletionToolChoiceOptionUnionParam{OfAuto: openai.String(string(openai.ChatCompletionToolChoiceOptionAutoNone))}
		return &value, nil
	case types.ToolForce:
		if strings.TrimSpace(choice.Name) == "" {
			value := openai.ChatCompletionToolChoiceOptionUnionParam{OfAuto: openai.String(string(openai.ChatCompletionToolChoiceOptionAutoRequired))}
			return &value, nil
		}
		value := openai.ToolChoiceOptionFunctionToolChoice(openai.ChatCompletionNamedToolChoiceFunctionParam{Name: strings.TrimSpace(choice.Name)})
		return &value, nil
	default:
		return nil, fmt.Errorf("unsupported tool choice type: %s", choice.Type)
	}
}

func toModelUsage(usage openai.CompletionUsage) model.TokenUsage {
	return model.TokenUsage{
		PromptTokens:       usage.PromptTokens,
		CachedPromptTokens: usage.PromptTokensDetails.CachedTokens,
		CompletionTokens:   usage.CompletionTokens,
		TotalTokens:        usage.TotalTokens,
	}
}
