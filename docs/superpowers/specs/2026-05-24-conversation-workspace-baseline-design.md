# 会话级工作区与 baseline 合并提示设计

## 背景

当前最新实现把每一轮 `agent.run` 任务映射成一个独立 task workspace。实际体验上，这会让一次会话的连续对话产生多个待合并对象，用户需要按轮次处理工作区，成本过高。产品期望是：一个会话只拥有一个可写工作区，后续轮次在同一个会话工作区内累积修改，最终由用户一次性合并回自己的 `home workspace`。

现有前端待合并提示不是一次性 toast，而是按 `pendingWorkspaceMergeTaskIdByConversation` 做本地映射；在同一浏览器 localStorage 未丢失时，切回原会话仍会显示。但它缺少后端真源同步：如果换浏览器、清空 localStorage，或后端已有 pending merge 而前端没有缓存，提示可能消失。因此本次设计把待合并状态改为以后端 workspace 状态为准，前端缓存只做加速和恢复辅助。

## 目标

- `mutable` 模式下，一个 conversation 只对应一个可写 workspace。
- 后端 task 仍按每轮运行创建；task 不再等同于 workspace 隔离单元。
- 一个会话的多轮修改在同一个 conversation workspace 内累积。
- 合并目标始终是用户的 `home workspace`。
- workspace 未发生实际文件变化时，不提示合并。
- 同一用户同一时间只允许一个 conversation workspace 处于 `pending_merge`。
- 切换会话、刷新页面、换浏览器后，未合并状态仍能通过后端查询恢复提示。
- 合并 UI 从 composer 上方大横幅收敛为“可写 / 只读”模式切换旁的小型确认控件。

## 非目标

- 不重做 `core/tasks` 的任务生命周期。
- 不实现文件级冲突合并；确认仍是整目录回写 `home workspace`。
- 不把 `readonly` 升级成 OS 级只读沙箱。
- 不在本次引入数据库表存储 workspace manifest；第一版继续使用 workspace 内部 sidecar 文件。
- 不清理历史 task workspace 目录；旧目录继续保留，兼容已有状态。

## 术语

- **home workspace**：用户长期持有的个人工作区，合并目标。
- **conversation workspace**：某个会话的可写工作区，多轮 `mutable agent.run` 复用同一目录。
- **baseline manifest**：conversation workspace 在最近一次与 home 同步后的文件清单，用于判断后续是否发生实际修改。
- **pending merge**：当前 conversation workspace 相比 baseline 有差异，等待用户确认合并或丢弃。
- **task**：后端每轮执行任务，仍由 `core/tasks` 管理，不作为可写 workspace 的长期身份。

## 架构决策

### Workspace 身份

`mutable` 模式下，executor 解析 workspace id 时优先使用 `RunTaskInput.ConversationID`。没有 conversation id 的兼容路径继续 fallback 到 `task.ID`。这样不改任务系统，又能把可写 workspace 粒度从“每轮 task”降到“每个 conversation”。

第一版可以继续复用当前磁盘布局：

```text
data/workspaces/
  users/
    {user_id}/
      home/
      tasks/
        {conversation_id}/
      backups/
```

这里 `tasks/{conversation_id}` 的目录名暂时保留，是为了减少迁移和周边测试改动；代码内部应逐步使用 `workspace_id` / `conversation_id` 命名，避免继续扩大 `TaskID` 语义。

### Baseline manifest

conversation workspace 首次创建时，从 home 拷贝当前内容，并生成 baseline manifest。后续成功完成 `mutable agent.run` 时，manager 重新扫描 workspace 当前 manifest，与 baseline 对比：

- manifest 一致：workspace 状态不进入 `pending_merge`，前端不提示合并。
- manifest 不一致：workspace 状态进入或保持 `pending_merge`，前端显示小型确认控件。

manifest sidecar 建议命名为 `.workspace-baseline.json`。扫描时排除：

- `.workspace-state.json`
- `.workspace-baseline.json`

manifest 条目按路径排序，使用 slash 路径。建议记录目录和文件：

```json
{
  "version": 1,
  "entries": [
    { "path": "AGENTS.md", "kind": "file", "size": 123, "sha256": "..." },
    { "path": "skills", "kind": "dir" },
    { "path": "skills/debugging/SKILL.md", "kind": "file", "size": 456, "sha256": "..." }
  ]
}
```

manifest 扫描遇到 symlink 应返回错误，不跟随链接。文件权限变化第一版不作为“是否提示合并”的依据，判断重点是路径、目录存在性和文件内容。

### 累积修改

当某个 conversation workspace 已经是 `pending_merge` 时，用户可以继续在该会话发送新一轮 `mutable agent.run`。新一轮运行继续使用同一个 workspace，修改继续累积。任务成功结束后再次对比 baseline；只要仍有差异，就保持 `pending_merge`。

### Home 级合并互斥

所有 conversation workspace 最终都会整目录回写同一个用户的 `home workspace`。在不做文件级冲突合并的前提下，`pending_merge` 必须被视为用户 home 级互斥资源：

- 如果会话 A 已经处于 `pending_merge`，会话 A 仍允许继续发送 `mutable agent.run` 并累积修改。
- 如果会话 A 已经处于 `pending_merge`，会话 B 不允许再启动新的 `mutable agent.run`。
- 会话 B 可以使用 `readonly` 模式继续提问，但 readonly 不回写 home、不进入 merge 流程。
- 后端拒绝会话 B 的 mutable 请求时应返回明确错误，提示先合并或丢弃会话 A 的工作区。

这条规则避免多个 conversation workspace 同时等待合并后，由“后点合并”的会话整目录覆盖前一个会话已经合并的结果。

历史数据、异常恢复或并发竞争仍可能让多个 workspace 同时进入 `pending_merge`。因此 confirm 前还必须做冲突保护：重新计算当前 home manifest，并与该 conversation workspace 的 baseline manifest 比较。若不一致，说明 home 已被其他会话或外部流程更新，confirm 必须失败并返回 `409 Conflict`，不得执行整目录覆盖。用户需要先丢弃该 workspace，或在更新后的 home 上重新运行相关请求。

### Confirm / discard 语义

确认合并：

1. 校验 conversation workspace 属于当前用户。
2. 重新计算当前 home manifest，并确认它与 conversation workspace 的 baseline manifest 一致。
3. 若 manifest 不一致，返回 `409 Conflict`，不覆盖 home。
4. 备份当前 home 到 `backups/`。
5. 用 conversation workspace 整目录覆盖 home，排除内部 sidecar 文件。
6. 重新生成 baseline manifest，代表“当前 workspace 已与 home 同步”。
7. 将 workspace 状态标记为 `merged`。

丢弃变更：

1. 校验 conversation workspace 属于当前用户。
2. 用当前 home 重新覆盖 conversation workspace。
3. 重新生成 baseline manifest。
4. 将 workspace 状态标记为 `discarded`。

丢弃后继续发送新消息时，仍复用同一个 conversation workspace 目录，只是内容已经恢复为 home。

### Readonly 边界

本次会话级累积主要针对 `mutable` workspace。`readonly` 仍然表示“不回写 home、不产生合并提示”，不是 OS 级只读。为了避免用户在已有 pending merge 的会话里切到 readonly 时误丢可写改动，第一版不让 readonly 复用并重置 conversation workspace。实现可继续使用一次性 workspace 或当前已有 discard 路径，但最终状态不得触发合并提示。

后续如果要严格 readonly，应单独设计工具权限或沙箱挂载策略。

## 后端接口

需要新增以 conversation 为入口的 workspace 状态查询接口，作为前端提示的真源：

```http
GET /api/v1/conversations/{conversation_id}/workspace
```

返回：

- 没有 workspace 或没有待处理状态：返回 `null` 或 state 为 `completed/merged/discarded`。
- 有未合并修改：返回 `state = pending_merge`，包含 `workspace_id`、`conversation_id`、`mode`、`updated_at` 等字段。

确认和丢弃建议新增 conversation 语义接口：

```http
POST /api/v1/conversations/{conversation_id}/workspace/confirm
POST /api/v1/conversations/{conversation_id}/workspace/discard
```

现有 task 语义接口保留兼容：

```http
POST /api/v1/tasks/{id}/workspace/confirm
POST /api/v1/tasks/{id}/workspace/discard
```

兼容接口内部应从 task input 解析 `conversation_id`，优先操作 conversation workspace；缺失时 fallback 到历史 task workspace。

## 前端行为

- 切换会话或加载会话时，调用 conversation workspace 状态接口。
- localStorage 中的 pending 映射只做缓存，不能作为唯一来源。
- 当前会话存在 `pending_merge` 时，在“可写 / 只读”切换旁显示小型确认控件。
- 控件文案保持短：例如 `待合并`、`合并`、`丢弃`。
- 合并或丢弃成功后，立即用接口返回状态清理当前会话提示，并刷新会话列表。
- 如果后端判定无变更，前端不显示任何合并提示。

## 错误处理

- baseline manifest 缺失时，优先从当前 workspace 生成一次 baseline，并按“无差异”处理，避免历史 workspace 首次加载时误提示。
- manifest 扫描失败时，`mutable` 成功任务应返回清晰错误并保留 workspace，不自动合并。
- confirm 失败且 home 覆盖已开始时，必须尝试从 backup 恢复 home，并把失败原因写入 state。
- confirm 前发现 home manifest 与 workspace baseline 不一致时，返回 `409 Conflict`，不创建 backup、不覆盖 home。
- conversation workspace 查询时，如果会话不属于当前用户，返回 401/403，不泄露路径。
- task confirm/discard 兼容接口找不到 conversation id 时，继续使用 task id，以保护历史任务。

## 测试要求

- 同一个 conversation 连续两轮 `mutable agent.run` 复用同一个 workspace root。
- 第二轮在第一轮 pending merge 未处理时继续累积修改。
- 同一用户已有其他 conversation workspace 处于 `pending_merge` 时，新 conversation 的 `mutable agent.run` 被拒绝。
- 同一用户已有其他 conversation workspace 处于 `pending_merge` 时，新 conversation 的 `readonly agent.run` 不被拒绝。
- confirm 前如果当前 home manifest 已不同于该 workspace baseline，返回 `409 Conflict` 且不覆盖 home。
- workspace 相比 baseline 无变化时，任务成功但不进入 `pending_merge`。
- workspace 相比 baseline 有变化时，任务成功后进入 `pending_merge`。
- confirm 后更新 home，更新 baseline，并清理前端提示。
- discard 后从 home 恢复 workspace，更新 baseline，并清理前端提示。
- 切换会话后从后端恢复 pending merge 提示，不依赖 localStorage。
- readonly 不触发 pending merge。
- task confirm/discard 兼容接口优先映射到 conversation workspace。

## 交付前校验

- 后端聚焦：`go test ./core/workspaces ./core/agent ./app/handlers`
- 后端全量：`go test ./...`
- 前端聚焦：`pnpm --dir webapp exec vitest run src/lib/api.spec.ts src/lib/chat-state.spec.ts src/views/ChatView.spec.ts`
- 前端类型检查：`pnpm --dir webapp exec vue-tsc -b`
