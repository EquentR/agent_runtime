package migration

import (
	"testing"

	"github.com/EquentR/agent_runtime/app/models"
	"github.com/EquentR/agent_runtime/core/agent"
	"github.com/EquentR/agent_runtime/core/memory"
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
