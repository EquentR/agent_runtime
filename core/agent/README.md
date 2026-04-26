# Agent Runtime

`core/agent` 提供 stream-first agent runtime，连接 `providers`、`tools`、`memory`、`tasks`、`prompt`、`skills`、`audit` 等模块，驱动完整的 agent 执行循环。

## 核心组件

### Runner（`types.go`、`stream.go`）

- `Runner` 是运行时编排中枢，持有 LLM client、tool registry、memory manager 和 event sink。
- `RunStream(ctx, input) -> RunStreamResult` 驱动核心 agent loop：模型调用 -> 工具执行 -> 结果回填，循环直到达到停止条件。
- `Run(ctx, input) -> RunResult` 在其上做聚合，返回最终聚合结果、token 用量和成本估算。
- 支持 step 上限（默认 128）、max tokens 限制和工具自动选择。

### Events 与 Sink（`events.go`、`task_adapter.go`）

- `StepEvent`、`ToolEvent`、`LogEvent`：运行时生命周期事件，emit 到 `EventSink` / `StreamEventSink`。
- `RunStreamEvent`：实时流事件（text delta、reasoning delta、tool call delta、usage、completed message）。
- `NewTaskRuntimeSink(runtime)`：将 agent event 桥接到 `core/tasks.Runtime`，支持 step 标记、任务事件写入、metadata 更新和挂起恢复。

### Task Executor（`executor.go`）

- `NewTaskExecutor(deps)` 返回 `coretasks.Executor` 闭包，一条组装路径覆盖：
  1. 从 `ModelCatalog` 解析 provider + model
  2. 从 `ConversationStore` 重载历史消息
  3. 从 `PromptResolver` 构建运行时提示词
  4. 从 `SkillsResolver` 注入选中的 workspace skills
  5. 解析并 hydration 附件引用
  6. 构建 `Runner` 并调用 `RunStream`
  7. 持久化结果消息、memory snapshot、usage 和 cost
  8. 处理 interaction/approval 挂起与恢复
- `ModelCatalog`：序列化所有已配置 provider 与模型的目录供前端使用。
- `RunTaskInput` / `RunTaskResult`：task 输入输出 DTO。

### Conversation Store（`conversation_store.go`）

- `Conversation`：会话元数据（title、last_message、message_count、memory summary/snapshots）。
- `ConversationMessage`：持久化消息（role、content、JSON blob、task_id）。
- 完整 CRUD：创建/查重（`EnsureConversation`）、列表、删除、消息追加、消息查询（可见/回放两种模式）。
- 自动维护 title（截断首条 user message）、last_message 和 message_count。
- Memory 持久化：`SetMemorySummary` / `SetMemorySnapshots` 保存压缩后的记忆状态。

### Memory 集成（`memory.go`、`memory_snapshots.go`、`request_budget.go`）

- 可选的 `memory.Manager`，提供短/长期记忆压缩。
- `MemoryContextSnapshot` / `MemoryCompressionSnapshot`：可序列化的记忆状态快照。
- 请求预算管控（`request_budget.go`）：确保组装后的请求消息不超出模型 token 上限，支持压缩和裁剪两种降级路径。

### 审计与成本（`audit.go`、`cost.go`）

- `executorAuditor`：记录 conversation 加载、消息追加/持久化、错误快照等 executor 层级审计事件。
- Runner 层级审计通过 `events.go` 中的 emit 函数记录 step、tool、interaction、memory 事件。
- 成本估算：基于 model pricing 和 token usage 计算 USD 成本。

### 运行提示词 Artifact（`runtime_prompt_artifact.go`）

- 将运行时提示词构建过程序列化为审计 artifact，记录 prompt segments、请求消息和 phase/source 级别的统计。

## 当前能力汇总

- 完整 agent loop：流式模型调用、工具串行执行、结果回填
- 多轮对话持久化与历史重载
- 短期记忆压缩 + 长期记忆持久化
- 请求 token 预算管控（压缩/裁剪降级）
- 工具审批挂起/恢复 + 人工问答挂起/恢复
- 运行时提示词路由与分发
- Workspace skills 注入（通过 executor）
- 附件 hydration 与 draft 提升
- provider state 无损回放
- task-level event 桥接与 audit 记录
- token usage 聚合与 cost estimation

## 非目标

- RAG / retrieval（由 `core/rag` 预留）
- 多 agent 编排 / child task / fan-out
- streaming orchestration（超过单 thread 的并发编排）

## 典型接入方式

1. 构造 `ExecutorDependencies`（model resolver、conversation store、tool registry、prompt/skills resolvers 等）
2. 调用 `NewTaskExecutor(deps)` 获取 `coretasks.Executor`
3. 将 executor 注册到 `core/tasks.Manager`，通过 task API 触发执行
4. UI 通过 SSE 订阅 task events，实时获取流式消息、工具调用和审批/交互通知

## Conversation + Tasks 模型

1. UI 持有 `conversation_id`
2. 每一轮创建一个新的 `agent.run` task
3. Task input 包含 `conversation_id`、`provider_id`、`model_id`、`message`、`attachments`、`skills`
4. Executor 从 conversation store 重载历史消息
5. Agent 执行完成后，把本轮 user/assistant/tool 消息写回 conversation
6. Tasks 是一次执行单元，conversation 负责跨轮连续对话

## Conversation Read APIs

会话在第一次 `agent.run` 请求且未提供 `conversation_id` 时隐式创建。

| 端点 | 说明 |
|------|------|
| `GET /conversations` | 按最近活跃排序的会话列表 |
| `GET /conversations/:id` | 会话元数据 |
| `GET /conversations/:id/messages` | 按时间顺序的历史消息 |
| `DELETE /conversations/:id` | 删除会话及所有消息 |

会话展示字段自动维护：`title`（截断首条 user message）、`last_message`（最后非空消息摘要）、`message_count`。

## 与 tasks 的关系

`NewTaskRuntimeSink(...)` 将 step/tool/log/stream 事件桥接到 `tasks.Runtime`，适配层留在 `core/agent`，避免 `core/tasks` 反向依赖 `agent`。
