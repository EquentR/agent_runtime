package handlers

import (
	"io/fs"
	"net/http"
	"strings"

	swaggerdocs "github.com/EquentR/agent_runtime/docs/swagger"
	"github.com/gin-gonic/gin"
)

// SwaggerHandler 提供浏览器可访问的 Swagger UI 与原始文档文件。
type SwaggerHandler struct{}

// NewSwaggerHandler 创建 Swagger 文档处理器。
func NewSwaggerHandler() *SwaggerHandler {
	return &SwaggerHandler{}
}

// Register 注册 Swagger UI 页面与原始 JSON/YAML 文档路由。
func (h *SwaggerHandler) Register(rg *gin.RouterGroup) {
	group := rg.Group("swagger")
	group.GET("", h.handleIndexRedirect)
	group.GET("/", h.handleIndexRedirect)
	group.GET("/index.html", h.handleIndexHTML)
	group.GET("/swagger.json", h.handleSwaggerJSON)
	group.GET("/swagger.yaml", h.handleSwaggerYAML)
}

// handleIndexRedirect 将根路径重定向到可直接访问的 Swagger UI 页面。暂时
//
// @Summary Swagger UI 重定向
// @Description 将 `/swagger` 与 `/swagger/` 重定向到可直接访问的 Swagger UI 页面。
// @Tags swagger
// @Produce html
// @Success 302 {string} string "redirect to /swagger/index.html"
// @Router /swagger [get]
func (h *SwaggerHandler) handleIndexRedirect(c *gin.Context) {
	path := strings.TrimSuffix(c.Request.URL.Path, "/")
	c.Redirect(http.StatusFound, path+"/index.html")
}

// handleIndexHTML 输出嵌入式 Swagger UI HTML 页面。
//
// @Summary 获取 Swagger UI 页面
// @Description 返回内嵌的 Swagger UI HTML 页面，便于在浏览器中查看和调试 API。
// @Tags swagger
// @Produce html
// @Success 200 {string} string "Swagger UI HTML"
// @Router /swagger/index.html [get]
func (h *SwaggerHandler) handleIndexHTML(c *gin.Context) {
	serveSwaggerAsset(c, "index.html", "text/html; charset=utf-8")
}

// handleSwaggerJSON 输出生成后的 Swagger JSON 文档。
//
// @Summary 获取 Swagger JSON 文档
// @Description 返回当前服务使用的 Swagger JSON 文档。
// @Tags swagger
// @Produce json
// @Success 200 {string} string "swagger json"
// @Router /swagger/swagger.json [get]
func (h *SwaggerHandler) handleSwaggerJSON(c *gin.Context) {
	serveSwaggerAsset(c, "swagger.json", "application/json; charset=utf-8")
}

// handleSwaggerYAML 输出生成后的 Swagger YAML 文档。
//
// @Summary 获取 Swagger YAML 文档
// @Description 返回当前服务使用的 Swagger YAML 文档。
// @Tags swagger
// @Produce plain
// @Success 200 {string} string "swagger yaml"
// @Router /swagger/swagger.yaml [get]
func (h *SwaggerHandler) handleSwaggerYAML(c *gin.Context) {
	serveSwaggerAsset(c, "swagger.yaml", "application/yaml; charset=utf-8")
}

// serveSwaggerAsset 从嵌入式文件系统读取静态文档资源并直接输出。
func serveSwaggerAsset(c *gin.Context, name string, contentType string) {
	data, err := fs.ReadFile(swaggerdocs.StaticFS(), name)
	if err != nil {
		c.String(http.StatusNotFound, err.Error())
		return
	}
	c.Data(http.StatusOK, contentType, data)
}
