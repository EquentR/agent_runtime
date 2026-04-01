# Prompt Management DB/Admin UI Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a database-backed prompt management system with admin-only web UI, scene-based default prompt injection, runtime `AGENTS.md` workspace injection, and audit-only prompt visibility.

**Architecture:** Add a new prompt domain that stores prompt documents and scene bindings in SQLite, resolves prompts at `agent.run` execution time, and injects them into the loop by phase (`session`, `step_pre_model`, `tool_result`). Keep prompt content out of normal conversation history and chat UI; only expose resolved prompt details through audit artifacts and admin-only prompt management endpoints.

**Tech Stack:** Go, GORM, Gin, Vue 3, Vue Router, Vitest, SQLite, existing audit/task/conversation infrastructure.

---

### Task 1: Define prompt persistence models and migration

**Files:**
- Create: `core/prompt/model.go`
- Modify: `app/migration/define.go`
- Test: `app/migration/task_migration_test.go`

**Step 1: Write the failing migration test**

Add a test in `app/migration/task_migration_test.go` that boots migrations and asserts the new tables exist:
- `prompt_documents`
- `prompt_bindings`

Also assert key columns exist for:
- documents: `id`, `name`, `content`, `status`, `created_by`, `updated_by`
- bindings: `id`, `prompt_id`, `scene`, `phase`, `is_default`, `priority`, `provider_id`, `model_id`, `status`

**Step 2: Run test to verify it fails**

Run: `go test ./app/migration -run Prompt -v`
Expected: FAIL because the prompt tables are not registered yet.

**Step 3: Add the prompt GORM models**

Create `core/prompt/model.go` with two models:
- `PromptDocument`
- `PromptBinding`

Requirements:
- ASCII-only content in code comments and identifiers
- stable `TableName()` methods
- timestamps on both tables
- explicit status fields so documents/bindings can be disabled independently
- `phase` constrained by convention to `session`, `step_pre_model`, `tool_result`
- `scope` or `source_kind` on documents to distinguish built-in/admin-authored prompt content from future expansions

Suggested fields:
- `PromptDocument`: `ID`, `Name`, `Description`, `Content`, `Scope`, `Status`, `CreatedBy`, `UpdatedBy`, `CreatedAt`, `UpdatedAt`
- `PromptBinding`: `ID`, `PromptID`, `Scene`, `Phase`, `IsDefault`, `Priority`, `ProviderID`, `ModelID`, `Status`, `CreatedBy`, `UpdatedBy`, `CreatedAt`, `UpdatedAt`

**Step 4: Register the migration**

Modify `app/migration/define.go` to add a new migration after the current latest migration.

Requirements:
- keep existing migration order intact
- only append a new migration
- migrate `core/prompt` tables there

**Step 5: Run test to verify it passes**

Run: `go test ./app/migration -run Prompt -v`
Expected: PASS.

**Step 6: Commit**

```bash
git add app/migration/define.go app/migration/task_migration_test.go core/prompt/model.go
git commit -m "feat: add prompt persistence tables"
```

### Task 2: Build prompt store CRUD and default binding queries

**Files:**
- Create: `core/prompt/store.go`
- Create: `core/prompt/store_test.go`

**Step 1: Write failing store tests**

Create `core/prompt/store_test.go` with tests for:
- `AutoMigrate()` creates prompt tables
- creating and reading prompt documents
- updating prompt documents
- listing bindings filtered by scene and phase
- default binding query returns multiple active defaults for the same scene+phase ordered by `priority asc`, then creation order
- disabled document is excluded from resolution query
- disabled binding is excluded from resolution query

**Step 2: Run tests to verify they fail**

Run: `go test ./core/prompt -run Store -v`
Expected: FAIL because store implementation does not exist.

**Step 3: Implement the store**

Create `core/prompt/store.go` with:
- `Store`
- `NewStore(db *gorm.DB)`
- `AutoMigrate()`
- document CRUD used by admin APIs
- binding CRUD used by admin APIs
- list/query helpers used by resolver

Add typed filter inputs instead of passing raw maps.

Requirements:
- no prompt resolution logic here
- trim user-provided string filters
- return stable ordering
- keep provider/model filters optional for V1

**Step 4: Run tests to verify they pass**

Run: `go test ./core/prompt -run Store -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add core/prompt/store.go core/prompt/store_test.go
git commit -m "feat: add prompt store and binding queries"
```

### Task 3: Add runtime workspace prompt loader for `AGENTS.md`

**Files:**
- Create: `core/prompt/workspace_loader.go`
- Create: `core/prompt/workspace_loader_test.go`

**Step 1: Write failing loader tests**

Create tests for:
- loading `AGENTS.md` from workspace root
- returning no prompts when file is missing
- trimming empty/whitespace-only file content
- enforcing a max size cap so very large files do not flood prompts

**Step 2: Run tests to verify they fail**

Run: `go test ./core/prompt -run Workspace -v`
Expected: FAIL because the loader does not exist.

**Step 3: Implement the loader**

Create `core/prompt/workspace_loader.go` with a loader that:
- only reads `<workspaceRoot>/AGENTS.md`
- returns a runtime-only prompt segment for `session`
- does not persist anything to DB
- silently ignores missing file
- returns metadata describing source kind `workspace_file` and source ref `AGENTS.md`

**Step 4: Run tests to verify they pass**

Run: `go test ./core/prompt -run Workspace -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add core/prompt/workspace_loader.go core/prompt/workspace_loader_test.go
git commit -m "feat: load workspace prompt from agents file"
```

### Task 4: Implement prompt resolver with scene-based multi-default injection

**Files:**
- Create: `core/prompt/resolver.go`
- Create: `core/prompt/resolver_test.go`

**Step 1: Write failing resolver tests**

Create tests for:
- resolving multiple default prompts for the same `scene + phase`
- preserving priority order
- keeping provider/model fields optional for V1
- mapping legacy `system_prompt` into session injection
- appending workspace `AGENTS.md` after DB defaults
- producing separate resolved buckets for `session`, `step_pre_model`, `tool_result`
- returning structured source metadata for audit artifacts

**Step 2: Run tests to verify they fail**

Run: `go test ./core/prompt -run Resolver -v`
Expected: FAIL because resolver implementation does not exist.

**Step 3: Implement resolver types and logic**

Add a resolver that accepts:
- task type
- scene
- provider id
- model id
- legacy `system_prompt`
- workspace root

Output should include:
- resolved prompt segments with source metadata
- `session` messages
- `step_pre_model` messages
- `tool_result` messages

Ordering rules for V1:
1. DB default bindings by `priority asc`, then `created_at asc`
2. legacy `system_prompt`
3. runtime `AGENTS.md`

Requirements:
- same scene+phase can inject more than one default prompt
- empty content must be discarded
- keep model/provider dimensions in the API even if not used in initial UI

**Step 4: Run tests to verify they pass**

Run: `go test ./core/prompt -run Resolver -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add core/prompt/resolver.go core/prompt/resolver_test.go
git commit -m "feat: resolve scene prompt injections"
```

### Task 5: Wire prompt runtime into application startup

**Files:**
- Modify: `app/commands/serve.go`
- Modify: `app/router/deps.go`
- Modify: `app/commands/serve_test.go`

**Step 1: Write failing startup/DI tests**

Extend `app/commands/serve_test.go` to assert:
- prompt store is created and exposed through router dependencies
- `registerAgentRunExecutor(...)` receives prompt resolver dependencies
- startup wiring still exposes audit routes and existing behavior

**Step 2: Run tests to verify they fail**

Run: `go test ./app/commands -run Prompt -v`
Expected: FAIL because prompt runtime is not wired.

**Step 3: Implement startup wiring**

Modify `app/commands/serve.go` to:
- initialize `core/prompt.Store`
- initialize `core/prompt.Resolver`
- pass workspace root to resolver
- pass prompt runtime into `registerAgentRunExecutor(...)`
- expose prompt store/resolver through `router.Dependencies`

Modify `app/router/deps.go` to carry prompt dependencies needed by handlers.

**Step 4: Run tests to verify they pass**

Run: `go test ./app/commands -run Prompt -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add app/commands/serve.go app/commands/serve_test.go app/router/deps.go
git commit -m "feat: wire prompt runtime into server"
```

### Task 6: Extend `agent.run` input and executor prompt resolution

**Files:**
- Modify: `core/agent/executor.go`
- Modify: `core/agent/executor_test.go`

**Step 1: Write failing executor tests**

Add tests covering:
- `scene` in `RunTaskInput` drives resolver selection
- legacy `system_prompt` remains supported
- resolved prompts are passed into the runner
- failures in prompt resolution fail execution before model call

**Step 2: Run tests to verify they fail**

Run: `go test ./core/agent -run Executor.*Prompt -v`
Expected: FAIL because executor does not resolve structured prompts yet.

**Step 3: Implement executor changes**

Modify `core/agent/executor.go`:
- add optional `Scene string` to `RunTaskInput`
- inject prompt resolver into `ExecutorDependencies`
- resolve prompts once per task before runner construction
- default scene to `agent.run.default` when empty
- keep `SystemPrompt` as a compatibility field only

Do not append resolved prompts to conversation storage.

**Step 4: Run tests to verify they pass**

Run: `go test ./core/agent -run Executor.*Prompt -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add core/agent/executor.go core/agent/executor_test.go
git commit -m "feat: resolve prompts in agent executor"
```

### Task 7: Refactor runner prompt injection by phase without polluting chat history

**Files:**
- Modify: `core/agent/types.go`
- Modify: `core/agent/memory.go`
- Modify: `core/agent/memory_test.go`
- Modify: `core/agent/stream_test.go`

**Step 1: Write failing runner/memory tests**

Add tests for:
- session prompts injected into request messages before user/assistant history
- step prompts injected on every model turn
- tool-result prompts injected only when the next turn follows tool output
- prompts are sent to model requests but are not written back to memory as chat-visible content

**Step 2: Run tests to verify they fail**

Run: `go test ./core/agent -run 'Runner.*Prompt|Memory.*Prompt' -v`
Expected: FAIL because runner only supports one raw system prompt.

**Step 3: Implement phase-aware injection**

Modify `core/agent/types.go` so runner options carry structured resolved prompt data instead of only `SystemPrompt string`.

Modify `core/agent/memory.go` to:
- build request messages from conversation+memory as before
- inject `session` prompt messages
- inject `step_pre_model` prompt messages on each round
- inject `tool_result` prompt messages only for tool continuation turns

Constraints:
- prompt messages must only exist in request construction
- prompt messages must not be appended to `ConversationStore`
- prompt messages must not appear in transcript-building chat code paths

**Step 4: Run tests to verify they pass**

Run: `go test ./core/agent -run 'Runner.*Prompt|Memory.*Prompt' -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add core/agent/types.go core/agent/memory.go core/agent/memory_test.go core/agent/stream_test.go
git commit -m "feat: inject prompts by loop phase"
```

### Task 8: Upgrade audit artifacts to expose prompt sources only in audit

**Files:**
- Modify: `core/agent/stream.go`
- Modify: `core/agent/audit.go`
- Modify: `core/agent/stream_test.go`

**Step 1: Write failing audit tests**

Add tests that verify:
- `prompt.resolved` audit event includes scene and prompt counts
- resolved prompt artifact contains source metadata for DB prompt bindings, legacy system prompt, and workspace file prompt
- chat request payload sent to model contains prompts
- conversation persistence remains free of those prompt messages

**Step 2: Run tests to verify they fail**

Run: `go test ./core/agent -run 'Stream.*Prompt|Audit.*Prompt' -v`
Expected: FAIL because resolved prompt artifact is too small.

**Step 3: Implement audit artifact changes**

Modify `core/agent/stream.go` to attach a richer resolved prompt artifact containing:
- scene
- phase buckets
- ordered segments
- source metadata
- final request messages snapshot

Keep prompt visibility audit-only:
- do not route prompt text into normal task stream event payloads used by chat transcript rendering
- only expose them through audit replay artifacts

**Step 4: Run tests to verify they pass**

Run: `go test ./core/agent -run 'Stream.*Prompt|Audit.*Prompt' -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add core/agent/stream.go core/agent/audit.go core/agent/stream_test.go
git commit -m "feat: expose resolved prompts in audit artifacts"
```

### Task 9: Add admin-only prompt management HTTP API

**Files:**
- Create: `app/handlers/prompt_handler.go`
- Create: `app/handlers/prompt_handler_test.go`
- Modify: `app/router/init.go`
- Modify: `app/handlers/swagger_types.go`

**Step 1: Write failing handler tests**

Create tests for:
- admin can list prompt documents
- admin can create and update prompt documents
- admin can list/create/update/delete prompt bindings
- non-admin authenticated user gets unauthorized response
- anonymous request gets unauthorized response

**Step 2: Run tests to verify they fail**

Run: `go test ./app/handlers -run Prompt -v`
Expected: FAIL because prompt handler does not exist.

**Step 3: Implement handler and route registration**

Create `app/handlers/prompt_handler.go` following existing handler conventions.

Routes should be admin-only and read/write prompt definitions and bindings.

Suggested routes:
- `GET /prompts/documents`
- `GET /prompts/documents/:id`
- `POST /prompts/documents`
- `PUT /prompts/documents/:id`
- `GET /prompts/bindings`
- `POST /prompts/bindings`
- `PUT /prompts/bindings/:id`
- `DELETE /prompts/bindings/:id`

Modify `app/router/init.go` to register the handler.

Modify `app/handlers/swagger_types.go` with prompt request/response docs.

**Step 4: Run tests to verify they pass**

Run: `go test ./app/handlers -run Prompt -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add app/handlers/prompt_handler.go app/handlers/prompt_handler_test.go app/router/init.go app/handlers/swagger_types.go
git commit -m "feat: add admin prompt management api"
```

### Task 10: Keep prompts out of conversation read APIs and transcript presentation

**Files:**
- Modify: `core/agent/conversation_store.go`
- Modify: `app/handlers/conversation_handler_test.go`
- Modify: `webapp/src/lib/transcript.ts`
- Modify: `webapp/src/types/api.ts`

**Step 1: Write failing protection tests**

Add/extend tests that prove:
- conversation message APIs only return persisted chat/tool messages
- resolved system prompts from DB/workspace do not appear in conversation history endpoints
- transcript builders do not invent prompt UI entries from audit/task stream events

**Step 2: Run tests to verify they fail**

Run: `go test ./app/handlers -run Conversation.*Prompt -v && pnpm --dir webapp test -- --run transcript`
Expected: FAIL once tests are added.

**Step 3: Implement the protection rules**

Keep conversation persistence logic unchanged except where needed to ensure prompt messages are never appended.

If transcript code paths need guards, add them so system prompt injection is never rendered into chat entries.

Requirements:
- prompts remain available in audit replay only
- chat view remains user/assistant/tool/reasoning focused

**Step 4: Run tests to verify they pass**

Run: `go test ./app/handlers -run Conversation.*Prompt -v && pnpm --dir webapp test -- --run transcript`
Expected: PASS.

**Step 5: Commit**

```bash
git add core/agent/conversation_store.go app/handlers/conversation_handler_test.go webapp/src/lib/transcript.ts webapp/src/types/api.ts
git commit -m "fix: keep prompts out of chat transcript"
```

### Task 11: Build admin prompt management UI

**Files:**
- Create: `webapp/src/views/AdminPromptView.vue`
- Create: `webapp/src/views/AdminPromptView.spec.ts`
- Modify: `webapp/src/router/index.ts`
- Modify: `webapp/src/router/index.spec.ts`
- Modify: `webapp/src/lib/api.ts`
- Modify: `webapp/src/types/api.ts`

**Step 1: Write failing frontend tests**

Add tests for:
- admin route `/admin/prompts` is reachable by admin users
- non-admin users are redirected away
- page loads prompt documents and bindings
- create/edit form calls API helpers
- binding list reflects scene, phase, default flag, and priority

**Step 2: Run tests to verify they fail**

Run: `pnpm --dir webapp test -- --run AdminPromptView router`
Expected: FAIL because the route/view/API types do not exist.

**Step 3: Implement admin UI**

Create `webapp/src/views/AdminPromptView.vue` with an admin-only page modeled after `AdminAuditView.vue` style conventions.

UI requirements:
- document list panel
- document editor form
- binding list/editor for selected document
- scene, phase, default toggle, status, priority controls
- no prompt content rendered in chat pages

Modify `webapp/src/router/index.ts` to register `/admin/prompts` with `requiresAdmin: true`.

Modify `webapp/src/lib/api.ts` and `webapp/src/types/api.ts` to support prompt APIs.

**Step 4: Run tests to verify they pass**

Run: `pnpm --dir webapp test -- --run AdminPromptView router`
Expected: PASS.

**Step 5: Commit**

```bash
git add webapp/src/views/AdminPromptView.vue webapp/src/views/AdminPromptView.spec.ts webapp/src/router/index.ts webapp/src/router/index.spec.ts webapp/src/lib/api.ts webapp/src/types/api.ts
git commit -m "feat: add admin prompt management ui"
```

### Task 12: Regenerate API docs and run full verification

**Files:**
- Modify: `docs/swagger/*`
- Modify if needed: `docs/plans/2026-03-23-prompt-management-db-admin-ui.md`

**Step 1: Regenerate Swagger output**

Run the repository's Swagger generation command used for this project.

If there is no helper script, use the existing project convention/tooling already used in this repository.

**Step 2: Run focused backend tests**

Run:
- `go test ./core/prompt ./core/agent ./app/handlers ./app/commands ./app/migration`

Expected: PASS.

**Step 3: Run frontend tests**

Run:
- `pnpm --dir webapp test`

Expected: PASS.

**Step 4: Run repository verification**

Run:
- `go test ./...`
- `go build ./cmd/...`
- `go list ./...`
- `pnpm --dir webapp build`

Expected: all commands PASS.

**Step 5: Commit**

```bash
git add docs/swagger docs/plans/2026-03-23-prompt-management-db-admin-ui.md
git commit -m "docs: publish prompt management api updates"
```

### Task 13: Manual verification checklist

**Files:**
- Verify only; no new files required.

**Step 1: Verify admin prompt management access**

Manual check:
- login as admin
- open `/admin/prompts`
- create one session prompt document and one tool-result prompt document
- bind both to `agent.run.default`

Expected: documents and bindings save successfully.

**Step 2: Verify normal user cannot access prompt admin**

Manual check:
- login as non-admin user
- navigate to `/admin/prompts`

Expected: redirected to `/chat` by router and denied by backend API.

**Step 3: Verify prompt injection behavior**

Manual check:
- run a chat task in a repo with `AGENTS.md`
- inspect audit replay

Expected:
- audit replay shows DB prompts and workspace prompt in resolved prompt artifact
- normal chat transcript does not show prompt text

**Step 4: Verify multi-default same-scene injection**

Manual check:
- create two default `session` bindings for the same scene with different priorities
- run a task and inspect audit replay

Expected: both prompts appear in resolved prompt artifact in priority order.

**Step 5: Verify tool-result phase only appears after tool turns**

Manual check:
- run one task with a tool call and one without
- inspect audit replay for each

Expected:
- `tool_result` prompt appears only on the tool continuation run.

---

Plan complete and saved to `docs/plans/2026-03-23-prompt-management-db-admin-ui.md`. Two execution options:

**1. Subagent-Driven (this session)** - I dispatch fresh subagent per task, review between tasks, fast iteration

**2. Parallel Session (separate)** - Open new session with executing-plans, batch execution with checkpoints

**Which approach?**
