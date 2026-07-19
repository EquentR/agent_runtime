package updater

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type HelperRuntime interface {
	WaitForExit(ctx context.Context, pid int, expectedExecutable string) error
	Start(ctx context.Context, executable string, args []string, workingDirectory string, environment []string) (int, error)
	Stop(ctx context.Context, pid int, expectedExecutable string) error
	Health(ctx context.Context, pid int, version, token string) error
}

type HelperExecutionOptions struct {
	Job               HelperJob
	Runtime           HelperRuntime
	StateStore        *StateStore
	HealthTimeout     time.Duration
	TrustedKeys       map[string]ed25519.PublicKey
	StartEnvironment  []string
	ReplaceExecutable func(source, destination string) error
	FileOwner         *FileOwner
}

func ExecuteHelper(ctx context.Context, options HelperExecutionOptions) error {
	if options.Runtime == nil || options.StateStore == nil {
		return fmt.Errorf("helper runtime and state store are required")
	}
	job := options.Job
	if strings.TrimSpace(job.OperationID) == "" || job.MainPID <= 0 || strings.TrimSpace(job.ExecutablePath) == "" {
		return fmt.Errorf("helper job is missing required fields")
	}
	installRoot, err := canonicalRoot(job.InstallRoot)
	if err != nil {
		return err
	}
	stagedRoot, err := canonicalRoot(job.StagedRoot)
	if err != nil {
		return err
	}
	if !pathWithin(installRoot, stagedRoot) {
		return fmt.Errorf("staged root escapes install root")
	}
	updatesRoot := filepath.Join(installRoot, "data", "updates")
	if !samePath(filepath.Dir(options.StateStore.StatePath()), updatesRoot) {
		return fmt.Errorf("state store is outside the fixed updater root")
	}
	lock, err := AcquireProcessLock(filepath.Join(updatesRoot, "operation.lock"))
	if err != nil {
		return err
	}
	defer lock.Close()
	_, currentPath, err := safeRelativePath(installRoot, job.ExecutablePath)
	if err != nil {
		return err
	}
	verifiedExecutable := ""
	var previousManifest, nextManifest ManagedManifest
	managedManifestPath := filepath.Join(installRoot, "data", "updates", "managed-manifest.json")
	cleanupVerified := func() {}
	if !job.ManualRollback {
		verifiedExecutable, cleanupVerified, err = prepareVerifiedExecutable(job, options.TrustedKeys)
		if err != nil {
			return err
		}
		nextManifest, err = loadManagedManifest(filepath.Join(filepath.Dir(verifiedExecutable), "managed-manifest.json"), true)
		if err != nil {
			return err
		}
		previousManifest, err = loadManagedManifest(managedManifestPath, false)
		if err != nil {
			return err
		}
	}
	defer cleanupVerified()
	if options.HealthTimeout <= 0 {
		options.HealthTimeout = 90 * time.Second
	}
	replaceExecutable := options.ReplaceExecutable
	if replaceExecutable == nil {
		replaceExecutable = copyRegularFileAtomic
	}
	if err := options.Runtime.WaitForExit(ctx, job.MainPID, currentPath); err != nil {
		return failHelperState(options.StateStore, lock, job, fmt.Errorf("wait for main process: %w", err))
	}
	if job.ManualRollback {
		return executeManualRollback(ctx, options, lock, job, currentPath)
	}
	if err := advanceHelperState(options.StateStore, lock, job, PhaseBackingUp); err != nil {
		return err
	}
	backupRoot, err := helperBackupRoot(job, installRoot)
	if err != nil {
		return failHelperState(options.StateStore, lock, job, err)
	}
	manifest, err := CreateBackup(BackupOptions{InstallRoot: installRoot, BackupRoot: backupRoot, OperationID: job.OperationID, Mode: job.BackupMode, Paths: job.BackupPaths, CreatedAt: time.Now().UTC()})
	if err != nil {
		_ = restartOldProcess(ctx, options.Runtime, currentPath, job, options.StartEnvironment)
		return failHelperState(options.StateStore, lock, job, fmt.Errorf("create upgrade backup: %w", err))
	}
	if err := advanceHelperState(options.StateStore, lock, job, PhaseReplacing); err != nil {
		return err
	}
	if _, err := ApplyManagedFiles(installRoot, filepath.Dir(verifiedExecutable), previousManifest, nextManifest); err != nil {
		return rollbackHelper(ctx, options, lock, job, manifest, currentPath, 0, fmt.Errorf("apply managed release files: %w", err))
	}
	if err := replaceExecutable(verifiedExecutable, currentPath); err != nil {
		return rollbackHelper(ctx, options, lock, job, manifest, currentPath, 0, fmt.Errorf("replace executable: %w", err))
	}
	if err := advanceHelperState(options.StateStore, lock, job, PhaseRestarting); err != nil {
		return rollbackHelper(ctx, options, lock, job, manifest, currentPath, 0, fmt.Errorf("advance restarting state: %w", err))
	}
	targetPID, err := options.Runtime.Start(ctx, currentPath, job.Arguments, job.WorkingDirectory, options.StartEnvironment)
	if err != nil {
		return rollbackHelper(ctx, options, lock, job, manifest, currentPath, 0, fmt.Errorf("start target process: %w", err))
	}
	if err := advanceHelperState(options.StateStore, lock, job, PhaseHealthCheck); err != nil {
		return rollbackHelper(ctx, options, lock, job, manifest, currentPath, targetPID, fmt.Errorf("advance health check state: %w", err))
	}
	healthCtx, cancel := context.WithTimeout(ctx, options.HealthTimeout)
	healthErr := options.Runtime.Health(healthCtx, targetPID, job.TargetVersion, job.HealthToken)
	cancel()
	if healthErr != nil {
		return rollbackHelper(ctx, options, lock, job, manifest, currentPath, targetPID, fmt.Errorf("target health check: %w", healthErr))
	}
	if err := writeJSONAtomic(managedManifestPath, nextManifest, 0o600); err != nil {
		return rollbackHelper(ctx, options, lock, job, manifest, currentPath, targetPID, fmt.Errorf("commit managed release manifest: %w", err))
	}
	if err := advanceHelperState(options.StateStore, lock, job, PhaseSucceeded); err != nil {
		return rollbackHelper(ctx, options, lock, job, manifest, currentPath, targetPID, fmt.Errorf("advance succeeded state: %w", err))
	}
	pruneHelperBackups(options.StateStore, job, backupRoot, PhaseSucceeded)
	return nil
}

func loadManagedManifest(path string, required bool) (ManagedManifest, error) {
	raw, err := readLimitedFile(path, 4<<20)
	if os.IsNotExist(err) && !required {
		return ManagedManifest{Files: map[string]string{}}, nil
	}
	if err != nil {
		return ManagedManifest{}, err
	}
	var manifest ManagedManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return ManagedManifest{}, fmt.Errorf("decode managed release manifest: %w", err)
	}
	if manifest.Files == nil {
		manifest.Files = map[string]string{}
	}
	return manifest, nil
}

func executeManualRollback(ctx context.Context, options HelperExecutionOptions, lock *ProcessLock, job HelperJob, currentPath string) error {
	backupRoot, err := helperBackupRoot(job, job.InstallRoot)
	if err != nil {
		return failHelperState(options.StateStore, lock, job, err)
	}
	if _, err := CreateBackup(BackupOptions{InstallRoot: job.InstallRoot, BackupRoot: backupRoot, OperationID: job.OperationID, Mode: job.BackupMode, Paths: job.BackupPaths, CreatedAt: time.Now().UTC()}); err != nil {
		return failHelperState(options.StateStore, lock, job, fmt.Errorf("create rollback safety backup: %w", err))
	}
	if !operationIDPattern.MatchString(job.RestoreBackupID) {
		return failHelperState(options.StateStore, lock, job, fmt.Errorf("rollback backup ID is invalid"))
	}
	if err := RestoreBackup(job.InstallRoot, filepath.Join(backupRoot, job.RestoreBackupID)); err != nil {
		_ = advanceHelperState(options.StateStore, lock, job, PhaseRecoveryRequired)
		return fmt.Errorf("restore requested backup: %w", err)
	}
	handshake, err := NewHealthHandshakeStore(filepath.Join(job.InstallRoot, "data", "updates"))
	if err != nil {
		_ = advanceHelperState(options.StateStore, lock, job, PhaseRecoveryRequired)
		return fmt.Errorf("create restored health handshake: %w", err)
	}
	handshake.SetOwner(options.FileOwner)
	if err := handshake.Issue(job.HealthToken, job.TargetVersion, time.Now().UTC().Add(options.HealthTimeout+time.Minute)); err != nil {
		_ = advanceHelperState(options.StateStore, lock, job, PhaseRecoveryRequired)
		return fmt.Errorf("issue restored health handshake: %w", err)
	}
	targetPID, err := options.Runtime.Start(ctx, currentPath, job.Arguments, job.WorkingDirectory, options.StartEnvironment)
	if err != nil {
		_ = advanceHelperState(options.StateStore, lock, job, PhaseRecoveryRequired)
		return fmt.Errorf("start restored process: %w", err)
	}
	healthCtx, cancel := context.WithTimeout(ctx, options.HealthTimeout)
	healthErr := options.Runtime.Health(healthCtx, targetPID, job.TargetVersion, job.HealthToken)
	cancel()
	if healthErr != nil {
		_ = advanceHelperState(options.StateStore, lock, job, PhaseRecoveryRequired)
		return fmt.Errorf("restored process health check: %w", healthErr)
	}
	if err := advanceHelperState(options.StateStore, lock, job, PhaseRolledBack); err != nil {
		return err
	}
	pruneHelperBackups(options.StateStore, job, backupRoot, PhaseRolledBack)
	return nil
}

func pruneHelperBackups(store *StateStore, job HelperJob, backupRoot string, phase OperationPhase) {
	if job.RetainCount <= 0 && job.RetainDays <= 0 {
		return
	}
	if _, err := PruneBackups(backupRoot, job.RetainCount, job.RetainDays, time.Now().UTC()); err != nil {
		_ = store.appendEventUnlocked(OperationEvent{OperationID: job.OperationID, Phase: phase, At: time.Now().UTC(), Message: "backup retention cleanup failed"})
	}
}

func helperBackupRoot(job HelperJob, installRoot string) (string, error) {
	root := strings.TrimSpace(job.BackupRoot)
	if root == "" {
		root = filepath.Join(installRoot, "data", "updates", "backups")
	}
	return canonicalRoot(root)
}

func rollbackHelper(ctx context.Context, options HelperExecutionOptions, lock *ProcessLock, job HelperJob, _ BackupManifest, currentPath string, targetPID int, cause error) error {
	var rollbackErr error
	if err := advanceHelperState(options.StateStore, lock, job, PhaseRollingBack); err != nil {
		rollbackErr = err
	}
	if targetPID > 0 {
		if err := options.Runtime.Stop(ctx, targetPID, currentPath); err != nil {
			if !isProcessGoneError(err) {
				return errors.Join(cause, rollbackErr, fmt.Errorf("stop unhealthy target: %w", err))
			}
		}
	}
	backupRoot, backupRootErr := helperBackupRoot(job, job.InstallRoot)
	if backupRootErr != nil {
		return errors.Join(cause, rollbackErr, backupRootErr)
	}
	backupDir := filepath.Join(backupRoot, job.OperationID)
	if err := RestoreBackup(job.InstallRoot, backupDir); err != nil {
		_ = advanceHelperState(options.StateStore, lock, job, PhaseRecoveryRequired)
		return errors.Join(cause, rollbackErr, fmt.Errorf("restore upgrade backup: %w", err))
	}
	handshake, err := NewHealthHandshakeStore(filepath.Join(job.InstallRoot, "data", "updates"))
	if err != nil {
		_ = advanceHelperState(options.StateStore, lock, job, PhaseRecoveryRequired)
		return errors.Join(cause, rollbackErr, fmt.Errorf("create rollback health handshake: %w", err))
	}
	handshake.SetOwner(options.FileOwner)
	if err := handshake.Issue(job.HealthToken, job.CurrentVersion, time.Now().UTC().Add(options.HealthTimeout+time.Minute)); err != nil {
		_ = advanceHelperState(options.StateStore, lock, job, PhaseRecoveryRequired)
		return errors.Join(cause, rollbackErr, fmt.Errorf("issue rollback health handshake: %w", err))
	}
	oldPID, err := options.Runtime.Start(ctx, currentPath, job.Arguments, job.WorkingDirectory, options.StartEnvironment)
	if err != nil {
		_ = advanceHelperState(options.StateStore, lock, job, PhaseRecoveryRequired)
		return errors.Join(cause, rollbackErr, fmt.Errorf("restart old process: %w", err))
	}
	healthCtx, cancel := context.WithTimeout(ctx, options.HealthTimeout)
	oldHealthErr := options.Runtime.Health(healthCtx, oldPID, job.CurrentVersion, job.HealthToken)
	cancel()
	if oldHealthErr != nil {
		_ = advanceHelperState(options.StateStore, lock, job, PhaseRecoveryRequired)
		return errors.Join(cause, rollbackErr, fmt.Errorf("old process health check: %w", oldHealthErr))
	}
	if err := advanceHelperState(options.StateStore, lock, job, PhaseRolledBack); err != nil {
		return errors.Join(cause, rollbackErr, err)
	}
	return errors.Join(cause, rollbackErr)
}

func restartOldProcess(ctx context.Context, runtime HelperRuntime, currentPath string, job HelperJob, environment []string) error {
	_, err := runtime.Start(ctx, currentPath, job.Arguments, job.WorkingDirectory, environment)
	return err
}

func advanceHelperState(store *StateStore, lock *ProcessLock, job HelperJob, phase OperationPhase) error {
	state, err := store.loadLocked(lock)
	if err != nil {
		return err
	}
	state.OperationID = job.OperationID
	state.CurrentVersion = job.CurrentVersion
	state.TargetVersion = job.TargetVersion
	state.BackupID = job.OperationID
	state.BackupMode = job.BackupMode
	if state.BackupCreatedAt.IsZero() {
		state.BackupCreatedAt = time.Now().UTC()
	}
	state.BackupVersion = job.CurrentVersion
	state.Phase = phase
	_, err = store.saveLockedWithLock(lock, state.Generation, state)
	return err
}

func failHelperState(store *StateStore, lock *ProcessLock, job HelperJob, cause error) error {
	state, err := store.loadLocked(lock)
	if err == nil {
		state.OperationID = job.OperationID
		state.CurrentVersion = job.CurrentVersion
		state.TargetVersion = job.TargetVersion
		state.Phase = PhaseFailed
		state.Error = "update helper failed"
		_, _ = store.saveLockedWithLock(lock, state.Generation, state)
	}
	return cause
}

func prepareVerifiedExecutable(job HelperJob, trustedKeys map[string]ed25519.PublicKey) (string, func(), error) {
	noop := func() {}
	if len(trustedKeys) == 0 {
		return "", noop, fmt.Errorf("trusted release keys are required")
	}
	_, archivePath, err := safeRelativePath(job.StagedRoot, job.ArchivePath)
	if err != nil {
		return "", noop, err
	}
	_, checksumsPath, err := safeRelativePath(job.StagedRoot, job.ChecksumsPath)
	if err != nil {
		return "", noop, err
	}
	_, signaturePath, err := safeRelativePath(job.StagedRoot, job.SignaturePath)
	if err != nil {
		return "", noop, err
	}
	checksums, err := readLimitedFile(checksumsPath, 2<<20)
	if err != nil {
		return "", noop, err
	}
	signature, err := readLimitedFile(signaturePath, 64<<10)
	if err != nil {
		return "", noop, err
	}
	if err := VerifySignedChecksums(checksums, signature, trustedKeys); err != nil {
		return "", noop, err
	}
	parsed, err := ParseChecksums(checksums)
	if err != nil {
		return "", noop, err
	}
	expectedArchiveHash, ok := parsed[job.ArchiveAssetName]
	if !ok || !strings.EqualFold(expectedArchiveHash, job.ArchiveSHA256) {
		return "", noop, fmt.Errorf("helper archive is not bound to the signed checksum")
	}
	if err := VerifyFileChecksum(archivePath, expectedArchiveHash); err != nil {
		return "", noop, err
	}
	verifyBase := filepath.Join(job.InstallRoot, "data", "updates", "verify")
	if err := os.MkdirAll(verifyBase, 0o700); err != nil {
		return "", noop, err
	}
	verifyRoot, err := os.MkdirTemp(verifyBase, ".verified-*")
	if err != nil {
		return "", noop, err
	}
	cleanup := func() { _ = os.RemoveAll(verifyRoot) }
	if err := ExtractArchive(archivePath, verifyRoot, DefaultExtractLimits()); err != nil {
		cleanup()
		return "", noop, err
	}
	buildInfoPath, err := findUniqueBuildInfo(verifyRoot)
	if err != nil {
		cleanup()
		return "", noop, err
	}
	rawBuildInfo, err := readLimitedFile(buildInfoPath, 64<<10)
	if err != nil {
		cleanup()
		return "", noop, err
	}
	var buildInfo BuildInfo
	if err := json.Unmarshal(rawBuildInfo, &buildInfo); err != nil {
		cleanup()
		return "", noop, err
	}
	buildInfo = buildInfo.Normalized()
	if buildInfo.Version != job.TargetVersion || buildInfo.Distribution != DistributionRelease {
		cleanup()
		return "", noop, fmt.Errorf("verified build info does not match helper target")
	}
	wantAsset, err := officialAssetName(buildInfo.GOOS, buildInfo.GOARCH)
	if err != nil || wantAsset != job.ArchiveAssetName {
		cleanup()
		return "", noop, fmt.Errorf("verified archive name does not match build platform")
	}
	binaryName := "ice_art"
	if buildInfo.GOOS == "windows" {
		binaryName = "ice_art.exe"
	}
	executablePath := filepath.Join(filepath.Dir(buildInfoPath), binaryName)
	info, err := os.Lstat(executablePath)
	if err != nil {
		cleanup()
		return "", noop, fmt.Errorf("locate verified executable: %w", err)
	}
	if !info.Mode().IsRegular() {
		cleanup()
		return "", noop, fmt.Errorf("verified executable is not a regular file")
	}
	return executablePath, cleanup, nil
}

func readLimitedFile(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("file exceeds read limit: %s", path)
	}
	return data, nil
}
