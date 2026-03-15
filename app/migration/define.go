package migration

import (
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
