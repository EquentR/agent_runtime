package rest

import (
	"github.com/gin-gonic/gin"
)

func Init() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	e := gin.New()

	return e
}
