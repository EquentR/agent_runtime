# LLM Client

LLM 客户端实现，封装不同服务商的 API 调用。所有 provider 实现统一的 `model.LlmClient` 接口（`Chat` + `ChatStream`），`Chat` 方法内部委托给 `ChatStream` 做流式聚合。

## 目录结构

```
core/providers/
├── types/          package model       — 统一数据模型与接口
├── tools/          package tools       — 本地 token 计数器
└── client/
    ├── openai_completions/  package openai              — OpenAI Chat Completions API
    ├── openai_responses/    package openai_responses    — OpenAI Responses API
    └── google/              package google              — Google Gemini (GenAI)
```

## openai_completions
OpenAI 兼容接口客户端，支持标准的聊天补全和流式响应功能。可用于 OpenAI、Azure OpenAI 以及其他兼容 OpenAI API 格式的服务。

- SDK: `github.com/sashabaranov/go-openai`
- 非流式 `Chat` 支持 tools、tool_choice、assistant/tool 消息链路与 tool calls 回传
- 流式 `ChatStream` 同样支持 tools/tool_choice，并可在流结束后通过 `Stream.ToolCalls()` 获取完整 tool calls
- 流式可通过 `Stream.ResponseType()` / `Stream.FinishReason()` 判断结果类型
- ProviderState 格式：`openai_completions` / `openai_chat_message.v1`

## google
基于 `google.golang.org/genai` 的兼容层客户端，实现 Gemini API 适配。

- SDK: `google.golang.org/genai`
- 支持非流式 `Chat` 与流式 `ChatStream`
- 支持 tools、tool_choice 及 assistant/tool 消息链路转换
- 支持多模态消息（文本 + 图片/文本附件）到 GenAI `Content/Part` 的映射
- 推理文本通过 `part.Thought=true` 标记识别
- ProviderState 格式：`google_genai` / `google_genai_content.v1`

## openai_responses
基于 `github.com/openai/openai-go/v3` 的 Responses API 客户端。

- SDK: `github.com/openai/openai-go/v3`
- 支持非流式文本/工具调用解析、usage 映射
- 流式 SSE 事件驱动处理，支持 tool call 参数增量拼接
- 模型感知配置：根据 model ID（o1/o3/o4/gpt-5/codex-/oss）自动选择 system message 模式
- ProviderState 格式：`openai_responses` / `openai_response_state.v1`

### Gateway 兼容注意事项

以下结论来自 2026-03-06 的实测（`packyapi`）：
- `gpt-5.4` 在 `packyapi` 上要求 `input` 使用标准消息数组形式；`input: "..."` 会返回 `400`
- `gpt-5.4` 在 `packyapi` 上显式传入 `temperature` 或 `top_p` 会返回 `400`
- function tool 在 `strict=true` 时需补齐 `parameters.additionalProperties=false`
- `tool_choice` 的命名函数对象形态在 `packyapi` 上会触发反序列化错误
