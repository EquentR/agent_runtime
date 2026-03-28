package openai

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/EquentR/agent_runtime/core/providers/tools"
	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/types"
	"github.com/sashabaranov/go-openai"
)

type openAIStream struct {
	ctx               context.Context
	cancel            context.CancelFunc
	events            <-chan model.StreamEvent
	stats             *model.StreamStats
	startTime         time.Time
	firstTok          sync.Once
	asyncTokenCounter *tools.AsyncTokenCounter // 异步token计数器
	toolCalls         []types.ToolCall
	reasoning         string
	finalMu           sync.RWMutex
	finalMessage      model.Message
	completed         bool
	errMu             sync.RWMutex
	err               error
}

func (s *openAIStream) Recv() (string, error) {
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

func (s *openAIStream) RecvEvent() (model.StreamEvent, error) {
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

func (s *openAIStream) FinalMessage() (model.Message, error) {
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

func (s *openAIStream) Close() error {
	if !s.isCompleted() {
		s.cancel()
	}
	if s.asyncTokenCounter != nil {
		s.asyncTokenCounter.Close()
	}
	return nil
}

func (s *openAIStream) Context() context.Context {
	return s.ctx
}

func (s *openAIStream) Stats() *model.StreamStats {
	return s.stats
}

func (s *openAIStream) ToolCalls() []types.ToolCall {
	if len(s.toolCalls) == 0 {
		return nil
	}
	out := make([]types.ToolCall, len(s.toolCalls))
	copy(out, s.toolCalls)
	return out
}

func (s *openAIStream) ResponseType() model.StreamResponseType {
	return s.stats.ResponseType
}

func (s *openAIStream) FinishReason() string {
	return s.stats.FinishReason
}

func (s *openAIStream) Reasoning() string {
	return s.reasoning
}

func (s *openAIStream) setStreamError(err error) {
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

func (s *openAIStream) streamError() error {
	s.errMu.RLock()
	defer s.errMu.RUnlock()
	return s.err
}

func (s *openAIStream) setFinalMessage(msg model.Message) {
	s.finalMu.Lock()
	defer s.finalMu.Unlock()
	s.finalMessage = model.ChatResponse{Message: msg}.Message
	s.completed = true
}

func (s *openAIStream) final() model.Message {
	s.finalMu.RLock()
	defer s.finalMu.RUnlock()
	return s.finalMessage
}

func (s *openAIStream) isCompleted() bool {
	s.finalMu.RLock()
	defer s.finalMu.RUnlock()
	return s.completed
}

type streamToolCallAccumulator struct {
	calls map[int]types.ToolCall
}

func newStreamToolCallAccumulator() *streamToolCallAccumulator {
	return &streamToolCallAccumulator{
		calls: make(map[int]types.ToolCall),
	}
}

func (a *streamToolCallAccumulator) Append(toolCalls []openai.ToolCall) []types.ToolCall {
	if len(toolCalls) == 0 {
		return nil
	}

	updated := make([]types.ToolCall, 0, len(toolCalls))
	for _, tc := range toolCalls {
		// OpenAI streaming tool call 会按 index 拆成多个 delta，需要逐块拼接。
		idx := len(a.calls)
		if tc.Index != nil {
			idx = *tc.Index
		}

		current := a.calls[idx]
		if tc.ID != "" {
			current.ID = tc.ID
		}
		if tc.Function.Name != "" {
			current.Name = tc.Function.Name
		}
		if tc.Function.Arguments != "" {
			current.Arguments += tc.Function.Arguments
		}
		a.calls[idx] = current
		updated = append(updated, current)
	}
	return updated
}

func (a *streamToolCallAccumulator) ToolCalls() []types.ToolCall {
	if len(a.calls) == 0 {
		return nil
	}
	indexes := make([]int, 0, len(a.calls))
	for idx := range a.calls {
		indexes = append(indexes, idx)
	}
	sort.Ints(indexes)

	out := make([]types.ToolCall, 0, len(indexes))
	for _, idx := range indexes {
		out = append(out, a.calls[idx])
	}
	return out
}

type openAIToolCallAccumulator struct {
	calls map[int]openai.ToolCall
}

func newOpenAIToolCallAccumulator() *openAIToolCallAccumulator {
	return &openAIToolCallAccumulator{calls: make(map[int]openai.ToolCall)}
}

func (a *openAIToolCallAccumulator) Append(toolCalls []openai.ToolCall) {
	for _, tc := range toolCalls {
		idx := len(a.calls)
		if tc.Index != nil {
			idx = *tc.Index
		}

		current := a.calls[idx]
		if tc.ID != "" {
			current.ID = tc.ID
		}
		if tc.Type != "" {
			current.Type = tc.Type
		}
		if tc.Function.Name != "" {
			current.Function.Name = tc.Function.Name
		}
		if tc.Function.Arguments != "" {
			current.Function.Arguments += tc.Function.Arguments
		}
		a.calls[idx] = current
	}
}

func (a *openAIToolCallAccumulator) ToolCalls() []openai.ToolCall {
	if len(a.calls) == 0 {
		return nil
	}
	indexes := make([]int, 0, len(a.calls))
	for idx := range a.calls {
		indexes = append(indexes, idx)
	}
	sort.Ints(indexes)

	out := make([]openai.ToolCall, 0, len(indexes))
	for _, idx := range indexes {
		call := a.calls[idx]
		call.Index = nil
		out = append(out, call)
	}
	return out
}

func appendFunctionCall(current *openai.FunctionCall, delta *openai.FunctionCall) *openai.FunctionCall {
	if delta == nil {
		return current
	}
	if current == nil {
		current = &openai.FunctionCall{}
	}
	if delta.Name != "" {
		current.Name = delta.Name
	}
	if delta.Arguments != "" {
		current.Arguments += delta.Arguments
	}
	return current
}

func resolveStreamResponseType(finishReason string, toolCalls []types.ToolCall) model.StreamResponseType {
	if strings.EqualFold(finishReason, "tool_calls") || len(toolCalls) > 0 {
		return model.StreamResponseToolCall
	}
	if finishReason != "" {
		return model.StreamResponseText
	}
	return model.StreamResponseUnknown
}
