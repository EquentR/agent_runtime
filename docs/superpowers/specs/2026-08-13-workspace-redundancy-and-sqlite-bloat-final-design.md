# Workspace 存储冗余与 SQLite 膨胀治理最终方案

> 状态：方案已确定，待实现。
>
> 前置分析：`docs/superpowers/specs/2026-08-02-workspace-redundancy-and-sqlite-bloat-design.md`

## 摘要

远程环境的存储膨胀来自三个相互叠加的问题：

1. 每个 task workspace 复制整套 `skills/`，且任务结束后目录长期保留。
2. `task_events` 把 `text_delta`、`tool_call_delta` 等流式增量逐条写进 SQLite。
3. `audit_artifacts` 重复保存 `runtime_prompt_envelope`、`model_request`、`request_messages` 等多份几乎相同的完整请求。

最终方案按以下方向治理：

- `skills/` 改为用户 home 的共享只读目录，task workspace 不再复制。
- 流式 delta 只走进程内 `EventHub`，不写入数据库。
- 审计只保留前端需要展示的“大事件”，每次模型回复只保存一个完整 artifact。
- 审计保留期 30 天，但绝不删除用户的 conversation 与 conversation message。
- 修复 sent attachment 回收，允许按保留期删除附件文件。
- 提供一次历史数据治理与 SQLite `VACUUM` 入口。

第二轮评审（2026-08）在既有方案之外补充了以下增量项：

- `tool.started` / `tool.finished` 的 `task_events` 载荷裁剪：只留摘要，完整参数与输出由 audit artifact 承载。
- `tasks.result_json` 收敛：不再保存完整 `final_message`（含完整 reasoning），只保留摘要与 usage。
- `memory.context_state` 不再随每次 step 双写 `task_events` + `audit_events`。
- `user_sessions` 增加过期清理。
- 服务优雅关闭时执行 `wal_checkpoint(TRUNCATE)`，日常收掉 WAL 残留。

## 最终决策

以下决策来自需求评审，实现时应优先遵守：

- `skills/` 采用共享只读目录，不复制进 task workspace。
- 技能内容通过 prompt 注入和 `using_skills` 结果进入模型上下文，不依赖 task workspace 内的 `skills/` 路径。
- 前端审计不需要查看流式 delta，因此 `text_delta`、`reasoning_delta`、`tool_call_delta`、流式 `usage`、流式 `completed` 均不持久化。
- 审计保留一次请求的完整回复，回复包含 reasoning、tool call、provider state 等模型侧信息。
- 审计页面只展示大事件：模型完整回复、工具调用开始参数、工具调用结果、请求组装等。
- sent attachment 允许 GC，并修复当前删除失败后无法继续清理的问题。
- 审计保留期默认 30 天。
- 审计 GC 只删除 audit 数据，不删除 conversation、conversation message 或 task 快照。
- `task_events` 中的 `tool.started` / `tool.finished` 只持久化摘要载荷（`tool_call_id`、`tool_name`、`arguments_length`、`output_length`、`error`），完整 Arguments/Output 由 audit artifact 承载。
- `tasks.result_json` 只保留摘要（final content 截断、usage、cost、stop_reason、conversation_id 等），不保存完整 `final_message` 的 reasoning/provider data。
- `memory.context_state` 归入实时事件（`EmitLive`），不再写入 `task_events`；`audit_events` 中按低频事件保留（每次 step 一条，payload 很小，审计时间线仍可展示）。
- `user_sessions` 按 `expires_at` 过期清理，默认保留期 30 天。
- 服务优雅关闭时执行 `PRAGMA wal_checkpoint(TRUNCATE)`，减少日常 WAL 残留。

## 目标

- 停止 task workspace 对整套 `skills/` 的重复复制。
- 为 readonly 任务补齐正确的终态流转，保证 workspace 可以被回收。
- 阻止流式事件继续写入 SQLite。
- 将审计 artifact 收敛为每次模型请求/回复各一份完整记录。
- 修复 sent attachment GC，并补齐历史孤立附件治理。
- 提供 workspace、audit、task events、attachment 的可配置保留期。
- 提供历史数据治理与 SQLite 文件收缩能力。
- 保持 conversation、audit replay、workspace browser 现有链路可用。
- 消除 `task_events` 中 tool 事件与 audit artifact 的重复载荷。
- 收敛 `tasks.result_json` 与 conversation / audit 的三重复存储。
- 为 `user_sessions` 补齐过期清理。
- 优雅关闭时自动 checkpoint WAL。

## 非目标

- 不改变 conversation、task、approval、interaction 的现有领域模型。
- 不删除用户已有 conversation、conversation message 或 task 快照。
- 不实现容器或沙箱执行后端。
- 不做细粒度文件级 workspace merge，workspace 确认仍为整目录回写。
- 不实现审计全文检索。
- 不在前端维护第二套存储配置。
- 不手改 Swagger 生成产物。

## 架构

### 1. Workspace：共享只读 skills

#### 当前问题

`core/workspaces/manager.go` 的 `CreateTaskWorkspace` 把整个 home 复制到 task root。过滤规则没有排除 `skills/`，因此每个任务都复制约 4MB 技能文件。readonly 任务虽然不回写 home，但同样产生完整副本。

#### 目标结构

```text
data/workspaces/users/{user_id}/
  home/
    AGENTS.md
    skills/                    # 用户共享技能，只读供任务解析
  tasks/
    {task_id}/                 # 任务执行目录，不包含 skills/
  backups/
    {task_id}-{timestamp}/
```

task workspace 只保留任务执行所需的文件。skills 作为用户 home 的共享资源，由 `core/skills` 从独立 root 解析。

#### 运行时 root 分离

引入 `WorkspaceContext` 或等效结构，把三种 root 分开：

```text
ExecutionRoot  -> task workspace
PromptRoot     -> task workspace
SkillsRoot     -> user home/skills
```

调整点：

- `core/skills.Loader` 从 `SkillsRoot` 读取。
- `core/prompt` 的 workspace prompt 仍从 task workspace 读取 `AGENTS.md`。
- `using_skills` 从 `SkillsRoot` 读取技能内容和可见资源。
- `core/tools/builtin.Options` 增加 `SkillsRoot`，执行目录仍是 `WorkspaceRoot`。
- 任务内不得把共享 skills 当作可写目录。技能编辑走 home workspace 管理入口。

技能资源路径需要与执行路径解耦。`using_skills` 返回的 `ResourceRefs` 不能再依赖 task workspace 下的 `skills/...` 路径，应由 `SkillsRoot` 提供受控的只读访问。

#### readonly 任务终态

当前 `completeExecutorWorkspace` 对 readonly 任务直接返回，导致 readonly workspace 长期保持 `active`。必须改为：

- readonly 任务成功、失败、取消时统一标记 `completed`。
- mutable 任务继续使用 `completed`、`pending_merge`、`merged`、`discarded` 状态机。
- workspace GC 永不删除 `active`、`pending_merge`。

#### workspace 回收

在 `core/workspaces.Manager` 增加：

```go
CleanupExpired(ctx context.Context, now time.Time, options CleanupOptions) (CleanupReport, error)
```

规则：

- 遍历 `users/{user_id}/tasks/*` 和 `users/{user_id}/backups/*`。
- 读取 `.workspace-state.json` 判断状态。
- `completed`、`discarded`、`merged` 超过 `taskRetention` 后删除。
- backup 超过 `backupRetention` 后删除。
- 状态文件缺失或损坏时只记录错误，不执行 `RemoveAll`。
- 删除前再次验证目标路径位于 workspaces root 内。
- 使用独立 `terminal_at` 字段计算保留期，避免 `updated_at` 被后续无关更新改变。

默认配置：

```yaml
workspaces:
  root: data/workspaces
  taskRetention: 720h
  backupRetention: 720h
  gcInterval: 1h
```

`retention <= 0` 表示关闭自动回收。

### 2. Task events：流式事件只走内存

#### 当前问题

`taskRuntimeSink.OnStreamEvent` 把每个 `RunStreamEvent` 通过 `Runtime.Emit` 写入 `task_events`。这导致一个任务产生上万条 `log.message` 行，其中 `tool_call_delta` 体积最大。

#### 最终行为

`core/tasks.Runtime` 增加 `EmitLive`：

```go
func (r *Runtime) EmitLive(ctx context.Context, eventType string, level string, payload any) error
```

`EmitLive` 只通过 `EventHub` 广播，不写 `task_events`。`taskRuntimeSink.OnStreamEvent` 改为调用 `EmitLive`。

以下流式事件不持久化：

- `text_delta`
- `reasoning_delta`
- `tool_call_delta`
- 流式 `usage`
- 流式 `completed`

`task_events` 只保留生命周期与大事件：

- `task.created`
- `task.started`
- `task.finished`
- `step.started`
- `step.finished`
- `tool.started`
- `tool.finished`
- `approval.requested`
- `approval.resolved`
- `interaction.requested`
- `interaction.responded`
- 低频 `log.message`

#### SSE 重连语义

实时 delta 必须标记为不可回放。SSE 订阅重连后只补发 `after_seq` 之后的持久化事件，不补发已经丢失的 delta。最终回复以 conversation 或 audit 中的完整消息为准。

### 3. 审计：大事件模型

#### 保留的审计事件

审计时间线只保留前端需要展示的边界事件：

- `run.started` / `run.finished`
- `step.started` / `step.finished`
- `prompt.resolved`
- `request.built`
- `model.completed`
- `tool.started`
- `tool.finished`
- `interaction.requested`
- `interaction.responded`

不再新增：

- `text_delta`
- `reasoning_delta`
- `tool_call_delta`
- 流式 `usage`
- 流式 `completed`
- `request_messages`

#### canonical artifact

每次模型步骤只保留两个核心完整 artifact：

- `model_request`：发给 provider 的完整请求。
- `model_response`：模型返回的完整回复。

`model_response.Message` 已经包含：

- 完整文本内容
- reasoning
- reasoning items
- tool calls
- provider state

因此不需要保存 delta 拼接结果。`runnerModelResponseArtifact` 保持完整，并确保 `Message` 是最终组装结果。

#### runtime prompt envelope

`runtime_prompt_envelope` 改为轻量元数据，不再保存完整 messages 或 prompt 内容：

```text
message_count
prompt_message_count
segment_count
phase_segment_counts
source_counts
source hash / size
```

真正的最终请求内容由 `model_request` 承载。这样既可以避免重复写入，也保留请求组装事件的展示摘要。

#### request messages

删除 `request_messages` artifact。`user_message.appended` 仍可保留事件本身，但不再引用完整历史消息。历史消息已经存在于 conversation 和首个 `model_request` 中。

#### SHA256

`core/audit.Store.CreateArtifact` 应在调用方未提供 hash 时自动计算 `SHA256`。用于后续去重、文件外置和历史治理校验。

#### 审计保留期

```yaml
storage:
  auditRetention: 720h
```

清理以完整 `audit_run` 为单位：

1. 删除关联 artifact 文件。
2. 删除 `audit_events`。
3. 删除 `audit_artifacts`。
4. 删除 `audit_runs`。

活动中的 audit run 永不删除，以 `finished_at` 判断过期。

审计 GC 不处理：

- `conversations`
- `conversation_messages`
- `tasks`

因此用户已有会话不受审计过期影响。前端在 audit run 已删除时应显示审计数据已过期，而不是会话不存在。

#### 可选：大 artifact 外置

如果 `model_request` 和 `model_response` 仍导致 SQLite 过大，后续可将超过阈值的 `body_json` 存到：

```text
data/audit-artifacts/{run_id}/{artifact_id}.json
```

SQLite 只保留 `storage_key`、`size_bytes`、`sha256`。replay 按需读取文件。第一版不强制实现，先通过去重和 30 天保留控制体积。

### 4. 附件 GC

允许 sent attachment 按保留期清理，但不删除 conversation。

#### 修复删除顺序

当前 `Store.GCExpired` 先把 sent 状态改为 `expired`，再删除文件。如果文件删除失败，后续 GC 不再处理该记录。

改为：

1. 删除存储对象和 `.meta.json`。
2. `ErrObjectNotFound` 视为删除成功。
3. 成功后更新 DB 状态为 `expired`。
4. 删除失败时保留原状态，等待下一轮重试。
5. 额外扫描 `status=expired` 的历史记录，清理遗留文件。

#### expires_at IS NULL

不直接全量补齐过期时间。先做 dry-run，只清理明确孤立的附件：

- conversation 已不存在。
- 没有被任何 conversation message 引用的附件。
- 已知为临时生成物的附件。
- 文件存在但没有对应 DB 记录。
- DB 记录存在但文件已丢失。

清理附件文件后，保留 DB 中的元数据并标记 `expired`，前端可以显示“附件已过期”。

### 5. SQLite 维护

历史数据清理后执行：

```sql
PRAGMA wal_checkpoint(TRUNCATE);
```

`VACUUM` 不放进热服务定时循环。提供显式维护入口：

```text
ice_art storage-maintenance --dry-run
ice_art storage-maintenance --apply
ice_art storage-maintenance --vacuum
```

执行 `VACUUM` 前：

- 停止任务领取或停止服务。
- 确认没有活跃长事务。
- 确认磁盘有足够临时空间。
- 先完成数据库备份。

### 6. 增量优化：事件载荷裁剪与快照收敛

> 第二轮评审补充。这些问题不在第一版主方案范围内，但体积影响明确、改动面小，建议与主方案同一轮实现。

#### 6.1 `task_events` 的 tool 事件载荷裁剪

当前 `core/agent/events.go` 的 `emitToolStart` / `emitToolFinish` 把完整 `ToolEvent`（含 `Arguments` 原文、`Output` 完整内容）经 `Emit` 写入 `task_events`，同时 audit 又各存一份 `tool_arguments` / `tool_output` artifact，形成重复。实测 `tool.finished` 249 行约 1.1MB（单条可达 4KB+，即完整 list_files 输出），`tool.started` 250 行约 174KB。

改动：

- `taskRuntimeSink.OnToolStart` / `OnToolFinish` 持久化时只保留摘要载荷：`tool_call_id`、`tool_name`、`arguments_length`、`output_length`、`error`。
- 完整 `Arguments` / `Output` 由 audit artifact 承载（replay 已依赖 audit），不再在 `task_events` 重复。
- 实时推送仍可带完整载荷，复用 `RuntimeEventPayload` 的 Persistent / Live 分离（该机制已存在于 `core/tasks/runtime.go`）。

前端适配（SSE 重连补发场景）：

- 前端 `webapp/src/lib/transcript.ts` 的 `tool.finished` 分支读取 `payload.Output`，实时流仍能拿到完整载荷，展示不受影响。
- 重连后经 `after_seq` 从 `task_events` 补发的 `tool.finished` 只有摘要（无 `Output`）。前端需要容忍该场景：`tool` 条目结果为空时以 conversation 中的 tool 消息为准（历史重建已走 `conversation_messages`，见 `transcript.ts` 的 `fromConversationMessages` 路径），或显示 `output_length` 摘要；不阻塞 transcript 拼装。

#### 6.2 `tasks.result_json` 收敛

当前 `result_json` 保存完整 `final_message`（含 reasoning、usage、provider data），与 `conversation_messages` 的 assistant 消息、audit 的 `model_response` 三重复。实测每任务 4-14KB，94 行约 1MB，长期按任务数线性增长。

改动：

- `result_json` 只保留摘要：`conversation_id`、`stop_reason`、usage、cost、`final_message` 的 content 截断（例如 512 字符）与 role/model 元数据。
- 完整消息以 conversation / audit 为准；前端如需完整回复，从 conversation 消息读取。
- 前端契约核对：`webapp/src/lib/api.ts` 的 `normalizeRunTaskResult` 只消费 `final_message` 的展示字段（content/role/usage），不依赖 reasoning 全量，摘要化安全。

#### 6.3 `memory.context_state` 不再双写

当前每次 step 都写 `task_events`（238 行）并同时写 `audit_events`（238 行）。payload 单条很小（约 200B），但属于每 step 一次的冗余双写。

改动：

- `task_events` 侧归入实时事件：改调 `EmitLive`，不持久化。
- `audit_events` 侧保留（低频、体积小，审计时间线可展示 memory 状态变化）。

#### 6.4 `user_sessions` 过期清理

当前 `user_sessions` 无任何清理逻辑，行数随登录持续增长（本地已 37 行）。模型已有 `ExpiresAt` 字段（`app/models/user.go`）。

改动：

- 复用附件 GC 的后台循环模式，在 `app/commands/serve.go` 增加 session 清理 loop：删除 `expires_at <= now` 的过期会话。
- 配置 `storage.sessionRetention`（默认 720h），`retention <= 0` 关闭。

#### 6.5 优雅关闭 checkpoint

当前关闭时不做 `wal_checkpoint`，本地 `app.db-wal` 残留 4.3MB。维护命令的 `wal_checkpoint(TRUNCATE)` 只覆盖显式维护场景。

改动：

- 服务 shutdown 钩子中先执行 `PRAGMA wal_checkpoint(TRUNCATE)` 再退出，日常即可收掉 WAL 残留。
- 与维护命令共存：shutdown checkpoint 不执行 `VACUUM`（VACUUM 仍需停机窗口）。

## 配置

```yaml
workspaces:
  root: data/workspaces
  taskRetention: 720h
  backupRetention: 720h
  gcInterval: 1h

attachments:
  storageBackend: filesystem
  filesystem:
    root: data/attachments
  draftTTL: 24h
  sentRetention: 720h
  gcInterval: 1h

storage:
  auditRetention: 720h
  taskEventRetention: 720h
  sessionRetention: 720h
  maintenanceInterval: 24h
  vacuumEnabled: true
```

配置默认值不能触发危险删除。任何 `retention <= 0` 均表示关闭对应自动回收。

## 数据流变化

### 修改前

```text
LLM stream
  -> RunStreamEvent
  -> taskRuntimeSink.OnStreamEvent
  -> task_events.log.message（每个 delta 一行）

Model step
  -> runtime_prompt_envelope（完整 messages）
  -> model_request（完整 messages）
  -> request_messages（完整 messages）
```

### 修改后

```text
LLM stream
  -> RunStreamEvent
  -> Runtime.EmitLive
  -> EventHub（仅实时订阅者）

Model step
  -> model_request（唯一完整请求）
  -> model_response（唯一完整回复）
  -> runtime_prompt_envelope（轻量元数据）
  -> 不再写 request_messages
```

## 接口与代码变更

- `core/workspaces.Manager`：增加 `CleanupExpired`，增加 `SkillsRoot` 或等价解析能力。
- `core/workspaces.WorkspaceStateFile`：增加 `TerminalAt`。
- `core/agent/executor.go`：拆出 workspace context，readonly 任务完成后标记终态。
- `core/tasks.Runtime`：增加 `EmitLive`。
- `core/agent/task_adapter.go`：流式事件改调 `EmitLive`；tool 事件持久化只含摘要。
- `core/agent/audit.go`：移除 `request_messages`。
- `core/agent/stream.go`：压缩 `runtime_prompt_envelope`，保持 `model_response` 完整。
- `core/audit.Store`：自动计算 SHA256，增加 audit run 级清理。
- `core/audit.Replay`：适配不再存在的 artifact，且支持显示审计已过期。
- `core/attachments.Store`：修正 GC 顺序并处理历史 `expired` 记录。
- `core/tools/builtin`：增加 `SkillsRoot` 支持。
- `core/agent/events.go`：tool 事件持久化载荷裁剪为摘要。
- `core/agent/executor.go`：`result_json` 摘要化。
- `app/models/user.go`：`user_sessions` 保留 `ExpiresAt`（已有），清理直接复用。
- `app/commands/serve.go`：增加 workspace GC、audit GC、task event GC、session GC 和维护命令装配；shutdown 钩子加 `wal_checkpoint(TRUNCATE)`。

## 测试计划

### Workspace

- task workspace 不包含 `skills/`。
- `core/skills` 仍能从 `SkillsRoot` 解析技能。
- `using_skills` 仍能读取技能和受控资源。
- readonly 任务成功、失败、取消后均进入 `completed`。
- `CleanupExpired` 不删除 `active`、`pending_merge`。
- `CleanupExpired` 删除超过保留期的 `completed`、`discarded`、`merged`。
- `retention <= 0` 时后台 loop 跳过。
- 路径越界时拒绝删除。

### Task events

- 流式 delta 不写入 `task_events`。
- 生命周期事件仍可查询。
- SSE 实时事件可推送，重连后只补发持久化事件。
- `tool.started` / `tool.finished` 的持久化载荷只含摘要，不含完整 Arguments/Output。
- audit 的 `tool_arguments` / `tool_output` artifact 仍保留完整内容。

### Audit

- 单次模型步骤只产生一个 `model_request` 和一个 `model_response`。
- 不再产生 `request_messages`。
- `runtime_prompt_envelope` 不含完整 messages。
- `model_response.Message` 包含 reasoning、tool calls 等完整信息。
- audit replay 仍可展示大事件。
- audit 30 天后删除 audit run，不删除 conversation。
- artifact 的 `sha256` 非空。

### Task snapshot / sessions

- `result_json` 只含摘要，不含完整 reasoning；前端摘要展示字段可用。
- 过期的 `user_sessions` 被清理，未过期会话保留。

### Attachment

- 过期 sent 文件被删除，metadata 文件同步删除。
- 删除失败时保留原状态，下一轮继续重试。
- 历史 `expired` 记录会再次清理遗留文件。
- 孤立附件 dry-run 能正确识别，不误删仍被引用的附件。
- GC 不修改或删除 conversation message。

### Database

- 清理后执行 `wal_checkpoint(TRUNCATE)`。
- `VACUUM` 只在显式维护流程执行。
- 数据库文件在治理后显著缩小。
- 优雅关闭后 `-wal` 文件被 checkpoint，残留明显减小。

## 验收标准

- 新任务不再复制 `skills/`。
- 新任务不再为流式 delta 写 `task_events`。
- 新任务不再产生 `request_messages`。
- 每个模型步骤只产生一个完整 `model_request` 和一个完整 `model_response`。
- readonly workspace 任务结束后可被 GC。
- `active`、`pending_merge` workspace 不会被自动删除。
- audit 过期后 conversation 和 conversation message 仍然存在。
- 过期 sent attachment 文件被删除，但 conversation 数据不受影响。
- 历史治理后 SQLite 文件显著缩小，且没有大量未引用 artifact。
- `task_events` 中 tool 事件体积显著下降（不再携带完整输出）。
- `result_json` 与 conversation / audit 不再三重复保存完整消息。
- `user_sessions` 过期行不再累积。

## 实施顺序

1. 增加 `SkillsRoot` 并停止复制 `skills/`。
2. 修复 readonly 任务终态。
3. 实现 `EmitLive`，停止流式事件落库。
4. 收敛审计 artifact，移除 `request_messages`。
5. 增加 workspace、audit、task events 保留期。
6. 修复 attachment GC 和历史孤立附件治理。
7. 执行历史数据 dry-run 与批量治理。
8. 停机执行 `wal_checkpoint(TRUNCATE)` 和 `VACUUM`。
9. 根据实际体积决定是否将大 audit artifact 外置到文件系统。
10. 增量项（可与 3-5 并行）：tool 事件载荷裁剪、`result_json` 摘要化、`memory.context_state` 改 `EmitLive`。
11. 增量项：`user_sessions` 过期清理 loop。
12. 增量项：shutdown 钩子 `wal_checkpoint(TRUNCATE)`。

## 风险

- `SkillsRoot` 分离会影响 `using_skills` 和技能资源的路径语义，需要同步测试。
- 流式 delta 不再可回放，SSE 重连和前端 transcript 逻辑必须适配。
- `model_response` 依赖 `FinalMessage()` 返回完整 reasoning 和 provider state；不同 provider 需要验证字段完整性。
- audit run 级删除必须严格按顺序执行，避免留下引用失效的 artifact。
- 附件 GC 对 `expires_at IS NULL` 的记录需要 dry-run，避免误删仍被 conversation 使用的文件。
- tool 事件载荷裁剪后，如果前端或回放依赖 `task_events` 中的完整输出，需要改为依赖 audit artifact；实现前核对前端 transcript 是否读取 tool.finished 的 `Output` 字段。已核对：实时流读取（`transcript.ts`），重连补发只有摘要时需按上文适配，历史重建走 `conversation_messages`，不受影响。
- `result_json` 摘要化前核对前端是否消费完整 `final_message`（当前 `normalizeRunTaskResult` 只消费展示字段，摘要化安全）。
- session 清理不能误删活跃会话，须严格按 `expires_at` 判断。

## 关键代码位置

- `core/workspaces/manager.go`
- `core/workspaces/types.go`
- `core/skills/loader.go`
- `core/skills/resolver.go`
- `core/tools/builtin/using_skills.go`
- `core/tools/builtin/options.go`
- `core/tasks/runtime.go`
- `core/tasks/event_hub.go`
- `core/agent/task_adapter.go`
- `core/agent/stream.go`
- `core/agent/audit.go`
- `core/agent/runtime_prompt_artifact.go`
- `core/audit/store.go`
- `core/audit/replay.go`
- `core/attachments/store.go`
- `core/attachments/filesystem.go`
- `core/agent/events.go`
- `core/agent/executor.go`
- `app/models/user.go`（`user_sessions`）
- `app/commands/serve.go`
- `app/config/app.go`
