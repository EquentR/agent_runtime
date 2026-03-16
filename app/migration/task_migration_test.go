package migration

import (
	"testing"

	"github.com/EquentR/agent_runtime/core/memory"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	"github.com/EquentR/agent_runtime/pkg/db"
	"github.com/EquentR/agent_runtime/pkg/log"
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
}
