# Tool Approval Mode Design

**Goal:** Add a reusable tool approval mode that intercepts dangerous tool executions, pauses the running `agent.run` task for human approval, and resumes execution from a durable checkpoint after the user approves or rejects the request.

**Architecture:** Put approval at the runtime boundary between model-emitted tool calls and actual tool execution. Tool definitions declare approval metadata and can perform argument-aware risk evaluation. The `core/agent` runner creates approval records and checkpoints before executing guarded tool calls, then suspends the task through `core/tasks`. Approval decisions are exposed through task-scoped APIs and stream events so both the chat UI and an admin/backoffice UI can drive the same approval flow.

**Tech Stack:** Go 1.25, Gin, GORM/SQLite, existing `core/agent`, `core/tasks`, `core/tools`, Vue 3 webapp, existing task SSE transport

---

## Scope And Guardrails

- In scope:
  - reusable approval metadata on tool definitions
  - argument-aware approval checks for dangerous operations
  - durable approval records and task resume checkpoints
  - `task.waiting` plus dedicated approval SSE events
  - chat UI approval card and separate approval management view
  - approval-aware executor resume behavior for `agent.run`
- Out of scope:
  - generic policy engine or role-based approval routing beyond current auth model
  - batch approvals across multiple tasks in the first cut
  - provider-specific approval semantics
  - automatic command classification driven by an LLM

## Current Code Constraints

- Tool execution is centralized in `core/agent/stream.go`; the runner currently executes each `assistant.ToolCalls` item immediately through `registry.Execute(...)`.
- `core/tasks` already supports `waiting` state, `Suspend(...)`, `ResumeWaitingTask(...)`, `task.waiting`, and `task.resumed`.
- The webapp transcript currently understands `tool.started`, `tool.finished`, and terminal task events, but not approval-specific events.
- `delete_file` and `kill_process` currently enforce a placeholder `confirm=true` argument instead of a real approval workflow.

This means approval should not be implemented inside individual builtin handlers. The correct interception point is the runner path before `registry.Execute(...)`.

## Desired User Experience

1. The model emits a tool call.
2. The runner evaluates the tool definition and tool arguments.
3. If approval is not required, execution continues unchanged.
4. If approval is required, the backend creates a pending approval record, persists a resume checkpoint, emits `approval.requested`, and suspends the task with `suspend_reason=waiting_for_tool_approval`.
5. The chat UI and approval management UI both show the pending approval with the same payload.
6. A user approves or rejects the request.
7. The backend records the decision, resumes the waiting task, and the executor continues from the suspended tool call.
8. If approved, the real tool executes. If rejected or expired, the executor injects a synthetic tool result back to the model so the run can continue safely.

## Proposed Backend Design

### 1. Tool Definition Extensions

Extend the tool definition model so approval is declared once and enforced centrally.

Add approval-related fields to the tool definition layer in `core/types` and `core/tools`:

```go
type ToolApprovalMode string

const (
	ToolApprovalNever ToolApprovalMode = "never"
	ToolApprovalAlways ToolApprovalMode = "always"
	ToolApprovalConditional ToolApprovalMode = "conditional"
)

type ToolApprovalRule struct {
	Mode        ToolApprovalMode `json:"mode"`
	RiskLevel   string           `json:"risk_level,omitempty"`
	Reason      string           `json:"reason,omitempty"`
	Summary     string           `json:"summary,omitempty"`
}
```

At the registry/runtime layer, support optional tool-specific evaluators:

```go
type ApprovalEvaluator func(ctx context.Context, arguments map[string]any) (ToolApprovalRule, error)
type ArgumentSummarizer func(arguments map[string]any) string
```

Rules:

- builtin and future MCP-backed tools can both declare approval behavior through the same shape
- the evaluator runs on the server only; the frontend never decides whether approval is required
- the evaluator returns a normalized `risk_level`, human-readable reason, and compact argument summary for UI display

Initial policy:

- `delete_file`: always approval
- `kill_process`: always approval
- `exec_command`: conditional approval based on parsed command content
- all other tools: never approval unless explicitly configured later

### 2. Approval Domain Model

Add a dedicated approval store instead of overloading task metadata.

Create a new persisted model, for example in `core/approvals` or `core/tools/approval`:

```go
type ToolApproval struct {
	ID               string          `gorm:"type:varchar(64);primaryKey"`
	TaskID           string          `gorm:"type:varchar(64);not null;index"`
	ConversationID   string          `gorm:"type:varchar(64);index"`
	StepIndex        int             `gorm:"not null"`
	ToolCallID       string          `gorm:"type:varchar(128);not null;index"`
	ToolName         string          `gorm:"type:varchar(128);not null;index"`
	ToolArgumentsJSON json.RawMessage `gorm:"column:tool_arguments_json;type:blob;not null"`
	ArgumentsSummary string          `gorm:"type:text"`
	RiskLevel        string          `gorm:"type:varchar(32)"`
	Status           string          `gorm:"type:varchar(32);not null;index"`
	DecisionBy       string          `gorm:"type:varchar(128)"`
	DecisionReason   string          `gorm:"type:text"`
	DecisionAt       *time.Time
	ExpiresAt        *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}
```

Suggested statuses:

- `pending`
- `approved`
- `rejected`
- `expired`
- `cancelled`

Task metadata should still carry a small resume pointer, for example:

```json
{
  "resume_mode": "tool_approval",
  "approval_id": "approval_123",
  "checkpoint": {
    "step": 2,
    "tool_call_index": 1,
    "assistant_message": { ... },
    "produced_messages_before_checkpoint": [ ... ]
  }
}
```

The approval table owns workflow state. Task metadata owns only the executor resume hint.

### 3. Runner Interception Point

Modify `core/agent/stream.go` where tool calls are processed.

Current behavior:

1. emit `tool.started`
2. decode arguments
3. execute tool
4. emit `tool.finished`
5. append `tool` message

New behavior:

1. decode arguments
2. evaluate approval requirement from tool metadata + evaluator
3. if approval not required, keep current behavior
4. if approval required:
   - create approval record
   - persist checkpoint metadata on the task
   - emit `approval.requested`
   - suspend the task with `waiting_for_tool_approval`
   - return `coretasks.ErrTaskSuspended`

Important detail:

- do not emit `tool.started` before approval is granted
- approval is a gate before the tool is considered running
- the checkpoint must record the full assistant message for the current step and the tool call index within that message

### 4. Executor Resume Semantics

Update the `agent.run` executor in `core/agent/executor.go` so task startup first checks whether the task is resuming from `resume_mode=tool_approval`.

Resume algorithm:

1. load task metadata
2. if there is no approval checkpoint, use current execution path
3. if there is an approval checkpoint:
   - load the referenced approval record
   - reconstruct the assistant message and prior produced conversation from the checkpoint
   - continue the tool loop from `tool_call_index`

Decision handling:

- `approved`: execute the real tool, append real `tool` message, continue same step
- `rejected`: do not execute the real tool; append a synthetic `tool` message describing the rejection, then continue same step
- `expired`: same as rejected but with `approval expired` reason

Rejected or expired approvals should not fail the task by default. They should act like a controlled tool response so the model can explain, revise, or choose a safer alternative.

## Approval API

Add task-scoped approval APIs in `app/handlers`.

### Endpoints

- `GET /api/v1/tasks/:id/approvals`
  - returns approvals for the task, newest first or grouped by current pending item
- `POST /api/v1/tasks/:id/approvals/:approval_id/decision`
  - request body: `{ "decision": "approve" | "reject", "reason": "..." }`
  - validates task ownership and approval ownership
  - persists decision and resumes the waiting task when applicable

Optional backoffice endpoint:

- `GET /api/v1/approvals/pending?conversation_id=...`

Decision API rules:

- approvals are idempotent; repeated decisions on terminal approvals return the current record
- approval of a non-pending approval must not resume the task twice
- task cancellation while waiting marks the pending approval as `cancelled`

## Task Events And SSE

Keep existing task events and add explicit approval events so the frontend does not infer approval state from `waiting` alone.

Add new task event types:

- `approval.requested`
- `approval.resolved`

Payload for `approval.requested` should include:

- `approval_id`
- `task_id`
- `conversation_id`
- `step`
- `tool_call_id`
- `tool_name`
- `arguments_summary`
- `risk_level`
- `reason`
- `status`

Payload for `approval.resolved` should include:

- `approval_id`
- `decision`
- `decision_reason`
- `decision_by`
- `status`

Keep using existing task state transitions:

- task enters `waiting`
- `suspend_reason=waiting_for_tool_approval`
- task returns to `queued` via `task.resumed`

The frontend should use approval events for presentation and `task.waiting` only for coarse task state.

## Frontend Design

### 1. Shared API Types

Extend `webapp/src/types/api.ts` with:

- `ToolApproval`
- `ApprovalDecisionRequest`
- approval-related `TaskStreamEvent` payload variants

Extend `webapp/src/lib/api.ts` with:

- `fetchTaskApprovals(taskId)`
- `submitApprovalDecision(taskId, approvalId, input)`

### 2. Chat UI

Update transcript handling in `webapp/src/lib/transcript.ts` so it can render approval-related entries.

Expected behavior:

- when `approval.requested` arrives, render a pending approval card in the transcript
- when `approval.resolved` arrives, update the approval card to approved or rejected
- when the task is `waiting_for_tool_approval`, display task status as waiting instead of failed

Approval card content:

- tool name
- risk level
- human-readable reason
- argument summary
- optional reason input box
- `Allow` and `Reject` buttons

The card belongs in the chat transcript because approval is part of the execution trace, not a separate admin-only concept.

### 3. Separate Approval View

Add a separate approval management surface that consumes the same API and event types.

This can be an admin-style list or panel showing:

- pending approvals
- task id / conversation id
- tool name
- risk level
- requester
- created time
- action buttons

The first cut can be task-local. A global queue can be added later if needed.

## Builtin Tool Changes

Replace the current placeholder confirmation behavior.

### `delete_file`

- remove required `confirm=true` parameter from public contract
- mark tool as `always` approval
- expose path summary for UI display

### `kill_process`

- remove required `confirm=true` parameter
- mark tool as `always` approval
- expose pid/name summary for UI display

### `exec_command`

Add conditional approval with argument-aware classification.

First-cut dangerous command heuristics may include:

- deletion: `rm`, `del`, `rmdir`, `Remove-Item`
- process termination: `kill`, `taskkill`, `Stop-Process`
- permission/system mutation: `chmod`, `chown`, registry mutation, service control, firewall mutation
- destructive package or filesystem commands

These heuristics should live in server-side evaluator code near the builtin tool, not in the frontend.

## File And Package Impact

Expected touched areas:

- Create: approval store package and tests
- Modify: `core/types/tool.go`
- Modify: `core/tools/register.go`
- Modify: `core/tools/builtin/delete_file.go`
- Modify: `core/tools/builtin/kill_process.go`
- Modify: `core/tools/builtin/exec_command.go`
- Modify: `core/agent/stream.go`
- Modify: `core/agent/executor.go`
- Modify: `core/agent/task_adapter.go`
- Modify: `core/tasks/types.go`
- Modify: `app/handlers/task_handler.go`
- Create: approval-specific handler(s) and tests
- Modify: `app/router/init.go`
- Modify: `app/migration/define.go`
- Modify: `app/migration/init.go`
- Modify: `app/migration/task_migration_test.go`
- Modify: `webapp/src/types/api.ts`
- Modify: `webapp/src/lib/api.ts`
- Modify: `webapp/src/lib/transcript.ts`
- Modify: chat view / message list components
- Create: approval management page or component set

## State Machine Summary

### Approval state

- `pending` -> `approved`
- `pending` -> `rejected`
- `pending` -> `expired`
- `pending` -> `cancelled`

### Task state around approval

- `running` -> `waiting` when approval is requested
- `waiting` -> `queued` when approval is resolved and resume is requested
- `queued` -> `running` when a worker reclaims the task
- `waiting` -> `cancelled` if the task is cancelled before approval resolves

### Tool execution semantics

- no approval required: execute immediately
- approved: execute real tool on resume
- rejected: inject synthetic tool message and continue
- expired: inject synthetic tool message and continue

## Testing Strategy

### Backend

Add focused tests for:

- tool approval evaluator behavior for `delete_file`, `kill_process`, and `exec_command`
- approval record creation and persistence
- `task.waiting` plus `approval.requested` event emission
- decision API idempotency and ownership checks
- task resume from approval checkpoint
- rejected approval producing synthetic tool output instead of terminal task failure
- cancelled waiting task marking pending approval as `cancelled`

Suggested commands:

- `go test ./core/tools/builtin -run "Test(DeleteFile|KillProcess|ExecCommand)"`
- `go test ./core/agent -run "TestAgentExecutor|TestRunner"`
- `go test ./core/tasks -run "TestStore|TestManager"`
- `go test ./app/handlers -run "TestTask|TestApproval"`

### Frontend

Add tests for:

- approval event normalization
- transcript rendering of pending and resolved approvals
- submit-approval action wiring
- waiting task display does not show a failure state

Suggested commands:

- `pnpm exec vitest run src/lib/api.spec.ts`
- `pnpm exec vitest run src/lib/transcript.spec.ts`
- `pnpm exec vitest run src/views/*.spec.ts`
- `pnpm exec vue-tsc -b`

## Implementation Milestones

### Milestone 1: Backend approval primitives

- add approval model, store, migration, and decision API
- add approval event types
- no UI required yet

### Milestone 2: Runner checkpoint + resume

- intercept approval before tool execution
- persist checkpoints
- resume task after decision

### Milestone 3: Builtin migration

- convert `delete_file` and `kill_process`
- add `exec_command` conditional evaluator

### Milestone 4: Chat UI approval card

- render pending approvals inline
- allow approve/reject directly in chat

### Milestone 5: Separate approval management UI

- add task-scoped or global approval review view

## Key Design Decisions

- approval is enforced before tool execution, not by passing `confirm=true`
- approval state is durable and queryable through a first-class record
- task metadata stores resume hints only; workflow state lives in approval records
- rejection is modeled as synthetic tool output so the run can continue coherently
- the backend decides whether approval is required; the frontend only renders and submits decisions

## Open Questions For Implementation

- whether approval records should live in a new top-level `core/approvals` package or under `core/tools`
- whether the first UI cut should include a global pending approval queue or only task-scoped views
- whether approval expiry should be time-based in v1 or manual only
- how rich the first `exec_command` heuristic needs to be on Windows vs Unix command styles
