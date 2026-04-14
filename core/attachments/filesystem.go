package attachments

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

type FilesystemStore struct {
	root string
}

type filesystemObjectMetadata struct {
	FileName string `json:"file_name"`
	MimeType string `json:"mime_type"`
}

func NewFilesystemStore(root string) (*FilesystemStore, error) {
	trimmedRoot := strings.TrimSpace(root)
	if trimmedRoot == "" {
		return nil, fmt.Errorf("filesystem root cannot be empty")
	}
	absRoot, err := filepath.Abs(trimmedRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve filesystem root: %w", err)
	}
	if err := os.MkdirAll(absRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create filesystem root: %w", err)
	}
	return &FilesystemStore{root: filepath.Clean(absRoot)}, nil
}

func (s *FilesystemStore) PutDraft(ctx context.Context, input PutDraftInput) (*StoredObject, error) {
	if err := s.requireStore(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.FileName) == "" {
		return nil, fmt.Errorf("draft file name cannot be empty")
	}
	if len(input.Data) == 0 {
		return nil, fmt.Errorf("draft data cannot be empty")
	}

	storageKey := strings.TrimSpace(input.StorageKey)
	if storageKey == "" {
		storageKey = buildDraftStorageKey(input.FileName)
	}
	path, err := s.resolvePath(storageKey)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("storage key %q already exists", storageKey)
		}
		return nil, err
	}
	if _, err := file.Write(input.Data); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	if err := s.writeMetadata(path, filesystemObjectMetadata{
		FileName: strings.TrimSpace(input.FileName),
		MimeType: normalizeMimeType(input.MimeType),
	}); err != nil {
		_ = os.Remove(path)
		return nil, err
	}

	return &StoredObject{
		StorageBackend: BackendFilesystem,
		StorageKey:     storageKey,
		FileName:       strings.TrimSpace(input.FileName),
		MimeType:       normalizeMimeType(input.MimeType),
		SizeBytes:      int64(len(input.Data)),
		Kind:           normalizeKind("", input.MimeType),
	}, nil
}

func (s *FilesystemStore) PromoteDraft(ctx context.Context, storageKey string) (string, error) {
	if err := s.requireStore(); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	path, err := s.resolvePath(storageKey)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%w: %s", ErrObjectNotFound, strings.TrimSpace(storageKey))
		}
		return "", err
	}

	nextKey := buildSentStorageKey(storageKey)
	nextPath, err := s.resolvePath(nextKey)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(nextPath), 0o755); err != nil {
		return "", err
	}
	if _, err := os.Stat(nextPath); err == nil {
		return "", fmt.Errorf("storage key %q already exists", nextKey)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.Rename(path, nextPath); err != nil {
		return "", err
	}

	metaPath := metadataPath(path)
	nextMetaPath := metadataPath(nextPath)
	if _, err := os.Stat(metaPath); err == nil {
		if err := os.Rename(metaPath, nextMetaPath); err != nil {
			_ = os.Rename(nextPath, path)
			return "", err
		}
	} else if err != nil && !os.IsNotExist(err) {
		_ = os.Rename(nextPath, path)
		return "", err
	}
	return nextKey, nil
}

func (s *FilesystemStore) Open(ctx context.Context, storageKey string) (io.ReadCloser, ObjectMeta, error) {
	if err := s.requireStore(); err != nil {
		return nil, ObjectMeta{}, err
	}
	if err := ctx.Err(); err != nil {
		return nil, ObjectMeta{}, err
	}

	path, err := s.resolvePath(storageKey)
	if err != nil {
		return nil, ObjectMeta{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ObjectMeta{}, fmt.Errorf("%w: %s", ErrObjectNotFound, strings.TrimSpace(storageKey))
		}
		return nil, ObjectMeta{}, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, ObjectMeta{}, err
	}
	meta, err := s.readMetadata(path)
	if err != nil {
		_ = file.Close()
		return nil, ObjectMeta{}, err
	}
	return file, ObjectMeta{
		StorageKey: strings.TrimSpace(storageKey),
		FileName:   firstNonEmpty(meta.FileName, filepath.Base(path)),
		MimeType:   meta.MimeType,
		SizeBytes:  info.Size(),
		ModTime:    info.ModTime().UTC(),
	}, nil
}

func (s *FilesystemStore) Delete(ctx context.Context, storageKey string) error {
	if err := s.requireStore(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	path, err := s.resolvePath(storageKey)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: %s", ErrObjectNotFound, strings.TrimSpace(storageKey))
		}
		return err
	}
	_ = os.Remove(metadataPath(path))
	return nil
}

func (s *FilesystemStore) Stat(ctx context.Context, storageKey string) (ObjectMeta, error) {
	if err := s.requireStore(); err != nil {
		return ObjectMeta{}, err
	}
	if err := ctx.Err(); err != nil {
		return ObjectMeta{}, err
	}

	path, err := s.resolvePath(storageKey)
	if err != nil {
		return ObjectMeta{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ObjectMeta{}, fmt.Errorf("%w: %s", ErrObjectNotFound, strings.TrimSpace(storageKey))
		}
		return ObjectMeta{}, err
	}
	meta, err := s.readMetadata(path)
	if err != nil {
		return ObjectMeta{}, err
	}
	return ObjectMeta{
		StorageKey: strings.TrimSpace(storageKey),
		FileName:   firstNonEmpty(meta.FileName, filepath.Base(path)),
		MimeType:   meta.MimeType,
		SizeBytes:  info.Size(),
		ModTime:    info.ModTime().UTC(),
	}, nil
}

func (s *FilesystemStore) GCExpired(ctx context.Context, now time.Time, limit int) (int, error) {
	if err := s.requireStore(); err != nil {
		return 0, err
	}

	draftsRoot := filepath.Join(s.root, "drafts")
	if _, err := os.Stat(draftsRoot); os.IsNotExist(err) {
		return 0, nil
	} else if err != nil {
		return 0, err
	}

	deleted := 0
	walkErr := filepath.WalkDir(draftsRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if strings.HasSuffix(entry.Name(), ".meta.json") {
			return nil
		}
		if limit > 0 && deleted >= limit {
			return filepath.SkipAll
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.ModTime().After(now) {
			return nil
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		_ = os.Remove(metadataPath(path))
		deleted++
		return nil
	})
	if walkErr != nil {
		if walkErr == filepath.SkipAll {
			return deleted, nil
		}
		return deleted, walkErr
	}
	return deleted, nil
}

func (s *FilesystemStore) requireStore() error {
	if s == nil || strings.TrimSpace(s.root) == "" {
		return fmt.Errorf("filesystem store root cannot be empty")
	}
	return nil
}

func (s *FilesystemStore) resolvePath(storageKey string) (string, error) {
	trimmedKey := strings.TrimSpace(storageKey)
	if trimmedKey == "" {
		return "", fmt.Errorf("storage key cannot be empty")
	}
	cleanedKey := filepath.Clean(filepath.FromSlash(trimmedKey))
	if cleanedKey == "." || cleanedKey == "" || cleanedKey == ".." || filepath.IsAbs(cleanedKey) || strings.HasPrefix(cleanedKey, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("storage key %q is invalid", storageKey)
	}
	path := filepath.Join(s.root, cleanedKey)
	rel, err := filepath.Rel(s.root, path)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("storage key %q escapes root", storageKey)
	}
	return path, nil
}

func buildDraftStorageKey(fileName string) string {
	now := time.Now().UTC()
	safeName := sanitizeFileName(fileName)
	return filepath.ToSlash(filepath.Join("drafts", now.Format("20060102"), uuid.NewString()+"-"+safeName))
}

func buildSentStorageKey(storageKey string) string {
	trimmed := strings.TrimSpace(storageKey)
	trimmed = strings.TrimPrefix(trimmed, "drafts/")
	trimmed = strings.TrimPrefix(trimmed, "sent/")
	if trimmed == "" {
		trimmed = uuid.NewString()
	}
	return filepath.ToSlash(filepath.Join("sent", trimmed))
}

func sanitizeFileName(fileName string) string {
	base := filepath.Base(strings.TrimSpace(fileName))
	base = strings.ReplaceAll(base, string(filepath.Separator), "_")
	base = strings.ReplaceAll(base, "/", "_")
	if base == "" || base == "." {
		return "attachment.bin"
	}
	return base
}

func (s *FilesystemStore) readMetadata(path string) (filesystemObjectMetadata, error) {
	data, err := os.ReadFile(metadataPath(path))
	if err != nil {
		if os.IsNotExist(err) {
			return filesystemObjectMetadata{}, nil
		}
		return filesystemObjectMetadata{}, err
	}
	var meta filesystemObjectMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return filesystemObjectMetadata{}, err
	}
	meta.FileName = strings.TrimSpace(meta.FileName)
	meta.MimeType = normalizeMimeType(meta.MimeType)
	return meta, nil
}

func (s *FilesystemStore) writeMetadata(path string, meta filesystemObjectMetadata) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return os.WriteFile(metadataPath(path), data, 0o644)
}

func metadataPath(path string) string {
	return path + ".meta.json"
}
