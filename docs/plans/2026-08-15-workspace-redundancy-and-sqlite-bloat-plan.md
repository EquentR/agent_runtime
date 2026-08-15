# Workspace 存储冗余与 SQLite 膨胀治理实施计划

> 状态：计划待评审，未开始执行。
>
> Spec：`docs/superpowers/specs/2026-08-13-workspace-redundancy-and-sqlite-bloat-final-design.md`
>
> 执行模式：subagent-driven-development 强审模式（`strong-review`），除非用户后续明确切换。
>
> 本计划把 spec 拆成垂直任务块；每个任务块同时是 to-tickets 语义下的一个 ticket。依赖图无环，执行时按拓扑序推进。

## 任务图

- `TASK-01` Shared Skills Root：无前置依赖。
- `TASK-02` Readonly 终态与 Workspace GC：无前置依赖。
- `TASK-03` EmitLive 与流式 delta：无前置依赖。
- `TASK-04` Tool 事件载荷裁剪：依赖 `TASK-03`。
- `TASK-05` 审计 canonical artifact：无前置依赖。
- `TASK-06` 审计保留期 GC：依赖 `TASK-05`。
- `TASK-07` result_json 摘要化：无前置依赖。
- `TASK-08` memory.context_state 单写：依赖 `TASK-03`。
- `TASK-09` Attachment GC 修复：无前置依赖。
- `TASK-10` Session GC：无前置依赖。
- `TASK-11` 优雅关闭 checkpoint：无前置依赖。
- `TASK-12` 维护命令与历史治理：依赖 `TASK-02`、`TASK-04`、`TASK-05`、`TASK-06`、`TASK-07`、`TASK-08`、`TASK-09`、`TASK-10`、`TASK-11`。

## TASK-01: Shared Skills Root

### Goal

任务 workspace 不再复制整套 `skills/`，技能改为从用户 home 的共享只读目录解析；模型通过 prompt 注入和 `using_skills` 结果获取技能内容。

### Acceptance

- 新建 mutable 与 readonly task workspace 时，目录内不再包含 `skills/`。
- `core/skills` 能从共享 skills root 解析技能列表、技能详情和资源引用。
- `using_skills` 返回的技能内容和受控资源仍然可用，且不依赖 task workspace 内的 `skills/` 路径。
- 任务运行、技能选择、workspace browser 和技能管理接口在共享目录下行为不变。
- 技能目录对任务执行不可写；技能编辑入口仍在用户 home workspace。

### Dependencies

- None — can start immediately.

### Test seam

- `core/workspaces` 目录创建测试。
- `core/skills` loader/resolver 测试。
- `core/tools/builtin` 的 `using_skills` 测试。
- executor 与 skill handler 的集成测试。
- 前端技能列表/详情 API 测试。

### Checklist

- [x] 为共享 skills root 编写失败的 workspace、skills、using_skills 测试。
- [x] 拆分执行根、prompt 根和 skills 根，停止复制 `skills/`。
- [x] 让 `using_skills` 从共享 skills root 读取，并处理资源路径语义。
- [x] 更新配置装配与技能管理入口。
- [x] 运行聚焦测试并确认通过。
- [x] 记录任务 checkpoint commit：`33d5ac1` 实现；收尾 commit 补充 readonly 与 `list_files` 测试。

## TASK-02: Readonly 终态与 Workspace GC

### Goal

readonly 任务结束后进入可回收终态，过期 workspace 和 backup 能安全回收，`active` 与 `pending_merge` 永不自动删除。

### Acceptance

- readonly 任务成功、失败、取消后统一标记 `completed`。
- `CleanupExpired` 删除超过保留期的 `completed`、`discarded`、`merged` workspace。
- `CleanupExpired` 删除超过保留期的 backup。
- `active`、`pending_merge` 和状态损坏目录不会被自动删除。
- 删除前校验路径位于 workspaces root 内，越界时拒绝。
- `taskRetention`、`backupRetention`、`gcInterval` 可配置，`retention <= 0` 关闭回收。

### Dependencies

- None — can start immediately.

### Test seam

- `core/workspaces` manager 测试。
- executor workspace 收尾路径测试。
- `app/commands` 后台 GC loop 测试。
- 路径越界与保留期配置测试。

### Checklist

- [x] 为 readonly 终态和 `CleanupExpired` 编写失败测试。
- [x] 补齐 readonly 任务终态流转。
- [x] 实现 workspace/backup 保留期与安全删除。
- [x] 增加后台 GC loop 和配置。
- [x] 运行聚焦测试并确认通过。
- [x] 记录任务 checkpoint commit：`58f869a` 实现；`ee6ad6c` 修复配置零值关闭回收与 GC loop 测试。

## TASK-03: EmitLive 与流式 delta

### Goal

流式 delta 只通过内存 `EventHub` 推送给实时订阅者，不再写入 `task_events`；SSE 重连只补发持久化生命周期事件。

### Acceptance

- `Runtime.EmitLive` 只广播、不写数据库。
- `text_delta`、`reasoning_delta`、`tool_call_delta`、流式 `usage`、流式 `completed` 不再出现于 `task_events`。
- 实时 SSE 订阅者仍能收到这些事件。
- SSE 重连补发不包含已丢失的 delta，最终回复以 conversation 或 audit 完整消息为准。
- 生命周期事件仍按原样持久化。

### Dependencies

- None — can start immediately.

### Test seam

- `core/tasks` runtime/event hub 测试。
- `core/agent` task adapter 测试。
- `app/handlers` SSE 订阅测试。
- `webapp/src/lib` transcript 与 `streamRunTask` 测试。

### Checklist

- [x] 为 live-only 事件编写失败测试。
- [x] 增加 `EmitLive` 并接入 `OnStreamEvent`。
- [x] 明确 live 事件序号语义，保证不污染 `after_seq`。
- [x] 适配 SSE 重连与前端 transcript。
- [x] 运行聚焦测试并确认通过。
- [x] 记录任务 checkpoint commit：`774117e`。

## TASK-04: Tool 事件载荷裁剪

### Goal

`tool.started` / `tool.finished` 的持久化载荷只保留摘要，完整参数与输出由 audit artifact 承载；实时推送仍带完整载荷。

### Acceptance

- `task_events.tool.started` 只包含 `tool_call_id`、`tool_name`、`arguments_length`。
- `task_events.tool.finished` 只包含 `tool_call_id`、`tool_name`、`arguments_length`、`output_length`、`error`。
- 实时 SSE 的 tool 事件仍包含完整 Arguments/Output。
- audit 的 `tool_arguments`、`tool_output` artifact 仍保留完整内容。
- 前端在重连补发只拿到摘要时不阻塞 transcript 拼装，以 conversation 历史或摘要兜底。

### Dependencies

- `TASK-03`

### Test seam

- `core/agent` runner/task adapter 事件载荷测试。
- `core/tasks` 持久化 payload 测试。
- `webapp/src/lib/transcript` tool 事件测试。
- audit artifact 完整性测试。

### Checklist

- [x] 为摘要化持久化载荷编写失败测试。
- [x] 用 Persistent/Live 分离实现 tool 事件双载荷。
- [x] 适配前端 transcript 对缺失 Output 的容忍。
- [x] 运行聚焦测试并确认通过。
- [x] 记录任务 checkpoint commit：`fdf554e`。

## TASK-05: 审计 canonical artifact

### Goal

每次模型步骤只保留一份完整 `model_request` 和一份完整 `model_response`；移除重复的 `request_messages`，`runtime_prompt_envelope` 只保留轻量元数据，并补齐 SHA256。

### Acceptance

- 单次模型步骤只产生一个 `model_request` artifact。
- 单次模型步骤只产生一个 `model_response` artifact，且 Message 包含完整内容、reasoning、tool calls、provider state。
- 不再产生 `request_messages` artifact。
- `runtime_prompt_envelope` 不再包含完整 messages 或 prompt 内容，只保留统计与来源信息。
- artifact 写入时自动计算并保存 `sha256`。
- audit replay 和审计页面仍能展示模型生成、请求组装、工具调用等大事件。

### Dependencies

- None — can start immediately.

### Test seam

- `core/agent` runner/executor/stream audit 测试。
- `core/audit` store 与 replay 测试。
- `app/handlers` audit handler 测试。
- `webapp/src/views/AdminAuditView` 测试。

### Checklist

- [ ] 为 canonical artifact 和 SHA256 编写失败测试。
- [ ] 压缩 `runtime_prompt_envelope` 并移除 `request_messages`。
- [ ] 确保 `model_response` 为最终完整消息。
- [ ] 在 audit store 层补齐 SHA256。
- [ ] 运行聚焦测试并确认通过。
- [ ] 记录任务 checkpoint commit。

## TASK-06: 审计保留期 GC

### Goal

审计数据按 30 天保留期以 audit run 为单位回收，用户 conversation 与 conversation message 不受影响。

### Acceptance

- 已结束的 audit run 超过 `auditRetention` 后，事件、artifact、run 按顺序删除。
- 活跃 audit run 不会被删除。
- 删除不涉及 `conversations`、`conversation_messages`、`tasks`。
- 前端在审计数据已过期时显示过期状态，而不是会话不存在。
- `auditRetention` 可配置，`retention <= 0` 关闭。

### Dependencies

- `TASK-05`

### Test seam

- `core/audit` store GC 测试。
- `app/commands` audit GC loop 测试。
- `app/handlers` 与前端审计过期展示测试。

### Checklist

- [ ] 为 run 级审计清理编写失败测试。
- [ ] 实现按 `finished_at` 判断的保留期删除。
- [ ] 增加后台 GC loop 与配置。
- [ ] 适配前端过期展示。
- [ ] 运行聚焦测试并确认通过。
- [ ] 记录任务 checkpoint commit。

## TASK-07: result_json 摘要化

### Goal

`tasks.result_json` 只保存任务结果摘要，不再与 conversation、audit 三重复保存完整 `final_message`。

### Acceptance

- `result_json` 只包含 conversation id、provider/model、usage、cost、stop reason 和截断后的最终内容摘要。
- `result_json` 不再包含完整 reasoning、provider data 或完整 tool calls。
- conversation 与 audit 仍保存完整回复。
- 前端任务详情、轮询结果和流式收尾仍能正常展示摘要字段。
- Swagger 中任务结果契约与实现一致。

### Dependencies

- None — can start immediately.

### Test seam

- `core/agent` executor result 测试。
- `app/handlers` task/swagger 契约测试。
- `webapp/src/lib/api` normalize 测试。
- 前端任务结果展示测试。

### Checklist

- [ ] 为摘要化 result 编写失败测试。
- [ ] 新增摘要 DTO 并替换 `RunTaskResult` 落库形态。
- [ ] 更新前端归一化与展示兼容。
- [ ] 更新 Swagger 相关断言并重新生成文档。
- [ ] 运行聚焦测试并确认通过。
- [ ] 记录任务 checkpoint commit。

## TASK-08: memory.context_state 单写

### Goal

`memory.context_state` 不再写入 `task_events`，只保留 audit 低频事件与实时推送。

### Acceptance

- `task_events` 不再出现 `memory.context_state`。
- `audit_events` 仍按每次 step 一条保留轻量 payload。
- 前端实时 memory 状态展示不受影响。
- 重连后 memory 状态以 conversation 持久化快照兜底。

### Dependencies

- `TASK-03`

### Test seam

- `core/agent` runner/events 测试。
- `core/tasks` 持久化事件断言。
- `webapp/src/lib/transcript` memory 事件测试。

### Checklist

- [ ] 为 memory 单写行为编写失败测试。
- [ ] 将 task events 侧改为 `EmitLive`。
- [ ] 保留 audit 低频事件。
- [ ] 运行聚焦测试并确认通过。
- [ ] 记录任务 checkpoint commit。

## TASK-09: Attachment GC 修复

### Goal

过期 sent attachment 能可靠回收，删除失败可重试，历史遗留 `expired` 记录和孤立文件可清理，且不影响 conversation。

### Acceptance

- GC 先删除存储对象和 `.meta.json`，成功后再更新 DB 状态。
- `ErrObjectNotFound` 视为删除成功。
- 文件删除失败时保留原状态，下一轮继续重试。
- 历史 `status=expired` 但文件仍存在的记录会再次清理。
- 孤立附件 dry-run 能识别 conversation 不存在、无消息引用、文件无记录等场景。
- GC 不删除或修改 conversation message。

### Dependencies

- None — can start immediately.

### Test seam

- `core/attachments` store/filesystem GC 测试。
- 删除失败重试与 orphan dry-run 测试。
- `app/commands` attachment GC loop 测试。

### Checklist

- [ ] 为新的删除顺序和重试语义编写失败测试。
- [ ] 调整 GC 事务顺序并处理历史 `expired` 记录。
- [ ] 实现孤立附件 dry-run 与清理能力。
- [ ] 运行聚焦测试并确认通过。
- [ ] 记录任务 checkpoint commit。

## TASK-10: Session GC

### Goal

过期的 `user_sessions` 由后台任务定期清理，避免登录记录无限增长。

### Acceptance

- 后台 loop 删除 `expires_at <= now` 的 session。
- 未过期 session 保留。
- `sessionRetention` 可配置，默认 720h，`retention <= 0` 关闭。
- 当前用户请求路径的过期 session 行为不变。

### Dependencies

- None — can start immediately.

### Test seam

- `app/logics` auth/session 清理测试。
- `app/commands` session GC loop 测试。
- 配置解析测试。

### Checklist

- [ ] 为 session 过期清理编写失败测试。
- [ ] 实现按 `expires_at` 的批量删除与 loop。
- [ ] 增加配置项与装配。
- [ ] 运行聚焦测试并确认通过。
- [ ] 记录任务 checkpoint commit。

## TASK-11: 优雅关闭 checkpoint

### Goal

服务优雅关闭时执行 `PRAGMA wal_checkpoint(TRUNCATE)`，日常收掉 WAL 残留，不执行 VACUUM。

### Acceptance

- shutdown 流程在 HTTP 停止后执行 `wal_checkpoint(TRUNCATE)`。
- shutdown checkpoint 不执行 `VACUUM`。
- 关闭后 `-wal` 文件残留明显减小或消失。
- 正常启动、运行和退出不受影响。

### Dependencies

- None — can start immediately.

### Test seam

- `app/commands` serve/graceful shutdown 测试。
- 可注入 SQLite 连接或等价抽象验证 checkpoint 调用。

### Checklist

- [ ] 为 shutdown checkpoint 编写失败测试。
- [ ] 在关闭钩子中接入 checkpoint。
- [ ] 运行聚焦测试并确认通过。
- [ ] 记录任务 checkpoint commit。

## TASK-12: 维护命令与历史治理

### Goal

提供 `ice_art storage-maintenance` 的 dry-run、apply、vacuum 模式，并在确认 dry-run 结果后执行一次历史数据治理和最终验证。

### Acceptance

- `--dry-run` 输出待清理对象与预计空间，不修改任何数据。
- `--apply` 清理历史 task events、冗余 audit artifact、过期 audit run、workspace、attachment、session。
- `--vacuum` 只在显式维护窗口执行，不在热路径运行。
- 历史治理后 conversation、conversation message、task 快照仍存在。
- 全量 Go 测试、构建、前端类型检查与相关 Vitest 全部通过。

### Dependencies

- `TASK-02`
- `TASK-04`
- `TASK-05`
- `TASK-06`
- `TASK-07`
- `TASK-08`
- `TASK-09`
- `TASK-10`
- `TASK-11`

### Test seam

- `app/commands` 维护命令测试。
- 历史数据迁移与清理集成测试。
- 全量验证命令：`go test ./...`、`go build ./cmd/...`、`go list ./...`、`pnpm --dir webapp exec vue-tsc -b`、相关 Vitest。

### Checklist

- [ ] 为 dry-run/apply/vacuum 编写失败测试。
- [ ] 实现维护命令与历史数据治理步骤。
- [ ] 运行一次 dry-run 并记录结果。
- [ ] 用户确认后执行 apply 与 vacuum。
- [ ] 运行全量验证并确认通过。
- [ ] 记录任务 checkpoint commit。

## 评审问题

执行前请确认：

- 任务粒度是否合适，是否需要合并或拆分。
- 依赖边是否准确，特别是 `TASK-04`/`TASK-08` 依赖 `TASK-03`，`TASK-06` 依赖 `TASK-05`。
- `TASK-12` 作为最终历史治理任务是否需要在用户确认 dry-run 后再执行。

未获批准前不开始任何实现。
