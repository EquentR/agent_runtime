//go:build embed_web

package router

import (
	"embed"
	"io/fs"
)

//go:embed all:embedded_web
var embeddedFrontend embed.FS

func embeddedFrontendFiles() fs.FS {
	sub, err := fs.Sub(embeddedFrontend, "embedded_web")
	if err != nil {
		return nil
	}
	return validatedEmbeddedFrontendFS(sub)
}
