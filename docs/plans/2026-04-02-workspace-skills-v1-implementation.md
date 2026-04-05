# Workspace Skills V1 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 为 runtime 增加一个类似 Claude Code 的本地技能包系统：从工作目录下的 `skills/` 动态扫描技能，提供只读查询接口、聊天侧手动选择能力，并在 `agent.run` 执行时把指定技能作为运行时上下文注入当前 agent。

**Architecture:** 保持技能为“工作目录内的文件系统资源”，不入数据库，不走后台运营配置。后端新增 `core/skills` 负责扫描、解析、校验与运行时加载；`agent.run` 显式接收 `skills` 列表，由 executor 在 prompt/session 组装阶段注入技能内容；前端新增只读 skills API 消费和聊天输入区手动选择。技能文件格式采用 `skills/<skill-name>/SKILL.md`，frontmatter 可选；无 frontmatter 时自动从目录名和 Markdown 标题/正文推断最小元数据。

**Tech Stack:** Go 1.25、Gin、现有 `core/agent` / `core/prompt` / `core/tasks` / `app/handlers` / `webapp` Vue 3 + TypeScript + Vitest

---

## 0. 背景与现状

当前代码库里已经有两条与本功能强相关的能力链路：

1. 工作目录级规则注入
   - `core/prompt/workspace_loader.go` 已支持从工作目录读取 `AGENTS.md`
   - `core/prompt/resolver.go` 会把工作目录规则与 DB prompt bindings 一起整合到 session prompt 中
   - `core/agent/executor.go` 会在每次 `agent.run` 时调用 prompt resolver

2. 前后端已有聊天任务执行与配置透传链路
   - `app/handlers/task_handler.go` 已能接收 `agent.run` 请求并透传给 task manager
   - `webapp/src/lib/api.ts` 已有 `buildRunTaskRequest(...)`
   - `webapp/src/views/ChatView.vue` 已有按会话维护草稿和发送消息的状态组织方式

这意味着 Workspace Skills V1 不需要发明新的底层执行链路，只需要：
- 在工作目录扫描 `skills/`
- 为技能建立只读模型与 API
- 把显式选择的 skill 内容注入 executor 运行时上下文
- 在 ChatView 增加 skills 选择 UI

---

## 1. 范围、边界与默认决策

### 1.1 本期范围

- 基于工作目录扫描 `skills/` 目录
- 只支持本地文件系统技能包
- 只支持 `skills/<name>/SKILL.md` 规范
- 支持 `SKILL.md` frontmatter 元数据解析
- 无 frontmatter 时自动推断最小元数据
- 提供后端只读 skills 列表/详情 API
- `agent.run` 支持显式传入 `skills: []string`
- executor 将技能内容注入当前运行上下文
- 前端聊天界面支持手动选择一个或多个 skill

### 1.2 明确不做

- 不做数据库版 skills
- 不做 admin skills CRUD
- 不做自动技能推荐或自动匹配
- 不做技能依赖解析、嵌套 include、继承
- 不做子代理 / child task / sub-agent skill 协作
- 不做技能直接执行脚本
- 不做技能资源文件自动拼接
- 不做 skills 的持久化缓存

### 1.3 默认决策

- 技能唯一标识：目录名
- 必需文件：`skills/<name>/SKILL.md`
- frontmatter：可选
- 无 frontmatter：自动推断
- hidden skill：列表隐藏，但显式请求仍允许访问和加载
- skill 注入：只在本轮运行时注入，不写入 conversation history
- 前端选择状态：按 conversation 保存
- skill 不存在：直接报错，不静默忽略
- skill 注入顺序：在 workspace `AGENTS.md` 之后

---

## 2. 文件格式与解析规则

### 2.1 目录规范

```text
skills/
  debugging/
    SKILL.md
  code-review/
    SKILL.md
    examples.md
    checklist.txt
```

### 2.2 `SKILL.md` 头部 frontmatter 规范

支持但不强制：

```md
---
name: debugging
description: 系统化排查问题的技能
tools:
  - grep
  - read
  - bash
tags:
  - debugging
  - analysis
version: v1
hidden: false
---

# Debugging

按以下流程系统化定位问题。
```

### 2.3 字段语义

- `name`
  - 仅作一致性校验
  - 最终唯一标识始终以目录名为准
- `description`
  - 展示用描述
  - 缺失时回退为正文第一段
- `tools`
  - 展示用推荐工具清单
  - v1 不参与运行时强校验
- `tags`
  - 展示与筛选预留字段
- `version`
  - 展示字段
- `hidden`
  - 列表默认过滤，但显式访问可见

### 2.4 无 frontmatter 回退规则

若 `SKILL.md` 没有 frontmatter：

- `name`: 目录名
- `title`: 第一个一级标题 `# `；若没有则回退目录名
- `description`: 标题后的第一段非空正文；若没有则为空字符串
- `hidden`: `false`
- `tools`: 空数组
- `tags`: 空数组

### 2.5 校验规则

- 目录名不能为空
- 目录名不能包含路径分隔符
- 目录名不能为 `.` 或 `..`
- frontmatter 中若存在 `name` 且与目录名不一致，直接报错
- frontmatter YAML 非法时直接报错
- `SKILL.md` 为空白时允许加载，但 `content` 为空会导致运行时注入内容为空；v1 建议在解析阶段直接报错，避免无意义技能

### 2.6 正文注入规则

注入正文时不带 frontmatter 原文，只带清洗后的 Markdown 正文。

统一前缀（刻意使用英文，因为这是给模型看的运行时提示，与 `workspace_loader.go` 中 `workspacePromptPrefix` 的英文前缀保持一致）：

```text
The following skill was loaded from the user's workspace. Treat it as an active skill package for this run.
Skill: <name>
Source: skills/<name>/SKILL.md
---
<skill body>
```

---

## 3. 后端模块设计

### 3.1 新增包：`core/skills`

建议文件：

- `core/skills/types.go`
- `core/skills/errors.go`
- `core/skills/loader.go`
- `core/skills/parser.go`
- `core/skills/resolver.go`
- `core/skills/helpers.go`（仅在实现时确有必要再创建）

### 3.2 核心数据结构建议

```go
type Skill struct {
	Name         string   `json:"name"`
	Title        string   `json:"title"`
	Description  string   `json:"description,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	Tools        []string `json:"tools,omitempty"`
	Version      string   `json:"version,omitempty"`
	Hidden       bool     `json:"hidden,omitempty"`
	SourceRef    string   `json:"source_ref"`
	Directory    string   `json:"-"`
	Content      string   `json:"content,omitempty"`
	ResourceRefs []string `json:"resource_refs,omitempty"`
}

type SkillListItem struct {
	Name        string   `json:"name"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Tools       []string `json:"tools,omitempty"`
	Version     string   `json:"version,omitempty"`
	Hidden      bool     `json:"hidden,omitempty"`
	SourceRef   string   `json:"source_ref"`
}

type ResolvedSkill struct {
	Name        string
	Title       string
	SourceRef   string
	Content     string
	RuntimeOnly bool // 应始终为 true，与 workspace prompt segments 保持一致
}
```

### 3.3 错误定义建议

```go
var ErrSkillNotFound = errors.New("skill not found")
var ErrInvalidSkillName = errors.New("invalid skill name")
var ErrInvalidSkillDocument = errors.New("invalid skill document")
```

所有上抛错误都带 skill 名或路径上下文，例如：

- `resolve skill "debugging": skill not found`
- `parse skill "review": invalid skill document: frontmatter name does not match directory`

---

## 4. API 设计

### 4.1 路由

- `GET /api/v1/skills`
- `GET /api/v1/skills/:name`

### 4.2 列表接口语义

- 扫描当前 workspace 下所有 skills
- 默认过滤 `hidden=true`
- 返回排序后的 skill 列表
- 不返回 `content`

### 4.3 详情接口语义

- 按 `name` 获取 skill 详情
- 允许获取 `hidden=true` skill
- 返回完整 `content`

### 4.4 `agent.run` 入参扩展

在 `RunTaskInput` 中新增：

```go
Skills []string `json:"skills,omitempty"`
```

请求示例：

```json
{
  "task_type": "agent.run",
  "created_by": "alice",
  "input": {
    "conversation_id": "conv_123",
    "provider_id": "openai",
    "model_id": "gpt-5.4",
    "message": "帮我 review 当前仓库",
    "skills": ["code-review", "debugging"]
  }
}
```

### 4.5 输入清洗规则

- `skills` 中每项 `TrimSpace`
- 过滤空字符串
- 去重并保持原顺序
- 若最终为空数组，可省略字段或传空；后端应兼容两者

---

## 5. 运行时注入设计

### 5.1 注入位置

在 `core/agent/executor.go` 的 `NewTaskExecutor` 闭包内部，现有流程为：

1. `json.Unmarshal(task.InputJSON, &input)` — 解析输入
2. `deps.Resolver.Resolve(...)` — 解析 provider/model
3. `deps.PromptResolver.Resolve(...)` — 解析 prompt segments
4. `deps.ConversationStore.EnsureConversation(...)` — 获取会话
5. `NewRunner(client, deps.Registry, Options{...})` — 创建 runner

skills 注入应发生在步骤 3 之后、步骤 5 之前：调用 `SkillsResolver.Resolve(ctx, input.Skills)` 获取 resolved skills，将其作为额外的 `ResolvedPromptSegment`（`Phase: "session"`, `RuntimeOnly: true`）追加到 `resolvedPrompt.Segments` 中，然后传给 `NewRunner`。

注意 executor 是闭包实现（`func(ctx, task, runtime) (output, error)`），不是方法链。不要为 skills 新建独立的中间件或 wrapper，直接在闭包内部的合适位置插入即可。

### 5.2 为什么不把 skills 合并进 `core/prompt`

因为两者边界不同：

- `prompt` 是场景/平台级注入规则，由 DB bindings + workspace 文件组成
- `skill` 是本次 run 显式启用的能力包，由用户在前端选择

首版保持解耦更稳：

- `core/prompt` 继续只负责 prompt bindings 和 workspace 基础规则
- `core/skills` 只负责 skills 扫描、解析和 resolve
- executor 作为装配层合并两者的输出

但两者共享注入载体：resolved skills 最终转换为 `coreprompt.ResolvedPromptSegment`（`SourceKind: "workspace_skill"`, `RuntimeOnly: true`）追加到 `ResolvedPrompt.Segments` 中。这意味着 runner 不需要感知 skills 概念，只看到统一的 prompt segments。

### 5.3 注入相对顺序

实际 `core/prompt/resolver.go` 的 `Resolve()` 方法中，session 阶段的 segments 顺序为：

1. DB default bindings (`phase: session`)
2. legacy system prompt
3. workspace `AGENTS.md`

之后还会追加其他阶段的 bindings：

4. DB default bindings (`phase: step_pre_model`)
5. DB default bindings (`phase: tool_result`)

**selected skills 应作为 `phase: session` 的 segments 追加在 workspace `AGENTS.md` 之后（即位置 3 与 4 之间）。** 具体实现方式：在 executor 中获取 `resolvedPrompt` 后，将 resolved skills 作为额外的 `ResolvedPromptSegment`（`Phase: "session"`, `SourceKind: "workspace_skill"`, `RuntimeOnly: true`）追加到 `resolvedPrompt.Segments` 中。由于 session 阶段的 segments 已经按 `Order` 编号，新追加的 skill segments 的 `Order` 应继续递增。

**重要实现约束：** `ResolvedPrompt.appendSegment()` 是 `core/prompt` 包的未导出方法，`core/agent` 包无法直接调用。有两种解决方式：
1. **推荐：在 `core/prompt` 中新增导出方法** `func (r *ResolvedPrompt) AppendSegment(segment ResolvedPromptSegment)`，供 executor 使用。该方法内部自动递增 `Order` 并同步更新对应的 `Session`/`StepPreModel`/`ToolResult` 切片。
2. 备选：在 executor 中直接操作 `resolvedPrompt.Segments` 切片和 `resolvedPrompt.Session` 切片，但这会打破封装，且需要手动维护 `Order` 计数。

理由：

- `AGENTS.md` 是工作区基础规则
- selected skills 是当前 run 显式启用的附加能力
- 显式启用的内容更适合排在后面，离当前运行更近
- skills 不应影响 `step_pre_model` 和 `tool_result` 阶段的 bindings 顺序

### 5.4 conversation history 策略

skill 注入只在本轮运行时进入模型上下文（通过 `ResolvedPrompt.Segments`，标记 `RuntimeOnly: true`），不调用 `ConversationStore.AppendMessages` 写入持久化消息。

理由：

- 避免污染用户可见消息历史
- 避免多轮对话中重复堆积技能说明
- 更符合 workspace rules / runtime-only guidance 的语义

如需审计，建议记录：

- `skills: ["debugging", "code-review"]`
- `resolved_skills: [{name, source_ref}]`

若现有审计模型不方便扩展，v1 至少在 executor 侧保留明确的注入步骤与日志。

---

## 6. 前端交互设计

### 6.1 交互范围

在 `ChatView.vue` 中增加 skills 选择能力，不新增独立页面。

### 6.2 推荐 UI 形态

- 在 provider/model 选择区域附近增加 `Skills` 多选入口
- 点击后展开下拉或面板
- 每个 skill 展示：
  - `title`
  - `description`
  - `tags`（可选）
- 已选 skills 以 tag/chip 展示

### 6.3 状态组织建议

```ts
const availableSkills = ref<SkillListItem[]>([])
const skillsLoading = ref(false)
const selectedSkillsByConversation = ref<Record<string, string[]>>({})
```

### 6.4 为什么按会话保存技能选择

因为不同 conversation 可能承担不同任务：

- 会话 A：`debugging`
- 会话 B：`code-review`

按会话隔离更符合用户心智，也与 `draftEntriesByConversation` 一致。

### 6.5 错误策略

- skills 列表加载失败：不阻塞基本聊天，但给出非致命提示
- 发送消息时某个 skill 后端校验失败：提示错误并中止本次发送

---

## 7. 逐任务实施计划

### Task 1: 建立 `core/skills` 基础模型与错误定义

**Files:**
- Create: `core/skills/types.go`
- Create: `core/skills/errors.go`
- Test: `core/skills/types_test.go`

**Step 1: Write the failing test**

添加测试，断言：

- `Skill` 和 `SkillListItem` 的 JSON 字段名稳定
- `Skill.Content` 不应出现在列表模型中
- 错误 sentinel 可通过 `errors.Is(...)` 判定

建议测试名：

- `TestSkillListItemJSONShapeIsStable`
- `TestSkillErrorsSupportErrorsIs`

**Step 2: Run test to verify it fails**

Run: `go test ./core/skills -run "TestSkill(ListItem|Errors)"`

Expected: FAIL，因为 `core/skills` 包尚不存在。

**Step 3: Write minimal implementation**

实现：

- `Skill`
- `SkillListItem`
- `ResolvedSkill`
- `ErrSkillNotFound`
- `ErrInvalidSkillName`
- `ErrInvalidSkillDocument`

**Step 4: Run test to verify it passes**

Run: `go test ./core/skills -run "TestSkill(ListItem|Errors)"`

Expected: PASS.

**Step 5: Commit**

```bash
git add core/skills/types.go core/skills/errors.go core/skills/types_test.go
git commit -m "feat: add workspace skill core types"
```

---

### Task 2: 实现目录扫描与路径安全

**Files:**
- Create: `core/skills/loader.go`
- Test: `core/skills/loader_test.go`

**Step 1: Write the failing test**

添加测试，断言：

- `skills/` 不存在时返回空列表
- 扫描多个目录时只识别存在 `SKILL.md` 的 skill
- 按目录名排序
- `Get(name)` 只允许安全目录名
- symlink 目录和 symlink 文件被忽略

建议测试名：

- `TestLoaderListReturnsEmptyWhenSkillsDirectoryMissing`
- `TestLoaderListLoadsWorkspaceSkillsSortedByName`
- `TestLoaderListIgnoresDirectoriesWithoutSkillDocument`
- `TestLoaderGetRejectsInvalidSkillName`
- `TestLoaderListIgnoresSymlinkSkillDirectory`
- `TestLoaderListIgnoresSymlinkSkillDocument`

**Step 2: Run test to verify it fails**

Run: `go test ./core/skills -run "TestLoader"`

Expected: FAIL，因为 loader 还不存在。

**Step 3: Write minimal implementation**

实现：

- `NewLoader(workspaceRoot string) *Loader`
- `func (l *Loader) List(ctx context.Context) ([]SkillListItem, error)`
- `func (l *Loader) Get(ctx context.Context, name string) (*Skill, error)`

规则：

- 根目录固定为 `<workspaceRoot>/skills`
- 只识别一级目录，不递归扫描子目录名作为 skill id
- skill 名由目录名给出
- `SourceRef` 形如 `skills/<name>/SKILL.md`
- 资源文件列表可先不做，或仅作为 `ResourceRefs` 预留

**Step 4: Run test to verify it passes**

Run: `go test ./core/skills -run "TestLoader"`

Expected: PASS.

**Step 5: Commit**

```bash
git add core/skills/loader.go core/skills/loader_test.go
git commit -m "feat: add workspace skill loader"
```

---

### Task 3: 实现 frontmatter 与正文解析

**Files:**
- Create: `core/skills/parser.go`
- Test: `core/skills/parser_test.go`

**Step 1: Write the failing test**

添加测试，断言：

- 有 frontmatter 时正确解析 `description/tools/tags/version/hidden`
- 无 frontmatter 时从标题和正文推断
- 标题缺失时回退目录名
- frontmatter `name` 与目录名不一致时报错
- 非法 YAML 报错
- 空白正文报错
- `tools/tags` 去空去重保序

建议测试名：

- `TestParseSkillDocumentParsesFrontmatterMetadata`
- `TestParseSkillDocumentInfersMetadataWithoutFrontmatter`
- `TestParseSkillDocumentFallsBackTitleToDirectoryName`
- `TestParseSkillDocumentRejectsFrontmatterNameMismatch`
- `TestParseSkillDocumentRejectsInvalidFrontmatterYAML`
- `TestParseSkillDocumentRejectsWhitespaceOnlyBody`
- `TestParseSkillDocumentNormalizesTagsAndTools`

**Step 2: Run test to verify it fails**

Run: `go test ./core/skills -run "TestParseSkillDocument"`

Expected: FAIL，因为 parser 尚不存在。

**Step 3: Write minimal implementation**

实现内部解析函数，例如：

```go
func parseSkillDocument(directoryName string, sourceRef string, content string) (*Skill, error)
```

解析要求：

- frontmatter 可选
- 正文中提取 `# ` 标题
- 正文第一段作为描述回退
- frontmatter 原文不得进入最终 `Skill.Content`
- 最终 `Skill.Name` 始终等于目录名

**Step 4: Run test to verify it passes**

Run: `go test ./core/skills -run "TestParseSkillDocument"`

Expected: PASS.

**Step 5: Commit**

```bash
git add core/skills/parser.go core/skills/parser_test.go
git commit -m "feat: parse workspace skill markdown documents"
```

---

### Task 4: 实现技能 resolver 与运行时注入载体

**Files:**
- Create: `core/skills/resolver.go`
- Test: `core/skills/resolver_test.go`

**Step 1: Write the failing test**

添加测试，断言：

- resolver 能按顺序解析多个 skill
- 返回内容顺序与输入顺序一致
- 重复 skill 会在 resolver 或调用方被去重
- skill 不存在时报错
- `hidden` skill 在显式 resolve 时允许返回
- 生成注入内容时带统一前缀，不包含 frontmatter

建议测试名：

- `TestResolverResolveReturnsSkillsInRequestedOrder`
- `TestResolverResolveReturnsErrorForMissingSkill`
- `TestResolverResolveAllowsExplicitHiddenSkill`
- `TestResolvedSkillContentUsesWorkspacePrefix`

**Step 2: Run test to verify it fails**

Run: `go test ./core/skills -run "TestResolver"`

Expected: FAIL，因为 resolver 尚不存在。

**Step 3: Write minimal implementation**

实现：

```go
type ResolveInput struct {
	Names []string
}

type Resolver struct {
	loader *Loader
}

func NewResolver(loader *Loader) *Resolver
func (r *Resolver) Resolve(ctx context.Context, input ResolveInput) ([]ResolvedSkill, error)
```

实现要求：

- 对输入 names 做最小清洗
- 可复用 loader.Get
- 为每个 skill 构造统一注入前缀

**Step 4: Run test to verify it passes**

Run: `go test ./core/skills -run "TestResolver"`

Expected: PASS.

**Step 5: Commit**

```bash
git add core/skills/resolver.go core/skills/resolver_test.go
git commit -m "feat: add workspace skill resolver"
```

---

### Task 5: 暴露只读 Skills API

**Files:**
- Create: `app/handlers/skill_handler.go`
- Create: `app/handlers/skill_handler_test.go`
- Modify: `app/router/deps.go`
- Modify: `app/router/init.go`
- Modify: `app/commands/serve.go`
- Modify: `app/handlers/swagger_types.go`

**Step 1: Write the failing test**

添加 handler 测试，断言：

- `GET /api/v1/skills` 返回非 hidden skills 列表
- `GET /api/v1/skills/:name` 返回 skill 详情和 `content`
- 不存在 skill 返回 404
- hidden skill 不出现在列表中
- hidden skill 详情可读取

建议测试名：

- `TestSkillHandlerListReturnsVisibleWorkspaceSkills`
- `TestSkillHandlerGetReturnsWorkspaceSkillDetail`
- `TestSkillHandlerGetReturnsNotFoundForUnknownSkill`
- `TestSkillHandlerListFiltersHiddenSkills`
- `TestSkillHandlerGetAllowsHiddenSkillByName`

**Step 2: Run test to verify it fails**

Run: `go test ./app/handlers -run "TestSkillHandler"`

Expected: FAIL，因为 handler 及 wiring 尚不存在。

**Step 3: Write minimal implementation**

实现：

- `SkillHandler` 只读接口，包含 `List` 和 `Get` 方法
- 在 `app/router/deps.go` 的 `Dependencies` 结构中新增 `SkillLoader *coreskills.Loader`（或 `*coreskills.Resolver`）
- 在 `app/router/init.go` 中注册 `SkillHandler` 路由
- 在 `app/commands/serve.go` 中以 `workspaceRoot` 构造 `coreskills.NewLoader(workspaceRoot)` 并传入 `buildRouterDependencies`
- swagger types 增加 `Skill` 列表与详情响应结构

接口约定：

- `GET /skills`
- `GET /skills/:name`

**Step 4: Run test to verify it passes**

Run: `go test ./app/handlers -run "TestSkillHandler"`

Expected: PASS.

**Step 5: Commit**

```bash
git add app/handlers/skill_handler.go app/handlers/skill_handler_test.go app/router/deps.go app/router/init.go app/commands/serve.go app/handlers/swagger_types.go
git commit -m "feat: expose readonly workspace skills api"
```

---

### Task 6: 扩展 `agent.run` 输入结构支持 `skills`

**Files:**
- Modify: `core/agent/executor.go`
- Modify: `app/handlers/task_handler.go`
- Modify: `app/handlers/task_handler_test.go`
- Modify: `app/handlers/swagger_types.go`
- Modify: `core/agent/executor_test.go`

**重要实现细节：** `CreateTaskRequest.Input` 的类型是 `map[string]any`，handler 层不会直接解析 `skills` 字段。`skills` 数组会作为 `map[string]any` 的一部分原样序列化进 `task.InputJSON`，最终在 executor 闭包内通过 `json.Unmarshal(task.InputJSON, &input)` 反序列化到 `RunTaskInput.Skills`。因此：
- handler 层不需要额外提取 `skills` 字段，只需确保 `map[string]any` 能透传 JSON 数组
- 输入清洗（TrimSpace、去空、去重）应放在 executor 侧，在 `json.Unmarshal` 之后统一执行
- swagger types 的 `RunTaskInput` 展示结构需同步新增 `skills` 字段

**Step 1: Write the failing test**

添加测试，断言：

- `RunTaskInput` 能正确反序列化 `skills`
- 空字符串和重复 skill 会被清洗
- handler 的 `CreateTaskRequest` 能透传含 `skills` 的 input map

建议测试名：

- `TestRunTaskInputDeserializesSkillsFromJSON`
- `TestNormalizeSkillNamesTrimsDeduplicatesAndFiltersEmpty`
- `TestTaskHandlerCreateTaskTransparentlyPassesSkillsInInput`

**Step 2: Run test to verify it fails**

Run: `go test ./app/handlers ./core/agent -run "Test(RunTaskInput.*Skills|NormalizeSkillNames|TaskHandlerCreateTask.*Skills)"`

Expected: FAIL，因为 `RunTaskInput` 还没有 `skills` 字段。

**Step 3: Write minimal implementation**

实现：

- `RunTaskInput.Skills []string`（在 `core/agent/executor.go`）
- `normalizeSkillNames(names []string) []string` 清洗 helper（在 `core/skills/helpers.go` 或 `core/agent/executor.go`）
- swagger 请求结构同步新增 `skills`

要求：

- 清洗逻辑集中在一个 helper 函数中
- 前端也应在发送前做一次相同规则的清洗，但以后端为准

**Step 4: Run test to verify it passes**

Run: `go test ./app/handlers ./core/agent -run "Test(RunTaskInput.*Skills|NormalizeSkillNames|TaskHandlerCreateTask.*Skills)"`

Expected: PASS.

**Step 5: Commit**

```bash
git add core/agent/executor.go app/handlers/task_handler.go app/handlers/task_handler_test.go app/handlers/swagger_types.go core/agent/executor_test.go
git commit -m "feat: accept selected skills in agent run input"
```

---

### Task 7: 在 executor 中解析并注入 selected skills

> **注意：** 本任务依赖 Task 4（resolver）和 Task 6（RunTaskInput.Skills 字段）。本任务关注的是 executor 闭包内部的 skills resolve 和 prompt 注入逻辑，而 Task 6 关注的是输入结构扩展。

**Files:**
- Modify: `core/agent/executor.go`
- Modify: `core/prompt/resolver.go`（新增导出的 `AppendSegment` 方法）
- Modify: `app/commands/serve.go`
- Modify: `core/agent/executor_test.go`

**Step 1: Write the failing test**

添加测试，断言：

- 指定 `skills` 时，executor 会解析对应 skill 并将其作为 `ResolvedPromptSegment` 注入
- 注入的 segments 的 `Phase` 为 `"session"`、`RuntimeOnly` 为 `true`
- 注入顺序在 workspace prompts（`AGENTS.md`）之后
- skill 内容不会通过 `ConversationStore.AppendMessages` 写入 conversation history
- 不存在 skill 时整个 run 失败
- 空 `skills` 不影响现有路径

建议测试名：

- `TestAgentExecutorInjectsSelectedSkillsAfterWorkspacePrompts`
- `TestAgentExecutorDoesNotPersistInjectedSkillsIntoConversationHistory`
- `TestAgentExecutorReturnsErrorWhenSelectedSkillIsMissing`
- `TestAgentExecutorRunWithoutSkillsKeepsExistingBehavior`

**Step 2: Run test to verify it fails**

Run: `go test ./core/agent -run "TestAgentExecutor.*Skill"`

Expected: FAIL，因为 executor 还没有 skills resolver 和注入逻辑。

**Step 3: Write minimal implementation**

实现建议：

- 在 `ExecutorDependencies` 中新增 `SkillsResolver *skills.Resolver`（或直接注入 `*skills.Loader`）
- 在 `buildAgentRunExecutorDependencies(...)` 中新增对应参数
- 在 executor 闭包内部，`deps.PromptResolver.Resolve(...)` 之后插入 skills 解析逻辑：
  ```go
  if len(input.Skills) > 0 {
      resolvedSkills, err := deps.SkillsResolver.Resolve(ctx, skills.ResolveInput{Names: normalizeSkillNames(input.Skills)})
      // ...将 resolvedSkills 转换为 ResolvedPromptSegment 追加到 resolvedPrompt
  }
  ```
- 追加的 segments 必须设置 `RuntimeOnly: true`、`Phase: "session"`、`SourceKind: "workspace_skill"`
- 这些 segments 只进入本次 `NewRunner` 的 `Options.ResolvedPrompt`，不调用 `ConversationStore.AppendMessages`

如实现中需要，可新增轻量 helper，例如：

- `normalizeSkillNames(names []string) []string`（如果 Task 6 未放在此处）
- `appendSkillSegments(resolved *coreprompt.ResolvedPrompt, skills []skills.ResolvedSkill)`

**Step 4: Run test to verify it passes**

Run: `go test ./core/agent -run "TestAgentExecutor.*Skill"`

Expected: PASS.

**Step 5: Commit**

```bash
git add core/agent/executor.go core/prompt/resolver.go app/commands/serve.go core/agent/executor_test.go
git commit -m "feat: inject selected workspace skills into agent runs"
```

---

### Task 8: 扩展前端类型与 API

**Files:**
- Modify: `webapp/src/types/api.ts`
- Modify: `webapp/src/lib/api.ts`
- Test: `webapp/src/lib/api.spec.ts`

**Step 1: Write the failing test**

添加测试，断言：

- `fetchSkills()` 能正确解析列表接口响应
- `fetchSkill(name)` 能正确解析详情接口响应
- `buildRunTaskRequest(...)` 传入 `skills` 时会正确写入 `request.input.skills`
- `buildRunTaskRequest(...)` 会对 `skills` 做去空、去重、保序
- `createRunTask(...)` 能正确接受并透传 `skills` 参数

建议测试名：

- `it('fetchSkills returns normalized workspace skills')`
- `it('fetchSkill returns workspace skill detail')`
- `it('buildRunTaskRequest includes selected skills')`
- `it('buildRunTaskRequest normalizes selected skills')`

**Step 2: Run test to verify it fails**

Run: `pnpm --dir webapp exec vitest run src/lib/api.spec.ts -t "skills"`

Expected: FAIL，因为前端还没有 skills 类型和 API 方法。

**Step 3: Write minimal implementation**

实现：

- 在 `webapp/src/types/api.ts` 中新增类型：
  ```ts
  export interface SkillListItem {
    name: string
    title: string
    description?: string
    tags?: string[]
    tools?: string[]
    version?: string
    hidden?: boolean
    source_ref: string
  }

  export interface SkillDetail extends SkillListItem {
    content?: string
    resource_refs?: string[]
  }
  ```
- 在 `webapp/src/types/api.ts` 中扩展 `RunTaskRequest.input`：
  ```ts
  input: {
    conversation_id?: string
    provider_id: string
    model_id: string
    message: string
    created_by: string
    skills?: string[]  // 新增
  }
  ```
- 同步扩展 `TaskInput` 接口，新增 `skills?: string[]`
- 在 `webapp/src/lib/api.ts` 中：
  - 新增 `fetchSkills(): Promise<SkillListItem[]>`
  - 新增 `fetchSkill(name: string): Promise<SkillDetail>`
  - 扩展 `buildRunTaskRequest(...)` 的输入参数类型，新增 `skills?: string[]`
  - 在 `buildRunTaskRequest` 内部将 `skills` 做去空去重后写入 `request.input.skills`

**Step 4: Run test to verify it passes**

Run: `pnpm --dir webapp exec vitest run src/lib/api.spec.ts -t "skills"`

Expected: PASS.

**Step 5: Commit**

```bash
git add webapp/src/types/api.ts webapp/src/lib/api.ts webapp/src/lib/api.spec.ts
git commit -m "feat: add frontend workspace skills api support"
```

---

### Task 9: 在 ChatView 增加技能选择 UI

**Files:**
- Modify: `webapp/src/views/ChatView.vue`
- Optional Create: `webapp/src/components/chat/SkillPicker.vue`
- Test: `webapp/src/views/ChatView.spec.ts` 或 `webapp/src/components/chat/SkillPicker.spec.ts`

**Step 1: Write the failing test**

添加测试，断言：

- 页面初始化时加载 skills 列表
- 用户选择 skills 后，发送消息请求包含选中 skills
- 切换 conversation 时 skills 选择按会话恢复
- skills 接口失败时不阻塞基本聊天

建议测试名：

- `it('loads workspace skills for chat selection')`
- `it('sends selected skills with agent run request')`
- `it('stores selected skills per conversation')`
- `it('keeps chat usable when skills loading fails')`

**Step 2: Run test to verify it fails**

Run: `pnpm --dir webapp exec vitest run src/views/ChatView.spec.ts -t "skills"`

Expected: FAIL，因为 ChatView 还没有 skills 相关状态和 UI。

**Step 3: Write minimal implementation**

实现：

- skills 列表加载（在组件 mount 或首次需要时调用 `fetchSkills()`）
- 多选 UI（下拉/面板形态）
- `selectedSkillsByConversation` 状态管理
- 修改 `handleSend` 函数：将当前会话的 selected skills 传给 `createRunTask`
  - 需要同步修改 `createRunTask` 的参数签名，接受 `skills?: string[]`
  - `createRunTask` 内部已调用 `buildRunTaskRequest`，Task 8 中已扩展该函数
- 将 `selectedSkillsByConversation` 纳入 `saveChatState/loadChatState` 的 localStorage 持久化范围，与 `draftEntriesByConversation` 保持一致

建议先最小实现，不额外抽复杂组件；只有当 `ChatView.vue` 明显过于臃肿时，再把 picker 抽到 `components/chat/SkillPicker.vue`。

**Step 4: Run test to verify it passes**

Run: `pnpm --dir webapp exec vitest run src/views/ChatView.spec.ts -t "skills"`

Expected: PASS.

**Step 5: Commit**

```bash
git add webapp/src/views/ChatView.vue webapp/src/views/ChatView.spec.ts
git commit -m "feat: add chat skill picker"
```

---

### Task 10: 更新 AGENTS.md 反映 Workspace Skills 能力

**Files:**
- Modify: `AGENTS.md`

**Step 1: 确定需要文档化的内容**

列出必须在 AGENTS.md 中更新的条目：

- 在"仓库结构"部分新增 `core/skills` 的说明
- 在"建议优先阅读"部分新增 `core/skills/loader.go`、`core/skills/resolver.go`
- 在"架构规则"部分新增技能系统的边界规则（不入数据库、工作目录文件系统为唯一事实来源）
- 在"已验证命令"部分新增 `go test ./core/skills` 命令

不要在 README.md 中添加详细的技能格式规范——这是面向产品用户的文档，应在产品文档中处理，而非仓库代码级指南。如果用户明确要求独立的 skills 文档，再创建 `docs/skills.md`。

**Step 2: Update AGENTS.md**

更新上述条目，保持中文，遵循现有的格式和风格。

**Step 3: Commit**

```bash
git add AGENTS.md
git commit -m "docs: update AGENTS.md with workspace skills module"
```

---

### Task 11: 运行聚焦与全量验证

**Files:**
- Modify: `docs/plans/2026-04-02-workspace-skills-v1-implementation.md`

**Step 1: Run focused backend tests**

Run: `go test ./core/skills ./core/agent ./app/handlers -run "Test(Loader|ParseSkillDocument|Resolver|SkillHandler|AgentExecutor.*Skill|RunTaskInput.*Skills|NormalizeSkillNames|TaskHandlerCreateTask.*Skills)"`

Expected: PASS.

**Step 2: Run full backend verification**

Run: `go test ./...`

Expected: PASS.

**Step 3: Verify package graph and build**

Run: `go list ./...`

Expected: PASS.

Run: `go build ./cmd/...`

Expected: PASS.

**Step 4: Run frontend verification**

Run: `pnpm --dir webapp exec vue-tsc -b`

Expected: PASS.

Run: `pnpm --dir webapp test`

Expected: PASS.

**Step 5: Inspect collateral impacts**

重点检查：

- `core/agent` 现有 prompt / conversation / audit 测试
- `app/handlers` 现有 task 请求结构测试
- `ChatView` 现有发送消息与会话切换测试

**Step 6: Commit**

```bash
git add docs/plans/2026-04-02-workspace-skills-v1-implementation.md
git commit -m "docs: add workspace skills v1 implementation plan"
```

---

## 8. 实现备注

### 8.1 为什么首版不做缓存

- 工作目录下的技能文件可能随分支切换立即变化
- 不缓存可以保证“改文件即生效”
- 当前数量级下扫描 `skills/*/SKILL.md` 的成本可以接受

若后续发现性能问题，再增加基于文件修改时间的轻量缓存，而不是现在就引入缓存失效复杂度。

### 8.2 为什么不引入 `skill.yaml`

当前决定已经明确：元数据写在 `SKILL.md` 头部 frontmatter 中，避免双文件维护和不一致问题。

### 8.3 hidden 策略为什么这样定

`hidden=true` 的 skill 通常意味着：

- 不想默认展示给所有用户
- 但仍可能被内部流程或显式引用使用

因此“列表隐藏、显式可读”是更稳妥的默认值。

### 8.4 为什么 skill 名坚持以目录名为准

因为这是前后端引用、URL 路由、工作目录路径之间最稳定的一层映射。若允许 frontmatter 改写唯一标识，会引入：

- URL 与目录不一致
- 文件迁移后引用歧义
- Windows/Unix 文件系统大小写行为差异

目录名作为 ID，frontmatter 只作展示和校验，是最简单、最抗歧义的方案。

### 8.5 为什么不把 skill 写进 conversation history

如果写入历史：

- 用户聊天记录会出现大量内部规则文本
- 同一 skill 在多轮对话中会反复堆积
- 不利于 UI 展示和会话简洁性

因此只做 runtime-only 注入更合适。

---

## 9. 建议的最终交付状态

当本计划完成时，应满足以下可观察行为：

1. 在工作目录创建：

```text
skills/debugging/SKILL.md
```

2. 后端可访问：

- `GET /api/v1/skills`
- `GET /api/v1/skills/debugging`

3. 前端聊天界面可看到 `debugging` skill 并手动勾选

4. 发送消息时，请求载荷会带：

```json
"skills": ["debugging"]
```

5. executor 会把 `debugging` 技能正文注入本轮模型上下文

6. 技能正文不会污染 conversation 持久化历史

7. 移除或修改工作目录下 skill 文件后，无需重启服务也能反映变化

---

## 10. Review Checklist

Review 本计划时，重点确认：

- 是否坚持文件系统为唯一事实来源
- hidden 策略是否接受
- skill 注入顺序是否接受
- 前端是否按会话保存选择
- skill 不存在时是否应失败而非忽略
- 是否需要在 v1 就补充 skill 详情预览 UI

---

Plan complete and saved to `docs/plans/2026-04-02-workspace-skills-v1-implementation.md`. Two execution options:

**1. Subagent-Driven (this session)** - I dispatch fresh subagent per task, review between tasks, fast iteration

**2. Parallel Session (separate)** - Open new session with executing-plans, batch execution with checkpoints

Which approach?
