package google

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/types"
	genai "google.golang.org/genai"
)

type streamToolCallAccumulator struct {
	mu    sync.Mutex
	calls map[string]types.ToolCall
	order []string
}

// newStreamToolCallAccumulator 创建一个“稳定顺序”的函数调用累加器。
//
// 与 OpenAI 不同，GenAI 的函数调用通常以完整对象 part 形式出现；
// 这里仍做增量聚合，兼容多 chunk 重复更新场景。
func newStreamToolCallAccumulator() *streamToolCallAccumulator {
	return &streamToolCallAccumulator{calls: make(map[string]types.ToolCall)}
}

// Append 记录当前 chunk 中观察到的函数调用 part。
//
// 优先使用 call ID 作为唯一键；若缺失，则回退到 chunk 内索引，
// 保证结果顺序可预测。
func (a *streamToolCallAccumulator) Append(parts []*genai.Part) {
	a.mu.Lock()
	defer a.mu.Unlock()

	for idx, part := range parts {
		if part == nil || part.FunctionCall == nil {
			continue
		}
		fc := part.FunctionCall
		key := fc.ID
		if key == "" {
			key = "idx:" + strconv.Itoa(idx)
		}

		current, exists := a.calls[key]
		if !exists {
			a.order = append(a.order, key)
		}
		if fc.ID != "" {
			current.ID = fc.ID
		}
		if fc.Name != "" {
			current.Name = fc.Name
		}
		if fc.Args != nil {
			args, err := json.Marshal(fc.Args)
			if err == nil {
				current.Arguments = string(args)
			}
		}
		if len(part.ThoughtSignature) > 0 {
			current.ThoughtSignature = append([]byte(nil), part.ThoughtSignature...)
		}
		a.calls[key] = current
	}
}

func (a *streamToolCallAccumulator) ToolCalls() []types.ToolCall {
	a.mu.Lock()
	defer a.mu.Unlock()

	if len(a.calls) == 0 {
		return nil
	}

	keys := make([]string, len(a.order))
	copy(keys, a.order)
	if len(keys) == 0 {
		keys = make([]string, 0, len(a.calls))
		for key := range a.calls {
			keys = append(keys, key)
		}
		sort.Strings(keys)
	}

	out := make([]types.ToolCall, 0, len(keys))
	for _, key := range keys {
		out = append(out, a.calls[key])
	}
	return out
}

func resolveStreamResponseType(finishReason string, toolCalls []types.ToolCall) model.StreamResponseType {
	// 与 openai 适配层保持一致：
	// 一旦存在 tool calls，优先判定为工具调用响应。
	if len(toolCalls) > 0 {
		return model.StreamResponseToolCall
	}
	if finishReason != "" {
		return model.StreamResponseText
	}
	return model.StreamResponseUnknown
}

// normalizeFinishReason 将 provider 枚举值转换为 llm_core 侧使用的小写字符串。
func normalizeFinishReason(reason genai.FinishReason) string {
	if reason == "" || reason == genai.FinishReasonUnspecified {
		return ""
	}
	return strings.ToLower(string(reason))
}

type genAIStream struct {
	ctx       context.Context
	cancel    context.CancelFunc
	ch        <-chan string
	stats     *model.StreamStats
	startTime time.Time
	firstTok  sync.Once
	toolCalls []types.ToolCall
	reasoning string

	errMu sync.RWMutex
	err   error
}

func (s *genAIStream) setStreamError(err error) {
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

func (s *genAIStream) streamError() error {
	s.errMu.RLock()
	defer s.errMu.RUnlock()
	return s.err
}

// Recv 从内部桥接通道读取下一段文本。
//
// 与 llm_core Stream 契约一致：
// - ("", nil) 表示流结束
// - 非 nil error 表示上下文取消/超时
func (s *genAIStream) Recv() (string, error) {
	select {
	case <-s.ctx.Done():
		if err := s.streamError(); err != nil {
			return "", err
		}
		return "", s.ctx.Err()
	case msg, ok := <-s.ch:
		if !ok {
			if err := s.streamError(); err != nil {
				if errors.Is(err, io.EOF) {
					return "", nil
				}
				return "", err
			}
			return "", nil
		}
		return msg, nil
	}
}

func (s *genAIStream) Close() error {
	// Close 为协作式关闭：取消上下文，触发生产协程自然退出。
	s.cancel()
	return nil
}

func (s *genAIStream) Context() context.Context { return s.ctx }

func (s *genAIStream) Stats() *model.StreamStats { return s.stats }

func (s *genAIStream) ToolCalls() []types.ToolCall {
	if len(s.toolCalls) == 0 {
		return nil
	}
	out := make([]types.ToolCall, len(s.toolCalls))
	copy(out, s.toolCalls)
	return out
}

func (s *genAIStream) ResponseType() model.StreamResponseType { return s.stats.ResponseType }

func (s *genAIStream) FinishReason() string { return s.stats.FinishReason }

func (s *genAIStream) Reasoning() string { return s.reasoning }
