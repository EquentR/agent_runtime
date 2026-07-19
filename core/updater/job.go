package updater

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

var operationIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
var serviceNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.@-]{0,63}$`)

func ValidOperationID(value string) bool {
	return operationIDPattern.MatchString(strings.TrimSpace(value))
}

type HelperJob struct {
	OperationID          string     `json:"operation_id"`
	InstallRoot          string     `json:"install_root"`
	StagedRoot           string     `json:"staged_root"`
	TargetVersion        string     `json:"target_version"`
	CurrentVersion       string     `json:"current_version,omitempty"`
	MainPID              int        `json:"main_pid"`
	ExpiresAt            time.Time  `json:"expires_at"`
	ExecutablePath       string     `json:"executable_path"`
	StagedExecutablePath string     `json:"staged_executable_path"`
	BackupRoot           string     `json:"backup_root"`
	BackupMode           BackupMode `json:"backup_mode"`
	BackupPaths          []string   `json:"backup_paths"`
	RetainCount          int        `json:"retain_count,omitempty"`
	RetainDays           int        `json:"retain_days,omitempty"`
	Arguments            []string   `json:"arguments,omitempty"`
	WorkingDirectory     string     `json:"working_directory,omitempty"`
	HealthToken          string     `json:"health_token,omitempty"`
	HealthURL            string     `json:"health_url,omitempty"`
	RuntimeMode          string     `json:"runtime_mode,omitempty"`
	ServiceName          string     `json:"service_name,omitempty"`
	ManualRollback       bool       `json:"manual_rollback,omitempty"`
	RestoreBackupID      string     `json:"restore_backup_id,omitempty"`
	MainExecutablePath   string     `json:"main_executable_path"`
	ArchivePath          string     `json:"archive_path,omitempty"`
	ChecksumsPath        string     `json:"checksums_path,omitempty"`
	SignaturePath        string     `json:"signature_path,omitempty"`
	ArchiveAssetName     string     `json:"archive_asset_name,omitempty"`
	ArchiveSHA256        string     `json:"archive_sha256,omitempty"`
	StagedExecutableSHA  string     `json:"staged_executable_sha256,omitempty"`
}

type SignedHelperJob struct {
	Job HelperJob `json:"job"`
	MAC string    `json:"mac"`
}

func SignHelperJob(job HelperJob, secret []byte) (SignedHelperJob, error) {
	if len(secret) < 32 {
		return SignedHelperJob{}, fmt.Errorf("helper job secret must contain at least 32 bytes")
	}
	raw, err := json.Marshal(job)
	if err != nil {
		return SignedHelperJob{}, err
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(raw)
	return SignedHelperJob{Job: job, MAC: base64.StdEncoding.EncodeToString(mac.Sum(nil))}, nil
}

func VerifyHelperJob(signed SignedHelperJob, secret []byte, now time.Time, expectedInstallRoot string) (HelperJob, error) {
	if len(secret) < 32 {
		return HelperJob{}, fmt.Errorf("helper job secret must contain at least 32 bytes")
	}
	raw, err := json.Marshal(signed.Job)
	if err != nil {
		return HelperJob{}, err
	}
	provided, err := base64.StdEncoding.DecodeString(signed.MAC)
	if err != nil {
		return HelperJob{}, fmt.Errorf("decode helper job MAC: %w", err)
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(raw)
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return HelperJob{}, fmt.Errorf("helper job integrity check failed")
	}
	job := signed.Job
	if !operationIDPattern.MatchString(job.OperationID) || job.MainPID <= 0 || job.ExpiresAt.IsZero() || !now.UTC().Before(job.ExpiresAt.UTC()) {
		return HelperJob{}, fmt.Errorf("helper job is invalid or expired")
	}
	if _, err := parseReleaseVersion(job.TargetVersion); err != nil {
		return HelperJob{}, fmt.Errorf("invalid helper target version: %w", err)
	}
	if job.ManualRollback && !operationIDPattern.MatchString(job.RestoreBackupID) {
		return HelperJob{}, fmt.Errorf("helper rollback backup ID is invalid")
	}
	if job.BackupMode != BackupModeCompact && job.BackupMode != BackupModeFull {
		return HelperJob{}, fmt.Errorf("helper backup mode is invalid")
	}
	if job.RetainCount < 0 || job.RetainCount > 100 || job.RetainDays < 0 || job.RetainDays > 3650 {
		return HelperJob{}, fmt.Errorf("helper backup retention is invalid")
	}
	expectedRoot, err := canonicalRoot(expectedInstallRoot)
	if err != nil {
		return HelperJob{}, err
	}
	jobRoot, err := canonicalRoot(job.InstallRoot)
	if err != nil {
		return HelperJob{}, err
	}
	if !samePath(expectedRoot, jobRoot) {
		return HelperJob{}, fmt.Errorf("helper job install root mismatch")
	}
	stagedRoot, err := canonicalRoot(job.StagedRoot)
	if err != nil {
		return HelperJob{}, err
	}
	stagedBase := filepath.Join(expectedRoot, "data", "updates", "staged")
	if !pathWithin(stagedBase, stagedRoot) {
		return HelperJob{}, fmt.Errorf("helper staged root escapes install root")
	}
	backupRoot := filepath.Join(expectedRoot, "data", "updates", "backups")
	if strings.TrimSpace(job.BackupRoot) != "" {
		configuredBackupRoot, err := canonicalRoot(job.BackupRoot)
		if err != nil || !samePath(configuredBackupRoot, backupRoot) {
			return HelperJob{}, fmt.Errorf("helper backup root is not the fixed install backup root")
		}
	}
	if job.MainExecutablePath != "" {
		mainExecutable, err := canonicalRoot(job.MainExecutablePath)
		if err != nil || !samePath(mainExecutable, filepath.Join(expectedRoot, job.ExecutablePath)) {
			return HelperJob{}, fmt.Errorf("helper main executable path mismatch")
		}
	}
	binaryName := "ice_art"
	if runtime.GOOS == "windows" {
		binaryName = "ice_art.exe"
	}
	if filepath.Clean(job.ExecutablePath) != binaryName {
		return HelperJob{}, fmt.Errorf("helper executable path must be %s", binaryName)
	}
	if job.RuntimeMode == "systemd" || job.RuntimeMode == "windows-service" {
		if !serviceNamePattern.MatchString(job.ServiceName) {
			return HelperJob{}, fmt.Errorf("helper service name is invalid")
		}
	}
	job.InstallRoot = jobRoot
	job.StagedRoot = stagedRoot
	job.BackupRoot = backupRoot
	if job.MainExecutablePath == "" {
		job.MainExecutablePath = filepath.Join(expectedRoot, job.ExecutablePath)
	}
	return job, nil
}

func samePath(left, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func pathWithin(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
