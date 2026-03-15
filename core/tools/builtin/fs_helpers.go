package builtin

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func (e runtimeEnv) resolveWorkspacePath(raw string) (string, string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", "", fmt.Errorf("path is required")
	}

	clean := filepath.Clean(filepath.FromSlash(trimmed))
	if filepath.IsAbs(clean) {
		return "", "", fmt.Errorf("absolute paths are not allowed: %s", raw)
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", "", fmt.Errorf("path escapes workspace: %s", raw)
	}

	abs := filepath.Join(e.workspaceRoot, clean)
	abs = filepath.Clean(abs)
	rel, err := filepath.Rel(e.workspaceRoot, abs)
	if err != nil {
		return "", "", fmt.Errorf("resolve path: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", "", fmt.Errorf("path escapes workspace: %s", raw)
	}
	if err := ensureNoSymlink(abs); err != nil {
		return "", "", err
	}
	return abs, filepath.ToSlash(rel), nil
}

func (e runtimeEnv) resolveWorkspaceFile(raw string, mustExist bool) (string, string, error) {
	abs, rel, err := e.resolveWorkspacePath(raw)
	if err != nil {
		return "", "", err
	}

	info, err := os.Stat(abs)
	if err != nil {
		if mustExist || !errors.Is(err, os.ErrNotExist) {
			return "", "", err
		}
		return abs, rel, nil
	}
	if info.IsDir() {
		return "", "", fmt.Errorf("path is a directory: %s", rel)
	}
	return abs, rel, nil
}

func (e runtimeEnv) resolveWorkspaceDir(raw string, mustExist bool) (string, string, error) {
	abs, rel, err := e.resolveWorkspacePath(raw)
	if err != nil {
		return "", "", err
	}

	info, err := os.Stat(abs)
	if err != nil {
		if mustExist || !errors.Is(err, os.ErrNotExist) {
			return "", "", err
		}
		return abs, rel, nil
	}
	if !info.IsDir() {
		return "", "", fmt.Errorf("path is not a directory: %s", rel)
	}
	return abs, rel, nil
}

func ensureNoSymlink(absPath string) error {
	clean := filepath.Clean(absPath)
	volume := filepath.VolumeName(clean)
	rest := strings.TrimPrefix(clean, volume)
	current := volume
	if strings.HasPrefix(rest, string(os.PathSeparator)) {
		current += string(os.PathSeparator)
		rest = strings.TrimPrefix(rest, string(os.PathSeparator))
	}

	parts := strings.Split(rest, string(os.PathSeparator))
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		if current == "" || current == string(os.PathSeparator) || current == volume+string(os.PathSeparator) {
			current = filepath.Join(current, part)
		} else {
			current = filepath.Join(current, part)
		}

		info, err := os.Lstat(current)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink paths are not supported: %s", clean)
		}
	}

	return nil
}

func ensureParentDir(path string, createDirs bool) error {
	parent := filepath.Dir(path)
	if createDirs {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return err
		}
		return ensureNoSymlink(parent)
	}
	info, err := os.Stat(parent)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("parent path is not a directory: %s", parent)
	}
	return nil
}

func copyFileContents(source string, destination string) error {
	src, err := os.Open(source)
	if err != nil {
		return err
	}
	defer src.Close()

	info, err := src.Stat()
	if err != nil {
		return err
	}

	dst, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return err
	}
	return dst.Close()
}

func samePath(left string, right string) bool {
	return filepath.Clean(left) == filepath.Clean(right)
}

func isCrossDeviceError(err error) bool {
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		return errors.Is(linkErr.Err, syscall.EXDEV)
	}
	return false
}
