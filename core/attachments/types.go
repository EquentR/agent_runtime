package attachments

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"
)

var ErrAttachmentNotFound = errors.New("attachment not found")
var ErrAttachmentNotDraft = errors.New("attachment is not draft")
var ErrObjectNotFound = errors.New("attachment object not found")
var ErrAttachmentExpired = errors.New("attachment is expired")

type Status string

const (
	StatusDraft   Status = "draft"
	StatusSent    Status = "sent"
	StatusExpired Status = "expired"
)

type Lifecycle string

const (
	LifecycleDraft                Lifecycle = "draft"
	LifecycleConversationRetained Lifecycle = "conversation_retained"
)

const (
	BackendFilesystem = "filesystem"
	KindImage         = "image"
	KindText          = "text"
	KindBinary        = "binary"
)

type CreateDraftInput struct {
	ID             string
	ConversationID string
	CreatedBy      string
	StorageBackend string
	StorageKey     string
	SHA256         string
	FileName       string
	MimeType       string
	SizeBytes      int64
	Kind           string
	PreviewText    string
	ContextText    string
	Width          *int
	Height         *int
	ExpiresAt      *time.Time
}

type PromoteInput struct {
	ConversationID string
	MessageSeq     *int64
	RetainUntil    *time.Time
}

type PutDraftInput struct {
	StorageKey string
	FileName   string
	MimeType   string
	Data       []byte
}

type StoredObject struct {
	StorageBackend string
	StorageKey     string
	FileName       string
	MimeType       string
	SizeBytes      int64
	Kind           string
}

type ObjectMeta struct {
	StorageKey string
	FileName   string
	MimeType   string
	SizeBytes  int64
	ModTime    time.Time
}

type Storage interface {
	PutDraft(ctx context.Context, input PutDraftInput) (*StoredObject, error)
	PromoteDraft(ctx context.Context, storageKey string) (string, error)
	Open(ctx context.Context, storageKey string) (io.ReadCloser, ObjectMeta, error)
	Delete(ctx context.Context, storageKey string) error
	Stat(ctx context.Context, storageKey string) (ObjectMeta, error)
	GCExpired(ctx context.Context, now time.Time, limit int) (int, error)
}

func normalizeBackend(value string) string {
	backend := strings.TrimSpace(value)
	if backend == "" {
		return BackendFilesystem
	}
	return backend
}

func normalizeKind(kind string, mimeType string) string {
	normalized := strings.TrimSpace(kind)
	if normalized != "" {
		return normalized
	}
	mimeType = strings.TrimSpace(strings.ToLower(mimeType))
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return KindImage
	case strings.HasPrefix(mimeType, "text/"), strings.HasSuffix(mimeType, "+json"), mimeType == "application/json":
		return KindText
	default:
		return KindBinary
	}
}
