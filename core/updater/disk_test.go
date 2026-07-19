package updater

import "testing"

func TestAvailableDiskBytesReturnsCapacityForExistingPath(t *testing.T) {
	available, err := AvailableDiskBytes(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if available == 0 {
		t.Fatal("AvailableDiskBytes() = 0")
	}
}
