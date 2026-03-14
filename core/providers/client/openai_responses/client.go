package openai_official

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/EquentR/agent_runtime/core/providers/tools"
	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/types"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/ssestream"
	"github.com/openai/openai-go/responses"
)

type responseAPI interface {
	New(ctx context.Context, body responses.ResponseNewParams, opts ...option.RequestOption) (*responses.Response, error)
	NewStreaming(ctx context.Context, body responses.ResponseNewParams, opts ...option.RequestOption) *ssestream.Stream[responses.ResponseStreamEventUnion]
}

type Client struct {
	api responseAPI
}

func NewOpenAiResponsesClient(apiKey, baseURL string, requestTimeout time.Duration) *Client {
	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	if requestTimeout > 0 {
		opts = append(opts, option.WithRequestTimeout(requestTimeout))
	}
	cli := openai.NewClient(opts...)
	return &Client{api: &cli.Responses}
}

func (c *Client) Chat(ctx context.Context, req model.ChatRequest) (model.ChatResponse, error) {
	start := time.Now()
	params, err := buildResponseRequestParams(req)
	if err != nil {
		return model.ChatResponse{}, err
	}

	resp, err := c.api.New(ctx, params)
	if err != nil {
		return model.ChatResponse{}, err
	}

	out, err := extractChatResponse(resp)
	if err != nil {
		return model.ChatResponse{}, err
	}
	out.Latency = time.Since(start)
	return out, nil
}

func (c *Client) ChatStream(ctx context.Context, req model.ChatRequest) (model.Stream, error) {
	start := time.Now()
	streamCtx, cancel := context.WithCancel(ctx)

	params, err := buildResponseRequestParams(req)
	if err != nil {
		cancel()
		return nil, err
	}

	remote := c.api.NewStreaming(streamCtx, params)
	if remote == nil {
		cancel()
		return nil, errors.New("openai responses stream is nil")
	}

	ch := make(chan string)
	s := &responseStream{
		ctx:       streamCtx,
		cancel:    cancel,
		remote:    remote,
		ch:        ch,
		stats:     &model.StreamStats{ResponseType: model.StreamResponseUnknown},
		startTime: start,
	}

	asyncCounter, err := tools.NewCl100kAsyncTokenCounter()
	if err != nil {
		asyncCounter, _ = tools.NewAsyncTokenCounter(tools.CountModeRune, "")
	}
	s.asyncTokenCounter = asyncCounter

	_, promptMessages, err := buildOpenAIOfficialPromptMessages(req.Messages)
	if err == nil {
		promptTokens := asyncCounter.CountPromptMessages(promptMessages)
		asyncCounter.SetPromptCount(int64(promptTokens))
	}

	go func() {
		defer close(ch)
		defer remote.Close()

		acc := newStreamToolCallAccumulator()
		splitter := model.NewLeadingThinkStreamSplitter()
		var reasoningBuilder strings.Builder
		defer func() {
			if pending := splitter.Finalize(); pending != "" {
				ch <- pending
			}
			reasoning := strings.TrimSpace(reasoningBuilder.String())
			if reasoning == "" {
				reasoning = splitter.Reasoning()
			}
			s.setReasoning(reasoning)
			finalToolCalls := acc.ToolCalls()
			s.setToolCalls(finalToolCalls)

			s.statsMu.Lock()
			s.stats.ResponseType = resolveStreamResponseType(s.stats.FinishReason, finalToolCalls)
			s.stats.TotalLatency = time.Since(s.startTime)
			s.stats.LocalTokenCount = s.asyncTokenCounter.FinallyCalc()
			if s.stats.Usage.TotalTokens == 0 {
				s.stats.Usage.PromptTokens = s.asyncTokenCounter.GetPromptCount()
				s.stats.Usage.CompletionTokens = s.stats.LocalTokenCount
				s.stats.Usage.TotalTokens = s.asyncTokenCounter.GetTotalCount()
			}
			s.statsMu.Unlock()
			s.asyncTokenCounter.Close()
		}()

		for remote.Next() {
			if streamCtx.Err() != nil {
				return
			}
			event := remote.Current()
			s.statsMu.Lock()
			applyStreamEvent(
				event,
				acc,
				s.stats,
				&s.firstTok,
				s.startTime,
				splitter,
				&reasoningBuilder,
				s.asyncTokenCounter.Append,
				func(delta string) {
					ch <- delta
				},
				s.setStreamError,
			)
			s.statsMu.Unlock()
		}

		if err := remote.Err(); err != nil {
			if !errors.Is(err, io.EOF) {
				s.setStreamError(err)
			}
		}
	}()

	return s, nil
}

func buildOpenAIOfficialPromptMessages(messages []model.Message) ([]responses.ResponseInputParam, []string, error) {
	input, err := buildResponseInput(messages)
	if err != nil {
		return nil, nil, err
	}
	promptMessages := make([]string, 0, len(messages))
	for _, m := range messages {
		promptMessages = append(promptMessages, m.Content)
	}
	return []responses.ResponseInputParam{input}, promptMessages, nil
}

type responseStream struct {
	ctx    context.Context
	cancel context.CancelFunc
	remote *ssestream.Stream[responses.ResponseStreamEventUnion]
	ch     <-chan string

	statsMu           sync.RWMutex
	stats             *model.StreamStats
	startTime         time.Time
	firstTok          sync.Once
	asyncTokenCounter *tools.AsyncTokenCounter
	toolCallsMu       sync.RWMutex
	toolCalls         []types.ToolCall
	reasoningMu       sync.RWMutex
	reasoning         string

	errMu sync.RWMutex
	err   error
}

func (s *responseStream) setStreamError(err error) {
	if err == nil {
		return
	}
	s.errMu.Lock()
	defer s.errMu.Unlock()
	if s.err != nil {
		return
	}
	s.err = err
}

func (s *responseStream) streamError() error {
	s.errMu.RLock()
	defer s.errMu.RUnlock()
	return s.err
}

func (s *responseStream) Recv() (string, error) {
	select {
	case <-s.ctx.Done():
		if err := s.streamError(); err != nil {
			return "", err
		}
		return "", s.ctx.Err()
	case msg, ok := <-s.ch:
		if !ok {
			if err := s.streamError(); err != nil {
				return "", err
			}
			return "", nil
		}
		return msg, nil
	}
}

func (s *responseStream) Close() error {
	s.cancel()
	if s.remote != nil {
		_ = s.remote.Close()
	}
	if s.asyncTokenCounter != nil {
		s.asyncTokenCounter.Close()
	}
	return nil
}

func (s *responseStream) Context() context.Context { return s.ctx }

func (s *responseStream) Stats() *model.StreamStats {
	s.statsMu.RLock()
	defer s.statsMu.RUnlock()
	if s.stats == nil {
		return &model.StreamStats{}
	}
	copyStats := *s.stats
	return &copyStats
}

func (s *responseStream) ToolCalls() []types.ToolCall {
	s.toolCallsMu.RLock()
	defer s.toolCallsMu.RUnlock()

	if len(s.toolCalls) == 0 {
		return nil
	}
	out := make([]types.ToolCall, len(s.toolCalls))
	copy(out, s.toolCalls)
	return out
}

func (s *responseStream) ResponseType() model.StreamResponseType {
	s.statsMu.RLock()
	defer s.statsMu.RUnlock()
	if s.stats == nil {
		return model.StreamResponseUnknown
	}
	return s.stats.ResponseType
}

func (s *responseStream) FinishReason() string {
	s.statsMu.RLock()
	defer s.statsMu.RUnlock()
	if s.stats == nil {
		return ""
	}
	return s.stats.FinishReason
}

func (s *responseStream) Reasoning() string {
	s.reasoningMu.RLock()
	defer s.reasoningMu.RUnlock()
	return s.reasoning
}

func (s *responseStream) setToolCalls(calls []types.ToolCall) {
	s.toolCallsMu.Lock()
	defer s.toolCallsMu.Unlock()

	if len(calls) == 0 {
		s.toolCalls = nil
		return
	}

	s.toolCalls = make([]types.ToolCall, len(calls))
	copy(s.toolCalls, calls)
}

func (s *responseStream) setReasoning(reasoning string) {
	s.reasoningMu.Lock()
	defer s.reasoningMu.Unlock()
	s.reasoning = reasoning
}
