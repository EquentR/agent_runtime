//go:build !windows

package updater

import "os"

func replaceFilePath(source, destination string) error {
	return os.Rename(source, destination)
}
