package audit

import (
	"encoding/json"
	"time"
)

type Run struct {
	ID             string        `json:"id" gorm:"type:varchar(64);primaryKey"`
	TaskID         string        `json:"task_id" gorm:"type:varchar(64);not null;uniqueIndex:idx_audit_runs_task_id"`
	ConversationID string        `json:"conversation_id,omitempty" gorm:"type:varchar(64);index"`
	TaskType       string        `json:"task_type" gorm:"type:varchar(128);not null;index"`
	ProviderID     string        `json:"provider_id,omitempty" gorm:"type:varchar(128);index"`
	ModelID        string        `json:"model_id,omitempty" gorm:"type:varchar(128);index"`
	RunnerID       string        `json:"runner_id,omitempty" gorm:"type:varchar(128);index"`
	Status         Status        `json:"status" gorm:"type:varchar(32);not null;index"`
	CreatedBy      string        `json:"created_by,omitempty" gorm:"type:varchar(128)"`
	Replayable     bool          `json:"replayable" gorm:"not null;default:false"`
	SchemaVersion  SchemaVersion `json:"schema_version" gorm:"type:varchar(16);not null"`
	StartedAt      *time.Time    `json:"started_at,omitempty"`
	FinishedAt     *time.Time    `json:"finished_at,omitempty"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
}

func (Run) TableName() string {
	return "audit_runs"
}

type Event struct {
	ID    uint64 `json:"id" gorm:"primaryKey;autoIncrement"`
	RunID string `json:"run_id" gorm:"type:varchar(64);not null;uniqueIndex:idx_audit_run_seq,priority:1;index"`
	// TaskID is duplicated from the parent run so task-centric audit queries can filter events without joining audit_runs.
	TaskID        string          `json:"task_id" gorm:"type:varchar(64);not null;index"`
	Seq           int64           `json:"seq" gorm:"not null;uniqueIndex:idx_audit_run_seq,priority:2"`
	Phase         Phase           `json:"phase" gorm:"type:varchar(64);not null;index"`
	EventType     string          `json:"event_type" gorm:"type:varchar(64);not null;index"`
	Level         string          `json:"level" gorm:"type:varchar(16);not null;default:'info'"`
	StepIndex     int             `json:"step_index" gorm:"not null;default:0"`
	ParentSeq     int64           `json:"parent_seq" gorm:"not null;default:0"`
	RefArtifactID string          `json:"ref_artifact_id,omitempty" gorm:"type:varchar(64);index"`
	PayloadJSON   json.RawMessage `json:"payload,omitempty" gorm:"column:payload_json;type:blob"`
	CreatedAt     time.Time       `json:"created_at" gorm:"not null;index"`
}

func (Event) TableName() string {
	return "audit_events"
}

type Artifact struct {
	ID             string          `json:"id" gorm:"type:varchar(64);primaryKey"`
	RunID          string          `json:"run_id" gorm:"type:varchar(64);not null;index"`
	Kind           ArtifactKind    `json:"kind" gorm:"type:varchar(64);not null;index"`
	MimeType       string          `json:"mime_type" gorm:"type:varchar(128);not null"`
	Encoding       string          `json:"encoding" gorm:"type:varchar(32);not null;default:'identity'"`
	SizeBytes      int64           `json:"size_bytes" gorm:"not null;default:0"`
	SHA256         string          `json:"sha256,omitempty" gorm:"type:varchar(64);index"`
	RedactionState string          `json:"redaction_state" gorm:"type:varchar(32);not null;default:'raw'"`
	BodyJSON       json.RawMessage `json:"body,omitempty" gorm:"column:body_json;type:blob"`
	CreatedAt      time.Time       `json:"created_at" gorm:"not null;index"`
}

func (Artifact) TableName() string {
	return "audit_artifacts"
}
