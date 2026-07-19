package tasks

import (
	"context"
	"testing"
	"time"
)

func TestManagerMaintenanceControlsClaimsAndActiveExecutors(t *testing.T) {
	manager := NewManager(nil, ManagerOptions{})
	cancelled := make(chan struct{}, 1)
	manager.setActiveCancel("task-1", func() { cancelled <- struct{}{} })
	manager.PauseClaims()
	if !manager.ClaimsPaused() || manager.ActiveTaskCount() != 1 {
		t.Fatalf("paused/count = %v/%d, want true/1", manager.ClaimsPaused(), manager.ActiveTaskCount())
	}
	manager.CancelActiveTasks()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("CancelActiveTasks() did not cancel executor")
	}
	manager.clearActiveCancel("task-1")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := manager.WaitForIdle(ctx); err != nil {
		t.Fatalf("WaitForIdle() error = %v", err)
	}
	manager.ResumeClaims()
	if manager.ClaimsPaused() {
		t.Fatal("ClaimsPaused() = true after ResumeClaims")
	}
}
