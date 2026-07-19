//go:build windows

package updater

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

const windowsStopAccess = windows.PROCESS_TERMINATE | windows.SYNCHRONIZE | windows.PROCESS_QUERY_LIMITED_INFORMATION

func (r *DirectRuntime) WaitForExit(ctx context.Context, pid int, expectedExecutable string) error {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		if windowsProcessGone(err) {
			return nil
		}
		return fmt.Errorf("open process %d: %w", pid, err)
	}
	defer windows.CloseHandle(handle)
	if err := verifyWindowsProcessExecutable(handle, expectedExecutable); err != nil {
		return err
	}
	for {
		result, err := windows.WaitForSingleObject(handle, 200)
		if err != nil {
			return err
		}
		if result == windows.WAIT_OBJECT_0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
}

func (r *DirectRuntime) Start(ctx context.Context, executable string, args []string, workingDirectory string, environment []string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	command := exec.Command(executable, args...)
	command.Dir = workingDirectory
	command.Env = environment
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS, HideWindow: true}
	if err := command.Start(); err != nil {
		return 0, err
	}
	pid := command.Process.Pid
	_ = command.Process.Release()
	return pid, nil
}

func (r *DirectRuntime) Stop(ctx context.Context, pid int, expectedExecutable string) error {
	if pid <= 0 {
		return fmt.Errorf("invalid process ID")
	}
	handle, err := windows.OpenProcess(windowsStopAccess, false, uint32(pid))
	if err != nil {
		if windowsProcessGone(err) {
			return nil
		}
		return err
	}
	defer windows.CloseHandle(handle)
	if err := verifyWindowsProcessExecutable(handle, expectedExecutable); err != nil {
		return err
	}
	if err := windows.TerminateProcess(handle, 1); err != nil {
		return err
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		result, err := windows.WaitForSingleObject(handle, 200)
		if err != nil {
			return err
		}
		if result == windows.WAIT_OBJECT_0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("process %d did not exit", pid)
		default:
		}
	}
}

func windowsProcessGone(err error) bool {
	return errors.Is(err, windows.ERROR_INVALID_PARAMETER)
}

func isProcessGoneError(err error) bool {
	return errors.Is(err, ErrProcessGone) || windowsProcessGone(err)
}

func verifyWindowsProcessExecutable(handle windows.Handle, expected string) error {
	buffer := make([]uint16, windows.MAX_PATH*4)
	size := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(handle, 0, &buffer[0], &size); err != nil {
		return err
	}
	actual := filepath.Clean(windows.UTF16ToString(buffer[:size]))
	expected, err := filepath.Abs(expected)
	if err != nil {
		return err
	}
	if !samePath(actual, expected) {
		return fmt.Errorf("process executable mismatch: got %s, want %s", actual, expected)
	}
	return nil
}
