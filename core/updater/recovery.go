package updater

import (
	"fmt"
	"time"
)

func (s *StateStore) RecoverInterrupted(runningVersion string) (OperationState, error) {
	if s == nil {
		return OperationState{}, fmt.Errorf("state store is not configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	lock, err := AcquireProcessLock(s.lockPath)
	if err != nil {
		return OperationState{}, err
	}
	defer lock.Close()
	state, err := s.loadUnlocked()
	if err != nil {
		return OperationState{}, err
	}
	if state.Phase == PhaseIdle || state.Phase == PhaseAvailable || state.Phase == PhaseSucceeded || state.Phase == PhaseFailed || state.Phase == PhaseRolledBack || state.Phase == PhaseRecoveryRequired {
		return state, nil
	}
	next := PhaseRecoveryRequired
	if state.Phase == PhaseRollingBack && (runningVersion == state.CurrentVersion || runningVersion == state.TargetVersion) {
		next = PhaseRolledBack
	} else if runningVersion == state.TargetVersion && (state.Phase == PhaseReplacing || state.Phase == PhaseRestarting || state.Phase == PhaseHealthCheck) {
		next = PhaseSucceeded
	} else if runningVersion == state.CurrentVersion {
		next = PhaseFailed
	}
	state.Phase = next
	state.Generation++
	state.UpdatedAt = time.Now().UTC()
	if next == PhaseRecoveryRequired {
		state.Error = "interrupted update does not match the running build"
	} else {
		state.Error = ""
	}
	if err := writeJSONAtomic(s.statePath, state, 0o600); err != nil {
		return OperationState{}, err
	}
	if err := s.owner.apply(s.statePath); err != nil {
		return OperationState{}, err
	}
	if err := s.appendEventUnlocked(OperationEvent{OperationID: state.OperationID, Phase: state.Phase, At: state.UpdatedAt, Message: state.Error}); err != nil {
		return OperationState{}, err
	}
	return state, nil
}
