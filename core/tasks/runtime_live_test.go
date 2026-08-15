package tasks

import (
	"context"
	"testing"
	"time"
)

func TestRuntimeEmitLivePublishesWithoutPersisting(t *testing.T) {
	store := newTestStore(t)
	manager := NewManager(store, ManagerOptions{})
	ctx := context.Background()

	created, err := manager.CreateTask(ctx, CreateTaskInput{TaskType: "agent.run"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}

	ch, unsubscribe := manager.Subscribe(created.ID)
	defer unsubscribe()

	runtime := newRuntime(manager, created.ID)
	if err := runtime.EmitLive(ctx, EventLogMessage, "info", map[string]any{"kind": "text_delta", "text": "he"}); err != nil {
		t.Fatalf("EmitLive() error = %v", err)
	}

	select {
	case event := <-ch:
		if !event.Live || event.Seq != 0 || event.EventType != EventLogMessage {
			t.Fatalf("live event = %#v, want live log.message with seq 0", event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for live event")
	}

	var count int64
	if err := store.db.Model(&TaskEvent{}).Where("task_id = ?", created.ID).Count(&count).Error; err != nil {
		t.Fatalf("count task events error = %v", err)
	}
	if count != 1 {
		t.Fatalf("persisted task events = %d, want 1 (task.created only)", count)
	}
}
