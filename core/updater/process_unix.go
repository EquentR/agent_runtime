//go:build !windows

package updater

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

func (r *DirectRuntime) WaitForExit(ctx context.Context, pid int, expectedExecutable string) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}
	if err := verifyUnixProcessExecutable(pid, expectedExecutable); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for {
		if err := process.Signal(syscall.Signal(0)); err != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
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
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := command.Start(); err != nil {
		return 0, err
	}
	pid := command.Process.Pid
	_ = command.Process.Release()
	return pid, nil
}

func (r *DirectRuntime) Stop(ctx context.Context, pid int, expectedExecutable string) error {
	if err := verifyUnixProcessExecutable(pid, expectedExecutable); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return err
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		if err := process.Signal(syscall.Signal(0)); err != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			if err := process.Kill(); err != nil {
				return fmt.Errorf("kill process %d: %w", pid, err)
			}
			return nil
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func isProcessGoneError(err error) bool {
	return errors.Is(err, ErrProcessGone) || os.IsNotExist(err)
}

func verifyUnixProcessExecutable(pid int, expected string) error {
	actual, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return err
	}
	expected, err = filepath.Abs(expected)
	if err != nil {
		return err
	}
	if !samePath(actual, expected) {
		return fmt.Errorf("process executable mismatch: got %s, want %s", actual, expected)
	}
	return nil
}
