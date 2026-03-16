# Agent MVP

`core/agent` 提供一个最小可复用的 stream-first agent runtime，用来把现有 `providers`、`tools`、`memory`、`tasks` 连接起来，先落地 runtime 核心编排，而不是一次性覆盖完整 product 级 agent 能力。

## 当前能力

- 以 `RunStream` 为主路径的单线程 agent loop，`Run` 基于其做聚合
- 基于 `model.LlmClient` 的流式模型调用与最终 replayable message 收敛
- assistant tool calls 串行执行与 tool result 回填
- short-term memory 接入，调用前组装上下文，调用后写回 assistant/tool 消息
- 支持 `LLMModel` 驱动的 model id、output budget 与 pricing 接入
- run-level usage 聚合与 token-based cost estimation
- 可选 `EventSink` / `StreamEventSink`，可直接桥接 `core/tasks` runtime
- conversation store，可按 `conversation_id` 持久化并重载多轮消息历史
- `agent.run` executor，可让每一轮对话以 task 形式执行并写回 conversation
- tool context 中透传 `step_id`、`actor` 与 metadata

## 明确非目标

- skills system
- RAG / retrieval
- streaming orchestration
- multi-agent / child task
- session persistence / checkpoint recovery
- approval workflow

## 典型接入方式

1. 构造 provider client
2. 准备 `tools.Registry` 与 `registry.List()`
3. 可选挂载 `memory.Manager`
4. 调用 `NewRunner(...)`
5. 使用 `RunStream(...)` 获取实时事件，或用 `Run(...)` 获取聚合结果

## Conversation + Tasks

当前推荐的多轮对话模型是：

1. UI 持有 `conversation_id`
2. 每一轮创建一个新的 `agent.run` task
3. 任务输入包含 `conversation_id`、`provider_id`、`model_id`、`message`
4. executor 从 conversation store 重载历史消息
5. agent 执行完成后，把本轮 user/assistant/tool 消息写回 conversation

这样 `tasks` 仍然是一次执行单元，而 conversation 负责跨轮连续对话。

## Conversation Read APIs

当前不会单独提供“创建空会话”接口。会话会在第一次 `agent.run` 请求且未提供 `conversation_id` 时隐式创建。

为 UI 恢复聊天窗口，当前提供只读接口：

- `GET /conversations` 获取按最近活跃排序的会话列表
- `GET /conversations/:id` 获取会话元数据
- `GET /conversations/:id/messages` 获取按时间顺序的历史消息

会话展示字段会在写入消息时自动维护：

- `title`：默认从首条 user message 截断生成
- `last_message`：最后一条非空消息摘要
- `message_count`：当前累计消息数

## 与 tasks 的关系

当前包提供 `NewTaskRuntimeSink(...)`，可把 step/tool/log/stream 事件桥接到 `tasks.Runtime`。适配层仍放在 `core/agent`，避免 `core/tasks` 反向依赖 `agent`。
