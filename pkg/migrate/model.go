package migrate

import (
	"fmt"

	"github.com/EquentR/agent_runtime/pkg/db"
	"github.com/EquentR/agent_runtime/pkg/log"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type DataVersion struct {
	ID      int    `json:"id" gorm:"type:integer;not null;primaryKey;comment:ID"`
	Version string `json:"version" gorm:"type:varchar(32);not null;comment:ID"`
}

type Migration struct {
	Version string
	Fun     func()
}

func migrationInTransaction(v string, fun func(tx *gorm.DB) error) {
	err := db.DB().Transaction(func(tx *gorm.DB) error {
		SetDataVersion(tx, v)
		return fun(tx)
	})
	if err != nil {
		log.Fatal(fmt.Sprintf("%s auto migration failed", v), zap.Error(err))
	}
}

func NewMigration(v string, fun func(tx *gorm.DB) error) Migration {
	return Migration{
		Version: v,
		Fun: func() {
			migrationInTransaction(v, fun)
		},
	}
}
