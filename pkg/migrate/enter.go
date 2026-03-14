package migrate

import (
	"sort"

	"github.com/EquentR/agent_runtime/pkg/db"
	"github.com/EquentR/agent_runtime/pkg/log"
	"github.com/hashicorp/go-version"
	"go.uber.org/zap"
)

func AutoMigrate(currentVersion string, vm []Migration) {
	err := db.DB().AutoMigrate(&DataVersion{})
	if err != nil {
		log.Fatal("data version auto migrate failed", zap.Error(err))
	}

	savedVersion, err := GetDataVersion()
	if err != nil {
		log.Fatal("get data version failed", zap.Error(err))
	}

	//数据版本比应用版本还大
	if compareVersions(currentVersion, savedVersion) < 0 {
		log.Warn("the current version is smaller than the data version")
	}

	sort.Slice(vm, func(i, j int) bool {
		return compareVersions(vm[i].Version, vm[j].Version) < 0
	})

	for _, migration := range vm {
		if compareVersions(migration.Version, currentVersion) > 0 {
			//数据版本比当前版本还大，说明应用版本相对于数据版本回退了
			log.Warn("the migration version is greater than the application version")
		}
		//如果当前数据版本小于迁移版本，说明需要通过该过程升级数据
		if compareVersions(migration.Version, savedVersion) > 0 {
			migration.Fun()
			savedVersion = migration.Version
			SetDataVersion(db.DB(), savedVersion)
		}
	}

	if compareVersions(savedVersion, currentVersion) < 0 {
		//当前版本不是存储的数据版本，但是也不需要特殊数据升级的时候
		SetDataVersion(db.DB(), currentVersion)
	}
}

// compareVersions compares two version strings using go-version package
// Returns: -1 if v1 < v2, 0 if v1 == v2, 1 if v1 > v2
func compareVersions(v1, v2 string) int {
	ver1, err := version.NewVersion(v1)
	if err != nil {
		log.Fatal("parse version failed", zap.String("version", v1), zap.Error(err))
	}
	ver2, err := version.NewVersion(v2)
	if err != nil {
		log.Fatal("parse version failed", zap.String("version", v2), zap.Error(err))
	}
	return ver1.Compare(ver2)
}
