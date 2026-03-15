package tasks

import (
	"context"
	"testing"
	"time"
)

// TestStoreCreateTaskPersistsQueuedSnapshotAndCreatedEvent 验证创建任务时会同时写入快照与 created 事件。
func TestStoreCreateTaskPersistsQueuedSnapshotAndCreatedEvent(t *testing.T) {
	store := newTestStore(t)

	task, events, err := store.CreateTask(context.Background(), CreateTaskInput{
		TaskType:  "agent.run",
		Input:     map[string]any{"prompt": "hello"},
		Config:    map[string]any{"timeout_seconds": 30},
		Metadata:  map[string]any{"source": "web"},
		CreatedBy: "user-1",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	if task.ID == "" {
		t.Fatal("task id = empty, want non-empty")
	}
	if task.Status != StatusQueued {
		t.Fatalf("task status = %q, want %q", task.Status, StatusQueued)
	}
	if task.RootTaskID != task.ID {
		t.Fatalf("root task id = %q, want self %q", task.RootTaskID, task.ID)
	}
	if task.ExecutionMode != ExecutionModeSerial {
		t.Fatalf("execution mode = %q, want %q", task.ExecutionMode, ExecutionModeSerial)
	}
	if got := decodeJSONRaw(t, task.InputJSON)["prompt"]; got != "hello" {
		t.Fatalf("task input prompt = %#v, want %q", got, "hello")
	}
	if len(events) != 1 {
		t.Fatalf("event count = %d, want 1", len(events))
	}
	if events[0].Seq != 1 || events[0].EventType != EventTaskCreated {
		t.Fatalf("event = %#v, want seq=1 type=%q", events[0], EventTaskCreated)
	}

	persisted, err := store.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if persisted.Status != StatusQueued {
		t.Fatalf("persisted status = %q, want %q", persisted.Status, StatusQueued)
	}
}

// TestStoreClaimNextTaskTransitionsQueuedTaskToRunning 验证领取任务会推进状态并写入租约信息。
func TestStoreClaimNextTaskTransitionsQueuedTaskToRunning(t *testing.T) {
	store := newTestStore(t)

	created, _, err := store.CreateTask(context.Background(), CreateTaskInput{TaskType: "agent.run"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	claimed, events, err := store.ClaimNextTask(context.Background(), "runner-1", 45*time.Second)
	if err != nil {
		t.Fatalf("ClaimNextTask() error = %v", err)
	}
	if claimed == nil {
		t.Fatal("claimed task = nil, want task")
	}
	if claimed.ID != created.ID {
		t.Fatalf("claimed id = %q, want %q", claimed.ID, created.ID)
	}
	if claimed.Status != StatusRunning {
		t.Fatalf("claimed status = %q, want %q", claimed.Status, StatusRunning)
	}
	if claimed.RunnerID != "runner-1" {
		t.Fatalf("runner id = %q, want %q", claimed.RunnerID, "runner-1")
	}
	if claimed.StartedAt == nil || claimed.LeaseExpiresAt == nil {
		t.Fatal("started_at or lease_expires_at = nil, want timestamps")
	}
	if !mustParseTime(t, claimed.LeaseExpiresAt).After(mustParseTime(t, claimed.StartedAt)) {
		t.Fatalf("lease_expires_at = %v, want after started_at = %v", claimed.LeaseExpiresAt, claimed.StartedAt)
	}
	if len(events) != 1 || events[0].EventType != EventTaskStarted || events[0].Seq != 2 {
		t.Fatalf("events = %#v, want single %q seq=2", events, EventTaskStarted)
	}
}

// TestStoreRequestCancelTransitionsRunningTask 验证取消请求会把任务推进到 cancel_requested。
func TestStoreRequestCancelTransitionsRunningTask(t *testing.T) {
	store := newTestStore(t)

	created, _, err := store.CreateTask(context.Background(), CreateTaskInput{TaskType: "agent.run"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if _, _, err := store.ClaimNextTask(context.Background(), "runner-1", 30*time.Second); err != nil {
		t.Fatalf("ClaimNextTask() error = %v", err)
	}

	updated, events, err := store.RequestCancel(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("RequestCancel() error = %v", err)
	}
	if updated.Status != StatusCancelRequested {
		t.Fatalf("status = %q, want %q", updated.Status, StatusCancelRequested)
	}
	if updated.CancelRequestedAt == nil {
		t.Fatal("cancel_requested_at = nil, want timestamp")
	}
	if len(events) != 1 || events[0].EventType != EventTaskCancelRequested || events[0].Seq != 3 {
		t.Fatalf("events = %#v, want single %q seq=3", events, EventTaskCancelRequested)
	}
}

// TestStoreRetryTaskCreatesNewQueuedTaskLinkedToOriginal 验证重试会生成新的排队任务并关联原任务。
func TestStoreRetryTaskCreatesNewQueuedTaskLinkedToOriginal(t *testing.T) {
	store := newTestStore(t)

	original, _, err := store.CreateTask(context.Background(), CreateTaskInput{
		TaskType: "agent.run",
		Input:    map[string]any{"prompt": "retry me"},
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if _, _, err := store.ClaimNextTask(context.Background(), "runner-1", 30*time.Second); err != nil {
		t.Fatalf("ClaimNextTask() error = %v", err)
	}
	if _, _, err := store.MarkSucceeded(context.Background(), original.ID, map[string]any{"message": "done"}); err != nil {
		t.Fatalf("MarkSucceeded() error = %v", err)
	}

	retried, events, err := store.RetryTask(context.Background(), original.ID)
	if err != nil {
		t.Fatalf("RetryTask() error = %v", err)
	}
	if retried.ID == original.ID {
		t.Fatalf("retried id = %q, want different from original", retried.ID)
	}
	if retried.RetryOfTaskID != original.ID {
		t.Fatalf("retry_of_task_id = %q, want %q", retried.RetryOfTaskID, original.ID)
	}
	if retried.Status != StatusQueued {
		t.Fatalf("retried status = %q, want %q", retried.Status, StatusQueued)
	}
	if got := decodeJSONRaw(t, retried.InputJSON)["prompt"]; got != "retry me" {
		t.Fatalf("retried input prompt = %#v, want %q", got, "retry me")
	}
	if len(events) != 1 || events[0].EventType != EventTaskCreated || events[0].Seq != 1 {
		t.Fatalf("events = %#v, want single created event seq=1", events)
	}
}

// TestStoreListEventsReturnsOnlyEventsAfterSequence 验证事件查询支持按序号增量拉取。
func TestStoreListEventsReturnsOnlyEventsAfterSequence(t *testing.T) {
	store := newTestStore(t)

	created, _, err := store.CreateTask(context.Background(), CreateTaskInput{TaskType: "agent.run"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if _, _, err := store.ClaimNextTask(context.Background(), "runner-1", 30*time.Second); err != nil {
		t.Fatalf("ClaimNextTask() error = %v", err)
	}
	if _, _, err := store.RequestCancel(context.Background(), created.ID); err != nil {
		t.Fatalf("RequestCancel() error = %v", err)
	}

	events, err := store.ListEvents(context.Background(), created.ID, 1, 10)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("event count = %d, want 2", len(events))
	}
	if events[0].Seq != 2 || events[1].Seq != 3 {
		t.Fatalf("event seqs = %#v, want [2 3]", []int64{events[0].Seq, events[1].Seq})
	}
}
