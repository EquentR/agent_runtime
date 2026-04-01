# Agent Conversation Task E2E Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build an end-to-end chat execution chain where the UI selects a configured provider/model, sends `conversation_id` with each turn, the backend persists conversation history, creates an `agent.run` task for each turn, executes the agent through `core/tasks`, and writes the new messages back into the conversation for future turns.

**Architecture:** Introduce a lightweight conversation/session store in `core/agent` that persists normalized chat messages plus selected provider/model ids. Keep `tasks` responsible for one execution at a time, and use `conversation_id` as the durable unit of multi-turn chat continuity. The HTTP layer continues to create tasks, but now validates/creates conversations and passes agent input into a registered `agent.run` executor.

**Tech Stack:** Go, Gin, GORM/SQLite, `core/agent`, `core/tasks`, `core/types`, existing task REST/SSE API

---

## Scope And Guardrails

- In scope:
  - persistent conversation/session model
  - UI-supplied `provider_id`, `model_id`, `conversation_id`
  - create-or-continue conversation flow
  - `agent.run` executor registration in app startup
  - conversation history reload into next agent loop
  - HTTP/API extensions for conversation-aware task creation
- Out of scope:
  - full chat UI
  - auth / tenant isolation
  - branchable conversation trees
  - message editing/regeneration variants
  - websocket push beyond existing task SSE

## Proposed Files

- Create: `core/agent/conversation_store.go`
- Create: `core/agent/conversation_store_test.go`
- Create: `core/agent/executor.go`
- Create: `core/agent/executor_test.go`
- Modify: `app/commands/serve.go`
- Modify: `app/handlers/task_handler.go`
- Modify: `app/handlers/task_handler_test.go`
- Modify: `app/migration/define.go`
- Modify: `app/migration/init.go`
- Modify: `app/migration/task_migration_test.go`
- Modify: `README.md`
- Modify: `core/agent/README.md`

Optional follow-up only if needed during implementation:

- Create: `app/handlers/conversation_handler.go`
- Create: `app/handlers/conversation_handler_test.go`

For the first cut, do not add a separate conversation REST API unless tests show task-only creation is too awkward.

## Data Model

Create two new GORM models in `core/agent/conversation_store.go`:

```go
type Conversation struct {
	ID         string    `gorm:"type:varchar(64);primaryKey"`
	ProviderID string    `gorm:"type:varchar(128);not null;index"`
	ModelID    string    `gorm:"type:varchar(128);not null"`
	Title      string    `gorm:"type:varchar(255)"`
	CreatedBy  string    `gorm:"type:varchar(128)"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type ConversationMessage struct {
	ID             uint64          `gorm:"primaryKey;autoIncrement"`
	ConversationID string          `gorm:"type:varchar(64);not null;index"`
	Seq            int64           `gorm:"not null;index"`
	Role           string          `gorm:"type:varchar(32);not null"`
	Content        string          `gorm:"type:text"`
	MessageJSON    json.RawMessage `gorm:"type:blob;not null"`
	TaskID         string          `gorm:"type:varchar(64);index"`
	CreatedAt      time.Time
}
```

Rules:

- Persist the full normalized `model.Message` in `MessageJSON`
- Keep `Role`/`Content` duplicated for cheap listing/debugging
- Sequence is append-only within a conversation
- Store selected `ProviderID` and `ModelID` on `Conversation`, not per message

### Task 1: Add conversation store and migration

**Files:**
- Create: `core/agent/conversation_store.go`
- Create: `core/agent/conversation_store_test.go`
- Modify: `app/migration/define.go`
- Modify: `app/migration/task_migration_test.go`

**Step 1: Write the failing tests**

Add:

- `TestConversationStoreCreateConversation`
- `TestConversationStoreAppendAndListMessages`
- `TestBootstrapMigratesConversationTables`

**Step 2: Run tests to verify they fail**

Run: `go test ./core/agent ./app/migration -run "TestConversationStore|TestBootstrapMigratesConversationTables"`
Expected: FAIL

**Step 3: Write minimal implementation**

Implement a store API such as:

```go
type ConversationStore struct { db *gorm.DB }

func NewConversationStore(db *gorm.DB) *ConversationStore
func (s *ConversationStore) AutoMigrate() error
func (s *ConversationStore) GetConversation(ctx context.Context, id string) (*Conversation, error)
func (s *ConversationStore) CreateConversation(ctx context.Context, input CreateConversationInput) (*Conversation, error)
func (s *ConversationStore) EnsureConversation(ctx context.Context, input EnsureConversationInput) (*Conversation, error)
func (s *ConversationStore) AppendMessages(ctx context.Context, conversationID string, taskID string, messages []model.Message) error
func (s *ConversationStore) ListMessages(ctx context.Context, conversationID string) ([]model.Message, error)
```

Migration:

- add `to004` for conversation tables
- ensure migration bootstrap test checks for both new tables

**Step 4: Run tests to verify they pass**

Run: `go test ./core/agent ./app/migration -run "TestConversationStore|TestBootstrapMigratesConversationTables"`
Expected: PASS

### Task 2: Add configured model resolution by provider id + model id

**Files:**
- Create: `core/agent/executor.go`
- Create: `core/agent/executor_test.go`

**Step 1: Write the failing tests**

Add tests:

- `TestResolveConfiguredModelByProviderAndModelID`
- `TestResolveConfiguredModelRejectsUnknownProvider`
- `TestResolveConfiguredModelRejectsUnknownModel`

**Step 2: Run tests to verify they fail**

Run: `go test ./core/agent -run "TestResolveConfiguredModel"`
Expected: FAIL

**Step 3: Write minimal implementation**

Since current config only exposes one `LLMProvider`, implement the first cut as:

- `provider_id` must match `config.LLM.ProviderName()`
- `model_id` must match `config.LLM.FindModel(modelID)`

Keep the resolver shape future-friendly:

```go
type ModelResolver struct {
	Provider *coretypes.LLMProvider
}

func (r *ModelResolver) Resolve(providerID, modelID string) (*coretypes.LLMModel, error)
```

This preserves the UI contract now, while leaving room for multiple providers later.

**Step 4: Run tests to verify they pass**

Run: `go test ./core/agent -run "TestResolveConfiguredModel"`
Expected: PASS

### Task 3: Define `agent.run` task input and executor behavior

**Files:**
- Modify: `core/agent/executor.go`
- Modify: `core/agent/executor_test.go`

**Step 1: Write the failing tests**

Add:

- `TestAgentExecutorLoadsConversationHistoryAndAppendsNewTurn`
- `TestAgentExecutorCreatesConversationWhenMissing`
- `TestAgentExecutorUsesTaskRuntimeSink`

Input shape for `agent.run`:

```go
type RunTaskInput struct {
	ConversationID string `json:"conversation_id"`
	ProviderID     string `json:"provider_id"`
	ModelID        string `json:"model_id"`
	UserID         string `json:"user_id,omitempty"`
	Message        string `json:"message"`
	SystemPrompt   string `json:"system_prompt,omitempty"`
	CreatedBy      string `json:"created_by,omitempty"`
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./core/agent -run "TestAgentExecutor"`
Expected: FAIL

**Step 3: Write minimal implementation**

Implement an app-facing executor constructor:

```go
type ExecutorDependencies struct {
	Resolver          *ModelResolver
	ConversationStore *ConversationStore
	Registry          *tools.Registry
	ClientFactory     ...
}

func NewTaskExecutor(deps ExecutorDependencies) coretasks.Executor
```

Execution flow:

1. decode `RunTaskInput` from `task.InputJSON`
2. resolve configured provider/model
3. ensure conversation exists and matches provider/model
4. load historical messages from conversation store
5. append current user message as this turn input
6. create `agent.Runner` with `NewTaskRuntimeSink(runtime)`
7. execute `runner.Run(...)`
8. append user + produced assistant/tool messages into conversation store
9. return result payload containing conversation id, final message, usage, cost

Do not persist history from task events; persist from normalized `model.Message` objects.

**Step 4: Run tests to verify they pass**

Run: `go test ./core/agent -run "TestAgentExecutor"`
Expected: PASS

### Task 4: Register `agent.run` executor during app startup

**Files:**
- Modify: `app/commands/serve.go`

**Step 1: Write a failing integration-style test if practical**

If adding a direct `serve.go` test is too heavy, cover registration through handler tests in Task 5 instead.

**Step 2: Write minimal implementation**

In `Serve(...)`:

- create `conversationStore := agent.NewConversationStore(db.DB())`
- build a `ModelResolver` from `c.LLM`
- build required provider client factory for the configured provider
- register `taskManager.RegisterExecutor("agent.run", agent.NewTaskExecutor(...))`

For the first cut, only support whatever provider is configured in `conf/app.yaml`. If the configured provider type/model pair is unsupported, return executor error at runtime rather than widening scope now.

**Step 3: Verify registration path indirectly**

Use the HTTP tests from Task 5 to prove the executor is reachable end-to-end.

### Task 5: Extend task creation HTTP flow for conversation-aware agent runs

**Files:**
- Modify: `app/handlers/task_handler.go`
- Modify: `app/handlers/task_handler_test.go`

**Step 1: Write the failing HTTP tests**

Add:

- `TestTaskHandlerCreateAgentRunTaskWithConversationInput`
- `TestTaskHandlerAgentRunEndToEndAppendsConversationHistory`

The second test should:

1. start a task manager with real `agent.run` executor
2. POST first `agent.run` task with `conversation_id`, `provider_id`, `model_id`, `message`
3. wait for success
4. POST second `agent.run` task with same `conversation_id`
5. verify the executor loaded the earlier turn and appended the second turn

**Step 2: Run tests to verify they fail**

Run: `go test ./app/handlers -run "TestTaskHandlerCreateAgentRunTaskWithConversationInput|TestTaskHandlerAgentRunEndToEndAppendsConversationHistory"`
Expected: FAIL

**Step 3: Write minimal implementation**

Update `CreateTaskRequest` docs/comments if needed, but keep the endpoint generic.

Recommended behavior:

- `task_type == "agent.run"` accepts input payload containing:
  - `conversation_id`
  - `provider_id`
  - `model_id`
  - `message`
- if `conversation_id` is empty, let the executor create one and return it in task result

Do not add separate validation logic in handler that duplicates executor validation unless it improves obvious HTTP errors.

**Step 4: Run tests to verify they pass**

Run: `go test ./app/handlers -run "TestTaskHandlerCreateAgentRunTaskWithConversationInput|TestTaskHandlerAgentRunEndToEndAppendsConversationHistory"`
Expected: PASS

### Task 6: Make result payload UI-friendly enough for next turn reload

**Files:**
- Modify: `core/agent/executor.go`
- Modify: `core/agent/executor_test.go`

**Step 1: Write the failing tests**

Add assertions that terminal task result includes at least:

- `conversation_id`
- `provider_id`
- `model_id`
- `final_message`
- `usage`
- `cost`
- `messages_appended`

**Step 2: Run tests to verify they fail**

Run: `go test ./core/agent -run "TestAgentExecutor.*Result"`
Expected: FAIL

**Step 3: Write minimal implementation**

Return a structured result object from executor:

```go
type RunTaskResult struct {
	ConversationID  string                `json:"conversation_id"`
	ProviderID      string                `json:"provider_id"`
	ModelID         string                `json:"model_id"`
	FinalMessage    model.Message         `json:"final_message"`
	Usage           model.TokenUsage      `json:"usage"`
	Cost            *coretypes.CostBreakdown `json:"cost,omitempty"`
	MessagesAppended int                  `json:"messages_appended"`
}
```

This gives the UI everything needed to continue the next round.

**Step 4: Run tests to verify they pass**

Run: `go test ./core/agent -run "TestAgentExecutor.*Result"`
Expected: PASS

### Task 7: Run full verification and update docs

**Files:**
- Modify: `core/agent/README.md`
- Modify: `README.md`

**Step 1: Update docs after code is green**

Document:

- conversation persistence model
- `conversation_id` + `provider_id` + `model_id` task input contract
- one-task-per-turn execution model
- how next turn reload works

**Step 2: Run focused verification**

Run: `go test ./core/agent ./app/handlers ./app/migration ./core/tasks`
Expected: PASS

**Step 3: Run full verification**

Run: `go test ./...`
Expected: PASS

Run: `go build ./cmd/...`
Expected: PASS

Run: `go list ./...`
Expected: PASS

## Implementation Notes

- Persist conversation history separately from tasks; tasks remain execution units, not session containers.
- Always reload prior messages from conversation store before each `agent.run` turn.
- Conversation and selected provider/model must remain stable after creation; reject mismatched future turns.
- Keep first cut provider resolution simple: one configured provider matched by `provider_id`, one model selected by `model_id`.
- Store normalized `model.Message` JSON so replay-related fields survive across turns.
- Reuse `NewTaskRuntimeSink(...)` so task SSE automatically exposes agent execution progress.

## Suggested Commit Breakdown

Only create commits if the user explicitly asks.

- Commit 1: `feat: add conversation store for multi-turn agent sessions`
- Commit 2: `feat: register agent.run executor with conversation reload`
- Commit 3: `test: cover end-to-end agent task execution chain`
- Commit 4: `docs: document conversation-backed agent tasks`
