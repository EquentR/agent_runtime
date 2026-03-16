package migration

import (
	"github.com/EquentR/agent_runtime/core/memory"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	"github.com/EquentR/agent_runtime/pkg/migrate"
	"gorm.io/gorm"
)

// to001 初始化迁移，创建数据版本表
var to001 = migrate.NewMigration("0.0.1", func(tx *gorm.DB) error {
	err := tx.AutoMigrate(&migrate.DataVersion{})
	if err != nil {
		return err
	}
	return nil
})

// to002 创建任务快照表与事件流表，为后台任务管理器提供持久化存储。
var to002 = migrate.NewMigration("0.0.2", func(tx *gorm.DB) error {
	return tx.AutoMigrate(&coretasks.Task{}, &coretasks.TaskEvent{})
})

// to003 创建长期记忆表，按 user_id 隔离一条用户摘要记录。
var to003 = migrate.NewMigration("0.0.3", func(tx *gorm.DB) error {
	return tx.AutoMigrate(&memory.LongTermMemory{})
})
