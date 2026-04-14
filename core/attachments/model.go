package attachments

import "time"

type Attachment struct {
	ID             string     `json:"id" gorm:"type:varchar(64);primaryKey"`
	ConversationID string     `json:"conversation_id" gorm:"type:varchar(64);index"`
	MessageSeq     *int64     `json:"message_seq,omitempty" gorm:"index"`
	CreatedBy      string     `json:"created_by" gorm:"type:varchar(128);index"`
	StorageBackend string     `json:"storage_backend" gorm:"type:varchar(32);not null"`
	StorageKey     string     `json:"storage_key" gorm:"type:varchar(512);not null;uniqueIndex"`
	SHA256         string     `json:"sha256" gorm:"type:varchar(128);not null;default:''"`
	FileName       string     `json:"file_name" gorm:"type:varchar(255);not null"`
	MimeType       string     `json:"mime_type" gorm:"type:varchar(255);not null"`
	SizeBytes      int64      `json:"size_bytes" gorm:"not null;default:0"`
	Kind           string     `json:"kind" gorm:"type:varchar(32);not null;default:''"`
	Status         Status     `json:"status" gorm:"type:varchar(32);not null;index"`
	Lifecycle      Lifecycle  `json:"lifecycle" gorm:"type:varchar(64);not null;index"`
	PreviewText    string     `json:"preview_text" gorm:"type:text;not null;default:''"`
	ContextText    string     `json:"context_text" gorm:"type:text;not null;default:''"`
	Width          *int       `json:"width,omitempty"`
	Height         *int       `json:"height,omitempty"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty" gorm:"index"`
	LastAccessedAt *time.Time `json:"last_accessed_at,omitempty" gorm:"index"`
	DeletedAt      *time.Time `json:"deleted_at,omitempty" gorm:"index"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (Attachment) TableName() string {
	return "conversation_attachments"
}
