package commands

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"time"

	"github.com/EquentR/agent_runtime/app/config"
	coreupdater "github.com/EquentR/agent_runtime/core/updater"
)

func RunUpdateHelper(cfg *config.Config, jobPath, signingKeyID, signingPublicKey, expectedRuntimeMode, expectedServiceName, stateOwner, protectedRoot string) error {
	if cfg == nil {
		return fmt.Errorf("helper config is required")
	}
	file, err := os.Open(jobPath)
	if err != nil {
		return err
	}
	defer file.Close()
	var signed coreupdater.SignedHelperJob
	decoder := json.NewDecoder(io.LimitReader(file, 2<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&signed); err != nil {
		return fmt.Errorf("decode helper job: %w", err)
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	installRoot, err := resolveHelperInstallRoot(executable, jobPath)
	if err != nil {
		return err
	}
	secret := sha256.Sum256([]byte(cfg.Security.AppSecret))
	job, err := coreupdater.VerifyHelperJob(signed, secret[:], time.Now().UTC(), installRoot)
	if err != nil {
		return err
	}
	if expectedRuntimeMode != "" && job.RuntimeMode != expectedRuntimeMode {
		return fmt.Errorf("helper runtime mode does not match fixed launcher mode")
	}
	if expectedServiceName != "" && job.ServiceName != expectedServiceName {
		return fmt.Errorf("helper service name does not match fixed launcher service")
	}
	if protectedRoot != "" {
		protectedRoot, err = filepath.Abs(protectedRoot)
		if err != nil || filepath.Clean(protectedRoot) == filepath.Clean(job.InstallRoot) {
			return fmt.Errorf("protected updater root is invalid")
		}
		job.BackupRoot = filepath.Join(protectedRoot, "backups")
		job.RetainCount = cfg.Updates.Backup.RetainCount
		job.RetainDays = cfg.Updates.Backup.RetainDays
	}
	owner, err := resolveUpdateFileOwner(stateOwner)
	if err != nil {
		return err
	}
	trustedKeys, err := coreupdater.ParseTrustedPublicKey(signingKeyID, signingPublicKey)
	if err != nil {
		return err
	}
	store, err := coreupdater.NewStateStore(filepath.Join(installRoot, "data", "updates"))
	if err != nil {
		return err
	}
	store.SetOwner(owner)
	err = coreupdater.ExecuteHelper(context.Background(), coreupdater.HelperExecutionOptions{
		Job:              job,
		Runtime:          coreupdater.NewPlatformRuntime(job.RuntimeMode, job.ServiceName, job.HealthURL, nil),
		StateStore:       store,
		HealthTimeout:    cfg.Updates.HealthTimeout,
		TrustedKeys:      trustedKeys,
		StartEnvironment: os.Environ(),
		FileOwner:        owner,
	})
	if err == nil && filepath.Base(jobPath) == "pending-helper.json" {
		err = os.Remove(jobPath)
	}
	return err
}

func resolveUpdateFileOwner(name string) (*coreupdater.FileOwner, error) {
	if name == "" {
		return nil, nil
	}
	account, err := user.Lookup(name)
	if err != nil {
		return nil, err
	}
	uid, err := strconv.Atoi(account.Uid)
	if err != nil {
		return nil, fmt.Errorf("parse updater owner UID: %w", err)
	}
	gid, err := strconv.Atoi(account.Gid)
	if err != nil {
		return nil, fmt.Errorf("parse updater owner GID: %w", err)
	}
	return &coreupdater.FileOwner{UID: uid, GID: gid}, nil
}

func resolveHelperInstallRoot(executable, jobPath string) (string, error) {
	executable, err := filepath.Abs(executable)
	if err != nil {
		return "", err
	}
	jobPath, err = filepath.Abs(jobPath)
	if err != nil {
		return "", err
	}
	executableDir := filepath.Dir(executable)
	pending := filepath.Join(executableDir, "data", "updates", "pending-helper.json")
	if filepath.Clean(jobPath) == filepath.Clean(pending) {
		return executableDir, nil
	}
	installRoot := executableDir
	for range 4 {
		installRoot = filepath.Dir(installRoot)
	}
	relative, err := filepath.Rel(installRoot, executableDir)
	if err != nil || filepath.ToSlash(filepath.Dir(relative)) != "data/updates/helpers" {
		return "", fmt.Errorf("update helper is not running from the managed helper directory")
	}
	return installRoot, nil
}
