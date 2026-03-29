package migration

import (
	"database/sql"
	"sort"
	"strings"
	"testing"

	"github.com/EquentR/agent_runtime/app/models"
	"github.com/EquentR/agent_runtime/core/agent"
	"github.com/EquentR/agent_runtime/core/approvals"
	"github.com/EquentR/agent_runtime/core/memory"
	"github.com/EquentR/agent_runtime/core/prompt"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	"github.com/EquentR/agent_runtime/pkg/db"
	"github.com/EquentR/agent_runtime/pkg/log"
	"github.com/EquentR/agent_runtime/pkg/migrate"
)

// TestBootstrapMigratesTaskTables 验证迁移启动后会创建任务相关表。
func TestBootstrapMigratesTaskTables(t *testing.T) {
	log.Init(&log.Config{Level: "error"})

	db.Init(&db.Database{
		Name:     "task_migration_test",
		DbDir:    t.TempDir(),
		InMemory: true,
		LogLevel: "silent",
	})

	Bootstrap("0.0.2")

	if !db.DB().Migrator().HasTable(&coretasks.Task{}) {
		t.Fatal("tasks table was not created")
	}
	if !db.DB().Migrator().HasTable(&coretasks.TaskEvent{}) {
		t.Fatal("task_events table was not created")
	}
	if !db.DB().Migrator().HasTable(&memory.LongTermMemory{}) {
		t.Fatal("long_term_memories table was not created")
	}
	if !db.DB().Migrator().HasTable(&agent.Conversation{}) {
		t.Fatal("conversations table was not created")
	}
	if !db.DB().Migrator().HasTable(&agent.ConversationMessage{}) {
		t.Fatal("conversation_messages table was not created")
	}
}

func TestBootstrapBackfillsAdminRoleForExistingUsers(t *testing.T) {
	log.Init(&log.Config{Level: "error"})

	databaseDir := t.TempDir()
	db.Init(&db.Database{
		Name:     "task_migration_role_test",
		DbDir:    databaseDir,
		InMemory: true,
		LogLevel: "silent",
	})

	rawDB := db.DB()
	if err := rawDB.Migrator().DropTable(&models.UserSession{}, &models.User{}, &migrate.DataVersion{}); err != nil {
		t.Fatalf("reset migration tables error = %v", err)
	}
	if err := rawDB.AutoMigrate(&migrate.DataVersion{}); err != nil {
		t.Fatalf("AutoMigrate(data_versions) error = %v", err)
	}
	if err := rawDB.Exec(`CREATE TABLE users (
		id integer primary key autoincrement,
		username varchar(128) not null unique,
		password_hash varchar(255) not null,
		created_at datetime,
		updated_at datetime
	)`).Error; err != nil {
		t.Fatalf("create legacy users table error = %v", err)
	}
	if err := rawDB.Exec(`INSERT INTO users (username, password_hash, created_at, updated_at) VALUES
		('alice', 'hash-1', '2026-03-20 10:00:00', '2026-03-20 10:00:00'),
		('bob', 'hash-2', '2026-03-20 10:05:00', '2026-03-20 10:05:00')`).Error; err != nil {
		t.Fatalf("seed legacy users error = %v", err)
	}
	if err := rawDB.Create(&migrate.DataVersion{ID: 1, Version: "0.0.6"}).Error; err != nil {
		t.Fatalf("seed data version error = %v", err)
	}

	Bootstrap("0.0.7")

	var users []models.User
	if err := rawDB.Order("id asc").Find(&users).Error; err != nil {
		t.Fatalf("load users error = %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("len(users) = %d, want 2", len(users))
	}
	if users[0].Role != models.UserRoleAdmin {
		t.Fatalf("first migrated user role = %q, want %q", users[0].Role, models.UserRoleAdmin)
	}
	if users[1].Role != models.UserRoleUser {
		t.Fatalf("second migrated user role = %q, want %q", users[1].Role, models.UserRoleUser)
	}
}

func TestBootstrapMigratesPromptTables(t *testing.T) {
	log.Init(&log.Config{Level: "error"})

	db.Init(&db.Database{
		Name:     "prompt_migration_test",
		DbDir:    t.TempDir(),
		InMemory: true,
		LogLevel: "silent",
	})
	rawDB := db.DB()
	if err := rawDB.Migrator().DropTable(&prompt.PromptBinding{}, &prompt.PromptDocument{}, &migrate.DataVersion{}); err != nil {
		t.Fatalf("reset prompt migration tables error = %v", err)
	}

	Bootstrap("0.0.8")

	assertTableHasColumns(t, "prompt_documents",
		"id",
		"name",
		"description",
		"content",
		"scope",
		"status",
		"created_by",
		"updated_by",
		"created_at",
		"updated_at",
	)
	assertTableColumnDefault(t, "prompt_documents", "status", "active")
	assertTableHasIndexWithColumns(t, "prompt_documents", "scope")
	assertTableHasIndexWithColumns(t, "prompt_documents", "status")

	assertTableHasColumns(t, "prompt_bindings",
		"id",
		"prompt_id",
		"scene",
		"phase",
		"is_default",
		"priority",
		"provider_id",
		"model_id",
		"status",
		"created_by",
		"updated_by",
		"created_at",
		"updated_at",
	)
	assertTableColumnDefault(t, "prompt_bindings", "is_default", "false", "0")
	assertTableColumnDefault(t, "prompt_bindings", "priority", "0")
	assertTableColumnDefault(t, "prompt_bindings", "status", "active")
	assertTableHasIndexWithColumns(t, "prompt_bindings", "prompt_id")
	assertTableHasIndexWithColumns(t, "prompt_bindings", "scene", "phase")
	assertTableHasForeignKey(t, "prompt_bindings", "prompt_documents", "prompt_id", "id")
}

func TestBootstrapMigratesTaskConcurrencyKeyColumn(t *testing.T) {
	log.Init(&log.Config{Level: "error"})

	db.Init(&db.Database{
		Name:     "task_concurrency_key_migration_test",
		DbDir:    t.TempDir(),
		InMemory: true,
		LogLevel: "silent",
	})

	rawDB := db.DB()
	if err := rawDB.Migrator().DropTable("tasks", &migrate.DataVersion{}); err != nil {
		t.Fatalf("reset task migration tables error = %v", err)
	}
	if err := rawDB.AutoMigrate(&migrate.DataVersion{}); err != nil {
		t.Fatalf("AutoMigrate(data_versions) error = %v", err)
	}
	if err := rawDB.Exec(`CREATE TABLE tasks (
		id varchar(64) primary key,
		task_type varchar(128) not null,
		status varchar(32) not null,
		input_json blob not null,
		config_json blob not null,
		metadata_json blob not null,
		result_json blob,
		error_json blob,
		current_step_key varchar(128),
		current_step_title varchar(255),
		step_seq integer not null default 0,
		execution_mode varchar(32) not null,
		root_task_id varchar(64) not null,
		parent_task_id varchar(64),
		child_index integer not null default 0,
		retry_of_task_id varchar(64),
		waiting_on_task_id varchar(64),
		suspend_reason varchar(255),
		runner_id varchar(128),
		heartbeat_at datetime,
		lease_expires_at datetime,
		cancel_requested_at datetime,
		started_at datetime,
		finished_at datetime,
		created_by varchar(128),
		idempotency_key varchar(128),
		created_at datetime,
		updated_at datetime
	)`).Error; err != nil {
		t.Fatalf("create legacy tasks table error = %v", err)
	}
	if rawDB.Migrator().HasColumn("tasks", "concurrency_key") {
		t.Fatal("legacy tasks table unexpectedly has concurrency_key column")
	}
	if err := rawDB.Create(&migrate.DataVersion{ID: 1, Version: "0.0.8"}).Error; err != nil {
		t.Fatalf("seed data version error = %v", err)
	}

	Bootstrap("0.0.9")

	if !rawDB.Migrator().HasColumn("tasks", "concurrency_key") {
		t.Fatal("tasks.concurrency_key column was not created")
	}
}

func TestTaskMigrationCreatesToolApprovalsTable(t *testing.T) {
	log.Init(&log.Config{Level: "error"})

	db.Init(&db.Database{
		Name:     "tool_approval_migration_test",
		DbDir:    t.TempDir(),
		InMemory: true,
		LogLevel: "silent",
	})

	Bootstrap("0.1.0")

	if !db.DB().Migrator().HasTable(&approvals.ToolApproval{}) {
		t.Fatal("tool_approvals table was not created")
	}
	assertTableHasColumns(t, "tool_approvals",
		"id",
		"task_id",
		"conversation_id",
		"step_index",
		"tool_call_id",
		"tool_name",
		"arguments_summary",
		"risk_level",
		"reason",
		"status",
		"decision_by",
		"decision_reason",
		"decision_at",
		"expires_at",
		"created_at",
		"updated_at",
	)
	assertTableHasUniqueIndexWithColumns(t, "tool_approvals", "task_id", "tool_call_id")
}

func assertTableHasColumns(t *testing.T, table string, columns ...string) {
	t.Helper()

	migrator := db.DB().Migrator()
	if !migrator.HasTable(table) {
		t.Fatalf("%s table was not created", table)
	}

	for _, column := range columns {
		if !migrator.HasColumn(table, column) {
			t.Fatalf("%s.%s column was not created", table, column)
		}
	}
}

type pragmaColumnInfo struct {
	Name         string         `gorm:"column:name"`
	DefaultValue sql.NullString `gorm:"column:dflt_value"`
}

type pragmaIndexEntry struct {
	Name   string `gorm:"column:name"`
	Unique int    `gorm:"column:unique"`
}

type pragmaIndexColumnInfo struct {
	Seqno int    `gorm:"column:seqno"`
	Name  string `gorm:"column:name"`
}

type pragmaForeignKeyInfo struct {
	Table string `gorm:"column:table"`
	From  string `gorm:"column:from"`
	To    string `gorm:"column:to"`
}

func assertTableColumnDefault(t *testing.T, table string, column string, want ...string) {
	t.Helper()

	defaultValue, ok := tableColumnDefault(t, table, column)
	if !ok {
		t.Fatalf("%s.%s column default missing", table, column)
	}

	got := normalizeDefaultValue(defaultValue)
	for _, candidate := range want {
		if got == normalizeDefaultValue(candidate) {
			return
		}
	}

	t.Fatalf("%s.%s default = %q, want one of %v", table, column, defaultValue, want)
}

func tableColumnDefault(t *testing.T, table string, column string) (string, bool) {
	t.Helper()

	var columns []pragmaColumnInfo
	if err := db.DB().Raw("PRAGMA table_info('" + table + "')").Scan(&columns).Error; err != nil {
		t.Fatalf("PRAGMA table_info(%s) error = %v", table, err)
	}
	for _, info := range columns {
		if info.Name == column && info.DefaultValue.Valid {
			return info.DefaultValue.String, true
		}
	}
	return "", false
}

func normalizeDefaultValue(value string) string {
	trimmed := strings.TrimSpace(value)
	trimmed = strings.Trim(trimmed, "'\"")
	return strings.ToLower(trimmed)
}

func assertTableHasIndexWithColumns(t *testing.T, table string, wantColumns ...string) {
	t.Helper()

	for _, index := range tableIndexes(t, table) {
		gotColumns := tableIndexColumns(t, index.Name)
		if sameStrings(gotColumns, wantColumns) {
			return
		}
	}

	t.Fatalf("%s index with columns %v was not created", table, wantColumns)
}

func assertTableHasUniqueIndexWithColumns(t *testing.T, table string, wantColumns ...string) {
	t.Helper()

	for _, index := range tableIndexes(t, table) {
		if index.Unique != 1 {
			continue
		}
		gotColumns := tableIndexColumns(t, index.Name)
		if sameStrings(gotColumns, wantColumns) {
			return
		}
	}

	t.Fatalf("%s unique index with columns %v was not created", table, wantColumns)
}

func tableIndexes(t *testing.T, table string) []pragmaIndexEntry {
	t.Helper()

	var indexes []pragmaIndexEntry
	if err := db.DB().Raw("PRAGMA index_list('" + table + "')").Scan(&indexes).Error; err != nil {
		t.Fatalf("PRAGMA index_list(%s) error = %v", table, err)
	}
	return indexes
}

func tableIndexColumns(t *testing.T, indexName string) []string {
	t.Helper()

	var columns []pragmaIndexColumnInfo
	if err := db.DB().Raw("PRAGMA index_info('" + indexName + "')").Scan(&columns).Error; err != nil {
		t.Fatalf("PRAGMA index_info(%s) error = %v", indexName, err)
	}
	sort.Slice(columns, func(i, j int) bool {
		return columns[i].Seqno < columns[j].Seqno
	})

	result := make([]string, 0, len(columns))
	for _, column := range columns {
		result = append(result, column.Name)
	}
	return result
}

func sameStrings(got []string, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func assertTableHasForeignKey(t *testing.T, table string, referencedTable string, fromColumn string, toColumn string) {
	t.Helper()

	for _, fk := range tableForeignKeys(t, table) {
		if fk.Table == referencedTable && fk.From == fromColumn && fk.To == toColumn {
			return
		}
	}

	t.Fatalf("%s foreign key %s -> %s.%s was not created", table, fromColumn, referencedTable, toColumn)
}

func tableForeignKeys(t *testing.T, table string) []pragmaForeignKeyInfo {
	t.Helper()

	var foreignKeys []pragmaForeignKeyInfo
	if err := db.DB().Raw("PRAGMA foreign_key_list('" + table + "')").Scan(&foreignKeys).Error; err != nil {
		t.Fatalf("PRAGMA foreign_key_list(%s) error = %v", table, err)
	}
	return foreignKeys
}
