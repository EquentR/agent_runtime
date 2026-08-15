# Workspace 存储冗余与 SQLite 膨胀治理设计

> 状态：探索完成；最终方案见 `2026-08-13-workspace-redundancy-and-sqlite-bloat-final-design.md`。

## 摘要

针对 31.57.10.23 环境的只读探索确认了两个独立但相互叠加的存储问题：

1. `data/workspaces` 里的每个 task workspace 都完整复制了 home 的 `skills/`，且任务结束后目录不会自动回收，导致 50 个左右会话就累积约 219MB。
2. `data/app.db` 在单用户、50 会话多轮使用后达到 1.09GB，主要来自 `audit_artifacts` 重复保存完整模型请求，以及 `task_events` 把每个流式事件逐条落库。

本文档只整理探索结果与后续治理设计，不包含实现代码。设计目标是先消除重复写入，再补齐保留期与回收机制，最后做一次历史数据治理。

## 探索事实

### 远程环境快照

`/opt/ice-art` 总量约 1.4GB：

| 路径 | 大小 |
| --- | ---: |
| `data/app.db` | 1.09GB |
| `data/workspaces` | 219MB |
| `data/attachments` | 132MB |
| `logs` | 488KB |

SQLite 文件参数：

- `page_size = 4096`
- `page_count = 266874`
- `journal_mode = wal`
- `freelist_count = 1`

### 数据库表占用

按 BLOB 原始长度统计：

| 表 | 行数 | 原始 BLOB 体积 |
| --- | ---: | ---: |
| `audit_artifacts` | 2,205 | 876,597,849 |
| `task_events` | 113,202 | 153,891,323 |
| `audit_events` | 4,111 | 518,688 |
| `conversation_messages` | 740 | 较小 |
| `conversations` | 46 | 较小 |

`audit_artifacts` 按 kind 统计：

| kind | 行数 | 原始体积 |
| --- | ---: | ---: |
| `model_request` | 390 | 384,705,603 |
| `runtime_prompt_envelope` | 390 | 381,884,307 |
| `request_messages` | 148 | 105,465,237 |
| `model_response` | 326 | 3,817,651 |
| `tool_output` | 272 | 354,936 |
| `tool_arguments` | 272 | 243,657 |
| 其余 | 407 | 约 126KB |

`task_events` 中的 `log.message` 按流事件类型统计：

| 类型 | 行数 | 原始体积 |
| --- | ---: | ---: |
| `tool_call_delta` | 47,123 | 141,354,246 |
| `text_delta` | 62,647 | 7,446,084 |
| `completed` | 326 | 3,817,770 |
| `usage` | 342 | 66,451 |
| `reasoning_delta` | 406 | 50,603 |
| `stream_recovery` | 51 | 11,890 |

单个任务 `tsk_37238d99-...` 就产生了 12,131 条 `log.message`，共约 88MB，其中 11,683 条是 `tool_call_delta`。

### Workspace 快照

`data/workspaces/users` 当前状态：

- 用户 1：home 4MB、tasks 196MB（49 个任务目录）、backups 12MB（3 份备份）
- 用户 2：home 4MB、tasks 4MB（1 个任务目录）
- 49 个任务目录几乎都是 4MB，主要内容是整套 `skills/`
- `skills/docx`、`skills/xlsx`、`skills/pptx` 各自约 1.3MB，其中 `scripts/office/schemas` 是主要体积来源
- 7 个 readonly 任务目录同样复制了整套 skills
- 49 个 mutable 任务目录状态全部为 `completed`，另有 7 个 readonly 任务目录状态为 `active`
- 当前 workspace 下未发现 `.attachments` 目录

### 附件快照

- `data/attachments/sent` 共 71 个文件，约 132MB
- `conversation_attachments` 中 `expired` 有 115 条，size 合计约 139MB
- `sent` 有 35 条，size 合计约 30MB
- 大量 6 月创建的 sent 附件 `expires_at` 为 NULL，至今仍保留在文件系统

## 根因分析

### 1. Workspace 复制与保留

`core/workspaces/manager.go` 的 `CreateTaskWorkspace` 无条件把 home 整目录复制到 task root：

- 过滤规则只排除 `.attachments`、`.workspace-state.json`、`.workspace-baseline.json`
- `skills/` 属于 home 的正常内容，因此每个会话都会复制整套技能
- readonly 任务也走同一复制路径，readonly 语义只限制回写 home，不限制磁盘复制

任务结束后只更新状态，不删除目录：

- `CompleteTaskWorkspace` 只标记 `completed`
- `DiscardTaskWorkspace` 只标记 `discarded`
- `ConfirmTaskWorkspace` 只标记 `merged`

原始设计文档明确“第一版不自动回收”，见 `docs/superpowers/specs/2026-05-23-user-workspace-isolation-design.md`。因此目录数量等于历史会话数量，空间随会话线性增长。

当前环境的 132MB 附件不在 workspace 复制路径中，`isWorkspaceSidecar` 已排除 `.attachments`，所以附件膨胀应归因于全局 sent 附件保留，而不是会话级复制。

### 2. 审计 artifact 重复保存完整请求

每个模型步骤会保存两个几乎相同的完整请求 artifact：

- `runtime_prompt_envelope`：包含 resolved prompt 的完整 messages
- `model_request`：包含发给 provider 的完整 `ChatRequest.Messages`

对应写入点在 `core/agent/stream.go`：

- `ArtifactKindRuntimePromptEnvelope` 的 attach
- `ArtifactKindModelRequest` 的 attach

每次任务开始还会额外保存一份 `request_messages`，见 `core/agent/audit.go`。它的内容与首步 `runtime_prompt_envelope`/`model_request` 高度重叠。

远程数据中仅 `model_request` + `runtime_prompt_envelope` 就是约 767MB，加上 `request_messages` 约 105MB，三者合计约 872MB，基本就是 SQLite 1GB 的主体。

### 3. 流式事件被逐条落库

`taskRuntimeSink.OnStreamEvent` 把每个 `RunStreamEvent` 都通过 `Runtime.Emit` 写成 `task_events` 的 `log.message`：

- `text_delta` 每收到一个文本增量就写一行
- `tool_call_delta` 每个参数增量都写一行，且每条都携带完整的 ToolCall/arguments 载荷
- `completed`、`usage`、`stream_recovery` 也写行

这些事件原本只用于实时推送，不应该成为永久数据库记录。单个任务因此出现上万条事件行。

### 4. 附件 GC 未收敛

系统已有 `startAttachmentGCLoop`，`core/attachments/store.go` 的 `GCExpired` 会把过期 sent 标记为 expired 并删除存储对象。但远程环境仍有 115 条 expired 记录对应的文件，且大量旧 sent 附件 `expires_at` 为 NULL，说明清理没有按预期收敛，需要在实现阶段验证：

- GC loop 是否一直在运行
- sent 存储 key 是否与 DB 中记录一致
- `expires_at IS NULL` 的旧记录是否有兜底保留期

## 目标

- 消除每个会话对整套 `skills/` 的重复复制
- 为 completed/discarded/merged task workspace 增加保留期与回收机制
- 让 `audit_artifacts` 不再保存多份完整请求
- 让 `task_events` 不再持久化 text/tool delta 流事件
- 为审计 artifact、task events、workspace、sent attachment 补齐可配置保留期
- 提供一次历史数据治理手段，并能在验证后缩小 SQLite 文件
- 保持现有 audit replay、workspace browser、conversation 链路可用

## 非目标

- 不改变 conversation、task、approval、interaction 的现有领域模型
- 不引入容器/沙箱执行后端
- 不做细粒度文件级 merge，workspace 确认仍为整目录回写
- 不实现审计全文检索
- 不在前端维护第二套存储配置
- 不手改 Swagger 生成产物

## 设计约束

- 保持 `app -> core -> pkg` 分层
- workspace 路径安全规则继续生效，不允许符号链接穿透
- audit replay 依赖 `model_request`、`model_response`、tool artifact 等完整 body，去重不能破坏 replay
- `core/skills`、`core/prompt` 只从 workspace root 读取文件
- 新增后台任务统一在 `app/commands/serve.go` 装配，不新建启动路径
- 配置默认值不能触发危险删除，`retention <= 0` 应视为不启用自动回收

## 建议方案

### 1. Workspace 复制与回收

#### 1.1 短期：停止无意义复制

readonly 任务不修改 home，也不需要把 `skills/` 复制进 task root。建议对 readonly 任务跳过 `skills/` 复制，让 `core/skills` 解析回 home 的 skills；如果工具必须在 task root 内访问 skills，则为 skills 增加独立只读解析路径，而不是物理复制。

mutable 任务先保留复制，但引入技能变更检测：

- `buildManifest` 已能计算每个文件的 SHA256
- 如果本次任务没有修改 `skills/`，则不在 task root 保留技能副本，或在回收时立即删除未变更部分
- 如果任务修改了 `skills/`，仍走现有 merge 流程，但只回写发生变化的 skill 文件

该方案需要同步调整 `core/skills` 与 `core/prompt` 的 root 解析，避免 task root 内缺少 `skills/` 时解析失败。

#### 1.2 长期：共享只读技能

更彻底的方案是把 `skills/` 视为用户 home 的共享只读资源：

- task workspace 不复制 skills
- 任务需要读取 skill 时，从 home 的 skills 目录解析
- 用户对 skills 的编辑通过独立管理入口写入 home
- 任务内产生的 skill 变更单独进入 pending merge，不参与整目录复制

此方案改动面较大，建议作为独立后续迭代，不在存储治理第一版内强制完成。

#### 1.3 自动回收

在 `core/workspaces` 增加清理能力：

- 新增 `CleanupExpired(ctx, now, options) (int, error)`
- 遍历 `users/{user_id}/tasks/*`，读取 `.workspace-state.json`
- 永不删除 `active`、`pending_merge` 状态
- 对 `completed`、`discarded`、`merged` 超过 `taskRetention` 的目录执行删除
- 删除前再次校验路径在 workspaces root 内，避免越界
- 删除空父目录
- 对 `backups/*` 超过 `backupRetention` 的目录执行删除

清理逻辑放在 `app/commands/serve.go` 的后台循环中，复用附件 GC 的启动模式：

```go
startWorkspaceGCLoop(ctx, workspaceManager, cfg.Workspaces.ResolvedGCInterval())
```

#### 1.4 配置

扩展现有 `WorkspacesConfig`：

```yaml
workspaces:
  root: data/workspaces
  taskRetention: 30d
  backupRetention: 30d
  gcInterval: 1h
```

默认建议：

- `taskRetention = 30d`
- `backupRetention = 30d`
- `gcInterval = 1h`
- `retention <= 0` 表示不自动回收

### 2. 审计 artifact 去重

#### 2.1 保留 canonical artifact

建议把 `model_request` 作为每次模型请求的唯一完整 artifact：

- 保留 `model_request`
- `runtime_prompt_envelope` 降级为轻量元数据：只保存 segments、prompt message count、source counts，不保存完整 messages
- 不再保存 `request_messages`，或只保存一条指向首个 `model_request` 的引用

对应改动点：

- `core/agent/audit.go` 移除或改小 `ArtifactKindRequestMessages`
- `core/agent/stream.go` 对 `ArtifactKindRuntimePromptEnvelope` 使用不含完整 messages 的 artifact body
- `core/audit/replay.go` 保留 `model_request` 的 inline body；若 `runtime_prompt_envelope` 只含元数据，replay 视图同步调整

#### 2.2 可选：大 artifact 移出 SQLite

如果审计 replay 需要长期保留完整 body，建议后续把 `audit_artifacts` 的 `body_json` 移到文件系统：

- DB 只保存 `storage_key`、`size_bytes`、`sha256`
- 文件按 `data/audit/{run_id}/{artifact_id}.json` 存放
- replay 接口按需读取文件

该方案涉及 `core/audit` 存储抽象和 handler 读取路径，建议作为第二阶段。

#### 2.3 补齐元数据

远程 `audit_artifacts.sha256` 全为空。写入 artifact 时应计算并保存 SHA256，便于后续去重和 GC。

### 3. Task events 流事件治理

#### 3.1 流事件只走内存

`taskRuntimeSink.OnStreamEvent` 不应调用持久化 `Runtime.Emit`。建议增加 live-only 发布方法：

- `Runtime.EmitLive(ctx, eventType, level, payload)` 只发布到 EventHub
- `taskRuntimeSink.OnStreamEvent` 改调 `EmitLive`
- `text_delta`、`tool_call_delta`、`reasoning_delta`、`usage` 不写 `task_events`
- `completed`、`stream_recovery` 如果前端需要事后回溯，可以保留为单条事件，但 payload 不应包含完整消息

#### 3.2 保留生命周期事件

`task_events` 继续保留：

- `task.created`、`task.started`、`task.finished`
- `step.started`、`step.finished`
- `tool.started`、`tool.finished`
- `approval.*`、`interaction.*`
- 低频 `log.message`

#### 3.3 保留期

新增 `task_events` 清理：

- 按 `created_at` 删除超过 `taskEventRetention` 的事件
- 删除需要带 limit，避免单次事务过大
- 建议默认 `taskEventRetention = 30d`

### 4. 附件 sent GC

在现有 `AttachmentStorageConfig` 基础上验证并修复 sent 清理：

- 确认 `startAttachmentGCLoop` 在服务运行期间持续执行
- 修复 `expires_at IS NULL` 的旧 sent 记录，按 `created_at + sentRetention` 计算过期时间
- 删除 sent 对象时同时删除 `*.meta.json`
- 增加集成测试：过期 sent 文件必须从 `data/attachments/sent` 消失

### 5. SQLite 文件维护

删除大量行后 SQLite 文件不会自动缩小。增加维护能力：

- 在服务低峰执行 `wal_checkpoint(TRUNCATE)`
- 定期对可写数据库执行 `VACUUM`
- VACUUM 前确认没有活跃长事务，或使用独立维护窗口
- 提供一次性治理脚本/管理命令用于当前 1GB 文件

建议配置：

```yaml
storage:
  maintenanceInterval: 24h
  vacuumEnabled: true
```

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
  -> model_request（唯一完整 artifact）
  -> runtime_prompt_envelope（轻量元数据）
  -> 不再写 request_messages
```

## 接口与配置变更

### 配置

```yaml
workspaces:
  root: data/workspaces
  taskRetention: 30d
  backupRetention: 30d
  gcInterval: 1h

attachments:
  storageBackend: filesystem
  filesystem:
    root: data/attachments
  draftTTL: 24h
  sentRetention: 720h
  gcInterval: 1h

storage:
  taskEventRetention: 30d
  auditArtifactRetention: 90d
  maintenanceInterval: 24h
  vacuumEnabled: true
```

### 新增能力

- `core/workspaces`：`CleanupExpired`
- `core/tasks`：`Runtime.EmitLive`
- `core/audit`：artifact 清理与 SHA256 补齐
- `app/commands/serve.go`：workspace GC loop、storage maintenance loop

## 测试计划

### Workspace

- 创建 readonly 任务时不复制 `skills/`
- 创建 mutable 任务时技能未变更则回收技能副本
- `CleanupExpired` 不删除 `active`/`pending_merge`
- `CleanupExpired` 删除超过保留期的 `completed`/`discarded`/`merged`
- 删除路径越界时拒绝执行
- 后台 loop 配置 `retention <= 0` 时跳过

### Audit

- 单次模型步骤只产生一个完整请求 artifact
- `runtime_prompt_envelope` 不再包含完整 messages
- 不再产生 `request_messages`
- audit replay 仍能从 `model_request` 重建请求
- artifact 的 `sha256` 非空

### Task events

- `text_delta`/`tool_call_delta` 不写入 `task_events`
- 生命周期事件仍可查询
- 保留期清理按 limit 分页执行

### 附件

- 过期 sent 记录在 GC 后文件被删除
- `expires_at IS NULL` 的旧 sent 按兜底保留期计算
- metadata 文件随对象一起删除

### 数据库维护

- 删除后执行 `wal_checkpoint(TRUNCATE)`
- VACUUM 在低峰可执行，不阻塞正常请求
- 数据库文件大小在治理后显著下降

## 实施顺序

建议按以下顺序推进：

1. 流事件 live-only：消除 `task_events` 的持续膨胀
2. 审计 artifact 去重：停止写入重复完整请求
3. Workspace 保留期与回收：控制目录数量
4. 附件 sent GC 修复：清理历史附件
5. SQLite 维护：checkpoint + VACUUM + 历史数据治理
6. 长期技能共享方案：独立迭代

## 风险与开放问题

### 风险

- 审计去重可能影响 replay 展示，需要先确认前端/API 对 `runtime_prompt_envelope` 和 `request_messages` 的依赖
- `core/skills` 与 `core/prompt` 当前都从 task root 解析；跳过 skills 复制需要同步修改解析路径
- workspace 自动回收不能误删正在运行的 readonly 任务目录
- SQLite VACUUM 需要维护窗口，不能在热路径中执行
- 远程环境当前没有运行中的 `ice_art` 进程，附件 GC 是否“跑过但没有删除”还是“从未运行”需要进一步验证

### 开放问题

1. 是否允许用户通过 workspace browser 编辑 `skills/`？如果允许，任务内修改 skills 的合并策略是什么？
2. audit replay 是否必须保留完整 `runtime_prompt_envelope`？如果是，只能移出 SQLite，不能直接删除。
3. 历史 1GB 数据治理是否需要用户确认后删除，还是仅删除已过期记录？
4. `sentRetention = 720h` 与当前 `expires_at IS NULL` 旧记录如何处理？

## 附录：关键代码位置

- `core/workspaces/manager.go`：`CreateTaskWorkspace`、`CompleteTaskWorkspace`、`DiscardTaskWorkspace`、`isWorkspaceSidecar`
- `core/agent/task_adapter.go`：`OnStreamEvent`
- `core/agent/stream.go`：`runtime_prompt_envelope`、`model_request` attach
- `core/agent/audit.go`：`request_messages` attach
- `core/audit/replay.go`：replay 依赖的 artifact kinds
- `core/attachments/store.go`：`GCExpired`
- `app/commands/serve.go`：`startAttachmentGCLoop`
