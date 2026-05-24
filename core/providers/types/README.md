# Model

LLM 交互的数据模型和接口定义，位于 `core/providers/types` 包（package `model`）。

## 主要内容

- **interface.go** — 定义 `LlmClient` 接口，包含 `Chat(ctx, ChatRequest) (ChatResponse, error)` 和 `ChatStream(ctx, ChatRequest) (Stream, error)` 两个方法，是所有 provider 适配器必须实现的契约。
- **types.go** — 定义核心数据结构：`Message`（统一消息模型，含 ProviderState）、`ChatRequest` / `ChatResponse`、`TokenUsage`、`SamplingParams`、`Attachment`、`ReasoningItem`、`ProviderState` 等。
- **stream.go** — 定义流式接口 `Stream` 及其方法（`Recv`、`RecvEvent`、`FinalMessage`、`Stats`、`ToolCalls`、`ResponseType`、`FinishReason`、`Reasoning`、`Close`）、`StreamEvent` 枚举和 `StreamStats` 统计数据。
- **reasoning.go** — 思考文本处理工具：`SplitLeadingThinkBlock`（拆分 `<think>` 块）、`JoinReasoning`（去重拼接推理文本）、`LeadingThinkStreamSplitter`（流式 `<think>` 标签拆分器）。

## ProviderState 约定

`ProviderState` 是 provider 适配器跨轮恢复上下文的私有载荷。通用运行时只负责持久化和原样回传，不解析具体结构。

- Chat Completions / Gemini 可保存 provider 原生消息或 content 快照，用于后续历史重放。
- OpenAI Responses 适配器保存 response id 和完整 output archive，包含 reasoning、assistant message、function call 等 output item，以便 continuation 时恢复模型侧上下文。
- 当 provider 只留下 refs-only 状态且目标 API 不支持对应引用机制时，适配器应回退到标准 `Message` 内容和 tool call 重放，而不是让 `core/agent` 或 `core/tasks` 感知 provider 专属细节。
