package interactions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var ErrInteractionNotFound = errors.New("interaction not found")

type Kind string

const (
	KindApproval Kind = "approval"
	KindQuestion Kind = "question"
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusApproved  Status = "approved"
	StatusRejected  Status = "rejected"
	StatusResponded Status = "responded"
	StatusExpired   Status = "expired"
	StatusCancelled Status = "cancelled"
)

type Interaction struct {
	ID             string     `json:"id" gorm:"type:varchar(64);primaryKey"`
	TaskID         string     `json:"task_id" gorm:"type:varchar(64);not null;index"`
	ConversationID string     `json:"conversation_id" gorm:"type:varchar(64);index"`
	StepIndex      int        `json:"step_index" gorm:"not null;default:0"`
	ToolCallID     string     `json:"tool_call_id" gorm:"type:varchar(128);index"`
	Kind           Kind       `json:"kind" gorm:"type:varchar(32);not null;index"`
	Status         Status     `json:"status" gorm:"type:varchar(32);not null;index"`
	RequestJSON    []byte     `json:"request_json" gorm:"type:blob;not null"`
	ResponseJSON   []byte     `json:"response_json" gorm:"type:blob"`
	RespondedBy    string     `json:"responded_by" gorm:"type:varchar(128)"`
	RespondedAt    *time.Time `json:"responded_at"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (Interaction) TableName() string {
	return "interactions"
}

type Option struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type QuestionRequest struct {
	Prompt      string   `json:"prompt"`
	Options     []Option `json:"options,omitempty"`
	AllowCustom bool     `json:"allow_custom"`
}

type ResponseInput struct {
	Status      Status
	Response    any
	RespondedBy string
	RespondedAt *time.Time
}

type CreateInteractionInput struct {
	ID             string
	TaskID         string
	ConversationID string
	StepIndex      int
	ToolCallID     string
	Kind           Kind
	Status         Status
	Request        any
	Response       any
	RespondedBy    string
	RespondedAt    *time.Time
}

type Store struct {
	db *gorm.DB
}

func NewStore(db *gorm.DB) *Store {
	return &Store{db: db}
}

func (s *Store) AutoMigrate() error {
	if s == nil || s.db == nil {
		return fmt.Errorf("interaction store db cannot be nil")
	}
	return s.db.AutoMigrate(&Interaction{})
}

func (s *Store) CreateInteraction(ctx context.Context, input CreateInteractionInput) (*Interaction, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("interaction store db cannot be nil")
	}

	requestJSON, err := marshalJSON(input.Request)
	if err != nil {
		return nil, fmt.Errorf("marshal interaction request: %w", err)
	}
	responseJSON, err := marshalJSON(input.Response)
	if err != nil {
		return nil, fmt.Errorf("marshal interaction response: %w", err)
	}

	interaction := &Interaction{
		ID:             strings.TrimSpace(input.ID),
		TaskID:         strings.TrimSpace(input.TaskID),
		ConversationID: strings.TrimSpace(input.ConversationID),
		StepIndex:      input.StepIndex,
		ToolCallID:     strings.TrimSpace(input.ToolCallID),
		Kind:           input.Kind,
		Status:         input.Status,
		RequestJSON:    requestJSON,
		ResponseJSON:   responseJSON,
		RespondedBy:    strings.TrimSpace(input.RespondedBy),
		RespondedAt:    input.RespondedAt,
	}
	if interaction.ID == "" {
		interaction.ID = newInteractionID()
	}
	if interaction.TaskID == "" {
		return nil, fmt.Errorf("task id cannot be empty")
	}
	if !isSupportedKind(interaction.Kind) {
		return nil, fmt.Errorf("unsupported interaction kind %q", interaction.Kind)
	}
	if interaction.Kind == "" {
		return nil, fmt.Errorf("interaction kind cannot be empty")
	}
	if interaction.Status == "" {
		interaction.Status = StatusPending
	}
	if !isSupportedStatus(interaction.Status) {
		return nil, fmt.Errorf("unsupported interaction status %q", interaction.Status)
	}
	if interaction.Status == StatusPending {
		if len(interaction.ResponseJSON) > 0 || interaction.RespondedBy != "" || interaction.RespondedAt != nil {
			return nil, fmt.Errorf("pending interaction cannot include response metadata")
		}
	} else if len(interaction.ResponseJSON) > 0 {
		if interaction.RespondedBy == "" && interaction.RespondedAt == nil {
			return nil, fmt.Errorf("responded interaction must include responder metadata")
		}
	}
	if len(interaction.RequestJSON) == 0 {
		interaction.RequestJSON = []byte("{}")
	}

	if err := s.db.WithContext(ctx).Create(interaction).Error; err != nil {
		return nil, err
	}
	return interaction, nil
}

func (s *Store) GetInteraction(ctx context.Context, taskID string, interactionID string) (*Interaction, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("interaction store db cannot be nil")
	}
	trimmedTaskID := strings.TrimSpace(taskID)
	if trimmedTaskID == "" {
		return nil, fmt.Errorf("task id cannot be empty")
	}
	trimmedInteractionID := strings.TrimSpace(interactionID)
	if trimmedInteractionID == "" {
		return nil, fmt.Errorf("interaction id cannot be empty")
	}

	query := s.db.WithContext(ctx).Where("id = ?", trimmedInteractionID).Where("task_id = ?", trimmedTaskID)

	var interaction Interaction
	err := query.Take(&interaction).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("%w: %s", ErrInteractionNotFound, trimmedInteractionID)
	}
	if err != nil {
		return nil, err
	}
	return &interaction, nil
}

func (s *Store) ListTaskInteractions(ctx context.Context, taskID string) ([]Interaction, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("interaction store db cannot be nil")
	}
	trimmedTaskID := strings.TrimSpace(taskID)
	if trimmedTaskID == "" {
		return nil, nil
	}
	var listed []Interaction
	err := s.db.WithContext(ctx).
		Where("task_id = ?", trimmedTaskID).
		Order("created_at desc").
		Order("id desc").
		Find(&listed).Error
	if err != nil {
		return nil, err
	}
	return listed, nil
}

func (s *Store) RespondInteraction(ctx context.Context, taskID string, interactionID string, input ResponseInput) (*Interaction, bool, error) {
	if s == nil || s.db == nil {
		return nil, false, fmt.Errorf("interaction store db cannot be nil")
	}
	trimmedTaskID := strings.TrimSpace(taskID)
	if trimmedTaskID == "" {
		return nil, false, fmt.Errorf("task id cannot be empty")
	}
	trimmedInteractionID := strings.TrimSpace(interactionID)
	if trimmedInteractionID == "" {
		return nil, false, fmt.Errorf("interaction id cannot be empty")
	}
	if !isSupportedStatus(input.Status) || input.Status == StatusPending {
		return nil, false, fmt.Errorf("unsupported interaction response status %q", input.Status)
	}
	responseJSON, err := marshalJSON(input.Response)
	if err != nil {
		return nil, false, fmt.Errorf("marshal interaction response: %w", err)
	}
	changed := false
	var interaction Interaction
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ? AND task_id = ?", trimmedInteractionID, trimmedTaskID).Take(&interaction).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("%w: %s", ErrInteractionNotFound, trimmedInteractionID)
			}
			return err
		}
		if interaction.Status != StatusPending {
			return nil
		}
		now := time.Now().UTC()
		if input.RespondedAt != nil {
			now = input.RespondedAt.UTC()
		}
		interaction.Status = input.Status
		interaction.ResponseJSON = responseJSON
		interaction.RespondedBy = strings.TrimSpace(input.RespondedBy)
		interaction.RespondedAt = &now
		interaction.UpdatedAt = now
		if err := tx.Save(&interaction).Error; err != nil {
			return err
		}
		changed = true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return &interaction, changed, nil
}

func marshalJSON(value any) ([]byte, error) {
	if value == nil {
		return nil, nil
	}
	return json.Marshal(value)
}

func newInteractionID() string {
	return "interaction_" + uuid.NewString()
}

func isSupportedKind(kind Kind) bool {
	switch kind {
	case KindApproval, KindQuestion:
		return true
	default:
		return false
	}
}

func isSupportedStatus(status Status) bool {
	switch status {
	case StatusPending, StatusApproved, StatusRejected, StatusResponded, StatusExpired, StatusCancelled:
		return true
	default:
		return false
	}
}
