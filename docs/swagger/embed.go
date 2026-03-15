package swagger

import (
	"embed"
	"io/fs"
)

// files 打包 Swagger UI 页面与生成后的 OpenAPI 文档。
//
//go:embed index.html swagger.json swagger.yaml
var files embed.FS

// StaticFS 返回供 HTTP 层读取的只读文件系统。
func StaticFS() fs.FS {
	sub, err := fs.Sub(files, ".")
	if err != nil {
		return files
	}
	return sub
}
