package updater

import (
	"bytes"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestSignedHelperJobBindsInstallRootAndExpiry(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 7, 19, 1, 2, 3, 0, time.UTC)
	job := HelperJob{
		OperationID:   "op-1",
		InstallRoot:   root,
		StagedRoot:    filepath.Join(root, "data", "updates", "staged", "op-1"),
		TargetVersion: "v1.2.3",
		MainPID:       123,
		ExpiresAt:     now.Add(time.Minute),
		BackupMode:    BackupModeCompact,
	}
	job.ExecutablePath = "ice_art"
	if runtime.GOOS == "windows" {
		job.ExecutablePath = "ice_art.exe"
	}
	secret := bytes.Repeat([]byte{7}, 32)
	signed, err := SignHelperJob(job, secret)
	if err != nil {
		t.Fatalf("SignHelperJob() error = %v", err)
	}
	verified, err := VerifyHelperJob(signed, secret, now, root)
	if err != nil || verified.OperationID != "op-1" {
		t.Fatalf("VerifyHelperJob() = %#v, %v", verified, err)
	}
	signed.Job.StagedRoot = filepath.Join(filepath.Dir(root), "outside")
	if _, err := VerifyHelperJob(signed, secret, now, root); err == nil {
		t.Fatal("VerifyHelperJob(tampered) error = nil")
	}
	signed, _ = SignHelperJob(job, secret)
	if _, err := VerifyHelperJob(signed, secret, now.Add(2*time.Minute), root); err == nil {
		t.Fatal("VerifyHelperJob(expired) error = nil")
	}
}

func TestSignedHelperJobRejectsArbitraryBackupRootAndOperationID(t *testing.T) {
	root := t.TempDir()
	secret := bytes.Repeat([]byte{9}, 32)
	job := HelperJob{OperationID: "../escape", InstallRoot: root, StagedRoot: filepath.Join(root, "data", "updates", "staged", "op"), TargetVersion: "v1.2.3", MainPID: 123, ExpiresAt: time.Now().Add(time.Minute), BackupRoot: filepath.Join(filepath.Dir(root), "outside")}
	signed, _ := SignHelperJob(job, secret)
	if _, err := VerifyHelperJob(signed, secret, time.Now(), root); err == nil {
		t.Fatal("VerifyHelperJob() error = nil, want unsafe operation rejection")
	}
	job.OperationID = "safe-op"
	signed, _ = SignHelperJob(job, secret)
	if _, err := VerifyHelperJob(signed, secret, time.Now(), root); err == nil {
		t.Fatal("VerifyHelperJob() error = nil, want arbitrary backup root rejection")
	}
}
