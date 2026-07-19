package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type ManagedManifest struct {
	Version string            `json:"version"`
	Files   map[string]string `json:"files"`
}

type MergeResult struct {
	Added     []string `json:"added,omitempty"`
	Updated   []string `json:"updated,omitempty"`
	Preserved []string `json:"preserved,omitempty"`
	Conflicts []string `json:"conflicts,omitempty"`
	Removed   []string `json:"removed,omitempty"`
}

func BuildManagedManifest(version, root string, paths []string) (ManagedManifest, error) {
	root, err := canonicalRoot(root)
	if err != nil {
		return ManagedManifest{}, err
	}
	manifest := ManagedManifest{Version: strings.TrimSpace(version), Files: make(map[string]string, len(paths))}
	for _, relative := range paths {
		relative, path, err := safeRelativePath(root, relative)
		if err != nil {
			return ManagedManifest{}, err
		}
		hash, err := fileSHA256(path)
		if err != nil {
			return ManagedManifest{}, fmt.Errorf("hash managed file %s: %w", relative, err)
		}
		manifest.Files[relative] = hash
	}
	return manifest, nil
}

func ApplyManagedFiles(installRoot, stagedRoot string, previous, next ManagedManifest) (MergeResult, error) {
	installRoot, err := canonicalRoot(installRoot)
	if err != nil {
		return MergeResult{}, err
	}
	stagedRoot, err = canonicalRoot(stagedRoot)
	if err != nil {
		return MergeResult{}, err
	}
	if previous.Files == nil {
		previous.Files = map[string]string{}
	}
	if next.Files == nil {
		next.Files = map[string]string{}
	}
	pathsSet := make(map[string]struct{}, len(previous.Files)+len(next.Files))
	for path := range previous.Files {
		pathsSet[path] = struct{}{}
	}
	for path := range next.Files {
		pathsSet[path] = struct{}{}
	}
	paths := make([]string, 0, len(pathsSet))
	for path := range pathsSet {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	result := MergeResult{}
	for _, relative := range paths {
		relative, installedPath, err := safeRelativePath(installRoot, relative)
		if err != nil {
			return MergeResult{}, err
		}
		previousHash, wasManaged := previous.Files[relative]
		nextHash, remainsManaged := next.Files[relative]
		installedHash, installedExists, err := existingRegularFileHash(installedPath)
		if err != nil {
			return MergeResult{}, err
		}

		if !remainsManaged {
			if installedExists && wasManaged && strings.EqualFold(installedHash, previousHash) {
				if err := ensureNoSymlink(filepath.Dir(installedPath)); err != nil {
					return MergeResult{}, err
				}
				if err := os.Remove(installedPath); err != nil {
					return MergeResult{}, err
				}
				result.Removed = append(result.Removed, relative)
			} else if installedExists {
				result.Preserved = append(result.Preserved, relative)
			}
			continue
		}

		_, stagedPath, err := safeRelativePath(stagedRoot, relative)
		if err != nil {
			return MergeResult{}, err
		}
		stagedHash, err := fileSHA256(stagedPath)
		if err != nil {
			return MergeResult{}, err
		}
		if !strings.EqualFold(stagedHash, nextHash) {
			return MergeResult{}, fmt.Errorf("staged managed file hash mismatch: %s", relative)
		}
		if !installedExists {
			if err := copyRegularFileAtomic(stagedPath, installedPath); err != nil {
				return MergeResult{}, err
			}
			result.Added = append(result.Added, relative)
			continue
		}
		if wasManaged && strings.EqualFold(installedHash, previousHash) {
			if err := copyRegularFileAtomic(stagedPath, installedPath); err != nil {
				return MergeResult{}, err
			}
			result.Updated = append(result.Updated, relative)
			continue
		}
		if err := copyRegularFileAtomic(stagedPath, installedPath+".new"); err != nil {
			return MergeResult{}, err
		}
		result.Preserved = append(result.Preserved, relative)
		result.Conflicts = append(result.Conflicts, relative)
	}
	return result, nil
}

func safeRelativePath(root, relative string) (string, string, error) {
	relative = filepath.ToSlash(filepath.Clean(filepath.FromSlash(strings.TrimSpace(relative))))
	if relative == "" || relative == "." || relative == ".." || strings.HasPrefix(relative, "../") || filepath.IsAbs(relative) || filepath.VolumeName(relative) != "" {
		return "", "", fmt.Errorf("unsafe relative path %q", relative)
	}
	path := filepath.Join(root, filepath.FromSlash(relative))
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", "", fmt.Errorf("path escapes root: %q", relative)
	}
	if err := ensureNoSymlink(filepath.Dir(path)); err != nil {
		return "", "", err
	}
	return relative, path, nil
}

func existingRegularFileHash(path string) (string, bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if !info.Mode().IsRegular() {
		return "", false, fmt.Errorf("managed path is not a regular file: %s", path)
	}
	hash, err := fileSHA256(path)
	return hash, true, err
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func copyRegularFileAtomic(sourcePath, destinationPath string) error {
	info, err := os.Lstat(sourcePath)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("source is not a regular file: %s", sourcePath)
	}
	if err := ensureNoSymlink(filepath.Dir(destinationPath)); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destinationPath), 0o755); err != nil {
		return err
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	temp, err := os.CreateTemp(filepath.Dir(destinationPath), ".update-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if _, err := io.Copy(temp, source); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Chmod(info.Mode().Perm()); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := ensureNoSymlink(destinationPath); err != nil {
		return err
	}
	return replaceFilePath(tempPath, destinationPath)
}
