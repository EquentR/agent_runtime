package attachments

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestAttachmentStoreCreateDraftPersistsMetadataAndStorageKey(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	expiresAt := time.Now().UTC().Add(2 * time.Hour)
	created, err := store.CreateDraft(ctx, CreateDraftInput{
		CreatedBy:      " alice ",
		StorageBackend: BackendFilesystem,
		StorageKey:     "drafts/att-1-notes.txt",
		FileName:       " notes.txt ",
		MimeType:       " text/plain ",
		SizeBytes:      128,
		Kind:           KindText,
		PreviewText:    "first line",
		ContextText:    "notes context",
		ExpiresAt:      &expiresAt,
	})
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	if created.ID == "" {
		t.Fatal("created attachment id = empty, want non-empty")
	}
	if created.CreatedBy != "alice" {
		t.Fatalf("created created_by = %q, want %q", created.CreatedBy, "alice")
	}
	if created.Status != StatusDraft {
		t.Fatalf("created status = %q, want %q", created.Status, StatusDraft)
	}
	if created.Lifecycle != LifecycleDraft {
		t.Fatalf("created lifecycle = %q, want %q", created.Lifecycle, LifecycleDraft)
	}
	if created.StorageKey != "drafts/att-1-notes.txt" {
		t.Fatalf("created storage_key = %q, want %q", created.StorageKey, "drafts/att-1-notes.txt")
	}

	loaded, err := store.GetAttachment(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetAttachment() error = %v", err)
	}
	if loaded.FileName != "notes.txt" {
		t.Fatalf("loaded file_name = %q, want %q", loaded.FileName, "notes.txt")
	}
	if loaded.MimeType != "text/plain" {
		t.Fatalf("loaded mime_type = %q, want %q", loaded.MimeType, "text/plain")
	}
	if loaded.SizeBytes != 128 {
		t.Fatalf("loaded size_bytes = %d, want %d", loaded.SizeBytes, 128)
	}
	if loaded.StorageBackend != BackendFilesystem {
		t.Fatalf("loaded storage_backend = %q, want %q", loaded.StorageBackend, BackendFilesystem)
	}
	if loaded.PreviewText != "first line" {
		t.Fatalf("loaded preview_text = %q, want %q", loaded.PreviewText, "first line")
	}
	if loaded.ContextText != "notes context" {
		t.Fatalf("loaded context_text = %q, want %q", loaded.ContextText, "notes context")
	}

	columnTypes, err := store.db.Migrator().ColumnTypes(&Attachment{})
	if err != nil {
		t.Fatalf("ColumnTypes() error = %v", err)
	}
	for _, columnType := range columnTypes {
		if strings.EqualFold(columnType.Name(), "data") {
			t.Fatal("conversation_attachments unexpectedly has raw data column")
		}
	}
}

func TestAttachmentStorePromoteDraftToSentBindsConversationAndKeepsMetadata(t *testing.T) {
	ctx := context.Background()
	storage, err := NewFilesystemStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFilesystemStore() error = %v", err)
	}
	storedObject, err := storage.PutDraft(ctx, PutDraftInput{
		FileName: "image.png",
		MimeType: "image/png",
		Data:     []byte("png-bytes"),
	})
	if err != nil {
		t.Fatalf("PutDraft() error = %v", err)
	}
	store := newTestStoreWithStorage(t, storage)

	draft, err := store.CreateDraft(ctx, CreateDraftInput{
		CreatedBy:      "alice",
		StorageBackend: BackendFilesystem,
		StorageKey:     storedObject.StorageKey,
		FileName:       "image.png",
		MimeType:       "image/png",
		SizeBytes:      storedObject.SizeBytes,
		Kind:           KindImage,
		PreviewText:    "preview",
		ContextText:    "[image attachment: image.png]",
	})
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}

	messageSeq := int64(7)
	retainUntil := time.Now().UTC().Add(24 * time.Hour)
	sent, err := store.PromoteDraftToSent(ctx, draft.ID, PromoteInput{
		ConversationID: " conv-1 ",
		MessageSeq:     &messageSeq,
		RetainUntil:    &retainUntil,
	})
	if err != nil {
		t.Fatalf("PromoteDraftToSent() error = %v", err)
	}
	if sent.Status != StatusSent {
		t.Fatalf("sent status = %q, want %q", sent.Status, StatusSent)
	}
	if sent.Lifecycle != LifecycleConversationRetained {
		t.Fatalf("sent lifecycle = %q, want %q", sent.Lifecycle, LifecycleConversationRetained)
	}
	if !strings.HasPrefix(sent.StorageKey, "sent/") {
		t.Fatalf("sent storage_key = %q, want sent/ prefix", sent.StorageKey)
	}
	if sent.ConversationID != "conv-1" {
		t.Fatalf("sent conversation_id = %q, want %q", sent.ConversationID, "conv-1")
	}
	if sent.MessageSeq == nil || *sent.MessageSeq != 7 {
		t.Fatalf("sent message_seq = %v, want 7", sent.MessageSeq)
	}
	if sent.StorageKey == draft.StorageKey {
		t.Fatalf("sent storage_key = %q, want key different from draft key", sent.StorageKey)
	}
	if sent.ExpiresAt == nil || !sent.ExpiresAt.Equal(retainUntil) {
		t.Fatalf("sent expires_at = %v, want %v", sent.ExpiresAt, retainUntil)
	}
	if sent.FileName != "image.png" {
		t.Fatalf("sent file_name = %q, want %q", sent.FileName, "image.png")
	}
	reader, meta, err := storage.Open(ctx, sent.StorageKey)
	if err != nil {
		t.Fatalf("Open() promoted error = %v", err)
	}
	defer reader.Close()
	if meta.FileName != "image.png" {
		t.Fatalf("promoted file_name = %q, want %q", meta.FileName, "image.png")
	}
	if meta.MimeType != "image/png" {
		t.Fatalf("promoted mime_type = %q, want %q", meta.MimeType, "image/png")
	}

	loaded, err := store.GetAttachment(ctx, draft.ID)
	if err != nil {
		t.Fatalf("GetAttachment() error = %v", err)
	}
	if loaded.Status != StatusSent {
		t.Fatalf("loaded status = %q, want %q", loaded.Status, StatusSent)
	}
	if loaded.ConversationID != "conv-1" {
		t.Fatalf("loaded conversation_id = %q, want %q", loaded.ConversationID, "conv-1")
	}
	if loaded.PreviewText != "preview" {
		t.Fatalf("loaded preview_text = %q, want %q", loaded.PreviewText, "preview")
	}
	if loaded.StorageKey != sent.StorageKey {
		t.Fatalf("loaded storage_key = %q, want %q", loaded.StorageKey, sent.StorageKey)
	}
}

func TestFilesystemStoreOpenDeleteAndGCExpired(t *testing.T) {
	root := t.TempDir()
	store, err := NewFilesystemStore(root)
	if err != nil {
		t.Fatalf("NewFilesystemStore() error = %v", err)
	}

	stored, err := store.PutDraft(context.Background(), PutDraftInput{
		FileName: "hello.txt",
		MimeType: "text/plain",
		Data:     []byte("hello"),
	})
	if err != nil {
		t.Fatalf("PutDraft() error = %v", err)
	}
	if stored.StorageKey == "" {
		t.Fatal("stored storage_key = empty, want non-empty")
	}
	if !strings.HasPrefix(stored.StorageKey, "drafts/") {
		t.Fatalf("stored storage_key = %q, want drafts/ prefix", stored.StorageKey)
	}

	path := filepath.Join(root, filepath.FromSlash(stored.StorageKey))
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Stat(%q) error = %v", path, err)
	}

	reader, meta, err := store.Open(context.Background(), stored.StorageKey)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("stored content = %q, want %q", string(data), "hello")
	}
	if meta.SizeBytes != int64(len(data)) {
		t.Fatalf("meta size_bytes = %d, want %d", meta.SizeBytes, len(data))
	}
	if meta.FileName != "hello.txt" {
		t.Fatalf("meta file_name = %q, want %q", meta.FileName, "hello.txt")
	}
	if meta.MimeType != "text/plain" {
		t.Fatalf("meta mime_type = %q, want %q", meta.MimeType, "text/plain")
	}

	promotedKey, err := store.PromoteDraft(context.Background(), stored.StorageKey)
	if err != nil {
		t.Fatalf("PromoteDraft() error = %v", err)
	}
	if !strings.HasPrefix(promotedKey, "sent/") {
		t.Fatalf("promotedKey = %q, want sent/ prefix", promotedKey)
	}
	if deleted, err := store.GCExpired(context.Background(), time.Now().UTC().Add(time.Hour), 10); err != nil {
		t.Fatalf("GCExpired() after promote error = %v", err)
	} else if deleted != 0 {
		t.Fatalf("GCExpired() after promote deleted = %d, want 0", deleted)
	}
	promotedReader, promotedMeta, err := store.Open(context.Background(), promotedKey)
	if err != nil {
		t.Fatalf("Open() promotedKey error = %v", err)
	}
	_ = promotedReader.Close()
	if promotedMeta.FileName != "hello.txt" {
		t.Fatalf("promotedMeta file_name = %q, want %q", promotedMeta.FileName, "hello.txt")
	}
	if promotedMeta.MimeType != "text/plain" {
		t.Fatalf("promotedMeta mime_type = %q, want %q", promotedMeta.MimeType, "text/plain")
	}

	if err := store.Delete(context.Background(), promotedKey); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, _, err := store.Open(context.Background(), promotedKey); err == nil {
		t.Fatal("Open() error = nil after delete, want non-nil")
	}

	stalePath := filepath.Join(root, "drafts", "stale.txt")
	if err := os.MkdirAll(filepath.Dir(stalePath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(stalePath, []byte("stale"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	staleTime := time.Now().UTC().Add(-2 * time.Hour)
	if err := os.Chtimes(stalePath, staleTime, staleTime); err != nil {
		t.Fatalf("Chtimes() error = %v", err)
	}

	deleted, err := store.GCExpired(context.Background(), time.Now().UTC().Add(-time.Hour), 10)
	if err != nil {
		t.Fatalf("GCExpired() error = %v", err)
	}
	if deleted != 1 {
		t.Fatalf("GCExpired() deleted = %d, want %d", deleted, 1)
	}
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Fatalf("Stat(%q) error = %v, want not exists", stalePath, err)
	}
}

func TestAttachmentStoreGCExpiresDraftAttachments(t *testing.T) {
	ctx := context.Background()
	storage, err := NewFilesystemStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFilesystemStore() error = %v", err)
	}
	store := newTestStoreWithStorage(t, storage)

	storedObject, err := storage.PutDraft(ctx, PutDraftInput{
		FileName: "draft.txt",
		MimeType: "text/plain",
		Data:     []byte("draft"),
	})
	if err != nil {
		t.Fatalf("PutDraft() error = %v", err)
	}
	expiredAt := time.Now().UTC().Add(-time.Minute)
	attachment, err := store.CreateDraft(ctx, CreateDraftInput{
		ID:             "att_draft_gc",
		CreatedBy:      "alice",
		StorageBackend: storedObject.StorageBackend,
		StorageKey:     storedObject.StorageKey,
		FileName:       storedObject.FileName,
		MimeType:       storedObject.MimeType,
		SizeBytes:      storedObject.SizeBytes,
		Kind:           storedObject.Kind,
		ExpiresAt:      &expiredAt,
	})
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}

	processed, err := store.GCExpired(ctx, time.Now().UTC(), 10)
	if err != nil {
		t.Fatalf("GCExpired() error = %v", err)
	}
	if processed != 1 {
		t.Fatalf("GCExpired() processed = %d, want 1", processed)
	}
	if _, err := store.GetAttachment(ctx, attachment.ID); !errors.Is(err, ErrAttachmentNotFound) {
		t.Fatalf("GetAttachment() error = %v, want ErrAttachmentNotFound", err)
	}
	if _, err := storage.Stat(ctx, storedObject.StorageKey); !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("Stat() error = %v, want ErrObjectNotFound", err)
	}
}

func TestAttachmentStoreMarksSentAttachmentExpiredWithoutRemovingMetadata(t *testing.T) {
	ctx := context.Background()
	storage, err := NewFilesystemStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFilesystemStore() error = %v", err)
	}
	store := newTestStoreWithStorage(t, storage)

	storedObject, err := storage.PutDraft(ctx, PutDraftInput{
		FileName: "sent.txt",
		MimeType: "text/plain",
		Data:     []byte("sent"),
	})
	if err != nil {
		t.Fatalf("PutDraft() error = %v", err)
	}
	draft, err := store.CreateDraft(ctx, CreateDraftInput{
		ID:             "att_sent_gc",
		CreatedBy:      "alice",
		StorageBackend: storedObject.StorageBackend,
		StorageKey:     storedObject.StorageKey,
		FileName:       storedObject.FileName,
		MimeType:       storedObject.MimeType,
		SizeBytes:      storedObject.SizeBytes,
		Kind:           storedObject.Kind,
	})
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	expiredAt := time.Now().UTC().Add(-time.Minute)
	sent, err := store.PromoteDraftToSent(ctx, draft.ID, PromoteInput{
		ConversationID: "conv_1",
		RetainUntil:    &expiredAt,
	})
	if err != nil {
		t.Fatalf("PromoteDraftToSent() error = %v", err)
	}

	processed, err := store.GCExpired(ctx, time.Now().UTC(), 10)
	if err != nil {
		t.Fatalf("GCExpired() error = %v", err)
	}
	if processed != 1 {
		t.Fatalf("GCExpired() processed = %d, want 1", processed)
	}
	loaded, err := store.GetAttachment(ctx, sent.ID)
	if err != nil {
		t.Fatalf("GetAttachment() error = %v", err)
	}
	if loaded.Status != StatusExpired {
		t.Fatalf("loaded status = %q, want %q", loaded.Status, StatusExpired)
	}
	if loaded.FileName != "sent.txt" {
		t.Fatalf("loaded file_name = %q, want %q", loaded.FileName, "sent.txt")
	}
	if loaded.ConversationID != "conv_1" {
		t.Fatalf("loaded conversation_id = %q, want %q", loaded.ConversationID, "conv_1")
	}
	if _, err := storage.Stat(ctx, sent.StorageKey); !errors.Is(err, ErrObjectNotFound) {
		t.Fatalf("Stat() error = %v, want ErrObjectNotFound", err)
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()

	store := NewStore(newTestDB(t))
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	return store
}

func newTestStoreWithStorage(t *testing.T, storage Storage) *Store {
	t.Helper()

	store := NewStore(newTestDB(t), storage)
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	return store
}

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	return db
}
