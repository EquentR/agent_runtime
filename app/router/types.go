package router

import (
	"bytes"
	"io/fs"
	"net/http"
	"path"
	"strings"

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
	initRouterWithEmbeddedFrontend(e, registers, baseUrl, staticPaths, embeddedFrontendFiles())
}

func initRouterWithEmbeddedFrontend(e *gin.Engine, registers []Register, baseUrl string, staticPaths []rest.Static, embeddedFrontend fs.FS) {
	embeddedFrontend = validatedEmbeddedFrontendFS(embeddedFrontend)
	e.NoRoute(func(c *gin.Context) {
		if embeddedFrontend != nil && !isAPIPath(c.Request.URL.Path, baseUrl) && serveEmbeddedFrontend(c, embeddedFrontend) {
			return
		}
		c.JSON(404, gin.H{
			"msg": "Not Found",
		})
	})

	rg := e.Group(baseUrl)
	RegisterAPI(rg, registers)

	if embeddedFrontend != nil {
		return
	}

	for _, static := range staticPaths {
		if static.Dir != "" && static.Path != "" {
			e.Use(gStatic.Serve(static.Path, gStatic.LocalFile(static.Dir, true)))
			log.Infof("Registered static path: Dir[%s] -> Server[%s]", static.Dir, static.Path)
		}
	}
}

func validatedEmbeddedFrontendFS(frontend fs.FS) fs.FS {
	if frontend == nil {
		return nil
	}
	info, err := fs.Stat(frontend, "index.html")
	if err != nil || info.IsDir() {
		return nil
	}
	return frontend
}

func isAPIPath(requestPath string, baseUrl string) bool {
	baseUrl = strings.TrimSpace(baseUrl)
	if baseUrl == "" || baseUrl == "/" {
		return false
	}
	if !strings.HasPrefix(baseUrl, "/") {
		baseUrl = "/" + baseUrl
	}
	baseUrl = strings.TrimRight(baseUrl, "/")
	return requestPath == baseUrl || strings.HasPrefix(requestPath, baseUrl+"/")
}

func serveEmbeddedFrontend(c *gin.Context, frontend fs.FS) bool {
	fileName := cleanFrontendRequestPath(c.Request.URL.Path)
	if serveEmbeddedFile(c, frontend, fileName) {
		return true
	}
	if fileName != "index.html" {
		return serveEmbeddedFile(c, frontend, "index.html")
	}
	return false
}

func cleanFrontendRequestPath(requestPath string) string {
	fileName := strings.TrimPrefix(path.Clean("/"+strings.TrimPrefix(requestPath, "/")), "/")
	if fileName == "." || fileName == "" {
		return "index.html"
	}
	return fileName
}

func serveEmbeddedFile(c *gin.Context, frontend fs.FS, fileName string) bool {
	info, err := fs.Stat(frontend, fileName)
	if err != nil || info.IsDir() {
		return false
	}
	data, err := fs.ReadFile(frontend, fileName)
	if err != nil {
		return false
	}
	http.ServeContent(c.Writer, c.Request, path.Base(fileName), info.ModTime(), bytes.NewReader(data))
	c.Abort()
	return true
}
