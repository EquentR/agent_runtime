package updater

import (
	"testing"
	"time"
)

func TestMaintenanceGateOwnsSingleOperation(t *testing.T) {
	gate := NewMaintenanceGate()
	started := time.Date(2026, 7, 19, 1, 2, 3, 0, time.UTC)
	if err := gate.Enter("op-1", started); err != nil {
		t.Fatalf("Enter() error = %v", err)
	}
	if err := gate.Enter("op-2", started); err == nil {
		t.Fatal("Enter(second) error = nil, want conflict")
	}
	status := gate.Status()
	if !status.Active || status.OperationID != "op-1" || !status.StartedAt.Equal(started) {
		t.Fatalf("Status() = %#v", status)
	}
	if !gate.Exit("op-1") || gate.Status().Active {
		t.Fatal("Exit(op-1) did not clear maintenance")
	}
}
