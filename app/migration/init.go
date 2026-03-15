package migration

import (
	"github.com/EquentR/agent_runtime/pkg/log"
	"github.com/EquentR/agent_runtime/pkg/migrate"
)

var versionMigrations = []migrate.Migration{
	to001,
}

func Bootstrap(version string) {
	log.Info("DB migration starting...")
	migrate.AutoMigrate(version, versionMigrations)
}
