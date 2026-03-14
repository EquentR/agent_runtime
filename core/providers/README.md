# LLM Client

LLM 客户端实现，封装不同服务商的 API 调用。

## openai_completions
OpenAI 兼容接口客户端，支持标准的聊天补全和流式响应功能。可用于 OpenAI、Azure OpenAI 以及其他兼容 OpenAI API 格式的服务。

- 非流式 `Chat` 支持 tools、tool_choice、assistant/tool 消息链路与 tool calls 回传
- 流式 `ChatStream` 同样支持 tools/tool_choice，并可在流结束后通过 `Stream.ToolCalls()` 获取完整 tool calls
- 流式可通过 `Stream.ResponseType()` / `Stream.FinishReason()` 判断本次结果是文本回复还是工具调用
- 非流式 `Chat` 内部已改为基于 `ChatStream` 聚合，外部 `ChatRequest`/`ChatResponse` 结构保持不变

## google
基于 `google.golang.org/genai` 的兼容层客户端，复用统一 `ChatRequest` / `ChatResponse` / `Stream` 接口，支持 Gemini API。

- 支持非流式 `Chat` 与流式 `ChatStream`
- 支持 tools、tool_choice 及 assistant/tool 消息链路转换
- 支持多模态消息（文本 + 图片/文本附件）到 GenAI `Content/Part` 的映射
- 流式结束后可通过 `Stream.ToolCalls()` 获取完整工具调用，并通过 `Stream.ResponseType()` / `Stream.FinishReason()` 判定回复类型

## openai_responses
基于 `github.com/openai/openai-go` 的 Responses API 客户端，复用统一 `ChatRequest` / `ChatResponse` / `Stream` 接口。

- 构造函数：`NewOpenAiResponsesClient(apiKey, baseURL string, requestTimeout time.Duration)`
- `baseURL` 为可选项，用于网关/代理；为空时使用 SDK 默认 OpenAI 端点
- 支持非流式文本/工具调用解析、usage 映射，以及流式文本增量与工具调用参数增量拼接

### Gateway compatibility notes

- OpenAI 官方端点与 `aihubmix` 已验证可正常使用 Responses API。
- `packyapi`（`https://www.packyapi.com/v1`）对 Responses API 仅部分兼容，以下结论来自 2026-03-06 的实测。
- `gpt-5.4` 在 `packyapi` 上要求 `input` 使用标准消息数组形式；`input: "..."` 会返回 `400 bad_response_status_code`。
- `gpt-5.4` 在 `packyapi` 上显式传入 `temperature` 或 `top_p` 会返回 `400 bad_response_status_code`；省略这两个字段可正常返回。
- function tool 在开启 `strict=true` 时仍需补齐 `parameters.additionalProperties=false`，否则会返回 `invalid_function_parameters`。
- `tool_choice` 的命名函数对象形态在 `packyapi` 上会触发服务端 JSON 反序列化错误；字符串形态（如 `auto` / `none` / `required`）可正常工作。
- `packyapi` 当前未暴露兼容的 `/chat/completions` 端点，实测返回 `404`。
