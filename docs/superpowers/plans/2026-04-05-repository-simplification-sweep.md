# Repository Simplification Sweep Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove high-value duplication and obvious hot-path waste across backend handlers/core stores, frontend chat flow, and frontend admin screens without changing product scope, public routes, or repository layering.

**Architecture:** Execute the cleanup as three tracks in order: backend cleanup, frontend chat-path cleanup, and frontend admin cleanup. Prefer tiny shared helpers with immediate call sites over broad rewrites, keep `app -> core -> pkg` intact, and verify each track independently before moving to the next.

**Tech Stack:** Go 1.25, Gin, GORM/SQLite-backed tests, Vue 3 + TypeScript + Vite, Vitest, Vue Test Utils.

---

## Planned File Structure

### New files

- `app/handlers/task_access.go`
  - Shared handler-local task ownership and actor resolution helpers for approval/interaction routes.
- `app/handlers/task_access_test.go`
  - Narrow tests for shared task-access behavior.
- `pkg/jsonutil/raw_message.go`
  - Shared JSON raw-message normalization helper for core stores.
- `pkg/jsonutil/raw_message_test.go`
  - Validates nil/default/raw/invalid JSON behavior.
- `core/skills/names.go`
  - Shared `NormalizeNames(...)` helper for skill-name dedupe.
- `core/skills/names_test.go`
  - Verifies trim/dedupe/empty filtering.
- `webapp/src/lib/task-runtime.ts`
  - Shared task status, task→conversation resolution, and approval-entry hydration helpers.
- `webapp/src/lib/task-runtime.spec.ts`
  - Tests shared task-runtime helpers.
- `webapp/src/lib/question-entry.ts`
  - Normalizes question interaction entries for `MessageList.vue`.
- `webapp/src/lib/question-entry.spec.ts`
  - Tests question normalization and response summarization.
- `webapp/src/lib/model-selection.ts`
  - Shared provider/model fallback helper.
- `webapp/src/lib/model-selection.spec.ts`
  - Tests provider/model fallback behavior.
- `webapp/src/lib/time.ts`
  - Shared lightweight timestamp display helper.
- `webapp/src/lib/time.spec.ts`
  - Tests timestamp formatting.

### Modified files

- `app/handlers/approval_handler.go`
- `app/handlers/interaction_handler.go`
- `app/handlers/conversation_handler.go`
- `app/handlers/approval_handler_test.go`
- `app/handlers/interaction_handler_test.go`
- `app/handlers/conversation_handler_test.go`
- `core/tasks/manager.go`
- `core/tasks/runtime.go`
- `core/tasks/store.go`
- `core/tasks/manager_test.go`
- `core/audit/store.go`
- `core/audit/store_test.go`
- `core/interactions/store.go`
- `core/interactions/store_test.go`
- `core/skills/resolver.go`
- `core/skills/resolver_test.go`
- `core/agent/executor.go`
- `core/agent/executor_test.go`
- `webapp/src/lib/api.ts`
- `webapp/src/lib/session.ts`
- `webapp/src/lib/api.spec.ts`
- `webapp/src/lib/session.spec.ts`
- `webapp/src/lib/transcript.ts`
- `webapp/src/lib/transcript.spec.ts`
- `webapp/src/lib/chat-state.ts`
- `webapp/src/lib/chat-state.spec.ts`
- `webapp/src/views/ChatView.vue`
- `webapp/src/views/ChatView.spec.ts`
- `webapp/src/views/ApprovalView.vue`
- `webapp/src/views/ApprovalView.spec.ts`
- `webapp/src/components/MessageList.vue`
- `webapp/src/components/MessageList.spec.ts`
- `webapp/src/components/ConversationSidebar.vue`
- `webapp/src/components/ConversationSidebar.spec.ts`
- `webapp/src/views/AdminPromptView.vue`
- `webapp/src/views/AdminPromptView.spec.ts`
- `webapp/src/views/AdminAuditView.vue`
- `webapp/src/views/AdminAuditView.spec.ts`
- `webapp/src/types/api.ts`

---

## Track 1: Backend cleanup

### Task 1: Extract shared handler task-access helpers

**Files:**
- Create: `app/handlers/task_access.go`
- Create: `app/handlers/task_access_test.go`
- Modify: `app/handlers/approval_handler.go:180-221`
- Modify: `app/handlers/interaction_handler.go:208-249`
- Test: `app/handlers/approval_handler_test.go`
- Test: `app/handlers/interaction_handler_test.go`

- [ ] **Step 1: Write the failing shared-helper tests**

```go
package handlers

import (
    "net/http/httptest"
    "testing"

    "github.com/EquentR/agent_runtime/app/models"
    coretasks "github.com/EquentR/agent_runtime/core/tasks"
    "github.com/gin-gonic/gin"
)

func TestEnsureTaskOwnedByCurrentUserAllowsOwnerAndAdmin(t *testing.T) {
    task := &coretasks.Task{CreatedBy: "alice"}

    ownerCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
    ownerCtx.Set(authUserContextKey, &models.User{Username: "alice", Role: models.UserRoleUser})
    if err := ensureTaskOwnedByCurrentUser(ownerCtx, true, task); err != nil {
        t.Fatalf("owner err = %v, want nil", err)
    }

    adminCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
    adminCtx.Set(authUserContextKey, &models.User{Username: "root", Role: models.UserRoleAdmin})
    if err := ensureTaskOwnedByCurrentUser(adminCtx, true, task); err != nil {
        t.Fatalf("admin err = %v, want nil", err)
    }
}

func TestResolveTaskActorFallsBackToTaskCreator(t *testing.T) {
    ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
    got := resolveTaskActor(ctx, &coretasks.Task{CreatedBy: "alice"})
    if got != "alice" {
        t.Fatalf("resolveTaskActor() = %q, want %q", got, "alice")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./app/handlers -run 'TestEnsureTaskOwnedByCurrentUser|TestResolveTaskActor' -v`
Expected: FAIL with undefined `ensureTaskOwnedByCurrentUser` / `resolveTaskActor`.

- [ ] **Step 3: Write minimal helper implementation**

`app/handlers/task_access.go`

```go
package handlers

import (
    "errors"
    "fmt"

    coretasks "github.com/EquentR/agent_runtime/core/tasks"
    resp "github.com/EquentR/agent_runtime/pkg/rest"
    "github.com/gin-gonic/gin"
)

func loadOwnedTask(c *gin.Context, manager *coretasks.Manager, authRequired bool) (*coretasks.Task, []resp.ResOpt, error) {
    if manager == nil {
        return nil, nil, fmt.Errorf("task manager is not configured")
    }
    task, err := manager.GetTask(c.Request.Context(), c.Param("id"))
    if err != nil {
        if errors.Is(err, coretasks.ErrTaskNotFound) {
            return nil, []resp.ResOpt{resp.WithCode(404)}, err
        }
        return nil, nil, err
    }
    if err := ensureTaskOwnedByCurrentUser(c, authRequired, task); err != nil {
        return nil, []resp.ResOpt{resp.WithCode(401)}, err
    }
    return task, nil, nil
}

func ensureTaskOwnedByCurrentUser(c *gin.Context, authRequired bool, task *coretasks.Task) error {
    if !authRequired || task == nil {
        return nil
    }
    return ensureOwnerReadableByCurrentUser(c, task.CreatedBy, "无权访问该任务")
}

func resolveTaskActor(c *gin.Context, task *coretasks.Task) string {
    if user := currentAuthUser(c); user != nil && user.Username != "" {
        return user.Username
    }
    if task == nil {
        return ""
    }
    return task.CreatedBy
}
```

- [ ] **Step 4: Replace duplicated handler-local helpers**

`app/handlers/approval_handler.go`

```go
func (h *ApprovalHandler) loadAccessibleTask(c *gin.Context) (*coretasks.Task, []resp.ResOpt, error) {
    if h == nil || h.manager == nil {
        return nil, nil, fmt.Errorf("task manager is not configured")
    }
    if h.approvals == nil {
        return nil, nil, fmt.Errorf("approval store is not configured")
    }
    return loadOwnedTask(c, h.manager, h.authRequired)
}

func (h *ApprovalHandler) resolveDecisionBy(c *gin.Context, task *coretasks.Task) string {
    return resolveTaskActor(c, task)
}
```

`app/handlers/interaction_handler.go`

```go
func (h *InteractionHandler) loadAccessibleTask(c *gin.Context) (*coretasks.Task, []resp.ResOpt, error) {
    if h == nil || h.manager == nil {
        return nil, nil, fmt.Errorf("task manager is not configured")
    }
    if h.interactions == nil {
        return nil, nil, fmt.Errorf("interaction store is not configured")
    }
    return loadOwnedTask(c, h.manager, h.authRequired)
}

func (h *InteractionHandler) resolveDecisionBy(c *gin.Context, task *coretasks.Task) string {
    return resolveTaskActor(c, task)
}
```

- [ ] **Step 5: Run focused tests**

Run: `go test ./app/handlers -run 'TestApprovalHandler|TestInteractionHandler|TestEnsureTaskOwnedByCurrentUser|TestResolveTaskActor' -v`
Expected: PASS for approval, interaction, and shared helper tests.

- [ ] **Step 6: Commit**

```bash
git add app/handlers/task_access.go app/handlers/task_access_test.go app/handlers/approval_handler.go app/handlers/interaction_handler.go
git commit -m "refactor(handlers): share task access helpers"
```

### Task 2: Remove duplicate approval-interaction creation work

**Files:**
- Modify: `core/tasks/manager.go:193-220`
- Modify: `core/tasks/runtime.go:74-89`
- Modify: `core/tasks/manager_test.go`

- [ ] **Step 1: Add the failing manager regression test**

Add this test to `core/tasks/manager_test.go`:

```go
func TestManagerCreateApprovalCreatesApprovalInteractionExactlyOnce(t *testing.T) {
    store := newTestStore(t)
    approvalStore, interactionStore := newApprovalAndInteractionStoresForTest(t, store)
    manager := NewManager(store, ManagerOptions{ApprovalStore: approvalStore, InteractionStore: interactionStore})

    task, err := manager.CreateTask(context.Background(), CreateTaskInput{TaskType: "agent.run", CreatedBy: "alice"})
    if err != nil {
        t.Fatalf("CreateTask() error = %v", err)
    }

    approval, err := manager.CreateApproval(context.Background(), approvals.CreateApprovalInput{
        TaskID:         task.ID,
        ConversationID: "conv-1",
        ToolCallID:     "call-1",
        ToolName:       "bash",
    })
    if err != nil {
        t.Fatalf("CreateApproval() error = %v", err)
    }

    interactions, err := interactionStore.ListTaskInteractions(context.Background(), task.ID)
    if err != nil {
        t.Fatalf("ListTaskInteractions() error = %v", err)
    }
    if len(interactions) != 1 {
        t.Fatalf("len(interactions) = %d, want 1", len(interactions))
    }
    if interactions[0].ToolCallID != approval.ToolCallID {
        t.Fatalf("interaction.ToolCallID = %q, want %q", interactions[0].ToolCallID, approval.ToolCallID)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./core/tasks -run TestManagerCreateApprovalCreatesApprovalInteractionExactlyOnce -v`
Expected: FAIL with duplicate interaction creation or unexpected interaction count.

- [ ] **Step 3: Keep interaction creation in one place**

`core/tasks/manager.go`

```go
func (m *Manager) CreateApproval(ctx context.Context, input approvals.CreateApprovalInput) (*approvals.ToolApproval, error) {
    if m == nil || m.approvals == nil || m.store == nil {
        return nil, fmt.Errorf("approval store is not configured")
    }
    approval, err := m.approvals.FindApprovalByToolCall(ctx, input.TaskID, input.ToolCallID)
    if err != nil {
        return nil, err
    }
    if approval == nil {
        approval, err = m.approvals.CreateApproval(ctx, input)
        if err != nil {
            return nil, err
        }
    }
    approval, events, err := m.finalizeCreatedApproval(ctx, approval)
    if err != nil {
        return nil, err
    }
    if _, err := m.ensureApprovalInteraction(ctx, approval); err != nil {
        return nil, err
    }
    if len(events) > 0 {
        m.publish(events...)
    }
    return approval, nil
}
```

`core/tasks/runtime.go`

```go
func (r *Runtime) CreateApproval(ctx context.Context, input approvals.CreateApprovalInput) (*approvals.ToolApproval, error) {
    if r == nil || r.manager == nil {
        return nil, fmt.Errorf("task runtime is not configured")
    }
    if input.TaskID == "" {
        input.TaskID = r.taskID
    }
    return r.manager.CreateApproval(ctx, input)
}
```

- [ ] **Step 4: Run focused tests**

Run: `go test ./core/tasks -run 'TestManagerCreateApprovalCreatesApprovalInteractionExactlyOnce|TestManagerResolveTaskApproval' -v`
Expected: PASS, including existing approval resume regressions.

- [ ] **Step 5: Commit**

```bash
git add core/tasks/manager.go core/tasks/runtime.go core/tasks/manager_test.go
git commit -m "refactor(tasks): remove duplicate approval interaction sync"
```

### Task 3: Consolidate raw JSON normalization for core stores

**Files:**
- Create: `pkg/jsonutil/raw_message.go`
- Create: `pkg/jsonutil/raw_message_test.go`
- Modify: `core/tasks/store.go:976-989`
- Modify: `core/audit/store.go:558-594`
- Modify: `core/interactions/store.go:265-270`
- Modify: `core/audit/store_test.go`
- Modify: `core/interactions/store_test.go`

- [ ] **Step 1: Write the failing shared JSON helper tests**

`pkg/jsonutil/raw_message_test.go`

```go
package jsonutil

import (
    "encoding/json"
    "testing"
)

func TestNormalizeRawMessageHandlesNilRawAndCompactJSON(t *testing.T) {
    got, err := NormalizeRawMessage(nil, true)
    if err != nil {
        t.Fatalf("NormalizeRawMessage(nil) error = %v", err)
    }
    if string(got) != "{}" {
        t.Fatalf("NormalizeRawMessage(nil) = %s, want {}", string(got))
    }

    got, err = NormalizeRawMessage([]byte(" {\n  \"a\": 1\n } "), false)
    if err != nil {
        t.Fatalf("NormalizeRawMessage(raw) error = %v", err)
    }
    if string(got) != `{"a":1}` {
        t.Fatalf("NormalizeRawMessage(raw) = %s, want compact json", string(got))
    }
}

func TestMarshalRawMessageRejectsInvalidJSONBytes(t *testing.T) {
    _, err := MarshalRawMessage(json.RawMessage("{"), false)
    if err == nil {
        t.Fatal("MarshalRawMessage(invalid json) error = nil, want error")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/jsonutil -v`
Expected: FAIL with missing package or undefined helpers.

- [ ] **Step 3: Add the shared helper**

`pkg/jsonutil/raw_message.go`

```go
package jsonutil

import (
    "bytes"
    "encoding/json"
    "fmt"
)

func MarshalRawMessage(value any, objectDefault bool) (json.RawMessage, error) {
    switch v := value.(type) {
    case nil:
        return defaultRawMessage(objectDefault), nil
    case json.RawMessage:
        return NormalizeRawMessage(v, objectDefault)
    case []byte:
        return NormalizeRawMessage(v, objectDefault)
    default:
        raw, err := json.Marshal(value)
        if err != nil {
            return nil, err
        }
        return json.RawMessage(raw), nil
    }
}

func NormalizeRawMessage(raw []byte, objectDefault bool) (json.RawMessage, error) {
    trimmed := bytes.TrimSpace(raw)
    if len(trimmed) == 0 {
        return defaultRawMessage(objectDefault), nil
    }
    if !json.Valid(trimmed) {
        return nil, fmt.Errorf("invalid json")
    }
    var compacted bytes.Buffer
    if err := json.Compact(&compacted, trimmed); err != nil {
        return nil, err
    }
    return json.RawMessage(compacted.Bytes()), nil
}

func defaultRawMessage(objectDefault bool) json.RawMessage {
    if objectDefault {
        return json.RawMessage("{}")
    }
    return json.RawMessage("null")
}
```

- [ ] **Step 4: Replace store-local helpers**

`core/tasks/store.go`

```go
import "github.com/EquentR/agent_runtime/pkg/jsonutil"

func marshalJSON(value any, objectDefault bool) (json.RawMessage, error) {
    return jsonutil.MarshalRawMessage(value, objectDefault)
}
```

`core/audit/store.go`

```go
import "github.com/EquentR/agent_runtime/pkg/jsonutil"

func marshalJSON(value any, objectDefault bool) (json.RawMessage, error) {
    return jsonutil.MarshalRawMessage(value, objectDefault)
}

func normalizeRawJSON(raw []byte, objectDefault bool) (json.RawMessage, error) {
    return jsonutil.NormalizeRawMessage(raw, objectDefault)
}
```

`core/interactions/store.go`

```go
import "github.com/EquentR/agent_runtime/pkg/jsonutil"

func marshalJSON(value any) ([]byte, error) {
    raw, err := jsonutil.MarshalRawMessage(value, false)
    if err != nil {
        return nil, err
    }
    if string(raw) == "null" {
        return nil, nil
    }
    return raw, nil
}
```

- [ ] **Step 5: Run focused tests**

Run: `go test ./pkg/jsonutil ./core/tasks ./core/audit ./core/interactions -v`
Expected: PASS, including existing store regressions.

- [ ] **Step 6: Commit**

```bash
git add pkg/jsonutil/raw_message.go pkg/jsonutil/raw_message_test.go core/tasks/store.go core/audit/store.go core/interactions/store.go
git commit -m "refactor(core): share raw json normalization"
```

### Task 4: Simplify conversation list enrichment and trust stored summaries

**Files:**
- Modify: `app/handlers/conversation_handler.go:220-277`
- Modify: `app/handlers/conversation_handler_test.go`
- Test: `core/agent/conversation_store_test.go`

- [ ] **Step 1: Add a failing conversation-list regression test**

Add this test to `app/handlers/conversation_handler_test.go`:

```go
func TestConversationHandlerListUsesStoredVisibleSummaryFields(t *testing.T) {
    store, _, server := newConversationHandlerTestServer(t)
    ctx := context.Background()
    if _, err := store.CreateConversation(ctx, coreagent.CreateConversationInput{ID: "conv_1", ProviderID: "openai", ModelID: "gpt-5.4"}); err != nil {
        t.Fatalf("CreateConversation() error = %v", err)
    }
    if err := store.AppendMessages(ctx, "conv_1", "task_1", []model.Message{
        {Role: model.RoleUser, Content: "hello"},
        {Role: model.RoleAssistant, Content: "hi"},
    }); err != nil {
        t.Fatalf("AppendMessages() error = %v", err)
    }

    resp, err := http.Get(server.URL + "/api/v1/conversations")
    if err != nil {
        t.Fatalf("http.Get() error = %v", err)
    }
    defer resp.Body.Close()
    got := decodeConversationListResponse(t, resp.Body)
    if len(got) != 1 {
        t.Fatalf("len(got) = %d, want 1", len(got))
    }
    if got[0].Title == "" || got[0].LastMessage == "" || got[0].MessageCount != 2 {
        t.Fatalf("conversation summary = %#v, want stored visible summary fields", got[0])
    }
}
```

- [ ] **Step 2: Run test to verify it fails or exposes the current over-enrichment path**

Run: `go test ./app/handlers -run TestConversationHandlerListUsesStoredVisibleSummaryFields -v`
Expected: FAIL before the list path stops rebuilding every summary from scratch.

- [ ] **Step 3: Split list enrichment from detail enrichment**

`app/handlers/conversation_handler.go`

```go
func (h *ConversationHandler) enrichConversations(ctx context.Context, conversations []coreagent.Conversation) []coreagent.Conversation {
    if len(conversations) == 0 {
        return conversations
    }
    enriched := make([]coreagent.Conversation, 0, len(conversations))
    for _, conversation := range conversations {
        enriched = append(enriched, h.enrichConversationListItem(ctx, &conversation))
    }
    sort.SliceStable(enriched, func(i, j int) bool {
        left := enriched[i]
        right := enriched[j]
        leftHasVisible := left.LastMessageAt != nil
        rightHasVisible := right.LastMessageAt != nil
        if leftHasVisible != rightHasVisible {
            return leftHasVisible
        }
        if leftHasVisible && rightHasVisible && !left.LastMessageAt.Equal(*right.LastMessageAt) {
            return left.LastMessageAt.After(*right.LastMessageAt)
        }
        if !left.CreatedAt.Equal(right.CreatedAt) {
            return left.CreatedAt.After(right.CreatedAt)
        }
        return left.ID > right.ID
    })
    return enriched
}

func (h *ConversationHandler) enrichConversationListItem(ctx context.Context, conversation *coreagent.Conversation) coreagent.Conversation {
    enriched := *conversation
    if enriched.Title == "" && h.store != nil && conversation.ID != "" {
        title, lastMessage, messageCount, lastMessageAt, err := h.store.BuildVisibleConversationSummary(ctx, conversation.ID)
        if err == nil {
            enriched.Title = title
            enriched.LastMessage = lastMessage
            enriched.MessageCount = messageCount
            enriched.LastMessageAt = lastMessageAt
        }
    }
    return enriched
}
```

Keep `enrichConversation(...)` for the detail route only, including audit run lookup.

- [ ] **Step 4: Run focused tests**

Run: `go test ./app/handlers ./core/agent -run 'TestConversationHandler|TestConversationStore' -v`
Expected: PASS for list/detail conversation behavior.

- [ ] **Step 5: Commit**

```bash
git add app/handlers/conversation_handler.go app/handlers/conversation_handler_test.go core/agent/conversation_store_test.go
git commit -m "refactor(conversations): slim list enrichment path"
```

### Task 5: Share skill-name normalization in one helper

**Files:**
- Create: `core/skills/names.go`
- Create: `core/skills/names_test.go`
- Modify: `core/skills/resolver.go:52-70`
- Modify: `core/agent/executor.go:177-195,252`
- Modify: `core/skills/resolver_test.go`
- Modify: `core/agent/executor_test.go`

- [ ] **Step 1: Add the failing shared helper tests**

`core/skills/names_test.go`

```go
package skills

import "testing"

func TestNormalizeNamesTrimsDedupesAndDropsEmpty(t *testing.T) {
    got := NormalizeNames([]string{" debug ", "", "debug", " review "})
    want := []string{"debug", "review"}
    if len(got) != len(want) {
        t.Fatalf("len(got) = %d, want %d", len(got), len(want))
    }
    for i := range want {
        if got[i] != want[i] {
            t.Fatalf("got[%d] = %q, want %q", i, got[i], want[i])
        }
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./core/skills -run TestNormalizeNamesTrimsDedupesAndDropsEmpty -v`
Expected: FAIL with undefined `NormalizeNames`.

- [ ] **Step 3: Add the shared helper and replace both copies**

`core/skills/names.go`

```go
package skills

import "strings"

func NormalizeNames(names []string) []string {
    if len(names) == 0 {
        return []string{}
    }
    seen := make(map[string]struct{}, len(names))
    result := make([]string, 0, len(names))
    for _, name := range names {
        trimmed := strings.TrimSpace(name)
        if trimmed == "" {
            continue
        }
        if _, ok := seen[trimmed]; ok {
            continue
        }
        seen[trimmed] = struct{}{}
        result = append(result, trimmed)
    }
    return result
}
```

`core/skills/resolver.go`

```go
names := NormalizeNames(input.Names)
```

`core/agent/executor.go`

```go
input.Skills = coreskills.NormalizeNames(input.Skills)
```

Delete the local `normalizeSkillNames` copies from both files.

- [ ] **Step 4: Run focused tests**

Run: `go test ./core/skills ./core/agent -run 'TestNormalizeNames|TestExecutor' -v`
Expected: PASS for skills resolver and executor coverage.

- [ ] **Step 5: Commit**

```bash
git add core/skills/names.go core/skills/names_test.go core/skills/resolver.go core/agent/executor.go
git commit -m "refactor(skills): share name normalization helper"
```

---

## Track 2: Frontend chat-path cleanup

### Task 6: Unify frontend API/session/message normalization at the boundary

**Files:**
- Modify: `webapp/src/lib/api.ts`
- Modify: `webapp/src/lib/session.ts`
- Modify: `webapp/src/lib/transcript.ts`
- Modify: `webapp/src/lib/api.spec.ts`
- Modify: `webapp/src/lib/session.spec.ts`
- Modify: `webapp/src/lib/transcript.spec.ts`

- [ ] **Step 1: Add failing normalization regressions**

Add these tests:

`webapp/src/lib/api.spec.ts`

```ts
it('normalizes stream-shaped assistant messages through the shared message normalizer', () => {
  expect(
    normalizeConversationMessage({
      Role: 'assistant',
      Content: 'hello',
      ProviderID: 'openai',
      ModelID: 'gpt-5.4',
      ToolCalls: [{ ID: 'call_1', Name: 'bash', Arguments: '{"cmd":"pwd"}' }],
    } as any),
  ).toMatchObject({
    role: 'assistant',
    content: 'hello',
    provider_id: 'openai',
    model_id: 'gpt-5.4',
    tool_calls: [{ id: 'call_1', name: 'bash', arguments: '{"cmd":"pwd"}' }],
  })
})
```

`webapp/src/lib/transcript.spec.ts`

```ts
it('reuses api message normalization for stream message payloads', () => {
  const entries = updateTranscriptFromStreamEvent([], {
    type: 'log.message',
    payload: {
      Kind: 'completed',
      Message: {
        Role: 'assistant',
        Content: 'done',
        ProviderID: 'openai',
        ModelID: 'gpt-5.4',
      },
    },
  } as any)

  expect(entries).toEqual([expect.objectContaining({ kind: 'reply', content: 'done', provider_id: 'openai' })])
})
```

- [ ] **Step 2: Run the failing tests**

Run: `pnpm --dir webapp exec vitest run src/lib/api.spec.ts src/lib/transcript.spec.ts`
Expected: FAIL before `transcript.ts` stops maintaining a separate message normalizer.

- [ ] **Step 3: Export one shared request helper and one shared message normalizer path**

`webapp/src/lib/api.ts`

```ts
export async function requestJSON<T>(basePath: string, path: string, init?: RequestInit) {
  const response = await fetch(`${basePath}${path}`, {
    credentials: 'include',
    headers: {
      'Content-Type': 'application/json',
      ...(init?.headers ?? {}),
    },
    ...init,
  })

  const payload = (await response.json()) as ApiEnvelope<T>
  return unwrapEnvelope(payload)
}

async function request<T>(path: string, init?: RequestInit) {
  return requestJSON<T>(API_BASE, path, init)
}
```

`webapp/src/lib/session.ts`

```ts
import { normalizeAuthUser, requestJSON, unwrapEnvelope } from './api'

async function requestAuth<T>(path: string, init?: RequestInit) {
  return requestJSON<T>('/api/v1/auth', path, init)
}
```

`webapp/src/lib/transcript.ts`

```ts
import {
  normalizeConversationMessage,
  normalizeInteractionRecord,
  normalizeToolApproval,
  normalizeTranscriptTokenUsage,
} from './api'

function normalizeStreamMessage(message: Record<string, unknown>) {
  return normalizeConversationMessage(message as Partial<ConversationMessage> & {
    Role?: string
    Content?: string
    ProviderID?: string
    ModelID?: string
    ToolCallId?: string
    ToolCallID?: string
    ToolCalls?: Array<{ ID?: string; Name?: string; Arguments?: string }>
    toolCalls?: Array<{ id?: string; name?: string; arguments?: string }>
  })
}
```

- [ ] **Step 4: Remove duplicate auth/session test blocks**

In `webapp/src/lib/api.spec.ts`, delete the repeated `describe('auth normalization helpers'...)` and `describe('approval API helpers'...)` blocks at the bottom of the file so there is only one copy of each suite.

- [ ] **Step 5: Run focused tests**

Run: `pnpm --dir webapp exec vitest run src/lib/api.spec.ts src/lib/session.spec.ts src/lib/transcript.spec.ts`
Expected: PASS for API, session, and transcript normalization.

- [ ] **Step 6: Commit**

```bash
git add webapp/src/lib/api.ts webapp/src/lib/session.ts webapp/src/lib/transcript.ts webapp/src/lib/api.spec.ts webapp/src/lib/session.spec.ts webapp/src/lib/transcript.spec.ts
git commit -m "refactor(webapp): unify api and transcript normalization"
```

### Task 7: Extract shared task-runtime helpers for chat and approval flows

**Files:**
- Create: `webapp/src/lib/task-runtime.ts`
- Create: `webapp/src/lib/task-runtime.spec.ts`
- Modify: `webapp/src/views/ChatView.vue`
- Modify: `webapp/src/views/ApprovalView.vue`
- Modify: `webapp/src/views/ChatView.spec.ts`
- Modify: `webapp/src/views/ApprovalView.spec.ts`
- Modify: `webapp/src/types/api.ts`

- [ ] **Step 1: Add the failing shared-helper tests**

`webapp/src/lib/task-runtime.spec.ts`

```ts
import { describe, expect, it } from 'vitest'
import { buildApprovalEntriesFromList, isTaskActive, resolveTaskConversationId } from './task-runtime'

describe('task-runtime helpers', () => {
  it('resolves task conversation ids from result, result_json, or input', () => {
    expect(resolveTaskConversationId({ result: { conversation_id: 'conv_a' } } as any)).toBe('conv_a')
    expect(resolveTaskConversationId({ result_json: { conversation_id: 'conv_b' } } as any)).toBe('conv_b')
    expect(resolveTaskConversationId({ input: { conversation_id: 'conv_c' } } as any)).toBe('conv_c')
  })

  it('treats queued, running, waiting, and cancel_requested as active', () => {
    expect(isTaskActive({ status: 'queued' } as any)).toBe(true)
    expect(isTaskActive({ status: 'succeeded' } as any)).toBe(false)
  })

  it('maps approval records directly into approval transcript entries', () => {
    const entries = buildApprovalEntriesFromList([
      {
        id: 'approval_1',
        task_id: 'task_1',
        conversation_id: 'conv_1',
        step_index: 1,
        tool_call_id: 'call_1',
        tool_name: 'bash',
        arguments_summary: 'pwd',
        risk_level: 'low',
        status: 'pending',
      },
    ] as any)

    expect(entries).toEqual([expect.objectContaining({ kind: 'approval', approval: expect.objectContaining({ id: 'approval_1' }) })])
  })
})
```

- [ ] **Step 2: Run the failing tests**

Run: `pnpm --dir webapp exec vitest run src/lib/task-runtime.spec.ts`
Expected: FAIL with missing helper module.

- [ ] **Step 3: Add shared task/runtime helpers**

`webapp/src/lib/task-runtime.ts`

```ts
import type { TaskDetails, ToolApproval, TranscriptEntry } from '../types/api'
import { buildApprovalStreamEvent, updateTranscriptFromStreamEvent } from './transcript'

export const ACTIVE_TASK_STATUSES = ['queued', 'running', 'waiting', 'cancel_requested'] as const
export const WAITING_FOR_TOOL_APPROVAL = 'waiting_for_tool_approval'
export const WAITING_FOR_INTERACTION = 'waiting_for_interaction'

export function resolveTaskConversationId(task: TaskDetails | null | undefined) {
  return task?.result?.conversation_id ?? task?.result_json?.conversation_id ?? task?.input?.conversation_id ?? ''
}

export function isTaskActive(task: TaskDetails | null | undefined) {
  return ACTIVE_TASK_STATUSES.includes((task?.status ?? '') as (typeof ACTIVE_TASK_STATUSES)[number])
}

export function buildApprovalEntriesFromList(nextApprovals: ToolApproval[]) {
  let nextEntries: TranscriptEntry[] = []
  for (const approval of nextApprovals) {
    nextEntries = updateTranscriptFromStreamEvent(nextEntries, buildApprovalStreamEvent(approval))
  }
  return nextEntries
}
```

- [ ] **Step 4: Replace duplicated view-local helpers**

`webapp/src/views/ApprovalView.vue`

```ts
import { buildApprovalEntriesFromList, isTaskActive, resolveTaskConversationId } from '../lib/task-runtime'

const taskConversationId = computed(() => resolveTaskConversationId(task.value))

function applyApprovalList(nextApprovals: ToolApproval[]) {
  approvalEntries.value = buildApprovalEntriesFromList(nextApprovals)
}
```

`webapp/src/views/ChatView.vue`

```ts
import {
  isTaskActive,
  resolveTaskConversationId,
  WAITING_FOR_INTERACTION,
  WAITING_FOR_TOOL_APPROVAL,
} from '../lib/task-runtime'

async function hydratePendingApprovals(task: TaskDetails | null | undefined, conversationId = '') {
  if (!task || task.status !== 'waiting' || (task.suspend_reason !== WAITING_FOR_TOOL_APPROVAL && task.suspend_reason !== WAITING_FOR_INTERACTION)) {
    return
  }
  // keep the existing logic, but use the shared constants and resolver
}
```

Also widen `TaskSnapshot['suspend_reason']` in `webapp/src/types/api.ts` to a string union that includes the known waiting reasons instead of using raw string checks everywhere.

- [ ] **Step 5: Run focused tests**

Run: `pnpm --dir webapp exec vitest run src/lib/task-runtime.spec.ts src/views/ChatView.spec.ts src/views/ApprovalView.spec.ts`
Expected: PASS for shared runtime helpers and both view suites.

- [ ] **Step 6: Commit**

```bash
git add webapp/src/lib/task-runtime.ts webapp/src/lib/task-runtime.spec.ts webapp/src/views/ChatView.vue webapp/src/views/ApprovalView.vue webapp/src/types/api.ts
git commit -m "refactor(webapp): share task runtime helpers"
```

### Task 8: Normalize question-entry data once for `MessageList`

**Files:**
- Create: `webapp/src/lib/question-entry.ts`
- Create: `webapp/src/lib/question-entry.spec.ts`
- Modify: `webapp/src/components/MessageList.vue`
- Modify: `webapp/src/components/MessageList.spec.ts`

- [ ] **Step 1: Add the failing question-normalization tests**

`webapp/src/lib/question-entry.spec.ts`

```ts
import { describe, expect, it } from 'vitest'
import { normalizeQuestionEntry, summarizeQuestionResponse } from './question-entry'

describe('question-entry helpers', () => {
  it('normalizes options, prompt, placeholder, and flags once', () => {
    const question = normalizeQuestionEntry({
      kind: 'question',
      question_interaction: {
        id: 'interaction_1',
        status: 'pending',
        request_json: {
          question: 'Which environment?',
          options: ['staging', 'production'],
          placeholder: '补充你的回答',
          allow_custom: true,
          multiple: false,
        },
      },
    } as any)

    expect(question).toMatchObject({
      id: 'interaction_1',
      prompt: 'Which environment?',
      options: ['staging', 'production'],
      allowCustom: true,
      multiple: false,
    })
  })

  it('summarizes stored response payloads into visible text', () => {
    expect(summarizeQuestionResponse({ selected_option_id: 'staging', custom_text: 'ASAP' })).toBe('staging\nASAP')
  })
})
```

- [ ] **Step 2: Run the failing tests**

Run: `pnpm --dir webapp exec vitest run src/lib/question-entry.spec.ts`
Expected: FAIL with missing helper module.

- [ ] **Step 3: Add the question normalizer**

`webapp/src/lib/question-entry.ts`

```ts
import type { TranscriptEntry } from '../types/api'

export const CUSTOM_QUESTION_OPTION_VALUE = '__custom__'

export function normalizeQuestionEntry(entry: TranscriptEntry) {
  if (entry.kind !== 'question' || !entry.question_interaction) {
    return null
  }
  const request = entry.question_interaction.request_json ?? {}
  const rawOptions = request.options
  return {
    id: entry.question_interaction.id,
    status: entry.question_interaction.status,
    prompt: String(request.question ?? ''),
    placeholder: String(request.placeholder ?? '补充你的回答'),
    options: Array.isArray(rawOptions) ? rawOptions.map((item) => String(item)).filter(Boolean) : [],
    allowCustom: request.allow_custom === true,
    multiple: request.multiple === true,
    response: entry.question_interaction.response_json,
    interaction: entry.question_interaction,
  }
}

export function summarizeQuestionResponse(response: Record<string, unknown> | undefined) {
  if (!response || typeof response !== 'object') {
    return ''
  }
  const parts: string[] = []
  const selectedOptionId = typeof response.selected_option_id === 'string' ? response.selected_option_id.trim() : ''
  const selectedOptionIds = Array.isArray(response.selected_option_ids)
    ? response.selected_option_ids.map((value) => String(value).trim()).filter(Boolean)
    : []
  const customText = typeof response.custom_text === 'string' ? response.custom_text.trim() : ''
  if (selectedOptionId) parts.push(selectedOptionId)
  if (selectedOptionIds.length > 0) parts.push(selectedOptionIds.join('、'))
  if (customText) parts.push(customText)
  return parts.join('\n')
}
```

- [ ] **Step 4: Replace repeated question parsing in `MessageList.vue`**

At the top of `webapp/src/components/MessageList.vue`:

```ts
import { CUSTOM_QUESTION_OPTION_VALUE, normalizeQuestionEntry, summarizeQuestionResponse } from '../lib/question-entry'
```

Then replace the repeated helpers with one local accessor:

```ts
function questionData(entry: TranscriptEntry) {
  return normalizeQuestionEntry(entry)
}

function questionOptions(entry: TranscriptEntry) {
  return questionData(entry)?.options ?? []
}

function questionPrompt(entry: TranscriptEntry) {
  return questionData(entry)?.prompt ?? ''
}

function questionPlaceholder(entry: TranscriptEntry) {
  return questionData(entry)?.placeholder ?? ''
}

function questionAllowsCustom(entry: TranscriptEntry) {
  return questionData(entry)?.allowCustom ?? false
}

function questionMultiple(entry: TranscriptEntry) {
  return questionData(entry)?.multiple ?? false
}

function questionFinalAnswer(entry: TranscriptEntry) {
  return summarizeQuestionResponse(questionData(entry)?.response as Record<string, unknown> | undefined)
}
```

- [ ] **Step 5: Run focused tests**

Run: `pnpm --dir webapp exec vitest run src/lib/question-entry.spec.ts src/components/MessageList.spec.ts`
Expected: PASS for question rendering and submission helpers.

- [ ] **Step 6: Commit**

```bash
git add webapp/src/lib/question-entry.ts webapp/src/lib/question-entry.spec.ts webapp/src/components/MessageList.vue webapp/src/components/MessageList.spec.ts
git commit -m "refactor(webapp): normalize question entries once"
```

### Task 9: Reduce chat-path reactive churn and fix fragile sidebar markup

**Files:**
- Modify: `webapp/src/lib/chat-state.ts`
- Modify: `webapp/src/lib/chat-state.spec.ts`
- Modify: `webapp/src/views/ChatView.vue:129-220,585-810`
- Modify: `webapp/src/views/ChatView.spec.ts`
- Modify: `webapp/src/components/ConversationSidebar.vue`
- Modify: `webapp/src/components/ConversationSidebar.spec.ts`

- [ ] **Step 1: Add the failing persistence and startup regression tests**

Add this test to `webapp/src/lib/chat-state.spec.ts`:

```ts
it('stores only durable chat state fields', () => {
  saveChatState({
    activeConversationId: 'conv_1',
    activeTaskId: 'task_1',
    activeTaskEventSeq: 7,
    entries: [{ id: 'reply-1', kind: 'reply', title: '', content: 'hello' } as any],
    draftEntriesByConversation: { conv_1: [{ id: 'draft-1', kind: 'reply', title: '', content: 'partial' } as any] },
    selectedSkillsByConversation: { conv_1: ['debugging'] },
  })

  const parsed = JSON.parse(localStorage.getItem('agent-runtime.chat-state') || '{}')
  expect(parsed.activeConversationId).toBe('conv_1')
  expect(parsed.activeTaskId).toBe('task_1')
})
```

Add this test to `webapp/src/views/ChatView.spec.ts`:

```ts
it('starts catalog, skills, and conversations loading during initial mount', async () => {
  const catalog = createDeferred<any>()
  const skills = createDeferred<any>()
  const conversations = createDeferred<any>()

  api.fetchModelCatalog.mockReturnValue(catalog.promise)
  api.fetchSkills.mockReturnValue(skills.promise)
  api.fetchConversations.mockReturnValue(conversations.promise)

  const router = makeRouter()
  await router.push('/chat')
  await router.isReady()

  mount(ChatView, { global: { plugins: [router] } })
  await flushPromises()

  expect(api.fetchModelCatalog).toHaveBeenCalledTimes(1)
  expect(api.fetchSkills).toHaveBeenCalledTimes(1)
  expect(api.fetchConversations).toHaveBeenCalledTimes(1)

  catalog.resolve({ default_provider_id: 'openai', default_model_id: 'gpt-5.4', providers: [] })
  skills.resolve([])
  conversations.resolve([])
})
```

- [ ] **Step 2: Run the failing tests**

Run: `pnpm --dir webapp exec vitest run src/lib/chat-state.spec.ts src/views/ChatView.spec.ts src/components/ConversationSidebar.spec.ts`
Expected: FAIL before the state-saving and sidebar/template cleanup lands.

- [ ] **Step 3: Batch state writes and parallelize startup**

`webapp/src/lib/chat-state.ts`

```ts
let pendingSave: number | null = null
let latestState: ChatState | null = null

export function scheduleChatStateSave(state: ChatState) {
  latestState = state
  if (pendingSave != null) {
    return
  }
  pendingSave = window.setTimeout(() => {
    pendingSave = null
    if (latestState) {
      localStorage.setItem(CHAT_STATE_KEY, JSON.stringify(latestState))
    }
  }, 50)
}
```

`webapp/src/views/ChatView.vue`

```ts
import { clearChatState, loadChatState, scheduleChatStateSave } from '../lib/chat-state'

function syncChatState() {
  scheduleChatStateSave({
    activeConversationId: activeConversationId.value,
    activeTaskId: activeTaskId.value,
    activeTaskEventSeq: activeTaskEventSeq.value,
    entries: entries.value,
    draftEntriesByConversation: draftEntriesByConversation.value,
    selectedSkillsByConversation: selectedSkillsByConversation.value,
  })
}

async function loadConversations(preferredConversationId = '') {
  sidebarLoading.value = true
  try {
    const loadedConversations = await fetchConversations()
    conversations.value = Array.isArray(loadedConversations) ? loadedConversations : []
    const loadedIds = new Set(conversations.value.map((conversation) => conversation.id))
    pendingConversationById.value = Object.fromEntries(
      Object.entries(pendingConversationById.value).filter(([conversationId]) => !loadedIds.has(conversationId)),
    )
    // keep the remainder of the selection logic unchanged
  } finally {
    sidebarLoading.value = false
  }
}

onMounted(async () => {
  // same setup work
  const saved = loadChatState()
  activeConversationId.value = saved.activeConversationId
  activeTaskId.value = saved.activeTaskId
  activeTaskEventSeq.value = saved.activeTaskEventSeq
  entries.value = saved.entries
  draftEntriesByConversation.value = saved.draftEntriesByConversation
  selectedSkillsByConversation.value = saved.selectedSkillsByConversation
  await Promise.all([loadCatalog(), loadAvailableSkills(), loadConversations()])
  if (!activeConversationId.value || !syncSelectionFromConversation(activeConversationId.value)) {
    applyDefaultSelection()
  }
  await resumeSavedTask()
})
```

- [ ] **Step 4: Remove nested button markup from the conversation sidebar**

In `webapp/src/components/ConversationSidebar.vue`, change each conversation row from “button containing a delete button” to a non-button row wrapper with sibling action buttons, e.g.:

```vue
<div
  v-for="conversation in conversations"
  :key="conversation.id"
  class="conversation-card"
  :class="{ active: conversation.id === activeConversationId }"
  :data-conversation-id="conversation.id"
>
  <button
    type="button"
    class="conversation-card-main"
    @click="$emit('select-conversation', conversation.id)"
  >
    <!-- existing title / preview markup -->
  </button>
  <button
    type="button"
    class="conversation-delete-button"
    @click.stop="$emit('delete-conversation', conversation.id)"
  >
    删除
  </button>
</div>
```

Keep the existing styles/colors, matching the remembered audit/chat visual language instead of introducing new native-looking controls.

- [ ] **Step 5: Run focused tests**

Run: `pnpm --dir webapp exec vitest run src/lib/chat-state.spec.ts src/views/ChatView.spec.ts src/components/ConversationSidebar.spec.ts`
Expected: PASS for startup behavior, saved-state behavior, and sidebar interaction markup.

- [ ] **Step 6: Commit**

```bash
git add webapp/src/lib/chat-state.ts webapp/src/lib/chat-state.spec.ts webapp/src/views/ChatView.vue webapp/src/views/ChatView.spec.ts webapp/src/components/ConversationSidebar.vue webapp/src/components/ConversationSidebar.spec.ts
git commit -m "refactor(webapp): reduce chat path reactive churn"
```

---

## Track 3: Frontend admin cleanup

### Task 10: Share admin helpers and flatten the audit loading waterfall

**Files:**
- Create: `webapp/src/lib/model-selection.ts`
- Create: `webapp/src/lib/model-selection.spec.ts`
- Create: `webapp/src/lib/time.ts`
- Create: `webapp/src/lib/time.spec.ts`
- Modify: `webapp/src/views/AdminPromptView.vue`
- Modify: `webapp/src/views/AdminPromptView.spec.ts`
- Modify: `webapp/src/views/AdminAuditView.vue`
- Modify: `webapp/src/views/AdminAuditView.spec.ts`
- Modify: `webapp/src/views/ChatView.vue`

- [ ] **Step 1: Add the failing shared-helper tests**

`webapp/src/lib/model-selection.spec.ts`

```ts
import { describe, expect, it } from 'vitest'
import { resolveProviderById, resolveProviderDefaultModel } from './model-selection'

describe('model-selection helpers', () => {
  const providers = [
    { id: 'openai', name: 'OpenAI', models: [{ id: 'gpt-5.4', name: 'GPT 5.4', type: 'chat' }] },
    { id: 'google', name: 'Google', models: [{ id: 'gemini-2.5-flash', name: 'Gemini 2.5 Flash', type: 'chat' }] },
  ]

  it('finds providers and keeps valid fallback models', () => {
    const provider = resolveProviderById(providers as any, 'openai')
    expect(resolveProviderDefaultModel(provider, 'gpt-5.4')).toBe('gpt-5.4')
    expect(resolveProviderDefaultModel(provider, 'missing')).toBe('gpt-5.4')
  })
})
```

`webapp/src/lib/time.spec.ts`

```ts
import { describe, expect, it } from 'vitest'
import { formatCompactTimestamp } from './time'

describe('time helper', () => {
  it('formats ISO timestamps for compact display', () => {
    expect(formatCompactTimestamp('2026-03-22T10:00:00Z')).toBe('2026-03-22 10:00')
    expect(formatCompactTimestamp('')).toBe('--')
  })
})
```

Add this concurrency regression to `webapp/src/views/AdminAuditView.spec.ts`:

```ts
it('starts conversation and run loading together when selecting a conversation', async () => {
  const conversation = createDeferred<any>()
  const runs = createDeferred<any[]>()
  api.fetchConversations.mockResolvedValue([{ id: 'conv_1', title: 'First chat', last_message: '', message_count: 1, provider_id: 'openai', model_id: 'gpt-5.4', created_by: 'alice', created_at: '2026-03-22T09:00:00Z', updated_at: '2026-03-22T09:01:00Z' }])
  api.fetchConversation.mockReturnValue(conversation.promise)
  api.fetchAuditConversationRuns.mockReturnValue(runs.promise)
  api.fetchAuditRunReplay.mockResolvedValue({ run: {}, timeline: [], artifacts: [] })

  const wrapper = mount(AdminAuditView)
  await flushPromises()
  await wrapper.get('[data-conversation-id="conv_1"]').trigger('click')

  expect(api.fetchConversation).toHaveBeenCalledWith('conv_1')
  expect(api.fetchAuditConversationRuns).toHaveBeenCalledWith('conv_1')

  conversation.resolve({ id: 'conv_1', title: 'First chat', last_message: '', message_count: 1, provider_id: 'openai', model_id: 'gpt-5.4', created_by: 'alice', created_at: '2026-03-22T09:00:00Z', updated_at: '2026-03-22T09:01:00Z' })
  runs.resolve([])
  await flushPromises()
})
```

- [ ] **Step 2: Run the failing tests**

Run: `pnpm --dir webapp exec vitest run src/lib/model-selection.spec.ts src/lib/time.spec.ts src/views/AdminAuditView.spec.ts src/views/AdminPromptView.spec.ts`
Expected: FAIL with missing helper modules and serialized audit loading.

- [ ] **Step 3: Add shared provider/model and time helpers**

`webapp/src/lib/model-selection.ts`

```ts
import type { ModelCatalogProvider } from '../types/api'

export function resolveProviderById(providers: ModelCatalogProvider[], providerId: string) {
  return providers.find((provider) => provider.id === providerId) ?? null
}

export function resolveProviderDefaultModel(provider: ModelCatalogProvider | null, fallbackModelId = '', emptyFallback = '') {
  if (!provider) {
    return emptyFallback
  }
  if (fallbackModelId && provider.models.some((model) => model.id === fallbackModelId)) {
    return fallbackModelId
  }
  return provider.models[0]?.id ?? emptyFallback
}
```

`webapp/src/lib/time.ts`

```ts
export function formatCompactTimestamp(value?: string) {
  if (!value) {
    return '--'
  }
  return value.replace('T', ' ').slice(0, 16)
}
```

- [ ] **Step 4: Replace duplicated view-local helpers and parallelize admin audit loading**

`webapp/src/views/AdminPromptView.vue`

```ts
import { resolveProviderById, resolveProviderDefaultModel } from '../lib/model-selection'
import { formatCompactTimestamp } from '../lib/time'

function resolveBindingProvider(providerId: string) {
  return resolveProviderById(availableBindingProviders.value, providerId)
}

function resolveBindingProviderDefaultModel(provider: ModelCatalogProvider | null, fallbackModelId = '') {
  return resolveProviderDefaultModel(provider, fallbackModelId, fallbackModelId)
}

function formatTime(value: string) {
  return formatCompactTimestamp(value)
}
```

`webapp/src/views/ChatView.vue`

```ts
import { resolveProviderById, resolveProviderDefaultModel } from '../lib/model-selection'

function resolveProvider(providerId: string) {
  return resolveProviderById(availableProviders.value, providerId)
}

function resolveProviderDefaultModel(provider: ModelCatalogProvider | null, fallbackModelId = '') {
  return resolveProviderDefaultModel(provider, fallbackModelId)
}
```

`webapp/src/views/AdminAuditView.vue`

```ts
import { formatCompactTimestamp } from '../lib/time'

function formatConversationTime(value?: string) {
  return formatCompactTimestamp(value)
}

async function selectConversation(conversationId: string) {
  selectedConversationId.value = conversationId
  detailLoading.value = true
  errorMessage.value = ''
  auditRuns.value = []
  auditReplays.value = []
  selectedTurnIndex.value = null
  expandedTimelineKey.value = null
  activeFilter.value = 'all'
  summaryExpanded.value = false
  closeTurnMenu()

  try {
    const [conversation, runs] = await Promise.all([
      fetchConversation(conversationId),
      fetchAuditConversationRuns(conversationId),
    ])
    selectedConversation.value = conversation
    auditRuns.value = runs
    if (runs.length === 0) {
      return
    }
    auditReplays.value = await Promise.all(runs.map((run) => fetchAuditRunReplay(run.id)))
    const first = mergedTimeline.value[0]
    expandedTimelineKey.value = first ? timelineEntryKey(first) : null
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : '加载审计详情失败'
  } finally {
    detailLoading.value = false
  }
}
```

- [ ] **Step 5: Run focused tests**

Run: `pnpm --dir webapp exec vitest run src/lib/model-selection.spec.ts src/lib/time.spec.ts src/views/AdminPromptView.spec.ts src/views/AdminAuditView.spec.ts src/views/ChatView.spec.ts`
Expected: PASS for provider/model fallback reuse, timestamp reuse, and audit detail concurrency.

- [ ] **Step 6: Commit**

```bash
git add webapp/src/lib/model-selection.ts webapp/src/lib/model-selection.spec.ts webapp/src/lib/time.ts webapp/src/lib/time.spec.ts webapp/src/views/AdminPromptView.vue webapp/src/views/AdminAuditView.vue webapp/src/views/ChatView.vue
git commit -m "refactor(webapp): share admin helpers and flatten audit loading"
```

---

## Final verification

- [ ] **Step 1: Run backend test suite**

Run: `go test ./...`
Expected: PASS for all Go packages.

- [ ] **Step 2: Run backend build validation**

Run: `go build ./cmd/...`
Expected: successful build with no compile errors.

- [ ] **Step 3: Run frontend type-check**

Run: `pnpm --dir webapp exec vue-tsc -b`
Expected: PASS with no type errors.

- [ ] **Step 4: Run frontend test suite**

Run: `pnpm --dir webapp test -- --runInBand`
Expected: PASS for webapp Vitest suite.

- [ ] **Step 5: Review diff before integration**

Run: `git diff --stat && git diff`
Expected: changes grouped by the 10 tasks above; no unrelated file churn.

- [ ] **Step 6: Final integration commit**

```bash
git add app/handlers core pkg webapp/src
git commit -m "refactor: simplify runtime and webapp duplication"
```
