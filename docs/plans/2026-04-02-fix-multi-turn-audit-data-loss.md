# 修复多轮对话审计数据只保留一轮的问题

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 修复多轮对话场景下，审计 UI 只展示一轮 loop 审计数据的问题，使所有轮次的审计事件都能被查询和展示。

**Architecture:** 当前 audit_runs 表按 task_id 唯一索引建立一对一关系，每轮对话（用户发一条消息）会创建一个新 task，因此每轮产生独立的 audit run。但读取路径 `GetLatestRunByConversationID` 只返回最新一个 run，前端也只存储单个 `audit_run_id`，导致只能看到一轮的审计数据。修复方向是在 store 层新增按 conversation 列出所有 runs 的能力，新增对应 API，并更新前端展示逻辑以聚合所有轮次的审计事件。

**Tech Stack:** Go 1.25, GORM, Gin, Vue 3 + TypeScript + Vite, Vitest

---

## 根因分析

### 数据模型层：数据实际是完整的

审计数据**并没有丢失**。每一轮对话都正确产生了独立的 audit run 和完整的 events/artifacts：

1. 用户发消息 → task handler 创建新 task（`task_1`）→ executor 执行 → 审计记录 `run_A`（绑定 `task_1`，`conv_X`）
2. 用户再发消息 → task handler 创建新 task（`task_2`）→ executor 执行 → 审计记录 `run_B`（绑定 `task_2`，`conv_X`）
3. 数据库中 `run_A` 和 `run_B` 的 events 都完整存在

### 读取路径：只暴露了最新一个 run

问题出在三个环节：

1. **`Store.GetLatestRunByConversationID`**（`core/audit/store.go:68`）：按 `created_at DESC` 排序只返回 1 条记录。`conversation_handler.go:267` 使用此方法，只把最新 run 的 ID 放入 conversation 响应。

2. **Conversation 模型只存一个 `audit_run_id`**（`core/agent/conversation_store.go:35`）：`AuditRunID string` 字段只能容纳一个 ID。前端 `Conversation` 类型（`webapp/src/types/api.ts:97`）也只有单值字段。

3. **审计 handler 只能按单个 run_id 查询**（`app/handlers/audit_handler.go`）：三个 GET 端点都以 `:id`（单个 run ID）为入口，没有按 conversation 聚合的能力。

4. **前端 AdminAuditView 按单个 conversation 的 `audit_run_id` 加载审计数据**：选中 conversation 后只 fetch 一个 run 的事件。

### "有时是第一轮，有时是最后一轮"的原因

- 默认行为是 `GetLatestRunByConversationID` 返回最后一轮（`ORDER BY created_at DESC`）。
- 但如果前端在早期轮次时缓存了 `audit_run_id`，且后续未刷新 conversation 列表，则展示的是第一轮的数据。

---

## 修复方案

### 核心思路

在审计 store 层新增 `ListRunsByConversationID` 方法，在 handler 层新增按 conversation 查询所有 runs 及其聚合事件的 API，在前端改为加载所有轮次的审计数据。

### 不改动的部分

- 写入路径不变：每个 task 仍然产生独立的 audit run，events 仍然 append-only。
- `audit_runs` 表结构不变：`task_id` 唯一索引保持不变。
- 现有的按 run ID 查询的 API 保持兼容。

---

## Task 1: 新增 `ListRunsByConversationID` store 方法

**Files:**
- Modify: `core/audit/store.go`
- Test: `core/audit/store_test.go`

**Step 1: 写失败测试**

在 `core/audit/store_test.go` 中新增测试：

```go
func TestStoreListRunsByConversationIDReturnsAllRunsChronologically(t *testing.T) {
	db := newTestDB(t)
	store := NewStore(db)
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	ctx := context.Background()

	// 创建 3 个绑定同一 conversation 但不同 task 的 run
	for i, taskID := range []string{"task_1", "task_2", "task_3"} {
		_, err := store.CreateRun(ctx, StartRunInput{
			TaskID:         taskID,
			ConversationID: "conv_1",
			TaskType:       "agent.run",
			SchemaVersion:  SchemaVersionV1,
			Status:         StatusRunning,
			StartedAt:      time.Now().UTC().Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatalf("CreateRun(%s) error = %v", taskID, err)
		}
	}
	// 另一个 conversation 的 run，不应出现在结果中
	_, _ = store.CreateRun(ctx, StartRunInput{
		TaskID: "task_other", ConversationID: "conv_2",
		TaskType: "agent.run", SchemaVersion: SchemaVersionV1, Status: StatusRunning,
	})

	runs, err := store.ListRunsByConversationID(ctx, "conv_1")
	if err != nil {
		t.Fatalf("ListRunsByConversationID() error = %v", err)
	}
	if len(runs) != 3 {
		t.Fatalf("got %d runs, want 3", len(runs))
	}
	// 按 created_at ASC 排列
	for i := 1; i < len(runs); i++ {
		if runs[i].CreatedAt.Before(runs[i-1].CreatedAt) {
			t.Fatalf("runs[%d].CreatedAt (%v) before runs[%d].CreatedAt (%v)", i, runs[i].CreatedAt, i-1, runs[i-1].CreatedAt)
		}
	}
}

func TestStoreListRunsByConversationIDReturnsEmptyForUnknown(t *testing.T) {
	db := newTestDB(t)
	store := NewStore(db)
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}

	runs, err := store.ListRunsByConversationID(context.Background(), "no_such_conv")
	if err != nil {
		t.Fatalf("ListRunsByConversationID() error = %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("got %d runs, want 0", len(runs))
	}
}
```

**Step 2: 运行测试确认失败**

Run: `go test ./core/audit -run '^TestStoreListRunsByConversationID' -v`
Expected: 编译失败，`store.ListRunsByConversationID` 不存在

**Step 3: 实现 `ListRunsByConversationID`**

在 `core/audit/store.go` 中，紧随 `GetLatestRunByConversationID` 之后添加：

```go
func (s *Store) ListRunsByConversationID(ctx context.Context, conversationID string) ([]Run, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store db cannot be nil")
	}
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return nil, nil
	}

	var runs []Run
	err := s.db.WithContext(ctx).
		Where("conversation_id = ?", conversationID).
		Order("created_at asc").
		Order("id asc").
		Find(&runs).Error
	if err != nil {
		return nil, err
	}
	return runs, nil
}
```

**Step 4: 运行测试确认通过**

Run: `go test ./core/audit -run '^TestStoreListRunsByConversationID' -v`
Expected: PASS

**Step 5: 提交**

```bash
git add core/audit/store.go core/audit/store_test.go
git commit -m "feat(audit): add ListRunsByConversationID to support multi-turn audit queries"
```

---

## Task 2: 新增 `ListEventsByConversationID` store 方法

**Files:**
- Modify: `core/audit/store.go`
- Test: `core/audit/store_test.go`

**Step 1: 写失败测试**

```go
func TestStoreListEventsByConversationIDReturnsEventsAcrossRuns(t *testing.T) {
	db := newTestDB(t)
	store := NewStore(db)
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	ctx := context.Background()

	// 创建 2 个同一 conversation 的 run，各追加 2 个 event
	run1, _ := store.CreateRun(ctx, StartRunInput{
		TaskID: "task_1", ConversationID: "conv_1",
		TaskType: "agent.run", SchemaVersion: SchemaVersionV1, Status: StatusRunning,
	})
	run2, _ := store.CreateRun(ctx, StartRunInput{
		TaskID: "task_2", ConversationID: "conv_1",
		TaskType: "agent.run", SchemaVersion: SchemaVersionV1, Status: StatusRunning,
	})
	store.AppendEvent(ctx, run1.ID, AppendEventInput{EventType: "step.started", StepIndex: 1})
	store.AppendEvent(ctx, run1.ID, AppendEventInput{EventType: "step.finished", StepIndex: 1})
	store.AppendEvent(ctx, run2.ID, AppendEventInput{EventType: "step.started", StepIndex: 1})
	store.AppendEvent(ctx, run2.ID, AppendEventInput{EventType: "step.finished", StepIndex: 1})

	// 另一个 conversation 的 event
	runOther, _ := store.CreateRun(ctx, StartRunInput{
		TaskID: "task_other", ConversationID: "conv_2",
		TaskType: "agent.run", SchemaVersion: SchemaVersionV1, Status: StatusRunning,
	})
	store.AppendEvent(ctx, runOther.ID, AppendEventInput{EventType: "step.started", StepIndex: 1})

	events, err := store.ListEventsByConversationID(ctx, "conv_1")
	if err != nil {
		t.Fatalf("ListEventsByConversationID() error = %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("got %d events, want 4", len(events))
	}
	// 验证按 run 的 created_at 和 event 的 seq 排序
	for i := 1; i < len(events); i++ {
		prev := events[i-1]
		curr := events[i]
		if prev.RunID == curr.RunID && prev.Seq >= curr.Seq {
			t.Fatalf("events[%d].Seq (%d) >= events[%d].Seq (%d) within same run", i-1, prev.Seq, i, curr.Seq)
		}
	}
}
```

**Step 2: 运行测试确认失败**

Run: `go test ./core/audit -run '^TestStoreListEventsByConversationID' -v`
Expected: 编译失败

**Step 3: 实现方法**

在 `core/audit/store.go` 中添加：

```go
func (s *Store) ListEventsByConversationID(ctx context.Context, conversationID string) ([]Event, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store db cannot be nil")
	}
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return nil, nil
	}

	// 先查出该 conversation 下所有 run_id，再按这些 run_id 查 events
	var runIDs []string
	if err := s.db.WithContext(ctx).
		Model(&Run{}).
		Where("conversation_id = ?", conversationID).
		Order("created_at asc, id asc").
		Pluck("id", &runIDs).Error; err != nil {
		return nil, err
	}
	if len(runIDs) == 0 {
		return nil, nil
	}

	var events []Event
	err := s.db.WithContext(ctx).
		Where("run_id IN ?", runIDs).
		Order("created_at asc").
		Order("seq asc").
		Order("id asc").
		Find(&events).Error
	if err != nil {
		return nil, err
	}
	return events, nil
}
```

**Step 4: 运行测试确认通过**

Run: `go test ./core/audit -run '^TestStoreListEventsByConversationID' -v`
Expected: PASS

**Step 5: 提交**

```bash
git add core/audit/store.go core/audit/store_test.go
git commit -m "feat(audit): add ListEventsByConversationID for cross-run event queries"
```

---

## Task 3: 新增审计 handler 端点 —— 按 conversation 查询 runs 和 events

**Files:**
- Modify: `app/handlers/audit_handler.go`
- Test: `app/handlers/audit_handler_test.go`

**Step 1: 写失败测试**

在 `audit_handler_test.go` 中新增测试（参考现有测试风格），覆盖：

1. `GET /audit/conversations/:conversation_id/runs` 返回该 conversation 下所有 audit runs（按 created_at ASC）
2. `GET /audit/conversations/:conversation_id/events` 返回该 conversation 下所有 audit events（跨 runs 聚合）
3. 空 conversation 返回空数组
4. 鉴权：非本人 conversation 返回 401

测试示例（仅展示 runs 端点的骨架，events 端点类似）：

```go
func TestAuditHandlerListRunsByConversationReturnsAllRuns(t *testing.T) {
	// 参考 TestAuditHandlerGetRunReturnsRunPayload 的 setup 风格
	// 创建 2 个 task + 2 个 audit run，绑定同一 conversation_id
	// GET /audit/conversations/conv_1/runs
	// 断言 status 200，返回 2 个 run，按 created_at 升序
}
```

**Step 2: 运行测试确认失败**

Run: `go test ./app/handlers -run '^TestAuditHandlerListRunsByConversation' -v`
Expected: 路由不存在，404 或编译失败

**Step 3: 实现 handler 方法**

在 `audit_handler.go` 中新增两个端点方法和路由注册：

```go
// 在 Register 方法中追加两个 handler
func (h *AuditHandler) Register(rg *gin.RouterGroup) {
	// ... 现有的 3 个 handler ...
	resp.HandlerWrapper(rg, "audit", []*resp.Handler{
		resp.NewJsonOptionsHandler(h.handleGetRun),
		resp.NewJsonOptionsHandler(h.handleGetRunEvents),
		resp.NewJsonOptionsHandler(h.handleGetRunReplay),
		resp.NewJsonOptionsHandler(h.handleListConversationRuns),    // 新增
		resp.NewJsonOptionsHandler(h.handleListConversationEvents),  // 新增
	}, options...)
}

func (h *AuditHandler) handleListConversationRuns() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "/conversations/:conversation_id/runs", func(c *gin.Context) (any, []resp.ResOpt, error) {
		if h.store == nil {
			return nil, nil, fmt.Errorf("audit store is not configured")
		}
		conversationID := c.Param("conversation_id")
		runs, err := h.store.ListRunsByConversationID(c.Request.Context(), conversationID)
		if err != nil {
			return nil, nil, err
		}
		if len(runs) > 0 {
			if err := h.ensureRunAccess(c, &runs[0]); err != nil {
				return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, err
			}
		}
		return runs, nil, nil
	}, nil
}

func (h *AuditHandler) handleListConversationEvents() (method, relativePath string, wrapper resp.JsonOptionsResultWrapper, opts []resp.WrapperOption) {
	return http.MethodGet, "/conversations/:conversation_id/events", func(c *gin.Context) (any, []resp.ResOpt, error) {
		if h.store == nil {
			return nil, nil, fmt.Errorf("audit store is not configured")
		}
		conversationID := c.Param("conversation_id")
		runs, err := h.store.ListRunsByConversationID(c.Request.Context(), conversationID)
		if err != nil {
			return nil, nil, err
		}
		if len(runs) > 0 {
			if err := h.ensureRunAccess(c, &runs[0]); err != nil {
				return nil, []resp.ResOpt{resp.WithCode(http.StatusUnauthorized)}, err
			}
		}
		events, err := h.store.ListEventsByConversationID(c.Request.Context(), conversationID)
		if err != nil {
			return nil, nil, err
		}
		return events, nil, nil
	}, nil
}
```

**Step 4: 运行测试确认通过**

Run: `go test ./app/handlers -run '^TestAuditHandlerListRunsByConversation' -v`
Expected: PASS

**Step 5: 提交**

```bash
git add app/handlers/audit_handler.go app/handlers/audit_handler_test.go
git commit -m "feat(audit): add conversation-level audit runs and events endpoints"
```

---

## Task 4: 更新 conversation 响应，增加 `audit_run_ids` 字段

**Files:**
- Modify: `core/agent/conversation_store.go`
- Modify: `app/handlers/conversation_handler.go`
- Test: `app/handlers/conversation_handler_test.go`

**Step 1: 写失败测试**

在 `conversation_handler_test.go` 中新增测试，验证 conversation 详情响应中包含 `audit_run_ids` 数组字段。

**Step 2: 运行测试确认失败**

Run: `go test ./app/handlers -run '^TestConversationDetailIncludesAllAuditRunIDs$' -v`
Expected: FAIL

**Step 3: 实现**

在 `core/agent/conversation_store.go` 的 `Conversation` struct 中添加：

```go
AuditRunIDs   []string   `json:"audit_run_ids,omitempty" gorm:"-"`
```

在 `app/handlers/conversation_handler.go` 的 `enrichConversation` 方法中，将原来的 `GetLatestRunByConversationID` 替换为 `ListRunsByConversationID`：

```go
// 替换前:
run, err := h.auditStore.GetLatestRunByConversationID(ctx, conversation.ID)
if err == nil && run != nil {
    enriched.AuditRunID = run.ID
}

// 替换后:
runs, err := h.auditStore.ListRunsByConversationID(ctx, conversation.ID)
if err == nil && len(runs) > 0 {
    enriched.AuditRunID = runs[len(runs)-1].ID  // 保持向后兼容
    ids := make([]string, len(runs))
    for i, r := range runs {
        ids[i] = r.ID
    }
    enriched.AuditRunIDs = ids
}
```

**Step 4: 运行测试确认通过**

Run: `go test ./app/handlers -run '^TestConversationDetailIncludesAllAuditRunIDs$' -v`
Expected: PASS

**Step 5: 运行现有 conversation 测试确认不破坏兼容性**

Run: `go test ./app/handlers -run '^TestConversation' -v`
Expected: 全部 PASS

**Step 6: 提交**

```bash
git add core/agent/conversation_store.go app/handlers/conversation_handler.go app/handlers/conversation_handler_test.go
git commit -m "feat(audit): include all audit_run_ids in conversation response"
```

---

## Task 5: 更新前端类型与 API

**Files:**
- Modify: `webapp/src/types/api.ts`
- Modify: `webapp/src/lib/api.ts`
- Test: `webapp/src/lib/api.spec.ts`

**Step 1: 更新 TypeScript 类型**

在 `webapp/src/types/api.ts` 的 `Conversation` interface 中添加：

```typescript
audit_run_ids?: string[]
```

**Step 2: 新增 API 方法**

在 `webapp/src/lib/api.ts` 中添加：

```typescript
export async function fetchAuditConversationRuns(conversationId: string) {
  return fetchJson<AuditRun[]>(`/audit/conversations/${conversationId}/runs`)
}

export async function fetchAuditConversationEvents(conversationId: string) {
  return fetchJson<AuditEvent[]>(`/audit/conversations/${conversationId}/events`)
}
```

**Step 3: 写测试**

在 `webapp/src/lib/api.spec.ts` 中补充测试，验证新方法存在且可调用。

**Step 4: 运行测试确认通过**

Run: `pnpm --dir webapp exec vitest run src/lib/api.spec.ts -v`
Expected: PASS

**Step 5: 类型检查**

Run: `pnpm --dir webapp exec vue-tsc -b`
Expected: 无错误

**Step 6: 提交**

```bash
git add webapp/src/types/api.ts webapp/src/lib/api.ts webapp/src/lib/api.spec.ts
git commit -m "feat(webapp): add conversation-level audit API and types"
```

---

## Task 6: 更新 AdminAuditView 支持多轮审计展示

**Files:**
- Modify: `webapp/src/views/AdminAuditView.vue`
- Test: `webapp/src/views/AdminAuditView.spec.ts`

**Step 1: 写失败测试**

在 `AdminAuditView.spec.ts` 中修改或新增测试，验证：
- 选中 conversation 后，加载该 conversation 下所有 audit runs
- 事件列表展示所有轮次的事件，按时间排序
- 每个事件标注所属的 run（轮次）

**Step 2: 修改 AdminAuditView.vue**

将现有的 `fetchAuditRun(auditRunId)` + `fetchAuditRunEvents(auditRunId)` 调用替换为：
- `fetchAuditConversationRuns(conversationId)` 获取所有 runs
- `fetchAuditConversationEvents(conversationId)` 获取聚合事件

在 UI 层面：
- 显示 runs 列表（轮次选择器，或全部展开）
- events 列表增加轮次标注（如 "轮次 1"、"轮次 2"）
- 保留按单个 run 查看的能力（点击某个 run 过滤其 events）

**Step 3: 运行测试确认通过**

Run: `pnpm --dir webapp exec vitest run src/views/AdminAuditView.spec.ts -v`
Expected: PASS

**Step 4: 类型检查**

Run: `pnpm --dir webapp exec vue-tsc -b`
Expected: 无错误

**Step 5: 提交**

```bash
git add webapp/src/views/AdminAuditView.vue webapp/src/views/AdminAuditView.spec.ts
git commit -m "feat(webapp): show all conversation turns in audit view"
```

---

## Task 7: 全量验证

**Step 1: 后端全量测试**

Run: `go test ./...`
Expected: 全部 PASS

**Step 2: 后端构建**

Run: `go build ./cmd/...`
Expected: 编译成功

**Step 3: 前端类型检查**

Run: `pnpm --dir webapp exec vue-tsc -b`
Expected: 无错误

**Step 4: 前端测试**

Run: `pnpm --dir webapp test`
Expected: 全部 PASS

**Step 5: 提交**

如有遗留修改，合并提交。

---

## 改动影响范围总结

| 层 | 文件 | 变更类型 |
|---|------|---------|
| audit store | `core/audit/store.go` | 新增 2 个查询方法 |
| audit store test | `core/audit/store_test.go` | 新增测试 |
| audit handler | `app/handlers/audit_handler.go` | 新增 2 个端点 |
| audit handler test | `app/handlers/audit_handler_test.go` | 新增测试 |
| conversation model | `core/agent/conversation_store.go` | 新增 `AuditRunIDs` 字段 |
| conversation handler | `app/handlers/conversation_handler.go` | 改用 ListRuns 替代 GetLatestRun |
| conversation handler test | `app/handlers/conversation_handler_test.go` | 补充测试 |
| frontend types | `webapp/src/types/api.ts` | 新增字段和类型 |
| frontend api | `webapp/src/lib/api.ts` | 新增 2 个 fetch 方法 |
| frontend api test | `webapp/src/lib/api.spec.ts` | 补充测试 |
| admin audit view | `webapp/src/views/AdminAuditView.vue` | 改为多轮聚合展示 |
| admin audit view test | `webapp/src/views/AdminAuditView.spec.ts` | 更新测试 |

### 不需要改动的部分

- 写入路径（executor、runner、task manager 的审计记录逻辑）完全不变
- `audit_runs`、`audit_events`、`audit_artifacts` 表结构不变
- 现有按 run ID 查询的 3 个 API 端点保持兼容
- 无需数据库迁移
