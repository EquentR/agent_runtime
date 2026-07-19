package updater

import (
	"fmt"
	"sync"
	"time"
)

type MaintenanceStatus struct {
	Active      bool      `json:"active"`
	OperationID string    `json:"operation_id,omitempty"`
	StartedAt   time.Time `json:"started_at,omitempty"`
}

type MaintenanceGate struct {
	mu          sync.RWMutex
	active      bool
	operationID string
	startedAt   time.Time
}

func NewMaintenanceGate() *MaintenanceGate {
	return &MaintenanceGate{}
}

func (g *MaintenanceGate) Enter(operationID string, startedAt time.Time) error {
	if g == nil {
		return fmt.Errorf("maintenance gate is not configured")
	}
	if operationID == "" {
		return fmt.Errorf("maintenance operation ID is required")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.active {
		return fmt.Errorf("maintenance operation %q is already active", g.operationID)
	}
	g.active = true
	g.operationID = operationID
	g.startedAt = startedAt.UTC()
	return nil
}

func (g *MaintenanceGate) Exit(operationID string) bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.active || g.operationID != operationID {
		return false
	}
	g.active = false
	g.operationID = ""
	g.startedAt = time.Time{}
	return true
}

func (g *MaintenanceGate) Active() bool {
	return g != nil && g.Status().Active
}

func (g *MaintenanceGate) Status() MaintenanceStatus {
	if g == nil {
		return MaintenanceStatus{}
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return MaintenanceStatus{Active: g.active, OperationID: g.operationID, StartedAt: g.startedAt}
}
