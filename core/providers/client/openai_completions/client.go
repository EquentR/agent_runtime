package openai

import (
	"context"
	"strings"
	"time"

	"github.com/EquentR/agent_runtime/core/providers/tools"
	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/sashabaranov/go-openai"
)

type Client struct {
	client *openai.Client
}

func NewOpenAiCompletionsClient(baseUrl, apiKey string) *Client {
	cfg := openai.DefaultConfig(apiKey)
	if baseUrl != "" {
		cfg.BaseURL = baseUrl
	}
	return &Client{
		client: openai.NewClientWithConfig(cfg),
	}
}

func (c *Client) Chat(ctx context.Context, req model.ChatRequest) (model.ChatResponse, error) {
	start := time.Now()
	stream, err := c.ChatStream(ctx, req)
	if err != nil {
		return model.ChatResponse{}, err
	}
	defer stream.Close()

	var contentBuilder strings.Builder
	for {
		chunk, err := stream.Recv()
		if err != nil {
			return model.ChatResponse{}, err
		}
		if chunk == "" {
			break
		}
		contentBuilder.WriteString(chunk)
	}

	stats := stream.Stats()
	latency := stats.TotalLatency
	if latency == 0 {
		latency = time.Since(start)
	}

	return model.ChatResponse{
		Content:   contentBuilder.String(),
		Reasoning: stream.Reasoning(),
		ToolCalls: stream.ToolCalls(),
		Usage:     stats.Usage,
		Latency:   latency,
	}, nil
}

func (c *Client) ChatStream(ctx context.Context, req model.ChatRequest) (model.Stream, error) {
	start := time.Now()
	streamCtx, cancel := context.WithCancel(ctx)

	oaiReq, promptMessages, err := buildChatCompletionStreamRequest(req)
	if err != nil {
		cancel()
		return nil, err
	}

	resp, err := c.client.CreateChatCompletionStream(streamCtx, oaiReq)
	if err != nil {
		cancel()
		return nil, err
	}
	ch := make(chan string)

	s := &openAIStream{
		ctx:       streamCtx,
		cancel:    cancel,
		ch:        ch,
		stats:     &model.StreamStats{ResponseType: model.StreamResponseUnknown},
		startTime: start,
	}

	// 初始化token计数器（使用tokenizer模式）
	asyncCounter, err := tools.NewCl100kAsyncTokenCounter()
	if err != nil {
		// 降级到rune模式
		asyncCounter, _ = tools.NewAsyncTokenCounter(tools.CountModeRune, "")
	}
	s.asyncTokenCounter = asyncCounter

	promptTokens := asyncCounter.CountPromptMessages(promptMessages)
	asyncCounter.SetPromptCount(int64(promptTokens))

	go func() {
		defer close(ch)
		defer resp.Close()

		toolCallAccumulator := newStreamToolCallAccumulator()
		splitter := model.NewLeadingThinkStreamSplitter()
		// 新版兼容接口可能把 reasoning content 与正文分开下发，
		// 这里单独累积，避免只依赖 <think> 切分导致信息丢失。
		var reasoningBuilder strings.Builder
		defer func() {
			if pending := splitter.Finalize(); pending != "" {
				ch <- pending
			}
			reasoning := strings.TrimSpace(reasoningBuilder.String())
			if reasoning == "" {
				reasoning = splitter.Reasoning()
			}
			s.reasoning = reasoning
			s.toolCalls = toolCallAccumulator.ToolCalls()
			s.stats.ResponseType = resolveStreamResponseType(s.stats.FinishReason, s.toolCalls)
		}()

		for {
			select {
			case <-streamCtx.Done():
				// 在退出前进行最终计数
				if s.asyncTokenCounter != nil {
					s.stats.LocalTokenCount = s.asyncTokenCounter.FinallyCalc()
					// 填充Usage数据（如果API未返回）
					if s.stats.Usage.TotalTokens == 0 {
						s.stats.Usage.PromptTokens = s.asyncTokenCounter.GetPromptCount()
						s.stats.Usage.CompletionTokens = s.stats.LocalTokenCount
						s.stats.Usage.TotalTokens = s.asyncTokenCounter.GetTotalCount()
					}
				}
				return
			default:
				chunk, err := resp.Recv()
				if err != nil {
					// stream 结束，进行最终计数
					s.stats.TotalLatency = time.Since(s.startTime)
					if s.asyncTokenCounter != nil {
						s.stats.LocalTokenCount = s.asyncTokenCounter.FinallyCalc()
						// 填充Usage数据（如果API未返回）
						if s.stats.Usage.TotalTokens == 0 {
							s.stats.Usage.PromptTokens = s.asyncTokenCounter.GetPromptCount()
							s.stats.Usage.CompletionTokens = s.stats.LocalTokenCount
							s.stats.Usage.TotalTokens = s.asyncTokenCounter.GetTotalCount()
						}
					}
					return
				}

				// 记录首token延迟
				if len(chunk.Choices) > 0 {
					choice := chunk.Choices[0]
					if choice.FinishReason != "" && choice.FinishReason != openai.FinishReasonNull {
						s.stats.FinishReason = string(choice.FinishReason)
					}

					reasoningDelta := choice.Delta.ReasoningContent
					if reasoningDelta != "" {
						s.firstTok.Do(func() {
							s.stats.TTFT = time.Since(s.startTime)
						})
						if s.asyncTokenCounter != nil {
							s.asyncTokenCounter.Append(reasoningDelta)
						}
						reasoningBuilder.WriteString(reasoningDelta)
					}

					if len(choice.Delta.ToolCalls) > 0 {
						toolCallAccumulator.Append(choice.Delta.ToolCalls)
					}

					delta := choice.Delta.Content
					if delta != "" {
						s.firstTok.Do(func() {
							s.stats.TTFT = time.Since(s.startTime)
						})
						if s.asyncTokenCounter != nil {
							s.asyncTokenCounter.Append(delta)
						}
						if emit := splitter.Consume(delta); emit != "" {
							ch <- emit
						}
					}
				}

				// 处理usage数据（如果API返回）
				if chunk.Usage != nil && chunk.Usage.TotalTokens > 0 {
					s.stats.Usage.PromptTokens = int64(chunk.Usage.PromptTokens)
					s.stats.Usage.CachedPromptTokens = cachedPromptTokens(*chunk.Usage)
					s.stats.Usage.CompletionTokens = int64(chunk.Usage.CompletionTokens)
					s.stats.Usage.TotalTokens = int64(chunk.Usage.TotalTokens)
				}
			}
		}
	}()

	return s, nil
}
