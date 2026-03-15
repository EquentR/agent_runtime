package tasks

import (
	"context"
	"errors"
	"testing"
	"time"
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
