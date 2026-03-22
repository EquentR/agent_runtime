package tasks

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
)

// TestManagerExecutesQueuedTaskWithRegisteredExecutor 验证后台管理器可以领取并成功执行任务。
func TestManagerExecutesQueuedTaskWithRegisteredExecutor(t *testing.T) {
	store := newTestStore(t)
	manager := NewManager(store, ManagerOptions{
		RunnerID:          "runner-1",
		PollInterval:      5 * time.Millisecond,
		LeaseDuration:     100 * time.Millisecond,
		HeartbeatInterval: 20 * time.Millisecond,
	})
	if err := manager.RegisterExecutor("agent.run", func(ctx context.Context, task *Task, runtime *Runtime) (any, error) {
		if err := runtime.StartStep(ctx, "prepare", "Prepare response"); err != nil {
			return nil, err
		}
		if err := runtime.FinishStep(ctx, map[string]any{"ok": true}); err != nil {
			return nil, err
		}
		return map[string]any{"message": "done"}, nil
	}); err != nil {
		t.Fatalf("RegisterExecutor() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.Start(ctx)

	created, err := manager.CreateTask(ctx, CreateTaskInput{TaskType: "agent.run"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	completed := waitForTaskStatus(t, ctx, manager, created.ID, StatusSucceeded)
	if got := decodeJSONRaw(t, completed.ResultJSON)["message"]; got != "done" {
		t.Fatalf("result message = %#v, want %q", got, "done")
	}

	events, err := manager.ListEvents(ctx, created.ID, 0, 20)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) < 5 {
		t.Fatalf("event count = %d, want at least 5", len(events))
	}
	if events[0].EventType != EventTaskCreated {
		t.Fatalf("first event = %q, want %q", events[0].EventType, EventTaskCreated)
	}
	if events[len(events)-1].EventType != EventTaskFinished {
		t.Fatalf("last event = %q, want %q", events[len(events)-1].EventType, EventTaskFinished)
	}
}

// TestManagerCancelRunningTaskCancelsExecutorContext 验证取消会传播到执行器上下文。
func TestManagerCancelRunningTaskCancelsExecutorContext(t *testing.T) {
	store := newTestStore(t)
	manager := NewManager(store, ManagerOptions{
		RunnerID:          "runner-1",
		PollInterval:      5 * time.Millisecond,
		LeaseDuration:     100 * time.Millisecond,
		HeartbeatInterval: 20 * time.Millisecond,
	})

	ctxCanceled := make(chan struct{}, 1)
	if err := manager.RegisterExecutor("agent.run", func(ctx context.Context, task *Task, runtime *Runtime) (any, error) {
		if err := runtime.StartStep(ctx, "wait", "Wait for cancellation"); err != nil {
			return nil, err
		}
		<-ctx.Done()
		ctxCanceled <- struct{}{}
		return nil, ctx.Err()
	}); err != nil {
		t.Fatalf("RegisterExecutor() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.Start(ctx)

	created, err := manager.CreateTask(ctx, CreateTaskInput{TaskType: "agent.run"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	_ = waitForTaskStatus(t, ctx, manager, created.ID, StatusRunning)
	updated, err := manager.CancelTask(ctx, created.ID)
	if err != nil {
		t.Fatalf("CancelTask() error = %v", err)
	}
	if updated.Status != StatusCancelRequested {
		t.Fatalf("cancel status = %q, want %q", updated.Status, StatusCancelRequested)
	}

	select {
	case <-ctxCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("executor context was not cancelled")
	}

	final := waitForTaskStatus(t, ctx, manager, created.ID, StatusCancelled)
	if final.FinishedAt == nil {
		t.Fatal("finished_at = nil, want timestamp")
	}
}

func TestManagerCancelQueuedTaskTransitionsDirectlyToCancelled(t *testing.T) {
	store := newTestStore(t)
	manager := NewManager(store, ManagerOptions{})

	ctx := context.Background()
	created, err := manager.CreateTask(ctx, CreateTaskInput{TaskType: "agent.run"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	updated, err := manager.CancelTask(ctx, created.ID)
	if err != nil {
		t.Fatalf("CancelTask() error = %v", err)
	}
	if updated.Status != StatusCancelled {
		t.Fatalf("cancelled status = %q, want %q", updated.Status, StatusCancelled)
	}

	persisted, err := manager.GetTask(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if persisted.Status != StatusCancelled {
		t.Fatalf("persisted status = %q, want %q", persisted.Status, StatusCancelled)
	}

	events, err := manager.ListEvents(ctx, created.ID, 0, 10)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("event count = %d, want 3", len(events))
	}
	if events[1].EventType != EventTaskCancelRequested {
		t.Fatalf("second event = %q, want %q", events[1].EventType, EventTaskCancelRequested)
	}
	if events[2].EventType != EventTaskFinished {
		t.Fatalf("last event = %q, want %q", events[2].EventType, EventTaskFinished)
	}
}

func TestManagerCancelTaskDoesNotUseStaleQueuedSnapshotForRunningTask(t *testing.T) {
	store := newTestStore(t)
	manager := NewManager(store, ManagerOptions{})

	ctxKey := struct{}{}
	ctx := context.WithValue(context.Background(), ctxKey, true)
	created, err := manager.CreateTask(ctx, CreateTaskInput{TaskType: "agent.run"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	callbackName := "test:tasks:cancel_stale_queued_snapshot"
	injected := false
	if err := store.db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if injected || tx.Statement == nil || tx.Statement.Schema == nil || tx.Statement.Schema.Table != "tasks" {
			return
		}
		if tx.Statement.Context == nil || tx.Statement.Context.Value(ctxKey) == nil {
			return
		}
		task, ok := tx.Statement.Dest.(*Task)
		if !ok || task == nil || task.ID != created.ID {
			return
		}
		injected = true
		now := time.Now().UTC()
		leaseExpiry := now.Add(time.Minute)
		updateErr := store.db.WithContext(context.Background()).Model(&Task{}).Where("id = ?", created.ID).Updates(map[string]any{
			"status":           StatusRunning,
			"runner_id":        "runner-race",
			"started_at":       now,
			"heartbeat_at":     now,
			"lease_expires_at": leaseExpiry,
		}).Error
		if updateErr != nil {
			tx.AddError(updateErr)
		}
	}); err != nil {
		t.Fatalf("register callback error = %v", err)
	}
	defer func() {
		if err := store.db.Callback().Query().Remove(callbackName); err != nil {
			t.Fatalf("remove callback error = %v", err)
		}
	}()

	updated, err := manager.CancelTask(ctx, created.ID)
	if err != nil {
		t.Fatalf("CancelTask() error = %v", err)
	}
	if !injected {
		t.Fatal("stale snapshot race was not injected")
	}
	if updated.Status != StatusCancelRequested {
		t.Fatalf("updated status = %q, want %q", updated.Status, StatusCancelRequested)
	}

	persisted, err := manager.GetTask(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if persisted.Status != StatusCancelRequested {
		t.Fatalf("persisted status = %q, want %q", persisted.Status, StatusCancelRequested)
	}
	if persisted.FinishedAt != nil {
		t.Fatalf("finished_at = %v, want nil", persisted.FinishedAt)
	}
	if persisted.RunnerID != "runner-race" {
		t.Fatalf("runner_id = %q, want %q", persisted.RunnerID, "runner-race")
	}

	events, err := manager.ListEvents(context.Background(), created.ID, 0, 10)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("event count = %d, want 2", len(events))
	}
	if events[1].EventType != EventTaskCancelRequested {
		t.Fatalf("last event = %q, want %q", events[1].EventType, EventTaskCancelRequested)
	}
}

func TestManagerCancelTaskDoesNotOverwriteTaskFinishedWhileCancelInFlight(t *testing.T) {
	store := newTestStore(t)
	manager := NewManager(store, ManagerOptions{})

	created, err := manager.CreateTask(context.Background(), CreateTaskInput{TaskType: "agent.run"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if _, _, err := store.ClaimNextTask(context.Background(), "runner-race", time.Minute); err != nil {
		t.Fatalf("ClaimNextTask() error = %v", err)
	}

	ctxKey := struct{}{}
	ctx := context.WithValue(context.Background(), ctxKey, true)
	callbackName := "test:tasks:cancel_inflight_finish_race"
	injected := false
	if err := store.db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if injected || tx.Statement == nil || tx.Statement.Schema == nil || tx.Statement.Schema.Table != "tasks" {
			return
		}
		if tx.Statement.Context == nil || tx.Statement.Context.Value(ctxKey) == nil {
			return
		}
		task, ok := tx.Statement.Dest.(*Task)
		if !ok || task == nil || task.ID != created.ID || task.Status != StatusRunning {
			return
		}
		injected = true
		if _, _, err := store.MarkSucceeded(context.Background(), created.ID, map[string]any{"message": "done"}); err != nil {
			tx.AddError(err)
		}
	}); err != nil {
		t.Fatalf("register callback error = %v", err)
	}
	defer func() {
		if err := store.db.Callback().Query().Remove(callbackName); err != nil {
			t.Fatalf("remove callback error = %v", err)
		}
	}()

	updated, err := manager.CancelTask(ctx, created.ID)
	if err != nil {
		t.Fatalf("CancelTask() error = %v", err)
	}
	if !injected {
		t.Fatal("finish race was not injected")
	}
	if updated.Status != StatusSucceeded {
		t.Fatalf("updated status = %q, want %q", updated.Status, StatusSucceeded)
	}

	persisted, err := manager.GetTask(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if persisted.Status != StatusSucceeded {
		t.Fatalf("persisted status = %q, want %q", persisted.Status, StatusSucceeded)
	}
	if persisted.FinishedAt == nil {
		t.Fatal("finished_at = nil, want timestamp")
	}

	events, err := manager.ListEvents(context.Background(), created.ID, 0, 10)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("event count = %d, want 3", len(events))
	}
	if events[2].EventType != EventTaskFinished {
		t.Fatalf("last event = %q, want %q", events[2].EventType, EventTaskFinished)
	}
	for _, event := range events {
		if event.EventType == EventTaskCancelRequested {
			t.Fatalf("unexpected event type %q after late cancel", event.EventType)
		}
	}
}

// TestManagerSubscribeReceivesLiveEvents 验证实时订阅可以收到任务事件。
func TestManagerSubscribeReceivesLiveEvents(t *testing.T) {
	store := newTestStore(t)
	manager := NewManager(store, ManagerOptions{
		RunnerID:          "runner-1",
		PollInterval:      5 * time.Millisecond,
		LeaseDuration:     100 * time.Millisecond,
		HeartbeatInterval: 20 * time.Millisecond,
	})
	if err := manager.RegisterExecutor("agent.run", func(ctx context.Context, task *Task, runtime *Runtime) (any, error) {
		if err := runtime.StartStep(ctx, "prepare", "Prepare response"); err != nil {
			return nil, err
		}
		return map[string]any{"message": "done"}, nil
	}); err != nil {
		t.Fatalf("RegisterExecutor() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	created, err := manager.CreateTask(ctx, CreateTaskInput{TaskType: "agent.run"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	ch, unsubscribe := manager.Subscribe(created.ID)
	defer unsubscribe()

	manager.Start(ctx)

	event := waitForEvent(t, ch, EventTaskStarted, EventStepStarted, EventTaskFinished)
	if event.TaskID != created.ID {
		t.Fatalf("event task id = %q, want %q", event.TaskID, created.ID)
	}
}

// TestManagerReturnsErrorForDuplicateExecutorRegistration 验证重复注册执行器会返回错误。
func TestManagerReturnsErrorForDuplicateExecutorRegistration(t *testing.T) {
	store := newTestStore(t)
	manager := NewManager(store, ManagerOptions{})

	if err := manager.RegisterExecutor("agent.run", func(context.Context, *Task, *Runtime) (any, error) {
		return nil, nil
	}); err != nil {
		t.Fatalf("first RegisterExecutor() error = %v", err)
	}

	err := manager.RegisterExecutor("agent.run", func(context.Context, *Task, *Runtime) (any, error) {
		return nil, errors.New("should not register")
	})
	if err == nil {
		t.Fatal("second RegisterExecutor() error = nil, want non-nil")
	}
}

func TestManagerPublishesAuditRunLifecycleOnSuccess(t *testing.T) {
	store := newTestStore(t)
	recorder := newRecordingAuditRecorder()
	manager := NewManager(store, ManagerOptions{
		RunnerID:          "runner-1",
		PollInterval:      5 * time.Millisecond,
		LeaseDuration:     100 * time.Millisecond,
		HeartbeatInterval: 20 * time.Millisecond,
		AuditRecorder:     recorder,
	})
	if err := manager.RegisterExecutor("agent.run", func(context.Context, *Task, *Runtime) (any, error) {
		return map[string]any{"ok": true}, nil
	}); err != nil {
		t.Fatalf("RegisterExecutor() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	created, err := manager.CreateTask(ctx, CreateTaskInput{TaskType: "agent.run", CreatedBy: "user-1"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if !recorder.hasRunForTask(created.ID) {
		t.Fatalf("task %q has no audit run", created.ID)
	}

	manager.Start(ctx)
	waitForAuditEvent(t, recorder, created.ID, "run.succeeded")

	assertAuditStartStatuses(t, recorder, created.ID, StatusQueued, StatusRunning)
	assertAuditEventTypes(t, recorder, created.ID, "run.created", "run.started", "run.succeeded")
	assertAuditFinishedStatus(t, recorder, created.ID, StatusSucceeded)
	if !recorder.hasStartedRun(created.ID) {
		t.Fatalf("task %q has no started audit run", created.ID)
	}
}

func TestManagerPublishesAuditRunLifecycleOnFailure(t *testing.T) {
	store := newTestStore(t)
	recorder := newRecordingAuditRecorder()
	manager := NewManager(store, ManagerOptions{
		RunnerID:          "runner-1",
		PollInterval:      5 * time.Millisecond,
		LeaseDuration:     100 * time.Millisecond,
		HeartbeatInterval: 20 * time.Millisecond,
		AuditRecorder:     recorder,
	})
	if err := manager.RegisterExecutor("agent.run", func(context.Context, *Task, *Runtime) (any, error) {
		return nil, errors.New("boom")
	}); err != nil {
		t.Fatalf("RegisterExecutor() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.Start(ctx)

	created, err := manager.CreateTask(ctx, CreateTaskInput{TaskType: "agent.run"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	waitForAuditEvent(t, recorder, created.ID, "run.failed")
	assertAuditEventTypes(t, recorder, created.ID, "run.created", "run.started", "run.failed")
	assertAuditFinishedStatus(t, recorder, created.ID, StatusFailed)
}

func TestManagerPublishesAuditRunLifecycleOnCancellation(t *testing.T) {
	store := newTestStore(t)
	recorder := newRecordingAuditRecorder()
	manager := NewManager(store, ManagerOptions{
		RunnerID:          "runner-1",
		PollInterval:      5 * time.Millisecond,
		LeaseDuration:     100 * time.Millisecond,
		HeartbeatInterval: 20 * time.Millisecond,
		AuditRecorder:     recorder,
	})

	release := make(chan struct{})
	if err := manager.RegisterExecutor("agent.run", func(ctx context.Context, task *Task, runtime *Runtime) (any, error) {
		<-ctx.Done()
		<-release
		return nil, ctx.Err()
	}); err != nil {
		t.Fatalf("RegisterExecutor() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.Start(ctx)

	created, err := manager.CreateTask(ctx, CreateTaskInput{TaskType: "agent.run"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	_ = waitForTaskStatus(t, ctx, manager, created.ID, StatusRunning)
	if _, err := manager.CancelTask(ctx, created.ID); err != nil {
		t.Fatalf("CancelTask() error = %v", err)
	}
	close(release)

	waitForAuditEvent(t, recorder, created.ID, "run.cancelled")
	assertAuditEventTypes(t, recorder, created.ID, "run.created", "run.started", "run.cancelled")
	assertAuditFinishedStatus(t, recorder, created.ID, StatusCancelled)
}

func TestManagerRetryTaskReservesAuditRun(t *testing.T) {
	store := newTestStore(t)
	recorder := newRecordingAuditRecorder()
	manager := NewManager(store, ManagerOptions{AuditRecorder: recorder})

	original, err := manager.CreateTask(context.Background(), CreateTaskInput{TaskType: "agent.run"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if !recorder.hasRunForTask(original.ID) {
		t.Fatalf("original task %q has no audit run", original.ID)
	}

	retried, err := manager.RetryTask(context.Background(), original.ID)
	if err != nil {
		t.Fatalf("RetryTask() error = %v", err)
	}
	if !recorder.hasRunForTask(retried.ID) {
		t.Fatalf("retried task %q has no audit run", retried.ID)
	}
	assertAuditEventTypes(t, recorder, retried.ID, "run.created")
}

type recordingAuditRecorder struct {
	mu             sync.Mutex
	runsByTaskID   map[string]*AuditRun
	startInputs    map[string][]AuditStartRunInput
	eventsByTaskID map[string][]recordedAuditEvent
	finishes       map[string]AuditFinishRunInput
}

type recordedAuditEvent struct {
	RunID string
	AuditAppendEventInput
}

func newRecordingAuditRecorder() *recordingAuditRecorder {
	return &recordingAuditRecorder{
		runsByTaskID:   make(map[string]*AuditRun),
		startInputs:    make(map[string][]AuditStartRunInput),
		eventsByTaskID: make(map[string][]recordedAuditEvent),
		finishes:       make(map[string]AuditFinishRunInput),
	}
}

func (r *recordingAuditRecorder) StartRun(_ context.Context, input AuditStartRunInput) (*AuditRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	run := r.runsByTaskID[input.TaskID]
	if run == nil {
		run = &AuditRun{ID: "run_for_" + input.TaskID, TaskID: input.TaskID}
		r.runsByTaskID[input.TaskID] = run
	}
	r.startInputs[input.TaskID] = append(r.startInputs[input.TaskID], input)
	return &AuditRun{ID: run.ID, TaskID: run.TaskID}, nil
}

func (r *recordingAuditRecorder) AppendEvent(_ context.Context, runID string, input AuditAppendEventInput) (*AuditEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	taskID := taskIDFromRunID(runID)
	r.eventsByTaskID[taskID] = append(r.eventsByTaskID[taskID], recordedAuditEvent{RunID: runID, AuditAppendEventInput: input})
	return &AuditEvent{RunID: runID, EventType: input.EventType}, nil
}

func (r *recordingAuditRecorder) FinishRun(_ context.Context, runID string, input AuditFinishRunInput) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.finishes[taskIDFromRunID(runID)] = input
	return nil
}

func (r *recordingAuditRecorder) hasRunForTask(taskID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.runsByTaskID[taskID]
	return ok
}

func (r *recordingAuditRecorder) hasStartedRun(taskID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, input := range r.startInputs[taskID] {
		if input.Status == StatusRunning && !input.StartedAt.IsZero() {
			return true
		}
	}
	return false
}

func (r *recordingAuditRecorder) eventTypes(taskID string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	events := r.eventsByTaskID[taskID]
	result := make([]string, 0, len(events))
	for _, event := range events {
		result = append(result, event.EventType)
	}
	return result
}

func (r *recordingAuditRecorder) startStatuses(taskID string) []Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	inputs := r.startInputs[taskID]
	result := make([]Status, 0, len(inputs))
	for _, input := range inputs {
		result = append(result, input.Status)
	}
	return result
}

func (r *recordingAuditRecorder) finishedStatus(taskID string) (Status, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	input, ok := r.finishes[taskID]
	return input.Status, ok
}

func waitForAuditEvent(t *testing.T, recorder *recordingAuditRecorder, taskID string, want string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, eventType := range recorder.eventTypes(taskID) {
			if eventType == want {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("task %s did not record audit event %q", taskID, want)
}

func assertAuditEventTypes(t *testing.T, recorder *recordingAuditRecorder, taskID string, want ...string) {
	t.Helper()
	got := recorder.eventTypes(taskID)
	if len(got) != len(want) {
		t.Fatalf("audit event count = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("audit events = %v, want %v", got, want)
		}
	}
}

func assertAuditStartStatuses(t *testing.T, recorder *recordingAuditRecorder, taskID string, want ...Status) {
	t.Helper()
	got := recorder.startStatuses(taskID)
	if len(got) < len(want) {
		t.Fatalf("audit start statuses = %v, want at least %v", got, want)
	}
	for _, status := range want {
		found := false
		for _, candidate := range got {
			if candidate == status {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("audit start statuses = %v, missing %q", got, status)
		}
	}
}

func assertAuditFinishedStatus(t *testing.T, recorder *recordingAuditRecorder, taskID string, want Status) {
	t.Helper()
	got, ok := recorder.finishedStatus(taskID)
	if !ok {
		t.Fatalf("task %s has no audit finish", taskID)
	}
	if got != want {
		t.Fatalf("audit finish status = %q, want %q", got, want)
	}
}

func taskIDFromRunID(runID string) string {
	const prefix = "run_for_"
	if len(runID) > len(prefix) && runID[:len(prefix)] == prefix {
		return runID[len(prefix):]
	}
	return runID
}
