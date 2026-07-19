//go:build !windows && !linux

package updater

import "fmt"

func AvailableDiskBytes(string) (uint64, error) {
	return 0, fmt.Errorf("disk availability is unsupported on this platform")
}
