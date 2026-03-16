package memory

import (
	"context"
	"errors"
	"fmt"
	"testing"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestLongTermGetSummaryRequiresUserID(t *testing.T) {
	mgr, err := NewLongTermManager(newTestMemoryDB(t), LongTermOptions{})
	if err != nil {
		t.Fatalf("NewLongTermManager() error = %v", err)
	}

	_, err = mgr.GetSummary(context.Background(), "")
	if err == nil {
		t.Fatal("GetSummary() error = nil, want user id error")
	}
}

func TestLongTermGetSummaryCreatesUserScopedRecord(t *testing.T) {
	mgr, err := NewLongTermManager(newTestMemoryDB(t), LongTermOptions{})
	if err != nil {
		t.Fatalf("NewLongTermManager() error = %v", err)
	}

	summary, err := mgr.GetSummary(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("GetSummary() error = %v", err)
	}
	if summary != "" {
		t.Fatalf("GetSummary() = %q, want empty summary", summary)
	}

	var count int64
	if err := mgr.db.Model(&LongTermMemory{}).Where("user_id = ?", "user-1").Count(&count).Error; err != nil {
		t.Fatalf("count long-term records: %v", err)
	}
	if count != 1 {
		t.Fatalf("user record count = %d, want 1", count)
	}
}

func TestLongTermFlushWithoutMessagesSkipsCompression(t *testing.T) {
	compressCalls := 0
	mgr, err := NewLongTermManager(newTestMemoryDB(t), LongTermOptions{
		Compressor: func(_ context.Context, request LongTermCompressionRequest) (string, error) {
			compressCalls++
			return "unused", nil
		},
	})
	if err != nil {
		t.Fatalf("NewLongTermManager() error = %v", err)
	}

	summary, err := mgr.Flush(context.Background(), LongTermFlushRequest{UserID: "user-1"})
	if err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if compressCalls != 0 {
		t.Fatalf("compressor called %d times, want 0", compressCalls)
	}
	if summary != "" {
		t.Fatalf("Flush() = %q, want empty summary", summary)
	}
}

func TestLongTermFlushPersistsCompressedSummary(t *testing.T) {
	var seen LongTermCompressionRequest
	mgr, err := NewLongTermManager(newTestMemoryDB(t), LongTermOptions{
		Compressor: func(_ context.Context, request LongTermCompressionRequest) (string, error) {
			seen = request
			return "User Preferences\n- likes concise answers", nil
		},
	})
	if err != nil {
		t.Fatalf("NewLongTermManager() error = %v", err)
	}

	got, err := mgr.Flush(context.Background(), LongTermFlushRequest{
		UserID:   "user-1",
		Messages: []model.Message{{Role: model.RoleUser, Content: "以后回答请简洁一点"}},
	})
	if err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if seen.UserID != "user-1" {
		t.Fatalf("LongTermCompressionRequest.UserID = %q, want user-1", seen.UserID)
	}
	if seen.PreviousSummary != "" {
		t.Fatalf("LongTermCompressionRequest.PreviousSummary = %q, want empty", seen.PreviousSummary)
	}
	if seen.Instruction == "" {
		t.Fatal("LongTermCompressionRequest.Instruction = empty, want default instruction")
	}
	if got != "User Preferences\n- likes concise answers" {
		t.Fatalf("Flush() = %q, want persisted summary", got)
	}

	stored, err := mgr.GetSummary(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("GetSummary() error = %v", err)
	}
	if stored != got {
		t.Fatalf("stored summary = %q, want %q", stored, got)
	}
}

func TestLongTermFlushKeepsPreviousSummaryWhenCompressionFails(t *testing.T) {
	mgr, err := NewLongTermManager(newTestMemoryDB(t), LongTermOptions{
		Compressor: func(_ context.Context, request LongTermCompressionRequest) (string, error) {
			if request.PreviousSummary == "" {
				return "Persistent Facts\n- user works on runtime project", nil
			}
			return "", errors.New("llm unavailable")
		},
	})
	if err != nil {
		t.Fatalf("NewLongTermManager() error = %v", err)
	}

	if _, err := mgr.Flush(context.Background(), LongTermFlushRequest{
		UserID:   "user-1",
		Messages: []model.Message{{Role: model.RoleUser, Content: "记住我在维护 runtime 项目"}},
	}); err != nil {
		t.Fatalf("initial Flush() error = %v", err)
	}

	_, err = mgr.Flush(context.Background(), LongTermFlushRequest{
		UserID:   "user-1",
		Messages: []model.Message{{Role: model.RoleUser, Content: "再记一点别的"}},
	})
	if err == nil {
		t.Fatal("Flush() error = nil, want compressor failure")
	}

	stored, err := mgr.GetSummary(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("GetSummary() error = %v", err)
	}
	if stored != "Persistent Facts\n- user works on runtime project" {
		t.Fatalf("stored summary = %q, want previous successful summary", stored)
	}
}

func newTestMemoryDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := db.AutoMigrate(&LongTermMemory{}); err != nil {
		t.Fatalf("auto migrate long-term memory: %v", err)
	}
	return db
}
