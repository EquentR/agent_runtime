# Conversation Workspace Side Panel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把聊天页的工作区改成可显隐的右侧副工作区，移动端退化成抽屉，同时把工作区文件查看器的视觉密度收紧，避免它继续像一张占屏的大卡片。

**Architecture:** `ChatView.vue` 负责工作区的开关、桌面/移动端布局切换，以及把工作区和聊天主区放进同一个响应式壳里；`WorkspaceBrowserPanel.vue` 只保留文件树、预览、diff 和下载这些内容本身，不再承担“页面卡片”的外层视觉。桌面端用右侧挤压式布局，移动端用覆盖式抽屉，但两种模式都保持同一份组件实例，避免关闭后丢失搜索、展开状态和当前文件选择。

**Tech Stack:** Vue 3, TypeScript, Element Plus, Vue Test Utils, Vitest, existing `webapp/src/style.css` and component-scoped styles.

---

### Task 1: `ChatView` 工作区开关和侧边壳层

**Files:**
- Modify: `webapp/src/views/ChatView.vue`
- Modify: `webapp/src/views/ChatView.spec.ts`

- [ ] **Step 1: 写失败测试**

先把顶部工具栏里的工作区按钮和侧边壳层行为钉死，确认它和“上下文 / 思考过程”在同一行，并且切换时不会把工作区内容卸载掉。

```ts
it('shows a workspace toggle beside the context and thinking controls', async () => {
  api.fetchConversations.mockResolvedValue([
    {
      id: 'conv_1',
      title: 'Workspace chat',
      last_message: 'hello',
      message_count: 1,
      provider_id: 'openai',
      model_id: 'gpt-5.4',
      created_by: 'demo-user',
      created_at: '',
      updated_at: '',
    },
  ])
  api.fetchConversationMessages.mockResolvedValue([{ role: 'assistant', content: 'hello' }])

  const router = makeRouter()
  await router.push('/chat/conv_1')
  await router.isReady()

  const wrapper = mount(ChatView, {
    global: { plugins: [router] },
  })

  await flushPromises()

  const topbarRight = wrapper.get('.topbar-right')
  expect(topbarRight.find('[data-context-stats-trigger]').exists()).toBe(true)
  expect(topbarRight.find('.thinking-toggle').exists()).toBe(true)
  expect(topbarRight.find('[data-workspace-toggle]').exists()).toBe(true)
  expect(wrapper.find('[data-workspace-shell]').exists()).toBe(true)
  expect(wrapper.find('[data-workspace-shell]').isVisible()).toBe(true)
})

it('collapses and reopens the workspace shell without losing panel state', async () => {
  api.fetchConversations.mockResolvedValue([
    {
      id: 'conv_1',
      title: 'Workspace chat',
      last_message: 'hello',
      message_count: 1,
      provider_id: 'openai',
      model_id: 'gpt-5.4',
      created_by: 'demo-user',
      created_at: '',
      updated_at: '',
    },
  ])
  api.fetchConversationMessages.mockResolvedValue([{ role: 'assistant', content: 'hello' }])
  api.fetchConversationWorkspaceSnapshot.mockResolvedValue({
    task_id: 'task_workspace',
    home_root: '/home',
    task_root: '/task',
    tree: [
      { path: 'notes.md', name: 'notes.md', type: 'file', size: 10 },
    ],
  })

  const router = makeRouter()
  await router.push('/chat/conv_1')
  await router.isReady()

  const wrapper = mount(ChatView, {
    global: { plugins: [router] },
  })

  await flushPromises()
  await wrapper.get('[data-workspace-toggle]').trigger('click')
  await flushPromises()
  await wrapper.get('[data-workspace-toggle]').trigger('click')
  await flushPromises()

  expect(wrapper.find('[data-workspace-shell]').exists()).toBe(true)
  expect(wrapper.find('[data-workspace-shell]').isVisible()).toBe(true)
  expect(wrapper.text()).toContain('Workspace')
})
```

Run:

```bash
pnpm --dir webapp exec vitest run src/views/ChatView.spec.ts -t "workspace toggle|workspace shell"
```

Expected: FAIL, because `ChatView` 里还没有新的工作区开关和侧边壳层。

- [ ] **Step 2: 实现最小改动**

在 `ChatView.vue` 里新增一个独立的工作区可见性状态，顶部放一个图标按钮，和上下文统计、思考过程开关同列。桌面端让主聊天区和工作区并排，工作区占右侧固定宽度区间；移动端让工作区变成覆盖式抽屉，配一个遮罩层。工作区内容组件不要再用 `v-if` 反复销毁，改成保留实例并通过展示状态切换，这样搜索词、树展开、当前选中文件都能留住。

建议把布局壳层拆成清晰的类名：
- `chat-stage.workspace-open`
- `chat-stage.workspace-hidden`
- `workspace-shell`
- `workspace-backdrop`
- `workspace-toggle`

桌面宽度建议控制在 `clamp(360px, 34vw, 520px)` 左右，移动端抽屉则跟随现有侧栏的遮罩交互。

- [ ] **Step 3: 跑测试确认变绿**

Run:

```bash
pnpm --dir webapp exec vitest run src/views/ChatView.spec.ts -t "workspace toggle|workspace shell"
```

Expected: PASS。

- [ ] **Step 4: 提交**

```bash
git add webapp/src/views/ChatView.vue webapp/src/views/ChatView.spec.ts
git commit -m "feat: add workspace side panel shell"
```

---

### Task 2: `WorkspaceBrowserPanel` 收紧样式密度

**Files:**
- Modify: `webapp/src/components/chat/WorkspaceBrowserPanel.vue`
- Modify: `webapp/src/components/chat/WorkspaceBrowserPanel.spec.ts`

- [ ] **Step 1: 写失败测试**

先把面板“还是能工作”这件事固定住，再动样式。测试重点不是它长什么样，而是它在更窄的侧边壳里仍然能加载 snapshot、切文件、看 diff、下载文件，并且不把内部状态写死在卡片结构上。

```ts
it('keeps file selection, diff, and download actions working inside a compact shell', async () => {
  api.fetchConversationWorkspaceSnapshot.mockResolvedValue({
    task_id: 'task_1',
    home_root: '/home',
    task_root: '/task',
    tree: [
      { path: 'notes.md', name: 'notes.md', type: 'file', size: 10 },
    ],
  })
  api.fetchConversationWorkspaceFile.mockResolvedValue({
    path: 'notes.md',
    name: 'notes.md',
    binary: false,
    content: 'hello from workspace',
  })
  api.fetchConversationWorkspaceDiff.mockResolvedValue({
    path: 'notes.md',
    binary: false,
    unified_diff: '-old\n+new\n',
    truncated: false,
  })

  const wrapper = mount(WorkspaceBrowserPanel, {
    props: { conversationId: 'conv_1' },
  })

  await flushPromises()
  await wrapper.get('[data-workspace-node="notes.md"]').trigger('click')
  await flushPromises()

  expect(api.fetchConversationWorkspaceFile).toHaveBeenCalledWith('conv_1', 'notes.md')
  expect(api.fetchConversationWorkspaceDiff).toHaveBeenCalledWith('conv_1', 'notes.md')
  expect(wrapper.text()).toContain('hello from workspace')
  expect(wrapper.get('[data-workspace-download]').attributes('href')).toContain('path=notes.md')
})
```

Run:

```bash
pnpm --dir webapp exec vitest run src/components/chat/WorkspaceBrowserPanel.spec.ts -t "compact shell"
```

Expected: FAIL, because组件还没加上更适合侧边栏的密度和数据属性。

- [ ] **Step 2: 实现紧凑化样式**

把 `WorkspaceBrowserPanel.vue` 里的外层“卡片感”拆掉，重点做这几件事：

- 外层边框、圆角和内边距改成更像侧栏的窄壳，而不是独立卡片。
- 标题、搜索栏和 tab 行压缩高度，减少空白区。
- 文件树和预览区保持双栏，但左右比例更偏向预览区，避免树区抢掉主视野。
- 预览区继续保留 `文件 / Diff / 下载`，但按钮尺寸和间距更紧凑。
- 在窄壳里让滚动容器撑满高度，避免内容区被空白撑大。
- 继续保留 `downloadUrl`、`previewMode`、`selectedPath` 和 diff 相关状态，不做功能改写。

如果需要新的标记，优先用 `data-workspace-browser-panel`、`data-workspace-browser-body`、`data-workspace-download` 这类稳定选择器，别把样式测试绑在脆弱的 DOM 顺序上。

- [ ] **Step 3: 跑测试确认变绿**

Run:

```bash
pnpm --dir webapp exec vitest run src/components/chat/WorkspaceBrowserPanel.spec.ts -t "compact shell"
pnpm --dir webapp exec vitest run src/views/ChatView.spec.ts -t "workspace toggle|workspace shell"
```

Expected: PASS。

- [ ] **Step 4: 提交**

```bash
git add webapp/src/components/chat/WorkspaceBrowserPanel.vue webapp/src/components/chat/WorkspaceBrowserPanel.spec.ts
git commit -m "feat: tighten workspace browser styling"
```

---

### Task 3: 响应式回归和本地视觉确认

**Files:**
- Modify: `webapp/src/views/ChatView.spec.ts`
- Modify: `webapp/src/components/chat/WorkspaceBrowserPanel.spec.ts`
- Optional verify only: `webapp/src/views/ChatView.vue`, `webapp/src/components/chat/WorkspaceBrowserPanel.vue`

- [ ] **Step 1: 补响应式断言**

把桌面和移动端的壳层差异写进测试，避免后面只改样式没改交互。

```ts
it('switches the workspace shell to a drawer on narrow screens', async () => {
  setViewportWidth(800)
  api.fetchConversations.mockResolvedValue([
    {
      id: 'conv_1',
      title: 'Mobile workspace chat',
      last_message: 'hello',
      message_count: 1,
      provider_id: 'openai',
      model_id: 'gpt-5.4',
      created_by: 'demo-user',
      created_at: '',
      updated_at: '',
    },
  ])
  api.fetchConversationMessages.mockResolvedValue([{ role: 'assistant', content: 'hello' }])

  const router = makeRouter()
  await router.push('/chat/conv_1')
  await router.isReady()

  const wrapper = mount(ChatView, {
    global: { plugins: [router] },
  })

  await flushPromises()

  expect(wrapper.find('.chat-shell').classes()).toContain('workspace-mobile')
  await wrapper.get('[data-workspace-toggle]').trigger('click')
  await flushPromises()
  expect(wrapper.find('.workspace-backdrop').exists()).toBe(true)
})
```

Run:

```bash
pnpm --dir webapp exec vitest run src/views/ChatView.spec.ts -t "drawer|workspace mobile"
```

Expected: FAIL until the mobile drawer classes and backdrop are wired.

- [ ] **Step 2: 做本地视觉确认**

先确保前端开发服务器在跑：

```bash
pnpm --dir webapp dev --host 0.0.0.0
```

然后在浏览器里打开 `http://localhost:5173`，分别看 1280px 和 800px 两种宽度：

- 1280px：工作区在右侧，不再像独立卡片占一大块垂直面积。
- 800px：工作区切成抽屉，带遮罩，关闭后聊天主区恢复完整宽度。
- 顶部工作区按钮和“上下文 / 思考过程”仍然在同一行。
- 文件树、diff、下载都还在，而且不会因为壳层收紧而溢出。

- [ ] **Step 3: 跑最终回归**

Run:

```bash
pnpm --dir webapp exec vue-tsc -b
pnpm --dir webapp exec vitest run src/views/ChatView.spec.ts src/components/chat/WorkspaceBrowserPanel.spec.ts
pnpm --dir webapp build
```

Expected: 全部退出码为 `0`。

- [ ] **Step 4: 收尾提交**

```bash
git add webapp/src/views/ChatView.spec.ts webapp/src/components/chat/WorkspaceBrowserPanel.spec.ts
git commit -m "test: lock workspace side panel responsiveness"
```

---

## Self-Review Checklist

- Scope coverage:
  - 顶部显隐按钮和侧边壳层：Task 1。
  - 工作区文件查看器的窄壳样式：Task 2。
  - 桌面 / 移动响应式回归与本地视觉确认：Task 3。
- Placeholder scan:
  - 没有 `TBD`、`TODO`、`implement later` 或空泛的“补测试”。
  - 每个会改代码的步骤都给了具体文件和可执行命令。
- Type and behavior consistency:
  - `ChatView.vue` 继续负责布局与可见性状态。
  - `WorkspaceBrowserPanel.vue` 继续只负责内容，不接管外层壳层。
  - 关键稳定选择器建议统一用 `data-workspace-*` 命名，避免测试跟着视觉类名漂移。
