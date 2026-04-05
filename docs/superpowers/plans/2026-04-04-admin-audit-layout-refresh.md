# Admin Audit Layout Refresh Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refresh `/admin/audit` so the page uses a compact collapsible summary card, a chat-style turn dropdown, complete Chinese event labels, and a detail-first two-column layout.

**Architecture:** Extend the replay bundle timeline entries in `core/audit/replay.go` with a stable `display_name` field, surface it through Swagger and frontend types, then update `AdminAuditView.vue` to consume that field while reusing the existing chat `model-menu` interaction and current audit card styling. Keep the logic local to the audit view and replay builder; do not introduce new shared UI abstractions or browser-native controls.

**Tech Stack:** Go, Gin, GORM, Vue 3 `<script setup>`, TypeScript, Vitest, existing `webapp/src/style.css` design system

---

## File Structure

- Modify: `core/audit/replay.go`
  - Add `DisplayName` to `ReplayEventEntry` and generate Chinese display labels from `event_type`.
- Modify: `core/audit/replay_test.go`
  - Add failing coverage for `display_name`, especially `interaction.requested` and `run.waiting`.
- Modify: `app/handlers/swagger_types.go`
  - Document the new `display_name` field in replay event swagger output.
- Modify: `webapp/src/types/api.ts`
  - Add `display_name?: string` to `AuditReplayEvent`.
- Modify: `webapp/src/views/AdminAuditView.vue`
  - Replace the split summary cards with one collapsible summary card.
  - Replace turn buttons with a chat-style dropdown.
  - Prefer `display_name` for timeline headings and detail headings.
- Modify: `webapp/src/views/AdminAuditView.spec.ts`
  - Add failing tests for collapsed summary, turn dropdown, and display-name fallback behavior.
- Modify: `webapp/src/style.css`
  - Add compact summary/timeline/detail layout rules and reuse the existing `model-menu` visual language.

### Task 1: Add replay event display names in backend

**Files:**
- Modify: `core/audit/replay.go:35-45`
- Modify: `core/audit/replay.go:112-132`
- Modify: `core/audit/replay.go:164-260`
- Test: `core/audit/replay_test.go`

- [ ] **Step 1: Write the failing Go test for replay display names**

Add this test to `core/audit/replay_test.go` near the existing replay bundle tests:

```go
func TestBuildReplayBundleIncludesDisplayNames(t *testing.T) {
	store := newReplayStore(t)
	now := time.Date(2026, time.March, 21, 17, 0, 0, 0, time.UTC)

	run := &Run{
		ID:            "run_display_names",
		TaskID:        "task_display_names",
		TaskType:      "agent.run",
		Status:        StatusWaiting,
		Replayable:    true,
		SchemaVersion: SchemaVersionV1,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := store.db.Create(run).Error; err != nil {
		t.Fatalf("create run error = %v", err)
	}

	events := []*Event{
		{
			RunID:       run.ID,
			TaskID:      run.TaskID,
			Seq:         1,
			Phase:       PhaseRun,
			EventType:   "run.waiting",
			Level:       "info",
			PayloadJSON: json.RawMessage(`{"status":"waiting"}`),
			CreatedAt:   now.Add(time.Second),
		},
		{
			RunID:       run.ID,
			TaskID:      run.TaskID,
			Seq:         2,
			Phase:       PhaseRun,
			EventType:   "interaction.requested",
			Level:       "info",
			PayloadJSON: json.RawMessage(`{"kind":"question"}`),
			CreatedAt:   now.Add(2 * time.Second),
		},
		{
			RunID:       run.ID,
			TaskID:      run.TaskID,
			Seq:         3,
			Phase:       PhaseRun,
			EventType:   "unknown.event",
			Level:       "info",
			PayloadJSON: json.RawMessage(`{"ok":true}`),
			CreatedAt:   now.Add(3 * time.Second),
		},
	}
	for _, event := range events {
		if err := store.db.Create(event).Error; err != nil {
			t.Fatalf("create event seq %d error = %v", event.Seq, err)
		}
	}

	bundle, err := BuildReplayBundle(context.Background(), store, run.ID)
	if err != nil {
		t.Fatalf("BuildReplayBundle() error = %v", err)
	}
	if len(bundle.Timeline) != 3 {
		t.Fatalf("len(bundle.Timeline) = %d, want 3", len(bundle.Timeline))
	}
	if bundle.Timeline[0].DisplayName != "运行等待中" {
		t.Fatalf("timeline[0].DisplayName = %q, want 运行等待中", bundle.Timeline[0].DisplayName)
	}
	if bundle.Timeline[1].DisplayName != "用户交互请求" {
		t.Fatalf("timeline[1].DisplayName = %q, want 用户交互请求", bundle.Timeline[1].DisplayName)
	}
	if bundle.Timeline[2].DisplayName != "unknown.event" {
		t.Fatalf("timeline[2].DisplayName = %q, want unknown.event fallback", bundle.Timeline[2].DisplayName)
	}
}
```

Also extend the existing happy-path assertions in `TestBuildReplayBundleReturnsOrderedTimeline`:

```go
	if bundle.Timeline[0].DisplayName != "运行开始" {
		t.Fatalf("timeline[0].DisplayName = %q, want 运行开始", bundle.Timeline[0].DisplayName)
	}
	if bundle.Timeline[1].DisplayName != "构建 LLM 请求" {
		t.Fatalf("timeline[1].DisplayName = %q, want 构建 LLM 请求", bundle.Timeline[1].DisplayName)
	}
	if bundle.Timeline[2].DisplayName != "工具调用完成" {
		t.Fatalf("timeline[2].DisplayName = %q, want 工具调用完成", bundle.Timeline[2].DisplayName)
	}
```

- [ ] **Step 2: Run the Go replay tests to confirm failure**

Run:

```bash
go test ./core/audit -run 'TestBuildReplayBundle(ReturnsOrderedTimeline|IncludesDisplayNames)$'
```

Expected: FAIL with compile errors like `bundle.Timeline[0].DisplayName undefined`.

- [ ] **Step 3: Add `display_name` to replay entries and generate labels**

Update `core/audit/replay.go`.

First, extend `ReplayEventEntry`:

```go
type ReplayEventEntry struct {
	Seq         int64            `json:"seq"`
	Phase       Phase            `json:"phase"`
	EventType   string           `json:"event_type"`
	DisplayName string           `json:"display_name"`
	Level       string           `json:"level"`
	StepIndex   int              `json:"step_index,omitempty"`
	ParentSeq   int64            `json:"parent_seq,omitempty"`
	CreatedAt   time.Time        `json:"created_at"`
	Payload     json.RawMessage  `json:"payload,omitempty"`
	Artifact    *ArtifactSummary `json:"artifact,omitempty"`
}
```

Then set the field when building timeline entries:

```go
		entry := ReplayEventEntry{
			Seq:         event.Seq,
			Phase:       event.Phase,
			EventType:   event.EventType,
			DisplayName: replayEventDisplayName(event.EventType),
			Level:       event.Level,
			StepIndex:   event.StepIndex,
			ParentSeq:   event.ParentSeq,
			CreatedAt:   event.CreatedAt,
			Payload:     cloneRawJSON(event.PayloadJSON),
		}
```

Add this helper below `summarizeRun` and before `referencedArtifactIDs`:

```go
func replayEventDisplayName(eventType string) string {
	switch strings.TrimSpace(eventType) {
	case "run.created":
		return "运行已创建"
	case "run.started":
		return "运行开始"
	case "run.waiting":
		return "运行等待中"
	case "run.finished":
		return "运行完成"
	case "run.succeeded":
		return "运行成功"
	case "run.failed":
		return "运行失败"
	case "conversation.loaded":
		return "会话已加载"
	case "user_message.appended":
		return "用户消息追加"
	case "step.started":
		return "步骤开始"
	case "step.finished":
		return "步骤完成"
	case "prompt.resolved":
		return "提示词解析"
	case "request.built":
		return "构建 LLM 请求"
	case "model.completed":
		return "模型生成"
	case "tool.started":
		return "工具调用开始"
	case "tool.called":
		return "工具调用"
	case "tool.finished":
		return "工具调用完成"
	case "approval.requested":
		return "审批请求"
	case "approval.resolved":
		return "审批已处理"
	case "interaction.requested":
		return "用户交互请求"
	case "interaction.responded":
		return "用户交互已响应"
	case "messages.persisted":
		return "消息已持久化"
	default:
		trimmed := strings.TrimSpace(eventType)
		if trimmed == "" {
			return "审计事件"
		}
		return trimmed
	}
}
```

- [ ] **Step 4: Run the Go replay tests to verify they pass**

Run:

```bash
go test ./core/audit -run 'TestBuildReplayBundle(ReturnsOrderedTimeline|IncludesDisplayNames)$'
```

Expected: PASS with `ok   github.com/EquentR/agent_runtime/core/audit`.

- [ ] **Step 5: Commit the backend replay change**

Run:

```bash
git add core/audit/replay.go core/audit/replay_test.go
git commit -m "feat(audit): add replay event display names"
```

### Task 2: Surface `display_name` through API docs and frontend types

**Files:**
- Modify: `app/handlers/swagger_types.go:452-463`
- Modify: `webapp/src/types/api.ts:148-158`

- [ ] **Step 1: Write the failing frontend type/doc expectation**

Add this assertion block in `webapp/src/views/AdminAuditView.spec.ts` inside the main audit view test replay payload for `run_2`, updating one timeline item to include the new field and expecting it in the UI later:

```ts
        {
          seq: 5,
          phase: 'run',
          event_type: 'interaction.requested',
          display_name: '用户交互请求',
          level: 'info',
          step_index: 1,
          parent_seq: 4,
          payload: { tool_name: 'search_web', reason: 'requires approval' },
          created_at: '2026-03-22T10:00:05Z',
        },
        {
          seq: 6,
          phase: 'run',
          event_type: 'run.waiting',
          display_name: '运行等待中',
          level: 'info',
          step_index: 1,
          parent_seq: 5,
          payload: { waiting_for: 'approval' },
          created_at: '2026-03-22T10:00:06Z',
        },
```

Also add these expectations after the page loads:

```ts
    expect(wrapper.text()).toContain('用户交互请求')
    expect(wrapper.text()).toContain('运行等待中')
```

This will not compile yet until the type accepts `display_name` and the view uses it.

- [ ] **Step 2: Run the audit view spec to verify failure**

Run:

```bash
pnpm --dir webapp test -- --run src/views/AdminAuditView.spec.ts
```

Expected: FAIL with TypeScript/Vue test errors because `display_name` is not part of `AuditReplayEvent` and the view still renders only `event_type` / `formatEventType(event_type)`.

- [ ] **Step 3: Add the field to Swagger and frontend types**

Update `app/handlers/swagger_types.go`:

```go
type AuditReplayEventSwaggerDoc struct {
	Seq         int64                                 `json:"seq"`
	Phase       string                                `json:"phase"`
	EventType   string                                `json:"event_type"`
	DisplayName string                                `json:"display_name"`
	Level       string                                `json:"level"`
	StepIndex   int                                   `json:"step_index"`
	ParentSeq   int64                                 `json:"parent_seq"`
	CreatedAt   string                                `json:"created_at"`
	Payload     any                                   `json:"payload"`
	Artifact    *AuditReplayArtifactSummarySwaggerDoc `json:"artifact"`
}
```

Update `webapp/src/types/api.ts`:

```ts
export interface AuditReplayEvent {
  seq: number
  phase: string
  event_type: string
  display_name?: string
  level: string
  step_index: number
  parent_seq: number
  created_at: string
  payload?: unknown
  artifact?: AuditReplayArtifactSummary | null
}
```

- [ ] **Step 4: Re-run the audit view spec to confirm the remaining failure is now in the UI logic**

Run:

```bash
pnpm --dir webapp test -- --run src/views/AdminAuditView.spec.ts
```

Expected: FAIL with assertion failures because the component still shows `interaction.requested` / `run.waiting` raw values instead of the new `display_name` content.

- [ ] **Step 5: Commit the type/doc update**

Run:

```bash
git add app/handlers/swagger_types.go webapp/src/types/api.ts webapp/src/views/AdminAuditView.spec.ts
git commit -m "chore(audit): surface replay display name metadata"
```

### Task 3: Rebuild `AdminAuditView` for compact summary and dropdown turns

**Files:**
- Modify: `webapp/src/views/AdminAuditView.vue`
- Test: `webapp/src/views/AdminAuditView.spec.ts`

- [ ] **Step 1: Write the failing audit view tests for collapsed summary and turn dropdown**

Update `webapp/src/views/AdminAuditView.spec.ts`.

First, adjust the mocked run list for `conv_1` to return two runs so the dropdown renders:

```ts
      return [
        {
          id: 'run_1',
          task_id: 'tsk_1',
          conversation_id: 'conv_1',
          task_type: 'agent.run',
          status: 'waiting',
          created_by: 'alice',
          schema_version: 'v1',
          created_at: '2026-03-22T09:00:00Z',
          updated_at: '2026-03-22T09:01:00Z',
        },
        {
          id: 'run_1b',
          task_id: 'tsk_1b',
          conversation_id: 'conv_1',
          task_type: 'agent.run',
          status: 'succeeded',
          created_by: 'alice',
          schema_version: 'v1',
          created_at: '2026-03-22T09:02:00Z',
          updated_at: '2026-03-22T09:03:00Z',
        },
      ]
```

Then add a second replay branch so `run_1b` resolves:

```ts
      if (runId === 'run_1b') {
        return {
          run: {
            id: 'run_1b',
            task_id: 'tsk_1b',
            conversation_id: 'conv_1',
            task_type: 'agent.run',
            provider_id: 'openai',
            model_id: 'gpt-5.4',
            runner_id: 'runner_1',
            status: 'succeeded',
            created_by: 'alice',
            replayable: true,
            schema_version: 'v1',
            created_at: '2026-03-22T09:02:00Z',
            updated_at: '2026-03-22T09:03:00Z',
          },
          timeline: [
            {
              seq: 1,
              phase: 'run',
              event_type: 'run.succeeded',
              display_name: '运行成功',
              level: 'info',
              step_index: 0,
              parent_seq: 0,
              payload: { status: 'done' },
              created_at: '2026-03-22T09:03:00Z',
            },
          ],
          artifacts: [],
        }
      }
```

Then add a new spec below the existing one:

```ts
  it('renders collapsed summary and turn dropdown using display name fallback rules', async () => {
    const wrapper = mount(AdminAuditView, {
      global: {
        stubs: {
          RouterLink: {
            props: ['to'],
            template: '<a :href="to" v-bind="$attrs"><slot /></a>',
          },
        },
      },
    })

    await flushPromises()
    await wrapper.find('[data-conversation-id="conv_1"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="audit-summary-card"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="audit-summary-card"]').text()).toContain('创建者')
    expect(wrapper.find('[data-testid="audit-summary-card"]').text()).toContain('轮次数')
    expect(wrapper.find('[data-testid="audit-summary-card"]').text()).toContain('Run ID')
    expect(wrapper.find('[data-testid="audit-summary-card"]').text()).toContain('状态')
    expect(wrapper.find('[data-testid="audit-summary-details"]').exists()).toBe(false)

    expect(wrapper.find('[data-testid="turn-menu-trigger"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="turn-menu-trigger"]').text()).toContain('全部轮次')
    expect(wrapper.find('[data-testid="turn-bar"]').exists()).toBe(false)

    expect(wrapper.find('.admin-audit-timeline-item').text()).toContain('运行开始')
    expect(wrapper.find('.admin-audit-timeline-item').text()).toContain('run.started')

    await wrapper.find('[data-testid="audit-summary-toggle"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="audit-summary-details"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('Task ID')
  })
```

- [ ] **Step 2: Run the audit view spec to verify failure**

Run:

```bash
pnpm --dir webapp test -- --run src/views/AdminAuditView.spec.ts
```

Expected: FAIL because the component still renders two separate summary cards and the old `admin-audit-turn-bar` button list.

- [ ] **Step 3: Refactor `AdminAuditView.vue` to support the new UI behavior**

Apply the following changes in `webapp/src/views/AdminAuditView.vue`.

Add the new refs/computed values near the other state:

```ts
const summaryCollapsed = ref(true)
const turnMenuOpen = ref(false)
const turnMenuRef = ref<HTMLElement | null>(null)

const selectedTurnLabel = computed(() => {
  if (selectedTurnIndex.value == null) {
    return '全部轮次'
  }
  return `轮次 ${selectedTurnIndex.value + 1}`
})

const summaryFacts = computed(() => {
  const conversation = selectedConversationSummary.value
  const run = selectedAuditRun.value
  return {
    creator: conversation?.created_by || '-',
    turns: String(auditRuns.value.length),
    runId: run?.id || resolveAuditRunId(conversation) || '未暴露 run_id',
    taskId: run?.task_id || '-',
    status: run?.status || '未找到审计运行',
    conversationId: conversation?.id || '-',
    createdAt: formatConversationTime(conversation?.created_at),
  }
})
```

Add helpers below `toggleTimelineEntry` / `applyFilter`:

```ts
function formatTimelineTitle(entry: AuditReplayEvent) {
  return entry.display_name || formatEventType(entry.event_type)
}

function closeTurnMenu() {
  turnMenuOpen.value = false
}

function toggleTurnMenu() {
  if (auditRuns.value.length <= 1) {
    return
  }
  turnMenuOpen.value = !turnMenuOpen.value
}

function chooseTurn(index: number | null) {
  selectTurn(index)
  closeTurnMenu()
}
```

Update `detailHeading` to prefer the backend field:

```ts
const detailHeading = computed(() => {
  if (activeArtifact.value) {
    return formatArtifactTitle(activeArtifact.value.kind)
  }
  if (activeTimelineEntry.value) {
    return formatTimelineTitle(activeTimelineEntry.value)
  }
  return '选择时间线条目'
})
```

Add a global pointer handler similar to chat menu handling and register/unregister it:

```ts
function handleGlobalPointerDown(event: PointerEvent) {
  const target = event.target
  if (turnMenuOpen.value && !(target instanceof Node && turnMenuRef.value?.contains(target))) {
    closeTurnMenu()
  }
}

onMounted(async () => {
  document.addEventListener('pointerdown', handleGlobalPointerDown)
  await loadConversationList()
})

onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', handleGlobalPointerDown)
})
```

Replace the two summary cards and old turn bar in the template with this structure:

```vue
        <section class="admin-audit-summary-card admin-audit-summary-compact" data-testid="audit-summary-card">
          <div class="admin-audit-summary-header">
            <div>
              <h2>会话 / 执行摘要</h2>
              <p class="admin-audit-summary-subtitle">优先给时间线明细留出更多展示空间</p>
            </div>
            <button
              class="ghost-button admin-audit-summary-toggle"
              type="button"
              data-testid="audit-summary-toggle"
              @click="summaryCollapsed = !summaryCollapsed"
            >
              {{ summaryCollapsed ? '展开' : '收起' }}
            </button>
          </div>

          <dl class="admin-audit-summary-inline">
            <div><dt>创建者</dt><dd>{{ summaryFacts.creator }}</dd></div>
            <div><dt>轮次数</dt><dd>{{ summaryFacts.turns }}</dd></div>
            <div><dt>Run ID</dt><dd>{{ summaryFacts.runId }}</dd></div>
            <div><dt>状态</dt><dd>{{ summaryFacts.status }}</dd></div>
          </dl>

          <dl v-if="!summaryCollapsed" class="admin-audit-summary-details" data-testid="audit-summary-details">
            <div><dt>会话 ID</dt><dd>{{ summaryFacts.conversationId }}</dd></div>
            <div><dt>Task ID</dt><dd>{{ summaryFacts.taskId }}</dd></div>
            <div><dt>开始时间</dt><dd>{{ summaryFacts.createdAt }}</dd></div>
          </dl>
        </section>
```

```vue
            <div class="admin-audit-panel-header">
              <div><h2>操作时间线</h2></div>
              <div class="admin-audit-panel-controls">
                <div v-if="auditRuns.length > 1" ref="turnMenuRef" class="model-menu admin-audit-turn-menu">
                  <button
                    class="model-menu-trigger admin-audit-turn-trigger"
                    type="button"
                    data-testid="turn-menu-trigger"
                    aria-haspopup="menu"
                    :aria-expanded="turnMenuOpen ? 'true' : 'false'"
                    @click="toggleTurnMenu"
                  >
                    <span class="model-menu-trigger-label">{{ selectedTurnLabel }}</span>
                    <span class="model-menu-trigger-caret" :class="{ open: turnMenuOpen }" aria-hidden="true"></span>
                  </button>
                  <transition name="model-menu-fade">
                    <div v-if="turnMenuOpen" class="model-menu-panel" role="menu">
                      <button
                        class="model-menu-option"
                        :class="{ active: selectedTurnIndex == null }"
                        type="button"
                        role="menuitemradio"
                        :aria-checked="selectedTurnIndex == null ? 'true' : 'false'"
                        data-turn-option="all"
                        @click="chooseTurn(null)"
                      >
                        <span class="model-menu-option-check" aria-hidden="true"></span>
                        <span class="model-menu-option-label">全部轮次</span>
                      </button>
                      <button
                        v-for="(run, index) in auditRuns"
                        :key="run.id"
                        class="model-menu-option"
                        :class="{ active: selectedTurnIndex === index }"
                        type="button"
                        role="menuitemradio"
                        :aria-checked="selectedTurnIndex === index ? 'true' : 'false'"
                        :data-turn-option="index"
                        @click="chooseTurn(index)"
                      >
                        <span class="model-menu-option-check" aria-hidden="true"></span>
                        <span class="model-menu-option-label">轮次 {{ index + 1 }}</span>
                      </button>
                    </div>
                  </transition>
                </div>

                <div class="admin-audit-filter-bar">
                  <button class="admin-audit-filter" :class="{ active: activeFilter === 'all' }" data-filter="all" type="button" @click="applyFilter('all')">全部</button>
                  <button class="admin-audit-filter" :class="{ active: activeFilter === 'request' }" data-filter="request" type="button" @click="applyFilter('request')">请求</button>
                  <button class="admin-audit-filter" :class="{ active: activeFilter === 'tool' }" data-filter="tool" type="button" @click="applyFilter('tool')">工具</button>
                  <button class="admin-audit-filter" :class="{ active: activeFilter === 'error' }" data-filter="error" type="button" @click="applyFilter('error')">错误</button>
                </div>
              </div>
            </div>
```

Update each timeline item body to show display-name first and raw metadata second:

```vue
                  <div class="admin-audit-timeline-copy">
                    <strong>{{ formatTimelineTitle(entry) }}</strong>
                    <p>
                      {{ entry.event_type }} · {{ formatPhase(entry.phase) }} · #{{ entry.seq }}
                      <template v-if="auditRuns.length > 1"> · 轮次 {{ entry.turnIndex + 1 }}</template>
                    </p>
                  </div>
```

And update the chip to show `formatPhase(entry.phase)` instead of duplicating the title:

```vue
                <span class="admin-audit-artifact-chip">{{ formatPhase(entry.phase) }}</span>
```

- [ ] **Step 4: Run the audit view spec to verify the behavior passes**

Run:

```bash
pnpm --dir webapp test -- --run src/views/AdminAuditView.spec.ts
```

Expected: PASS with both audit view tests green.

- [ ] **Step 5: Commit the audit view logic/test change**

Run:

```bash
git add webapp/src/views/AdminAuditView.vue webapp/src/views/AdminAuditView.spec.ts
git commit -m "feat(webapp): compact admin audit summary and turn picker"
```

### Task 4: Tighten audit layout styling and verify end-to-end

**Files:**
- Modify: `webapp/src/style.css:2026-2325`
- Test: `webapp/src/views/AdminAuditView.spec.ts`

- [ ] **Step 1: Write the failing style-sensitive assertions**

Extend the second spec in `webapp/src/views/AdminAuditView.spec.ts` with these checks:

```ts
    expect(wrapper.find('.admin-audit-summary-compact').exists()).toBe(true)
    expect(wrapper.find('.admin-audit-panel-header').exists()).toBe(true)
    expect(wrapper.find('.admin-audit-panel-controls').exists()).toBe(true)
    expect(wrapper.find('.admin-audit-turn-menu').exists()).toBe(true)
    expect(wrapper.find('.admin-audit-timeline-copy').exists()).toBe(true)
```

These classes do not exist yet, so the test should fail.

- [ ] **Step 2: Run the audit view spec to confirm failure**

Run:

```bash
pnpm --dir webapp test -- --run src/views/AdminAuditView.spec.ts
```

Expected: FAIL with `.exists()` assertions returning false for the new compact-layout classes.

- [ ] **Step 3: Update `webapp/src/style.css` for the compact detail-first layout**

Replace the current audit summary/detail/timeline rules in the `2026+` audit block with the following additions and edits.

Update the content layout and remove the old two-card summary grid dependency:

```css
.admin-audit-content {
  display: grid;
  grid-template-rows: auto minmax(0, 1fr);
  gap: 0.65rem;
  min-height: 0;
}

.admin-audit-summary-grid {
  display: block;
}

.admin-audit-summary-compact,
.admin-audit-card {
  border-radius: 16px;
  border: 1px solid rgba(25, 50, 59, 0.08);
  background: rgba(245, 249, 249, 0.85);
  padding: 0.8rem 0.95rem;
}

.admin-audit-summary-header,
.admin-audit-panel-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 0.75rem;
}

.admin-audit-summary-subtitle {
  margin-top: 0.18rem;
  color: #60767d;
  font-size: 0.78rem;
}

.admin-audit-summary-toggle {
  padding: 0.38rem 0.7rem;
}

.admin-audit-summary-inline,
.admin-audit-summary-details {
  margin: 0.65rem 0 0;
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 0.55rem 0.75rem;
}

.admin-audit-summary-details {
  padding-top: 0.65rem;
  border-top: 1px solid rgba(25, 50, 59, 0.08);
  grid-template-columns: repeat(3, minmax(0, 1fr));
}
```

Change the main two-column balance so the detail panel gets more width:

```css
.admin-audit-detail-grid {
  min-height: 0;
  display: grid;
  grid-template-columns: minmax(260px, 0.82fr) minmax(0, 1.38fr);
  gap: 0.7rem;
}

.admin-audit-timeline-panel,
.admin-audit-artifact-panel {
  min-height: 0;
  display: grid;
  grid-template-rows: auto minmax(0, 1fr);
  gap: 0.6rem;
}

.admin-audit-panel-controls {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 0.5rem;
  min-width: 0;
  flex-wrap: wrap;
}

.admin-audit-turn-menu {
  z-index: 3;
}

.admin-audit-turn-trigger {
  max-width: none;
}
```

Tighten the filter row and timeline items:

```css
.admin-audit-filter-bar {
  display: flex;
  align-items: center;
  gap: 0.4rem;
  flex-wrap: wrap;
  background: transparent;
  padding-bottom: 0;
}

.admin-audit-timeline {
  min-height: 0;
  overflow-y: auto;
  display: grid;
  align-content: start;
  gap: 0.45rem;
  padding-right: 0.15rem;
}

.admin-audit-timeline-item {
  width: 100%;
  border: 1px solid rgba(25, 50, 59, 0.08);
  border-radius: 14px;
  background: rgba(255, 255, 255, 0.78);
  padding: 0.62rem 0.72rem;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.6rem;
  text-align: left;
}

.admin-audit-timeline-leading {
  display: flex;
  align-items: flex-start;
  gap: 0.62rem;
  min-width: 0;
  flex: 1 1 auto;
}

.admin-audit-timeline-copy {
  min-width: 0;
}

.admin-audit-timeline-leading strong {
  display: block;
  color: #1f3b44;
  font-size: 0.88rem;
  line-height: 1.28;
}

.admin-audit-timeline-leading p {
  margin-top: 0.14rem;
  color: #60767d;
  font-size: 0.74rem;
  line-height: 1.25;
  word-break: break-word;
}

.admin-audit-artifact-chip {
  flex: 0 0 auto;
  border-radius: 999px;
  padding: 0.22rem 0.52rem;
  font-size: 0.71rem;
}
```

Tighten the detail card metadata and JSON area:

```css
.admin-audit-artifact-detail {
  min-height: 0;
  display: grid;
  grid-template-rows: auto minmax(0, 1fr);
  gap: 0.45rem;
}

.admin-audit-detail-meta {
  display: flex;
  align-items: center;
  gap: 0.55rem;
  flex-wrap: wrap;
  color: #60767d;
  font-size: 0.75rem;
}

.admin-audit-json {
  min-height: 0;
  height: 100%;
  overflow: auto;
  margin: 0;
  background: rgba(255, 255, 255, 0.88);
  border-radius: 12px;
  border: 1px solid rgba(25, 50, 59, 0.08);
  padding: 0.72rem 0.78rem;
}
```

Adjust responsive behavior so the merged summary still stacks cleanly:

```css
@media (max-width: 960px) {
  .admin-audit-detail-grid,
  .admin-audit-summary-inline,
  .admin-audit-summary-details {
    grid-template-columns: 1fr;
  }
}
```

- [ ] **Step 4: Run the focused frontend and backend verification commands**

Run:

```bash
pnpm --dir webapp test -- --run src/views/AdminAuditView.spec.ts && go test ./core/audit && go test ./app/handlers -run 'TestAuditHandler(GetReplayBundle|ListConversationRuns)' 
```

Expected: PASS for Vitest plus `ok` for both Go packages.

- [ ] **Step 5: Manually verify the page in the browser**

Open `http://localhost:5173/#/admin/audit` and verify:

```text
1. 顶部只剩一个“会话 / 执行摘要”卡片，默认折叠。
2. 轮次切换是胶囊触发器 + 下拉菜单，不是浏览器原生 select，也不是旧按钮条。
3. 时间线主标题显示中文，副行保留 interaction.requested / run.waiting 等原始事件名。
4. 左列比之前更窄，右侧 JSON / artifact 明显更宽。
5. 圆角、边框、阴影、配色和 chat / audit 现有页面一致，没有原生控件感。
```

- [ ] **Step 6: Commit the final web styling pass**

Run:

```bash
git add webapp/src/style.css webapp/src/views/AdminAuditView.spec.ts
git commit -m "style(webapp): compact admin audit timeline layout"
```

## Self-Review

### Spec coverage
- Compact merged summary card with default collapsed state: Task 3 + Task 4.
- Chat-style turn dropdown replacing button bar: Task 3 + Task 4.
- Backend-provided Chinese display names with frontend fallback: Task 1 + Task 2 + Task 3.
- Narrower timeline / wider detail panel: Task 4.
- Style consistency with existing chat/audit visuals and no native controls: Task 3 + Task 4.

### Placeholder scan
- No unresolved placeholders or vague deferred-work text remains.
- Every code-changing step includes concrete code blocks.
- Every verification step includes explicit commands and expected outcomes.

### Type consistency
- Backend and frontend both use `display_name`.
- View logic consistently calls `formatTimelineTitle(entry)`.
- Turn menu state uses `turnMenuOpen`, `turnMenuRef`, `selectedTurnLabel`, and `chooseTurn` consistently.
