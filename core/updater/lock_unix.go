//go:build !windows

package updater

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

var ErrOperationLocked = errors.New("updater operation is locked")

type ProcessLock struct {
	file *os.File
	once sync.Once
	err  error
}

func AcquireProcessLock(path string) (*ProcessLock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, errors.Join(ErrOperationLocked, err)
	}
	return &ProcessLock{file: file}, nil
}

func (l *ProcessLock) Close() error {
	if l == nil {
		return nil
	}
	l.once.Do(func() {
		if err := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN); err != nil {
			l.err = err
		}
		if err := l.file.Close(); l.err == nil {
			l.err = err
		}
	})
	return l.err
}
