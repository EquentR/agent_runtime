package openai

import (
	"context"
	"errors"
	"io"
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
	return chatResponseFromStream(start, stream)
}

func chatResponseFromStream(start time.Time, stream model.Stream) (model.ChatResponse, error) {
	for {
		chunk, err := stream.Recv()
		if err != nil {
			return model.ChatResponse{}, err
		}
		if chunk == "" {
			break
		}
	}

	message, err := stream.FinalMessage()
	if err != nil {
		return model.ChatResponse{}, err
	}
	stats := stream.Stats()
	latency := stats.TotalLatency
	if latency == 0 {
		latency = time.Since(start)
	}

	resp := model.ChatResponse{
		Message: message,
		Usage:   stats.Usage,
		Latency: latency,
	}
	resp.SyncFieldsFromMessage()
	return resp, nil
}

func emitStreamEvent(ctx context.Context, events chan<- model.StreamEvent, event model.StreamEvent) bool {
	select {
	case <-ctx.Done():
		return false
	case events <- event:
		return true
	}
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
	events := make(chan model.StreamEvent)

	s := &openAIStream{
		ctx:       streamCtx,
		cancel:    cancel,
		events:    events,
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
		defer close(events)
		defer resp.Close()

		toolCallAccumulator := newStreamToolCallAccumulator()
		nativeToolCallAccumulator := newOpenAIToolCallAccumulator()
		splitter := model.NewLeadingThinkStreamSplitter()
		var contentBuilder strings.Builder
		var rawContentBuilder strings.Builder
		var refusalBuilder strings.Builder
		nativeMessage := openai.ChatCompletionMessage{Role: model.RoleAssistant}
		// 新版兼容接口可能把 reasoning content 与正文分开下发，
		// 这里单独累积，避免只依赖 <think> 切分导致信息丢失。
		var reasoningBuilder strings.Builder
		defer func() {
			if pending := splitter.Finalize(); pending != "" && streamCtx.Err() == nil && s.streamError() == nil {
				contentBuilder.WriteString(pending)
				if !emitStreamEvent(streamCtx, events, model.StreamEvent{Type: model.StreamEventTextDelta, Text: pending}) {
					return
				}
			}
			reasoning := strings.TrimSpace(reasoningBuilder.String())
			if reasoning == "" {
				reasoning = splitter.Reasoning()
			}
			s.reasoning = reasoning
			s.toolCalls = toolCallAccumulator.ToolCalls()
			s.stats.ResponseType = resolveStreamResponseType(s.stats.FinishReason, s.toolCalls)
			if streamCtx.Err() == nil && s.streamError() == nil {
				nativeMessage.Content = rawContentBuilder.String()
				nativeMessage.Refusal = refusalBuilder.String()
				nativeMessage.ReasoningContent = strings.TrimSpace(reasoningBuilder.String())
				nativeMessage.ToolCalls = nativeToolCallAccumulator.ToolCalls()
				final, err := finalAssistantMessageFromNativeMessage(nativeMessage)
				if err != nil {
					s.setStreamError(err)
					return
				}
				s.setFinalMessage(final)
				emitStreamEvent(streamCtx, events, model.StreamEvent{Type: model.StreamEventCompleted, Message: final})
			}
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
					if !errors.Is(err, io.EOF) {
						s.setStreamError(err)
					}
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
					if choice.Delta.Role != "" {
						nativeMessage.Role = choice.Delta.Role
					}
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
						if !emitStreamEvent(streamCtx, events, model.StreamEvent{Type: model.StreamEventReasoningDelta, Reasoning: reasoningDelta}) {
							return
						}
					}

					if choice.Delta.Refusal != "" {
						refusalBuilder.WriteString(choice.Delta.Refusal)
					}

					nativeMessage.FunctionCall = appendFunctionCall(nativeMessage.FunctionCall, choice.Delta.FunctionCall)

					if len(choice.Delta.ToolCalls) > 0 {
						toolCallAccumulator.Append(choice.Delta.ToolCalls)
						nativeToolCallAccumulator.Append(choice.Delta.ToolCalls)
						for _, tc := range choice.Delta.ToolCalls {
							assembled := newStreamToolCallAccumulator()
							assembled.Append([]openai.ToolCall{tc})
							calls := assembled.ToolCalls()
							if len(calls) > 0 {
								if !emitStreamEvent(streamCtx, events, model.StreamEvent{Type: model.StreamEventToolCallDelta, ToolCall: calls[0]}) {
									return
								}
							}
						}
					}

					delta := choice.Delta.Content
					if delta != "" {
						s.firstTok.Do(func() {
							s.stats.TTFT = time.Since(s.startTime)
						})
						if s.asyncTokenCounter != nil {
							s.asyncTokenCounter.Append(delta)
						}
						rawContentBuilder.WriteString(delta)
						if emit := splitter.Consume(delta); emit != "" {
							contentBuilder.WriteString(emit)
							if !emitStreamEvent(streamCtx, events, model.StreamEvent{Type: model.StreamEventTextDelta, Text: emit}) {
								return
							}
						}
					}
				}

				// 处理usage数据（如果API返回）
				if chunk.Usage != nil && chunk.Usage.TotalTokens > 0 {
					s.stats.Usage.PromptTokens = int64(chunk.Usage.PromptTokens)
					s.stats.Usage.CachedPromptTokens = cachedPromptTokens(*chunk.Usage)
					s.stats.Usage.CompletionTokens = int64(chunk.Usage.CompletionTokens)
					s.stats.Usage.TotalTokens = int64(chunk.Usage.TotalTokens)
					if !emitStreamEvent(streamCtx, events, model.StreamEvent{Type: model.StreamEventUsage, Usage: s.stats.Usage}) {
						return
					}
				}
			}
		}
	}()

	return s, nil
}
