package google

import (
	"context"
	"io"
	"strings"
	"time"

	"github.com/EquentR/agent_runtime/core/providers/tools"
	model "github.com/EquentR/agent_runtime/core/providers/types"
	genai "google.golang.org/genai"
)

type Client struct {
	client *genai.Client
}

// NewGoogleGenAIClient 创建基于 google.golang.org/genai 的 Gemini 兼容客户端。
//
// baseURL 为可选项，主要用于网关/代理场景；为空时使用 SDK 默认 Gemini 端点。
func NewGoogleGenAIClient(baseURL, apiKey string) (*Client, error) {
	cfg := &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	}
	if baseURL != "" {
		cfg.HTTPOptions.BaseURL = baseURL
	}

	cli, err := genai.NewClient(context.Background(), cfg)
	if err != nil {
		return nil, err
	}
	return &Client{client: cli}, nil
}

// Chat 与 OpenAI 适配层保持一致：
// 内部通过 ChatStream 聚合为单次 ChatResponse。
//
// 这样上层可以在不同 provider 间切换而无需修改请求/响应处理逻辑。
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

// ChatStream 将统一 ChatRequest 转换为 GenAI GenerateContentStream 调用，
// 并将流式分片适配回 llm_core 的 Stream 接口。
//
// 行为对齐说明：
//   - 在 StreamStats 中填充 TTFT/latency/usage
//   - 增量收集 tool calls，并在流结束后统一暴露
//   - provider 未返回 usage 时，使用本地 token 统计兜底
func (c *Client) ChatStream(ctx context.Context, req model.ChatRequest) (model.Stream, error) {
	start := time.Now()
	streamCtx, cancel := context.WithCancel(ctx)

	contents, cfg, promptMessages, err := buildGenerateContentRequest(req)
	if err != nil {
		cancel()
		return nil, err
	}

	// SDK 流式迭代器；每次迭代返回一个 provider 分片。
	seq := c.client.Models.GenerateContentStream(streamCtx, req.Model, contents, cfg)

	ch := make(chan string)
	s := &genAIStream{
		ctx:       streamCtx,
		cancel:    cancel,
		ch:        ch,
		stats:     &model.StreamStats{ResponseType: model.StreamResponseUnknown},
		startTime: start,
	}

	// 优先使用 tokenizer 计数，保证与其他客户端口径一致。
	// 初始化失败时，降级为 rune 计数。
	asyncCounter, err := tools.NewCl100kAsyncTokenCounter()
	if err != nil {
		asyncCounter, _ = tools.NewAsyncTokenCounter(tools.CountModeRune, "")
	}
	promptTokens := asyncCounter.CountPromptMessages(promptMessages)
	asyncCounter.SetPromptCount(int64(promptTokens))

	go func() {
		defer close(ch)

		toolCallAccumulator := newStreamToolCallAccumulator()
		splitter := model.NewLeadingThinkStreamSplitter()
		var reasoningBuilder strings.Builder
		defer func() {
			if pending := splitter.Finalize(); pending != "" {
				ch <- pending
			}
			s.reasoning = strings.TrimSpace(reasoningBuilder.String())
			if s.reasoning == "" {
				s.reasoning = splitter.Reasoning()
			}
			s.toolCalls = toolCallAccumulator.ToolCalls()
			s.stats.ResponseType = resolveStreamResponseType(s.stats.FinishReason, s.toolCalls)
		}()

		for chunk, err := range seq {
			if streamCtx.Err() != nil {
				break
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				s.setStreamError(err)
				break
			}
			if chunk == nil {
				continue
			}

			s.stats.TotalLatency = time.Since(s.startTime)
			s.stats.Usage = toModelUsage(chunk.UsageMetadata)

			if len(chunk.Candidates) == 0 || chunk.Candidates[0] == nil {
				continue
			}
			candidate := chunk.Candidates[0]

			finishReason := normalizeFinishReason(candidate.FinishReason)
			if finishReason != "" {
				s.stats.FinishReason = finishReason
			}

			if candidate.Content != nil {
				for _, part := range candidate.Content.Parts {
					if part == nil {
						continue
					}
					if part.Text != "" && part.Thought {
						asyncCounter.Append(part.Text)
						reasoningBuilder.WriteString(part.Text)
						continue
					}
					// GenAI 的函数调用通过结构化 part 返回。
					// 这里先累积，最终通过 Stream.ToolCalls() 暴露。
					if part.FunctionCall != nil {
						toolCallAccumulator.Append([]*genai.Part{part})
					}
					// 文本可能分散在多个 part/chunk，逐段转发到消费通道。
					if part.Text != "" {
						s.firstTok.Do(func() {
							s.stats.TTFT = time.Since(s.startTime)
						})
						asyncCounter.Append(part.Text)
						if emit := splitter.Consume(part.Text); emit != "" {
							ch <- emit
						}
					}
				}
			}
		}

		s.stats.TotalLatency = time.Since(s.startTime)
		s.stats.LocalTokenCount = asyncCounter.FinallyCalc()
		if s.stats.Usage.TotalTokens == 0 {
			s.stats.Usage.PromptTokens = asyncCounter.GetPromptCount()
			s.stats.Usage.CompletionTokens = s.stats.LocalTokenCount
			s.stats.Usage.TotalTokens = asyncCounter.GetTotalCount()
		}
		asyncCounter.Close()
	}()

	return s, nil
}
