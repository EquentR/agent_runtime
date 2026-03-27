package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type transientWriteErrorContextKey struct{}

var transientWriteErrorMarkerKey transientWriteErrorContextKey

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

type transientWriteErrorCallback struct {
	injected bool
	remove   func() error
}

func withTransientWriteErrorContext(ctx context.Context, marker string) context.Context {
	return context.WithValue(ctx, transientWriteErrorMarkerKey, marker)
}

func registerTransientWriteErrorOnce(t *testing.T, db *gorm.DB, marker string, operation string, table string, message string) *transientWriteErrorCallback {
	t.Helper()

	callbackName := fmt.Sprintf("test:tasks:%s:%s:%s", strings.ReplaceAll(t.Name(), "/", "_"), operation, table)
	callback := &transientWriteErrorCallback{}
	hook := func(tx *gorm.DB) {
		if callback.injected || tx.Statement == nil || tx.Statement.Schema == nil {
			return
		}
		if tx.Statement.Schema.Table != table {
			return
		}
		if tx.Statement.Context == nil || tx.Statement.Context.Value(transientWriteErrorMarkerKey) != marker {
			return
		}
		callback.injected = true
		tx.AddError(fmt.Errorf("sqlite transient write failure: %s", message))
	}

	switch operation {
	case "create":
		if err := db.Callback().Create().Before("gorm:create").Register(callbackName, hook); err != nil {
			t.Fatalf("register create callback: %v", err)
		}
		callback.remove = func() error {
			return db.Callback().Create().Remove(callbackName)
		}
	case "update":
		if err := db.Callback().Update().Before("gorm:update").Register(callbackName, hook); err != nil {
			t.Fatalf("register update callback: %v", err)
		}
		callback.remove = func() error {
			return db.Callback().Update().Remove(callbackName)
		}
	default:
		t.Fatalf("unsupported callback operation %q", operation)
	}

	t.Cleanup(func() {
		if err := callback.remove(); err != nil {
			t.Fatalf("remove %s callback: %v", operation, err)
		}
	})

	return callback
}

func (c *transientWriteErrorCallback) AssertInjected(t *testing.T) {
	t.Helper()
	if c == nil || !c.injected {
		t.Fatal("transient write error was not injected")
	}
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
