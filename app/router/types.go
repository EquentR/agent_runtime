package router

import (
	"github.com/EquentR/agent_runtime/pkg/log"
	"github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/gin-gonic/gin"
	gStatic "github.com/soulteary/gin-static"
)

type Register interface {
	Register(apiGroup *gin.RouterGroup)
}

func RegisterAPI(apiGroup *gin.RouterGroup, registers []Register) {
	for _, register := range registers {
		register.Register(apiGroup)
	}
}

func InitRouter(e *gin.Engine, registers []Register, baseUrl string, staticPaths []rest.Static) {
	// 注册 phase1 路由
	rg := e.Group(baseUrl)
	RegisterAPI(rg, registers)

	// 注册静态资源
	for _, static := range staticPaths {
		if static.Dir != "" && static.Path != "" {
			e.Use(gStatic.Serve(static.Path, gStatic.LocalFile(static.Dir, true)))
			log.Infof("Registered static path: Dir[%s] -> Server[%s]", static.Dir, static.Path)
		}
	}
}
