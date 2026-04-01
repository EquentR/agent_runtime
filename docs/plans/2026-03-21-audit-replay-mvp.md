# Audit Replay MVP Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a first-version audit system that records `agent.run` execution as replayable evidence bundles and exposes run/event/replay read APIs for debugging.

**Architecture:** Keep `core/tasks` as the task snapshot plus lightweight event projection layer, then add a new `core/audit` package for append-only audit runs, events, and artifacts. Wire a recorder into task lifecycle, agent executor, and loop execution so each run can be reconstructed from persisted evidence without re-executing tools or model calls.

**Tech Stack:** Go, GORM, Gin, SQLite, existing `core/tasks` runtime, existing `core/agent` runner and conversation store.

---

### Task 1: Define Audit Domain Models And Migration

**Files:**
- Create: `core/audit/model.go`
- Create: `core/audit/types.go`
- Modify: `app/migration/define.go`
- Test: `core/audit/store_test.go`

**Step 1: Write the failing test**

Create `core/audit/store_test.go` with a migration-focused test that auto-migrates the audit models and verifies an `AuditRun`, `AuditEvent`, and `AuditArtifact` record can be created and queried back with stable sequence ordering.

```go
func TestAuditStoreAutoMigrateAndPersistRunGraph(t *testing.T) {
	db := newTestDB(t)
	store := NewStore(db)
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	run := &Run{ID: "run_1", TaskID: "task_1", TaskType: "agent.run", Status: "running", SchemaVersion: "v1"}
	if err := db.Create(run).Error; err != nil {
		t.Fatalf("create run error = %v", err)
	}
	artifact := &Artifact{ID: "art_1", RunID: run.ID, Kind: "request_messages", MimeType: "application/json"}
	if err := db.Create(artifact).Error; err != nil {
		t.Fatalf("create artifact error = %v", err)
	}
	event := &Event{RunID: run.ID, TaskID: run.TaskID, Seq: 1, EventType: "run.started", RefArtifactID: artifact.ID}
	if err := db.Create(event).Error; err != nil {
		t.Fatalf("create event error = %v", err)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./core/audit -run TestAuditStoreAutoMigrateAndPersistRunGraph -v`
Expected: FAIL with package or symbol not found errors because `core/audit` does not exist yet.

**Step 3: Write minimal implementation**

Create `core/audit/model.go` with GORM models for `Run`, `Event`, and `Artifact`. Keep `Event` append-only with unique `(run_id, seq)` and keep `Artifact` separated for large blobs.

```go
type Run struct {
	ID            string    `gorm:"type:varchar(64);primaryKey"`
	TaskID        string    `gorm:"type:varchar(64);not null;index"`
	ConversationID string   `gorm:"type:varchar(64);index"`
	TaskType      string    `gorm:"type:varchar(128);not null;index"`
	ProviderID    string    `gorm:"type:varchar(128);index"`
	ModelID       string    `gorm:"type:varchar(128);index"`
	RunnerID      string    `gorm:"type:varchar(128);index"`
	Status        string    `gorm:"type:varchar(32);not null;index"`
	CreatedBy     string    `gorm:"type:varchar(128)"`
	Replayable    bool      `gorm:"not null;default:true"`
	SchemaVersion string    `gorm:"type:varchar(16);not null"`
	StartedAt     *time.Time
	FinishedAt    *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type Event struct {
	ID            uint64          `gorm:"primaryKey;autoIncrement"`
	RunID         string          `gorm:"type:varchar(64);not null;uniqueIndex:idx_audit_run_seq,priority:1;index"`
	TaskID        string          `gorm:"type:varchar(64);not null;index"`
	Seq           int64           `gorm:"not null;uniqueIndex:idx_audit_run_seq,priority:2"`
	Phase         string          `gorm:"type:varchar(64);not null;index"`
	EventType     string          `gorm:"type:varchar(64);not null;index"`
	Level         string          `gorm:"type:varchar(16);not null"`
	StepIndex     int             `gorm:"not null;default:0"`
	ParentSeq     int64           `gorm:"not null;default:0"`
	RefArtifactID string          `gorm:"type:varchar(64);index"`
	PayloadJSON   json.RawMessage `gorm:"column:payload_json;type:blob;not null"`
	CreatedAt     time.Time       `gorm:"not null;index"`
}

type Artifact struct {
	ID             string          `gorm:"type:varchar(64);primaryKey"`
	RunID          string          `gorm:"type:varchar(64);not null;index"`
	Kind           string          `gorm:"type:varchar(64);not null;index"`
	MimeType       string          `gorm:"type:varchar(128);not null"`
	Encoding       string          `gorm:"type:varchar(32);not null"`
	SizeBytes      int64           `gorm:"not null;default:0"`
	SHA256         string          `gorm:"type:varchar(64);index"`
	RedactionState string          `gorm:"type:varchar(32);not null;default:'raw'"`
	BodyJSON       json.RawMessage `gorm:"column:body_json;type:blob"`
	CreatedAt      time.Time       `gorm:"not null;index"`
}
```

Add `core/audit/types.go` constants for schema version, phases, and artifact kinds. Register the new models in a new migration in `app/migration/define.go`.

**Step 4: Run test to verify it passes**

Run: `go test ./core/audit -run TestAuditStoreAutoMigrateAndPersistRunGraph -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add core/audit/model.go core/audit/types.go core/audit/store_test.go app/migration/define.go
git commit -m "feat: add audit persistence models"
```

### Task 2: Build The Audit Store And Recorder Interface

**Files:**
- Create: `core/audit/store.go`
- Create: `core/audit/recorder.go`
- Create: `core/audit/noop.go`
- Test: `core/audit/store_test.go`

**Step 1: Write the failing test**

Add tests for:
- `StartRun` creating a run once per task
- `AppendEvent` allocating monotonic `seq`
- `AttachArtifact` persisting JSON body and returning artifact id
- `FinishRun` setting terminal status and timestamps

```go
func TestRecorderAppendsEventsInSequence(t *testing.T) {
	db := newTestDB(t)
	store := NewStore(db)
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	recorder := NewRecorder(store)
	if _, err := recorder.StartRun(context.Background(), StartRunInput{RunID: "run_1", TaskID: "task_1", TaskType: "agent.run"}); err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	first, err := recorder.AppendEvent(context.Background(), "run_1", AppendEventInput{EventType: "run.started"})
	if err != nil {
		t.Fatalf("AppendEvent(first) error = %v", err)
	}
	second, err := recorder.AppendEvent(context.Background(), "run_1", AppendEventInput{EventType: "step.started"})
	if err != nil {
		t.Fatalf("AppendEvent(second) error = %v", err)
	}
	if first.Seq != 1 || second.Seq != 2 {
		t.Fatalf("seqs = [%d %d], want [1 2]", first.Seq, second.Seq)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./core/audit -run TestRecorderAppendsEventsInSequence -v`
Expected: FAIL because `NewRecorder`, `StartRun`, and `AppendEvent` are not implemented.

**Step 3: Write minimal implementation**

Implement a `Store` with helpers like:
- `AutoMigrate()`
- `CreateRun(ctx, run Run)`
- `GetRunByTaskID(ctx, taskID string)`
- `AppendEvent(ctx, runID string, input AppendEventInput)`
- `CreateArtifact(ctx, runID string, input CreateArtifactInput)`
- `FinishRun(ctx, runID string, status string, finishedAt time.Time)`

Implement a `Recorder` interface so callers depend on behavior, not storage:

```go
type Recorder interface {
	StartRun(ctx context.Context, input StartRunInput) (*Run, error)
	AppendEvent(ctx context.Context, runID string, input AppendEventInput) (*Event, error)
	AttachArtifact(ctx context.Context, runID string, input CreateArtifactInput) (*Artifact, error)
	FinishRun(ctx context.Context, runID string, input FinishRunInput) error
}
```

Add a `NoopRecorder` so existing code can opt out cleanly when audit is not configured.

**Step 4: Run test to verify it passes**

Run: `go test ./core/audit -run 'TestRecorder|TestAuditStore' -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add core/audit/store.go core/audit/recorder.go core/audit/noop.go core/audit/store_test.go
git commit -m "feat: add audit recorder primitives"
```

### Task 3: Wire Audit Run Lifecycle Into Task Creation And Completion

**Files:**
- Modify: `core/tasks/manager.go`
- Modify: `core/tasks/store.go`
- Modify: `app/commands/serve.go`
- Modify: `app/router/deps.go`
- Test: `core/tasks/manager_test.go`

**Step 1: Write the failing test**

Extend `core/tasks/manager_test.go` with a fake recorder and assert:
- `CreateTask` causes an audit run to exist or be reserved
- `task.started` triggers `run.started`
- terminal task completion triggers `run.succeeded` or `run.failed`

```go
func TestManagerPublishesAuditRunLifecycle(t *testing.T) {
	store := newTestStore(t)
	recorder := newRecordingAuditRecorder()
	manager := NewManager(store, ManagerOptions{RunnerID: "runner-1"})
	manager.audit = recorder
	if err := manager.RegisterExecutor("test", func(context.Context, *Task, *Runtime) (any, error) {
		return map[string]any{"ok": true}, nil
	}); err != nil {
		t.Fatalf("RegisterExecutor() error = %v", err)
	}
	manager.Start(context.Background())
	_, err := manager.CreateTask(context.Background(), CreateTaskInput{TaskType: "test"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	waitForRecorderEvent(t, recorder, "run.succeeded")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./core/tasks -run TestManagerPublishesAuditRunLifecycle -v`
Expected: FAIL because the manager has no audit dependency yet.

**Step 3: Write minimal implementation**

Add an optional audit dependency to the task manager and/or task store constructor path. Recommended pattern:

```go
type Manager struct {
	store *Store
	hub   *EventHub
	audit audit.Recorder
	...
}
```

On task creation, append a lightweight run bootstrap event such as `run.created`. On claim/start, append `run.started`. In `executeTask`, finish the audit run with `succeeded`, `failed`, or `cancelled` after the task store writes terminal state.

Keep `task_events` intact; the new audit calls are additive only.

**Step 4: Run test to verify it passes**

Run: `go test ./core/tasks -run TestManagerPublishesAuditRunLifecycle -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add core/tasks/manager.go core/tasks/store.go core/tasks/manager_test.go app/commands/serve.go app/router/deps.go
git commit -m "feat: audit task lifecycle"
```

### Task 4: Record Executor-Level Conversation And Persistence Events

**Files:**
- Modify: `core/agent/executor.go`
- Create: `core/agent/audit.go`
- Test: `core/agent/executor_test.go`

**Step 1: Write the failing test**

Add a recorder-backed executor test that verifies a normal `agent.run` emits:
- `conversation.loaded`
- `user_message.appended`
- `messages.persisted`
- `run.succeeded`

Also verify that the request conversation snapshot is attached as an artifact before the first model call.

```go
func TestAgentExecutorRecordsConversationAuditEvents(t *testing.T) {
	store := newConversationStoreForTest(t)
	recorder := newRecordingAuditRecorder()
	executor := NewTaskExecutor(ExecutorDependencies{
		Resolver: resolverForTest(),
		ConversationStore: store,
		ClientFactory: newStubClientFactory("hello"),
		AuditRecorder: recorder,
	})
	...
	assertAuditEvent(t, recorder, "conversation.loaded")
	assertAuditEvent(t, recorder, "messages.persisted")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./core/agent -run TestAgentExecutorRecordsConversationAuditEvents -v`
Expected: FAIL because executor dependencies do not include audit hooks.

**Step 3: Write minimal implementation**

Add `AuditRecorder audit.Recorder` to `ExecutorDependencies`. Resolve the run id from `task.ID` or a deterministic mapping. In `NewTaskExecutor`:
- record `conversation.loaded` after `ListMessages`
- record `user_message.appended` after user message persistence
- attach a `request_messages` artifact containing the history plus new user message
- record `messages.persisted` after assistant/tool/system messages are stored
- on error, attach `error_snapshot` with message plus partial produced messages

Keep payloads small and move full message arrays into artifacts.

**Step 4: Run test to verify it passes**

Run: `go test ./core/agent -run TestAgentExecutorRecordsConversationAuditEvents -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add core/agent/executor.go core/agent/audit.go core/agent/executor_test.go
git commit -m "feat: audit executor conversation flow"
```

### Task 5: Record Per-Step Loop, Model Request, And Tool Evidence

**Files:**
- Modify: `core/agent/types.go`
- Modify: `core/agent/events.go`
- Modify: `core/agent/stream.go`
- Test: `core/agent/runner_test.go`
- Test: `core/agent/stream_test.go`

**Step 1: Write the failing test**

Add a tool-using runner test that verifies each loop step emits audit-rich events:
- `step.started`
- `prompt.resolved`
- `request.built`
- `model.completed`
- `tool.started`
- `tool.finished`
- `step.finished`

Assert that full request payload, tool arguments, and tool outputs are attached as artifacts.

```go
func TestRunnerEmitsReplayableAuditArtifacts(t *testing.T) {
	registry := newTestRegistry(t, map[string]func(context.Context, map[string]interface{}) (string, error){
		"lookup_weather": func(context.Context, map[string]interface{}) (string, error) { return "sunny", nil },
	})
	recorder := newRecordingAuditRecorder()
	runner, err := NewRunner(client, registry, Options{Model: "test-model", AuditRecorder: recorder, AuditRunID: "run_1"})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	_, err = runner.Run(context.Background(), RunInput{Messages: []model.Message{{Role: model.RoleUser, Content: "weather?"}}})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	assertAuditEvent(t, recorder, "request.built")
	assertArtifactKind(t, recorder, "tool_output")
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./core/agent -run TestRunnerEmitsReplayableAuditArtifacts -v`
Expected: FAIL because `Runner.Options` has no audit fields and stream loop does not attach artifacts.

**Step 3: Write minimal implementation**

Extend `core/agent/types.go` options with audit fields:

```go
type Options struct {
	...
	AuditRecorder audit.Recorder
	AuditRunID    string
}
```

In `core/agent/stream.go`:
- before `ChatStream`, attach `resolved_prompt` and `model_request` artifacts
- after stream completion, attach `model_response` artifact with normalized assistant message and usage summary
- on each tool call, attach `tool_arguments` and `tool_output` artifacts
- emit audit events using a helper so event payloads reference artifact ids instead of inlining huge JSON blobs

If the stream errors, record `step.failed` or `run.failed` payload with the last known step index.

**Step 4: Run test to verify it passes**

Run: `go test ./core/agent -run 'TestRunnerEmitsReplayableAuditArtifacts|TestRunnerEmitsEventSinkSignalsForToolExecution' -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add core/agent/types.go core/agent/events.go core/agent/stream.go core/agent/runner_test.go core/agent/stream_test.go
git commit -m "feat: capture replayable loop evidence"
```

### Task 6: Build Replay Bundle Assembly

**Files:**
- Create: `core/audit/replay.go`
- Create: `core/audit/replay_test.go`
- Modify: `core/audit/store.go`

**Step 1: Write the failing test**

Create a replay assembly test that seeds one run, a few ordered events, and artifacts, then verifies `BuildReplayBundle` returns a stable JSON-ready structure with timeline entries and artifact references resolved.

```go
func TestBuildReplayBundleReturnsOrderedTimeline(t *testing.T) {
	store := seededReplayStore(t)
	bundle, err := BuildReplayBundle(context.Background(), store, "run_1")
	if err != nil {
		t.Fatalf("BuildReplayBundle() error = %v", err)
	}
	if bundle.Run.ID != "run_1" {
		t.Fatalf("bundle.Run.ID = %q, want run_1", bundle.Run.ID)
	}
	if len(bundle.Timeline) != 3 {
		t.Fatalf("len(bundle.Timeline) = %d, want 3", len(bundle.Timeline))
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./core/audit -run TestBuildReplayBundleReturnsOrderedTimeline -v`
Expected: FAIL because replay assembly is not implemented.

**Step 3: Write minimal implementation**

Implement replay types such as:

```go
type ReplayBundle struct {
	Run       RunSummary         `json:"run"`
	Timeline  []ReplayEventEntry `json:"timeline"`
	Artifacts []ArtifactSummary  `json:"artifacts"`
}
```

`BuildReplayBundle` should:
- load the run
- load ordered events for the run
- load all referenced artifacts
- materialize a deterministic bundle where each event includes artifact metadata and compact payloads

Do not yet inline every artifact body by default; include bodies only for JSON-safe kinds needed for debugging.

**Step 4: Run test to verify it passes**

Run: `go test ./core/audit -run 'TestBuildReplayBundle|TestRecorder' -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add core/audit/replay.go core/audit/replay_test.go core/audit/store.go
git commit -m "feat: assemble audit replay bundles"
```

### Task 7: Expose Read APIs For Audit Runs, Events, And Replay

**Files:**
- Create: `app/handlers/audit_handler.go`
- Create: `app/handlers/audit_handler_test.go`
- Modify: `app/router/init.go`
- Modify: `app/router/deps.go`
- Modify: `docs/swagger/docs.go`

**Step 1: Write the failing test**

Add handler tests for:
- `GET /api/v1/audit/runs/:id`
- `GET /api/v1/audit/runs/:id/events`
- `GET /api/v1/audit/runs/:id/replay`

Follow the existing handler test style from `app/handlers/task_handler_test.go`.

```go
func TestAuditHandlerGetReplayBundle(t *testing.T) {
	server, store := newAuditTestServer(t)
	seedReplayRun(t, store, "run_1")
	resp, err := http.Get(server.URL + "/api/v1/audit/runs/run_1/replay")
	if err != nil {
		t.Fatalf("http.Get() error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want 200", resp.StatusCode)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./app/handlers -run TestAuditHandlerGetReplayBundle -v`
Expected: FAIL because the handler and routes do not exist.

**Step 3: Write minimal implementation**

Create `AuditHandler` following the existing `Register` pattern used by `app/handlers/task_handler.go`. Expose read-only endpoints only. Use `resp.HandlerWrapper(...)` and keep request/response shaping inside the handler.

Add audit dependencies to `router.Dependencies` and register the new handler in `app/router/init.go`.

If Swagger is generated from annotations in this repo, add annotations to the handler and regenerate docs after the handler is stable.

**Step 4: Run test to verify it passes**

Run: `go test ./app/handlers -run 'TestAuditHandler' -v`
Expected: PASS.

**Step 5: Commit**

```bash
git add app/handlers/audit_handler.go app/handlers/audit_handler_test.go app/router/init.go app/router/deps.go docs/swagger/docs.go
git commit -m "feat: expose audit replay APIs"
```

### Task 8: Full Verification And Cleanup

**Files:**
- Modify: `app/commands/serve.go`
- Modify: `conf/app.yaml`
- Modify: `docs/plans/2026-03-21-audit-replay-mvp.md`

**Step 1: Add final verification coverage**

Make sure the app wiring initializes the audit store and recorder exactly once during startup, similar to task store and conversation store wiring.

Optionally add config gates in `conf/app.yaml` only if you need runtime control such as:

```yaml
audit:
  enabled: true
  inlineArtifactBytes: 262144
```

Do not add speculative knobs beyond what the code actively uses.

**Step 2: Run focused package tests**

Run: `go test ./core/audit ./core/agent ./core/tasks ./app/handlers`
Expected: PASS.

**Step 3: Run repository verification**

Run: `go test ./...`
Expected: PASS.

Run: `go build ./cmd/...`
Expected: PASS.

Run: `go list ./...`
Expected: PASS.

**Step 4: Update plan notes**

Record any scope trims directly in this plan file so later harness work knows what was intentionally deferred:
- no cross-run diff
- no sandbox re-execution
- no retention policy yet
- no redaction pipeline yet

**Step 5: Commit**

```bash
git add app/commands/serve.go conf/app.yaml docs/plans/2026-03-21-audit-replay-mvp.md
git commit -m "chore: wire and verify audit replay mvp"
```

### Task 8 Completion Notes

- Final verification stayed green for the focused audit/task/agent/handler packages and for the full repository checks.
- Audit API cleanup added auth/error-path coverage and aligned the generated Swagger `audit run.status` enum with the runtime task status set.
- No `audit` config block was added to `conf/app.yaml`; the MVP keeps audit recording always on because there is no active partial-disable mode or byte-limit knob in use yet.

### Intentionally Deferred After MVP Verification

- no cross-run diff
- no sandbox re-execution
- no retention policy yet
- no redaction pipeline yet
- no audit enable/disable knob yet
