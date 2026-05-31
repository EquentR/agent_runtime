package openai_chat

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/EquentR/agent_runtime/core/providers/tools"
	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

type Client struct {
	client         *openai.Client
	requestTimeout time.Duration
}

func NewOpenAIChatClient(apiKey, baseURL string, requestTimeout time.Duration) *Client {
	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if strings.TrimSpace(baseURL) != "" {
		opts = append(opts, option.WithBaseURL(strings.TrimSpace(baseURL)))
	}
	if requestTimeout > 0 {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.ResponseHeaderTimeout = requestTimeout
		opts = append(opts, option.WithHTTPClient(&http.Client{Transport: transport}))
	}
	client := openai.NewClient(opts...)
	return &Client{client: &client, requestTimeout: requestTimeout}
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

func (c *Client) ChatStream(ctx context.Context, req model.ChatRequest) (model.Stream, error) {
	start := time.Now()
	streamCtx, cancel := context.WithCancel(ctx)

	params, promptMessages, err := buildChatCompletionParams(req)
	if err != nil {
		cancel()
		return nil, err
	}

	remote := c.client.Chat.Completions.NewStreaming(streamCtx, params)
	if remote == nil {
		cancel()
		return nil, errors.New("openai chat stream is nil")
	}

	events := make(chan model.StreamEvent)
	s := &chatStream{
		ctx:    streamCtx,
		cancel: cancel,
		remote: remote,
		events: events,
		stats:  &model.StreamStats{ResponseType: model.StreamResponseUnknown},
		start:  start,
	}

	asyncCounter, err := tools.NewCl100kAsyncTokenCounter()
	if err != nil {
		asyncCounter, _ = tools.NewAsyncTokenCounter(tools.CountModeRune, "")
	}
	if asyncCounter != nil {
		asyncCounter.SetPromptCount(int64(asyncCounter.CountPromptMessages(promptMessages)))
	}

	go func() {
		defer close(events)
		defer remote.Close()
		if asyncCounter != nil {
			defer asyncCounter.Close()
		}

		acc := newStreamToolCallAccumulator()
		splitter := model.NewLeadingThinkStreamSplitter()
		var rawContent strings.Builder
		var visibleContent strings.Builder
		var reasoning strings.Builder
		var refusal strings.Builder
		finishReason := ""
		seenCompletionChoice := false

		var firstEventOnce sync.Once
		firstEventArrived := make(chan struct{})
		closeFirstEvent := func() {
			firstEventOnce.Do(func() {
				close(firstEventArrived)
			})
		}
		if c.requestTimeout > 0 {
			timer := time.NewTimer(c.requestTimeout)
			defer func() {
				closeFirstEvent()
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
			}()
			go func() {
				select {
				case <-timer.C:
					s.setStreamError(context.DeadlineExceeded)
					cancel()
				case <-firstEventArrived:
				case <-streamCtx.Done():
				}
			}()
		}
		markFirstEvent := closeFirstEvent

		defer func() {
			if pending := splitter.Finalize(); pending != "" && streamCtx.Err() == nil && s.streamError() == nil {
				visibleContent.WriteString(pending)
				if !emitStreamEvent(streamCtx, events, model.StreamEvent{Type: model.StreamEventTextDelta, Text: pending}) {
					return
				}
			}
			finalReasoning := strings.TrimSpace(reasoning.String())
			if finalReasoning == "" {
				finalReasoning = splitter.Reasoning()
			}
			s.setReasoning(finalReasoning)
			toolCalls := acc.ToolCalls()
			s.setToolCalls(toolCalls)
			s.statsMu.Lock()
			s.stats.TotalLatency = time.Since(start)
			s.stats.ResponseType = resolveStreamResponseType(finishReason, toolCalls)
			if s.stats.FinishReason == "" {
				s.stats.FinishReason = finishReason
			}
			if asyncCounter != nil {
				s.stats.LocalTokenCount = asyncCounter.FinallyCalc()
				if s.stats.Usage.TotalTokens == 0 {
					s.stats.Usage.PromptTokens = asyncCounter.GetPromptCount()
					s.stats.Usage.CompletionTokens = s.stats.LocalTokenCount
					s.stats.Usage.TotalTokens = asyncCounter.GetTotalCount()
				}
			}
			s.statsMu.Unlock()
			if streamCtx.Err() == nil && s.streamError() == nil {
				if !seenCompletionChoice {
					s.setStreamError(errors.New("openai chat stream ended with no completion chunks"))
					return
				}
				state := chatMessageStateFromParts(rawContent.String(), finalReasoning, refusal.String(), toolCalls)
				final, err := finalAssistantMessageFromState(state)
				if err != nil {
					s.setStreamError(err)
					return
				}
				if final.Content == "" {
					final.Content = visibleContent.String()
				}
				s.setFinalMessage(final)
				emitStreamEvent(streamCtx, events, model.StreamEvent{Type: model.StreamEventCompleted, Message: final})
			}
		}()

		for remote.Next() {
			markFirstEvent()
			if streamCtx.Err() != nil {
				return
			}
			chunk := remote.Current()
			if chunk.Usage.TotalTokens > 0 || chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0 {
				usage := toModelUsage(chunk.Usage)
				s.statsMu.Lock()
				s.stats.Usage = usage
				s.statsMu.Unlock()
				if !emitStreamEvent(streamCtx, events, model.StreamEvent{Type: model.StreamEventUsage, Usage: usage}) {
					return
				}
			}
			for _, choice := range chunk.Choices {
				seenCompletionChoice = true
				if choice.FinishReason != "" {
					finishReason = string(choice.FinishReason)
					s.statsMu.Lock()
					s.stats.FinishReason = finishReason
					s.statsMu.Unlock()
				}
				if deltaReasoning := reasoningDeltaFromRaw(choice.Delta.RawJSON()); deltaReasoning != "" {
					reasoning.WriteString(deltaReasoning)
					if asyncCounter != nil {
						asyncCounter.Append(deltaReasoning)
					}
					s.firstTok.Do(func() {
						s.statsMu.Lock()
						s.stats.TTFT = time.Since(start)
						s.statsMu.Unlock()
					})
					if !emitStreamEvent(streamCtx, events, model.StreamEvent{Type: model.StreamEventReasoningDelta, Reasoning: deltaReasoning}) {
						return
					}
				}
				if choice.Delta.Refusal != "" {
					refusal.WriteString(choice.Delta.Refusal)
				}
				for _, toolCallDelta := range choice.Delta.ToolCalls {
					call := acc.Append(toolCallDelta)
					if !emitStreamEvent(streamCtx, events, model.StreamEvent{Type: model.StreamEventToolCallDelta, ToolCall: call}) {
						return
					}
				}
				if choice.Delta.Content == "" {
					continue
				}
				rawContent.WriteString(choice.Delta.Content)
				if asyncCounter != nil {
					asyncCounter.Append(choice.Delta.Content)
				}
				s.firstTok.Do(func() {
					s.statsMu.Lock()
					s.stats.TTFT = time.Since(start)
					s.statsMu.Unlock()
				})
				if emit := splitter.Consume(choice.Delta.Content); emit != "" {
					visibleContent.WriteString(emit)
					if !emitStreamEvent(streamCtx, events, model.StreamEvent{Type: model.StreamEventTextDelta, Text: emit}) {
						return
					}
				}
			}
		}
		if err := remote.Err(); err != nil && !errors.Is(err, io.EOF) {
			s.setStreamError(err)
		}
	}()

	return s, nil
}

func emitStreamEvent(ctx context.Context, events chan<- model.StreamEvent, event model.StreamEvent) bool {
	select {
	case <-ctx.Done():
		return false
	case events <- event:
		return true
	}
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
	resp := model.ChatResponse{Message: message, Usage: stats.Usage, Latency: latency}
	resp.SyncFieldsFromMessage()
	return resp, nil
}
