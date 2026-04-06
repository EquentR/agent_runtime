package memory

import (
	"context"
	"fmt"
	"strings"

	model "github.com/EquentR/agent_runtime/core/providers/types"
)

type ChatClient interface {
	Chat(ctx context.Context, req model.ChatRequest) (model.ChatResponse, error)
}

type LLMCompressorOptions struct {
	Client modelChatClient
	Model  string
}

type modelChatClient interface {
	Chat(ctx context.Context, req model.ChatRequest) (model.ChatResponse, error)
}

func NewLLMShortTermCompressor(options LLMCompressorOptions) Compressor {
	client := options.Client
	modelName := strings.TrimSpace(options.Model)
	return func(ctx context.Context, request CompressionRequest) (string, error) {
		if client == nil {
			return "", fmt.Errorf("memory chat client is required")
		}
		resp, err := client.Chat(ctx, model.ChatRequest{
			Model:     modelName,
			MaxTokens: request.MaxSummaryTokens,
			Messages: []model.Message{
				{Role: model.RoleSystem, Content: strings.TrimSpace(request.Instruction)},
				{Role: model.RoleUser, Content: renderShortTermCompressionPrompt(request)},
			},
		})
		if err != nil {
			return "", err
		}
		content := strings.TrimSpace(resp.Message.Content)
		if content == "" {
			content = strings.TrimSpace(resp.Content)
		}
		if content == "" {
			return "", fmt.Errorf("memory compressor returned empty summary")
		}
		return content, nil
	}
}

func NewLLMLongTermCompressor(options LLMCompressorOptions) LongTermCompressor {
	client := options.Client
	modelName := strings.TrimSpace(options.Model)
	return func(ctx context.Context, request LongTermCompressionRequest) (string, error) {
		if client == nil {
			return "", fmt.Errorf("memory chat client is required")
		}
		resp, err := client.Chat(ctx, model.ChatRequest{
			Model: modelName,
			Messages: []model.Message{
				{Role: model.RoleSystem, Content: strings.TrimSpace(request.Instruction)},
				{Role: model.RoleUser, Content: renderLongTermCompressionPrompt(request)},
			},
		})
		if err != nil {
			return "", err
		}
		content := strings.TrimSpace(resp.Message.Content)
		if content == "" {
			content = strings.TrimSpace(resp.Content)
		}
		if content == "" {
			return "", fmt.Errorf("memory compressor returned empty summary")
		}
		return content, nil
	}
}

func renderShortTermCompressionPrompt(request CompressionRequest) string {
	var builder strings.Builder
	builder.WriteString("请压缩以下短期上下文，生成供后续继续工作的记忆摘要。")
	if request.TargetSummaryTokens > 0 {
		builder.WriteString(fmt.Sprintf(" 目标长度尽量控制在 %d tokens 以内，可略微超出，但不要明显超出。", request.TargetSummaryTokens))
	}
	builder.WriteString("\n\n")
	if summary := strings.TrimSpace(request.PreviousSummary); summary != "" {
		builder.WriteString("[Previous Summary]\n")
		builder.WriteString(summary)
		builder.WriteString("\n\n")
	}
	builder.WriteString("[Messages To Compress]\n")
	builder.WriteString(renderMessagesForCompression(request.Messages))
	return builder.String()
}

func renderLongTermCompressionPrompt(request LongTermCompressionRequest) string {
	var builder strings.Builder
	builder.WriteString("请基于以下用户历史信息更新长期记忆摘要。\n")
	builder.WriteString("[User ID]\n")
	builder.WriteString(strings.TrimSpace(request.UserID))
	builder.WriteString("\n\n")
	if summary := strings.TrimSpace(request.PreviousSummary); summary != "" {
		builder.WriteString("[Current Long-Term Summary]\n")
		builder.WriteString(summary)
		builder.WriteString("\n\n")
	}
	builder.WriteString("[Loop Messages]\n")
	builder.WriteString(renderMessagesForCompression(request.Messages))
	return builder.String()
}

func renderMessagesForCompression(messages []model.Message) string {
	parts := make([]string, 0, len(messages))
	for _, message := range messages {
		if compact := summarizeMessage(message); compact != "" {
			parts = append(parts, compact)
		}
	}
	if len(parts) == 0 {
		return "(empty)"
	}
	return strings.Join(parts, "\n")
}
