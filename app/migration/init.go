package migration

import (
	"github.com/EquentR/agent_runtime/pkg/log"
	"github.com/EquentR/agent_runtime/pkg/migrate"
)

// versionMigrations 按版本顺序汇总应用级数据库迁移。
var versionMigrations = []migrate.Migration{
	to001,
	to002,
	to003,
	to004,
	to005,
	to006,
	to007,
	to008,
	to009,
	to010,
	to011,
	to012,
	to013,
}

// Bootstrap 在应用启动时执行数据库迁移。
func Bootstrap(version string) {
	log.Info("DB migration starting...")
	migrate.AutoMigrate(version, versionMigrations)
}
