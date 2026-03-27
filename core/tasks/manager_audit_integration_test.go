package tasks_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	coreaudit "github.com/EquentR/agent_runtime/core/audit"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestManagerStoreBackedAuditRecorderMarksRunRunningOnStart(t *testing.T) {
	taskStore, auditStore, db := newTaskAndAuditStores(t)
	manager := coretasks.NewManager(taskStore, coretasks.ManagerOptions{
		RunnerID:          "runner-1",
		PollInterval:      5 * time.Millisecond,
		LeaseDuration:     100 * time.Millisecond,
		HeartbeatInterval: 20 * time.Millisecond,
		AuditRecorder:     newTaskAuditRecorder(coreaudit.NewRecorder(auditStore)),
	})

	release := make(chan struct{})
	if err := manager.RegisterExecutor("agent.run", func(ctx context.Context, task *coretasks.Task, runtime *coretasks.Runtime) (any, error) {
		<-release
		return map[string]any{"ok": true}, nil
	}); err != nil {
		t.Fatalf("RegisterExecutor() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	created, err := manager.CreateTask(ctx, coretasks.CreateTaskInput{TaskType: "agent.run", CreatedBy: "user-1"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	run, err := auditStore.GetRunByTaskID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetRunByTaskID(created) error = %v", err)
	}
	if run == nil {
		t.Fatalf("audit run for task %q = nil", created.ID)
	}
	if run.Status != coreaudit.StatusQueued {
		t.Fatalf("created audit run status = %q, want %q", run.Status, coreaudit.StatusQueued)
	}

	manager.Start(ctx)
	_ = waitForTaskStatus(t, ctx, manager, created.ID, coretasks.StatusRunning)
	run = waitForAuditRunStatus(t, ctx, auditStore, created.ID, coreaudit.StatusRunning)
	if run.StartedAt == nil {
		t.Fatalf("started audit run started_at = nil for task %q", created.ID)
	}
	waitForAuditEvent(t, db, created.ID, "run.started")

	close(release)
	_ = waitForTaskStatus(t, ctx, manager, created.ID, coretasks.StatusSucceeded)
}

func TestManagerStoreBackedAuditRecorderMarksRunWaitingOnSuspend(t *testing.T) {
	taskStore, auditStore, db := newTaskAndAuditStores(t)
	manager := coretasks.NewManager(taskStore, coretasks.ManagerOptions{
		RunnerID:          "runner-1",
		PollInterval:      5 * time.Millisecond,
		LeaseDuration:     100 * time.Millisecond,
		HeartbeatInterval: 20 * time.Millisecond,
		AuditRecorder:     newTaskAuditRecorder(coreaudit.NewRecorder(auditStore)),
	})
	if err := manager.RegisterExecutor("agent.run", func(ctx context.Context, task *coretasks.Task, runtime *coretasks.Runtime) (any, error) {
		if err := runtime.Suspend(ctx, "waiting_for_child_tasks"); err != nil {
			return nil, err
		}
		return nil, coretasks.ErrTaskSuspended
	}); err != nil {
		t.Fatalf("RegisterExecutor() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.Start(ctx)

	created, err := manager.CreateTask(ctx, coretasks.CreateTaskInput{TaskType: "agent.run", CreatedBy: "user-1"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	_ = waitForTaskStatus(t, ctx, manager, created.ID, coretasks.StatusWaiting)
	run := waitForAuditRunStatus(t, ctx, auditStore, created.ID, coreaudit.Status(coretasks.StatusWaiting))
	if run.StartedAt == nil {
		t.Fatalf("waiting audit run started_at = nil for task %q", created.ID)
	}
	waitForAuditEvent(t, db, created.ID, "run.waiting")
}

type taskAuditRecorder struct {
	recorder coreaudit.Recorder
}

func newTaskAuditRecorder(recorder coreaudit.Recorder) coretasks.AuditRecorder {
	return &taskAuditRecorder{recorder: recorder}
}

func (r *taskAuditRecorder) StartRun(ctx context.Context, input coretasks.AuditStartRunInput) (*coretasks.AuditRun, error) {
	run, err := r.recorder.StartRun(ctx, coreaudit.StartRunInput{
		TaskID:        input.TaskID,
		TaskType:      input.TaskType,
		RunnerID:      input.RunnerID,
		CreatedBy:     input.CreatedBy,
		Status:        coreaudit.Status(input.Status),
		StartedAt:     input.StartedAt,
		SchemaVersion: coreaudit.SchemaVersionV1,
	})
	if err != nil {
		return nil, err
	}
	return &coretasks.AuditRun{ID: run.ID, TaskID: run.TaskID}, nil
}

func (r *taskAuditRecorder) AppendEvent(ctx context.Context, runID string, input coretasks.AuditAppendEventInput) (*coretasks.AuditEvent, error) {
	event, err := r.recorder.AppendEvent(ctx, runID, coreaudit.AppendEventInput{
		EventType: input.EventType,
		Payload:   input.Payload,
	})
	if err != nil {
		return nil, err
	}
	return &coretasks.AuditEvent{RunID: event.RunID, EventType: event.EventType}, nil
}

func (r *taskAuditRecorder) FinishRun(ctx context.Context, runID string, input coretasks.AuditFinishRunInput) error {
	return r.recorder.FinishRun(ctx, runID, coreaudit.FinishRunInput{
		Status:     coreaudit.Status(input.Status),
		FinishedAt: input.FinishedAt,
	})
}

func newTaskAndAuditStores(t *testing.T) (*coretasks.Store, *coreaudit.Store, *gorm.DB) {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}

	taskStore := coretasks.NewStore(db)
	if err := taskStore.AutoMigrate(); err != nil {
		t.Fatalf("task store migrate: %v", err)
	}
	auditStore := coreaudit.NewStore(db)
	if err := auditStore.AutoMigrate(); err != nil {
		t.Fatalf("audit store migrate: %v", err)
	}
	return taskStore, auditStore, db
}

func waitForTaskStatus(t *testing.T, ctx context.Context, manager *coretasks.Manager, taskID string, want coretasks.Status) *coretasks.Task {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, err := manager.GetTask(ctx, taskID)
		if err == nil && task.Status == want {
			return task
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("task %s did not reach status %q", taskID, want)
	return nil
}

func waitForAuditRunStatus(t *testing.T, ctx context.Context, store *coreaudit.Store, taskID string, want coreaudit.Status) *coreaudit.Run {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		run, err := store.GetRunByTaskID(ctx, taskID)
		if err == nil && run != nil && run.Status == want {
			return run
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("audit run for task %s did not reach status %q", taskID, want)
	return nil
}

func waitForAuditEvent(t *testing.T, db *gorm.DB, taskID string, want string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var event coreaudit.Event
		if err := db.Where("task_id = ? AND event_type = ?", taskID, want).First(&event).Error; err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("audit event %q for task %s was not persisted", want, taskID)
}
