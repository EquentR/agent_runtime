package router

import (
	"github.com/EquentR/agent_runtime/app/handlers"
	"github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/gin-gonic/gin"
)

var (
	registers = []Register{
		handlers.NewExampleHandler(),
	}
)

func Init(e *gin.Engine, baseUrl string, staticPath []rest.Static) {
	InitRouter(e, registers, baseUrl, staticPath)
}
