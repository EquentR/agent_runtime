package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/EquentR/agent_runtime/app/config"
	"github.com/EquentR/agent_runtime/app/logics"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	coreupdater "github.com/EquentR/agent_runtime/core/updater"
	"github.com/EquentR/agent_runtime/pkg/log"
)

func initUpdateManager(cfg *config.Config, build coreupdater.BuildInfo, configPath, signingKeyID, signingPublicKey, runtimeMode, serviceName string, tasks *coretasks.Manager, gate *coreupdater.MaintenanceGate, auth *logics.AuthLogic, audit *logics.AdminAuditLogic) (*logics.UpdateManager, error) {
	if cfg == nil {
		return nil, fmt.Errorf("update config is required")
	}
	capabilityError := ""
	updateConfig := cfg.Updates
	if err := updateConfig.Validate(); err != nil {
		capabilityError = "update configuration is invalid: " + err.Error()
		updateConfig.GitHubAPIBaseURL = ""
		updateConfig.DownloadURLTemplate = ""
		updateConfig.Backup.DefaultMode = "compact"
		if updateConfig.RuntimeMode != "direct" && updateConfig.RuntimeMode != "systemd" && updateConfig.RuntimeMode != "windows-service" {
			runtimeMode = "direct"
		}
	}
	if !cfg.Updates.Enabled {
		capabilityError = "self-update is disabled by configuration"
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	installRoot := filepath.Dir(executable)
	updatesRoot := filepath.Join(installRoot, "data", "updates")
	state, err := coreupdater.NewStateStore(updatesRoot)
	if err != nil {
		return nil, err
	}
	if _, err := state.RecoverInterrupted(build.Version); err != nil && !errors.Is(err, coreupdater.ErrOperationLocked) {
		return nil, fmt.Errorf("recover interrupted updater state: %w", err)
	}
	healthStore, err := coreupdater.NewHealthHandshakeStore(updatesRoot)
	if err != nil {
		return nil, err
	}
	cache, err := coreupdater.NewReleaseCache(filepath.Join(updatesRoot, "release-cache.json"))
	if err != nil {
		return nil, err
	}
	releaseClient, err := coreupdater.NewReleaseClient(coreupdater.ReleaseClientOptions{
		BaseURL: updateConfig.ResolvedGitHubAPIBaseURL(),
		Token:   os.Getenv(updateConfig.ResolvedGitHubTokenEnv()),
	})
	if err != nil {
		return nil, err
	}
	checker := coreupdater.NewReleaseChecker(coreupdater.ReleaseCheckerOptions{Source: releaseClient, Cache: cache, CurrentVersion: build.Version})
	trustedKeys, keyErr := coreupdater.ParseTrustedPublicKey(signingKeyID, signingPublicKey)
	if keyErr != nil {
		log.Warnf("Self-update installation disabled: %v", keyErr)
		trustedKeys = nil
	}
	if configPath == "" {
		configPath = "conf/app.yaml"
	}
	configPath, _ = filepath.Abs(configPath)
	mode := coreupdater.BackupMode(updateConfig.Backup.ResolvedMode())
	healthURL := fmt.Sprintf("http://127.0.0.1:%d%s/health", cfg.Server.Port, cfg.Server.ApiBasePath)
	return logics.NewUpdateManager(logics.UpdateManagerOptions{
		Current: build, Checker: checker, StateStore: state, HealthStore: healthStore, Maintenance: gate, TaskManager: tasks,
		AuthLogic: auth, AuditLogic: audit, TrustedKeys: trustedKeys, InstallRoot: installRoot, ConfigPath: configPath,
		HealthURL: healthURL, Arguments: os.Args[1:], WorkingDir: installRoot, JobSecret: []byte(cfg.Security.AppSecret),
		HealthTimeout: updateConfig.ResolvedHealthTimeout(), DrainTimeout: updateConfig.ResolvedDrainTimeout(), BackupMode: mode,
		BackupPaths:         []string{"conf/app.yaml", "data/app.db", "workspace", "data/workspaces", "data/updates/managed-manifest.json"},
		DownloadURLTemplate: updateConfig.DownloadURLTemplate, RequestShutdown: RequestShutdown,
		RuntimeMode: runtimeMode, ServiceName: serviceName,
		RetainCount: updateConfig.Backup.RetainCount, RetainDays: updateConfig.Backup.RetainDays, CapabilityError: capabilityError,
	})
}

func startUpdateCheckLoop(manager *logics.UpdateManager, interval time.Duration) {
	if manager == nil || globalCtx == nil {
		return
	}
	if interval <= 0 {
		interval = time.Hour
	}
	go func() {
		check := func() {
			if _, err := manager.Check(globalCtx); err != nil {
				log.Warnf("Self-update check failed: %v", err)
			}
		}
		check()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-globalCtx.Done():
				return
			case <-ticker.C:
				check()
			}
		}
	}()
}
