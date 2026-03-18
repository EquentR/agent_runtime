package openai_responses

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/EquentR/agent_runtime/core/providers/tools"
	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/types"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/ssestream"
	"github.com/openai/openai-go/v3/responses"
)

type responseAPI interface {
	New(ctx context.Context, body responses.ResponseNewParams, opts ...option.RequestOption) (*responses.Response, error)
	NewStreaming(ctx context.Context, body responses.ResponseNewParams, opts ...option.RequestOption) *ssestream.Stream[responses.ResponseStreamEventUnion]
}

type Client struct {
	api responseAPI
	cli *openai.Client
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
	return &Client{api: &cli.Responses, cli: &cli}
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

	events := make(chan model.StreamEvent)
	s := &responseStream{
		ctx:       streamCtx,
		cancel:    cancel,
		remote:    remote,
		events:    events,
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
		defer close(events)
		defer remote.Close()

		acc := newStreamToolCallAccumulator()
		splitter := model.NewLeadingThinkStreamSplitter()
		outputItems := make([]responses.ResponseOutputItemUnion, 0)
		responseID := ""
		var reasoningBuilder strings.Builder
		var contentBuilder strings.Builder
		reasoningItemsByOutputIndex := make(map[int64]model.ReasoningItem)
		defer func() {
			if pending := splitter.Finalize(); pending != "" {
				contentBuilder.WriteString(pending)
				events <- model.StreamEvent{Type: model.StreamEventTextDelta, Text: pending}
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
			if streamCtx.Err() == nil && s.streamError() == nil {
				state, err := providerStateFromOutputItems(responseID, outputItems)
				if err != nil {
					s.setStreamError(err)
					return
				}
				final := finalAssistantMessageFromResponse(
					contentBuilder.String(),
					reasoning,
					compactReasoningItemsByOutputIndex(reasoningItemsByOutputIndex),
					s.ToolCalls(),
					state,
				)
				s.setFinalMessage(final)
				events <- model.StreamEvent{Type: model.StreamEventCompleted, Message: final}
			}
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
				&outputItems,
				reasoningItemsByOutputIndex,
				s.stats,
				&s.firstTok,
				s.startTime,
				splitter,
				&reasoningBuilder,
				s.asyncTokenCounter.Append,
				func(delta string) {
					contentBuilder.WriteString(delta)
					events <- model.StreamEvent{Type: model.StreamEventTextDelta, Text: delta}
				},
				func(event model.StreamEvent) {
					events <- event
				},
				func(id string) {
					if strings.TrimSpace(id) != "" {
						responseID = strings.TrimSpace(id)
					}
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

func appendOutputItem(items *[]responses.ResponseOutputItemUnion, item responses.ResponseOutputItemUnion) {
	if items == nil || item.Type == "" {
		return
	}
	cloned := responses.ResponseOutputItemUnion{}
	if raw, err := json.Marshal(item); err == nil {
		if err := json.Unmarshal(raw, &cloned); err == nil {
			*items = append(*items, cloned)
			return
		}
	}
	*items = append(*items, item)
}

func buildOpenAIOfficialPromptMessages(messages []model.Message) ([]responses.ResponseInputParam, []string, error) {
	input, _, err := buildResponseInput(messages, "system")
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
	events <-chan model.StreamEvent

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

	finalMu      sync.RWMutex
	finalMessage model.Message
	completed    bool
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
	for {
		event, err := s.RecvEvent()
		if err != nil {
			return "", err
		}
		if event.Type == "" {
			return "", nil
		}
		if event.Type == model.StreamEventTextDelta {
			return event.Text, nil
		}
	}
}

func (s *responseStream) RecvEvent() (model.StreamEvent, error) {
	select {
	case <-s.ctx.Done():
		if err := s.streamError(); err != nil {
			return model.StreamEvent{}, err
		}
		return model.StreamEvent{}, s.ctx.Err()
	case event, ok := <-s.events:
		if !ok {
			if err := s.streamError(); err != nil {
				return model.StreamEvent{}, err
			}
			return model.StreamEvent{}, nil
		}
		return event, nil
	}
}

func (s *responseStream) FinalMessage() (model.Message, error) {
	if err := s.streamError(); err != nil && !s.isCompleted() {
		return model.Message{}, err
	}
	if err := s.ctx.Err(); err != nil && !s.isCompleted() {
		return model.Message{}, err
	}
	if !s.isCompleted() {
		return model.Message{}, errors.New("stream did not complete normally")
	}
	return s.final(), nil
}

func (s *responseStream) Close() error {
	if !s.isCompleted() {
		s.cancel()
	}
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

func (s *responseStream) setFinalMessage(msg model.Message) {
	s.finalMu.Lock()
	defer s.finalMu.Unlock()
	s.finalMessage = msg
	s.completed = true
}

func (s *responseStream) final() model.Message {
	s.finalMu.RLock()
	defer s.finalMu.RUnlock()
	return s.finalMessage
}

func (s *responseStream) isCompleted() bool {
	s.finalMu.RLock()
	defer s.finalMu.RUnlock()
	return s.completed
}
