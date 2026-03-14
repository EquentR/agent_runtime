package model

import (
	"context"
	"time"

	"github.com/EquentR/agent_runtime/core/types"
)

type Stream interface {
	// Recv 返回下一段内容
	// content == "" 且 err == nil 表示暂时无数据
	Recv() (content string, err error)

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
