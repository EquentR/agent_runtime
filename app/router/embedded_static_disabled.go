//go:build !embed_web

package router

import "io/fs"

func embeddedFrontendFiles() fs.FS {
	return nil
}
