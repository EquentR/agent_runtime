//go:build windows

package updater

import (
	"errors"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsStopAccessCanQueryAndTerminate(t *testing.T) {
	if windowsStopAccess&windows.PROCESS_QUERY_LIMITED_INFORMATION == 0 || windowsStopAccess&windows.PROCESS_TERMINATE == 0 {
		t.Fatalf("windowsStopAccess = %#x, want query and terminate rights", windowsStopAccess)
	}
}

func TestWindowsProcessGoneOnlyRecognizesMissingProcess(t *testing.T) {
	if !windowsProcessGone(windows.ERROR_INVALID_PARAMETER) {
		t.Fatal("ERROR_INVALID_PARAMETER should identify a missing process")
	}
	if windowsProcessGone(windows.ERROR_ACCESS_DENIED) || windowsProcessGone(errors.New("transient")) {
		t.Fatal("access and transient errors must not be treated as process exit")
	}
}
