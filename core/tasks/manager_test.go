package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func TestManagerCancelRunningTaskCancelsExecutorContextWithMultipleWorkers(t *testing.T) {
	store := newTestStore(t)
	manager := NewManager(store, ManagerOptions{
		RunnerID:          "runner-1",
		WorkerCount:       2,
		PollInterval:      5 * time.Millisecond,
		LeaseDuration:     120 * time.Millisecond,
		HeartbeatInterval: 25 * time.Millisecond,
	})

	targetStarted := make(chan struct{}, 1)
	otherStarted := make(chan struct{}, 1)
	targetCanceled := make(chan struct{}, 1)
	releaseOther := make(chan struct{})
	if err := manager.RegisterExecutor("agent.run", func(ctx context.Context, task *Task, runtime *Runtime) (any, error) {
		switch task.ConcurrencyKey {
		case "conv_target":
			targetStarted <- struct{}{}
			<-ctx.Done()
			targetCanceled <- struct{}{}
			return nil, ctx.Err()
		case "conv_other":
			otherStarted <- struct{}{}
			select {
			case <-releaseOther:
				return map[string]any{"message": "done"}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		default:
			return nil, errors.New("unexpected concurrency key")
		}
	}); err != nil {
		t.Fatalf("RegisterExecutor() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.Start(ctx)

	targetTask, err := manager.CreateTask(ctx, CreateTaskInput{TaskType: "agent.run", ConcurrencyKey: "conv_target"})
	if err != nil {
		t.Fatalf("CreateTask() target error = %v", err)
	}
	otherTask, err := manager.CreateTask(ctx, CreateTaskInput{TaskType: "agent.run", ConcurrencyKey: "conv_other"})
	if err != nil {
		t.Fatalf("CreateTask() other error = %v", err)
	}

	select {
	case <-targetStarted:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("timed out waiting for target task to start")
	}
	select {
	case <-otherStarted:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("timed out waiting for concurrent task to start")
	}

	updated, err := manager.CancelTask(ctx, targetTask.ID)
	if err != nil {
		t.Fatalf("CancelTask() error = %v", err)
	}
	if updated.Status != StatusCancelRequested {
		t.Fatalf("cancel status = %q, want %q", updated.Status, StatusCancelRequested)
	}

	select {
	case <-targetCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("executor context was not cancelled for running task")
	}

	close(releaseOther)

	targetFinal := waitForTaskStatus(t, ctx, manager, targetTask.ID, StatusCancelled)
	if targetFinal.FinishedAt == nil {
		t.Fatal("target finished_at = nil, want timestamp")
	}
	_ = waitForTaskStatus(t, ctx, manager, otherTask.ID, StatusSucceeded)
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

func TestManagerCancelWaitingTaskTransitionsDirectlyToCancelled(t *testing.T) {
	store := newTestStore(t)
	manager := NewManager(store, ManagerOptions{
		RunnerID:          "runner-1",
		PollInterval:      5 * time.Millisecond,
		LeaseDuration:     100 * time.Millisecond,
		HeartbeatInterval: 20 * time.Millisecond,
	})
	if err := manager.RegisterExecutor("agent.run", func(ctx context.Context, task *Task, runtime *Runtime) (any, error) {
		if err := runtime.Suspend(ctx, "waiting_for_child_tasks"); err != nil {
			return nil, err
		}
		return nil, ErrTaskSuspended
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

	waiting := waitForTaskStatus(t, ctx, manager, created.ID, StatusWaiting)
	if waiting.RunnerID != "" {
		t.Fatalf("waiting runner_id = %q, want empty", waiting.RunnerID)
	}

	updated, err := manager.CancelTask(ctx, created.ID)
	if err != nil {
		t.Fatalf("CancelTask() error = %v", err)
	}
	if updated.Status != StatusCancelled {
		t.Fatalf("updated status = %q, want %q", updated.Status, StatusCancelled)
	}
	if updated.FinishedAt == nil {
		t.Fatal("updated finished_at = nil, want timestamp")
	}

	persisted, err := manager.GetTask(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if persisted.Status != StatusCancelled {
		t.Fatalf("persisted status = %q, want %q", persisted.Status, StatusCancelled)
	}
	if persisted.FinishedAt == nil {
		t.Fatal("persisted finished_at = nil, want timestamp")
	}

	events, err := manager.ListEvents(ctx, created.ID, 0, 10)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != 5 {
		t.Fatalf("event count = %d, want 5", len(events))
	}
	if events[2].EventType != EventTaskWaiting {
		t.Fatalf("third event = %q, want %q", events[2].EventType, EventTaskWaiting)
	}
	if events[3].EventType != EventTaskCancelRequested {
		t.Fatalf("fourth event = %q, want %q", events[3].EventType, EventTaskCancelRequested)
	}
	if events[4].EventType != EventTaskFinished {
		t.Fatalf("fifth event = %q, want %q", events[4].EventType, EventTaskFinished)
	}
}

func TestManagerCancelDuringSuspendWindowPrefersExecutorCancellation(t *testing.T) {
	store := newTestStore(t)
	manager := NewManager(store, ManagerOptions{
		RunnerID:          "runner-1",
		PollInterval:      5 * time.Millisecond,
		LeaseDuration:     100 * time.Millisecond,
		HeartbeatInterval: 20 * time.Millisecond,
	})

	suspendReturned := make(chan struct{}, 1)
	executorCancelled := make(chan struct{}, 1)
	if err := manager.RegisterExecutor("agent.run", func(ctx context.Context, task *Task, runtime *Runtime) (any, error) {
		if err := runtime.Suspend(ctx, "waiting_for_child_tasks"); err != nil {
			return nil, err
		}
		suspendReturned <- struct{}{}
		<-ctx.Done()
		executorCancelled <- struct{}{}
		return nil, ErrTaskSuspended
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

	select {
	case <-suspendReturned:
	case <-time.After(2 * time.Second):
		t.Fatal("executor did not reach suspend window")
	}

	updated, err := manager.CancelTask(ctx, created.ID)
	if err != nil {
		t.Fatalf("CancelTask() error = %v", err)
	}
	if updated.Status != StatusCancelRequested {
		t.Fatalf("updated status = %q, want %q", updated.Status, StatusCancelRequested)
	}

	select {
	case <-executorCancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("executor context was not cancelled during suspend window")
	}

	final := waitForTaskStatus(t, ctx, manager, created.ID, StatusCancelled)
	if final.FinishedAt == nil {
		t.Fatal("final finished_at = nil, want timestamp")
	}

	events, err := manager.ListEvents(ctx, created.ID, 0, 10)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != 5 {
		t.Fatalf("event count = %d, want 5", len(events))
	}
	if events[2].EventType != EventTaskWaiting {
		t.Fatalf("third event = %q, want %q", events[2].EventType, EventTaskWaiting)
	}
	if events[3].EventType != EventTaskCancelRequested {
		t.Fatalf("fourth event = %q, want %q", events[3].EventType, EventTaskCancelRequested)
	}
	if events[4].EventType != EventTaskFinished {
		t.Fatalf("fifth event = %q, want %q", events[4].EventType, EventTaskFinished)
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

func TestManagerExecutesDifferentConcurrencyKeysInParallel(t *testing.T) {
	store := newTestStore(t)
	manager := NewManager(store, ManagerOptions{
		RunnerID:          "runner-1",
		WorkerCount:       2,
		PollInterval:      5 * time.Millisecond,
		LeaseDuration:     100 * time.Millisecond,
		HeartbeatInterval: 20 * time.Millisecond,
	})

	var mu sync.Mutex
	running := 0
	maxRunning := 0
	started := make(chan string, 2)
	release := make(chan struct{})
	if err := manager.RegisterExecutor("agent.run", func(ctx context.Context, task *Task, runtime *Runtime) (any, error) {
		mu.Lock()
		running++
		if running > maxRunning {
			maxRunning = running
		}
		mu.Unlock()

		defer func() {
			mu.Lock()
			running--
			mu.Unlock()
		}()

		started <- task.ConcurrencyKey
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}

		return map[string]any{"key": task.ConcurrencyKey}, nil
	}); err != nil {
		t.Fatalf("RegisterExecutor() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.Start(ctx)

	firstTask, err := manager.CreateTask(ctx, CreateTaskInput{TaskType: "agent.run", ConcurrencyKey: "conv_1"})
	if err != nil {
		t.Fatalf("CreateTask() first error = %v", err)
	}
	secondTask, err := manager.CreateTask(ctx, CreateTaskInput{TaskType: "agent.run", ConcurrencyKey: "conv_2"})
	if err != nil {
		t.Fatalf("CreateTask() second error = %v", err)
	}

	receiveStartedKey := func(label string) string {
		t.Helper()
		select {
		case key := <-started:
			return key
		case <-time.After(300 * time.Millisecond):
			t.Fatalf("timed out waiting for %s task to start", label)
			return ""
		}
	}

	firstKey := receiveStartedKey("first")
	secondKey := receiveStartedKey("second")
	if firstKey == secondKey {
		t.Fatalf("started keys = %q and %q, want different keys", firstKey, secondKey)
	}

	close(release)
	_ = waitForTaskStatus(t, ctx, manager, firstTask.ID, StatusSucceeded)
	_ = waitForTaskStatus(t, ctx, manager, secondTask.ID, StatusSucceeded)

	mu.Lock()
	gotMaxRunning := maxRunning
	mu.Unlock()
	if gotMaxRunning < 2 {
		t.Fatalf("max running executors = %d, want at least 2", gotMaxRunning)
	}
}

func TestManagerExecutesSameConcurrencyKeySeriallyEvenWithMultipleWorkers(t *testing.T) {
	store := newTestStore(t)
	manager := NewManager(store, ManagerOptions{
		RunnerID:          "runner-1",
		WorkerCount:       2,
		PollInterval:      5 * time.Millisecond,
		LeaseDuration:     100 * time.Millisecond,
		HeartbeatInterval: 20 * time.Millisecond,
	})

	var mu sync.Mutex
	invocations := 0
	maxSameKeyRunning := 0
	sameKeyRunning := 0
	firstStarted := make(chan struct{}, 1)
	secondStarted := make(chan struct{}, 1)
	releaseFirst := make(chan struct{})
	if err := manager.RegisterExecutor("agent.run", func(ctx context.Context, task *Task, runtime *Runtime) (any, error) {
		mu.Lock()
		invocations++
		callIndex := invocations
		sameKeyRunning++
		if sameKeyRunning > maxSameKeyRunning {
			maxSameKeyRunning = sameKeyRunning
		}
		mu.Unlock()

		defer func() {
			mu.Lock()
			sameKeyRunning--
			mu.Unlock()
		}()

		if callIndex == 1 {
			firstStarted <- struct{}{}
			select {
			case <-releaseFirst:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		} else {
			secondStarted <- struct{}{}
		}

		return map[string]any{"call": callIndex}, nil
	}); err != nil {
		t.Fatalf("RegisterExecutor() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.Start(ctx)

	firstTask, err := manager.CreateTask(ctx, CreateTaskInput{TaskType: "agent.run", ConcurrencyKey: "conv_1"})
	if err != nil {
		t.Fatalf("CreateTask() first error = %v", err)
	}
	secondTask, err := manager.CreateTask(ctx, CreateTaskInput{TaskType: "agent.run", ConcurrencyKey: "conv_1"})
	if err != nil {
		t.Fatalf("CreateTask() second error = %v", err)
	}

	select {
	case <-firstStarted:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("timed out waiting for first same-key task to start")
	}

	select {
	case <-secondStarted:
		t.Fatal("second same-key task started before first task finished")
	case <-time.After(150 * time.Millisecond):
	}

	close(releaseFirst)

	select {
	case <-secondStarted:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("timed out waiting for second same-key task to start after first finished")
	}

	_ = waitForTaskStatus(t, ctx, manager, firstTask.ID, StatusSucceeded)
	_ = waitForTaskStatus(t, ctx, manager, secondTask.ID, StatusSucceeded)

	mu.Lock()
	gotMaxSameKeyRunning := maxSameKeyRunning
	mu.Unlock()
	if gotMaxSameKeyRunning != 1 {
		t.Fatalf("max same-key running = %d, want 1", gotMaxSameKeyRunning)
	}
}

func TestManagerHeartbeatContinuesForLongRunningTaskWithMultipleWorkers(t *testing.T) {
	store := newTestStore(t)
	manager := NewManager(store, ManagerOptions{
		RunnerID:          "runner-1",
		WorkerCount:       2,
		PollInterval:      5 * time.Millisecond,
		LeaseDuration:     120 * time.Millisecond,
		HeartbeatInterval: 25 * time.Millisecond,
	})

	longStarted := make(chan struct{}, 1)
	otherStarted := make(chan struct{}, 1)
	releaseLong := make(chan struct{})
	releaseOther := make(chan struct{})
	if err := manager.RegisterExecutor("agent.run", func(ctx context.Context, task *Task, runtime *Runtime) (any, error) {
		switch task.ConcurrencyKey {
		case "conv_long":
			longStarted <- struct{}{}
			select {
			case <-releaseLong:
				return map[string]any{"message": "long done"}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		case "conv_other":
			otherStarted <- struct{}{}
			select {
			case <-releaseOther:
				return map[string]any{"message": "other done"}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		default:
			return nil, errors.New("unexpected concurrency key")
		}
	}); err != nil {
		t.Fatalf("RegisterExecutor() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.Start(ctx)

	longTask, err := manager.CreateTask(ctx, CreateTaskInput{TaskType: "agent.run", ConcurrencyKey: "conv_long"})
	if err != nil {
		t.Fatalf("CreateTask() long error = %v", err)
	}
	otherTask, err := manager.CreateTask(ctx, CreateTaskInput{TaskType: "agent.run", ConcurrencyKey: "conv_other"})
	if err != nil {
		t.Fatalf("CreateTask() other error = %v", err)
	}

	select {
	case <-longStarted:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("timed out waiting for long-running task to start")
	}
	select {
	case <-otherStarted:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("timed out waiting for concurrent task to start")
	}

	running := waitForTaskStatus(t, ctx, manager, longTask.ID, StatusRunning)
	initialHeartbeat := mustParseTime(t, running.HeartbeatAt)

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		updated, err := manager.GetTask(ctx, longTask.ID)
		if err == nil && updated.HeartbeatAt != nil && updated.HeartbeatAt.After(initialHeartbeat) {
			close(releaseLong)
			close(releaseOther)
			_ = waitForTaskStatus(t, ctx, manager, longTask.ID, StatusSucceeded)
			_ = waitForTaskStatus(t, ctx, manager, otherTask.ID, StatusSucceeded)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	close(releaseLong)
	close(releaseOther)
	t.Fatal("heartbeat did not advance for long-running task")
}

func TestManagerExecutorCanSuspendTaskWithoutWritingTerminalStatus(t *testing.T) {
	store := newTestStore(t)
	manager := NewManager(store, ManagerOptions{
		RunnerID:          "runner-1",
		PollInterval:      5 * time.Millisecond,
		LeaseDuration:     100 * time.Millisecond,
		HeartbeatInterval: 20 * time.Millisecond,
	})
	if err := manager.RegisterExecutor("agent.run", func(ctx context.Context, task *Task, runtime *Runtime) (any, error) {
		if err := runtime.Suspend(ctx, "waiting_for_child_tasks"); err != nil {
			return nil, err
		}
		return nil, ErrTaskSuspended
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

	_ = waitForTaskStatus(t, ctx, manager, created.ID, StatusWaiting)
	time.Sleep(50 * time.Millisecond)

	persisted, err := manager.GetTask(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if persisted.Status != StatusWaiting {
		t.Fatalf("persisted status = %q, want %q", persisted.Status, StatusWaiting)
	}
	if persisted.SuspendReason != "waiting_for_child_tasks" {
		t.Fatalf("suspend reason = %q, want %q", persisted.SuspendReason, "waiting_for_child_tasks")
	}

	events, err := manager.ListEvents(ctx, created.ID, 0, 10)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("event count = %d, want 3", len(events))
	}
	if events[2].EventType != EventTaskWaiting {
		t.Fatalf("last event = %q, want %q", events[2].EventType, EventTaskWaiting)
	}
	for _, event := range events {
		if event.EventType == EventTaskFinished {
			t.Fatalf("unexpected terminal event %q after suspension", event.EventType)
		}
	}
}

func TestManagerExecutorCanSuspendTaskWithoutExplicitSuspendedError(t *testing.T) {
	store := newTestStore(t)
	manager := NewManager(store, ManagerOptions{
		RunnerID:          "runner-1",
		PollInterval:      5 * time.Millisecond,
		LeaseDuration:     100 * time.Millisecond,
		HeartbeatInterval: 20 * time.Millisecond,
	})
	if err := manager.RegisterExecutor("agent.run", func(ctx context.Context, task *Task, runtime *Runtime) (any, error) {
		if err := runtime.Suspend(ctx, "waiting_for_child_tasks"); err != nil {
			return nil, err
		}
		return map[string]any{"message": "should_not_terminalize"}, nil
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

	_ = waitForTaskStatus(t, ctx, manager, created.ID, StatusWaiting)
	time.Sleep(50 * time.Millisecond)

	persisted, err := manager.GetTask(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if persisted.Status != StatusWaiting {
		t.Fatalf("persisted status = %q, want %q", persisted.Status, StatusWaiting)
	}
	if persisted.SuspendReason != "waiting_for_child_tasks" {
		t.Fatalf("suspend reason = %q, want %q", persisted.SuspendReason, "waiting_for_child_tasks")
	}
	if persisted.FinishedAt != nil {
		t.Fatalf("finished_at = %v, want nil", persisted.FinishedAt)
	}

	events, err := manager.ListEvents(ctx, created.ID, 0, 10)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("event count = %d, want 3", len(events))
	}
	if events[2].EventType != EventTaskWaiting {
		t.Fatalf("last event = %q, want %q", events[2].EventType, EventTaskWaiting)
	}
	for _, event := range events {
		if event.EventType == EventTaskFinished {
			t.Fatalf("unexpected terminal event %q after suspension", event.EventType)
		}
	}
}

func TestManagerChildCompletionRequeuesWaitingParent(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	manager := NewManager(store, ManagerOptions{
		RunnerID:          "runner-1",
		PollInterval:      5 * time.Millisecond,
		LeaseDuration:     100 * time.Millisecond,
		HeartbeatInterval: 20 * time.Millisecond,
	})
	if err := manager.RegisterExecutor("child.run", func(ctx context.Context, task *Task, runtime *Runtime) (any, error) {
		switch task.ChildIndex {
		case 0:
			return map[string]any{"message": "done"}, nil
		case 1:
			return nil, errors.New("child failed")
		default:
			return nil, errors.New("unexpected child index")
		}
	}); err != nil {
		t.Fatalf("RegisterExecutor() error = %v", err)
	}

	parent, _, err := store.CreateTask(ctx, CreateTaskInput{TaskType: "parent.run"})
	if err != nil {
		t.Fatalf("CreateTask() parent error = %v", err)
	}
	if _, _, err := store.ClaimNextTask(ctx, "runner-parent", time.Minute); err != nil {
		t.Fatalf("ClaimNextTask() parent error = %v", err)
	}
	if _, _, err := store.MarkWaiting(ctx, parent.ID, "waiting_for_child_tasks"); err != nil {
		t.Fatalf("MarkWaiting() parent error = %v", err)
	}

	if _, _, err := store.CreateTask(ctx, CreateTaskInput{
		TaskType:     "child.run",
		RootTaskID:   parent.RootTaskID,
		ParentTaskID: parent.ID,
		ChildIndex:   0,
	}); err != nil {
		t.Fatalf("CreateTask() first child error = %v", err)
	}
	if _, _, err := store.CreateTask(ctx, CreateTaskInput{
		TaskType:     "child.run",
		RootTaskID:   parent.RootTaskID,
		ParentTaskID: parent.ID,
		ChildIndex:   1,
	}); err != nil {
		t.Fatalf("CreateTask() second child error = %v", err)
	}

	firstChild, _, err := store.ClaimNextTask(ctx, "runner-1", time.Minute)
	if err != nil {
		t.Fatalf("ClaimNextTask() first child error = %v", err)
	}
	if firstChild == nil || firstChild.ParentTaskID != parent.ID {
		t.Fatalf("first child = %#v, want child under parent %q", firstChild, parent.ID)
	}
	manager.executeTask(ctx, firstChild)

	persistedParent, err := store.GetTask(ctx, parent.ID)
	if err != nil {
		t.Fatalf("GetTask() parent after first child error = %v", err)
	}
	if persistedParent.Status != StatusWaiting {
		t.Fatalf("parent status after first child = %q, want %q", persistedParent.Status, StatusWaiting)
	}

	secondChild, _, err := store.ClaimNextTask(ctx, "runner-1", time.Minute)
	if err != nil {
		t.Fatalf("ClaimNextTask() second child error = %v", err)
	}
	if secondChild == nil || secondChild.ParentTaskID != parent.ID {
		t.Fatalf("second child = %#v, want child under parent %q", secondChild, parent.ID)
	}
	manager.executeTask(ctx, secondChild)

	resumedParent, err := store.GetTask(ctx, parent.ID)
	if err != nil {
		t.Fatalf("GetTask() resumed parent error = %v", err)
	}
	if resumedParent.Status != StatusQueued {
		t.Fatalf("resumed parent status = %q, want %q", resumedParent.Status, StatusQueued)
	}
	if resumedParent.SuspendReason != "" {
		t.Fatalf("resumed parent suspend reason = %q, want empty", resumedParent.SuspendReason)
	}

	claimedParent, _, err := store.ClaimNextTask(ctx, "runner-parent-2", time.Minute)
	if err != nil {
		t.Fatalf("ClaimNextTask() resumed parent error = %v", err)
	}
	if claimedParent == nil || claimedParent.ID != parent.ID {
		t.Fatalf("claimed resumed parent = %#v, want %q", claimedParent, parent.ID)
	}
}

func TestManagerSupportsParentChildFanOutAndFanIn(t *testing.T) {
	store := newTestStore(t)
	manager := NewManager(store, ManagerOptions{
		RunnerID:          "runner-1",
		WorkerCount:       4,
		PollInterval:      5 * time.Millisecond,
		LeaseDuration:     120 * time.Millisecond,
		HeartbeatInterval: 25 * time.Millisecond,
	})

	decodePhase := func(task *Task) (string, error) {
		var metadata map[string]any
		if err := json.Unmarshal(task.MetadataJSON, &metadata); err != nil {
			return "", fmt.Errorf("unmarshal metadata: %w", err)
		}
		phase, _ := metadata["phase"].(string)
		return phase, nil
	}
	expectedChildConversationIDs := []string{"child-conv-1", "child-conv-2", "child-conv-3"}

	childStarted := make(chan string, 3)
	releaseChildren := make(chan struct{})
	parentPhases := make(chan string, 2)
	if err := manager.RegisterExecutor("agent.run", func(ctx context.Context, task *Task, runtime *Runtime) (any, error) {
		phase, err := decodePhase(task)
		if err != nil {
			return nil, err
		}

		switch phase {
		case "root":
			parentPhases <- phase
			for idx, childConversationID := range expectedChildConversationIDs {
				if _, err := runtime.CreateChildTask(ctx, CreateTaskInput{
					TaskType:       "agent.run",
					ChildIndex:     idx,
					ConcurrencyKey: childConversationID,
					Metadata: map[string]any{
						"phase": "child",
					},
				}); err != nil {
					return nil, err
				}
			}
			if err := runtime.UpdateMetadata(ctx, map[string]any{"phase": "awaiting_children"}); err != nil {
				return nil, err
			}
			if err := runtime.Suspend(ctx, "waiting_for_child_tasks"); err != nil {
				return nil, err
			}
			return nil, ErrTaskSuspended
		case "child":
			childStarted <- task.ConcurrencyKey
			select {
			case <-releaseChildren:
				return map[string]any{"conversation_id": task.ConcurrencyKey}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		case "awaiting_children":
			parentPhases <- phase
			children, err := runtime.ListChildTasks(ctx)
			if err != nil {
				return nil, err
			}
			conversationIDs := make([]string, 0, len(children))
			for _, child := range children {
				var result map[string]any
				if err := json.Unmarshal(child.ResultJSON, &result); err != nil {
					return nil, fmt.Errorf("unmarshal child result: %w", err)
				}
				conversationID, _ := result["conversation_id"].(string)
				conversationIDs = append(conversationIDs, conversationID)
			}
			return map[string]any{
				"phase":               phase,
				"child_conversations": conversationIDs,
			}, nil
		default:
			return nil, fmt.Errorf("unexpected phase %q", phase)
		}
	}); err != nil {
		t.Fatalf("RegisterExecutor() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.Start(ctx)

	parent, err := manager.CreateTask(ctx, CreateTaskInput{
		TaskType:       "agent.run",
		ConcurrencyKey: "parent-conv",
		Metadata: map[string]any{
			"phase": "root",
		},
	})
	if err != nil {
		t.Fatalf("CreateTask() parent error = %v", err)
	}

	waitingParent := waitForTaskStatus(t, ctx, manager, parent.ID, StatusWaiting)
	if got := decodeJSONRaw(t, waitingParent.MetadataJSON)["phase"]; got != "awaiting_children" {
		t.Fatalf("waiting parent metadata phase = %#v, want %q", got, "awaiting_children")
	}

	startedChildren := make(map[string]struct{}, 3)
	for len(startedChildren) < 3 {
		select {
		case childConversationID := <-childStarted:
			startedChildren[childConversationID] = struct{}{}
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("started child count = %d, want 3", len(startedChildren))
		}
	}

	close(releaseChildren)

	completedParent := waitForTaskStatus(t, ctx, manager, parent.ID, StatusSucceeded)
	parentResult := decodeJSONRaw(t, completedParent.ResultJSON)
	if got := parentResult["phase"]; got != "awaiting_children" {
		t.Fatalf("completed parent phase = %#v, want %q", got, "awaiting_children")
	}
	childConversations, ok := parentResult["child_conversations"].([]any)
	if !ok {
		t.Fatalf("completed parent child_conversations type = %T, want []any", parentResult["child_conversations"])
	}
	if len(childConversations) != len(expectedChildConversationIDs) {
		t.Fatalf("completed parent child_conversations length = %d, want %d", len(childConversations), len(expectedChildConversationIDs))
	}
	childConversationSet := make(map[string]struct{}, len(childConversations))
	for index, rawConversationID := range childConversations {
		conversationID, ok := rawConversationID.(string)
		if !ok {
			t.Fatalf("completed parent child_conversations[%d] type = %T, want string", index, rawConversationID)
		}
		childConversationSet[conversationID] = struct{}{}
	}
	for _, expectedConversationID := range expectedChildConversationIDs {
		if _, ok := childConversationSet[expectedConversationID]; !ok {
			t.Fatalf("completed parent child_conversations missing %q: %#v", expectedConversationID, childConversations)
		}
	}

	children, err := store.ListChildTasks(ctx, parent.ID)
	if err != nil {
		t.Fatalf("ListChildTasks() error = %v", err)
	}
	if len(children) != 3 {
		t.Fatalf("child count = %d, want 3", len(children))
	}
	for index, child := range children {
		if child.Status != StatusSucceeded {
			t.Fatalf("child %d status = %q, want %q", index, child.Status, StatusSucceeded)
		}
		if child.ConcurrencyKey == "" {
			t.Fatalf("child %d concurrency key = empty, want child conversation id", index)
		}
	}

	firstPhase := <-parentPhases
	secondPhase := <-parentPhases
	if firstPhase != "root" || secondPhase != "awaiting_children" {
		t.Fatalf("parent phases = [%q %q], want [%q %q]", firstPhase, secondPhase, "root", "awaiting_children")
	}

	events, err := manager.ListEvents(ctx, parent.ID, 0, 20)
	if err != nil {
		t.Fatalf("ListEvents() parent error = %v", err)
	}
	var sawWaiting bool
	var sawResumed bool
	for _, event := range events {
		if event.EventType == EventTaskWaiting {
			sawWaiting = true
		}
		if event.EventType == EventTaskResumed {
			sawResumed = true
		}
	}
	if !sawWaiting || !sawResumed {
		t.Fatalf("parent events missing waiting/resumed markers: waiting=%v resumed=%v", sawWaiting, sawResumed)
	}
}

func TestManagerRepeatedSuspendRecordsWaitingAuditOnce(t *testing.T) {
	store := newTestStore(t)
	recorder := newRecordingAuditRecorder()
	manager := NewManager(store, ManagerOptions{
		RunnerID:          "runner-1",
		PollInterval:      5 * time.Millisecond,
		LeaseDuration:     100 * time.Millisecond,
		HeartbeatInterval: 20 * time.Millisecond,
		AuditRecorder:     recorder,
	})
	if err := manager.RegisterExecutor("agent.run", func(ctx context.Context, task *Task, runtime *Runtime) (any, error) {
		if err := runtime.Suspend(ctx, "waiting_for_child_tasks"); err != nil {
			return nil, err
		}
		if err := runtime.Suspend(ctx, "waiting_for_child_tasks"); err != nil {
			return nil, err
		}
		return nil, ErrTaskSuspended
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

	_ = waitForTaskStatus(t, ctx, manager, created.ID, StatusWaiting)

	assertAuditEventTypes(t, recorder, created.ID, "run.created", "run.started", "run.waiting")
	startStatuses := recorder.startStatuses(created.ID)
	if len(startStatuses) != 3 {
		t.Fatalf("audit start status count = %d, want 3 (%v)", len(startStatuses), startStatuses)
	}
	if startStatuses[0] != StatusQueued || startStatuses[1] != StatusRunning || startStatuses[2] != StatusWaiting {
		t.Fatalf("audit start statuses = %v, want [%q %q %q]", startStatuses, StatusQueued, StatusRunning, StatusWaiting)
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
