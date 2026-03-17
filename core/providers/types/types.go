package model

import (
	"encoding/json"
	"time"

	"github.com/EquentR/agent_runtime/core/types"
)

const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

type Message struct {
	Role    string
	Content string
	// Reasoning 保存 provider 单独返回的思考文本；某些后端在后续 tool turn
	// 需要把这段内容原样回放，才能继续同一条推理链。
	Reasoning string
	// ReasoningItems 保存结构化推理片段（如 Responses API 的 reasoning item），
	// 便于后续请求按 provider 要求回放完整推理状态。
	ReasoningItems []ReasoningItem
	// Attachments supports image/text files for multimodal requests.
	Attachments []Attachment

	// assistant 发起的 Tool 调用
	ToolCalls []types.ToolCall
	// tool 执行结果id
	ToolCallId string
	// ProviderState 保存 provider 原生消息载荷，供后续同 provider 无损回放。
	ProviderState *ProviderState
}

type ProviderState struct {
	Provider   string
	Format     string
	Version    string
	ResponseID string
	Payload    json.RawMessage
}

type Attachment struct {
	FileName string
	MimeType string
	Data     []byte
}

type ReasoningItem struct {
	ID               string
	Summary          []ReasoningSummary
	EncryptedContent string
}

type ReasoningSummary struct {
	Text string
}

type ChatRequest struct {
	Model     string
	Messages  []Message
	MaxTokens int64

	Sampling SamplingParams

	// Tool 相关
	Tools      []types.Tool
	ToolChoice types.ToolChoice

	TraceID string // 非模型参数，但很关键
}

type ChatResponse struct {
	// Message is the authoritative normalized assistant message for new code.
	// Providers should prefer populating Message first, then call SyncFieldsFromMessage
	// to mirror legacy flattened fields during the transition period.
	Message Message

	// Content/Reasoning/ReasoningItems/ToolCalls are legacy convenience mirrors kept for
	// compatibility with existing callers. They should be derived from Message when both exist.
	Content string
	// Reasoning 是后端单独暴露出来的思考文本。
	Reasoning string
	// ReasoningItems 是后端返回的结构化推理数据，供上层存档、展示和回放。
	ReasoningItems []ReasoningItem
	// ToolCalls carries assistant tool invocation requests in non-stream responses.
	ToolCalls []types.ToolCall

	Usage   TokenUsage
	Latency time.Duration
}

// SyncFieldsFromMessage copies the authoritative Message payload into the legacy flattened fields.
// Prefer this direction in provider implementations.
func (r *ChatResponse) SyncFieldsFromMessage() {
	r.Content = r.Message.Content
	r.Reasoning = r.Message.Reasoning
	r.ReasoningItems = cloneReasoningItems(r.Message.ReasoningItems)
	r.ToolCalls = cloneToolCalls(r.Message.ToolCalls)
}

// SyncMessageFromFields builds Message from legacy flattened fields.
// Use only as a compatibility bridge when a caller still constructs ChatResponse via flat fields.
func (r *ChatResponse) SyncMessageFromFields() {
	r.Message.Role = RoleAssistant
	r.Message.Content = r.Content
	r.Message.Reasoning = r.Reasoning
	r.Message.ReasoningItems = cloneReasoningItems(r.ReasoningItems)
	r.Message.ToolCalls = cloneToolCalls(r.ToolCalls)
}

type TokenUsage struct {
	PromptTokens       int64
	CachedPromptTokens int64
	CompletionTokens   int64
	TotalTokens        int64
}

type SamplingParams struct {
	Temperature *float32
	TopP        *float32
	TopK        *int
}

func (sp *SamplingParams) SetTemperature(val float32) {
	sp.Temperature = &val
}

func (sp *SamplingParams) SetTopP(val float32) {
	sp.TopP = &val
}

func (sp *SamplingParams) SetTopK(val int) {
	sp.TopK = &val
}

func cloneMessages(messages []Message) []Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]Message, len(messages))
	for i, message := range messages {
		out[i] = cloneMessage(message)
	}
	return out
}

func cloneMessage(message Message) Message {
	message.ReasoningItems = cloneReasoningItems(message.ReasoningItems)
	message.Attachments = cloneAttachments(message.Attachments)
	message.ToolCalls = cloneToolCalls(message.ToolCalls)
	message.ProviderState = cloneProviderState(message.ProviderState)
	return message
}

func cloneReasoningItems(items []ReasoningItem) []ReasoningItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]ReasoningItem, len(items))
	for i, item := range items {
		out[i] = item
		if len(item.Summary) > 0 {
			out[i].Summary = append([]ReasoningSummary(nil), item.Summary...)
		}
	}
	return out
}

func cloneAttachments(attachments []Attachment) []Attachment {
	if len(attachments) == 0 {
		return nil
	}
	out := make([]Attachment, len(attachments))
	for i, attachment := range attachments {
		out[i] = attachment
		if len(attachment.Data) > 0 {
			out[i].Data = append([]byte(nil), attachment.Data...)
		}
	}
	return out
}

func cloneToolCalls(toolCalls []types.ToolCall) []types.ToolCall {
	if len(toolCalls) == 0 {
		return nil
	}
	out := make([]types.ToolCall, len(toolCalls))
	for i, toolCall := range toolCalls {
		out[i] = toolCall
		if len(toolCall.ThoughtSignature) > 0 {
			out[i].ThoughtSignature = append([]byte(nil), toolCall.ThoughtSignature...)
		}
	}
	return out
}

func cloneProviderState(state *ProviderState) *ProviderState {
	if state == nil {
		return nil
	}
	cloned := *state
	if len(state.Payload) > 0 {
		cloned.Payload = append(json.RawMessage(nil), state.Payload...)
	}
	return &cloned
}
