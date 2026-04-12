# Memory Context Authoritative Display Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make the chat context panel display the backend memory manager's authoritative context and compression numbers, both while streaming and after conversation reload, instead of reconstructing values from reply usage or transcript text.

**Architecture:** Keep the backend as the only source of truth. Extend the memory manager snapshot/compression contract so the runner emits structured memory events, the executor persists the latest memory snapshot onto the conversation/result payloads, and the frontend consumes those payloads directly. Remove heuristic frontend fallbacks that derive context usage from session history, parsed compression strings, or reply prompt token counts.

**Tech Stack:** Go 1.25, Gin, GORM/SQLite, Vue 3 + TypeScript, Vitest, vue-tsc

---

## Background & Constraints

- Current frontend `ChatView.vue` computes context usage with a three-level fallback: `memory.context_state` -> parsed `memory.compressed` text -> `reply.token_usage.prompt_tokens`.
- `webapp/src/lib/api.ts` currently subscribes to `memory.compressed` but not `memory.context_state`, so the authoritative branch is mostly dead during live streaming.
- `core/memory/manager.go` is the authoritative source for memory usage, but its current `ContextState()` and `CompressionTrace` payloads do not expose a single backend-provided total that the frontend can render directly.
- `Conversation` persistence currently stores `memory_summary` only. That is enough to resume memory continuity, but not enough for the frontend to reconstruct the last authoritative display after reload.
- The current shared workspace already contains relevant uncommitted frontend context-display changes. Execute this plan against the shared workspace, not a clean branch snapshot that omits those changes.
- Do not create commits unless the user explicitly asks for them.

---

### Task 1: Define the authoritative backend memory snapshot/event contract

**Files:**
- Modify: `core/memory/manager.go`
- Modify: `core/memory/message_budget.go`
- Modify: `core/agent/events.go`
- Modify: `core/agent/request_budget.go`
- Modify: `core/agent/memory.go`
- Modify: `core/tasks/types.go`
- Test: `core/agent/request_budget_test.go`
- Test: `core/agent/memory_test.go`

**Step 1: Write the failing tests**

Add focused Go tests that lock the contract before changing production code.

- In `core/agent/request_budget_test.go`, add a test like `TestBuildBudgetedRequestEmitsAuthoritativeMemoryContextStateAndCompressionTotals`.
- In `core/agent/memory_test.go`, add a test like `TestPrepareConversationContextEmitsMemorySnapshotWhenInitialCompressionOccurs`.

The assertions should require:

- `memory.context_state` to include backend-authored totals, not only short-term pieces.
- `memory.compressed` to include structured numeric fields for authoritative before/after values.
- `prepareConversationContextWithPersistedCount()` to stop swallowing compression/state changes that happen before the later request-budget path.

**Step 2: Run the focused tests to confirm they fail**

Run:

```bash
go test ./core/agent -run 'TestBuildBudgetedRequestEmitsAuthoritativeMemoryContextStateAndCompressionTotals|TestPrepareConversationContextEmitsMemorySnapshotWhenInitialCompressionOccurs' -v
```

Expected: FAIL because the current event payloads are incomplete and the initial preparation path does not emit the needed snapshot/event data.

**Step 3: Implement the minimal backend contract changes**

Make the backend produce one authoritative memory snapshot shape that the frontend can render without recomputing.

- Extend `core/memory/manager.go`:
  - Add rendered-summary and total token fields to `ContextState`.
  - Extend `CompressionTrace` with authoritative before/after totals so compression no longer only describes short-term tail tokens.
  - Add helper(s) inside the manager to compute rendered summary tokens and total managed-context tokens consistently.
- Update `core/agent/events.go` so `emitMemoryContextState(...)` and `emitMemoryCompressed(...)` emit the structured fields from the manager instead of the current partial payload.
- Add `EventMemoryContextState = "memory.context_state"` to `core/tasks/types.go`.
- Update `core/agent/request_budget.go` to emit the richer structured payload after request-budget preparation.
- Update `core/agent/memory.go` so `prepareConversationContextWithPersistedCount()` uses `RuntimeContextWithReserve(...)` rather than discarding the trace, and emits compression/state when compression already occurs in this early path.

Keep the payload backward compatible where practical by preserving existing keys like `tokens_before` / `tokens_after` while adding the authoritative total/breakdown fields the frontend will use.

**Step 4: Re-run the focused tests**

Run:

```bash
go test ./core/agent -run 'TestBuildBudgetedRequestEmitsAuthoritativeMemoryContextStateAndCompressionTotals|TestPrepareConversationContextEmitsMemorySnapshotWhenInitialCompressionOccurs' -v
```

Expected: PASS.

**Step 5: Run the package-level regression tests**

Run:

```bash
go test ./core/agent
```

Expected: PASS.

**Step 6: Do not commit**

Do not create a commit in this session unless the user explicitly asks.

---

### Task 2: Persist the latest authoritative memory snapshot for reload and task completion

**Files:**
- Modify: `core/agent/executor.go`
- Modify: `core/agent/conversation_store.go`
- Modify: `app/migration/define.go`
- Modify: `app/migration/init.go`
- Modify: `app/handlers/swagger_types.go`
- Test: `core/agent/executor_test.go`
- Test: `core/agent/conversation_store_test.go`
- Test: `app/handlers/conversation_handler_test.go`

**Step 1: Write the failing tests**

Add tests that prove reload can use backend-authored memory state without replay-side guessing.

- In `core/agent/executor_test.go`, add a test like `TestAgentExecutorPersistsLatestMemoryContextAndCompressionIntoConversationAndResult`.
- In `core/agent/conversation_store_test.go`, add a test like `TestConversationStorePersistsMemorySnapshotFields`.
- In `app/handlers/conversation_handler_test.go`, add a test like `TestConversationHandlerGetConversationIncludesLatestMemorySnapshot`.

Require the tests to verify:

- `RunTaskResult` contains the latest memory snapshot needed by the frontend after stream completion.
- `Conversation` persistence stores the latest memory snapshot and latest compression snapshot.
- `GET /conversations/:id` and `GET /conversations` expose those fields so reload can use them.

**Step 2: Run the focused tests to confirm they fail**

Run:

```bash
go test ./core/agent -run 'TestAgentExecutorPersistsLatestMemoryContextAndCompressionIntoConversationAndResult|TestConversationStorePersistsMemorySnapshotFields' -v
go test ./app/handlers -run '^TestConversationHandlerGetConversationIncludesLatestMemorySnapshot$' -v
```

Expected: FAIL because the executor/result/conversation contract does not expose those fields yet.

**Step 3: Implement the minimal persistence/API changes**

- Add conversation persistence fields for the latest authoritative memory snapshot and latest compression snapshot in `core/agent/conversation_store.go`.
- Register the corresponding schema migration in `app/migration/define.go` and `app/migration/init.go`.
- Extend `RunTaskResult` in `core/agent/executor.go` with the same structured fields.
- After runner completion, persist the latest memory snapshot onto the conversation using the backend manager state, and persist the latest compression snapshot from the runner/request path.
- On both success and failure paths, keep the persisted conversation snapshot aligned with the final stored memory state whenever a memory manager exists.
- Update `app/handlers/swagger_types.go` so the HTTP contract documents the new response shape.

Prefer explicit, typed JSON fields over asking the frontend to parse opaque strings.

**Step 4: Re-run the focused tests**

Run:

```bash
go test ./core/agent -run 'TestAgentExecutorPersistsLatestMemoryContextAndCompressionIntoConversationAndResult|TestConversationStorePersistsMemorySnapshotFields' -v
go test ./app/handlers -run '^TestConversationHandlerGetConversationIncludesLatestMemorySnapshot$' -v
```

Expected: PASS.

**Step 5: Run the package-level regression tests**

Run:

```bash
go test ./core/agent ./app/handlers ./app/migration
```

Expected: PASS.

**Step 6: Do not commit**

Do not create a commit in this session unless the user explicitly asks.

---

### Task 3: Wire the frontend API and transcript pipeline to the backend contract

**Files:**
- Modify: `webapp/src/types/api.ts`
- Modify: `webapp/src/lib/api.ts`
- Modify: `webapp/src/lib/transcript.ts`
- Test: `webapp/src/lib/api.spec.ts`
- Test: `webapp/src/lib/transcript.spec.ts`

**Step 1: Write the failing tests**

Add frontend unit tests that force the data boundary to become authoritative.

- In `webapp/src/lib/api.spec.ts`, add a test like `it('subscribes to memory.context_state stream events')`.
- In `webapp/src/lib/transcript.spec.ts`, add tests like:
  - `it('stores backend memory.context_state payloads on memory entries')`
  - `it('stores structured memory.compressed payloads without relying on content parsing')`

Require the tests to verify:

- `streamRunTask(...)` forwards `memory.context_state` events.
- transcript memory entries retain structured backend fields for both context snapshot and compression snapshot.

**Step 2: Run the focused tests to confirm they fail**

Run:

```bash
pnpm --dir webapp exec vitest run src/lib/api.spec.ts -t "subscribes to memory.context_state stream events"
pnpm --dir webapp exec vitest run src/lib/transcript.spec.ts -t "stores backend memory.context_state payloads on memory entries"
```

Expected: FAIL because the stream client does not subscribe to `memory.context_state` and transcript state is still partially string-based.

**Step 3: Implement the minimal frontend boundary changes**

- Extend `webapp/src/types/api.ts` with structured interfaces for:
  - conversation/task-result memory snapshot
  - stream memory context payload
  - stream memory compression payload
- Update `webapp/src/lib/api.ts` normalization so:
  - conversations and task results keep the memory snapshot fields
  - `streamRunTask(...)` subscribes to `memory.context_state`
- Update `webapp/src/lib/transcript.ts` so memory entries preserve structured payload data instead of depending on display strings for later calculations.

Keep reply token usage handling intact, but separate it clearly from memory-context state.

**Step 4: Re-run the focused tests**

Run:

```bash
pnpm --dir webapp exec vitest run src/lib/api.spec.ts -t "subscribes to memory.context_state stream events"
pnpm --dir webapp exec vitest run src/lib/transcript.spec.ts -t "stores backend memory.context_state payloads on memory entries"
```

Expected: PASS.

**Step 5: Run the file-level regression tests**

Run:

```bash
pnpm --dir webapp exec vitest run src/lib/api.spec.ts src/lib/transcript.spec.ts
```

Expected: PASS.

**Step 6: Do not commit**

Do not create a commit in this session unless the user explicitly asks.

---

### Task 4: Make `ChatView` render only authoritative backend memory values

**Files:**
- Modify: `webapp/src/views/ChatView.vue`
- Test: `webapp/src/views/ChatView.spec.ts`

**Step 1: Write the failing tests**

Add or update `ChatView.spec.ts` to prove that the UI stops inferring context usage from history-derived heuristics.

Add tests like:

- `it('shows context usage from backend memory snapshot instead of reply prompt tokens')`
- `it('shows persisted conversation memory snapshot after reload without parsing memory text')`

Require the tests to verify:

- The ring/panel use backend `memory_context.total_tokens` (or the agreed authoritative total field), not `reply.token_usage.prompt_tokens`.
- Compression details are read from structured backend payloads, not regex-parsed UI text.
- If no backend snapshot exists, the panel shows an unknown/empty state instead of inventing a value from history.

**Step 2: Run the focused tests to confirm they fail**

Run:

```bash
pnpm --dir webapp exec vitest run src/views/ChatView.spec.ts -t "shows context usage from backend memory snapshot instead of reply prompt tokens"
pnpm --dir webapp exec vitest run src/views/ChatView.spec.ts -t "shows persisted conversation memory snapshot after reload without parsing memory text"
```

Expected: FAIL because `ChatView.vue` still falls back to parsed compression text and reply prompt usage.

**Step 3: Implement the minimal view changes**

- Update `ChatView.vue` so `currentContextUsage` prefers:
  - latest streamed memory snapshot from transcript
  - otherwise the active conversation's persisted memory snapshot from the backend
- Remove context-usage fallbacks that derive the primary displayed value from:
  - parsed `memory.compressed` strings
  - `reply.token_usage.prompt_tokens`
- Keep model usage (`prompt_tokens`, `completion_tokens`, `cached_prompt_tokens`) visible as reply usage, but do not reuse it as a proxy for memory usage.
- Render compression stats from structured backend numeric fields only.

**Step 4: Re-run the focused tests**

Run:

```bash
pnpm --dir webapp exec vitest run src/views/ChatView.spec.ts -t "shows context usage from backend memory snapshot instead of reply prompt tokens"
pnpm --dir webapp exec vitest run src/views/ChatView.spec.ts -t "shows persisted conversation memory snapshot after reload without parsing memory text"
```

Expected: PASS.

**Step 5: Run the file-level regression tests**

Run:

```bash
pnpm --dir webapp exec vitest run src/views/ChatView.spec.ts
pnpm --dir webapp exec vue-tsc -b
```

Expected: PASS.

**Step 6: Do not commit**

Do not create a commit in this session unless the user explicitly asks.

---

### Task 5: Verify end-to-end behavior and document known baseline noise

**Files:**
- Modify if needed: any touched file from Tasks 1-4
- No new source files expected

**Step 1: Run the focused backend and frontend suites together**

Run:

```bash
go test ./core/agent ./app/handlers ./app/migration
pnpm --dir webapp exec vitest run src/lib/api.spec.ts src/lib/transcript.spec.ts src/views/ChatView.spec.ts
pnpm --dir webapp exec vue-tsc -b
go build ./cmd/...
go list ./...
```

Expected: PASS.

**Step 2: Run the full Go test suite and record any unchanged baseline failures**

Run:

```bash
go test ./...
```

Expected: ideally PASS. If the pre-existing baseline failure in `core/providers/client/openai_responses` still reproduces unchanged, record it explicitly as unrelated baseline noise rather than treating it as part of this fix.

**Step 3: Review the changed UX manually through the tests' asserted behavior**

Confirm the final behavior matches the intended contract:

- live stream context values come from backend memory events
- completed task state comes from backend task result/conversation snapshot
- reload state comes from persisted backend conversation snapshot
- reply usage remains visible but is no longer reused as context usage

**Step 4: Do not commit**

Do not create a commit in this session unless the user explicitly asks.
