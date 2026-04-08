# Skills And Memory Runtime Context Rework Spec

## 1. 背景

当前 runtime 在 `skills`、短期记忆压缩和提示词角色语义上混用了同一条 `system prompt` 注入链：

- `core/agent/executor.go` 会把用户手动选择的 skill 解析为完整 `SKILL.md` 正文并注入 prompt
- `core/prompt/resolver.go` / `core/runtimeprompt/builder.go` 会把这些运行时内容统一变成 `system` 消息
- `core/memory/manager.go` 压缩旧上下文后，同样把压缩记忆作为 `system` 摘要注入

这会带来三个问题：

1. 手动选择 skill 并不等于本轮一定需要整份技能全文，完整正文提前注入会稀释用户问题。
2. 压缩后的旧记忆和 skill 正文都可能盖住最近一次真实用户提问，模型容易在大段 guide 文本里迷路。
3. `skills`、压缩记忆、强规则提示词被混成同一种 `system` 语义，模型无法区分“必须遵守的规则”和“按需参考的 guide”。

本规格将这三条问题统一收敛为一次 runtime context 重构。

## 2. 目标

- 让 `system` 只承载强规则，不再承载可选 guide。
- 将手动选择的 skill 改为“摘要注入 + 按需全文加载”。
- 保证最近一次真实用户提问永远不会被并入旧记忆压缩区。
- 让 `using_skills` 返回的 skill 正文只影响当前轮推理，不污染会话持久化和短期记忆。
- 收敛 skill 元数据语义：前端与模型都以 `name` 为主，不再使用正文 H1 `title`。

## 3. 非目标

- 不做自动技能推荐。
- 不做 skill 依赖、继承、嵌套 include。
- 不做 skill 正文缓存或数据库持久化。
- 不引入新的消息角色；仍只使用 provider 已支持的 `system` / `user` / `assistant` / `tool`。
- 不改变已有 `AGENTS.md` / DB prompt / forced prompt 作为强规则的定位。

## 4. 核心设计

### 4.1 运行时上下文拆成两条链

运行时上下文分为两类：

1. Enforced system chain
   - 用于强规则
   - 继续走现有 `ResolvedPrompt` / `runtimeprompt.Envelope` 机制
   - 典型来源：forced blocks、DB prompt、`AGENTS.md`、`step_pre_model`、`tool_result` prompt

2. Synthetic conversation chain
   - 用于非强制、仅运行时生效的辅助消息
   - 作为 conversation body 的前导消息插入，不进入持久化历史
   - 典型来源：压缩记忆 recap、用户手动选择 skills 的摘要、`using_skills` 的临时 tool 结果

### 4.2 角色语义

- `system`
  - 仅用于强规则和必须生效的运行时约束
  - 例如：forced prompt、DB prompt、工作目录 `AGENTS.md`
- `assistant`
  - 用于压缩后的旧上下文 recap
  - 语义是“前情提要”，不是“必须遵守的规则”
- `user`
  - 用于用户手动选择的 skill 摘要
  - 语义是“本轮可参考的 guide / 线索”，不是系统强约束
- `tool`
  - 用于 `using_skills` 返回的 skill 正文
  - 该 tool 结果只在当前轮推理中可见，属于临时参考材料

### 4.3 注入顺序

每轮发送给模型的消息顺序固定为：

1. `system` chain
2. synthetic `assistant` recap
3. synthetic `user` skill summary
4. 保留的真实会话 tail

其中真实会话 tail 的最后一条必须是最近一次真实用户提问，除非当前轮是 tool continuation。

`step_pre_model` 仍属于 `system` chain；`tool_result` 的插入位置保持现有 after-tool-turn 语义，不在本次设计里改变。

## 5. Skills 设计

### 5.1 Skill 元数据

`skills/<name>/SKILL.md` 的正文只用于按需读取，不再从正文提取结构化元数据。

面向 runtime 注入和前端展示的 skill 摘要只使用头部 frontmatter 的：

- `name`
- `description`

同时保留以下约束：

- skill 的唯一标识仍然是目录名
- frontmatter 中若存在 `name`，必须与目录名一致
- `description` 若缺失，则保持为空字符串，不再回退到正文首段
- 不再提取正文 H1 作为 `title`
- 前端下拉、模型摘要、`skills` 请求参数统一使用 `name`

现有 `title` 字段从后端 API、Swagger、前端类型中移除；若某些内部结构短期保留该字段，也不得再参与展示、摘要或 prompt 组装。

### 5.2 手动选择 skill 的运行时行为

当用户在聊天界面手动选择一个或多个 skill 时：

- 不再注入完整 `SKILL.md`
- 只为已选择的 skill 生成一条 synthetic `user` 摘要消息
- 未被选择的 skill 不进入本轮上下文

该摘要消息的语义要求：

- 明确这些内容是 guide，不是强制规则
- 明确如需完整内容，应调用 `using_skills(name)`
- 只包含 `name` 和 `description`

建议文案示例：

```text
Selected workspace skills are available as optional guides for this request.
Use using_skills(name) only when a selected skill is actually relevant.

- debugging: 系统化排查问题的技能
- review: 面向风险和回归检查的代码评审技能
```

### 5.3 `using_skills` builtin tool

新增 builtin 工具：`using_skills`

入参：

```json
{
  "name": "debugging"
}
```

返回 JSON：

```json
{
  "name": "debugging",
  "description": "系统化排查问题的技能",
  "source_ref": "skills/debugging/SKILL.md",
  "directory": "E:/.../skills/debugging",
  "resource_refs": ["skills/debugging/SKILL.md", "skills/debugging/checklist.md"],
  "content": "# Debugging\n..."
}
```

语义约束：

- `content` 返回完整 `SKILL.md` 正文
- `directory` 必须返回，便于模型继续访问 skill 包内脚本和附加文档
- `resource_refs` 返回 skill 目录下可读资源的相对路径列表；不需要递归暴露隐藏文件或不安全路径
- 工具不做模糊匹配，只接受精确的 `name`
- 未找到 skill 时返回明确错误，不静默回退

### 5.4 `using_skills` 的可见性与持久化

`using_skills` 的输出必须是 `ephemeral tool output`：

- 当前轮中，tool 结果要进入后续模型推理上下文
- 但该结果不能进入持久化会话历史
- 不能进入 `memory.Manager` 的短期消息
- 不能被后续压缩到 recap 中

等价地说，`using_skills` 只影响“当前运行中的后续 step”，不影响“下一轮会话的历史基线”。

审计层可以记录：

- 调用了哪个 skill
- 调用时机
- 输出大小

但不要求在 transcript 中保存完整正文。

## 6. 短期记忆设计

### 6.1 记忆 recap 角色

压缩后的旧上下文 recap 不再以 `system` 注入，而是 synthetic `assistant` 消息。

原因：

- 旧记忆是上下文提要，不是规则
- `assistant` 更符合“历史状态回顾”的语义
- 可以显式降低其对当前问题的优先级压制

### 6.2 最近一次真实用户提问保护规则

最近一次真实 `user` 消息必须被硬保护：

- 正常情况下，最后一条真实 `user` 消息不得进入压缩前缀
- 压缩范围只能发生在它之前的历史区间
- 若 provider replay 需要更多上下文，允许在它之前保留最小 replay 后缀，但不能跨过最后一条真实用户消息

### 6.3 若最后一条用户消息单条超预算

若最后一条真实 `user` 消息本身已超出允许预算：

- 不允许把它并入旧历史 recap
- 需要单独压缩为一条新的 synthetic `user` replay 消息
- 该 replay 消息仍然放在最终请求的最尾部
- 它与旧历史 recap 分开处理

因此最终结构会变成：

`system chain -> assistant recap -> user skill summary -> preserved tail -> compressed latest user replay`

而不是：

`system chain -> mixed summary containing old history and latest user`

### 6.4 Memory 输出结构

`memory.RuntimeContext` 需要从当前的：

```go
type RuntimeContext struct {
    Summary *model.Message
    Body    []model.Message
}
```

重构为表达更清晰的结构，例如：

```go
type RuntimeContext struct {
    Recap *model.Message
    Tail  []model.Message
}
```

skills 摘要不属于 memory，本次不塞进 `memory.RuntimeContext`；它应由 agent runtime 在 memory 之外单独拼成 synthetic `user` 消息。

## 7. 运行时组装设计

### 7.1 责任边界

- `core/memory`
  - 只负责真实会话消息的短期压缩与 tail 保留
  - 不关心 skills
- `core/skills`
  - 负责 skill 元数据读取、skill 正文读取、selected skill 摘要构造
  - 不再负责把 skill 正文转成 prompt segment
- `core/agent`
  - 负责把 memory recap 与 skill summary 组装成 synthetic conversation prelude
- `core/runtimeprompt`
  - 继续负责 system chain 和 tool-result prompt 的组织
  - 不再负责 `MemorySummary` 这类非 system 内容

### 7.2 请求组装流程

建议的新流程：

1. executor 持久化真实用户消息
2. executor 解析本轮 selected skills 的摘要元数据
3. runner/memory 只基于真实会话历史生成 `Recap + Tail`
4. runner 在发送请求前构造 synthetic conversation prelude：
   - `Recap` as `assistant`
   - selected skill summary as `user`
5. runtimeprompt 只渲染 `system` chain
6. renderer 按顺序输出：`system chain + synthetic prelude + tail`

## 8. API 与前端契约

### 8.1 Skills API

列表和详情接口继续保留：

- `GET /api/v1/skills`
- `GET /api/v1/skills/:name`

但响应契约调整为：

- `name` 为主显示字段
- 删除 `title`
- `description` 仅来自 frontmatter
- 详情接口继续返回 `content` / `source_ref` / `resource_refs`

### 8.2 聊天前端

- skills 下拉显示 `name`
- 选择值仍为 `name`
- 发送 `agent.run` 时透传 `skills: string[]`
- 不在前端预读 `SKILL.md` 全文

## 9. 兼容性与迁移

- 这是一次同仓后端 + 前端联动调整，不要求兼容旧的 `title` 展示语义
- 若旧测试依赖 `title`、正文首段回退或完整 skill prompt 注入，需同步更新
- 当前会话历史里若已有旧版 skill 注入痕迹，不做回写清理；新实现只影响后续运行

## 10. 验收标准

满足以下条件视为完成：

1. 选中 skill 后，请求中不再出现完整 `SKILL.md` system 注入。
2. 选中 skill 后，请求中出现一条 synthetic `user` skill 摘要消息。
3. 模型可通过 `using_skills(name)` 读取完整正文与 skill 目录信息。
4. `using_skills` 结果不会写入 conversation store，也不会出现在 memory recap 中。
5. 压缩后最近一次真实用户提问仍能在最终请求尾部看到。
6. 若最后一条用户消息过长，会生成单独的压缩 user replay，而不是与旧历史混压。
7. 前端 skills 下拉显示 `name`，不显示正文 H1 `title`。
8. Skills API 不再依赖正文 H1/首段来生成摘要字段。
