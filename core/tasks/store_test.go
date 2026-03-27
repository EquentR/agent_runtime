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

	persisted, err := store.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if persisted.ConcurrencyKey != "conv_1" {
		t.Fatalf("persisted concurrency key = %q, want %q", persisted.ConcurrencyKey, "conv_1")
	}
	if persisted.Status != StatusQueued {
		t.Fatalf("persisted status = %q, want %q", persisted.Status, StatusQueued)
	}
	if len(task.InputJSON) != 2 || string(task.InputJSON) != "{}" {
		t.Fatalf("input json = %q, want empty object", string(task.InputJSON))
	}
	if len(task.ConfigJSON) != 2 || string(task.ConfigJSON) != "{}" {
		t.Fatalf("config json = %q, want empty object", string(task.ConfigJSON))
	}
	if len(task.MetadataJSON) != 2 || string(task.MetadataJSON) != "{}" {
		t.Fatalf("metadata json = %q, want empty object", string(task.MetadataJSON))
	}
	if task.RootTaskID != task.ID {
		t.Fatalf("root task id = %q, want self %q", task.RootTaskID, task.ID)
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

func TestStoreClaimNextTaskBlocksQueuedTaskWithSameConcurrencyKey(t *testing.T) {
	store := newTestStore(t)

	first, _, err := store.CreateTask(context.Background(), CreateTaskInput{
		TaskType:       "agent.run",
		ConcurrencyKey: "conv_1",
	})
	if err != nil {
		t.Fatalf("CreateTask() first error = %v", err)
	}
	second, _, err := store.CreateTask(context.Background(), CreateTaskInput{
		TaskType:       "agent.run",
		ConcurrencyKey: "conv_1",
	})
	if err != nil {
		t.Fatalf("CreateTask() second error = %v", err)
	}

	claimedFirst, _, err := store.ClaimNextTask(context.Background(), "runner-1", time.Minute)
	if err != nil {
		t.Fatalf("ClaimNextTask() first error = %v", err)
	}
	if claimedFirst == nil || claimedFirst.ID != first.ID {
		t.Fatalf("first claimed id = %v, want %q", claimedFirst, first.ID)
	}

	claimedSecond, _, err := store.ClaimNextTask(context.Background(), "runner-2", time.Minute)
	if err != nil {
		t.Fatalf("ClaimNextTask() second error = %v", err)
	}
	if claimedSecond != nil {
		t.Fatalf("claimed = %#v, want nil while %q blocks %q", claimedSecond, first.ID, second.ID)
	}

	persistedSecond, err := store.GetTask(context.Background(), second.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if persistedSecond.Status != StatusQueued {
		t.Fatalf("blocked task status = %q, want %q", persistedSecond.Status, StatusQueued)
	}
}

func TestStoreClaimNextTaskSkipsBlockedHeadAndClaimsDifferentConcurrencyKey(t *testing.T) {
	store := newTestStore(t)

	first, _, err := store.CreateTask(context.Background(), CreateTaskInput{
		TaskType:       "agent.run",
		ConcurrencyKey: "conv_1",
	})
	if err != nil {
		t.Fatalf("CreateTask() first error = %v", err)
	}
	blockedHead, _, err := store.CreateTask(context.Background(), CreateTaskInput{
		TaskType:       "agent.run",
		ConcurrencyKey: "conv_1",
	})
	if err != nil {
		t.Fatalf("CreateTask() blocked head error = %v", err)
	}
	third, _, err := store.CreateTask(context.Background(), CreateTaskInput{
		TaskType:       "agent.run",
		ConcurrencyKey: "conv_2",
	})
	if err != nil {
		t.Fatalf("CreateTask() third error = %v", err)
	}

	claimedFirst, _, err := store.ClaimNextTask(context.Background(), "runner-1", time.Minute)
	if err != nil {
		t.Fatalf("ClaimNextTask() first error = %v", err)
	}
	if claimedFirst == nil || claimedFirst.ID != first.ID {
		t.Fatalf("first claimed id = %v, want %q", claimedFirst, first.ID)
	}

	claimedSecond, _, err := store.ClaimNextTask(context.Background(), "runner-2", time.Minute)
	if err != nil {
		t.Fatalf("ClaimNextTask() second error = %v", err)
	}
	if claimedSecond == nil || claimedSecond.ID != third.ID {
		t.Fatalf("second claimed id = %v, want %q", claimedSecond, third.ID)
	}

	persistedBlockedHead, err := store.GetTask(context.Background(), blockedHead.ID)
	if err != nil {
		t.Fatalf("GetTask() blocked head error = %v", err)
	}
	if persistedBlockedHead.Status != StatusQueued {
		t.Fatalf("blocked head status = %q, want %q", persistedBlockedHead.Status, StatusQueued)
	}
}

func TestStoreClaimNextTaskEventuallyClaimsBlockedHeadAfterConflictingTaskFinishes(t *testing.T) {
	store := newTestStore(t)

	first, _, err := store.CreateTask(context.Background(), CreateTaskInput{
		TaskType:       "agent.run",
		ConcurrencyKey: "conv_1",
	})
	if err != nil {
		t.Fatalf("CreateTask() first error = %v", err)
	}
	blockedHead, _, err := store.CreateTask(context.Background(), CreateTaskInput{
		TaskType:       "agent.run",
		ConcurrencyKey: "conv_1",
	})
	if err != nil {
		t.Fatalf("CreateTask() blocked head error = %v", err)
	}
	third, _, err := store.CreateTask(context.Background(), CreateTaskInput{
		TaskType:       "agent.run",
		ConcurrencyKey: "conv_2",
	})
	if err != nil {
		t.Fatalf("CreateTask() third error = %v", err)
	}

	claimedFirst, _, err := store.ClaimNextTask(context.Background(), "runner-1", time.Minute)
	if err != nil {
		t.Fatalf("ClaimNextTask() first error = %v", err)
	}
	if claimedFirst == nil || claimedFirst.ID != first.ID {
		t.Fatalf("first claimed id = %v, want %q", claimedFirst, first.ID)
	}

	claimedSecond, _, err := store.ClaimNextTask(context.Background(), "runner-2", time.Minute)
	if err != nil {
		t.Fatalf("ClaimNextTask() second error = %v", err)
	}
	if claimedSecond == nil || claimedSecond.ID != third.ID {
		t.Fatalf("second claimed id = %v, want %q", claimedSecond, third.ID)
	}

	if _, _, err := store.MarkSucceeded(context.Background(), first.ID, map[string]any{"message": "done"}); err != nil {
		t.Fatalf("MarkSucceeded() first error = %v", err)
	}

	claimedBlockedHead, _, err := store.ClaimNextTask(context.Background(), "runner-3", time.Minute)
	if err != nil {
		t.Fatalf("ClaimNextTask() blocked head error = %v", err)
	}
	if claimedBlockedHead == nil || claimedBlockedHead.ID != blockedHead.ID {
		t.Fatalf("blocked head claimed id = %v, want %q", claimedBlockedHead, blockedHead.ID)
	}
	if claimedBlockedHead.Status != StatusRunning {
		t.Fatalf("blocked head status = %q, want %q", claimedBlockedHead.Status, StatusRunning)
	}
}

func TestStoreClaimNextTaskSkipsMoreThanOneBlockedBatchToClaimDifferentConcurrencyKey(t *testing.T) {
	store := newTestStore(t)

	active, _, err := store.CreateTask(context.Background(), CreateTaskInput{
		TaskType:       "agent.run",
		ConcurrencyKey: "conv_1",
	})
	if err != nil {
		t.Fatalf("CreateTask() active error = %v", err)
	}
	if _, _, err := store.ClaimNextTask(context.Background(), "runner-1", time.Minute); err != nil {
		t.Fatalf("ClaimNextTask() active error = %v", err)
	}

	blockedIDs := make([]string, 0, 33)
	for range 33 {
		blocked, _, err := store.CreateTask(context.Background(), CreateTaskInput{
			TaskType:       "agent.run",
			ConcurrencyKey: "conv_1",
		})
		if err != nil {
			t.Fatalf("CreateTask() blocked error = %v", err)
		}
		blockedIDs = append(blockedIDs, blocked.ID)
	}

	eligible, _, err := store.CreateTask(context.Background(), CreateTaskInput{
		TaskType:       "agent.run",
		ConcurrencyKey: "conv_2",
	})
	if err != nil {
		t.Fatalf("CreateTask() eligible error = %v", err)
	}

	claimed, _, err := store.ClaimNextTask(context.Background(), "runner-2", time.Minute)
	if err != nil {
		t.Fatalf("ClaimNextTask() eligible error = %v", err)
	}
	if claimed == nil || claimed.ID != eligible.ID {
		t.Fatalf("claimed id = %v, want eligible %q with active blocker %q", claimed, eligible.ID, active.ID)
	}

	for _, blockedID := range blockedIDs {
		persistedBlocked, err := store.GetTask(context.Background(), blockedID)
		if err != nil {
			t.Fatalf("GetTask(%q) error = %v", blockedID, err)
		}
		if persistedBlocked.Status != StatusQueued {
			t.Fatalf("blocked task %q status = %q, want %q", blockedID, persistedBlocked.Status, StatusQueued)
		}
	}
}

func TestStoreClaimNextTaskAllowsParallelClaimsWithoutConcurrencyKey(t *testing.T) {
	store := newTestStore(t)

	first, _, err := store.CreateTask(context.Background(), CreateTaskInput{TaskType: "agent.run"})
	if err != nil {
		t.Fatalf("CreateTask() first error = %v", err)
	}
	second, _, err := store.CreateTask(context.Background(), CreateTaskInput{TaskType: "agent.run"})
	if err != nil {
		t.Fatalf("CreateTask() second error = %v", err)
	}

	claimedFirst, _, err := store.ClaimNextTask(context.Background(), "runner-1", time.Minute)
	if err != nil {
		t.Fatalf("ClaimNextTask() first error = %v", err)
	}
	if claimedFirst == nil || claimedFirst.ID != first.ID {
		t.Fatalf("first claimed id = %v, want %q", claimedFirst, first.ID)
	}

	claimedSecond, _, err := store.ClaimNextTask(context.Background(), "runner-2", time.Minute)
	if err != nil {
		t.Fatalf("ClaimNextTask() second error = %v", err)
	}
	if claimedSecond == nil || claimedSecond.ID != second.ID {
		t.Fatalf("second claimed id = %v, want %q", claimedSecond, second.ID)
	}
	if claimedSecond.Status != StatusRunning {
		t.Fatalf("second claimed status = %q, want %q", claimedSecond.Status, StatusRunning)
	}
}

func TestStoreClaimNextTaskAllowsWaitingParentChildButStillBlocksUnrelatedSameKeyTask(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	parent, _, err := store.CreateTask(ctx, CreateTaskInput{
		TaskType:       "parent.run",
		ConcurrencyKey: "conv_1",
	})
	if err != nil {
		t.Fatalf("CreateTask() parent error = %v", err)
	}
	if _, _, err := store.ClaimNextTask(ctx, "runner-parent", time.Minute); err != nil {
		t.Fatalf("ClaimNextTask() parent error = %v", err)
	}
	if _, _, err := store.MarkWaiting(ctx, parent.ID, "waiting_for_child_tasks"); err != nil {
		t.Fatalf("MarkWaiting() parent error = %v", err)
	}

	child, _, err := store.CreateTask(ctx, CreateTaskInput{
		TaskType:       "child.run",
		RootTaskID:     parent.RootTaskID,
		ParentTaskID:   parent.ID,
		ConcurrencyKey: "conv_1",
	})
	if err != nil {
		t.Fatalf("CreateTask() child error = %v", err)
	}
	unrelated, _, err := store.CreateTask(ctx, CreateTaskInput{
		TaskType:       "agent.run",
		ConcurrencyKey: "conv_1",
	})
	if err != nil {
		t.Fatalf("CreateTask() unrelated error = %v", err)
	}

	claimedChild, _, err := store.ClaimNextTask(ctx, "runner-child", time.Minute)
	if err != nil {
		t.Fatalf("ClaimNextTask() child error = %v", err)
	}
	if claimedChild == nil || claimedChild.ID != child.ID {
		t.Fatalf("claimed child = %#v, want %q", claimedChild, child.ID)
	}
	if claimedChild.ParentTaskID != parent.ID {
		t.Fatalf("claimed child parent_task_id = %q, want %q", claimedChild.ParentTaskID, parent.ID)
	}

	if _, _, err := store.MarkSucceeded(ctx, child.ID, map[string]any{"message": "done"}); err != nil {
		t.Fatalf("MarkSucceeded() child error = %v", err)
	}

	claimedUnrelated, _, err := store.ClaimNextTask(ctx, "runner-other", time.Minute)
	if err != nil {
		t.Fatalf("ClaimNextTask() unrelated error = %v", err)
	}
	if claimedUnrelated != nil {
		t.Fatalf("claimed unrelated = %#v, want nil while waiting parent %q still blocks %q", claimedUnrelated, parent.ID, unrelated.ID)
	}

	persistedUnrelated, err := store.GetTask(ctx, unrelated.ID)
	if err != nil {
		t.Fatalf("GetTask() unrelated error = %v", err)
	}
	if persistedUnrelated.Status != StatusQueued {
		t.Fatalf("unrelated status = %q, want %q", persistedUnrelated.Status, StatusQueued)
	}
}

func TestStoreMarkWaitingTransitionsRunningTaskAndClearsLease(t *testing.T) {
	store := newTestStore(t)

	created, _, err := store.CreateTask(context.Background(), CreateTaskInput{TaskType: "agent.run"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	claimed, _, err := store.ClaimNextTask(context.Background(), "runner-1", time.Minute)
	if err != nil {
		t.Fatalf("ClaimNextTask() error = %v", err)
	}

	waiting, events, err := store.MarkWaiting(context.Background(), created.ID, "waiting_for_child_tasks")
	if err != nil {
		t.Fatalf("MarkWaiting() error = %v", err)
	}
	if waiting.Status != StatusWaiting {
		t.Fatalf("waiting status = %q, want %q", waiting.Status, StatusWaiting)
	}
	if waiting.SuspendReason != "waiting_for_child_tasks" {
		t.Fatalf("suspend reason = %q, want %q", waiting.SuspendReason, "waiting_for_child_tasks")
	}
	if waiting.RunnerID != "" {
		t.Fatalf("runner id = %q, want empty", waiting.RunnerID)
	}
	if waiting.LeaseExpiresAt != nil {
		t.Fatalf("lease_expires_at = %v, want nil", waiting.LeaseExpiresAt)
	}
	if waiting.HeartbeatAt == nil {
		t.Fatal("heartbeat_at = nil, want previous heartbeat")
	}
	if len(events) != 1 || events[0].EventType != EventTaskWaiting || events[0].Seq != 3 {
		t.Fatalf("events = %#v, want single %q seq=3", events, EventTaskWaiting)
	}

	persisted, err := store.GetTask(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if persisted.Status != StatusWaiting {
		t.Fatalf("persisted status = %q, want %q", persisted.Status, StatusWaiting)
	}
	if persisted.SuspendReason != "waiting_for_child_tasks" {
		t.Fatalf("persisted suspend reason = %q, want %q", persisted.SuspendReason, "waiting_for_child_tasks")
	}
	if persisted.RunnerID != "" {
		t.Fatalf("persisted runner id = %q, want empty", persisted.RunnerID)
	}
	if persisted.LeaseExpiresAt != nil {
		t.Fatalf("persisted lease_expires_at = %v, want nil", persisted.LeaseExpiresAt)
	}
	if persisted.HeartbeatAt == nil {
		t.Fatal("persisted heartbeat_at = nil, want previous heartbeat")
	}
	if !mustParseTime(t, persisted.HeartbeatAt).Equal(mustParseTime(t, claimed.HeartbeatAt)) {
		t.Fatalf("persisted heartbeat_at = %v, want unchanged %v", persisted.HeartbeatAt, claimed.HeartbeatAt)
	}
}

func TestStoreResumeWaitingTaskRequeuesTask(t *testing.T) {
	store := newTestStore(t)

	created, _, err := store.CreateTask(context.Background(), CreateTaskInput{TaskType: "agent.run"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if _, _, err := store.ClaimNextTask(context.Background(), "runner-1", time.Minute); err != nil {
		t.Fatalf("ClaimNextTask() error = %v", err)
	}
	if _, _, err := store.MarkWaiting(context.Background(), created.ID, "waiting_for_child_tasks"); err != nil {
		t.Fatalf("MarkWaiting() error = %v", err)
	}

	resumed, events, err := store.ResumeWaitingTask(context.Background(), created.ID, "children_complete")
	if err != nil {
		t.Fatalf("ResumeWaitingTask() error = %v", err)
	}
	if resumed.Status != StatusQueued {
		t.Fatalf("resumed status = %q, want %q", resumed.Status, StatusQueued)
	}
	if resumed.SuspendReason != "" {
		t.Fatalf("suspend reason = %q, want empty", resumed.SuspendReason)
	}
	if len(events) != 1 || events[0].EventType != EventTaskResumed || events[0].Seq != 4 {
		t.Fatalf("events = %#v, want single %q seq=4", events, EventTaskResumed)
	}

	persisted, err := store.GetTask(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if persisted.Status != StatusQueued {
		t.Fatalf("persisted status = %q, want %q", persisted.Status, StatusQueued)
	}
	if persisted.SuspendReason != "" {
		t.Fatalf("persisted suspend reason = %q, want empty", persisted.SuspendReason)
	}
}

func TestStoreTryResumeParentTaskDoesNothingWhileChildStillActive(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	parent, _, err := store.CreateTask(ctx, CreateTaskInput{TaskType: "agent.run"})
	if err != nil {
		t.Fatalf("CreateTask() parent error = %v", err)
	}
	if _, _, err := store.ClaimNextTask(ctx, "runner-parent", time.Minute); err != nil {
		t.Fatalf("ClaimNextTask() parent error = %v", err)
	}
	if _, _, err := store.MarkWaiting(ctx, parent.ID, "waiting_for_child_tasks"); err != nil {
		t.Fatalf("MarkWaiting() parent error = %v", err)
	}

	completedChild, _, err := store.CreateTask(ctx, CreateTaskInput{
		TaskType:     "agent.run",
		RootTaskID:   parent.RootTaskID,
		ParentTaskID: parent.ID,
	})
	if err != nil {
		t.Fatalf("CreateTask() completed child error = %v", err)
	}
	activeChild, _, err := store.CreateTask(ctx, CreateTaskInput{
		TaskType:     "agent.run",
		RootTaskID:   parent.RootTaskID,
		ParentTaskID: parent.ID,
	})
	if err != nil {
		t.Fatalf("CreateTask() active child error = %v", err)
	}
	if _, _, err := store.MarkSucceeded(ctx, completedChild.ID, map[string]any{"message": "done"}); err != nil {
		t.Fatalf("MarkSucceeded() completed child error = %v", err)
	}

	activeCount, err := store.CountActiveChildTasks(ctx, parent.ID)
	if err != nil {
		t.Fatalf("CountActiveChildTasks() error = %v", err)
	}
	if activeCount != 1 {
		t.Fatalf("active child count = %d, want 1 for child %q", activeCount, activeChild.ID)
	}

	resumed, events, err := store.TryResumeParentTask(ctx, parent.ID)
	if err != nil {
		t.Fatalf("TryResumeParentTask() error = %v", err)
	}
	if resumed.Status != StatusWaiting {
		t.Fatalf("parent status = %q, want %q", resumed.Status, StatusWaiting)
	}
	if len(events) != 0 {
		t.Fatalf("len(events) = %d, want 0", len(events))
	}

	persisted, err := store.GetTask(ctx, parent.ID)
	if err != nil {
		t.Fatalf("GetTask() parent error = %v", err)
	}
	if persisted.Status != StatusWaiting {
		t.Fatalf("persisted status = %q, want %q", persisted.Status, StatusWaiting)
	}
	if persisted.SuspendReason != "waiting_for_child_tasks" {
		t.Fatalf("persisted suspend reason = %q, want %q", persisted.SuspendReason, "waiting_for_child_tasks")
	}
}

func TestStoreTryResumeParentTaskRequeuesWaitingParentWhenAllChildrenTerminal(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	parent, _, err := store.CreateTask(ctx, CreateTaskInput{TaskType: "agent.run"})
	if err != nil {
		t.Fatalf("CreateTask() parent error = %v", err)
	}
	if _, _, err := store.ClaimNextTask(ctx, "runner-parent", time.Minute); err != nil {
		t.Fatalf("ClaimNextTask() parent error = %v", err)
	}
	if _, _, err := store.MarkWaiting(ctx, parent.ID, "waiting_for_child_tasks"); err != nil {
		t.Fatalf("MarkWaiting() parent error = %v", err)
	}

	succeededChild, _, err := store.CreateTask(ctx, CreateTaskInput{
		TaskType:     "agent.run",
		RootTaskID:   parent.RootTaskID,
		ParentTaskID: parent.ID,
	})
	if err != nil {
		t.Fatalf("CreateTask() succeeded child error = %v", err)
	}
	cancelledChild, _, err := store.CreateTask(ctx, CreateTaskInput{
		TaskType:     "agent.run",
		RootTaskID:   parent.RootTaskID,
		ParentTaskID: parent.ID,
	})
	if err != nil {
		t.Fatalf("CreateTask() cancelled child error = %v", err)
	}
	if _, _, err := store.MarkSucceeded(ctx, succeededChild.ID, map[string]any{"message": "done"}); err != nil {
		t.Fatalf("MarkSucceeded() child error = %v", err)
	}
	if _, _, err := store.MarkCancelled(ctx, cancelledChild.ID, map[string]any{"message": "cancelled"}); err != nil {
		t.Fatalf("MarkCancelled() child error = %v", err)
	}

	activeCount, err := store.CountActiveChildTasks(ctx, parent.ID)
	if err != nil {
		t.Fatalf("CountActiveChildTasks() error = %v", err)
	}
	if activeCount != 0 {
		t.Fatalf("active child count = %d, want 0", activeCount)
	}

	resumed, events, err := store.TryResumeParentTask(ctx, parent.ID)
	if err != nil {
		t.Fatalf("TryResumeParentTask() error = %v", err)
	}
	if resumed.Status != StatusQueued {
		t.Fatalf("parent status = %q, want %q", resumed.Status, StatusQueued)
	}
	if resumed.SuspendReason != "" {
		t.Fatalf("parent suspend reason = %q, want empty", resumed.SuspendReason)
	}
	if len(events) != 1 || events[0].EventType != EventTaskResumed || events[0].Seq != 4 {
		t.Fatalf("events = %#v, want single %q seq=4", events, EventTaskResumed)
	}

	persisted, err := store.GetTask(ctx, parent.ID)
	if err != nil {
		t.Fatalf("GetTask() parent error = %v", err)
	}
	if persisted.Status != StatusQueued {
		t.Fatalf("persisted status = %q, want %q", persisted.Status, StatusQueued)
	}
	if persisted.SuspendReason != "" {
		t.Fatalf("persisted suspend reason = %q, want empty", persisted.SuspendReason)
	}

	unchanged, noEvents, err := store.TryResumeParentTask(ctx, parent.ID)
	if err != nil {
		t.Fatalf("second TryResumeParentTask() error = %v", err)
	}
	if unchanged.Status != StatusQueued {
		t.Fatalf("unchanged status = %q, want %q", unchanged.Status, StatusQueued)
	}
	if len(noEvents) != 0 {
		t.Fatalf("len(noEvents) = %d, want 0", len(noEvents))
	}
}

func TestStoreUpdateHeartbeatDoesNotRestoreLeaseForWaitingTask(t *testing.T) {
	store := newTestStore(t)

	created, _, err := store.CreateTask(context.Background(), CreateTaskInput{TaskType: "agent.run"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if _, _, err := store.ClaimNextTask(context.Background(), "runner-1", time.Minute); err != nil {
		t.Fatalf("ClaimNextTask() error = %v", err)
	}
	if _, _, err := store.MarkWaiting(context.Background(), created.ID, "waiting_for_child_tasks"); err != nil {
		t.Fatalf("MarkWaiting() error = %v", err)
	}

	updated, err := store.UpdateHeartbeat(context.Background(), created.ID, "runner-2", time.Minute)
	if err != nil {
		t.Fatalf("UpdateHeartbeat() error = %v", err)
	}
	if updated.Status != StatusWaiting {
		t.Fatalf("updated status = %q, want %q", updated.Status, StatusWaiting)
	}
	if updated.RunnerID != "" {
		t.Fatalf("runner id = %q, want empty", updated.RunnerID)
	}
	if updated.LeaseExpiresAt != nil {
		t.Fatalf("lease_expires_at = %v, want nil", updated.LeaseExpiresAt)
	}

	persisted, err := store.GetTask(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if persisted.Status != StatusWaiting {
		t.Fatalf("persisted status = %q, want %q", persisted.Status, StatusWaiting)
	}
	if persisted.RunnerID != "" {
		t.Fatalf("persisted runner id = %q, want empty", persisted.RunnerID)
	}
	if persisted.LeaseExpiresAt != nil {
		t.Fatalf("persisted lease_expires_at = %v, want nil", persisted.LeaseExpiresAt)
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

func TestStoreRequestCancelDoesNotOverwriteTerminalTask(t *testing.T) {
	store := newTestStore(t)

	created, _, err := store.CreateTask(context.Background(), CreateTaskInput{TaskType: "agent.run"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if _, _, err := store.ClaimNextTask(context.Background(), "runner-1", 30*time.Second); err != nil {
		t.Fatalf("ClaimNextTask() error = %v", err)
	}
	if _, _, err := store.MarkSucceeded(context.Background(), created.ID, map[string]any{"message": "done"}); err != nil {
		t.Fatalf("MarkSucceeded() error = %v", err)
	}

	updated, events, err := store.RequestCancel(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("RequestCancel() error = %v", err)
	}
	if updated.Status != StatusSucceeded {
		t.Fatalf("updated status = %q, want %q", updated.Status, StatusSucceeded)
	}
	if len(events) != 0 {
		t.Fatalf("len(events) = %d, want 0", len(events))
	}

	persisted, err := store.GetTask(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if persisted.Status != StatusSucceeded {
		t.Fatalf("persisted status = %q, want %q", persisted.Status, StatusSucceeded)
	}
	if persisted.FinishedAt == nil {
		t.Fatal("finished_at = nil, want timestamp")
	}
	if got := decodeJSONRaw(t, persisted.ResultJSON)["message"]; got != "done" {
		t.Fatalf("result message = %#v, want %q", got, "done")
	}
	listed, err := store.ListEvents(context.Background(), created.ID, 0, 10)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	for _, event := range listed {
		if event.EventType == EventTaskCancelRequested {
			t.Fatalf("unexpected event type %q after late cancel", event.EventType)
		}
	}
}

func TestStoreFinishTaskKeepsExistingTerminalState(t *testing.T) {
	store := newTestStore(t)

	created, _, err := store.CreateTask(context.Background(), CreateTaskInput{TaskType: "agent.run"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if _, _, err := store.ClaimNextTask(context.Background(), "runner-1", 30*time.Second); err != nil {
		t.Fatalf("ClaimNextTask() error = %v", err)
	}
	first, firstEvents, err := store.MarkSucceeded(context.Background(), created.ID, map[string]any{"message": "done"})
	if err != nil {
		t.Fatalf("MarkSucceeded() error = %v", err)
	}
	if first.Status != StatusSucceeded || len(firstEvents) != 1 {
		t.Fatalf("first terminal write = (%q, %d events), want (%q, 1)", first.Status, len(firstEvents), StatusSucceeded)
	}

	second, secondEvents, err := store.MarkCancelled(context.Background(), created.ID, map[string]any{"message": "too late"})
	if err != nil {
		t.Fatalf("MarkCancelled() error = %v", err)
	}
	if second.Status != StatusSucceeded {
		t.Fatalf("second status = %q, want preserved %q", second.Status, StatusSucceeded)
	}
	if len(secondEvents) != 0 {
		t.Fatalf("len(secondEvents) = %d, want 0", len(secondEvents))
	}

	persisted, err := store.GetTask(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if persisted.Status != StatusSucceeded {
		t.Fatalf("persisted status = %q, want %q", persisted.Status, StatusSucceeded)
	}
	if got := decodeJSONRaw(t, persisted.ResultJSON)["message"]; got != "done" {
		t.Fatalf("result message = %#v, want %q", got, "done")
	}
	listed, err := store.ListEvents(context.Background(), created.ID, 0, 10)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(listed) != 3 {
		t.Fatalf("event count = %d, want 3", len(listed))
	}
	if listed[2].EventType != EventTaskFinished {
		t.Fatalf("last event = %q, want %q", listed[2].EventType, EventTaskFinished)
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

func TestStoreRetryTaskPreservesConcurrencyKey(t *testing.T) {
	store := newTestStore(t)

	original, _, err := store.CreateTask(context.Background(), CreateTaskInput{
		TaskType:       "agent.run",
		ConcurrencyKey: "conv_1",
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if _, _, err := store.ClaimNextTask(context.Background(), "runner-1", 30*time.Second); err != nil {
		t.Fatalf("ClaimNextTask() error = %v", err)
	}
	if _, _, err := store.MarkFailed(context.Background(), original.ID, map[string]any{"message": "boom"}); err != nil {
		t.Fatalf("MarkFailed() error = %v", err)
	}

	retried, _, err := store.RetryTask(context.Background(), original.ID)
	if err != nil {
		t.Fatalf("RetryTask() error = %v", err)
	}
	if retried.ConcurrencyKey != "conv_1" {
		t.Fatalf("retried concurrency key = %q, want %q", retried.ConcurrencyKey, "conv_1")
	}
	if retried.RetryOfTaskID != original.ID {
		t.Fatalf("retry_of_task_id = %q, want %q", retried.RetryOfTaskID, original.ID)
	}
	persisted, err := store.GetTask(context.Background(), retried.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if persisted.ConcurrencyKey != "conv_1" {
		t.Fatalf("persisted concurrency key = %q, want %q", persisted.ConcurrencyKey, "conv_1")
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

func TestStoreMarkFailedIncludesErrorInFinishedEventPayload(t *testing.T) {
	store := newTestStore(t)
	created, _, err := store.CreateTask(context.Background(), CreateTaskInput{TaskType: "agent.run"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if _, _, err := store.ClaimNextTask(context.Background(), "runner-1", 30*time.Second); err != nil {
		t.Fatalf("ClaimNextTask() error = %v", err)
	}
	_, events, err := store.MarkFailed(context.Background(), created.ID, map[string]any{"message": "boom"})
	if err != nil {
		t.Fatalf("MarkFailed() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	payload := decodeJSONRaw(t, events[0].PayloadJSON)
	if payload["status"] != string(StatusFailed) {
		t.Fatalf("status = %#v, want %q", payload["status"], StatusFailed)
	}
	errPayload, ok := payload["error"].(map[string]any)
	if !ok {
		t.Fatalf("error payload = %#v, want object", payload["error"])
	}
	if errPayload["message"] != "boom" {
		t.Fatalf("error.message = %#v, want boom", errPayload["message"])
	}
}

func TestStoreCreateTaskRetriesTransientSQLiteWriteError(t *testing.T) {
	store := newTestStore(t)
	ctx := withTransientWriteErrorContext(context.Background(), t.Name())
	injected := registerTransientWriteErrorOnce(t, store.db, t.Name(), "create", "tasks", "database table is locked")

	task, events, err := store.CreateTask(ctx, CreateTaskInput{TaskType: "agent.run"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	injected.AssertInjected(t)
	if task == nil {
		t.Fatal("task = nil, want created task")
	}
	if task.Status != StatusQueued {
		t.Fatalf("task status = %q, want %q", task.Status, StatusQueued)
	}
	if len(events) != 1 || events[0].EventType != EventTaskCreated {
		t.Fatalf("events = %#v, want single %q event", events, EventTaskCreated)
	}
}

func TestStoreClaimNextTaskRetriesTransientSQLiteWriteError(t *testing.T) {
	store := newTestStore(t)
	created, _, err := store.CreateTask(context.Background(), CreateTaskInput{TaskType: "agent.run"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	ctx := withTransientWriteErrorContext(context.Background(), t.Name())
	injected := registerTransientWriteErrorOnce(t, store.db, t.Name(), "update", "tasks", "database is deadlocked")

	claimed, events, err := store.ClaimNextTask(ctx, "runner-1", time.Minute)
	if err != nil {
		t.Fatalf("ClaimNextTask() error = %v", err)
	}
	injected.AssertInjected(t)
	if claimed == nil {
		t.Fatal("claimed = nil, want claimed task")
	}
	if claimed.ID != created.ID {
		t.Fatalf("claimed id = %q, want %q", claimed.ID, created.ID)
	}
	if claimed.Status != StatusRunning {
		t.Fatalf("claimed status = %q, want %q", claimed.Status, StatusRunning)
	}
	if len(events) != 1 || events[0].EventType != EventTaskStarted {
		t.Fatalf("events = %#v, want single %q event", events, EventTaskStarted)
	}
}

func TestStoreMarkSucceededRetriesTransientSQLiteWriteError(t *testing.T) {
	store := newTestStore(t)
	created, _, err := store.CreateTask(context.Background(), CreateTaskInput{TaskType: "agent.run"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if _, _, err := store.ClaimNextTask(context.Background(), "runner-1", time.Minute); err != nil {
		t.Fatalf("ClaimNextTask() error = %v", err)
	}

	ctx := withTransientWriteErrorContext(context.Background(), t.Name())
	injected := registerTransientWriteErrorOnce(t, store.db, t.Name(), "update", "tasks", "database table is locked")

	finished, events, err := store.MarkSucceeded(ctx, created.ID, map[string]any{"message": "done"})
	if err != nil {
		t.Fatalf("MarkSucceeded() error = %v", err)
	}
	injected.AssertInjected(t)
	if finished.Status != StatusSucceeded {
		t.Fatalf("finished status = %q, want %q", finished.Status, StatusSucceeded)
	}
	if len(events) != 1 || events[0].EventType != EventTaskFinished {
		t.Fatalf("events = %#v, want single %q event", events, EventTaskFinished)
	}
}

func TestStoreUpdateHeartbeatRetriesTransientSQLiteWriteError(t *testing.T) {
	store := newTestStore(t)
	created, _, err := store.CreateTask(context.Background(), CreateTaskInput{TaskType: "agent.run"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if _, _, err := store.ClaimNextTask(context.Background(), "runner-1", time.Minute); err != nil {
		t.Fatalf("ClaimNextTask() error = %v", err)
	}

	ctx := withTransientWriteErrorContext(context.Background(), t.Name())
	injected := registerTransientWriteErrorOnce(t, store.db, t.Name(), "update", "tasks", "interrupted")

	heartbeat, err := store.UpdateHeartbeat(ctx, created.ID, "runner-2", 2*time.Minute)
	if err != nil {
		t.Fatalf("UpdateHeartbeat() error = %v", err)
	}
	injected.AssertInjected(t)
	if heartbeat.RunnerID != "runner-2" {
		t.Fatalf("runner id = %q, want %q", heartbeat.RunnerID, "runner-2")
	}
	if heartbeat.HeartbeatAt == nil || heartbeat.LeaseExpiresAt == nil {
		t.Fatal("heartbeat timestamps = nil, want non-nil")
	}
}
