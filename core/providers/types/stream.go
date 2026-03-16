package model

import (
	"context"
	"time"

	"github.com/EquentR/agent_runtime/core/types"
)

type StreamEventType string

const (
	// StreamEventTextDelta carries incremental assistant-visible text in Text.
	// Other payload fields are zero values.
	StreamEventTextDelta StreamEventType = "text_delta"
	// StreamEventReasoningDelta carries incremental provider reasoning text in Reasoning.
	// Other payload fields are zero values.
	StreamEventReasoningDelta StreamEventType = "reasoning_delta"
	// StreamEventToolCallDelta carries one incremental/assembled tool call payload in ToolCall.
	// Providers may emit repeated events for the same logical tool call as arguments accumulate.
	// Other payload fields are zero values.
	StreamEventToolCallDelta StreamEventType = "tool_call_delta"
	// StreamEventUsage carries updated token accounting in Usage.
	// Other payload fields are zero values.
	StreamEventUsage StreamEventType = "usage"
	// StreamEventCompleted carries the final replayable assistant message in Message.
	// Implementations should emit it only for a normal completed stream, not partial/aborted output.
	// Other payload fields are zero values unless a provider intentionally duplicates final state.
	StreamEventCompleted StreamEventType = "completed"
)

type StreamEvent struct {
	Type StreamEventType
	// Text is set only for StreamEventTextDelta.
	Text string
	// Reasoning is set only for StreamEventReasoningDelta.
	Reasoning string
	// ToolCall is set only for StreamEventToolCallDelta.
	ToolCall types.ToolCall
	// Usage is set only for StreamEventUsage.
	Usage TokenUsage
	// Message is set only for StreamEventCompleted.
	Message Message
}

type StreamResult struct {
	// Message is the final replayable assistant message assembled from the stream.
	Message Message
	// Stats is the terminal stream accounting snapshot.
	Stats StreamStats
}

type Stream interface {
	// Recv 返回下一段内容
	// content == "" 且 err == nil 表示暂时无数据
	Recv() (content string, err error)

	// RecvEvent returns the next structured stream event.
	// When the stream ends normally, it returns a zero-value event and nil.
	RecvEvent() (StreamEvent, error)

	// FinalMessage returns the final replayable assistant message for a normally completed stream.
	// Aborted/canceled/failed streams must return a non-nil error and no partial final message.
	FinalMessage() (Message, error)

	// Close 主动中断（如用户关闭页面）
	Close() error

	// Context 关联生命周期
	Context() context.Context

	// Stats 统计数据
	Stats() *StreamStats

	// ToolCalls 返回本次回复中的工具调用（仅在 tool call 场景有值）
	ToolCalls() []types.ToolCall

	// ResponseType 标识本次回复类型（文本或工具调用）
	ResponseType() StreamResponseType

	// FinishReason 返回模型返回的结束原因（如 stop/tool_calls/length）
	FinishReason() string

	// Reasoning 返回本次回复累计的思考/推理文本。
	Reasoning() string
}

type StreamResponseType string

const (
	StreamResponseUnknown  StreamResponseType = "unknown"
	StreamResponseText     StreamResponseType = "text"
	StreamResponseToolCall StreamResponseType = "tool_call"
)

type StreamStats struct {
	Usage           TokenUsage
	TTFT            time.Duration
	TotalLatency    time.Duration
	LocalTokenCount int64 // 本地计数的completion tokens
	FinishReason    string
	ResponseType    StreamResponseType
}
