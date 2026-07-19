package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const backupMarkerContent = "ice-art-update-backup-v1\n"

type BackupMode string

const (
	BackupModeCompact BackupMode = "compact"
	BackupModeFull    BackupMode = "full"
)

type BackupOptions struct {
	InstallRoot string
	BackupRoot  string
	OperationID string
	Mode        BackupMode
	Paths       []string
	CreatedAt   time.Time
}

type BackupManifest struct {
	OperationID string        `json:"operation_id"`
	Mode        BackupMode    `json:"mode"`
	CreatedAt   time.Time     `json:"created_at"`
	Entries     []BackupEntry `json:"entries"`
}

type BackupEntry struct {
	Path    string      `json:"path"`
	Existed bool        `json:"existed"`
	IsDir   bool        `json:"is_dir,omitempty"`
	Mode    os.FileMode `json:"mode,omitempty"`
	SHA256  string      `json:"sha256,omitempty"`
}

func CreateBackup(options BackupOptions) (BackupManifest, error) {
	installRoot, err := canonicalRoot(options.InstallRoot)
	if err != nil {
		return BackupManifest{}, err
	}
	backupRoot, err := canonicalRoot(options.BackupRoot)
	if err != nil {
		return BackupManifest{}, err
	}
	operationID := strings.TrimSpace(options.OperationID)
	if !operationIDPattern.MatchString(operationID) {
		return BackupManifest{}, fmt.Errorf("invalid backup operation ID")
	}
	mode := options.Mode
	if mode != BackupModeCompact && mode != BackupModeFull {
		return BackupManifest{}, fmt.Errorf("invalid backup mode %q", mode)
	}
	createdAt := options.CreatedAt.UTC()
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	destination := filepath.Join(backupRoot, operationID)
	if _, err := os.Lstat(destination); err == nil {
		return BackupManifest{}, fmt.Errorf("backup already exists: %s", operationID)
	} else if !os.IsNotExist(err) {
		return BackupManifest{}, err
	}
	if err := os.MkdirAll(filepath.Join(destination, "files"), 0o700); err != nil {
		return BackupManifest{}, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(destination)
		}
	}()
	if err := os.WriteFile(filepath.Join(destination, ".ice-art-update-backup"), []byte(backupMarkerContent), 0o600); err != nil {
		return BackupManifest{}, err
	}
	manifest := BackupManifest{OperationID: operationID, Mode: mode, CreatedAt: createdAt}
	seen := make(map[string]struct{}, len(options.Paths))
	for _, relative := range options.Paths {
		relative, sourcePath, err := safeRelativePath(installRoot, relative)
		if err != nil {
			return BackupManifest{}, err
		}
		if _, exists := seen[relative]; exists {
			continue
		}
		seen[relative] = struct{}{}
		entry := BackupEntry{Path: relative}
		info, err := os.Lstat(sourcePath)
		if os.IsNotExist(err) {
			manifest.Entries = append(manifest.Entries, entry)
			continue
		}
		if err != nil {
			return BackupManifest{}, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return BackupManifest{}, fmt.Errorf("backup path is a symlink: %s", relative)
		}
		entry.Existed = true
		entry.IsDir = info.IsDir()
		entry.Mode = info.Mode().Perm()
		destinationPath := filepath.Join(destination, "files", filepath.FromSlash(relative))
		if info.IsDir() {
			entry.SHA256, err = treeSHA256(sourcePath)
			if err != nil {
				return BackupManifest{}, err
			}
			if err := copyBackupTree(sourcePath, destinationPath); err != nil {
				return BackupManifest{}, err
			}
		} else if info.Mode().IsRegular() {
			entry.SHA256, err = fileSHA256(sourcePath)
			if err != nil {
				return BackupManifest{}, err
			}
			if err := copyBackupFile(sourcePath, destinationPath, info.Mode().Perm()); err != nil {
				return BackupManifest{}, err
			}
		} else {
			return BackupManifest{}, fmt.Errorf("unsupported backup path type: %s", relative)
		}
		manifest.Entries = append(manifest.Entries, entry)
	}
	if err := writeJSONAtomic(filepath.Join(destination, "manifest.json"), manifest, 0o600); err != nil {
		return BackupManifest{}, err
	}
	cleanup = false
	return manifest, nil
}

func RestoreBackup(installRoot, backupDir string) error {
	installRoot, err := canonicalRoot(installRoot)
	if err != nil {
		return err
	}
	backupDir, err = canonicalRoot(backupDir)
	if err != nil {
		return err
	}
	if err := validateBackupMarker(backupDir); err != nil {
		return err
	}
	raw, err := os.ReadFile(filepath.Join(backupDir, "manifest.json"))
	if err != nil {
		return err
	}
	var manifest BackupManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return fmt.Errorf("decode backup manifest: %w", err)
	}
	if !operationIDPattern.MatchString(manifest.OperationID) || filepath.Base(backupDir) != manifest.OperationID {
		return fmt.Errorf("backup manifest operation ID does not match directory")
	}
	type restoreEntry struct {
		entry       BackupEntry
		source      string
		destination string
	}
	resolved := make([]restoreEntry, 0, len(manifest.Entries))
	filesRoot := filepath.Join(backupDir, "files")
	for _, entry := range manifest.Entries {
		_, destinationPath, err := safeRelativePath(installRoot, entry.Path)
		if err != nil {
			return err
		}
		if info, err := os.Lstat(destinationPath); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("restore destination is a symlink: %s", entry.Path)
		} else if err != nil && !os.IsNotExist(err) {
			return err
		}
		item := restoreEntry{entry: entry, destination: destinationPath}
		if entry.Existed {
			_, sourcePath, err := safeRelativePath(filesRoot, entry.Path)
			if err != nil {
				return err
			}
			info, err := os.Lstat(sourcePath)
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 || info.IsDir() != entry.IsDir || (!entry.IsDir && !info.Mode().IsRegular()) {
				return fmt.Errorf("backup source type mismatch: %s", entry.Path)
			}
			if !entry.IsDir && entry.SHA256 != "" {
				if err := VerifyFileChecksum(sourcePath, entry.SHA256); err != nil {
					return err
				}
			} else if entry.IsDir && entry.SHA256 != "" {
				actual, err := treeSHA256(sourcePath)
				if err != nil || !strings.EqualFold(actual, entry.SHA256) {
					return fmt.Errorf("backup directory hash mismatch: %s", entry.Path)
				}
			}
			item.source = sourcePath
		}
		resolved = append(resolved, item)
	}
	for _, item := range resolved {
		entry := item.entry
		destinationPath := item.destination
		if !entry.Existed {
			if err := os.RemoveAll(destinationPath); err != nil {
				return err
			}
			continue
		}
		if err := os.RemoveAll(destinationPath); err != nil {
			return err
		}
		if entry.IsDir {
			if err := copyBackupTree(item.source, destinationPath); err != nil {
				return err
			}
			continue
		}
		if err := copyBackupFile(item.source, destinationPath, entry.Mode); err != nil {
			return err
		}
		if entry.SHA256 != "" {
			if err := VerifyFileChecksum(destinationPath, entry.SHA256); err != nil {
				return err
			}
		}
	}
	return nil
}

func PruneBackups(backupRoot string, retainCount, retainDays int, now time.Time) ([]string, error) {
	entries, err := os.ReadDir(backupRoot)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	type candidate struct {
		path      string
		createdAt time.Time
	}
	var candidates []candidate
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(backupRoot, entry.Name())
		if validateBackupMarker(path) != nil {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(path, "manifest.json"))
		if err != nil {
			continue
		}
		var manifest BackupManifest
		if json.Unmarshal(raw, &manifest) != nil || manifest.OperationID != entry.Name() || manifest.CreatedAt.IsZero() {
			continue
		}
		candidates = append(candidates, candidate{path: path, createdAt: manifest.CreatedAt.UTC()})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].createdAt.After(candidates[j].createdAt) })
	if retainCount < 0 {
		retainCount = 0
	}
	cutoff := now.UTC().AddDate(0, 0, -retainDays)
	var removed []string
	for index, candidate := range candidates {
		exceedsCount := retainCount > 0 && index >= retainCount
		exceedsAge := retainDays > 0 && candidate.createdAt.Before(cutoff)
		if !exceedsCount && !exceedsAge {
			continue
		}
		if err := os.RemoveAll(candidate.path); err != nil {
			return removed, err
		}
		removed = append(removed, candidate.path)
	}
	return removed, nil
}

func validateBackupMarker(backupDir string) error {
	marker, err := os.ReadFile(filepath.Join(backupDir, ".ice-art-update-backup"))
	if err != nil {
		return err
	}
	if string(marker) != backupMarkerContent {
		return fmt.Errorf("invalid backup ownership marker")
	}
	return nil
}

func copyBackupTree(sourceRoot, destinationRoot string) error {
	if err := ensureNoSymlink(sourceRoot); err != nil {
		return err
	}
	return filepath.WalkDir(sourceRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("backup tree contains symlink: %s", path)
		}
		relative, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(destinationRoot, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o700)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("backup tree contains unsupported file: %s", path)
		}
		return copyBackupFile(path, destination, info.Mode().Perm())
	})
}

func treeSHA256(root string) (string, error) {
	hash := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("backup tree contains symlink: %s", path)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(hash, filepath.ToSlash(relative)+"\n"); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("backup tree contains unsupported file: %s", path)
		}
		fileHash, err := fileSHA256(path)
		if err != nil {
			return err
		}
		_, err = io.WriteString(hash, fileHash+"\n")
		return err
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func copyBackupFile(sourcePath, destinationPath string, mode os.FileMode) error {
	info, err := os.Lstat(sourcePath)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("backup source is not a regular file: %s", sourcePath)
	}
	if err := os.MkdirAll(filepath.Dir(destinationPath), 0o700); err != nil {
		return err
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	destination, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(destination, source); err != nil {
		_ = destination.Close()
		return err
	}
	if err := destination.Sync(); err != nil {
		_ = destination.Close()
		return err
	}
	return destination.Close()
}
