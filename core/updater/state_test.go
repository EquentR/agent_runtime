package updater

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestOperationPhaseAllowsOnlyDeclaredTransitions(t *testing.T) {
	allowed := [][2]OperationPhase{
		{PhaseIdle, PhaseChecking},
		{PhaseChecking, PhaseAvailable},
		{PhaseAvailable, PhaseDownloading},
		{PhaseDownloading, PhaseVerifying},
		{PhaseVerifying, PhaseStaged},
		{PhaseStaged, PhaseDraining},
		{PhaseDraining, PhaseBackingUp},
		{PhaseBackingUp, PhaseReplacing},
		{PhaseReplacing, PhaseRestarting},
		{PhaseRestarting, PhaseHealthCheck},
		{PhaseHealthCheck, PhaseSucceeded},
		{PhaseHealthCheck, PhaseRollingBack},
		{PhaseRollingBack, PhaseRolledBack},
	}
	for _, transition := range allowed {
		if !CanTransition(transition[0], transition[1]) {
			t.Fatalf("CanTransition(%q, %q) = false, want true", transition[0], transition[1])
		}
	}
	if CanTransition(PhaseIdle, PhaseReplacing) {
		t.Fatal("CanTransition(idle, replacing) = true, want false")
	}
}

func TestStateStoreUsesGenerationAndPersistsEvents(t *testing.T) {
	store, err := NewStateStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStateStore() error = %v", err)
	}
	now := time.Date(2026, 7, 19, 1, 2, 3, 0, time.UTC)
	saved, err := store.Save(0, OperationState{OperationID: "op-1", Phase: PhaseChecking, TargetVersion: "v1.2.3", UpdatedAt: now})
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if saved.Generation != 1 {
		t.Fatalf("Generation = %d, want 1", saved.Generation)
	}
	if _, err := store.Save(0, OperationState{OperationID: "op-2", Phase: PhaseChecking}); !errors.Is(err, ErrGenerationConflict) {
		t.Fatalf("Save(stale) error = %v, want ErrGenerationConflict", err)
	}
	loaded, err := store.Load()
	if err != nil || loaded.OperationID != "op-1" || loaded.Generation != 1 {
		t.Fatalf("Load() = %#v, %v", loaded, err)
	}
	if err := store.AppendEvent(OperationEvent{OperationID: "op-1", Phase: PhaseChecking, At: now, Message: "checking"}); err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}
	events, err := store.Events()
	if err != nil || len(events) != 2 || events[1].Message != "checking" {
		t.Fatalf("Events() = %#v, %v", events, err)
	}
	if filepath.Base(store.StatePath()) != "state.json" {
		t.Fatalf("StatePath() = %q", store.StatePath())
	}
}

func TestManualRollbackCanRestoreEligiblePreviousPhase(t *testing.T) {
	for _, phase := range []OperationPhase{PhaseIdle, PhaseAvailable, PhaseSucceeded} {
		if !CanTransition(PhaseRollingBack, phase) {
			t.Errorf("CanTransition(%q, %q) = false", PhaseRollingBack, phase)
		}
	}
}

func TestProcessLockRejectsConcurrentHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operation.lock")
	first, err := AcquireProcessLock(path)
	if err != nil {
		t.Fatalf("AcquireProcessLock(first) error = %v", err)
	}
	defer first.Close()
	if _, err := AcquireProcessLock(path); !errors.Is(err, ErrOperationLocked) {
		t.Fatalf("AcquireProcessLock(second) error = %v, want ErrOperationLocked", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first.Close() error = %v", err)
	}
	second, err := AcquireProcessLock(path)
	if err != nil {
		t.Fatalf("AcquireProcessLock(after close) error = %v", err)
	}
	_ = second.Close()
}
