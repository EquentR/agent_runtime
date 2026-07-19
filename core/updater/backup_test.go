package updater

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCreateAndRestoreBackupTracksExistingAndMissingPaths(t *testing.T) {
	installRoot := t.TempDir()
	backupRoot := t.TempDir()
	writeUpdaterTestFile(t, installRoot, "ice_art.exe", "old binary")
	writeUpdaterTestFile(t, installRoot, "conf/app.yaml", "old config")
	manifest, err := CreateBackup(BackupOptions{
		InstallRoot: installRoot,
		BackupRoot:  backupRoot,
		OperationID: "op-1",
		Mode:        BackupModeCompact,
		Paths:       []string{"ice_art.exe", "conf/app.yaml", "workspace/new.txt"},
		CreatedAt:   time.Date(2026, 7, 19, 1, 2, 3, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateBackup() error = %v", err)
	}
	if len(manifest.Entries) != 3 {
		t.Fatalf("len(Entries) = %d, want 3", len(manifest.Entries))
	}
	writeUpdaterTestFile(t, installRoot, "ice_art.exe", "new binary")
	writeUpdaterTestFile(t, installRoot, "workspace/new.txt", "new file")
	if err := RestoreBackup(installRoot, filepath.Join(backupRoot, "op-1")); err != nil {
		t.Fatalf("RestoreBackup() error = %v", err)
	}
	assertUpdaterTestFile(t, installRoot, "ice_art.exe", "old binary")
	if _, err := os.Stat(filepath.Join(installRoot, "workspace", "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("new path survived restore: %v", err)
	}
}

func TestPruneBackupsDeletesOnlyOwnedExpiredBackups(t *testing.T) {
	installRoot := t.TempDir()
	backupRoot := t.TempDir()
	for index, createdAt := range []time.Time{
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC),
	} {
		_, err := CreateBackup(BackupOptions{InstallRoot: installRoot, BackupRoot: backupRoot, OperationID: "op-" + string(rune('1'+index)), Mode: BackupModeCompact, Paths: []string{"missing"}, CreatedAt: createdAt})
		if err != nil {
			t.Fatalf("CreateBackup() error = %v", err)
		}
	}
	unowned := filepath.Join(backupRoot, "unowned")
	if err := os.MkdirAll(unowned, 0o755); err != nil {
		t.Fatalf("MkdirAll(unowned) error = %v", err)
	}
	removed, err := PruneBackups(backupRoot, 2, 30, time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("PruneBackups() error = %v", err)
	}
	if len(removed) != 1 || filepath.Base(removed[0]) != "op-1" {
		t.Fatalf("removed = %#v, want op-1 only", removed)
	}
	if _, err := os.Stat(unowned); err != nil {
		t.Fatalf("unowned directory removed: %v", err)
	}
}

func TestRestoreBackupValidatesAllSourcesBeforeMutatingInstall(t *testing.T) {
	installRoot := t.TempDir()
	backupRoot := t.TempDir()
	writeUpdaterTestFile(t, installRoot, "ice_art.exe", "old binary")
	writeUpdaterTestFile(t, installRoot, "conf/app.yaml", "old config")
	_, err := CreateBackup(BackupOptions{InstallRoot: installRoot, BackupRoot: backupRoot, OperationID: "op-validate", Mode: BackupModeCompact, Paths: []string{"ice_art.exe", "conf/app.yaml"}})
	if err != nil {
		t.Fatalf("CreateBackup() error = %v", err)
	}
	writeUpdaterTestFile(t, installRoot, "ice_art.exe", "current binary")
	writeUpdaterTestFile(t, installRoot, "conf/app.yaml", "current config")
	backupDir := filepath.Join(backupRoot, "op-validate")
	if err := os.WriteFile(filepath.Join(backupDir, "files", "conf", "app.yaml"), []byte("corrupt"), 0o644); err != nil {
		t.Fatalf("corrupt backup error = %v", err)
	}
	if err := RestoreBackup(installRoot, backupDir); err == nil {
		t.Fatal("RestoreBackup() error = nil, want corruption rejection")
	}
	assertUpdaterTestFile(t, installRoot, "ice_art.exe", "current binary")
	assertUpdaterTestFile(t, installRoot, "conf/app.yaml", "current config")
}
