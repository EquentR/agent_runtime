# Tool Approval Mode Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task, with a fresh implementer subagent per task and two review passes after each completed task: spec compliance first, then code quality.

**Goal:** Implement a reusable tool approval mode that pauses dangerous tool executions for human approval, then resumes `agent.run` from a durable checkpoint.

**Architecture:** Build approval as a first-class backend capability spanning tool metadata, persisted approval records, task SSE events, and agent resume checkpoints. Keep approval decisions server-authoritative, with the chat transcript and a separate approval view consuming the same API and event payloads.

**Tech Stack:** Go 1.25, Gin, GORM/SQLite, `core/agent`, `core/tasks`, `core/tools`, Vue 3, Vitest, existing task SSE transport

---

### Task 1: Add backend approval model, store, events, and task-scoped APIs

**Files:**
- Create: `core/approvals/store.go`
- Create: `core/approvals/store_test.go`
- Create: `app/handlers/approval_handler.go`
- Create: `app/handlers/approval_handler_test.go`
- Modify: `core/tasks/types.go`
- Modify: `core/tasks/manager.go`
- Modify: `core/tasks/manager_test.go`
- Modify: `core/tasks/store.go`
- Modify: `app/router/init.go`
- Modify: `app/router/deps.go`
- Modify: `app/commands/serve.go`
- Modify: `app/migration/define.go`
- Modify: `app/migration/init.go`
- Modify: `app/migration/task_migration_test.go`
- Modify: `app/handlers/swagger_types.go`

**Step 1: Write the failing tests**

Add tests that assert:
- approval records can be created, queried by task, and resolved exactly once
- new task event constants include `approval.requested` and `approval.resolved`
- task-scoped approval APIs enforce ownership and return approval records
- resolving a pending approval on a waiting task resumes the task exactly once
- cancelling a waiting task marks its pending approvals as `cancelled`

**Step 2: Run tests to verify they fail**

Run: `go test ./core/approvals ./app/handlers ./app/migration -run "Test(Approval|TaskMigration)"`
Expected: FAIL because approval persistence and handler wiring do not exist yet.

**Step 3: Write minimal implementation**

Implement:
- a persisted `ToolApproval` model with statuses `pending`, `approved`, `rejected`, `expired`, `cancelled`
- store methods such as `CreateApproval`, `GetApproval`, `ListTaskApprovals`, `ResolveApproval`, and `CancelPendingApprovalsByTask`
- task event constants for approval request and approval resolution
- manager-side cancellation wiring so `waiting_for_tool_approval` tasks cancel any still-pending approvals before entering terminal task state
- task-manager resume handling that treats approval decisions as single-resume operations and does not requeue the same task twice
- task-scoped REST endpoints:
  - `GET /api/v1/tasks/:id/approvals`
  - `POST /api/v1/tasks/:id/approvals/:approvalID/decision`
- app wiring so handlers can access the approval store and task manager
- keep expiry manual in v1: persist `ExpiresAt` on the model if provided later, but do not add background expiry processing in this implementation
- exact shared contract for v1:
  - approval list response returns records with `id`, `task_id`, `conversation_id`, `step_index`, `tool_call_id`, `tool_name`, `arguments_summary`, `risk_level`, `status`, `decision_by`, `decision_reason`, `decision_at`, `created_at`, `updated_at`
  - decision request body is `{ "decision": "approve" | "reject", "reason": string }`
  - `approval.requested` event payload includes `approval_id`, `task_id`, `conversation_id`, `step`, `tool_call_id`, `tool_name`, `arguments_summary`, `risk_level`, `reason`, `status`
  - `approval.resolved` event payload includes `approval_id`, `task_id`, `decision`, `decision_reason`, `decision_by`, `status`

**Step 4: Run tests to verify they pass**

Run: `go test ./core/approvals ./app/handlers ./app/migration -run "Test(Approval|TaskMigration)"`
Expected: PASS.

### Task 2: Extend tool definitions and builtin approval policies

**Files:**
- Modify: `core/types/tool.go`
- Modify: `core/tools/register.go`
- Create: `core/tools/approval.go`
- Modify: `core/tools/register_test.go`
- Modify: `core/tools/builtin/delete_file.go`
- Modify: `core/tools/builtin/kill_process.go`
- Modify: `core/tools/builtin/exec_command.go`
- Modify: `core/tools/builtin/register_test.go`

**Step 1: Write the failing tests**

Add tests that assert:
- tools can declare approval mode and optional argument-aware evaluator
- `delete_file` and `kill_process` require approval without needing `confirm=true`
- `exec_command` only requires approval for dangerous commands and returns a useful risk summary

**Step 2: Run tests to verify they fail**

Run: `go test ./core/tools ./core/tools/builtin -run "Test(Register|DeleteFile|KillProcess|ExecCommand)"`
Expected: FAIL because tool approval metadata and evaluators do not exist yet.

**Step 3: Write minimal implementation**

Implement:
- approval-related fields and helper types on tool definitions
- registry support for retrieving approval policy for a tool name
- builtin conversion from `confirm=true` placeholder checks to declarative approval rules
- first-cut `exec_command` dangerous command detection for deletion, process-kill, and obvious system mutation operations; broader shell policy remains future work

**Step 4: Run tests to verify they pass**

Run: `go test ./core/tools ./core/tools/builtin -run "Test(Register|DeleteFile|KillProcess|ExecCommand)"`
Expected: PASS.

### Task 3: Add runner approval interception, checkpoint persistence, and resume behavior

**Files:**
- Modify: `core/agent/stream.go`
- Modify: `core/agent/executor.go`
- Modify: `core/agent/executor_test.go`
- Modify: `core/agent/runner_test.go`
- Modify: `core/agent/task_adapter.go`
- Modify: `core/tasks/runtime.go`

**Step 1: Write the failing tests**

Add tests that assert:
- a guarded tool call emits `approval.requested`, suspends the task, and does not emit `tool.started`
- approving a pending tool resumes execution from the saved tool call index
- rejecting or expiring a pending approval injects a synthetic `tool` message and keeps the run alive instead of failing the task

**Step 2: Run tests to verify they fail**

Run: `go test ./core/agent ./core/tasks -run "Test(Runner|AgentExecutor|Manager).*Approval"`
Expected: FAIL because the runner does not currently suspend on tool approval or resume from checkpoints.

**Step 3: Write minimal implementation**

Implement:
- runner-side approval check before `registry.Execute(...)`
- checkpoint persistence on the task metadata containing `approval_id`, `step`, `assistant_message`, `tool_call_index`, and `produced_messages_before_checkpoint`
- task suspension with `waiting_for_tool_approval`
- executor resume path that reconstructs the assistant tool-call message and continues execution from the checkpoint
- synthetic tool output for rejected or expired approvals
- decision handling that emits `approval.resolved` exactly once and only resumes the task once for a pending approval

**Step 4: Run tests to verify they pass**

Run: `go test ./core/agent ./core/tasks -run "Test(Runner|AgentExecutor|Manager).*Approval"`
Expected: PASS.

### Task 4: Add shared frontend approval types, API helpers, and chat transcript approval card

**Files:**
- Modify: `webapp/src/types/api.ts`
- Modify: `webapp/src/lib/api.ts`
- Modify: `webapp/src/lib/transcript.ts`
- Modify: `webapp/src/components/MessageList.vue`
- Modify: `webapp/src/views/ChatView.vue`
- Create: `webapp/src/lib/transcript.spec.ts`
- Modify: existing relevant frontend specs

**Step 1: Write the failing tests**

Add tests that assert:
- approval payloads normalize correctly from REST and SSE
- pending approvals render as transcript entries instead of failure entries
- resolving an approval updates the transcript state
- chat view can submit approve/reject actions through shared API helpers

**Step 2: Run tests to verify they fail**

Run: `pnpm exec vitest run src/lib/api.spec.ts src/lib/transcript.spec.ts`
Expected: FAIL because approval DTOs and transcript rendering do not exist yet.

**Step 3: Write minimal implementation**

Implement:
- shared approval DTOs and API methods
- transcript event handling for `approval.requested` and `approval.resolved`
- inline approval card in the chat transcript with allow/reject actions and optional reason field
- waiting-state rendering that does not treat approval pauses as task failures

**Step 4: Run tests to verify they pass**

Run: `pnpm exec vitest run src/lib/api.spec.ts src/lib/transcript.spec.ts`
Expected: PASS.

### Task 5: Add separate approval management view and run broad verification

**Files:**
- Create: `webapp/src/views/ApprovalView.vue`
- Modify: `webapp/src/router/index.ts`
- Create: `webapp/src/views/ApprovalView.spec.ts`
- Modify: any shared approval components introduced in Task 4

**Step 1: Write the failing tests**

Add tests that assert:
- the separate approval view lists approvals for a selected task using the shared task-scoped API
- approve/reject actions update the list and preserve auth/routing expectations

**Step 2: Run tests to verify they fail**

Run: `pnpm exec vitest run src/views/ApprovalView.spec.ts`
Expected: FAIL because the view and route do not exist yet.

**Step 3: Write minimal implementation**

Implement:
- a separate approval management view using the same DTOs, decision API, and approval SSE event payloads as chat, scoped to a selected task in v1
- router wiring for the approval page
- lightweight shared presentation for pending approval records

**Step 4: Run focused and broad verification**

Run:
- `go test ./core/approvals ./core/tools ./core/tools/builtin ./core/agent ./core/tasks ./app/handlers ./app/migration`
- `pnpm exec vitest run src/lib/api.spec.ts src/lib/transcript.spec.ts src/views/ApprovalView.spec.ts`
- `pnpm exec vue-tsc -b`
- `go test ./...`

Expected: PASS.
