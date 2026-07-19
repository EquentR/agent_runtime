package updater

import "testing"

func TestStateStoreRecoversInterruptedOperationFromRunningVersion(t *testing.T) {
	for name, test := range map[string]struct {
		phase   OperationPhase
		running string
		want    OperationPhase
	}{
		"target is healthy after replacement":     {phase: PhaseRestarting, running: "v1.2.3", want: PhaseSucceeded},
		"old version survived before replacement": {phase: PhaseBackingUp, running: "v1.2.2", want: PhaseFailed},
		"rollback restored either known version":  {phase: PhaseRollingBack, running: "v1.2.2", want: PhaseRolledBack},
		"unknown running version needs recovery":  {phase: PhaseReplacing, running: "v9.9.9", want: PhaseRecoveryRequired},
	} {
		t.Run(name, func(t *testing.T) {
			store, _ := NewStateStore(t.TempDir())
			state, _ := store.Save(0, OperationState{OperationID: "op-1", Phase: PhaseChecking, CurrentVersion: "v1.2.2", TargetVersion: "v1.2.3"})
			path := []OperationPhase{PhaseAvailable, PhaseDownloading, PhaseVerifying, PhaseStaged, PhaseDraining, PhaseBackingUp, PhaseReplacing, PhaseRestarting, PhaseHealthCheck, PhaseRollingBack}
			for _, phase := range path {
				if phase == PhaseRollingBack && test.phase != PhaseRollingBack {
					break
				}
				if !CanTransition(state.Phase, phase) {
					continue
				}
				state, _ = store.Save(state.Generation, OperationState{OperationID: "op-1", Phase: phase, CurrentVersion: "v1.2.2", TargetVersion: "v1.2.3", BackupID: "op-1"})
				if phase == test.phase {
					break
				}
			}
			recovered, err := store.RecoverInterrupted(test.running)
			if err != nil || recovered.Phase != test.want {
				t.Fatalf("RecoverInterrupted() = %#v, %v, want %s", recovered, err, test.want)
			}
		})
	}
}
