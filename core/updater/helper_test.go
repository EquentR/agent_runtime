package updater

import (
	"archive/zip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExecuteHelperReplacesBinaryAndCommitsHealthyTarget(t *testing.T) {
	root := t.TempDir()
	stagedRoot := filepath.Join(root, "data", "updates", "staged", "op-1")
	backupRoot := filepath.Join(root, "data", "updates", "backups")
	writeUpdaterTestFile(t, root, "ice_art.exe", "old binary")
	if _, err := CreateBackup(BackupOptions{InstallRoot: root, BackupRoot: backupRoot, OperationID: "op-old", Mode: BackupModeCompact, Paths: []string{"ice_art.exe"}, CreatedAt: time.Now().Add(-48 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	artifact := prepareHelperArtifact(t, stagedRoot, "v1.2.3", "new binary")
	store, _ := NewStateStore(filepath.Join(root, "data", "updates"))
	state, _ := store.Save(0, OperationState{OperationID: "op-1", Phase: PhaseChecking, CurrentVersion: "v1.2.2", TargetVersion: "v1.2.3"})
	state, _ = store.Save(state.Generation, OperationState{OperationID: "op-1", Phase: PhaseAvailable, CurrentVersion: "v1.2.2", TargetVersion: "v1.2.3"})
	for _, phase := range []OperationPhase{PhaseDownloading, PhaseVerifying, PhaseStaged, PhaseDraining} {
		state, _ = store.Save(state.Generation, OperationState{OperationID: "op-1", Phase: phase, CurrentVersion: "v1.2.2", TargetVersion: "v1.2.3"})
	}
	runtime := &fakeHelperRuntime{}
	job := artifact.apply(HelperJob{OperationID: "op-1", InstallRoot: root, StagedRoot: stagedRoot, CurrentVersion: "v1.2.2", TargetVersion: "v1.2.3", MainPID: 123, ExpiresAt: time.Now().Add(time.Minute), ExecutablePath: "ice_art.exe", StagedExecutablePath: "package/ice_art.exe", BackupRoot: backupRoot, BackupMode: BackupModeCompact, BackupPaths: []string{"ice_art.exe"}, RetainCount: 1})
	if err := ExecuteHelper(context.Background(), HelperExecutionOptions{Job: job, Runtime: runtime, StateStore: store, HealthTimeout: time.Second, TrustedKeys: artifact.keys}); err != nil {
		t.Fatalf("ExecuteHelper() error = %v", err)
	}
	assertUpdaterTestFile(t, root, "ice_art.exe", "new binary")
	if runtime.startCalls != 1 || runtime.healthVersions[0] != "v1.2.3" {
		t.Fatalf("runtime = %#v", runtime)
	}
	finalState, _ := store.Load()
	if finalState.Phase != PhaseSucceeded || finalState.BackupID != "op-1" || finalState.BackupMode != BackupModeCompact || finalState.BackupVersion != "v1.2.2" || finalState.BackupCreatedAt.IsZero() {
		t.Fatalf("final state = %#v, want succeeded backup summary", finalState)
	}
	if _, err := os.Stat(filepath.Join(backupRoot, "op-old")); !os.IsNotExist(err) {
		t.Fatalf("retention did not prune old backup: %v", err)
	}
}

func TestExecuteHelperRestoresBackupWhenTargetHealthFails(t *testing.T) {
	root := t.TempDir()
	stagedRoot := filepath.Join(root, "data", "updates", "staged", "op-2")
	writeUpdaterTestFile(t, root, "ice_art.exe", "old binary")
	artifact := prepareHelperArtifact(t, stagedRoot, "v1.2.3", "bad binary")
	store, _ := NewStateStore(filepath.Join(root, "data", "updates"))
	state, _ := store.Save(0, OperationState{OperationID: "op-2", Phase: PhaseChecking})
	for _, phase := range []OperationPhase{PhaseAvailable, PhaseDownloading, PhaseVerifying, PhaseStaged, PhaseDraining} {
		state, _ = store.Save(state.Generation, OperationState{OperationID: "op-2", Phase: phase})
	}
	runtime := &fakeHelperRuntime{healthErrors: []error{errors.New("target unhealthy"), nil}}
	job := artifact.apply(HelperJob{OperationID: "op-2", InstallRoot: root, StagedRoot: stagedRoot, CurrentVersion: "v1.2.2", TargetVersion: "v1.2.3", MainPID: 123, ExpiresAt: time.Now().Add(time.Minute), ExecutablePath: "ice_art.exe", StagedExecutablePath: "package/ice_art.exe", BackupRoot: filepath.Join(root, "data", "updates", "backups"), BackupMode: BackupModeCompact, BackupPaths: []string{"ice_art.exe"}})
	if err := ExecuteHelper(context.Background(), HelperExecutionOptions{Job: job, Runtime: runtime, StateStore: store, HealthTimeout: time.Second, TrustedKeys: artifact.keys}); err == nil {
		t.Fatal("ExecuteHelper() error = nil, want target health failure")
	}
	assertUpdaterTestFile(t, root, "ice_art.exe", "old binary")
	if runtime.startCalls != 2 || runtime.stopCalls != 1 || len(runtime.healthVersions) != 2 || runtime.healthVersions[1] != "v1.2.2" {
		t.Fatalf("runtime = %#v", runtime)
	}
	finalState, _ := store.Load()
	if finalState.Phase != PhaseRolledBack {
		t.Fatalf("final phase = %q, want rolled_back", finalState.Phase)
	}
}

func TestExecuteHelperRestoresWhenUnhealthyTargetAlreadyExited(t *testing.T) {
	root := t.TempDir()
	stagedRoot := filepath.Join(root, "data", "updates", "staged", "op-exited")
	writeUpdaterTestFile(t, root, "ice_art.exe", "old binary")
	artifact := prepareHelperArtifact(t, stagedRoot, "v1.2.3", "bad binary")
	store, _ := NewStateStore(filepath.Join(root, "data", "updates"))
	state, _ := store.Save(0, OperationState{OperationID: "op-exited", Phase: PhaseChecking})
	for _, phase := range []OperationPhase{PhaseAvailable, PhaseDownloading, PhaseVerifying, PhaseStaged, PhaseDraining} {
		state, _ = store.Save(state.Generation, OperationState{OperationID: "op-exited", Phase: phase})
	}
	runtime := &fakeHelperRuntime{healthErrors: []error{errors.New("target unhealthy"), nil}, stopErr: ErrProcessGone}
	job := artifact.apply(HelperJob{OperationID: "op-exited", InstallRoot: root, StagedRoot: stagedRoot, CurrentVersion: "v1.2.2", TargetVersion: "v1.2.3", MainPID: 123, ExpiresAt: time.Now().Add(time.Minute), ExecutablePath: "ice_art.exe", BackupMode: BackupModeCompact, BackupPaths: []string{"ice_art.exe"}})
	if err := ExecuteHelper(context.Background(), HelperExecutionOptions{Job: job, Runtime: runtime, StateStore: store, HealthTimeout: time.Second, TrustedKeys: artifact.keys}); err == nil {
		t.Fatal("ExecuteHelper() error = nil, want target health failure")
	}
	assertUpdaterTestFile(t, root, "ice_art.exe", "old binary")
	finalState, _ := store.Load()
	if finalState.Phase != PhaseRolledBack {
		t.Fatalf("final phase = %q, want rolled_back", finalState.Phase)
	}
}

func TestExecuteHelperRejectsTamperedArchiveBeforeStoppingMain(t *testing.T) {
	root := t.TempDir()
	stagedRoot := filepath.Join(root, "data", "updates", "staged", "op-tamper")
	writeUpdaterTestFile(t, root, "ice_art.exe", "old binary")
	artifact := prepareHelperArtifact(t, stagedRoot, "v1.2.3", "new binary")
	archivePath := filepath.Join(stagedRoot, "downloads", "ice_art_windows_amd64.zip")
	if err := os.WriteFile(archivePath, []byte("tampered"), 0o644); err != nil {
		t.Fatalf("tamper archive error = %v", err)
	}
	store, _ := NewStateStore(filepath.Join(root, "data", "updates"))
	runtime := &fakeHelperRuntime{}
	job := artifact.apply(HelperJob{OperationID: "op-tamper", InstallRoot: root, StagedRoot: stagedRoot, CurrentVersion: "v1.2.2", TargetVersion: "v1.2.3", MainPID: 123, ExpiresAt: time.Now().Add(time.Minute), ExecutablePath: "ice_art.exe", StagedExecutablePath: "package/ice_art.exe", BackupMode: BackupModeCompact, BackupPaths: []string{"ice_art.exe"}})
	if err := ExecuteHelper(context.Background(), HelperExecutionOptions{Job: job, Runtime: runtime, StateStore: store, TrustedKeys: artifact.keys}); err == nil {
		t.Fatal("ExecuteHelper() error = nil, want tampering rejection")
	}
	if runtime.startCalls != 0 {
		t.Fatalf("startCalls = %d, want 0", runtime.startCalls)
	}
	assertUpdaterTestFile(t, root, "ice_art.exe", "old binary")
}

func TestExecuteHelperRollsBackWhenAtomicReplaceFails(t *testing.T) {
	root := t.TempDir()
	stagedRoot := filepath.Join(root, "data", "updates", "staged", "op-replace")
	writeUpdaterTestFile(t, root, "ice_art.exe", "old binary")
	artifact := prepareHelperArtifact(t, stagedRoot, "v1.2.3", "new binary")
	store, _ := NewStateStore(filepath.Join(root, "data", "updates"))
	state, _ := store.Save(0, OperationState{OperationID: "op-replace", Phase: PhaseChecking})
	for _, phase := range []OperationPhase{PhaseAvailable, PhaseDownloading, PhaseVerifying, PhaseStaged, PhaseDraining} {
		state, _ = store.Save(state.Generation, OperationState{OperationID: "op-replace", Phase: phase})
	}
	runtime := &fakeHelperRuntime{}
	job := artifact.apply(HelperJob{OperationID: "op-replace", InstallRoot: root, StagedRoot: stagedRoot, CurrentVersion: "v1.2.2", TargetVersion: "v1.2.3", MainPID: 123, ExpiresAt: time.Now().Add(time.Minute), ExecutablePath: "ice_art.exe", StagedExecutablePath: "package/ice_art.exe", BackupMode: BackupModeCompact, BackupPaths: []string{"ice_art.exe"}})
	err := ExecuteHelper(context.Background(), HelperExecutionOptions{Job: job, Runtime: runtime, StateStore: store, TrustedKeys: artifact.keys, ReplaceExecutable: func(_, _ string) error { return errors.New("replace failed") }})
	if err == nil {
		t.Fatal("ExecuteHelper() error = nil, want replace failure")
	}
	assertUpdaterTestFile(t, root, "ice_art.exe", "old binary")
	finalState, _ := store.Load()
	if finalState.Phase != PhaseRolledBack {
		t.Fatalf("final phase = %q, want rolled_back", finalState.Phase)
	}
}

func TestExecuteHelperIgnoresJobSelectedFileAndUsesVerifiedBinary(t *testing.T) {
	root := t.TempDir()
	stagedRoot := filepath.Join(root, "data", "updates", "staged", "op-derived")
	writeUpdaterTestFile(t, root, "ice_art.exe", "old binary")
	artifact := prepareHelperArtifact(t, stagedRoot, "v1.2.3", "new binary")
	store, _ := NewStateStore(filepath.Join(root, "data", "updates"))
	state, _ := store.Save(0, OperationState{OperationID: "op-derived", Phase: PhaseChecking})
	for _, phase := range []OperationPhase{PhaseAvailable, PhaseDownloading, PhaseVerifying, PhaseStaged, PhaseDraining} {
		state, _ = store.Save(state.Generation, OperationState{OperationID: "op-derived", Phase: phase})
	}
	job := artifact.apply(HelperJob{OperationID: "op-derived", InstallRoot: root, StagedRoot: stagedRoot, CurrentVersion: "v1.2.2", TargetVersion: "v1.2.3", MainPID: 123, ExpiresAt: time.Now().Add(time.Minute), ExecutablePath: "ice_art.exe", StagedExecutablePath: "package/build-info.json", StagedExecutableSHA: "attacker-controlled", BackupMode: BackupModeCompact, BackupPaths: []string{"ice_art.exe"}})
	if err := ExecuteHelper(context.Background(), HelperExecutionOptions{Job: job, Runtime: &fakeHelperRuntime{}, StateStore: store, TrustedKeys: artifact.keys}); err != nil {
		t.Fatalf("ExecuteHelper() error = %v", err)
	}
	assertUpdaterTestFile(t, root, "ice_art.exe", "new binary")
}

func TestExecuteHelperRollsBackWhenStateWriteFailsAfterReplace(t *testing.T) {
	root := t.TempDir()
	stagedRoot := filepath.Join(root, "data", "updates", "staged", "op-state")
	writeUpdaterTestFile(t, root, "ice_art.exe", "old binary")
	artifact := prepareHelperArtifact(t, stagedRoot, "v1.2.3", "new binary")
	store, _ := NewStateStore(filepath.Join(root, "data", "updates"))
	state, _ := store.Save(0, OperationState{OperationID: "op-state", Phase: PhaseChecking})
	for _, phase := range []OperationPhase{PhaseAvailable, PhaseDownloading, PhaseVerifying, PhaseStaged, PhaseDraining} {
		state, _ = store.Save(state.Generation, OperationState{OperationID: "op-state", Phase: phase})
	}
	failed := false
	store.writeState = func(path string, value any, mode os.FileMode) error {
		if next, ok := value.(OperationState); ok && next.Phase == PhaseRestarting && !failed {
			failed = true
			return errors.New("state disk failure")
		}
		return writeJSONAtomic(path, value, mode)
	}
	job := artifact.apply(HelperJob{OperationID: "op-state", InstallRoot: root, StagedRoot: stagedRoot, CurrentVersion: "v1.2.2", TargetVersion: "v1.2.3", MainPID: 123, ExpiresAt: time.Now().Add(time.Minute), ExecutablePath: "ice_art.exe", BackupMode: BackupModeCompact, BackupPaths: []string{"ice_art.exe"}})
	if err := ExecuteHelper(context.Background(), HelperExecutionOptions{Job: job, Runtime: &fakeHelperRuntime{}, StateStore: store, TrustedKeys: artifact.keys}); err == nil {
		t.Fatal("ExecuteHelper() error = nil, want state write failure")
	}
	assertUpdaterTestFile(t, root, "ice_art.exe", "old binary")
	finalState, _ := store.Load()
	if finalState.Phase != PhaseRolledBack {
		t.Fatalf("final phase = %q, want rolled_back", finalState.Phase)
	}
}

func TestExecuteHelperPerformsManualRollbackWithSafetySnapshot(t *testing.T) {
	root := t.TempDir()
	backupRoot := filepath.Join(root, "data", "updates", "backups")
	writeUpdaterTestFile(t, root, "ice_art.exe", "old binary")
	if _, err := CreateBackup(BackupOptions{InstallRoot: root, BackupRoot: backupRoot, OperationID: "op-old", Mode: BackupModeCompact, Paths: []string{"ice_art.exe"}}); err != nil {
		t.Fatal(err)
	}
	writeUpdaterTestFile(t, root, "ice_art.exe", "new binary")
	store, _ := NewStateStore(filepath.Join(root, "data", "updates"))
	state, _ := store.Save(0, OperationState{OperationID: "op-old", Phase: PhaseChecking})
	for _, phase := range []OperationPhase{PhaseAvailable, PhaseDownloading, PhaseVerifying, PhaseStaged, PhaseDraining, PhaseBackingUp, PhaseReplacing, PhaseRestarting, PhaseHealthCheck, PhaseSucceeded} {
		state, _ = store.Save(state.Generation, OperationState{OperationID: "op-old", Phase: phase, CurrentVersion: "v1.2.2", TargetVersion: "v1.2.3", BackupID: "op-old"})
	}
	state, _ = store.Save(state.Generation, OperationState{OperationID: "op-manual", Phase: PhaseRollingBack, CurrentVersion: "v1.2.3", TargetVersion: "v1.2.2", BackupID: "op-old"})
	stagedRoot := filepath.Join(root, "data", "updates", "staged", "op-manual")
	if err := os.MkdirAll(stagedRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	runtime := &fakeHelperRuntime{}
	job := HelperJob{OperationID: "op-manual", InstallRoot: root, StagedRoot: stagedRoot, CurrentVersion: "v1.2.3", TargetVersion: "v1.2.2", MainPID: 123, ExpiresAt: time.Now().Add(time.Minute), ExecutablePath: "ice_art.exe", BackupMode: BackupModeCompact, BackupPaths: []string{"ice_art.exe"}, ManualRollback: true, RestoreBackupID: "op-old", HealthToken: "health-token"}
	if err := ExecuteHelper(context.Background(), HelperExecutionOptions{Job: job, Runtime: runtime, StateStore: store, HealthTimeout: time.Second}); err != nil {
		t.Fatalf("ExecuteHelper(manual rollback) error = %v", err)
	}
	assertUpdaterTestFile(t, root, "ice_art.exe", "old binary")
	finalState, _ := store.Load()
	if finalState.Phase != PhaseRolledBack || runtime.healthVersions[0] != "v1.2.2" {
		t.Fatalf("state=%#v runtime=%#v", finalState, runtime)
	}
}

type fakeHelperRuntime struct {
	startCalls     int
	stopCalls      int
	healthErrors   []error
	healthVersions []string
	stopErr        error
}

type helperArtifact struct {
	keys       map[string]ed25519.PublicKey
	archiveSHA string
	binarySHA  string
}

func (a helperArtifact) apply(job HelperJob) HelperJob {
	if job.HealthToken == "" {
		job.HealthToken = "test-health-token"
	}
	job.ArchivePath = "downloads/ice_art_windows_amd64.zip"
	job.ChecksumsPath = "downloads/SHA256SUMS"
	job.SignaturePath = "downloads/SHA256SUMS.sig"
	job.ArchiveAssetName = "ice_art_windows_amd64.zip"
	job.ArchiveSHA256 = a.archiveSHA
	job.StagedExecutableSHA = a.binarySHA
	return job
}

func prepareHelperArtifact(t *testing.T, stagedRoot, version, binary string) helperArtifact {
	t.Helper()
	downloads := filepath.Join(stagedRoot, "downloads")
	if err := os.MkdirAll(downloads, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	archivePath := filepath.Join(downloads, "ice_art_windows_amd64.zip")
	file, _ := os.Create(archivePath)
	writer := zip.NewWriter(file)
	entry, _ := writer.Create("package/ice_art.exe")
	_, _ = entry.Write([]byte(binary))
	buildInfo, _ := writer.Create("package/build-info.json")
	_, _ = buildInfo.Write([]byte(`{"version":"` + version + `","commit":"abc","distribution":"release","goos":"windows","goarch":"amd64"}`))
	managedManifest, _ := writer.Create("package/managed-manifest.json")
	_, _ = managedManifest.Write([]byte(`{"version":"` + version + `","files":{}}`))
	_ = writer.Close()
	_ = file.Close()
	archiveData, _ := os.ReadFile(archivePath)
	archiveDigest := sha256.Sum256(archiveData)
	archiveSHA := hex.EncodeToString(archiveDigest[:])
	checksums := []byte(archiveSHA + "  ice_art_windows_amd64.zip\n")
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	envelope, _ := json.Marshal(SignatureEnvelope{KeyID: "release-2026", Algorithm: "ed25519", Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, checksums))})
	_ = os.WriteFile(filepath.Join(downloads, "SHA256SUMS"), checksums, 0o644)
	_ = os.WriteFile(filepath.Join(downloads, "SHA256SUMS.sig"), envelope, 0o644)
	binaryDigest := sha256.Sum256([]byte(binary))
	return helperArtifact{keys: map[string]ed25519.PublicKey{"release-2026": publicKey}, archiveSHA: archiveSHA, binarySHA: hex.EncodeToString(binaryDigest[:])}
}

func (f *fakeHelperRuntime) WaitForExit(context.Context, int, string) error { return nil }

func (f *fakeHelperRuntime) Start(_ context.Context, _ string, _ []string, _ string, _ []string) (int, error) {
	f.startCalls++
	return 1000 + f.startCalls, nil
}

func (f *fakeHelperRuntime) Stop(context.Context, int, string) error {
	f.stopCalls++
	return f.stopErr
}

func (f *fakeHelperRuntime) Health(_ context.Context, _ int, version, _ string) error {
	f.healthVersions = append(f.healthVersions, version)
	if len(f.healthErrors) == 0 {
		return nil
	}
	err := f.healthErrors[0]
	f.healthErrors = f.healthErrors[1:]
	return err
}
