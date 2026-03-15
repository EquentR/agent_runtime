package migration

import (
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
