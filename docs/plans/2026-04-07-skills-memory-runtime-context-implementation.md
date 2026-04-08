# Skills And Memory Runtime Context Rework Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 重构 runtime context 组装链，让 selected skills 只注入摘要、`using_skills` 按需返回全文且不持久化，同时保证短期记忆压缩永远保留最近一次真实用户提问。

**Architecture:** 保持现有 `app -> core -> pkg` 分层，不新增新的执行主路径。`core/skills` 改为负责 skill 摘要和正文读取；`core/memory` 只负责真实会话的 recap + tail；`core/agent` 在请求组装时把 recap 和 selected skill summary 拼成 synthetic conversation prelude；`core/tools` 新增支持 ephemeral tool output，供 `using_skills` 在当前轮临时展开 skill 正文。

**Tech Stack:** Go 1.25、Gin、现有 `core/agent` / `core/memory` / `core/runtimeprompt` / `core/tools`、Vue 3 + TypeScript + Vitest

---

### Task 1: 收敛 skill 元数据契约到 `name` / `description`

**Files:**
- Modify: `core/skills/parser.go`
- Modify: `core/skills/types.go`
- Modify: `core/skills/loader.go`
- Modify: `core/skills/resolver.go`
- Modify: `app/handlers/swagger_types.go`
- Modify: `app/handlers/skill_handler_test.go`
- Modify: `core/skills/*_test.go`

**Step 1: Write the failing tests**

补测试覆盖以下行为：

- frontmatter `name` 若存在则必须与目录名一致
- `description` 只来自 frontmatter，不再回退正文首段
- 正文 H1 不再映射为 `title`
- skills API 列表与详情响应不再包含 `title`

最小测试片段示例：

```go
func TestParseSkillDocumentIgnoresBodyTitleAndParagraphForSummary(t *testing.T) {
    skill, err := parseSkillDocument("debugging", "skills/debugging/SKILL.md", "---\nname: debugging\ndescription: debug guide\n---\n\n# Debugging\n\nbody text")
    if err != nil {
        t.Fatal(err)
    }
    if skill.Name != "debugging" || skill.Description != "debug guide" {
        t.Fatalf("unexpected summary: %#v", skill)
    }
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./core/skills ./app/handlers -run "Skill|ParseSkill"`

Expected: FAIL because `title` and body fallback are still in use.

**Step 3: Write minimal implementation**

- 删除 `extractSkillTitle` / `extractFirstParagraph` 驱动的摘要回退
- 从对外结构中移除 `Title`
- 让 resolver 输出的 selected skill 元数据只保留 `name` / `description` / `source_ref`
- 同步 Swagger 文档结构

**Step 4: Run test to verify it passes**

Run: `go test ./core/skills ./app/handlers -run "Skill|ParseSkill"`

Expected: PASS.

**Step 5: Checkpoint**

记录变更范围和需要联动更新的前端契约；不要提交，除非用户明确要求。

### Task 2: 更新前端 skills 展示与 API 类型

**Files:**
- Modify: `webapp/src/types/api.ts`
- Modify: `webapp/src/lib/api.spec.ts`
- Modify: `webapp/src/components/MessageComposer.vue`
- Modify: `webapp/src/components/MessageComposer.spec.ts`
- Modify: `webapp/src/views/ChatView.spec.ts`

**Step 1: Write the failing tests**

补或更新测试覆盖：

- `WorkspaceSkillListItem` 不再要求 `title`
- `MessageComposer` 的 option label 使用 `name`
- ChatView 透传的 selected skill 仍然是 `name[]`

最小断言示例：

```ts
expect(wrapper.findComponent({ name: 'ElSelect' }).props('modelValue')).toEqual([])
expect(wrapper.text()).toContain('debugging')
expect(wrapper.text()).not.toContain('Debugging')
```

**Step 2: Run test to verify it fails**

Run: `pnpm --dir webapp exec vitest run src/components/MessageComposer.spec.ts src/views/ChatView.spec.ts src/lib/api.spec.ts`

Expected: FAIL because the UI and API mocks still reference `title`.

**Step 3: Write minimal implementation**

- 删除前端 skills 类型中的 `title`
- `MessageComposer.vue` 将 `label: s.title` 改为 `label: s.name`
- 更新 API mock 与断言

**Step 4: Run test to verify it passes**

Run: `pnpm --dir webapp exec vitest run src/components/MessageComposer.spec.ts src/views/ChatView.spec.ts src/lib/api.spec.ts`

Expected: PASS.

### Task 3: 把 selected skills 从“全文 prompt 注入”改成“摘要注入”

**Files:**
- Modify: `core/skills/resolver.go`
- Create: `core/skills/summary.go`
- Create: `core/skills/summary_test.go`
- Modify: `core/agent/executor.go`
- Modify: `core/agent/executor_test.go`

**Step 1: Write the failing tests**

增加测试覆盖：

- selected skills 不再把完整 `SKILL.md` 作为 `system` segment 注入
- selected skills 会生成一条 synthetic `user` 摘要消息
- 摘要只包含 selected names 和 descriptions

建议测试方向：

- `executor_test.go` 断言请求消息中不再出现 `The following skill was loaded...`
- 断言请求消息中存在一条 `role=user` 的 runtime-only 摘要消息

**Step 2: Run test to verify it fails**

Run: `go test ./core/agent -run "SelectedSkills|Prompt"`

Expected: FAIL because executor 仍然通过 prompt segments 注入 skill 正文。

**Step 3: Write minimal implementation**

- 把 `core/skills/resolver.go` 改造成 selected skill summary resolver，而不是正文 resolver
- 新增摘要组装 helper，例如：

```go
type SelectedSkillSummary struct {
    Name        string
    Description string
    SourceRef   string
}
```

- executor 只把 summary 传给 runner，不再把 skill 正文 append 到 `ResolvedPrompt`

**Step 4: Run test to verify it passes**

Run: `go test ./core/agent -run "SelectedSkills|Prompt"`

Expected: PASS.

### Task 4: 为工具执行链引入 structured tool output 和 ephemeral 语义

**Files:**
- Modify: `core/tools/register.go`
- Modify: `core/tools/*_test.go`
- Modify: `core/tools/builtin/register.go`
- Modify: `core/tools/builtin/*.go`
- Modify: `core/agent/stream.go`
- Modify: `core/agent/runner_test.go`

**Step 1: Write the failing tests**

增加测试覆盖：

- 普通工具结果仍会进入 `produced`、会进入 memory、会被最终持久化
- 标记为 `ephemeral` 的工具结果会进入当前轮 `baseConversation`，但不会进入 `produced`

建议结构：

```go
type Result struct {
    Content   string
    Ephemeral bool
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./core/tools ./core/agent -run "Ephemeral|Tool"`

Expected: FAIL because当前工具执行只返回 `string`，没有结果模式。

**Step 3: Write minimal implementation**

- 将 `tools.Handler` / `Registry.Execute` 改为返回结构化结果
- builtin 工具默认返回 `Ephemeral: false`
- `stream.go` 中 tool output 进入：
  - `baseConversation`: always
  - `produced` / `memory`: only when `!Ephemeral`

**Step 4: Run test to verify it passes**

Run: `go test ./core/tools ./core/agent -run "Ephemeral|Tool"`

Expected: PASS.

### Task 5: 新增 `using_skills` builtin tool

**Files:**
- Create: `core/tools/builtin/using_skills.go`
- Create: `core/tools/builtin/using_skills_test.go`
- Modify: `core/tools/builtin/register.go`

**Step 1: Write the failing tests**

补测试覆盖：

- `using_skills` 只接受精确 `name`
- 返回 `name` / `description` / `source_ref` / `directory` / `resource_refs` / `content`
- 返回结果标记为 `Ephemeral: true`
- 目录越界、缺失 skill、非法名称时返回清晰错误

最小断言示例：

```go
var payload struct {
    Name      string   `json:"name"`
    Directory string   `json:"directory"`
    Content   string   `json:"content"`
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./core/tools/builtin -run "UsingSkills"`

Expected: FAIL because `using_skills` 工具尚不存在。

**Step 3: Write minimal implementation**

- 基于 `core/skills.Loader` 读取 skill 正文
- 列出 skill 目录下允许暴露的资源引用
- 返回结构化 JSON 文本，结果标记为 `Ephemeral: true`
- 在 builtin register 中注册 `using_skills`

**Step 4: Run test to verify it passes**

Run: `go test ./core/tools/builtin -run "UsingSkills"`

Expected: PASS.

### Task 6: 重构 memory 输出结构并保护最近一次真实用户提问

**Files:**
- Modify: `core/memory/manager.go`
- Modify: `core/memory/manager_test.go`
- Modify: `core/agent/memory.go`
- Modify: `core/agent/memory_test.go`

**Step 1: Write the failing tests**

补测试覆盖：

- recap 改为 `role=assistant`
- 压缩后最后一条真实用户消息仍留在 tail 中
- provider replay 边界可以向前扩展，但不能吞掉最后一条真实用户消息
- 若最后一条用户消息自身超预算，会生成单独的 compressed latest-user replay

建议新增测试名：

- `TestContextMessagesPreservesLatestRealUserMessage`
- `TestContextMessagesCompressesOversizedLatestUserAsStandaloneReplay`

**Step 2: Run test to verify it fails**

Run: `go test ./core/memory ./core/agent -run "ContextMessages|PrepareConversationContext"`

Expected: FAIL because现有 split 逻辑只围绕 provider replay 边界，不保护最后 user。

**Step 3: Write minimal implementation**

- 将 `RuntimeContext` 改成 `Recap + Tail`
- 调整 `splitMessagesForCompression` / `adaptSplitForBudget` 的保护锚点，优先保护最后真实 `user`
- 当最后用户消息过长时，生成单独的 synthetic replay，而不是并入旧 summary

**Step 4: Run test to verify it passes**

Run: `go test ./core/memory ./core/agent -run "ContextMessages|PrepareConversationContext"`

Expected: PASS.

### Task 7: 重构请求组装顺序，明确 system chain 与 synthetic conversation chain

**Files:**
- Modify: `core/agent/memory.go`
- Modify: `core/runtimeprompt/builder.go`
- Modify: `core/runtimeprompt/builder_test.go`
- Modify: `core/runtimeprompt/renderer.go`
- Modify: `core/agent/runtime_prompt_artifact.go`
- Modify: `core/agent/stream_test.go`

**Step 1: Write the failing tests**

补测试覆盖：

- `runtimeprompt.Builder` 不再接收 `MemorySummary string`
- `system` envelope 只包含强规则
- synthetic `assistant` recap 和 synthetic `user` skill summary 作为 body prelude 插在真实 tail 之前
- after-tool-turn 注入位置保持兼容

**Step 2: Run test to verify it fails**

Run: `go test ./core/runtimeprompt ./core/agent -run "RuntimePrompt|Renderer|Memory"`

Expected: FAIL because builder 仍然把 memory summary 当作 `system` segment。

**Step 3: Write minimal implementation**

- 从 `BuildInput` 中移除 `MemorySummary`
- `core/agent/memory.go` 负责组装 request body prelude
- renderer 继续负责 `system + body` 拼接，不重新解释 synthetic 消息语义

**Step 4: Run test to verify it passes**

Run: `go test ./core/runtimeprompt ./core/agent -run "RuntimePrompt|Renderer|Memory"`

Expected: PASS.

### Task 8: 做端到端回归验证

**Files:**
- Modify as needed: `core/agent/*.go`
- Modify as needed: `core/memory/*.go`
- Modify as needed: `core/skills/*.go`
- Modify as needed: `core/tools/builtin/*.go`
- Modify as needed: `webapp/src/**/*`

**Step 1: Run focused backend tests**

Run: `go test ./core/skills ./core/tools ./core/tools/builtin ./core/memory ./core/runtimeprompt ./core/agent ./app/handlers`

Expected: PASS.

**Step 2: Run frontend targeted tests**

Run: `pnpm --dir webapp exec vitest run src/components/MessageComposer.spec.ts src/views/ChatView.spec.ts src/lib/api.spec.ts`

Expected: PASS.

**Step 3: Run frontend type check**

Run: `pnpm --dir webapp exec vue-tsc -b`

Expected: PASS.

**Step 4: Run repository-level validation**

Run: `go test ./...`

Expected: PASS.

**Step 5: Optional build verification**

Run: `go build ./cmd/...`

Expected: PASS.

**Step 6: Checkpoint**

汇总以下验证证据：

- selected skill 不再以 system 正文注入
- `using_skills` 工具可正常返回全文且不持久化
- 最近一次真实用户提问在压缩后仍位于请求尾部
- 前端 skills 下拉显示 `name`
