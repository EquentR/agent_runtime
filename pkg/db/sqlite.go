package db

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/EquentR/agent_runtime/pkg/log"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

var (
	globalDB *gorm.DB
	once     sync.Once
)

func DB() *gorm.DB {
	if globalDB == nil {
		log.Panicf("database not initialized")
	}
	return globalDB
}

func Init(cfg *Database) {
	once.Do(func() {
		db, err := InitSqlite(cfg)
		if err != nil {
			log.Panicf("failed to connect database: %v", err)
		}
		globalDB = db
	})
}

func InitSqlite(cfg *Database) (*gorm.DB, error) {
	var db *gorm.DB
	var err error
	var dbFile string
	log.Infof("Connecting to database [%s]", cfg.Name)
	// 1、创建文件夹
	if !filepath.IsAbs(cfg.DbDir) {
		wd, err := os.Getwd()
		if err != nil {
			return nil, err
		}

		cfg.DbDir = filepath.Join(wd, cfg.DbDir)
	}
	err = os.MkdirAll(cfg.DbDir, os.ModePerm)
	if err != nil {
		return nil, err
	}
	dbFile = filepath.Join(cfg.DbDir, cfg.Name+".db")
	// 2、创建数据源
	params := []string{fmt.Sprintf("file:%s?", dbFile)}
	params = append(params, cfg.Params...)

	// 3、内存模式
	if cfg.InMemory {
		params = append(params, "mode=memory")
	}

	dsn := strings.Join(params, "&")

	db, err = gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: log.NewGormLogger(log.Log(), cfg.LogLevel),
	})
	if err != nil {
		return nil, err
	}
	log.Infof("Successful connected to database [%s]", cfg.Name)

	return db, nil
}
