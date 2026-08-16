package tasks

import (
	"context"
	"encoding/json"
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

func TestRuntimeEmitWithRuntimeEventPayloadPersistsSummaryAndPublishesLiveEventWithoutSeq(t *testing.T) {
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
	persistent := map[string]any{"tool_call_id": "call_1", "tool_name": "lookup_weather", "arguments_length": 2}
	live := map[string]any{"ToolCallID": "call_1", "ToolName": "lookup_weather", "Arguments": `{}`}
	if err := runtime.Emit(ctx, EventToolStarted, "info", RuntimeEventPayload{Persistent: persistent, Live: live}); err != nil {
		t.Fatalf("Emit() error = %v", err)
	}

	select {
	case event := <-ch:
		if !event.Live {
			t.Fatalf("live event = %#v, want live=true", event)
		}
		if event.Seq != 0 {
			t.Fatalf("live event seq = %d, want 0 so reconnect replay is not skipped", event.Seq)
		}
		var payload map[string]any
		if err := json.Unmarshal(event.PayloadJSON, &payload); err != nil {
			t.Fatalf("decode live payload error = %v", err)
		}
		if payload["Arguments"] != `{}` {
			t.Fatalf("live payload = %#v, want full arguments", payload)
		}
		if _, ok := payload["arguments_length"]; ok {
			t.Fatalf("live payload = %#v, must not contain persistent summary only fields", payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for live tool event")
	}

	events, err := store.ListEvents(ctx, created.ID, 0, 0)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("persisted task events = %d, want 2 (created + tool summary)", len(events))
	}
	persisted := events[1]
	if persisted.Live || persisted.Seq == 0 {
		t.Fatalf("persisted event = %#v, want non-live with allocated seq", persisted)
	}
	var persistedPayload map[string]any
	if err := json.Unmarshal(persisted.PayloadJSON, &persistedPayload); err != nil {
		t.Fatalf("decode persisted payload error = %v", err)
	}
	if persistedPayload["tool_call_id"] != "call_1" || persistedPayload["arguments_length"] != float64(2) {
		t.Fatalf("persisted payload = %#v, want summary only", persistedPayload)
	}
	if _, ok := persistedPayload["Arguments"]; ok {
		t.Fatalf("persisted payload = %#v, must not contain live full arguments", persistedPayload)
	}
}
