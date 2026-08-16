package commands

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/EquentR/agent_runtime/app/config"
	coreaudit "github.com/EquentR/agent_runtime/core/audit"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	"github.com/EquentR/agent_runtime/pkg/db"
)

func TestRunStorageMaintenanceDryRun(t *testing.T) {
	templateRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(templateRoot, "AGENTS.md"), []byte("# Maintenance template\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(AGENTS.md) error = %v", err)
	}
	cfg := config.Config{
		WorkspaceDir: templateRoot,
		Workspaces: config.WorkspacesConfig{
			Root:            t.TempDir(),
			TaskRetention:   durationPtr(24 * time.Hour),
			BackupRetention: durationPtr(24 * time.Hour),
		},
		Sqlite: db.Database{
			Name:     "maintenance",
			DbDir:    t.TempDir(),
			InMemory: true,
			Params:   []string{"_pragma=journal_mode(WAL)"},
		},
		Attachments: config.AttachmentStorageConfig{
			Filesystem: config.AttachmentFilesystemConfig{Root: t.TempDir()},
		},
		Security: config.SecurityConfig{AppSecret: "maintenance-test"},
	}
	if err := RunStorageMaintenance(&cfg, "0.1.3-test", true, false, false); err != nil {
		t.Fatalf("RunStorageMaintenance(dry run) error = %v", err)
	}

	taskStore := coretasks.NewStore(db.DB())
	ctx := context.Background()
	created, _, err := taskStore.CreateTask(ctx, coretasks.CreateTaskInput{TaskType: "agent.run"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	if _, err := taskStore.AppendEvent(ctx, created.ID, coretasks.EventLogMessage, "info", map[string]any{"Kind": "text_delta", "Text": "he"}); err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}
	auditStore := coreaudit.NewStore(db.DB())
	if _, err := auditStore.CreateRun(ctx, coreaudit.StartRunInput{RunID: "run_legacy", TaskID: "task_legacy", TaskType: "agent.run"}); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}
	if _, err := auditStore.CreateArtifact(ctx, "run_legacy", coreaudit.CreateArtifactInput{
		Kind: coreaudit.ArtifactKindRequestMessages, MimeType: "application/json",
		Body: map[string]any{"messages": []map[string]any{{"role": "user", "content": "legacy"}}},
	}); err != nil {
		t.Fatalf("CreateArtifact() error = %v", err)
	}

	if err := RunStorageMaintenance(&cfg, "0.1.3-test", false, true, true); err != nil {
		t.Fatalf("RunStorageMaintenance(apply+vacuum) error = %v", err)
	}
	count, err := taskStore.CountStreamDeltaEvents(ctx)
	if err != nil {
		t.Fatalf("CountStreamDeltaEvents() error = %v", err)
	}
	if count != 0 {
		t.Fatalf("CountStreamDeltaEvents() after apply = %d, want 0", count)
	}
	legacyCount, err := auditStore.CountLegacyRedundantArtifacts(ctx)
	if err != nil {
		t.Fatalf("CountLegacyRedundantArtifacts() error = %v", err)
	}
	if legacyCount != 0 {
		t.Fatalf("CountLegacyRedundantArtifacts() after apply = %d, want 0", legacyCount)
	}
	backupEntries, err := os.ReadDir(filepath.Join(cfg.Sqlite.DbDir, "maintenance-backups"))
	if err != nil {
		t.Fatalf("ReadDir(maintenance-backups) error = %v", err)
	}
	if len(backupEntries) == 0 {
		t.Fatal("maintenance backup file missing after apply")
	}
}

func TestRunStorageMaintenanceRejectsMissingVersion(t *testing.T) {
	cfg := config.Config{}
	if err := RunStorageMaintenance(&cfg, "", false, false, false); err == nil {
		t.Fatal("RunStorageMaintenance(empty version) error = nil, want non-nil")
	}
}

func TestRunStorageMaintenanceRejectsVacuumWhenDisabled(t *testing.T) {
	disabled := false
	cfg := config.Config{Storage: config.StorageConfig{VacuumEnabled: &disabled}}
	if err := RunStorageMaintenance(&cfg, "0.1.3-test", false, true, true); err == nil {
		t.Fatal("RunStorageMaintenance(vacuum disabled) error = nil, want non-nil")
	}
}

func durationPtr(value time.Duration) *time.Duration {
	return &value
}
