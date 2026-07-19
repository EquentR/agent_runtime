package updater

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
)

type ExtractLimits struct {
	MaxFiles      int
	MaxFileSize   int64
	MaxTotalSize  int64
	MaxPathLength int
}

func DefaultExtractLimits() ExtractLimits {
	return ExtractLimits{MaxFiles: 10_000, MaxFileSize: 1 << 30, MaxTotalSize: 2 << 30, MaxPathLength: 4096}
}

func ExtractArchive(archivePath, destination string, limits ExtractLimits) error {
	limits = normalizeExtractLimits(limits)
	if err := ensureDirectory(destination); err != nil {
		return err
	}
	lower := strings.ToLower(archivePath)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return extractZip(archivePath, destination, limits)
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return extractTarGzip(archivePath, destination, limits)
	default:
		return fmt.Errorf("unsupported archive format: %s", archivePath)
	}
}

func extractZip(archivePath, destination string, limits ExtractLimits) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer reader.Close()
	seen := make(map[string]struct{}, len(reader.File))
	var total int64
	for _, entry := range reader.File {
		path, err := safeArchivePath(destination, entry.Name, limits.MaxPathLength)
		if err != nil {
			return err
		}
		if _, exists := seen[path]; exists {
			return fmt.Errorf("duplicate archive path: %s", entry.Name)
		}
		seen[path] = struct{}{}
		if len(seen) > limits.MaxFiles {
			return fmt.Errorf("archive exceeds file count limit")
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err
			}
			continue
		}
		if !entry.FileInfo().Mode().IsRegular() {
			return fmt.Errorf("archive entry is not a regular file or directory: %s", entry.Name)
		}
		if entry.UncompressedSize64 > math.MaxInt64 || int64(entry.UncompressedSize64) > limits.MaxFileSize || total+int64(entry.UncompressedSize64) > limits.MaxTotalSize {
			return fmt.Errorf("archive exceeds extraction limits")
		}
		file, err := entry.Open()
		if err != nil {
			return err
		}
		if err := writeExtractedFile(path, file, int64(entry.UncompressedSize64), entry.Mode().Perm(), limits); err != nil {
			_ = file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		total += int64(entry.UncompressedSize64)
	}
	return nil
}

func extractTarGzip(archivePath, destination string, limits ExtractLimits) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	seen := make(map[string]struct{})
	var total int64
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		path, err := safeArchivePath(destination, header.Name, limits.MaxPathLength)
		if err != nil {
			return err
		}
		if _, exists := seen[path]; exists {
			return fmt.Errorf("duplicate archive path: %s", header.Name)
		}
		seen[path] = struct{}{}
		if len(seen) > limits.MaxFiles {
			return fmt.Errorf("archive exceeds file count limit")
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || header.Size > limits.MaxFileSize || total+header.Size > limits.MaxTotalSize {
				return fmt.Errorf("archive exceeds extraction limits")
			}
			if err := writeExtractedFile(path, reader, header.Size, header.FileInfo().Mode().Perm(), limits); err != nil {
				return err
			}
			total += header.Size
		default:
			return fmt.Errorf("archive entry type is not supported: %s", header.Name)
		}
	}
}

func writeExtractedFile(path string, source io.Reader, size int64, mode os.FileMode, limits ExtractLimits) error {
	if err := ensureDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".extract-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if _, err := io.CopyN(temp, io.LimitReader(source, size+1), size); err != nil {
		_ = temp.Close()
		return err
	}
	if extra, err := io.Copy(io.Discard, source); err == nil && extra > 0 {
		_ = temp.Close()
		return fmt.Errorf("archive entry contains more data than declared")
	}
	if mode.Perm() == 0 {
		mode = 0o644
	}
	if err := temp.Chmod(mode.Perm() & 0o777); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := ensureNoSymlink(path); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	return nil
}

func safeArchivePath(destination, name string, maxLength int) (string, error) {
	if name == "" || len(name) > maxLength || filepath.IsAbs(name) || filepath.VolumeName(name) != "" {
		return "", fmt.Errorf("unsafe archive path: %q", name)
	}
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe archive path: %q", name)
	}
	path := filepath.Join(destination, clean)
	relative, err := filepath.Rel(destination, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe archive path: %q", name)
	}
	return path, nil
}

func normalizeExtractLimits(limits ExtractLimits) ExtractLimits {
	defaults := DefaultExtractLimits()
	if limits.MaxFiles <= 0 {
		limits.MaxFiles = defaults.MaxFiles
	}
	if limits.MaxFileSize <= 0 {
		limits.MaxFileSize = defaults.MaxFileSize
	}
	if limits.MaxTotalSize <= 0 {
		limits.MaxTotalSize = defaults.MaxTotalSize
	}
	if limits.MaxPathLength <= 0 {
		limits.MaxPathLength = defaults.MaxPathLength
	}
	return limits
}

func ensureDirectory(path string) error {
	if err := ensureNoSymlink(path); err != nil {
		return err
	}
	return os.MkdirAll(path, 0o755)
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
	for _, part := range strings.Split(rest, string(os.PathSeparator)) {
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
			return fmt.Errorf("symlink path is not allowed: %s", clean)
		}
	}
	return nil
}
