# Task Manager Worker Pool Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Upgrade the persisted task manager from a single serial agent loop to a worker-pool scheduler that can run tasks in parallel across conversations, while preserving same-conversation serialization and establishing the state machine needed for future parent/subagent fan-out and fan-in.

**Architecture:** Keep the database as the single source of truth for queued work and leases. Replace the single manager polling loop with a fixed-size worker pool, add a persisted `concurrency_key` so claim logic can allow cross-conversation parallelism without letting same-conversation tasks overlap, then extend the task state machine with a `waiting` pause state so a parent task can release its worker while subagent child tasks run in parallel and later resume.

**Tech Stack:** Go 1.25, GORM, SQLite, persisted task snapshots and task events in `core/tasks`, agent executor integration in `core/agent`

---

## Current State And Constraints

- The current scheduler is single-consumer: `core/tasks/manager.go` starts one `run()` loop and executes claimed tasks serially.
- Claiming is FIFO-only: `core/tasks/store.go` selects the earliest `queued` task and immediately transitions it to `running`.
- The existing task model already contains useful workflow fields: `RootTaskID`, `ParentTaskID`, `ChildIndex`, `WaitingOnTaskID`, and `SuspendReason` in `core/tasks/model.go`.
- Cancellation, heartbeats, leases, task events, and audit writes already work and should remain intact.
- Future subagent support requires one parent conversation to spawn up to three child conversations that run in parallel, then resume the parent for aggregation.

## Design Decisions To Preserve

- Use a fixed-size worker pool backed by the persisted `tasks` table. Do not introduce an in-memory queue as the primary scheduler.
- Introduce a task-level `concurrency_key` and make claim eligibility depend on that key.
- Treat normal conversation tasks as `concurrency_key = conversation_id`.
- Treat subagent tasks as independent child-conversation tasks with their own child conversation ids as `concurrency_key`.
- Add `StatusWaiting` for paused parent tasks so they do not hold a worker while waiting for children.
- Resume a waiting parent when all of its child tasks are terminal, regardless of success or failure. Let the parent executor decide how to interpret child failures.
- Store executor-private phase markers in `MetadataJSON`; do not add a database column just for executor phase.

## Non-Goals For This Plan

- No priority queue, rate limiting, or dynamic worker autoscaling.
- No generic DAG engine or arbitrary workflow graph runtime.
- No new REST API shape unless implementation work later proves one is necessary.
- No change to the agent executor signature beyond adding runtime helpers and sentinel handling.

## Relevant Files Before Starting

- `core/tasks/types.go`
- `core/tasks/model.go`
- `core/tasks/store.go`
- `core/tasks/manager.go`
- `core/tasks/runtime.go`
- `core/tasks/store_test.go`
- `core/tasks/manager_test.go`
- `core/tasks/test_helpers_test.go`
- `core/agent/task_adapter.go`

## Verification Strategy

- Start with focused store tests for claim semantics before touching manager concurrency.
- Then add focused manager tests that prove real parallelism for different keys and strict serialization for same-key tasks.
- After introducing `waiting`, add tests for suspend/resume and parent-child requeue behavior.
- Finish with `go test ./core/tasks` and then broader repo checks: `go test ./...`, `go build ./cmd/...`, and `go list ./...`.

---

### Task 1: Add worker-pool and concurrency-key scaffolding

**Files:**
- Modify: `core/tasks/types.go`
- Modify: `core/tasks/model.go`
- Modify: `core/tasks/store.go`
- Modify: `core/tasks/manager.go`
- Test: `core/tasks/store_test.go`

**Step 1: Write the failing test**

Add focused store assertions that prove `CreateTask` persists a new `ConcurrencyKey` field and that `RetryTask` copies it forward. Use a new test such as:

```go
func TestStoreCreateTaskPersistsConcurrencyKey(t *testing.T) {
	store := newTestStore(t)

	task, _, err := store.CreateTask(context.Background(), CreateTaskInput{
		TaskType:       "agent.run",
		ConcurrencyKey: "conv_1",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if task.ConcurrencyKey != "conv_1" {
		t.Fatalf("concurrency key = %q, want %q", task.ConcurrencyKey, "conv_1")
	}
}
```

Add a retry coverage variant that expects the retried task to preserve the same key.

**Step 2: Run test to verify it fails**

Run: `go test ./core/tasks -run 'TestStore(CreateTaskPersistsConcurrencyKey|RetryTaskPreservesConcurrencyKey)'`
Expected: FAIL because `CreateTaskInput` and `Task` do not yet contain `ConcurrencyKey`.

**Step 3: Write minimal implementation**

Make these exact structural changes:

```go
// core/tasks/types.go
type CreateTaskInput struct {
	TaskType        string
	Input           any
	Config          any
	Metadata        any
	CreatedBy       string
	IdempotencyKey  string
	ExecutionMode   ExecutionMode
	RootTaskID      string
	ParentTaskID    string
	ChildIndex      int
	RetryOfTaskID   string
	WaitingOnTaskID string
	SuspendReason   string
	ConcurrencyKey  string
}
```

```go
// core/tasks/model.go
type Task struct {
	ID             string          `gorm:"type:varchar(64);primaryKey"`
	TaskType       string          `gorm:"type:varchar(128);not null;index"`
	Status         Status          `gorm:"type:varchar(32);not null;index"`
	ConcurrencyKey string          `gorm:"type:varchar(255);index"`
	// existing fields continue here
}
```

In `core/tasks/store.go`, copy `ConcurrencyKey` in both `newTask()` and `RetryTask()`.

In `core/tasks/manager.go`, add `WorkerCount int` to `ManagerOptions`, persist it on `Manager`, and default it to `1` inside `NewManager`.

**Step 4: Run test to verify it passes**

Run: `go test ./core/tasks -run 'TestStore(CreateTaskPersistsConcurrencyKey|RetryTaskPreservesConcurrencyKey)'`
Expected: PASS.

**Step 5: Commit**

```bash
git add core/tasks/types.go core/tasks/model.go core/tasks/store.go core/tasks/manager.go core/tasks/store_test.go
git commit -m "feat: add task concurrency key scaffolding"
```

---

### Task 2: Add claim tests for same-key serialization and cross-key parallel eligibility

**Files:**
- Modify: `core/tasks/store_test.go`

**Step 1: Write the failing test**

Add these new store tests:

```go
func TestStoreClaimNextTaskBlocksQueuedTaskWithSameConcurrencyKey(t *testing.T) {
	store := newTestStore(t)
	first, _, _ := store.CreateTask(context.Background(), CreateTaskInput{TaskType: "agent.run", ConcurrencyKey: "conv_1"})
	_, _, _ = store.CreateTask(context.Background(), CreateTaskInput{TaskType: "agent.run", ConcurrencyKey: "conv_1"})
	_, _, _ = store.ClaimNextTask(context.Background(), "runner-1", time.Minute)
	claimed, _, err := store.ClaimNextTask(context.Background(), "runner-2", time.Minute)
	if err != nil {
		t.Fatalf("ClaimNextTask() error = %v", err)
	}
	if claimed != nil {
		t.Fatalf("claimed = %#v, want nil while %q is still running", claimed, first.ID)
	}
}

func TestStoreClaimNextTaskSkipsBlockedHeadAndClaimsDifferentConcurrencyKey(t *testing.T) {
	store := newTestStore(t)
	_, _, _ = store.CreateTask(context.Background(), CreateTaskInput{TaskType: "agent.run", ConcurrencyKey: "conv_1"})
	second, _, _ := store.CreateTask(context.Background(), CreateTaskInput{TaskType: "agent.run", ConcurrencyKey: "conv_2"})
	_, _, _ = store.ClaimNextTask(context.Background(), "runner-1", time.Minute)
	claimed, _, err := store.ClaimNextTask(context.Background(), "runner-2", time.Minute)
	if err != nil {
		t.Fatalf("ClaimNextTask() error = %v", err)
	}
	if claimed == nil || claimed.ID != second.ID {
		t.Fatalf("claimed id = %v, want %q", claimed, second.ID)
	}
}
```

Add an additional empty-key test that verifies two tasks with no `ConcurrencyKey` can both be claimed.

**Step 2: Run test to verify it fails**

Run: `go test ./core/tasks -run 'TestStoreClaimNextTask(BlocksQueuedTaskWithSameConcurrencyKey|SkipsBlockedHeadAndClaimsDifferentConcurrencyKey|AllowsParallelClaimsWithoutConcurrencyKey)'`
Expected: FAIL because claim logic still blindly selects the earliest queued task.

**Step 3: Write minimal implementation**

Refactor `ClaimNextTask()` in `core/tasks/store.go` into a small retry wrapper plus a transactional helper. Use batch candidate scanning and conditional update semantics.

Implementation shape:

```go
func (s *Store) ClaimNextTask(ctx context.Context, runnerID string, lease time.Duration) (*Task, []TaskEvent, error) {
	for attempt := 0; attempt < 3; attempt++ {
		task, events, err := s.claimNextTaskOnce(ctx, runnerID, lease)
		if err != nil || task != nil {
			return task, events, err
		}
	}
	return nil, nil, nil
}
```

```go
func (s *Store) claimNextTaskOnce(ctx context.Context, runnerID string, lease time.Duration) (*Task, []TaskEvent, error) {
	// transaction
	// 1. load 16-32 queued candidates ordered by created_at asc, id asc
	// 2. for each candidate, skip it if hasActiveTaskWithSameConcurrencyKey(tx, candidate)
	// 3. conditionally update WHERE id = ? AND status = queued
	// 4. if RowsAffected == 1, append task.started event and return claimed task
	// 5. if none eligible, return nil
}
```

Helper shape:

```go
func hasActiveTaskWithSameConcurrencyKey(tx *gorm.DB, task *Task) (bool, error) {
	if task == nil || strings.TrimSpace(task.ConcurrencyKey) == "" {
		return false, nil
	}
	var count int64
	err := tx.Model(&Task{}).
		Where("id <> ?", task.ID).
		Where("concurrency_key = ?", task.ConcurrencyKey).
		Where("status IN ?", []Status{StatusRunning, StatusCancelRequested}).
		Count(&count).Error
	return count > 0, err
}
```

Do not treat `queued` as active. Do not require special SQL locking beyond the transactional conditional update and `RowsAffected` check.

**Step 4: Run test to verify it passes**

Run: `go test ./core/tasks -run 'TestStoreClaimNextTask(BlocksQueuedTaskWithSameConcurrencyKey|SkipsBlockedHeadAndClaimsDifferentConcurrencyKey|AllowsParallelClaimsWithoutConcurrencyKey)'`
Expected: PASS.

**Step 5: Commit**

```bash
git add core/tasks/store.go core/tasks/store_test.go
git commit -m "feat: serialize task claims by concurrency key"
```

---

### Task 3: Convert the manager from a single loop to a worker pool

**Files:**
- Modify: `core/tasks/manager.go`
- Modify: `core/tasks/manager_test.go`
- Test: `core/tasks/test_helpers_test.go`

**Step 1: Write the failing test**

Add a manager test that proves tasks with different keys can execute concurrently when `WorkerCount` is greater than one.

Example test shape:

```go
func TestManagerExecutesDifferentConcurrencyKeysInParallel(t *testing.T) {
	store := newTestStore(t)
	manager := NewManager(store, ManagerOptions{
		RunnerID:          "runner-1",
		WorkerCount:       2,
		PollInterval:      5 * time.Millisecond,
		LeaseDuration:     100 * time.Millisecond,
		HeartbeatInterval: 20 * time.Millisecond,
	})

	started := make(chan string, 2)
	release := make(chan struct{})
	if err := manager.RegisterExecutor("agent.run", func(ctx context.Context, task *Task, runtime *Runtime) (any, error) {
		started <- task.ConcurrencyKey
		<-release
		return map[string]any{"ok": true}, nil
	}); err != nil {
		t.Fatalf("RegisterExecutor() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.Start(ctx)

	_, _ = manager.CreateTask(ctx, CreateTaskInput{TaskType: "agent.run", ConcurrencyKey: "conv_1"})
	_, _ = manager.CreateTask(ctx, CreateTaskInput{TaskType: "agent.run", ConcurrencyKey: "conv_2"})

	first := <-started
	second := <-started
	if first == second {
		t.Fatalf("started keys = %q and %q, want different keys running concurrently", first, second)
	}
	close(release)
}
```

Add a companion test proving same-key tasks do not overlap even with `WorkerCount: 2`.

**Step 2: Run test to verify it fails**

Run: `go test ./core/tasks -run 'TestManagerExecutes(DifferentConcurrencyKeysInParallel|SameConcurrencyKeySeriallyEvenWithMultipleWorkers)'`
Expected: FAIL because `Start()` still launches only one run loop.

**Step 3: Write minimal implementation**

Refactor manager lifecycle:

```go
type Manager struct {
	store *Store
	hub   *EventHub
	audit AuditRecorder

	runnerID          string
	workerCount       int
	pollInterval      time.Duration
	leaseDuration     time.Duration
	heartbeatInterval time.Duration

	mu           sync.RWMutex
	executors    map[string]Executor
	activeCancel map[string]context.CancelFunc
	startOnce    sync.Once
}
```

```go
func (m *Manager) Start(ctx context.Context) {
	m.startOnce.Do(func() {
		for i := 0; i < m.workerCount; i++ {
			go m.runWorker(ctx, i)
		}
	})
}
```

```go
func (m *Manager) runWorker(ctx context.Context, workerIndex int) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		task, events, err := m.store.ClaimNextTask(ctx, m.runnerID, m.leaseDuration)
		if err != nil {
			time.Sleep(m.pollInterval)
			continue
		}
		if task == nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(m.pollInterval):
			}
			continue
		}

		m.recordTaskStarted(task)
		m.publish(events...)
		m.executeTask(ctx, task)
	}
}
```

Keep `activeCancel` keyed by `task.ID`; it already supports multiple concurrent running tasks.

**Step 4: Run test to verify it passes**

Run: `go test ./core/tasks -run 'TestManagerExecutes(DifferentConcurrencyKeysInParallel|SameConcurrencyKeySeriallyEvenWithMultipleWorkers)'`
Expected: PASS.

**Step 5: Commit**

```bash
git add core/tasks/manager.go core/tasks/manager_test.go core/tasks/test_helpers_test.go
git commit -m "feat: run task manager as worker pool"
```

---

### Task 4: Harden worker-pool behavior with cancellation and lease regression coverage

**Files:**
- Modify: `core/tasks/manager_test.go`
- Modify: `core/tasks/store_test.go`

**Step 1: Write the failing test**

Add focused regressions that prove existing semantics survive the worker-pool refactor:

- cancelling a running task still cancels the executor context
- a long-running task still refreshes its heartbeat under concurrent manager operation
- task claim ordering still eventually reaches the blocked head after the conflicting same-key task finishes

Example heartbeat assertion:

```go
func TestManagerHeartbeatContinuesForLongRunningTaskWithMultipleWorkers(t *testing.T) {
	// register executor that blocks long enough for two heartbeat intervals
	// claim one task and assert persisted HeartbeatAt moves forward
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./core/tasks -run 'TestManager(HeartbeatContinuesForLongRunningTaskWithMultipleWorkers|CancelRunningTaskCancelsExecutorContext)'`
Expected: If the refactor was incomplete, one of these tests will fail; otherwise this step may pass immediately after Task 3 and confirms parity.

**Step 3: Write minimal implementation**

Only adjust code if needed. Keep these invariants unchanged:

- `CancelTask()` still uses `activeCancel[taskID]`
- `heartbeatLoop()` still runs per running task
- terminal writes still happen through `MarkSucceeded`, `MarkFailed`, and `MarkCancelled`

If helper utilities are needed for concurrency-safe assertions, add them to `core/tasks/test_helpers_test.go` instead of duplicating test plumbing.

**Step 4: Run test to verify it passes**

Run: `go test ./core/tasks -run 'TestManager(HeartbeatContinuesForLongRunningTaskWithMultipleWorkers|CancelRunningTaskCancelsExecutorContext)'`
Expected: PASS.

**Step 5: Commit**

```bash
git add core/tasks/manager_test.go core/tasks/store_test.go core/tasks/test_helpers_test.go
git commit -m "test: cover worker pool lease and cancellation behavior"
```

---

### Task 5: Add the waiting state and task suspension protocol

**Files:**
- Modify: `core/tasks/types.go`
- Modify: `core/tasks/model.go`
- Modify: `core/tasks/store.go`
- Modify: `core/tasks/runtime.go`
- Modify: `core/tasks/manager.go`
- Modify: `core/tasks/store_test.go`
- Modify: `core/tasks/manager_test.go`

**Step 1: Write the failing test**

Add store coverage for `running -> waiting` and `waiting -> queued`, plus a manager test that proves an executor can suspend without being marked failed.

Suggested tests:

```go
func TestStoreMarkWaitingTransitionsRunningTaskAndClearsLease(t *testing.T) {}

func TestStoreResumeWaitingTaskRequeuesTask(t *testing.T) {}

func TestManagerExecutorCanSuspendTaskWithoutWritingTerminalStatus(t *testing.T) {}
```

The manager test should register an executor that calls `runtime.Suspend(ctx, "waiting_for_child_tasks")` and then returns `ErrTaskSuspended`. The final persisted status should be `StatusWaiting`, with no terminal `task.finished` event.

**Step 2: Run test to verify it fails**

Run: `go test ./core/tasks -run 'Test(StoreMarkWaitingTransitionsRunningTaskAndClearsLease|StoreResumeWaitingTaskRequeuesTask|ManagerExecutorCanSuspendTaskWithoutWritingTerminalStatus)'`
Expected: FAIL because `StatusWaiting`, `runtime.Suspend`, and the sentinel control flow do not exist yet.

**Step 3: Write minimal implementation**

Add state and events:

```go
const (
	StatusQueued          Status = "queued"
	StatusRunning         Status = "running"
	StatusWaiting         Status = "waiting"
	StatusCancelRequested Status = "cancel_requested"
	StatusCancelled       Status = "cancelled"
	StatusSucceeded       Status = "succeeded"
	StatusFailed          Status = "failed"
)
```

```go
const (
	EventTaskWaiting = "task.waiting"
	EventTaskResumed = "task.resumed"
)
```

Add store methods with transactional updates:

```go
func (s *Store) MarkWaiting(ctx context.Context, id string, reason string) (*Task, []TaskEvent, error)
func (s *Store) ResumeWaitingTask(ctx context.Context, id string, reason string) (*Task, []TaskEvent, error)
```

`MarkWaiting()` should:
- require current status `running`
- set `status = waiting`
- set `suspend_reason = reason`
- clear `runner_id`
- clear `lease_expires_at`
- keep `heartbeat_at` as the last observed heartbeat
- append `task.waiting`

`ResumeWaitingTask()` should:
- require current status `waiting`
- set `status = queued`
- clear `suspend_reason`
- append `task.resumed`

In `core/tasks/runtime.go` add:

```go
func (r *Runtime) Suspend(ctx context.Context, reason string) error {
	_, events, err := r.manager.store.MarkWaiting(ctx, r.taskID, reason)
	if err != nil {
		return err
	}
	r.manager.publish(events...)
	return nil
}
```

In `core/tasks/manager.go` add:

```go
var ErrTaskSuspended = errors.New("task suspended")
```

And update `executeTask()` so that `errors.Is(execErr, ErrTaskSuspended)` exits early without calling terminal store methods.

**Step 4: Run test to verify it passes**

Run: `go test ./core/tasks -run 'Test(StoreMarkWaitingTransitionsRunningTaskAndClearsLease|StoreResumeWaitingTaskRequeuesTask|ManagerExecutorCanSuspendTaskWithoutWritingTerminalStatus)'`
Expected: PASS.

**Step 5: Commit**

```bash
git add core/tasks/types.go core/tasks/model.go core/tasks/store.go core/tasks/runtime.go core/tasks/manager.go core/tasks/store_test.go core/tasks/manager_test.go
git commit -m "feat: add waiting task suspension flow"
```

---

### Task 6: Resume a waiting parent when all child tasks finish

**Files:**
- Modify: `core/tasks/store.go`
- Modify: `core/tasks/manager.go`
- Modify: `core/tasks/store_test.go`
- Modify: `core/tasks/manager_test.go`

**Step 1: Write the failing test**

Add coverage that models one waiting parent and multiple child tasks.

Suggested store tests:

```go
func TestStoreTryResumeParentTaskDoesNothingWhileChildStillActive(t *testing.T) {}

func TestStoreTryResumeParentTaskRequeuesWaitingParentWhenAllChildrenTerminal(t *testing.T) {}
```

Suggested manager test:

```go
func TestManagerChildCompletionRequeuesWaitingParent(t *testing.T) {}
```

The manager test should create a waiting parent and two children under the same `RootTaskID`; after the second child finishes, the parent should become `queued` again and then be claimable.

**Step 2: Run test to verify it fails**

Run: `go test ./core/tasks -run 'Test(StoreTryResumeParentTask|ManagerChildCompletionRequeuesWaitingParent)'`
Expected: FAIL because no parent-resume helper exists.

**Step 3: Write minimal implementation**

Add helpers in `core/tasks/store.go`:

```go
func (s *Store) CountActiveChildTasks(ctx context.Context, parentTaskID string) (int64, error)
func (s *Store) TryResumeParentTask(ctx context.Context, parentTaskID string) (*Task, []TaskEvent, error)
```

Implementation rules:

- `TryResumeParentTask()` loads the parent task.
- If the parent is not `StatusWaiting`, return without changes.
- Count children with `parent_task_id = parent.ID` and `status NOT IN (cancelled, succeeded, failed)`.
- If count is greater than zero, return without changes.
- If count is zero, call `ResumeWaitingTask()` inside the same transaction boundary or immediately after loading the parent safely.

Hook it into manager task completion:

```go
if execErr == nil && task.ParentTaskID != "" {
	_, resumeEvents, _ := m.store.TryResumeParentTask(context.Background(), task.ParentTaskID)
	m.publish(resumeEvents...)
}
```

Call the hook after all successful terminal writes, including cancel and failure paths, because parent resumption depends on child terminality, not child success.

**Step 4: Run test to verify it passes**

Run: `go test ./core/tasks -run 'Test(StoreTryResumeParentTask|ManagerChildCompletionRequeuesWaitingParent)'`
Expected: PASS.

**Step 5: Commit**

```bash
git add core/tasks/store.go core/tasks/manager.go core/tasks/store_test.go core/tasks/manager_test.go
git commit -m "feat: resume waiting parent after child fan-in"
```

---

### Task 7: Add executor-facing guidance for subagent fan-out and fan-in

**Files:**
- Modify: `core/tasks/runtime.go`
- Modify: `core/agent/task_adapter.go`
- Modify: `core/tasks/types.go`
- Modify: `core/tasks/manager_test.go`
- Modify: `core/tasks/store_test.go`

**Step 1: Write the failing test**

Add a manager-level integration-style test that simulates the intended parent/subagent flow:

1. parent task starts with `Metadata.phase = "root"`
2. executor creates three child tasks with distinct child conversation ids as `ConcurrencyKey`
3. parent suspends with `SuspendReason = "waiting_for_child_tasks"`
4. child executors succeed in parallel
5. parent is requeued and, on its second run, sees `Metadata.phase = "awaiting_children"`
6. parent aggregates child results and succeeds

This can be a single end-to-end test in `core/tasks/manager_test.go` without reaching HTTP handlers.

**Step 2: Run test to verify it fails**

Run: `go test ./core/tasks -run TestManagerSupportsParentChildFanOutAndFanIn`
Expected: FAIL until the orchestration helpers and resume flow are wired together.

**Step 3: Write minimal implementation**

Do not add a new database field for executor phase. Use `MetadataJSON` instead.

Expected task creation shape for subagent children:

```go
CreateTaskInput{
	TaskType:       "agent.run",
	RootTaskID:     parent.RootTaskID,
	ParentTaskID:   parent.ID,
	ChildIndex:     idx,
	ConcurrencyKey: childConversationID,
	Metadata: map[string]any{
		"phase": "child",
	},
}
```

Expected parent metadata transition before suspension:

```go
map[string]any{
	"phase": "awaiting_children",
}
```

If implementation needs a helper to decode or update metadata cleanly, add a small local helper in `core/tasks/store.go` or a new file under `core/tasks` rather than scattering JSON mutation logic in tests.

**Step 4: Run test to verify it passes**

Run: `go test ./core/tasks -run TestManagerSupportsParentChildFanOutAndFanIn`
Expected: PASS.

**Step 5: Commit**

```bash
git add core/tasks/runtime.go core/agent/task_adapter.go core/tasks/types.go core/tasks/store_test.go core/tasks/manager_test.go
git commit -m "feat: support parent child task fan-out and fan-in"
```

---

### Task 8: Run focused and broad verification

**Files:**
- Modify: `docs/plans/2026-03-26-task-manager-worker-pool-design.md`

**Step 1: Run focused task-manager tests**

Run: `go test ./core/tasks`
Expected: PASS.

**Step 2: Run broader backend verification**

Run: `go test ./...`
Expected: PASS.

**Step 3: Verify package graph and build**

Run: `go list ./... && go build ./cmd/...`
Expected: PASS.

**Step 4: Inspect for collateral impacts**

Check whether any task-facing handler or agent integration tests now need small expectation updates because of `StatusWaiting`, new event types, or `ConcurrencyKey` persistence.

Recommended commands:

- `go test ./app/handlers -run Task`
- `go test ./core/agent`

Expected: PASS, or clearly identified follow-up fixes if task status assumptions changed.

**Step 5: Commit**

```bash
git add docs/plans/2026-03-26-task-manager-worker-pool-design.md
git commit -m "docs: add task manager worker pool implementation plan"
```

---

## Implementation Notes For The Engineer

- Keep the worker-pool refactor and the `waiting` workflow as separate commits. They are different risk surfaces.
- Preserve the current task event ordering contract. New events should append cleanly and must not reorder existing `task.created`, `task.started`, and `task.finished` sequences.
- When updating claim logic, prefer a small candidate batch like `16` or `32`; do not attempt a complex vendor-specific locking query in the first pass.
- `StatusWaiting` is non-terminal and should be treated as an active task for UI lookups such as “latest active task by conversation” once that logic needs parent-task visibility.
- Do not infer `ConcurrencyKey` from `InputJSON` inside the store. Set it explicitly in `CreateTaskInput` from the caller boundary.
- Resume waiting parents when children are terminal, not only when children succeed.
- Keep executor phase in `MetadataJSON` and use narrow helper functions for metadata mutation if repeated JSON updates become noisy.

## Suggested Commit Sequence

1. `feat: add task concurrency key scaffolding`
2. `feat: serialize task claims by concurrency key`
3. `feat: run task manager as worker pool`
4. `test: cover worker pool lease and cancellation behavior`
5. `feat: add waiting task suspension flow`
6. `feat: resume waiting parent after child fan-in`
7. `feat: support parent child task fan-out and fan-in`
8. `docs: add task manager worker pool implementation plan`

## Rollout Advice

- Land Tasks 1-4 first and keep them production-safe without subagent usage.
- Enable subagent fan-out only after Tasks 5-7 are complete and task-manager tests are stable.
- If any race appears in SQLite tests, tighten tests around observable ordering with channels rather than adding sleeps.
