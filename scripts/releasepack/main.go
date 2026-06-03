package main

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	sourceDir := flag.String("source", ".", "repository root")
	destDir := flag.String("dest", "", "package output directory")
	flag.Parse()

	if err := PackRelease(*sourceDir, *destDir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func PackRelease(sourceDir string, destDir string) error {
	sourceDir = cleanPath(sourceDir)
	destDir = cleanPath(destDir)
	if sourceDir == "" {
		return fmt.Errorf("source directory is required")
	}
	if destDir == "" {
		return fmt.Errorf("dest directory is required")
	}

	if err := preparePackageDirs(destDir); err != nil {
		return err
	}

	if err := copyFile(
		filepath.Join(sourceDir, "conf", "app.release.yaml"),
		filepath.Join(destDir, "conf", "app.yaml"),
	); err != nil {
		return fmt.Errorf("copy release config: %w", err)
	}
	if err := copyWorkspaceTemplate(filepath.Join(sourceDir, "workspace"), filepath.Join(destDir, "workspace")); err != nil {
		return err
	}
	return nil
}

func preparePackageDirs(destDir string) error {
	if err := os.RemoveAll(filepath.Join(destDir, "conf")); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(destDir, "workspace")); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(destDir, "conf"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(destDir, "workspace"), 0o755); err != nil {
		return err
	}
	return nil
}

func copyWorkspaceTemplate(sourceRoot string, destRoot string) error {
	agentsSource := filepath.Join(sourceRoot, "AGENTS.md")
	if err := copyFile(agentsSource, filepath.Join(destRoot, "AGENTS.md")); err != nil {
		return fmt.Errorf("copy workspace AGENTS.md: %w", err)
	}

	skillsSource := filepath.Join(sourceRoot, "skills")
	skillsDest := filepath.Join(destRoot, "skills")
	info, err := os.Lstat(skillsSource)
	if err != nil {
		if os.IsNotExist(err) {
			return os.MkdirAll(skillsDest, 0o755)
		}
		return fmt.Errorf("copy workspace skills: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("copy workspace skills: symlink paths are not supported: %s", skillsSource)
	}
	if !info.IsDir() {
		return fmt.Errorf("copy workspace skills: %s is not a directory", skillsSource)
	}
	if err := copyTree(skillsSource, skillsDest); err != nil {
		return fmt.Errorf("copy workspace skills: %w", err)
	}
	return nil
}

func copyTree(sourceRoot string, destRoot string) error {
	if err := ensureNoSymlink(sourceRoot); err != nil {
		return err
	}
	if err := ensureNoSymlink(destRoot); err != nil {
		return err
	}
	if err := os.MkdirAll(destRoot, 0o755); err != nil {
		return err
	}
	return filepath.WalkDir(sourceRoot, func(currentPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if currentPath == sourceRoot {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink paths are not supported: %s", currentPath)
		}

		relativePath, err := filepath.Rel(sourceRoot, currentPath)
		if err != nil {
			return err
		}
		destinationPath := filepath.Join(destRoot, relativePath)

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
		return copyFileWithMode(currentPath, destinationPath, info.Mode().Perm())
	})
}

func copyFile(sourcePath string, destinationPath string) error {
	info, err := os.Lstat(sourcePath)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("symlink paths are not supported: %s", sourcePath)
	}
	return copyFileWithMode(sourcePath, destinationPath, info.Mode().Perm())
}

func copyFileWithMode(sourcePath string, destinationPath string, mode fs.FileMode) error {
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

func cleanPath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	return filepath.Clean(trimmed)
}
