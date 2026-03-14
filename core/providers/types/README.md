# Model

LLM 交互的数据模型和接口定义。

## 主要内容

- **interface.go** - 定义 `LlmClient` 接口，规范 LLM 客户端的标准行为
- **types.go** - 定义请求响应类型（`ChatRequest`、`ChatResponse`）、消息类型、token 使用统计和采样参数等
- **stream.go** - 定义流式响应接口 `Stream` 和流式统计数据 `StreamStats`，包含 tool call、response type、finish reason 等流式元信息
