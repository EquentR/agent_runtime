package attachments

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Store struct {
	db      *gorm.DB
	storage Storage
}

func NewStore(db *gorm.DB, storages ...Storage) *Store {
	store := &Store{db: db}
	if len(storages) > 0 {
		store.storage = storages[0]
	}
	return store
}

func (s *Store) AutoMigrate() error {
	if err := s.requireDB(); err != nil {
		return err
	}
	return s.db.AutoMigrate(&Attachment{})
}

func (s *Store) CreateDraft(ctx context.Context, input CreateDraftInput) (*Attachment, error) {
	if err := s.requireDB(); err != nil {
		return nil, err
	}

	attachment := &Attachment{
		ID:             firstNonEmpty(strings.TrimSpace(input.ID), newAttachmentID()),
		ConversationID: strings.TrimSpace(input.ConversationID),
		CreatedBy:      strings.TrimSpace(input.CreatedBy),
		StorageBackend: normalizeBackend(input.StorageBackend),
		StorageKey:     strings.TrimSpace(input.StorageKey),
		SHA256:         strings.TrimSpace(input.SHA256),
		FileName:       strings.TrimSpace(input.FileName),
		MimeType:       normalizeMimeType(input.MimeType),
		SizeBytes:      input.SizeBytes,
		Kind:           normalizeKind(input.Kind, input.MimeType),
		Status:         StatusDraft,
		Lifecycle:      LifecycleDraft,
		PreviewText:    strings.TrimSpace(input.PreviewText),
		ContextText:    strings.TrimSpace(input.ContextText),
		Width:          input.Width,
		Height:         input.Height,
		ExpiresAt:      input.ExpiresAt,
	}
	if err := validateAttachment(*attachment); err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Create(attachment).Error; err != nil {
		return nil, err
	}
	return attachment, nil
}

func (s *Store) GetAttachment(ctx context.Context, attachmentID string) (*Attachment, error) {
	if err := s.requireDB(); err != nil {
		return nil, err
	}
	trimmedID := strings.TrimSpace(attachmentID)
	if trimmedID == "" {
		return nil, fmt.Errorf("attachment id cannot be empty")
	}

	var attachment Attachment
	err := s.db.WithContext(ctx).First(&attachment, "id = ?", trimmedID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("%w: %s", ErrAttachmentNotFound, trimmedID)
	}
	if err != nil {
		return nil, err
	}
	return &attachment, nil
}

func (s *Store) PromoteDraftToSent(ctx context.Context, attachmentID string, input PromoteInput) (*Attachment, error) {
	if err := s.requireDB(); err != nil {
		return nil, err
	}

	var attachment Attachment
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&attachment, "id = ?", strings.TrimSpace(attachmentID)).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("%w: %s", ErrAttachmentNotFound, strings.TrimSpace(attachmentID))
			}
			return err
		}
		if attachment.Status != StatusDraft {
			return fmt.Errorf("%w: %s", ErrAttachmentNotDraft, attachment.ID)
		}

		now := time.Now().UTC()
		if s.storage != nil {
			nextKey, err := s.storage.PromoteDraft(ctx, attachment.StorageKey)
			if err != nil {
				return err
			}
			attachment.StorageKey = nextKey
		}
		attachment.ConversationID = strings.TrimSpace(input.ConversationID)
		attachment.MessageSeq = input.MessageSeq
		attachment.Status = StatusSent
		attachment.Lifecycle = LifecycleConversationRetained
		attachment.ExpiresAt = input.RetainUntil
		attachment.UpdatedAt = now
		if err := validateAttachment(attachment); err != nil {
			return err
		}
		return tx.Save(&attachment).Error
	})
	if err != nil {
		return nil, err
	}
	return &attachment, nil
}

func (s *Store) DeleteAttachment(ctx context.Context, attachmentID string) error {
	if err := s.requireDB(); err != nil {
		return err
	}
	trimmedID := strings.TrimSpace(attachmentID)
	if trimmedID == "" {
		return fmt.Errorf("attachment id cannot be empty")
	}

	result := s.db.WithContext(ctx).Delete(&Attachment{}, "id = ?", trimmedID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: %s", ErrAttachmentNotFound, trimmedID)
	}
	return nil
}

func (s *Store) ListExpiredAttachments(ctx context.Context, now time.Time, limit int) ([]Attachment, error) {
	if err := s.requireDB(); err != nil {
		return nil, err
	}

	query := s.db.WithContext(ctx).
		Model(&Attachment{}).
		Where("status IN ?", []Status{StatusDraft, StatusSent}).
		Where("expires_at IS NOT NULL").
		Where("expires_at <= ?", now.UTC()).
		Order("expires_at asc").
		Order("created_at asc").
		Order("id asc")
	if limit > 0 {
		query = query.Limit(limit)
	}

	var attachments []Attachment
	if err := query.Find(&attachments).Error; err != nil {
		return nil, err
	}
	return attachments, nil
}

func (s *Store) GCExpired(ctx context.Context, now time.Time, limit int) (int, error) {
	if err := s.requireDB(); err != nil {
		return 0, err
	}
	expired, err := s.ListExpiredAttachments(ctx, now, limit)
	if err != nil {
		return 0, err
	}
	if len(expired) == 0 {
		return 0, nil
	}

	processed := 0
	for _, attachment := range expired {
		if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			switch attachment.Status {
			case StatusDraft:
				result := tx.Delete(&Attachment{}, "id = ?", attachment.ID)
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected == 0 {
					return fmt.Errorf("%w: %s", ErrAttachmentNotFound, attachment.ID)
				}
				return nil
			case StatusSent:
				nowUTC := now.UTC()
				result := tx.Model(&Attachment{}).
					Where("id = ?", attachment.ID).
					Updates(map[string]any{
						"status":     StatusExpired,
						"updated_at": nowUTC,
						"expires_at": &nowUTC,
					})
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected == 0 {
					return fmt.Errorf("%w: %s", ErrAttachmentNotFound, attachment.ID)
				}
				return nil
			default:
				return nil
			}
		}); err != nil {
			return processed, err
		}
		if s.storage != nil {
			if err := s.storage.Delete(ctx, attachment.StorageKey); err != nil && !errors.Is(err, ErrObjectNotFound) {
				return processed, err
			}
		}
		processed++
	}
	return processed, nil
}

func (s *Store) requireDB() error {
	if s == nil || s.db == nil {
		return fmt.Errorf("attachment store db cannot be nil")
	}
	return nil
}

func newAttachmentID() string {
	return "att_" + uuid.NewString()
}

func validateAttachment(attachment Attachment) error {
	if strings.TrimSpace(attachment.ID) == "" {
		return fmt.Errorf("attachment id cannot be empty")
	}
	if backend := normalizeBackend(attachment.StorageBackend); backend != BackendFilesystem {
		return fmt.Errorf("attachment storage backend %q is not supported", attachment.StorageBackend)
	}
	if strings.TrimSpace(attachment.StorageKey) == "" {
		return fmt.Errorf("attachment storage key cannot be empty")
	}
	if strings.TrimSpace(attachment.FileName) == "" {
		return fmt.Errorf("attachment file name cannot be empty")
	}
	if attachment.SizeBytes < 0 {
		return fmt.Errorf("attachment size bytes cannot be negative")
	}
	switch attachment.Status {
	case StatusDraft, StatusSent, StatusExpired:
	default:
		return fmt.Errorf("attachment status %q is invalid", attachment.Status)
	}
	if attachment.Status == StatusSent && strings.TrimSpace(attachment.ConversationID) == "" {
		return fmt.Errorf("sent attachment conversation id cannot be empty")
	}
	switch attachment.Lifecycle {
	case LifecycleDraft, LifecycleConversationRetained:
	default:
		return fmt.Errorf("attachment lifecycle %q is invalid", attachment.Lifecycle)
	}
	return nil
}

func normalizeMimeType(value string) string {
	mimeType := strings.TrimSpace(value)
	if mimeType == "" {
		return "application/octet-stream"
	}
	return mimeType
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
