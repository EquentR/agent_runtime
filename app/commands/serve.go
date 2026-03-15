package commands

import (
	"fmt"
	"net"

	"github.com/EquentR/agent_runtime/app/config"
	"github.com/EquentR/agent_runtime/app/migration"
	"github.com/EquentR/agent_runtime/app/router"
	"github.com/EquentR/agent_runtime/pkg/db"
	"github.com/EquentR/agent_runtime/pkg/log"
	"github.com/EquentR/agent_runtime/pkg/rest"
)

func Serve(c *config.Config, version, commit string) {
	GracefulExit()
	// 初始化日志
	log.Init(&c.Log)

	// 打印版本信息
	log.Infof("Application: Version: %s, Git Commit: %s", version, commit)

	// 初始化数据库
	db.Init(&c.Sqlite)
	// 迁移表结构
	migration.Bootstrap(version)

	// 初始化web服务器
	engine := rest.Init()

	router.Init(engine, c.Server.ApiBasePath, c.Server.StaticPaths)

	addr := fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Panicf("Failed to listen on %s: %v", addr, err)
	}
	// 启动服务器
	go func() {
		if err := engine.RunListener(ln); err != nil {
			log.Panicf("Failed to run server: %v", err)
		}
	}()
	log.Infof("gin listening on %s", addr)

	// 等待关闭信号
	select {
	case <-globalCtx.Done():
		_ = ln.Close()
		log.Info("Shutting down server...")
	}
}
