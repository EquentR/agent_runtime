package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/EquentR/agent_runtime/app/config"
	"github.com/EquentR/agent_runtime/app/migration"
	"github.com/EquentR/agent_runtime/core/attachments"
	coreaudit "github.com/EquentR/agent_runtime/core/audit"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	"github.com/EquentR/agent_runtime/core/workspaces"
	"github.com/EquentR/agent_runtime/pkg/db"
	"github.com/EquentR/agent_runtime/pkg/log"
)

// RunStorageMaintenance 执行存储维护。version 必须与正式启动使用同一应用版本，
// 保证数据版本迁移路径与 Serve 一致。
func RunStorageMaintenance(cfg *config.Config, version string, dryRun bool, apply bool, vacuum bool) error {
	if cfg == nil {
		return fmt.Errorf("config is required")
	}
	if strings.TrimSpace(version) == "" {
		return fmt.Errorf("application version is required")
	}
	if vacuum && !cfg.Storage.ResolvedVacuumEnabled() {
		return fmt.Errorf("vacuum is disabled by storage.vacuumEnabled")
	}
	log.Init(&cfg.Log)
	db.Init(&cfg.Sqlite)
	migration.Bootstrap(version)
	ctx := context.Background()
	now := time.Now().UTC()

	taskStore := coretasks.NewStore(db.DB())
	auditStore := coreaudit.NewStore(db.DB())
	attachmentStorage, err := initAttachmentStorage(cfg.Attachments)
	if err != nil {
		return err
	}
	attachmentStore := attachments.NewStore(db.DB(), attachmentStorage)
	workspaceRuntime, err := initWorkspaceRuntime(*cfg)
	if err != nil {
		return err
	}
	authRuntime, err := initAuthRuntime(db.DB(), cfg.Security)
	if err != nil {
		return err
	}

	auditRetention := cfg.Storage.ResolvedAuditRetention()
	taskEventRetention := cfg.Storage.ResolvedTaskEventRetention()
	workspaceOptions := workspaces.CleanupOptions{
		TaskRetention:   cfg.Workspaces.ResolvedTaskRetention(),
		BackupRetention: cfg.Workspaces.ResolvedBackupRetention(),
	}

	if dryRun {
		taskDeltaCount, err := taskStore.CountStreamDeltaEvents(ctx)
		if err != nil {
			return err
		}
		taskDeltaBytes, err := taskStore.StreamDeltaEventBytes(ctx)
		if err != nil {
			return err
		}
		taskExpiredCount, err := taskStore.CountExpiredEvents(ctx, now, taskEventRetention)
		if err != nil {
			return err
		}
		auditRunCount, err := auditStore.CountExpiredRuns(ctx, now, auditRetention)
		if err != nil {
			return err
		}
		auditArtifactBytes, err := auditStore.SumExpiredRunArtifactBytes(ctx, now, auditRetention)
		if err != nil {
			return err
		}
		legacyArtifactCount, err := auditStore.CountLegacyRedundantArtifacts(ctx)
		if err != nil {
			return err
		}
		attachmentCount, err := attachmentStore.CountExpiredAttachments(ctx, now)
		if err != nil {
			return err
		}
		attachmentBytes, err := attachmentStore.SumExpiredAttachmentBytes(ctx, now)
		if err != nil {
			return err
		}
		orphanReport, err := attachmentStore.ScanOrphans(ctx)
		if err != nil {
			return err
		}
		sessionCount, err := authRuntime.AuthLogic.CountExpiredSessions(ctx, now)
		if err != nil {
			return err
		}
		workspaceReport, err := workspaceRuntime.Manager.CountExpired(ctx, now, workspaceOptions)
		if err != nil {
			return err
		}
		fmt.Printf("DRY RUN maintenance\n")
		fmt.Printf("  task stream delta events: %d (%d bytes)\n", taskDeltaCount, taskDeltaBytes)
		fmt.Printf("  expired task events: %d\n", taskExpiredCount)
		fmt.Printf("  legacy redundant audit artifacts: %d\n", legacyArtifactCount)
		fmt.Printf("  expired audit runs: %d\n", auditRunCount)
		fmt.Printf("  expired audit artifact bytes: %d\n", auditArtifactBytes)
		fmt.Printf("  expired attachments: %d (%d bytes)\n", attachmentCount, attachmentBytes)
		fmt.Printf("  orphan attachments: %d records, %d files\n", len(orphanReport.Attachments), len(orphanReport.Files))
		fmt.Printf("  expired sessions: %d\n", sessionCount)
		fmt.Printf("  expired task workspaces: %d\n", workspaceReport.DeletedTaskWorkspaces)
		fmt.Printf("  expired backups: %d\n", workspaceReport.DeletedBackups)
		return nil
	}

	if !apply {
		return fmt.Errorf("storage maintenance requires --storage-maintenance-apply or --storage-maintenance-vacuum")
	}

	backupPath, err := backupDatabaseBeforeMaintenance(cfg)
	if err != nil {
		return fmt.Errorf("backup database before maintenance: %w", err)
	}
	fmt.Printf("database backup: %s\n", backupPath)

	totalTaskDeltaDeleted := int64(0)
	fmt.Println("cleaning task stream delta events")
	for {
		deleted, err := taskStore.DeleteStreamDeltaEvents(ctx, 500)
		if err != nil {
			return err
		}
		totalTaskDeltaDeleted += deleted
		fmt.Printf("  task delta batch deleted: %d\n", deleted)
		if deleted == 0 {
			break
		}
	}
	totalTaskEventDeleted := int64(0)
	fmt.Println("cleaning expired task events")
	for {
		deleted, err := taskStore.DeleteExpiredEvents(ctx, now, taskEventRetention, 500)
		if err != nil {
			return err
		}
		totalTaskEventDeleted += deleted
		fmt.Printf("  task event batch deleted: %d\n", deleted)
		if deleted == 0 {
			break
		}
	}
	totalLegacyCompacted := int64(0)
	fmt.Println("compacting legacy audit artifacts")
	for {
		compacted, err := auditStore.CompactLegacyArtifacts(ctx, 500)
		if err != nil {
			return err
		}
		totalLegacyCompacted += compacted
		fmt.Printf("  legacy artifact batch compacted: %d\n", compacted)
		if compacted == 0 {
			break
		}
	}
	totalAuditDeleted := 0
	fmt.Println("cleaning expired audit runs")
	for {
		deleted, err := auditStore.CleanupExpiredRuns(ctx, now, auditRetention, 100)
		if err != nil {
			return err
		}
		totalAuditDeleted += deleted
		fmt.Printf("  audit run batch deleted: %d\n", deleted)
		if deleted == 0 {
			break
		}
	}
	totalAttachmentDeleted := 0
	fmt.Println("cleaning expired attachments")
	for {
		deleted, err := attachmentStore.GCExpired(ctx, now, 100)
		if err != nil {
			return err
		}
		totalAttachmentDeleted += deleted
		fmt.Printf("  attachment batch processed: %d\n", deleted)
		if deleted == 0 {
			break
		}
	}
	fmt.Println("cleaning orphan attachments")
	orphanReport, err := attachmentStore.CleanupOrphans(ctx, false)
	if err != nil {
		return err
	}
	totalSessionDeleted := 0
	fmt.Println("cleaning expired sessions")
	for {
		deleted, err := authRuntime.AuthLogic.CleanupExpiredSessions(ctx, now, 100)
		if err != nil {
			return err
		}
		totalSessionDeleted += deleted
		fmt.Printf("  session batch deleted: %d\n", deleted)
		if deleted == 0 {
			break
		}
	}
	fmt.Println("cleaning expired workspaces and backups")
	workspaceReport, err := workspaceRuntime.Manager.CleanupExpired(ctx, now, workspaceOptions)
	if err != nil {
		return err
	}
	if len(workspaceReport.Errors) > 0 {
		for _, cleanupErr := range workspaceReport.Errors {
			log.Warnf("Workspace maintenance item failed: %v", cleanupErr)
		}
	}

	fmt.Printf("APPLY maintenance\n")
	fmt.Printf("  task stream delta events deleted: %d\n", totalTaskDeltaDeleted)
	fmt.Printf("  expired task events deleted: %d\n", totalTaskEventDeleted)
	fmt.Printf("  legacy redundant audit artifacts compacted: %d\n", totalLegacyCompacted)
	fmt.Printf("  expired audit runs deleted: %d\n", totalAuditDeleted)
	fmt.Printf("  expired attachments processed: %d\n", totalAttachmentDeleted)
	fmt.Printf("  orphan attachments cleaned: %d records, %d files\n", len(orphanReport.Attachments), len(orphanReport.Files))
	fmt.Printf("  expired sessions deleted: %d\n", totalSessionDeleted)
	fmt.Printf("  expired task workspaces deleted: %d\n", workspaceReport.DeletedTaskWorkspaces)
	fmt.Printf("  expired backups deleted: %d\n", workspaceReport.DeletedBackups)

	if vacuum {
		fmt.Println("checkpointing and vacuuming database")
		if err := checkpointDatabase(db.DB()); err != nil {
			return err
		}
		if err := db.DB().Exec("VACUUM").Error; err != nil {
			return err
		}
		fmt.Println("VACUUM complete")
	}
	return nil
}

func backupDatabaseBeforeMaintenance(cfg *config.Config) (string, error) {
	if cfg == nil || strings.TrimSpace(cfg.Sqlite.DbDir) == "" {
		return "", nil
	}
	backupRoot := filepath.Join(cfg.Sqlite.DbDir, "maintenance-backups")
	if err := os.MkdirAll(backupRoot, 0o755); err != nil {
		return "", err
	}
	backupPath := filepath.Join(backupRoot, fmt.Sprintf("app-%s.db", time.Now().UTC().Format("20060102T150405Z")))
	escaped := strings.ReplaceAll(filepath.ToSlash(backupPath), "'", "''")
	if err := db.DB().Exec("VACUUM INTO '" + escaped + "'").Error; err != nil {
		return "", err
	}
	return backupPath, nil
}
