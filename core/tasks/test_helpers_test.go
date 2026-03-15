package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// newTestStore 为任务测试构造独立的内存数据库与 Store。
func newTestStore(t *testing.T) *Store {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}

	store := NewStore(db)
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	return store
}

// decodeJSONRaw 将原始 JSON 解码为 map，便于测试断言。
func decodeJSONRaw(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()

	if len(raw) == 0 {
		return map[string]any{}
	}

	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal json: %v", err)
	}
	return got
}

// mustParseTime 断言时间指针非空并返回其值。
func mustParseTime(t *testing.T, value *time.Time) time.Time {
	t.Helper()
	if value == nil {
		t.Fatal("time is nil")
	}
	return *value
}

// waitForTaskStatus 轮询等待任务进入目标状态。
func waitForTaskStatus(t *testing.T, ctx context.Context, manager *Manager, taskID string, want Status) *Task {
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

// waitForEvent 从订阅通道中等待指定类型的事件。
func waitForEvent(t *testing.T, ch <-chan TaskEvent, want ...string) TaskEvent {
	t.Helper()

	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()

	for {
		select {
		case event, ok := <-ch:
			if !ok {
				t.Fatal("event channel closed before expected event")
			}
			if len(want) == 0 || slices.Contains(want, event.EventType) {
				return event
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for events %v", want)
		}
	}
}
