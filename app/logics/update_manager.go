package logics

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/EquentR/agent_runtime/app/models"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	coreupdater "github.com/EquentR/agent_runtime/core/updater"
	"github.com/google/uuid"
)

type UpdateManagerOptions struct {
	Current             coreupdater.BuildInfo
	Checker             *coreupdater.ReleaseChecker
	StateStore          *coreupdater.StateStore
	HealthStore         *coreupdater.HealthHandshakeStore
	Maintenance         *coreupdater.MaintenanceGate
	TaskManager         *coretasks.Manager
	TaskDrainer         updateTaskDrainer
	AuthLogic           *AuthLogic
	AuditLogic          *AdminAuditLogic
	TrustedKeys         map[string]ed25519.PublicKey
	HTTPClient          *http.Client
	DownloadURLTemplate string
	RuntimeMode         string
	ServiceName         string
	InstallRoot         string
	ConfigPath          string
	HealthURL           string
	Arguments           []string
	WorkingDir          string
	JobSecret           []byte
	HealthTimeout       time.Duration
	DrainTimeout        time.Duration
	BackupMode          coreupdater.BackupMode
	BackupPaths         []string
	LaunchHelper        func(context.Context, string) error
	RequestShutdown     func()
	Now                 func() time.Time
	StageRelease        func(context.Context, coreupdater.StageOptions) (coreupdater.StagedRelease, error)
	RetainCount         int
	RetainDays          int
	CapabilityError     string
}

type updateTaskDrainer interface {
	PauseClaims()
	ResumeClaims()
	WaitForIdle(context.Context) error
	CancelActiveTasks()
	ActiveTaskCount() int
}

type UpdateStatus struct {
	Current             coreupdater.BuildInfo         `json:"current"`
	Latest              *coreupdater.Release          `json:"latest,omitempty"`
	UpdateAvailable     bool                          `json:"update_available"`
	CheckedAt           time.Time                     `json:"checked_at,omitempty"`
	CacheStale          bool                          `json:"cache_stale"`
	Capability          string                        `json:"capability"`
	Capable             bool                          `json:"capable"`
	CapabilityReason    string                        `json:"capability_reason,omitempty"`
	RuntimeMode         string                        `json:"runtime_mode"`
	SignatureStatus     string                        `json:"signature_status"`
	CacheUpdatedAt      time.Time                     `json:"cache_updated_at,omitempty"`
	Backup              *UpdateBackupSummary          `json:"backup,omitempty"`
	State               coreupdater.OperationState    `json:"state"`
	Maintenance         coreupdater.MaintenanceStatus `json:"maintenance"`
	LastError           string                        `json:"last_error,omitempty"`
	ForceInstallAllowed bool                          `json:"force_install_allowed"`
	Preflight           *UpdatePreflight              `json:"preflight,omitempty"`
}

type UpdatePreflight struct {
	EstimatedDownloadBytes      int64  `json:"estimated_download_bytes"`
	EstimatedCompactBackupBytes int64  `json:"estimated_compact_backup_bytes"`
	EstimatedFullBackupBytes    int64  `json:"estimated_full_backup_bytes"`
	AvailableBytes              uint64 `json:"available_bytes,omitempty"`
	ActiveTaskCount             int    `json:"active_task_count"`
	MaintenanceImpact           string `json:"maintenance_impact"`
	Error                       string `json:"error,omitempty"`
}

type UpdateBackupSummary struct {
	ID        string                 `json:"id"`
	Mode      coreupdater.BackupMode `json:"mode,omitempty"`
	CreatedAt time.Time              `json:"created_at,omitempty"`
	Version   string                 `json:"version,omitempty"`
}

var (
	ErrUpdateOperationConflict = errors.New("another update operation is active")
	ErrInvalidUpdateRequest    = errors.New("invalid update request")
	ErrUpdateUnavailable       = errors.New("self-update installation is unavailable")
	ErrUpdateDrainTimeout      = errors.New("update task drain timed out")
)

const drainFailureMessage = "task drain timed out; force install authorization required"

type UpdateManager struct {
	current             coreupdater.BuildInfo
	checker             *coreupdater.ReleaseChecker
	state               *coreupdater.StateStore
	healthStore         *coreupdater.HealthHandshakeStore
	maintenance         *coreupdater.MaintenanceGate
	tasks               updateTaskDrainer
	auth                *AuthLogic
	audit               *AdminAuditLogic
	trustedKeys         map[string]ed25519.PublicKey
	httpClient          *http.Client
	downloadURLTemplate string
	runtimeMode         string
	serviceName         string
	installRoot         string
	configPath          string
	healthURL           string
	arguments           []string
	workingDir          string
	jobSecret           []byte
	healthTimeout       time.Duration
	drainTimeout        time.Duration
	backupMode          coreupdater.BackupMode
	backupPaths         []string
	launchHelper        func(context.Context, string) error
	requestShutdown     func()
	authorizer          *coreupdater.OperationAuthorizer
	now                 func() time.Time
	stageRelease        func(context.Context, coreupdater.StageOptions) (coreupdater.StagedRelease, error)
	retainCount         int
	retainDays          int
	capabilityError     string
	mu                  sync.Mutex
	operationMu         sync.Mutex
	lastCheck           *coreupdater.CheckResult
}

func NewUpdateManager(options UpdateManagerOptions) (*UpdateManager, error) {
	if options.StateStore == nil {
		return nil, fmt.Errorf("update state store is required")
	}
	root := strings.TrimSpace(options.InstallRoot)
	if root == "" {
		root, _ = os.Getwd()
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if options.Maintenance == nil {
		options.Maintenance = coreupdater.NewMaintenanceGate()
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.HealthTimeout <= 0 {
		options.HealthTimeout = 90 * time.Second
	}
	if options.DrainTimeout <= 0 {
		options.DrainTimeout = 5 * time.Minute
	}
	if options.BackupMode == "" {
		options.BackupMode = coreupdater.BackupModeCompact
	}
	if len(options.JobSecret) < 32 {
		digest := sha256.Sum256(options.JobSecret)
		options.JobSecret = digest[:]
	}
	manager := &UpdateManager{
		current: options.Current.Normalized(), checker: options.Checker, state: options.StateStore, healthStore: options.HealthStore,
		maintenance: options.Maintenance, tasks: options.TaskManager, auth: options.AuthLogic, audit: options.AuditLogic,
		trustedKeys: options.TrustedKeys, installRoot: root, configPath: options.ConfigPath, healthURL: options.HealthURL,
		httpClient: options.HTTPClient, downloadURLTemplate: options.DownloadURLTemplate,
		runtimeMode: strings.TrimSpace(options.RuntimeMode), serviceName: strings.TrimSpace(options.ServiceName),
		arguments: append([]string(nil), options.Arguments...), workingDir: options.WorkingDir, jobSecret: options.JobSecret,
		healthTimeout: options.HealthTimeout, drainTimeout: options.DrainTimeout, backupMode: options.BackupMode,
		backupPaths: append([]string(nil), options.BackupPaths...), launchHelper: options.LaunchHelper,
		requestShutdown: options.RequestShutdown, authorizer: coreupdater.NewOperationAuthorizer(options.Now), now: options.Now,
		stageRelease: options.StageRelease, retainCount: options.RetainCount, retainDays: options.RetainDays,
		capabilityError: strings.TrimSpace(options.CapabilityError),
	}
	if options.TaskDrainer != nil {
		manager.tasks = options.TaskDrainer
	}
	if manager.stageRelease == nil {
		manager.stageRelease = coreupdater.StageRelease
	}
	if manager.workingDir == "" {
		manager.workingDir = root
	}
	if manager.launchHelper == nil {
		manager.launchHelper = func(ctx context.Context, jobPath string) error {
			return launchUpdateHelper(ctx, jobPath, manager.configPath, manager.runtimeMode, manager.serviceName)
		}
	}
	return manager, nil
}

func (m *UpdateManager) Status(ctx context.Context) (UpdateStatus, error) {
	if m == nil {
		return UpdateStatus{}, fmt.Errorf("update manager is not configured")
	}
	state, err := m.state.Load()
	if err != nil {
		return UpdateStatus{}, err
	}
	status := UpdateStatus{Current: m.current, State: state, Maintenance: m.maintenance.Status(), RuntimeMode: m.runtimeMode, SignatureStatus: "unavailable"}
	if status.RuntimeMode == "" {
		status.RuntimeMode = "direct"
	}
	if state.BackupID != "" {
		status.Backup = &UpdateBackupSummary{ID: state.BackupID, Mode: state.BackupMode, CreatedAt: state.BackupCreatedAt, Version: state.BackupVersion}
	}
	status.ForceInstallAllowed = state.Phase == coreupdater.PhaseFailed && state.Error == drainFailureMessage
	status.Capable, status.CapabilityReason = m.current.InstallationCapability()
	if status.Capable && len(m.trustedKeys) == 0 {
		status.Capable = false
		status.CapabilityReason = "release signing public key is not configured"
	}
	if len(m.trustedKeys) > 0 {
		status.SignatureStatus = "trusted"
	}
	if status.Capable && m.capabilityError != "" {
		status.Capable = false
		status.CapabilityReason = m.capabilityError
	}
	status.Capability = "notification_only"
	if status.Capable {
		status.Capability = "native"
	}
	m.mu.Lock()
	if m.lastCheck != nil {
		check := *m.lastCheck
		status.Latest = &check.Release
		status.UpdateAvailable = check.UpdateAvailable
		status.CheckedAt = check.CheckedAt
		status.CacheStale = check.FromCache
		status.CacheUpdatedAt = check.CheckedAt
	}
	m.mu.Unlock()
	if status.Latest != nil && !coreupdater.IsActiveOperationPhase(state.Phase) {
		preflight := m.buildPreflight(*status.Latest)
		status.Preflight = &preflight
	}
	return status, nil
}

func (m *UpdateManager) HealthStore() *coreupdater.HealthHandshakeStore {
	if m == nil {
		return nil
	}
	return m.healthStore
}

func (m *UpdateManager) Check(ctx context.Context) (UpdateStatus, error) {
	if m.checker == nil {
		return m.Status(ctx)
	}
	if !m.operationMu.TryLock() {
		return UpdateStatus{}, ErrUpdateOperationConflict
	}
	defer m.operationMu.Unlock()
	m.mu.Lock()
	state, stateErr := m.state.Load()
	if stateErr != nil {
		m.mu.Unlock()
		return UpdateStatus{}, stateErr
	}
	if stateErr == nil && (state.Phase == coreupdater.PhaseIdle || state.Phase == coreupdater.PhaseAvailable || state.Phase == coreupdater.PhaseSucceeded || state.Phase == coreupdater.PhaseFailed || state.Phase == coreupdater.PhaseRolledBack) {
		currentVersion := state.CurrentVersion
		if state.BackupID == "" {
			currentVersion = m.current.Version
		}
		state, stateErr = m.state.Save(state.Generation, operationStateWithBackup(coreupdater.OperationState{Phase: coreupdater.PhaseChecking, CurrentVersion: currentVersion, TargetVersion: state.TargetVersion}, state))
	}
	if stateErr != nil {
		m.mu.Unlock()
		return UpdateStatus{}, stateErr
	}
	if state.Phase != coreupdater.PhaseChecking {
		m.mu.Unlock()
		return UpdateStatus{}, fmt.Errorf("update operation is active in phase %s", state.Phase)
	}
	result, err := m.checker.Check(ctx)
	if err != nil {
		publicError := "release check failed"
		if result.Release.TagName == "" {
			_, _ = m.state.Save(state.Generation, operationStateWithBackup(coreupdater.OperationState{Phase: coreupdater.PhaseFailed, CurrentVersion: state.CurrentVersion, TargetVersion: state.TargetVersion, Error: publicError}, state))
			m.mu.Unlock()
			return UpdateStatus{}, err
		}
		m.lastCheck = &result
		phase := coreupdater.PhaseIdle
		if result.UpdateAvailable {
			phase = coreupdater.PhaseAvailable
		}
		_, _ = m.state.Save(state.Generation, operationStateWithBackup(coreupdater.OperationState{Phase: phase, CurrentVersion: state.CurrentVersion, TargetVersion: result.Release.TagName, Error: publicError}, state))
		m.mu.Unlock()
		status, statusErr := m.Status(ctx)
		if statusErr != nil {
			return UpdateStatus{}, statusErr
		}
		status.LastError = publicError
		return status, err
	}
	m.lastCheck = &result
	phase := coreupdater.PhaseIdle
	if result.UpdateAvailable {
		phase = coreupdater.PhaseAvailable
	}
	_, stateErr = m.state.Save(state.Generation, operationStateWithBackup(coreupdater.OperationState{Phase: phase, CurrentVersion: state.CurrentVersion, TargetVersion: result.Release.TagName}, state))
	m.mu.Unlock()
	if stateErr != nil {
		return UpdateStatus{}, stateErr
	}
	status, err := m.Status(ctx)
	if err != nil {
		return UpdateStatus{}, err
	}
	return status, nil
}

func (m *UpdateManager) Authorize(ctx context.Context, user *models.User, session *models.UserSession, password, action, target string) (string, time.Time, error) {
	if m.auth == nil || user == nil || session == nil {
		return "", time.Time{}, fmt.Errorf("administrator authorization is unavailable")
	}
	if err := m.auth.VerifyPassword(ctx, user.ID, password); err != nil {
		return "", time.Time{}, err
	}
	return m.authorizer.Issue(coreupdater.AuthorizationBinding{UserID: user.ID, SessionID: session.ID, Action: action, Target: target}, 5*time.Minute)
}

func (m *UpdateManager) Install(ctx context.Context, user *models.User, session *models.UserSession, token, operationID, target string, mode coreupdater.BackupMode) (coreupdater.OperationState, error) {
	return m.install(ctx, user, session, token, operationID, target, mode, false)
}

func (m *UpdateManager) ForceInstall(ctx context.Context, user *models.User, session *models.UserSession, token, operationID, target string, mode coreupdater.BackupMode) (coreupdater.OperationState, error) {
	return m.install(ctx, user, session, token, operationID, target, mode, true)
}

func (m *UpdateManager) install(ctx context.Context, user *models.User, session *models.UserSession, token, operationID, target string, mode coreupdater.BackupMode, force bool) (result coreupdater.OperationState, returnErr error) {
	if user == nil || session == nil {
		return coreupdater.OperationState{}, fmt.Errorf("administrator session is required")
	}
	if capable, reason := m.current.InstallationCapability(); !capable || len(m.trustedKeys) == 0 {
		if reason == "" {
			reason = "release signing public key is not configured"
		}
		return coreupdater.OperationState{}, fmt.Errorf("%w: %s", ErrUpdateUnavailable, reason)
	}
	if m.capabilityError != "" {
		return coreupdater.OperationState{}, fmt.Errorf("%w: %s", ErrUpdateUnavailable, m.capabilityError)
	}
	if mode == "" {
		mode = m.backupMode
	}
	if mode != coreupdater.BackupModeCompact && mode != coreupdater.BackupModeFull {
		return coreupdater.OperationState{}, fmt.Errorf("%w: invalid backup mode %q", ErrInvalidUpdateRequest, mode)
	}
	if strings.TrimSpace(operationID) == "" {
		operationID = strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	if !coreupdater.ValidOperationID(operationID) {
		return coreupdater.OperationState{}, fmt.Errorf("%w: invalid operation ID", ErrInvalidUpdateRequest)
	}
	action := "install"
	if force {
		action = "force_install"
	}
	if !m.operationMu.TryLock() {
		state, loadErr := m.state.Load()
		if loadErr == nil && state.OperationID == operationID && state.Action == action && state.RequestTarget == target {
			return state, nil
		}
		return state, ErrUpdateOperationConflict
	}
	defer m.operationMu.Unlock()
	state, err := m.state.Load()
	if err != nil {
		return coreupdater.OperationState{}, err
	}
	if state.OperationID == operationID && state.Action == action && state.RequestTarget == target && state.Phase != coreupdater.PhaseIdle && state.Phase != coreupdater.PhaseAvailable {
		return state, nil
	}
	if coreupdater.IsActiveOperationPhase(state.Phase) {
		return state, ErrUpdateOperationConflict
	}
	if force {
		if state.Phase != coreupdater.PhaseFailed || state.TargetVersion != target || state.Error != drainFailureMessage {
			return state, fmt.Errorf("%w: force install is only allowed after task drain timeout", ErrInvalidUpdateRequest)
		}
	}
	backupPaths, err := m.resolveBackupPaths(mode)
	if err != nil {
		return coreupdater.OperationState{}, err
	}
	m.mu.Lock()
	if m.lastCheck == nil || !m.lastCheck.UpdateAvailable || m.lastCheck.Release.TagName != target {
		m.mu.Unlock()
		return coreupdater.OperationState{}, fmt.Errorf("%w: target release is not checked or no longer available", ErrInvalidUpdateRequest)
	}
	release := m.lastCheck.Release
	m.mu.Unlock()
	preflight := m.buildPreflight(release)
	backupBytes := preflight.EstimatedCompactBackupBytes
	if mode == coreupdater.BackupModeFull {
		backupBytes = preflight.EstimatedFullBackupBytes
	}
	requiredBytes := preflight.EstimatedDownloadBytes*2 + backupBytes
	if preflight.AvailableBytes > 0 && requiredBytes > 0 && uint64(requiredBytes) > preflight.AvailableBytes {
		return state, fmt.Errorf("%w: insufficient disk space for update", ErrInvalidUpdateRequest)
	}
	if err := m.authorizer.Consume(token, coreupdater.AuthorizationBinding{UserID: user.ID, SessionID: session.ID, Action: action, Target: target}); err != nil {
		return coreupdater.OperationState{}, err
	}
	state, err = m.state.Save(state.Generation, operationStateWithBackup(coreupdater.OperationState{OperationID: operationID, Action: action, RequestTarget: target, Phase: coreupdater.PhaseDownloading, CurrentVersion: m.current.Version, TargetVersion: target}, state))
	if err != nil {
		return coreupdater.OperationState{}, err
	}
	stageRoot := filepath.Join(m.installRoot, "data", "updates", "staged", operationID)
	staged, err := m.stageRelease(ctx, coreupdater.StageOptions{Root: stageRoot, Release: release, GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, HTTPClient: m.httpClient, TrustedKeys: m.trustedKeys, DownloadURLTemplate: m.downloadURLTemplate})
	if err != nil {
		state.Phase = coreupdater.PhaseFailed
		state.Error = "release download or verification failed"
		_, _ = m.state.Save(state.Generation, state)
		return state, err
	}
	state, err = m.state.Save(state.Generation, operationStateWithBackup(coreupdater.OperationState{OperationID: operationID, Phase: coreupdater.PhaseVerifying, CurrentVersion: m.current.Version, TargetVersion: target}, state))
	if err != nil {
		return coreupdater.OperationState{}, err
	}
	state, err = m.state.Save(state.Generation, operationStateWithBackup(coreupdater.OperationState{OperationID: operationID, Phase: coreupdater.PhaseStaged, CurrentVersion: m.current.Version, TargetVersion: target}, state))
	if err != nil {
		return coreupdater.OperationState{}, err
	}
	if err := m.maintenance.Enter(operationID, m.now().UTC()); err != nil {
		state, saveErr := m.failBeforeHandoff(state, "update maintenance could not start")
		return state, errors.Join(ErrUpdateOperationConflict, err, saveErr)
	}
	dispatched := false
	defer func() {
		if dispatched {
			return
		}
		if m.tasks != nil {
			m.tasks.ResumeClaims()
		}
		m.maintenance.Exit(operationID)
		if current, loadErr := m.state.Load(); loadErr == nil && canFailUpdatePhase(current.Phase) {
			message := "update helper handoff failed"
			if errors.Is(returnErr, context.DeadlineExceeded) {
				message = drainFailureMessage
			}
			current.Error = message
			current.Phase = coreupdater.PhaseFailed
			var saveErr error
			result, saveErr = m.state.Save(current.Generation, current)
			returnErr = errors.Join(returnErr, saveErr)
		} else if loadErr != nil {
			returnErr = errors.Join(returnErr, loadErr)
		}
	}()
	if m.tasks != nil {
		m.tasks.PauseClaims()
		if force {
			m.tasks.CancelActiveTasks()
			cancelCtx, cancel := context.WithTimeout(ctx, m.drainTimeout)
			err := m.tasks.WaitForIdle(cancelCtx)
			cancel()
			if err != nil {
				return state, errors.Join(ErrUpdateDrainTimeout, err)
			}
		} else {
			drainCtx, cancel := context.WithTimeout(ctx, m.drainTimeout)
			err := m.tasks.WaitForIdle(drainCtx)
			cancel()
			if err != nil {
				return state, errors.Join(ErrUpdateDrainTimeout, err)
			}
		}
	}
	state, err = m.state.Save(state.Generation, operationStateWithBackup(coreupdater.OperationState{OperationID: operationID, Phase: coreupdater.PhaseDraining, CurrentVersion: m.current.Version, TargetVersion: target}, state))
	if err != nil {
		return coreupdater.OperationState{}, err
	}
	executable, err := os.Executable()
	if err != nil {
		return state, err
	}
	relativeExecutable, err := filepath.Rel(m.installRoot, executable)
	if err != nil {
		return state, err
	}
	healthToken, err := newHealthToken()
	if err != nil {
		return state, err
	}
	if m.healthStore != nil {
		if err := m.healthStore.Issue(healthToken, target, m.now().UTC().Add(m.healthTimeout+time.Minute)); err != nil {
			return state, err
		}
	}
	archivePath, _ := filepath.Rel(staged.Root, staged.ArchivePath)
	checksumsPath, _ := filepath.Rel(staged.Root, staged.ChecksumsPath)
	signaturePath, _ := filepath.Rel(staged.Root, staged.SignaturePath)
	job := coreupdater.HelperJob{OperationID: operationID, InstallRoot: m.installRoot, StagedRoot: staged.Root, TargetVersion: target, CurrentVersion: m.current.Version, MainPID: os.Getpid(), ExpiresAt: m.now().UTC().Add(10 * time.Minute), ExecutablePath: relativeExecutable, BackupRoot: filepath.Join(m.installRoot, "data", "updates", "backups"), BackupMode: mode, BackupPaths: backupPaths, RetainCount: m.retainCount, RetainDays: m.retainDays, Arguments: m.arguments, WorkingDirectory: m.workingDir, HealthToken: healthToken, HealthURL: m.healthURL, RuntimeMode: m.runtimeMode, ServiceName: m.serviceName, MainExecutablePath: executable, ArchivePath: filepath.ToSlash(archivePath), ChecksumsPath: filepath.ToSlash(checksumsPath), SignaturePath: filepath.ToSlash(signaturePath), ArchiveAssetName: filepath.Base(staged.ArchivePath), ArchiveSHA256: staged.ArchiveSHA256}
	signed, err := coreupdater.SignHelperJob(job, m.jobSecret)
	if err != nil {
		return state, err
	}
	jobPath := filepath.Join(stageRoot, "helper-job.json")
	jobFile, err := os.OpenFile(jobPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return state, err
	}
	err = json.NewEncoder(jobFile).Encode(signed)
	_ = jobFile.Close()
	if err != nil {
		return state, err
	}
	if err := m.launchHelper(ctx, jobPath); err != nil {
		return state, err
	}
	dispatched = true
	if m.requestShutdown != nil {
		m.requestShutdown()
	}
	return state, nil
}

func (m *UpdateManager) Rollback(ctx context.Context, user *models.User, session *models.UserSession, token, operationID, backupID string) (result coreupdater.OperationState, returnErr error) {
	if user == nil || session == nil {
		return coreupdater.OperationState{}, fmt.Errorf("administrator session is required")
	}
	if strings.TrimSpace(operationID) == "" {
		operationID = strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	if !coreupdater.ValidOperationID(operationID) {
		return coreupdater.OperationState{}, fmt.Errorf("%w: invalid operation ID", ErrInvalidUpdateRequest)
	}
	if !m.operationMu.TryLock() {
		state, loadErr := m.state.Load()
		if loadErr == nil && state.OperationID == operationID && state.Action == "rollback" && state.RequestTarget == backupID {
			return state, nil
		}
		return state, ErrUpdateOperationConflict
	}
	defer m.operationMu.Unlock()
	state, err := m.state.Load()
	if err != nil {
		return coreupdater.OperationState{}, err
	}
	if state.OperationID == operationID && state.Action == "rollback" && state.RequestTarget == backupID {
		return state, nil
	}
	if coreupdater.IsActiveOperationPhase(state.Phase) {
		return state, ErrUpdateOperationConflict
	}
	if (state.Phase != coreupdater.PhaseSucceeded && state.Phase != coreupdater.PhaseAvailable && state.Phase != coreupdater.PhaseIdle) || state.BackupID != backupID || state.CurrentVersion == "" {
		return state, fmt.Errorf("%w: requested backup is not the latest completed update backup", ErrInvalidUpdateRequest)
	}
	if err := m.authorizer.Consume(token, coreupdater.AuthorizationBinding{UserID: user.ID, SessionID: session.ID, Action: "rollback", Target: backupID}); err != nil {
		return coreupdater.OperationState{}, err
	}
	previousState := state
	if err := m.maintenance.Enter(operationID, m.now().UTC()); err != nil {
		return state, errors.Join(ErrUpdateOperationConflict, err)
	}
	dispatched := false
	transitioned := false
	defer func() {
		if dispatched {
			return
		}
		if m.tasks != nil {
			m.tasks.ResumeClaims()
		}
		m.maintenance.Exit(operationID)
		if !transitioned {
			return
		}
		current, loadErr := m.state.Load()
		if loadErr != nil {
			returnErr = errors.Join(returnErr, loadErr)
			return
		}
		if current.Phase != coreupdater.PhaseRollingBack {
			return
		}
		previousState.Generation = 0
		previousState.UpdatedAt = time.Time{}
		var saveErr error
		result, saveErr = m.state.Save(current.Generation, previousState)
		returnErr = errors.Join(returnErr, saveErr)
	}()
	if m.tasks != nil {
		m.tasks.PauseClaims()
		drainCtx, cancel := context.WithTimeout(ctx, m.drainTimeout)
		err = m.tasks.WaitForIdle(drainCtx)
		cancel()
		if err != nil {
			return state, errors.Join(ErrUpdateDrainTimeout, err)
		}
	}
	rollbackVersion := state.BackupVersion
	if rollbackVersion == "" {
		rollbackVersion = state.CurrentVersion
	}
	state, err = m.state.Save(state.Generation, operationStateWithBackup(coreupdater.OperationState{OperationID: operationID, Action: "rollback", RequestTarget: backupID, Phase: coreupdater.PhaseRollingBack, CurrentVersion: m.current.Version, TargetVersion: rollbackVersion}, state))
	if err != nil {
		return state, err
	}
	transitioned = true
	executable, err := os.Executable()
	if err != nil {
		return state, err
	}
	relativeExecutable, err := filepath.Rel(m.installRoot, executable)
	if err != nil {
		return state, err
	}
	stageRoot := filepath.Join(m.installRoot, "data", "updates", "staged", operationID)
	if err := os.MkdirAll(stageRoot, 0o700); err != nil {
		return state, err
	}
	healthToken, err := newHealthToken()
	if err != nil {
		return state, err
	}
	if m.healthStore != nil {
		if err := m.healthStore.Issue(healthToken, state.TargetVersion, m.now().UTC().Add(m.healthTimeout+time.Minute)); err != nil {
			return state, err
		}
	}
	job := coreupdater.HelperJob{OperationID: operationID, InstallRoot: m.installRoot, StagedRoot: stageRoot, TargetVersion: state.TargetVersion, CurrentVersion: m.current.Version, MainPID: os.Getpid(), ExpiresAt: m.now().UTC().Add(10 * time.Minute), ExecutablePath: relativeExecutable, BackupRoot: filepath.Join(m.installRoot, "data", "updates", "backups"), BackupMode: m.backupMode, BackupPaths: m.backupPaths, RetainCount: m.retainCount, RetainDays: m.retainDays, Arguments: m.arguments, WorkingDirectory: m.workingDir, HealthToken: healthToken, HealthURL: m.healthURL, RuntimeMode: m.runtimeMode, ServiceName: m.serviceName, MainExecutablePath: executable, ManualRollback: true, RestoreBackupID: backupID}
	signed, err := coreupdater.SignHelperJob(job, m.jobSecret)
	if err != nil {
		return state, err
	}
	jobPath := filepath.Join(stageRoot, "helper-job.json")
	jobFile, err := os.OpenFile(jobPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return state, err
	}
	err = json.NewEncoder(jobFile).Encode(signed)
	_ = jobFile.Close()
	if err != nil {
		return state, err
	}
	if err := m.launchHelper(ctx, jobPath); err != nil {
		return state, err
	}
	dispatched = true
	if m.requestShutdown != nil {
		m.requestShutdown()
	}
	return state, nil
}

func canFailUpdatePhase(phase coreupdater.OperationPhase) bool {
	return coreupdater.CanTransition(phase, coreupdater.PhaseFailed)
}

func operationStateWithBackup(next, previous coreupdater.OperationState) coreupdater.OperationState {
	if next.OperationID == "" {
		next.OperationID = previous.OperationID
	}
	if next.Action == "" {
		next.Action = previous.Action
	}
	if next.RequestTarget == "" {
		next.RequestTarget = previous.RequestTarget
	}
	next.BackupID = previous.BackupID
	next.BackupMode = previous.BackupMode
	next.BackupCreatedAt = previous.BackupCreatedAt
	next.BackupVersion = previous.BackupVersion
	return next
}

func (m *UpdateManager) failBeforeHandoff(state coreupdater.OperationState, message string) (coreupdater.OperationState, error) {
	if !canFailUpdatePhase(state.Phase) {
		return state, fmt.Errorf("cannot fail updater phase %q", state.Phase)
	}
	state.Phase = coreupdater.PhaseFailed
	state.Error = message
	return m.state.Save(state.Generation, state)
}

func newHealthToken() (string, error) {
	digest := sha256.Sum256([]byte(uuid.NewString() + time.Now().String()))
	return base64.RawURLEncoding.EncodeToString(digest[:]), nil
}

func (m *UpdateManager) buildPreflight(release coreupdater.Release) UpdatePreflight {
	preflight := UpdatePreflight{MaintenanceImpact: "writes are paused while tasks drain; the service restarts after helper handoff"}
	if m.tasks != nil {
		preflight.ActiveTaskCount = m.tasks.ActiveTaskCount()
	}
	if asset, err := release.AssetFor(m.current.GOOS, m.current.GOARCH); err == nil {
		preflight.EstimatedDownloadBytes += asset.Size
	}
	if asset, err := release.AssetNamed("SHA256SUMS"); err == nil {
		preflight.EstimatedDownloadBytes += asset.Size
	}
	if asset, err := release.AssetNamed("SHA256SUMS.sig"); err == nil {
		preflight.EstimatedDownloadBytes += asset.Size
	}
	compactPaths := m.backupPaths
	fullPaths, err := m.resolveBackupPaths(coreupdater.BackupModeFull)
	if err == nil {
		preflight.EstimatedCompactBackupBytes = m.estimateBackupBytes(compactPaths)
		preflight.EstimatedFullBackupBytes = m.estimateBackupBytes(fullPaths)
	} else {
		preflight.Error = "backup estimate unavailable"
	}
	if available, err := coreupdater.AvailableDiskBytes(m.installRoot); err == nil {
		preflight.AvailableBytes = available
	} else if preflight.Error == "" {
		preflight.Error = "available disk space unavailable"
	}
	return preflight
}

func (m *UpdateManager) estimateBackupBytes(paths []string) int64 {
	var total int64
	for _, relative := range paths {
		root := filepath.Join(m.installRoot, filepath.FromSlash(relative))
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil || entry == nil {
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if info, err := entry.Info(); err == nil && info.Mode().IsRegular() {
				total += info.Size()
			}
			return nil
		})
	}
	return total
}

func launchUpdateHelper(ctx context.Context, jobPath, configPath, runtimeMode, serviceName string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	if runtimeMode == "systemd" {
		pendingPath := filepath.Join(filepath.Dir(executable), "data", "updates", "pending-helper.json")
		return copyUpdateHelperFile(jobPath, pendingPath, 0o600)
	}
	operationID := filepath.Base(filepath.Dir(jobPath))
	helperName := "ice_art-helper"
	if runtime.GOOS == "windows" {
		helperName += ".exe"
	}
	helperPath := filepath.Join(filepath.Dir(executable), "data", "updates", "helpers", operationID, helperName)
	if err := copyUpdateHelperFile(executable, helperPath, 0o700); err != nil {
		return err
	}
	arguments := []string{"--update-helper-job", jobPath}
	if strings.TrimSpace(runtimeMode) != "" {
		arguments = append(arguments, "--runtime-mode", runtimeMode)
	}
	if strings.TrimSpace(serviceName) != "" {
		arguments = append(arguments, "--service-name", serviceName)
	}
	if strings.TrimSpace(configPath) != "" {
		arguments = append(arguments, "--config", configPath)
	}
	command := exec.Command(helperPath, arguments...)
	command.Dir = filepath.Dir(executable)
	return command.Start()
}

func copyUpdateHelperFile(source, destination string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	temp, err := os.CreateTemp(filepath.Dir(destination), ".helper-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if _, err := io.Copy(temp, input); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Chmod(mode); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, destination)
}

func (m *UpdateManager) resolveBackupPaths(mode coreupdater.BackupMode) ([]string, error) {
	if mode != coreupdater.BackupModeFull {
		return append([]string(nil), m.backupPaths...), nil
	}
	paths := []string{"conf", "workspace"}
	entries, err := os.ReadDir(filepath.Join(m.installRoot, "data"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, entry := range entries {
		if entry.Name() == "updates" || entry.Name() == "logs" {
			continue
		}
		paths = append(paths, filepath.ToSlash(filepath.Join("data", entry.Name())))
	}
	paths = append(paths, "data/updates/managed-manifest.json")
	return paths, nil
}
