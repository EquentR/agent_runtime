package updater

import (
	"fmt"
	"os"
)

type FileOwner struct {
	UID int
	GID int
}

func (o *FileOwner) apply(path string) error {
	if o == nil {
		return nil
	}
	if o.UID < 0 || o.GID < 0 {
		return fmt.Errorf("invalid updater file owner")
	}
	return os.Chown(path, o.UID, o.GID)
}
