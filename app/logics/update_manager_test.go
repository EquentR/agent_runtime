package logics

import (
	"context"
	"crypto/ed25519"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/EquentR/agent_runtime/app/models"
	coreupdater "github.com/EquentR/agent_runtime/core/updater"
)

type fakeUpdateTaskDrainer struct {
	waitErr     error
	paused      bool
	cancelCalls int
	activeCount int
}

func (f *fakeUpdateTaskDrainer) PauseClaims()                      { f.paused = true }
func (f *fakeUpdateTaskDrainer) ResumeClaims()                     { f.paused = false }
func (f *fakeUpdateTaskDrainer) WaitForIdle(context.Context) error { return f.waitErr }
func (f *fakeUpdateTaskDrainer) CancelActiveTasks()                { f.cancelCalls++ }
func (f *fakeUpdateTaskDrainer) ActiveTaskCount() int              { return f.activeCount }

func TestUpdateManagerStatusReportsCapabilityAndOperation(t *testing.T) {
	state, err := coreupdater.NewStateStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewUpdateManager(UpdateManagerOptions{
		Current:    coreupdater.BuildInfo{Version: "v1.2.3", Commit: "abc", Distribution: coreupdater.DistributionContainer, GOOS: "linux", GOARCH: "amd64"},
		StateStore: state,
	})
	if err != nil {
		t.Fatal(err)
	}
	status, err := manager.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Current.Version != "v1.2.3" || status.Capable {
		t.Fatalf("status = %#v", status)
	}
	if status.State.Phase != coreupdater.PhaseIdle {
		t.Fatalf("phase = %q", status.State.Phase)
	}
	_ = time.Now()
}

func TestUpdateManagerDrainFailureMarksOperationFailedAndAllowsRetry(t *testing.T) {
	root := t.TempDir()
	stateStore, err := coreupdater.NewStateStore(filepath.Join(root, "data", "updates"))
	if err != nil {
		t.Fatal(err)
	}
	drainer := &fakeUpdateTaskDrainer{waitErr: context.DeadlineExceeded}
	manager, err := NewUpdateManager(UpdateManagerOptions{
		Current:     coreupdater.BuildInfo{Version: "v1.0.0", Distribution: coreupdater.DistributionRelease, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH},
		StateStore:  stateStore,
		TaskDrainer: drainer,
		InstallRoot: root,
		TrustedKeys: map[string]ed25519.PublicKey{"test": make(ed25519.PublicKey, ed25519.PublicKeySize)},
		JobSecret:   []byte("01234567890123456789012345678901"),
		StageRelease: func(_ context.Context, options coreupdater.StageOptions) (coreupdater.StagedRelease, error) {
			if err := os.MkdirAll(options.Root, 0o700); err != nil {
				return coreupdater.StagedRelease{}, err
			}
			return coreupdater.StagedRelease{Root: options.Root}, nil
		},
		LaunchHelper: func(context.Context, string) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	manager.lastCheck = &coreupdater.CheckResult{UpdateAvailable: true, Release: coreupdater.Release{TagName: "v1.1.0"}}
	user := &models.User{ID: 1}
	session := &models.UserSession{ID: "session-1", UserID: 1}
	issue := func(action string) string {
		token, _, issueErr := manager.authorizer.Issue(coreupdater.AuthorizationBinding{UserID: user.ID, SessionID: session.ID, Action: action, Target: "v1.1.0"}, time.Minute)
		if issueErr != nil {
			t.Fatal(issueErr)
		}
		return token
	}

	_, err = manager.Install(context.Background(), user, session, issue("install"), "op-drain-failed", "v1.1.0", coreupdater.BackupModeCompact)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Install() error = %v, want deadline exceeded", err)
	}
	state, err := stateStore.Load()
	if err != nil {
		t.Fatal(err)
	}
	if state.Phase != coreupdater.PhaseFailed || manager.maintenance.Active() || drainer.paused {
		t.Fatalf("after drain failure state=%#v maintenance=%v paused=%v", state, manager.maintenance.Active(), drainer.paused)
	}

	drainer.waitErr = nil
	if _, err := manager.ForceInstall(context.Background(), user, session, issue("install"), "op-force", "v1.1.0", coreupdater.BackupModeCompact); !errors.Is(err, coreupdater.ErrOperationAuthorization) {
		t.Fatalf("ForceInstall() with install token error = %v, want authorization rejection", err)
	}
	if _, err := manager.ForceInstall(context.Background(), user, session, issue("force_install"), "op-force", "v1.1.0", coreupdater.BackupModeCompact); err != nil {
		t.Fatalf("ForceInstall() error = %v", err)
	}
	if drainer.cancelCalls != 1 {
		t.Fatalf("CancelActiveTasks() calls = %d, want 1", drainer.cancelCalls)
	}
	if state, err := manager.ForceInstall(context.Background(), user, session, "already-consumed", "op-force", "v1.1.0", coreupdater.BackupModeCompact); err != nil || state.OperationID != "op-force" {
		t.Fatalf("idempotent ForceInstall() state=%#v error=%v", state, err)
	}
}

func TestUpdateManagerCapabilityErrorBlocksStaging(t *testing.T) {
	stateStore, err := coreupdater.NewStateStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	staged := false
	manager, err := NewUpdateManager(UpdateManagerOptions{
		Current:         coreupdater.BuildInfo{Version: "v1.0.0", Distribution: coreupdater.DistributionRelease, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH},
		StateStore:      stateStore,
		TrustedKeys:     map[string]ed25519.PublicKey{"test": make(ed25519.PublicKey, ed25519.PublicKeySize)},
		CapabilityError: "update configuration is invalid",
		StageRelease: func(context.Context, coreupdater.StageOptions) (coreupdater.StagedRelease, error) {
			staged = true
			return coreupdater.StagedRelease{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	status, err := manager.Status(context.Background())
	if err != nil || status.Capable {
		t.Fatalf("Status() = %#v, %v", status, err)
	}
	_, err = manager.Install(context.Background(), &models.User{ID: 1}, &models.UserSession{ID: "session", UserID: 1}, "unused", "op-invalid-config", "v1.1.0", coreupdater.BackupModeCompact)
	if !errors.Is(err, ErrUpdateUnavailable) || staged {
		t.Fatalf("Install() error=%v staged=%v", err, staged)
	}
}

func TestUpdateManagerCompletedRollbackIsIdempotentByOperationID(t *testing.T) {
	store, err := coreupdater.NewStateStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	state, _ := store.Save(0, coreupdater.OperationState{Phase: coreupdater.PhaseChecking})
	state, _ = store.Save(state.Generation, coreupdater.OperationState{Phase: coreupdater.PhaseIdle, BackupID: "backup-old", CurrentVersion: "v1.0.0"})
	state, _ = store.Save(state.Generation, coreupdater.OperationState{OperationID: "rollback-op", Action: "rollback", RequestTarget: "backup-old", Phase: coreupdater.PhaseRollingBack, BackupID: "backup-old", CurrentVersion: "v1.1.0", TargetVersion: "v1.0.0"})
	state, _ = store.Save(state.Generation, coreupdater.OperationState{OperationID: "rollback-op", Action: "rollback", RequestTarget: "backup-old", Phase: coreupdater.PhaseRolledBack, BackupID: "rollback-op", CurrentVersion: "v1.1.0", TargetVersion: "v1.0.0"})
	manager, err := NewUpdateManager(UpdateManagerOptions{Current: coreupdater.BuildInfo{Version: "v1.0.0"}, StateStore: store})
	if err != nil {
		t.Fatal(err)
	}
	got, err := manager.Rollback(context.Background(), &models.User{ID: 1}, &models.UserSession{ID: "session", UserID: 1}, "already-consumed", "rollback-op", "backup-old")
	if err != nil || got.Phase != coreupdater.PhaseRolledBack {
		t.Fatalf("Rollback() state=%#v error=%v", got, err)
	}
}

func TestUpdateManagerStatusIncludesUpgradePreflight(t *testing.T) {
	root := t.TempDir()
	store, err := coreupdater.NewStateStore(filepath.Join(root, "data", "updates"))
	if err != nil {
		t.Fatal(err)
	}
	assetName := "ice_art_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	if runtime.GOOS == "windows" {
		assetName = "ice_art_" + runtime.GOOS + "_" + runtime.GOARCH + ".zip"
	}
	manager, err := NewUpdateManager(UpdateManagerOptions{
		Current:     coreupdater.BuildInfo{Version: "v1.0.0", Distribution: coreupdater.DistributionRelease, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH},
		StateStore:  store,
		TaskDrainer: &fakeUpdateTaskDrainer{activeCount: 2},
		InstallRoot: root,
		TrustedKeys: map[string]ed25519.PublicKey{"test": make(ed25519.PublicKey, ed25519.PublicKeySize)},
	})
	if err != nil {
		t.Fatal(err)
	}
	manager.lastCheck = &coreupdater.CheckResult{UpdateAvailable: true, Release: coreupdater.Release{TagName: "v1.1.0", Assets: []coreupdater.Asset{{Name: assetName, Size: 1000}, {Name: "SHA256SUMS", Size: 200}, {Name: "SHA256SUMS.sig", Size: 100}}}}
	status, err := manager.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Preflight == nil || status.Preflight.EstimatedDownloadBytes != 1300 || status.Preflight.AvailableBytes == 0 || status.Preflight.ActiveTaskCount != 2 {
		t.Fatalf("preflight = %#v", status.Preflight)
	}
}
