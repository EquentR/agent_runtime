# Interaction Unification And Question Tool Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a special `ask_user` tool that can pause a running agent for structured human input, while unifying existing approvals into a shared interaction framework with full audit traces.

**Architecture:** Introduce a new `core/interactions` domain that owns request/response persistence, waiting-state recovery, and compatibility adapters for legacy approvals. Generalize the current tool-approval checkpoint/resume flow into an interaction checkpoint so both approval and question prompts suspend through the same runtime path. Add a dedicated audit `interaction` phase plus request/response artifacts so pauses and resumptions are replayable.

**Tech Stack:** Go 1.25, Gin, GORM/SQLite, existing task manager + audit recorder, Vue 3 + TypeScript + Vitest.

---

### Task 1: Add Interaction Persistence And Upgrade Migration

**Files:**
- Create: `core/interactions/store.go`
- Create: `core/interactions/store_test.go`
- Modify: `app/migration/define.go`
- Modify: `app/migration/init.go`
- Modify: `app/migration/task_migration_test.go`
- Modify: `app/commands/serve.go`
- Modify: `app/router/deps.go`

**Step 1: Write the failing storage and migration tests**

```go
func TestStoreCreateInteractionPersistsQuestionRequest(t *testing.T) {
    store := newInteractionStoreForTest(t)
    created, err := store.CreateInteraction(context.Background(), CreateInteractionInput{
        ID:             "interaction_1",
        TaskID:         "task_1",
        ConversationID: "conv_1",
        ToolCallID:     "call_1",
        Kind:           KindQuestion,
        Request: QuestionRequest{
            Prompt:      "Which environment?",
            Options:     []Option{{ID: "staging", Label: "Staging"}},
            AllowCustom: true,
        },
    })
    if err != nil || created.Kind != KindQuestion {
        t.Fatalf("CreateInteraction() = %#v, %v", created, err)
    }
}

func TestTaskMigrationCreatesInteractionsTableAndBackfillsApprovals(t *testing.T) {
    // bootstrap old approval row, run new migration, assert interactions table exists
    // and copied interaction preserves the original approval id
}
```

**Step 2: Run the focused tests and verify they fail**

Run: `go test ./core/interactions ./app/migration -run 'Test(StoreCreateInteractionPersistsQuestionRequest|TaskMigrationCreatesInteractionsTableAndBackfillsApprovals)'`

Expected: FAIL because `core/interactions` does not exist and migration `to011` is missing.

**Step 3: Write the minimal persistence layer and migration**

```go
type Interaction struct {
    ID             string
    TaskID         string
    ConversationID string
    StepIndex      int
    ToolCallID     string
    Kind           string
    Status         string
    RequestJSON    []byte
    ResponseJSON   []byte
    RespondedBy    string
    RespondedAt    *time.Time
    CreatedAt      time.Time
    UpdatedAt      time.Time
}

const (
    KindApproval = "approval"
    KindQuestion = "question"
)
```

Implementation notes:
- Keep request/response bodies as JSON so approval and question can share one table.
- Add migration `to011` that creates `interactions` and backfills `tool_approvals` rows into `kind=approval` records, preserving IDs for in-flight compatibility.
- Wire `interactions.Store` into `Serve()` and `router.Dependencies` without removing `tool_approvals` yet.

**Step 4: Run the focused tests and verify they pass**

Run: `go test ./core/interactions ./app/migration -run 'Test(StoreCreateInteractionPersistsQuestionRequest|TaskMigrationCreatesInteractionsTableAndBackfillsApprovals|TestTaskMigrationCreatesToolApprovalsTable)'`

Expected: PASS.

**Step 5: Commit**

```bash
git add core/interactions/store.go core/interactions/store_test.go app/migration/define.go app/migration/init.go app/migration/task_migration_test.go app/commands/serve.go app/router/deps.go
git commit -m "feat: add persisted interaction requests"
```

### Task 2: Generalize Checkpoint/Resume From Approval To Interaction

**Files:**
- Modify: `core/types/task_metadata.go`
- Modify: `core/agent/types.go`
- Modify: `core/agent/executor.go`
- Modify: `core/agent/stream.go`
- Modify: `core/agent/task_adapter.go`
- Modify: `core/tasks/runtime.go`
- Modify: `core/tasks/manager.go`
- Modify: `core/agent/runner_test.go`
- Modify: `core/agent/executor_test.go`
- Modify: `core/tasks/manager_test.go`

**Step 1: Write the failing runtime tests**

```go
func TestRunnerSuspendsQuestionToolAsInteraction(t *testing.T) {
    result, err := runner.Run(ctx, RunInput{Messages: []model.Message{{Role: model.RoleUser, Content: "deploy it"}}})
    if !errors.Is(err, ErrInteractionPending) {
        t.Fatalf("Run() error = %v, want ErrInteractionPending", err)
    }
    if result.StopReason != "waiting_for_interaction" {
        t.Fatalf("StopReason = %q", result.StopReason)
    }
}

func TestExecutorResumesLegacyApprovalCheckpointThroughInteractionResume(t *testing.T) {
    // metadata contains old tool_approval_checkpoint key
    // executor should still resume the task after migrated interaction lookup
}
```

**Step 2: Run the focused tests and verify they fail**

Run: `go test ./core/agent ./core/tasks -run 'Test(RunnerSuspendsQuestionToolAsInteraction|ExecutorResumesLegacyApprovalCheckpointThroughInteractionResume)'`

Expected: FAIL because runtime still only knows approval checkpoint data.

**Step 3: Implement the checkpoint rename and dual-read compatibility**

```go
const (
    TaskMetadataKeyInteractionCheckpoint = "interaction_checkpoint"
    TaskMetadataKeyToolApprovalCheckpoint = "tool_approval_checkpoint" // legacy read path only
)

type interactionCheckpoint struct {
    InteractionID                    string
    Step                             int
    AssistantMessage                 model.Message
    ToolCallIndex                    int
    ProducedMessagesBeforeCheckpoint []model.Message
}
```

Implementation notes:
- Replace `ToolApprovalResume` with `InteractionResume`, but keep executor able to read legacy `tool_approval_checkpoint` for in-flight tasks.
- Rename suspend reason to `waiting_for_interaction` for new work, while manager/runtime still reconcile legacy `waiting_for_tool_approval` tasks.
- Centralize resume decision building so approved answers and question answers both become synthetic tool output when appropriate.

**Step 4: Run the focused tests and verify they pass**

Run: `go test ./core/agent ./core/tasks -run 'Test(RunnerSuspendsQuestionToolAsInteraction|ExecutorResumesLegacyApprovalCheckpointThroughInteractionResume|TestAgentExecutorResumesApprovedToolCallFromApprovalCheckpoint)'`

Expected: PASS.

**Step 5: Commit**

```bash
git add core/types/task_metadata.go core/agent/types.go core/agent/executor.go core/agent/stream.go core/agent/task_adapter.go core/tasks/runtime.go core/tasks/manager.go core/agent/runner_test.go core/agent/executor_test.go core/tasks/manager_test.go
git commit -m "refactor: unify agent checkpoint flow around interactions"
```

### Task 3: Add `ask_user` And Unified Interaction Audit Events

**Files:**
- Create: `core/tools/builtin/ask_user.go`
- Modify: `core/tools/builtin/register.go`
- Modify: `core/tools/builtin/register_test.go`
- Modify: `core/audit/types.go`
- Modify: `core/agent/events.go`
- Modify: `core/agent/stream.go`
- Create: `core/interactions/audit.go`
- Create: `core/interactions/audit_test.go`

**Step 1: Write the failing tool and audit tests**

```go
func TestRegisterRegistersAskUserTool(t *testing.T) {
    registry := newBuiltinRegistry(t, Options{WorkspaceRoot: t.TempDir()})
    tool := toolDefinitionByName(t, registry, "ask_user")
    if tool.Parameters.Properties["options"].Items == nil {
        t.Fatal("ask_user.options items = nil")
    }
}

func TestRunnerAuditRecordsInteractionRequestedBeforeSuspend(t *testing.T) {
    // expect phase=interaction, event_type=interaction.requested,
    // artifact kind=interaction_request before waiting return
}
```

**Step 2: Run the focused tests and verify they fail**

Run: `go test ./core/tools/builtin ./core/agent ./core/interactions -run 'Test(RegisterRegistersAskUserTool|RunnerAuditRecordsInteractionRequestedBeforeSuspend)'`

Expected: FAIL because `ask_user` and interaction audit phase do not exist.

**Step 3: Implement the special tool and audit helpers**

```go
func newAskUserTool(env environment) coretools.Tool {
    return coretools.Tool{
        Name:        "ask_user",
        Description: "Request structured human clarification and pause the task",
        Parameters: types.JSONSchema{ /* question, options, allow_custom */ },
        Handler: func(context.Context, map[string]any) (string, error) {
            return "", fmt.Errorf("ask_user is handled by the agent runtime")
        },
    }
}
```

Implementation notes:
- Do not execute `ask_user` through `registry.Execute`; intercept it in `Runner.executeAssistantToolCalls` and create a `kind=question` interaction instead.
- Add `PhaseInteraction` and artifact kinds `interaction_request`, `interaction_response`.
- Emit `interaction.requested` from the runner before suspend, and emit `interaction.responded` / `interaction.resumed` through a small helper that uses the full `core/audit.Recorder` so phase, step index, and artifacts are preserved.

**Step 4: Run the focused tests and verify they pass**

Run: `go test ./core/tools/builtin ./core/agent ./core/interactions -run 'Test(RegisterRegistersAskUserTool|RunnerAuditRecordsInteractionRequestedBeforeSuspend|TestInteractionAuditRecordsResponseAndResume)'`

Expected: PASS.

**Step 5: Commit**

```bash
git add core/tools/builtin/ask_user.go core/tools/builtin/register.go core/tools/builtin/register_test.go core/audit/types.go core/agent/events.go core/agent/stream.go core/interactions/audit.go core/interactions/audit_test.go
git commit -m "feat: add ask_user tool and interaction audit trail"
```

### Task 4: Expose Interaction HTTP APIs And Keep Approval Compatibility Endpoints

**Files:**
- Create: `app/handlers/interaction_handler.go`
- Create: `app/handlers/interaction_handler_test.go`
- Modify: `app/handlers/approval_handler.go`
- Modify: `app/handlers/approval_handler_test.go`
- Modify: `app/handlers/swagger_types.go`
- Modify: `app/handlers/swagger_handler_test.go`
- Modify: `app/router/init.go`
- Modify: `app/commands/serve.go`
- Modify: `docs/swagger/docs.go`
- Modify: `docs/swagger/swagger.yaml`

**Step 1: Write the failing handler tests**

```go
func TestInteractionHandlerRespondsToQuestionAndResumesTask(t *testing.T) {
    response := postInteractionResponseEnvelope(t, server.URL, ownerCookie, task.ID, interaction.ID, map[string]any{
        "selected_option_id": "staging",
        "custom_text": "",
    })
    if !response.OK {
        t.Fatalf("response OK = false, message = %q", response.Message)
    }
}

func TestApprovalHandlerDecisionUsesInteractionCompatibilityWrapper(t *testing.T) {
    // old /approvals endpoint should still return approval-shaped payloads
    // backed by the new interaction store
}
```

**Step 2: Run the focused tests and verify they fail**

Run: `go test ./app/handlers -run 'Test(InteractionHandlerRespondsToQuestionAndResumesTask|ApprovalHandlerDecisionUsesInteractionCompatibilityWrapper|SwaggerJSONIncludesApprovalPathsAndDefinitions)'`

Expected: FAIL because interaction routes and swagger types do not exist.

**Step 3: Implement handlers, SSE event aliases, and swagger docs**

```go
type InteractionResponseRequest struct {
    SelectedOptionID string `json:"selected_option_id"`
    CustomText       string `json:"custom_text"`
}
```

Implementation notes:
- Add `GET /tasks/:id/interactions` and `POST /tasks/:id/interactions/:interactionID/respond`.
- Keep `/tasks/:id/approvals` and `/tasks/:id/approvals/:approvalID/decision` as thin wrappers that filter/map `kind=approval` interactions.
- Publish `interaction.requested` and `interaction.responded` SSE events; for approval compatibility, continue publishing `approval.requested` / `approval.resolved` aliases when `kind=approval`.
- Regenerate swagger artifacts rather than hand-editing generated docs.

**Step 4: Run the focused tests and verify they pass**

Run: `go test ./app/handlers -run 'Test(InteractionHandlerRespondsToQuestionAndResumesTask|ApprovalHandlerDecisionUsesInteractionCompatibilityWrapper|TestSwaggerJSONIncludesApprovalPathsAndDefinitions|TestSwaggerJSONIncludesApprovalFailureCodes)'`

Expected: PASS.

**Step 5: Commit**

```bash
git add app/handlers/interaction_handler.go app/handlers/interaction_handler_test.go app/handlers/approval_handler.go app/handlers/approval_handler_test.go app/handlers/swagger_types.go app/handlers/swagger_handler_test.go app/router/init.go app/commands/serve.go docs/swagger/docs.go docs/swagger/swagger.yaml
git commit -m "feat: expose interaction APIs with approval compatibility"
```

### Task 5: Update Chat UI To Render And Answer Interactions

**Files:**
- Create: `webapp/src/components/InteractionRecordCard.vue`
- Modify: `webapp/src/components/MessageList.vue`
- Modify: `webapp/src/components/MessageList.spec.ts`
- Modify: `webapp/src/views/ChatView.vue`
- Modify: `webapp/src/views/ChatView.spec.ts`
- Modify: `webapp/src/lib/api.ts`
- Modify: `webapp/src/lib/api.spec.ts`
- Modify: `webapp/src/lib/transcript.ts`
- Modify: `webapp/src/lib/transcript.spec.ts`
- Modify: `webapp/src/types/api.ts`

**Step 1: Write the failing frontend tests**

```ts
it('renders a question interaction card with options and custom input', async () => {
  const entries = [buildQuestionInteractionEntry({ status: 'pending' })]
  const wrapper = mount(MessageList, { props: { entries, loading: false } })
  expect(wrapper.text()).toContain('Which environment?')
  expect(wrapper.find('[data-interaction-option="staging"]').exists()).toBe(true)
})

it('hydrates pending interactions when reopening a waiting task', async () => {
  api.fetchTaskInteractions.mockResolvedValue([questionInteraction])
  expect(wrapper.find('.interaction-card').exists()).toBe(true)
})
```

**Step 2: Run the focused frontend tests and verify they fail**

Run: `pnpm exec vitest run src/components/MessageList.spec.ts src/views/ChatView.spec.ts src/lib/transcript.spec.ts src/lib/api.spec.ts`

Expected: FAIL because interaction types, API helpers, and cards do not exist.

**Step 3: Implement the minimal chat-only interaction UI**

```ts
export interface InteractionRecord {
  id: string
  kind: 'approval' | 'question'
  status: 'pending' | 'responded' | 'expired' | 'cancelled'
  request: {
    prompt?: string
    options?: Array<{ id: string; label: string; description?: string }>
    allow_custom?: boolean
  }
  response?: {
    selected_option_id?: string
    custom_text?: string
  }
}
```

Implementation notes:
- Add `fetchTaskInteractions()` and `respondTaskInteraction()` helpers.
- Update transcript entries from `kind='approval'` to a generic `kind='interaction'` shape, but keep approval rendering behavior via `interaction.kind === 'approval'`.
- In `ChatView`, hydrate `waiting_for_interaction`, submit interaction responses, and restart SSE after a successful response.
- Keep the old approval page untouched unless shared types require a small compatibility adjustment.

**Step 4: Run the focused frontend checks and verify they pass**

Run: `pnpm exec vitest run src/components/MessageList.spec.ts src/views/ChatView.spec.ts src/lib/transcript.spec.ts src/lib/api.spec.ts && pnpm exec vue-tsc -b`

Expected: PASS.

**Step 5: Commit**

```bash
git add webapp/src/components/InteractionRecordCard.vue webapp/src/components/MessageList.vue webapp/src/components/MessageList.spec.ts webapp/src/views/ChatView.vue webapp/src/views/ChatView.spec.ts webapp/src/lib/api.ts webapp/src/lib/api.spec.ts webapp/src/lib/transcript.ts webapp/src/lib/transcript.spec.ts webapp/src/types/api.ts
git commit -m "feat: support interactive question cards in chat"
```

### Task 6: Run Full Verification And Regression Coverage

**Files:**
- Modify as needed based on failing checks from previous tasks.

**Step 1: Run the backend verification suite**

Run: `go test ./...`

Expected: PASS.

**Step 2: Run the backend build/list verification**

Run: `go build ./cmd/... && go list ./...`

Expected: PASS.

**Step 3: Run the frontend verification suite**

Run: `pnpm exec vitest run src/components/MessageList.spec.ts src/views/ChatView.spec.ts src/lib/transcript.spec.ts src/lib/api.spec.ts && pnpm exec vue-tsc -b`

Expected: PASS.

**Step 4: Re-read the requirements and verify them explicitly**

Checklist:
- `ask_user` exists as a special tool and can suspend/resume the same task.
- approvals and questions both use the shared interaction storage/runtime.
- legacy approval HTTP routes still work.
- existing in-flight approval data can still be resumed/read after upgrade.
- audit replay shows interaction request/response/resume evidence.
- chat UI can render options plus custom text input.

**Step 5: Commit the final stabilization changes**

```bash
git add .
git commit -m "test: verify interaction runtime end-to-end"
```
