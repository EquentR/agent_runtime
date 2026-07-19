//go:build windows

package updater

import (
	"errors"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sys/windows"
)

var ErrOperationLocked = errors.New("updater operation is locked")

type ProcessLock struct {
	file       *os.File
	overlapped windows.Overlapped
	once       sync.Once
	err        error
}

func AcquireProcessLock(path string) (*ProcessLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	lock := &ProcessLock{file: file}
	err = windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &lock.overlapped)
	if err != nil {
		_ = file.Close()
		return nil, errors.Join(ErrOperationLocked, err)
	}
	return lock, nil
}

func (l *ProcessLock) Close() error {
	if l == nil {
		return nil
	}
	l.once.Do(func() {
		if err := windows.UnlockFileEx(windows.Handle(l.file.Fd()), 0, 1, 0, &l.overlapped); err != nil {
			l.err = err
		}
		if err := l.file.Close(); l.err == nil {
			l.err = err
		}
	})
	return l.err
}
