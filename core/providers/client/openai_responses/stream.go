package openai_official

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/types"
	"github.com/openai/openai-go/responses"
)

type streamToolCallAccumulator struct {
	mu           sync.Mutex
	byCallID     map[string]types.ToolCall
	order        []string
	itemIDToCall map[string]string
}

func newStreamToolCallAccumulator() *streamToolCallAccumulator {
	return &streamToolCallAccumulator{
		byCallID:     make(map[string]types.ToolCall),
		itemIDToCall: make(map[string]string),
	}
}

func (a *streamToolCallAccumulator) AddOutputItem(item responses.ResponseOutputItemUnion) {
	if item.Type != "function_call" {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	callID := strings.TrimSpace(item.CallID)
	if callID == "" {
		callID = strings.TrimSpace(item.ID)
	}
	if callID == "" {
		return
	}

	current, ok := a.byCallID[callID]
	if !ok {
		a.order = append(a.order, callID)
	}
	current.ID = callID
	if item.Name != "" {
		current.Name = item.Name
	}
	if item.Arguments != "" {
		current.Arguments = item.Arguments
	}
	a.byCallID[callID] = current

	if item.ID != "" {
		a.itemIDToCall[item.ID] = callID
	}
}

func (a *streamToolCallAccumulator) AppendArgumentsDelta(callID, delta string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	callID = strings.TrimSpace(callID)
	if callID == "" {
		return
	}
	current, ok := a.byCallID[callID]
	if !ok {
		a.order = append(a.order, callID)
		current = types.ToolCall{ID: callID}
	}
	current.Arguments += delta
	a.byCallID[callID] = current
}

func (a *streamToolCallAccumulator) AppendArgumentsDeltaByItemID(itemID, delta string) {
	a.mu.Lock()
	callID := a.itemIDToCall[itemID]
	a.mu.Unlock()
	a.AppendArgumentsDelta(callID, delta)
}

func (a *streamToolCallAccumulator) SetArgumentsByItemID(itemID, args string) {
	a.mu.Lock()
	callID := a.itemIDToCall[itemID]
	a.mu.Unlock()

	if callID == "" {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	current := a.byCallID[callID]
	current.Arguments = args
	a.byCallID[callID] = current
}

func (a *streamToolCallAccumulator) ToolCalls() []types.ToolCall {
	a.mu.Lock()
	defer a.mu.Unlock()

	if len(a.byCallID) == 0 {
		return nil
	}

	keys := make([]string, len(a.order))
	copy(keys, a.order)
	if len(keys) == 0 {
		keys = make([]string, 0, len(a.byCallID))
		for k := range a.byCallID {
			keys = append(keys, k)
		}
		sort.Strings(keys)
	}

	out := make([]types.ToolCall, 0, len(keys))
	for _, key := range keys {
		out = append(out, a.byCallID[key])
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

func applyStreamEvent(
	event responses.ResponseStreamEventUnion,
	acc *streamToolCallAccumulator,
	stats *model.StreamStats,
	firstTok *sync.Once,
	start time.Time,
	splitter *model.LeadingThinkStreamSplitter,
	reasoning *strings.Builder,
	observeRaw func(string),
	emitText func(string),
	setErr func(error),
) {
	switch event.Type {
	case "response.output_item.added":
		if event.Item.Type == "reasoning" {
			for _, summary := range event.Item.Summary {
				reasoning.WriteString(summary.Text)
			}
		}
		acc.AddOutputItem(event.Item)
	case "response.function_call_arguments.delta":
		acc.AppendArgumentsDeltaByItemID(event.ItemID, event.Delta.OfString)
	case "response.function_call_arguments.done":
		acc.SetArgumentsByItemID(event.ItemID, event.Arguments)
	case "response.reasoning_summary_text.delta", "response.reasoning_summary.delta":
		observeRaw(event.Delta.OfString)
		reasoning.WriteString(event.Delta.OfString)
	case "response.output_text.delta":
		delta := event.Delta.OfString
		if delta == "" {
			return
		}
		observeRaw(delta)
		firstTok.Do(func() {
			stats.TTFT = time.Since(start)
		})
		if emit := splitter.Consume(delta); emit != "" {
			emitText(emit)
		}
	case "response.completed":
		stats.Usage = toModelUsage(event.Response.Usage)
		stats.FinishReason = streamFinishReasonFromResponse(event.Response, acc.ToolCalls())
	case "response.incomplete":
		stats.Usage = toModelUsage(event.Response.Usage)
		stats.FinishReason = streamFinishReasonFromResponse(event.Response, acc.ToolCalls())
	case "response.failed":
		stats.Usage = toModelUsage(event.Response.Usage)
		stats.FinishReason = streamFinishReasonFromResponse(event.Response, acc.ToolCalls())
		if event.Response.Error.Message != "" {
			setErr(errors.New(event.Response.Error.Message))
			return
		}
		setErr(errors.New("openai responses stream failed"))
	case "error":
		if event.Message != "" {
			setErr(errors.New(event.Message))
			return
		}
		setErr(errors.New("openai responses stream error"))
	}
}

func streamFinishReasonFromResponse(resp responses.Response, toolCalls []types.ToolCall) string {
	if len(toolCalls) > 0 {
		return "tool_calls"
	}
	if resp.Status == "incomplete" {
		if resp.IncompleteDetails.Reason == "max_output_tokens" {
			return "length"
		}
		if resp.IncompleteDetails.Reason != "" {
			return resp.IncompleteDetails.Reason
		}
	}
	if resp.Status == "failed" {
		return "error"
	}
	if resp.Status != "" {
		return "stop"
	}
	return ""
}
