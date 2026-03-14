package model

import (
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
