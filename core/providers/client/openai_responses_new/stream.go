package openai_responses_new

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/types"
	"github.com/openai/openai-go/v3/responses"
)

type streamToolCallAccumulator struct {
	mu           sync.Mutex
	byCallID     map[string]types.ToolCall
	order        []string
	itemIDToCall map[string]string
}

func (a *streamToolCallAccumulator) ToolCallByItemID(itemID string) (types.ToolCall, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	callID := a.itemIDToCall[itemID]
	if callID == "" {
		return types.ToolCall{}, false
	}
	call, ok := a.byCallID[callID]
	return call, ok
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
	if item.Arguments.OfString != "" {
		current.Arguments = item.Arguments.OfString
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
	outputItems *[]responses.ResponseOutputItemUnion,
	reasoningItems map[int64]model.ReasoningItem,
	stats *model.StreamStats,
	firstTok *sync.Once,
	start time.Time,
	splitter *model.LeadingThinkStreamSplitter,
	reasoning *strings.Builder,
	observeRaw func(string),
	emitText func(string),
	emitEvent func(model.StreamEvent),
	setResponseID func(string),
	setErr func(error),
) {
	switch event.Type {
	case "response.output_item.added":
		setOutputItem(outputItems, event.OutputIndex, event.Item)
		if event.Item.Type == "reasoning" {
			setReasoningItem(reasoningItems, event.OutputIndex, responseReasoningItemToModel(event.Item))
			for _, summary := range event.Item.Summary {
				reasoning.WriteString(summary.Text)
				emitEvent(model.StreamEvent{Type: model.StreamEventReasoningDelta, Reasoning: summary.Text})
			}
		}
		acc.AddOutputItem(event.Item)
		if event.Item.Type == "function_call" {
			if call, ok := acc.ToolCallByItemID(event.Item.ID); ok {
				emitEvent(model.StreamEvent{Type: model.StreamEventToolCallDelta, ToolCall: call})
			}
		}
	case "response.function_call_arguments.delta":
		acc.AppendArgumentsDeltaByItemID(event.ItemID, event.Delta)
		appendOutputItemFunctionArgumentsDelta(outputItems, event.ItemID, event.Delta)
		if call, ok := acc.ToolCallByItemID(event.ItemID); ok {
			emitEvent(model.StreamEvent{Type: model.StreamEventToolCallDelta, ToolCall: call})
		}
	case "response.function_call_arguments.done":
		acc.SetArgumentsByItemID(event.ItemID, event.Arguments)
		setOutputItemFunctionArguments(outputItems, event.ItemID, event.Arguments)
		if call, ok := acc.ToolCallByItemID(event.ItemID); ok {
			emitEvent(model.StreamEvent{Type: model.StreamEventToolCallDelta, ToolCall: call})
		}
	case "response.reasoning_summary_text.delta", "response.reasoning_summary.delta":
		observeRaw(event.Delta)
		reasoning.WriteString(event.Delta)
		appendOutputItemReasoningDelta(outputItems, event.OutputIndex, event.Delta)
		appendReasoningItemDelta(reasoningItems, event.OutputIndex, event.Delta)
		emitEvent(model.StreamEvent{Type: model.StreamEventReasoningDelta, Reasoning: event.Delta})
	case "response.output_text.delta":
		delta := event.Delta
		if delta == "" {
			return
		}
		observeRaw(delta)
		appendOutputItemTextDelta(outputItems, event.OutputIndex, event.ContentIndex, delta)
		firstTok.Do(func() {
			stats.TTFT = time.Since(start)
		})
		if emit := splitter.Consume(delta); emit != "" {
			emitText(emit)
		}
	case "response.completed":
		if setResponseID != nil {
			setResponseID(event.Response.ID)
		}
		stats.Usage = toModelUsage(event.Response.Usage)
		stats.FinishReason = streamFinishReasonFromResponse(event.Response, acc.ToolCalls())
		emitEvent(model.StreamEvent{Type: model.StreamEventUsage, Usage: stats.Usage})
	case "response.incomplete":
		if setResponseID != nil {
			setResponseID(event.Response.ID)
		}
		stats.Usage = toModelUsage(event.Response.Usage)
		stats.FinishReason = streamFinishReasonFromResponse(event.Response, acc.ToolCalls())
		emitEvent(model.StreamEvent{Type: model.StreamEventUsage, Usage: stats.Usage})
	case "response.failed":
		if setResponseID != nil {
			setResponseID(event.Response.ID)
		}
		stats.Usage = toModelUsage(event.Response.Usage)
		stats.FinishReason = streamFinishReasonFromResponse(event.Response, acc.ToolCalls())
		emitEvent(model.StreamEvent{Type: model.StreamEventUsage, Usage: stats.Usage})
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

func setOutputItem(items *[]responses.ResponseOutputItemUnion, outputIndex int64, item responses.ResponseOutputItemUnion) {
	slot := ensureOutputItemSlot(items, outputIndex)
	if slot == nil {
		return
	}
	*slot = cloneOutputItem(item)
}

func setReasoningItem(items map[int64]model.ReasoningItem, outputIndex int64, item model.ReasoningItem) {
	if items == nil || outputIndex < 0 {
		return
	}
	items[outputIndex] = item
}

func appendReasoningItemDelta(items map[int64]model.ReasoningItem, outputIndex int64, delta string) {
	if items == nil || delta == "" || outputIndex < 0 {
		return
	}
	item := items[outputIndex]
	if len(item.Summary) == 0 {
		item.Summary = append(item.Summary, model.ReasoningSummary{})
	}
	item.Summary[len(item.Summary)-1].Text += delta
	items[outputIndex] = item
}

func compactReasoningItemsByOutputIndex(items map[int64]model.ReasoningItem) []model.ReasoningItem {
	if len(items) == 0 {
		return nil
	}
	indexes := make([]int, 0, len(items))
	for index := range items {
		indexes = append(indexes, int(index))
	}
	sort.Ints(indexes)
	out := make([]model.ReasoningItem, 0, len(indexes))
	for _, index := range indexes {
		out = append(out, items[int64(index)])
	}
	return out
}

func cloneOutputItem(item responses.ResponseOutputItemUnion) responses.ResponseOutputItemUnion {
	cloned := responses.ResponseOutputItemUnion{}
	if raw, err := json.Marshal(item); err == nil {
		if err := json.Unmarshal(raw, &cloned); err == nil {
			return cloned
		}
	}
	return item
}

func ensureOutputItemSlot(items *[]responses.ResponseOutputItemUnion, outputIndex int64) *responses.ResponseOutputItemUnion {
	if items == nil || outputIndex < 0 {
		return nil
	}
	for int(outputIndex) >= len(*items) {
		*items = append(*items, responses.ResponseOutputItemUnion{})
	}
	return &(*items)[outputIndex]
}

func appendOutputItemTextDelta(items *[]responses.ResponseOutputItemUnion, outputIndex, contentIndex int64, delta string) {
	if items == nil || delta == "" || outputIndex < 0 {
		return
	}
	if int(outputIndex) >= len(*items) {
		return
	}
	item := &(*items)[outputIndex]
	if item.Type != "message" || contentIndex < 0 {
		return
	}
	for int(contentIndex) >= len(item.Content) {
		item.Content = append(item.Content, responses.ResponseOutputMessageContentUnion{Type: "output_text"})
	}
	item.Content[contentIndex].Type = "output_text"
	item.Content[contentIndex].Text += delta
}

func appendOutputItemReasoningDelta(items *[]responses.ResponseOutputItemUnion, outputIndex int64, delta string) {
	if items == nil || delta == "" || outputIndex < 0 {
		return
	}
	if int(outputIndex) >= len(*items) {
		return
	}
	item := &(*items)[outputIndex]
	if item.Type != "reasoning" {
		return
	}
	if len(item.Summary) == 0 {
		item.Summary = append(item.Summary, responses.ResponseReasoningItemSummary{})
	}
	item.Summary[len(item.Summary)-1].Text += delta
}

func appendOutputItemFunctionArgumentsDelta(items *[]responses.ResponseOutputItemUnion, itemID, delta string) {
	if items == nil || itemID == "" || delta == "" {
		return
	}
	for i := range *items {
		if (*items)[i].ID == itemID && (*items)[i].Type == "function_call" {
			(*items)[i].Arguments = responses.ResponseOutputItemUnionArguments{OfString: (*items)[i].Arguments.OfString + delta}
			return
		}
	}
}

func setOutputItemFunctionArguments(items *[]responses.ResponseOutputItemUnion, itemID, args string) {
	if items == nil || itemID == "" {
		return
	}
	for i := range *items {
		if (*items)[i].ID == itemID && (*items)[i].Type == "function_call" {
			(*items)[i].Arguments = responses.ResponseOutputItemUnionArguments{OfString: args}
			return
		}
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
