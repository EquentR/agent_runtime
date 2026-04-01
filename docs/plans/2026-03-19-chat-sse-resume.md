# Chat SSE Resume Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Restore live SSE updates when reopening a conversation whose task is still running, including route leave/return recovery.

**Architecture:** Persist the active streaming task in frontend chat state so a remounted chat view can reconnect to the same task stream. Add a task lookup endpoint keyed by `conversation_id` so selecting a historical conversation can discover an in-flight task and resume SSE even without cached task state. Keep the fix narrow: reuse existing task detail and SSE APIs, and only add the minimum backend query needed for conversation-to-task lookup.

**Tech Stack:** Vue 3, Vitest, TypeScript, Go, Gin, GORM, SQLite JSON queries

---

### Task 1: Frontend regression coverage for stream resumption

**Files:**
- Modify: `webapp/src/views/ChatView.spec.ts`

**Step 1: Write the failing test**

Add a spec that mounts `ChatView`, restores a saved running task from local storage, loads a persisted conversation, and expects `streamRunTask` to be called again after the view remount logic finishes.

**Step 2: Run test to verify it fails**

Run: `pnpm vitest run src/views/ChatView.spec.ts -t "resumes SSE for a running task after reopening the chat view"`
Expected: FAIL because the view currently reloads history only and never reconnects the SSE stream.

**Step 3: Write minimal implementation**

Update the view/state flow to persist the current task id and reconnect when the restored task is still active.

**Step 4: Run test to verify it passes**

Run: `pnpm vitest run src/views/ChatView.spec.ts -t "resumes SSE for a running task after reopening the chat view"`
Expected: PASS

### Task 2: Backend coverage for conversation-to-running-task lookup

**Files:**
- Modify: `app/handlers/task_handler_test.go`
- Modify: `app/handlers/task_handler.go`
- Modify: `core/tasks/manager.go`
- Modify: `core/tasks/store.go`

**Step 1: Write the failing test**

Add a handler test covering a request such as `GET /api/v1/tasks/running?conversation_id=conv_1` and assert it returns the latest non-terminal task linked to that conversation.

**Step 2: Run test to verify it fails**

Run: `go test ./app/handlers -run TestTaskHandlerFindsRunningTaskByConversation`
Expected: FAIL because no lookup route/query exists yet.

**Step 3: Write minimal implementation**

Add a small manager/store query that finds the most recent non-terminal task where `input_json.conversation_id` or `result_json.conversation_id` matches the requested conversation id, then expose it from the task handler.

**Step 4: Run test to verify it passes**

Run: `go test ./app/handlers -run TestTaskHandlerFindsRunningTaskByConversation`
Expected: PASS

### Task 3: Resume logic in ChatView

**Files:**
- Modify: `webapp/src/views/ChatView.vue`
- Modify: `webapp/src/lib/chat-state.ts`
- Modify: `webapp/src/lib/api.ts`
- Modify: `webapp/src/types/api.ts`

**Step 1: Write the failing test**

Extend frontend coverage so selecting a conversation history entry with an active backend task triggers the new lookup call and then reconnects `streamRunTask`.

**Step 2: Run test to verify it fails**

Run: `pnpm vitest run src/views/ChatView.spec.ts -t "reconnects SSE when selecting a conversation with a running task"`
Expected: FAIL because selection currently stops after loading messages.

**Step 3: Write minimal implementation**

Persist active task metadata in local storage, clear it on terminal task states, and on mount/selection resolve the matching running task then resubscribe to its SSE stream.

**Step 4: Run test to verify it passes**

Run: `pnpm vitest run src/views/ChatView.spec.ts`
Expected: PASS

### Task 4: Final verification

**Files:**
- Modify: `docs/plans/2026-03-19-chat-sse-resume.md`

**Step 1: Run focused backend tests**

Run: `go test ./app/handlers ./core/tasks`
Expected: PASS

**Step 2: Run focused frontend tests**

Run: `pnpm vitest run src/views/ChatView.spec.ts src/lib/api.spec.ts`
Expected: PASS

**Step 3: Run broader verification if time allows**

Run: `go test ./...` and `pnpm vitest run`
Expected: PASS
