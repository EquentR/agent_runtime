package openai

import (
	"context"
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
	ch                <-chan string
	stats             *model.StreamStats
	startTime         time.Time
	firstTok          sync.Once
	asyncTokenCounter *tools.AsyncTokenCounter // 异步token计数器
	toolCalls         []types.ToolCall
	reasoning         string
}

func (s *openAIStream) Recv() (string, error) {
	select {
	case <-s.ctx.Done():
		return "", s.ctx.Err()
	case msg, ok := <-s.ch:
		if !ok {
			return "", nil
		}
		return msg, nil
	}
}

func (s *openAIStream) Close() error {
	s.cancel()
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

type streamToolCallAccumulator struct {
	calls map[int]types.ToolCall
}

func newStreamToolCallAccumulator() *streamToolCallAccumulator {
	return &streamToolCallAccumulator{
		calls: make(map[int]types.ToolCall),
	}
}

func (a *streamToolCallAccumulator) Append(toolCalls []openai.ToolCall) {
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
	}
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

func resolveStreamResponseType(finishReason string, toolCalls []types.ToolCall) model.StreamResponseType {
	if strings.EqualFold(finishReason, "tool_calls") || len(toolCalls) > 0 {
		return model.StreamResponseToolCall
	}
	if finishReason != "" {
		return model.StreamResponseText
	}
	return model.StreamResponseUnknown
}
