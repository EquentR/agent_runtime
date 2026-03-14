package migrate

import (
	"github.com/EquentR/agent_runtime/pkg/db"
	"github.com/EquentR/agent_runtime/pkg/log"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func GetDataVersion() (string, error) {
	version := &DataVersion{
		ID:      1,
		Version: "0.0.0",
	}
	err := db.DB().FirstOrCreate(version).Error
	if err != nil {
		return "", err
	}
	return version.Version, nil
}

// SetDataVersion 设置数据版本，所有迭代过程一定要在事务中进行，并且该步骤一定要最先被调用
func SetDataVersion(tx *gorm.DB, version string) {
	if tx == nil {
		log.Fatal("数据库以及事务不能为空")
		return
	}
	err := tx.Model(&DataVersion{}).Where("id = ?", 1).
		Update("version", version).Error
	if err != nil {
		log.Fatal("设置数据版本失败", zap.Error(err))
	}
}
