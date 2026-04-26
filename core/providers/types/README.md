# Model

LLM 交互的数据模型和接口定义，位于 `core/providers/types` 包（package `model`）。

## 主要内容

- **interface.go** — 定义 `LlmClient` 接口，包含 `Chat(ctx, ChatRequest) (ChatResponse, error)` 和 `ChatStream(ctx, ChatRequest) (Stream, error)` 两个方法，是所有 provider 适配器必须实现的契约。
- **types.go** — 定义核心数据结构：`Message`（统一消息模型，含 ProviderState）、`ChatRequest` / `ChatResponse`、`TokenUsage`、`SamplingParams`、`Attachment`、`ReasoningItem`、`ProviderState` 等。
- **stream.go** — 定义流式接口 `Stream` 及其方法（`Recv`、`RecvEvent`、`FinalMessage`、`Stats`、`ToolCalls`、`ResponseType`、`FinishReason`、`Reasoning`、`Close`）、`StreamEvent` 枚举和 `StreamStats` 统计数据。
- **reasoning.go** — 思考文本处理工具：`SplitLeadingThinkBlock`（拆分 `<think>` 块）、`JoinReasoning`（去重拼接推理文本）、`LeadingThinkStreamSplitter`（流式 `<think>` 标签拆分器）。
