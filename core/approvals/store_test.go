package approvals

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestApprovalStoreCreateListGetAndResolveOnce(t *testing.T) {
	store := newTestStore(t)

	created, err := store.CreateApproval(context.Background(), CreateApprovalInput{
		TaskID:           "task-1",
		ConversationID:   "conv-1",
		StepIndex:        3,
		ToolCallID:       "call-1",
		ToolName:         "bash",
		ArgumentsSummary: "rm -rf /tmp/demo",
		RiskLevel:        "high",
		Reason:           "dangerous filesystem mutation",
	})
	if err != nil {
		t.Fatalf("CreateApproval() error = %v", err)
	}
	if created.ID == "" {
		t.Fatal("created approval id = empty, want non-empty")
	}
	if created.Status != StatusPending {
		t.Fatalf("created status = %q, want %q", created.Status, StatusPending)
	}

	listed, err := store.ListTaskApprovals(context.Background(), "task-1")
	if err != nil {
		t.Fatalf("ListTaskApprovals() error = %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("len(listed) = %d, want 1", len(listed))
	}
	if listed[0].ID != created.ID {
		t.Fatalf("listed approval id = %q, want %q", listed[0].ID, created.ID)
	}

	loaded, err := store.GetApproval(context.Background(), "task-1", created.ID)
	if err != nil {
		t.Fatalf("GetApproval() error = %v", err)
	}
	if loaded.ToolName != "bash" {
		t.Fatalf("loaded tool_name = %q, want %q", loaded.ToolName, "bash")
	}

	resolved, changed, err := store.ResolveApproval(context.Background(), "task-1", created.ID, ResolveApprovalInput{
		Decision:   DecisionApprove,
		Reason:     "looks safe enough",
		DecisionBy: "alice",
	})
	if err != nil {
		t.Fatalf("ResolveApproval() error = %v", err)
	}
	if !changed {
		t.Fatal("ResolveApproval() changed = false, want true")
	}
	if resolved.Status != StatusApproved {
		t.Fatalf("resolved status = %q, want %q", resolved.Status, StatusApproved)
	}
	if resolved.DecisionBy != "alice" {
		t.Fatalf("resolved decision_by = %q, want %q", resolved.DecisionBy, "alice")
	}
	if resolved.DecisionReason != "looks safe enough" {
		t.Fatalf("resolved decision_reason = %q, want %q", resolved.DecisionReason, "looks safe enough")
	}
	if resolved.DecisionAt == nil {
		t.Fatal("resolved decision_at = nil, want timestamp")
	}

	resolvedAgain, changed, err := store.ResolveApproval(context.Background(), "task-1", created.ID, ResolveApprovalInput{
		Decision:   DecisionReject,
		Reason:     "too late",
		DecisionBy: "bob",
	})
	if err != nil {
		t.Fatalf("ResolveApproval() second call error = %v", err)
	}
	if changed {
		t.Fatal("ResolveApproval() second changed = true, want false")
	}
	if resolvedAgain.Status != StatusApproved {
		t.Fatalf("resolved second status = %q, want %q", resolvedAgain.Status, StatusApproved)
	}
	if resolvedAgain.DecisionBy != "alice" {
		t.Fatalf("resolved second decision_by = %q, want %q", resolvedAgain.DecisionBy, "alice")
	}
}

func TestApprovalStoreCancelPendingApprovalsByTask(t *testing.T) {
	store := newTestStore(t)

	first, err := store.CreateApproval(context.Background(), CreateApprovalInput{TaskID: "task-1", ToolCallID: "call-1", ToolName: "bash"})
	if err != nil {
		t.Fatalf("CreateApproval() first error = %v", err)
	}
	second, err := store.CreateApproval(context.Background(), CreateApprovalInput{TaskID: "task-1", ToolCallID: "call-2", ToolName: "delete_file"})
	if err != nil {
		t.Fatalf("CreateApproval() second error = %v", err)
	}
	_, _, err = store.ResolveApproval(context.Background(), "task-1", second.ID, ResolveApprovalInput{
		Decision:   DecisionReject,
		Reason:     "denied",
		DecisionBy: "alice",
	})
	if err != nil {
		t.Fatalf("ResolveApproval() error = %v", err)
	}

	cancelledCount, err := store.CancelPendingApprovalsByTask(context.Background(), "task-1")
	if err != nil {
		t.Fatalf("CancelPendingApprovalsByTask() error = %v", err)
	}
	if cancelledCount != 1 {
		t.Fatalf("cancelled_count = %d, want 1", cancelledCount)
	}

	loadedFirst, err := store.GetApproval(context.Background(), "task-1", first.ID)
	if err != nil {
		t.Fatalf("GetApproval() first error = %v", err)
	}
	if loadedFirst.Status != StatusCancelled {
		t.Fatalf("first status = %q, want %q", loadedFirst.Status, StatusCancelled)
	}

	loadedSecond, err := store.GetApproval(context.Background(), "task-1", second.ID)
	if err != nil {
		t.Fatalf("GetApproval() second error = %v", err)
	}
	if loadedSecond.Status != StatusRejected {
		t.Fatalf("second status = %q, want %q", loadedSecond.Status, StatusRejected)
	}
}

func TestApprovalStoreListTaskApprovalsReturnsNewestFirst(t *testing.T) {
	store := newTestStore(t)

	first, err := store.CreateApproval(context.Background(), CreateApprovalInput{TaskID: "task-1", ToolCallID: "call-1", ToolName: "bash"})
	if err != nil {
		t.Fatalf("CreateApproval() first error = %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	second, err := store.CreateApproval(context.Background(), CreateApprovalInput{TaskID: "task-1", ToolCallID: "call-2", ToolName: "delete_file"})
	if err != nil {
		t.Fatalf("CreateApproval() second error = %v", err)
	}

	listed, err := store.ListTaskApprovals(context.Background(), "task-1")
	if err != nil {
		t.Fatalf("ListTaskApprovals() error = %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("len(listed) = %d, want 2", len(listed))
	}
	if listed[0].ID != second.ID {
		t.Fatalf("listed[0].ID = %q, want %q", listed[0].ID, second.ID)
	}
	if listed[1].ID != first.ID {
		t.Fatalf("listed[1].ID = %q, want %q", listed[1].ID, first.ID)
	}
}

func TestApprovalStoreCreateApprovalDeduplicatesConcurrentRequestsByTaskAndToolCall(t *testing.T) {
	store := newTestStore(t)

	const goroutineCount = 6
	results := make(chan *ToolApproval, goroutineCount)
	errorsCh := make(chan error, goroutineCount)
	start := make(chan struct{})
	var wg sync.WaitGroup

	for range goroutineCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			approval, err := store.CreateApproval(context.Background(), CreateApprovalInput{
				TaskID:           "task-1",
				ConversationID:   "conv-1",
				StepIndex:        1,
				ToolCallID:       "call-1",
				ToolName:         "bash",
				ArgumentsSummary: "rm -rf /tmp/demo",
				RiskLevel:        "high",
				Reason:           "dangerous filesystem mutation",
			})
			if err != nil {
				errorsCh <- err
				return
			}
			results <- approval
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errorsCh)

	for err := range errorsCh {
		if err != nil {
			t.Fatalf("CreateApproval() concurrent error = %v", err)
		}
	}

	var firstID string
	for approval := range results {
		if approval == nil {
			t.Fatal("approval result = nil, want persisted approval")
		}
		if firstID == "" {
			firstID = approval.ID
			continue
		}
		if approval.ID != firstID {
			t.Fatalf("approval id = %q, want %q", approval.ID, firstID)
		}
	}

	listed, err := store.ListTaskApprovals(context.Background(), "task-1")
	if err != nil {
		t.Fatalf("ListTaskApprovals() error = %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("len(listed) = %d, want 1", len(listed))
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}

	store := NewStore(db)
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	return store
}
