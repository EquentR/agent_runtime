package workspaces

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func copyPath(sourcePath string, destinationPath string, filter func(relativePath string, entry fs.DirEntry) bool) error {
	info, err := os.Lstat(sourcePath)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("symlink paths are not supported: %s", sourcePath)
	}
	if info.IsDir() {
		return copyDirectoryContents(sourcePath, destinationPath, filter)
	}
	if filter != nil && filter("", nil) {
		return nil
	}
	return copyFile(sourcePath, destinationPath, info.Mode().Perm())
}

func copyDirectoryContents(sourceRoot string, destinationRoot string, filter func(relativePath string, entry fs.DirEntry) bool) error {
	if err := ensureNoSymlink(sourceRoot); err != nil {
		return err
	}
	if err := ensureNoSymlink(destinationRoot); err != nil {
		return err
	}
	if err := os.MkdirAll(destinationRoot, 0o755); err != nil {
		return err
	}

	return filepath.WalkDir(sourceRoot, func(currentPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if currentPath == sourceRoot {
			return nil
		}

		relativePath, err := filepath.Rel(sourceRoot, currentPath)
		if err != nil {
			return err
		}
		if filter != nil && filter(filepath.ToSlash(relativePath), entry) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink paths are not supported: %s", currentPath)
		}

		destinationPath := filepath.Join(destinationRoot, relativePath)
		if entry.IsDir() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if err := ensureNoSymlink(destinationPath); err != nil {
				return err
			}
			return os.MkdirAll(destinationPath, info.Mode().Perm())
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}
		return copyFile(currentPath, destinationPath, info.Mode().Perm())
	})
}

func copyFile(sourcePath string, destinationPath string, mode fs.FileMode) error {
	destinationDir := filepath.Dir(destinationPath)
	if err := ensureNoSymlink(destinationDir); err != nil {
		return err
	}
	if err := os.MkdirAll(destinationDir, 0o755); err != nil {
		return err
	}

	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()

	temp, err := os.CreateTemp(destinationDir, ".copy-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			_ = os.Remove(tempPath)
		}
	}()

	if _, err := io.Copy(temp, source); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}

	if err := ensureNoSymlink(destinationPath); err != nil {
		return err
	}
	if info, err := os.Lstat(destinationPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink paths are not supported: %s", destinationPath)
		}
		if info.IsDir() {
			return fmt.Errorf("destination path is a directory: %s", destinationPath)
		}
		if err := os.Remove(destinationPath); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	if err := os.Rename(tempPath, destinationPath); err != nil {
		return err
	}
	cleanupTemp = false
	return nil
}

func ensureNoSymlink(path string) error {
	clean := filepath.Clean(path)
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
		current = filepath.Join(current, part)

		info, err := os.Lstat(current)
		if err != nil {
			if os.IsNotExist(err) {
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
