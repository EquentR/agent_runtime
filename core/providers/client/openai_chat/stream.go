package openai_chat

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/types"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/ssestream"
)

type streamToolCallAccumulator struct {
	mu    sync.Mutex
	calls map[int64]types.ToolCall
	order []int64
}

func newStreamToolCallAccumulator() *streamToolCallAccumulator {
	return &streamToolCallAccumulator{calls: make(map[int64]types.ToolCall)}
}

func (a *streamToolCallAccumulator) Append(delta openai.ChatCompletionChunkChoiceDeltaToolCall) types.ToolCall {
	a.mu.Lock()
	defer a.mu.Unlock()

	index := delta.Index
	current, ok := a.calls[index]
	if !ok {
		a.order = append(a.order, index)
	}
	if delta.ID != "" {
		current.ID = delta.ID
	}
	if delta.Function.Name != "" {
		current.Name = delta.Function.Name
	}
	if delta.Function.Arguments != "" {
		current.Arguments += delta.Function.Arguments
	}
	a.calls[index] = current
	return current
}

func (a *streamToolCallAccumulator) ToolCalls() []types.ToolCall {
	a.mu.Lock()
	defer a.mu.Unlock()

	if len(a.calls) == 0 {
		return nil
	}
	out := make([]types.ToolCall, 0, len(a.order))
	for _, index := range a.order {
		out = append(out, a.calls[index])
	}
	return out
}

type chatStream struct {
	ctx    context.Context
	cancel context.CancelFunc
	remote *ssestream.Stream[openai.ChatCompletionChunk]
	events <-chan model.StreamEvent

	statsMu  sync.RWMutex
	stats    *model.StreamStats
	start    time.Time
	firstTok sync.Once

	errMu sync.RWMutex
	err   error

	finalMu      sync.RWMutex
	finalMessage model.Message
	completed    bool

	toolCallsMu sync.RWMutex
	toolCalls   []types.ToolCall
	reasoningMu sync.RWMutex
	reasoning   string
}

func (s *chatStream) Recv() (string, error) {
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

func (s *chatStream) RecvEvent() (model.StreamEvent, error) {
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

func (s *chatStream) FinalMessage() (model.Message, error) {
	if err := s.streamError(); err != nil && !s.isCompleted() {
		return model.Message{}, err
	}
	if err := s.ctx.Err(); err != nil && !s.isCompleted() {
		return model.Message{}, err
	}
	if !s.isCompleted() {
		return model.Message{}, errors.New("openai chat stream final message is unavailable")
	}
	return s.final(), nil
}

func (s *chatStream) Close() error {
	if s.cancel != nil && !s.isCompleted() {
		s.cancel()
	}
	if s.remote != nil {
		return s.remote.Close()
	}
	return nil
}

func (s *chatStream) Context() context.Context {
	return s.ctx
}

func (s *chatStream) Stats() *model.StreamStats {
	s.statsMu.RLock()
	defer s.statsMu.RUnlock()
	cloned := *s.stats
	return &cloned
}

func (s *chatStream) ToolCalls() []types.ToolCall {
	s.toolCallsMu.RLock()
	defer s.toolCallsMu.RUnlock()
	return append([]types.ToolCall(nil), s.toolCalls...)
}

func (s *chatStream) ResponseType() model.StreamResponseType {
	return s.Stats().ResponseType
}

func (s *chatStream) FinishReason() string {
	return s.Stats().FinishReason
}

func (s *chatStream) Reasoning() string {
	s.reasoningMu.RLock()
	defer s.reasoningMu.RUnlock()
	return s.reasoning
}

func (s *chatStream) setStreamError(err error) {
	if err == nil {
		return
	}
	s.errMu.Lock()
	defer s.errMu.Unlock()
	if s.err == nil {
		s.err = err
	}
}

func (s *chatStream) streamError() error {
	s.errMu.RLock()
	defer s.errMu.RUnlock()
	return s.err
}

func (s *chatStream) setFinalMessage(message model.Message) {
	s.finalMu.Lock()
	defer s.finalMu.Unlock()
	s.finalMessage = cloneMessage(message)
	s.completed = true
}

func (s *chatStream) final() model.Message {
	s.finalMu.RLock()
	defer s.finalMu.RUnlock()
	return cloneMessage(s.finalMessage)
}

func (s *chatStream) isCompleted() bool {
	s.finalMu.RLock()
	defer s.finalMu.RUnlock()
	return s.completed
}

func (s *chatStream) setToolCalls(toolCalls []types.ToolCall) {
	s.toolCallsMu.Lock()
	defer s.toolCallsMu.Unlock()
	s.toolCalls = append([]types.ToolCall(nil), toolCalls...)
}

func (s *chatStream) setReasoning(reasoning string) {
	s.reasoningMu.Lock()
	defer s.reasoningMu.Unlock()
	s.reasoning = strings.TrimSpace(reasoning)
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

func reasoningDeltaFromRaw(raw string) string {
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return ""
	}
	value, _ := payload["reasoning_content"].(string)
	return value
}

func cloneMessage(message model.Message) model.Message {
	cloned := message
	if len(message.ToolCalls) > 0 {
		cloned.ToolCalls = append([]types.ToolCall(nil), message.ToolCalls...)
	}
	if len(message.Attachments) > 0 {
		cloned.Attachments = append([]model.Attachment(nil), message.Attachments...)
	}
	if len(message.ReasoningItems) > 0 {
		cloned.ReasoningItems = append([]model.ReasoningItem(nil), message.ReasoningItems...)
	}
	cloned.ProviderState = cloneProviderState(message.ProviderState)
	return cloned
}
