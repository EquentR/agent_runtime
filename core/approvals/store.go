package approvals

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var ErrApprovalNotFound = errors.New("approval not found")

type Status string

const (
	StatusPending   Status = "pending"
	StatusApproved  Status = "approved"
	StatusRejected  Status = "rejected"
	StatusExpired   Status = "expired"
	StatusCancelled Status = "cancelled"
)

type Decision string

const (
	DecisionApprove Decision = "approve"
	DecisionReject  Decision = "reject"
)

type ToolApproval struct {
	ID                        string     `json:"id" gorm:"type:varchar(64);primaryKey"`
	TaskID                    string     `json:"task_id" gorm:"type:varchar(64);not null;index;uniqueIndex:idx_tool_approvals_task_tool_call"`
	ConversationID            string     `json:"conversation_id" gorm:"type:varchar(64);index"`
	StepIndex                 int        `json:"step_index" gorm:"not null;default:0"`
	ToolCallID                string     `json:"tool_call_id" gorm:"type:varchar(128);not null;index;uniqueIndex:idx_tool_approvals_task_tool_call"`
	ToolName                  string     `json:"tool_name" gorm:"type:varchar(128);not null;index"`
	ArgumentsSummary          string     `json:"arguments_summary" gorm:"type:text;not null;default:''"`
	RiskLevel                 string     `json:"risk_level" gorm:"type:varchar(32);not null;default:''"`
	Reason                    string     `json:"reason" gorm:"type:text;not null;default:''"`
	Status                    Status     `json:"status" gorm:"type:varchar(32);not null;index"`
	DecisionBy                string     `json:"decision_by" gorm:"type:varchar(128)"`
	DecisionReason            string     `json:"decision_reason" gorm:"type:text"`
	DecisionAt                *time.Time `json:"decision_at"`
	ExpiresAt                 *time.Time `json:"-"`
	RequestedEventPublishedAt *time.Time `json:"-" gorm:"index"`
	ResolvedEventPublishedAt  *time.Time `json:"-" gorm:"index"`
	FinalizedAt               *time.Time `json:"-" gorm:"index"`
	CreatedAt                 time.Time  `json:"created_at"`
	UpdatedAt                 time.Time  `json:"updated_at"`
}

func (ToolApproval) TableName() string {
	return "tool_approvals"
}

type CreateApprovalInput struct {
	TaskID           string
	ConversationID   string
	StepIndex        int
	ToolCallID       string
	ToolName         string
	ArgumentsSummary string
	RiskLevel        string
	Reason           string
	ExpiresAt        *time.Time
}

type ResolveApprovalInput struct {
	Decision   Decision
	Reason     string
	DecisionBy string
}

type Store struct {
	db *gorm.DB
}

func NewStore(db *gorm.DB) *Store {
	return &Store{db: db}
}

func (s *Store) AutoMigrate() error {
	if s == nil || s.db == nil {
		return fmt.Errorf("approval store db cannot be nil")
	}
	return s.db.AutoMigrate(&ToolApproval{})
}

func (s *Store) CreateApproval(ctx context.Context, input CreateApprovalInput) (*ToolApproval, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("approval store db cannot be nil")
	}
	approval := &ToolApproval{
		ID:               newApprovalID(),
		TaskID:           strings.TrimSpace(input.TaskID),
		ConversationID:   strings.TrimSpace(input.ConversationID),
		StepIndex:        input.StepIndex,
		ToolCallID:       strings.TrimSpace(input.ToolCallID),
		ToolName:         strings.TrimSpace(input.ToolName),
		ArgumentsSummary: strings.TrimSpace(input.ArgumentsSummary),
		RiskLevel:        strings.TrimSpace(input.RiskLevel),
		Reason:           strings.TrimSpace(input.Reason),
		Status:           StatusPending,
		ExpiresAt:        input.ExpiresAt,
	}
	if approval.TaskID == "" {
		return nil, fmt.Errorf("task id cannot be empty")
	}
	if approval.ToolCallID == "" {
		return nil, fmt.Errorf("tool call id cannot be empty")
	}
	if approval.ToolName == "" {
		return nil, fmt.Errorf("tool name cannot be empty")
	}
	if err := s.db.WithContext(ctx).Create(approval).Error; err != nil {
		if isUniqueConstraintError(err) {
			existing, findErr := s.FindApprovalByToolCall(ctx, approval.TaskID, approval.ToolCallID)
			if findErr != nil {
				return nil, findErr
			}
			if existing != nil {
				return existing, nil
			}
		}
		return nil, err
	}
	return approval, nil
}

func (s *Store) FindApprovalByToolCall(ctx context.Context, taskID string, toolCallID string) (*ToolApproval, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("approval store db cannot be nil")
	}
	trimmedTaskID := strings.TrimSpace(taskID)
	trimmedToolCallID := strings.TrimSpace(toolCallID)
	if trimmedTaskID == "" || trimmedToolCallID == "" {
		return nil, nil
	}
	var approval ToolApproval
	err := s.db.WithContext(ctx).
		Where("task_id = ? AND tool_call_id = ?", trimmedTaskID, trimmedToolCallID).
		Order("created_at desc").
		Order("id desc").
		Take(&approval).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &approval, nil
}

func (s *Store) GetApproval(ctx context.Context, taskID string, approvalID string) (*ToolApproval, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("approval store db cannot be nil")
	}
	trimmedApprovalID := strings.TrimSpace(approvalID)
	if trimmedApprovalID == "" {
		return nil, fmt.Errorf("approval id cannot be empty")
	}
	query := s.db.WithContext(ctx).Where("id = ?", trimmedApprovalID)
	if trimmedTaskID := strings.TrimSpace(taskID); trimmedTaskID != "" {
		query = query.Where("task_id = ?", trimmedTaskID)
	}
	var approval ToolApproval
	err := query.Take(&approval).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("%w: %s", ErrApprovalNotFound, trimmedApprovalID)
	}
	if err != nil {
		return nil, err
	}
	return &approval, nil
}

func (s *Store) ListTaskApprovals(ctx context.Context, taskID string) ([]ToolApproval, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("approval store db cannot be nil")
	}
	trimmedTaskID := strings.TrimSpace(taskID)
	if trimmedTaskID == "" {
		return nil, nil
	}
	var approvals []ToolApproval
	err := s.db.WithContext(ctx).
		Where("task_id = ?", trimmedTaskID).
		Order("created_at desc").
		Order("id desc").
		Find(&approvals).Error
	if err != nil {
		return nil, err
	}
	return approvals, nil
}

func (s *Store) ResolveApproval(ctx context.Context, taskID string, approvalID string, input ResolveApprovalInput) (*ToolApproval, bool, error) {
	if s == nil || s.db == nil {
		return nil, false, fmt.Errorf("approval store db cannot be nil")
	}
	trimmedTaskID := strings.TrimSpace(taskID)
	trimmedApprovalID := strings.TrimSpace(approvalID)
	if trimmedTaskID == "" {
		return nil, false, fmt.Errorf("task id cannot be empty")
	}
	if trimmedApprovalID == "" {
		return nil, false, fmt.Errorf("approval id cannot be empty")
	}
	status, err := statusFromDecision(input.Decision)
	if err != nil {
		return nil, false, err
	}

	var approval ToolApproval
	changed := false
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ? AND task_id = ?", trimmedApprovalID, trimmedTaskID).Take(&approval).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("%w: %s", ErrApprovalNotFound, trimmedApprovalID)
			}
			return err
		}
		if approval.Status != StatusPending {
			return nil
		}

		now := time.Now().UTC()
		approval.Status = status
		approval.DecisionBy = strings.TrimSpace(input.DecisionBy)
		approval.DecisionReason = strings.TrimSpace(input.Reason)
		approval.DecisionAt = &now
		approval.UpdatedAt = now
		if err := tx.Save(&approval).Error; err != nil {
			return err
		}
		changed = true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return &approval, changed, nil
}

func (s *Store) ExpireApproval(ctx context.Context, taskID string, approvalID string, reason string) (*ToolApproval, bool, error) {
	if s == nil || s.db == nil {
		return nil, false, fmt.Errorf("approval store db cannot be nil")
	}
	trimmedTaskID := strings.TrimSpace(taskID)
	trimmedApprovalID := strings.TrimSpace(approvalID)
	if trimmedTaskID == "" {
		return nil, false, fmt.Errorf("task id cannot be empty")
	}
	if trimmedApprovalID == "" {
		return nil, false, fmt.Errorf("approval id cannot be empty")
	}

	var approval ToolApproval
	changed := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ? AND task_id = ?", trimmedApprovalID, trimmedTaskID).Take(&approval).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("%w: %s", ErrApprovalNotFound, trimmedApprovalID)
			}
			return err
		}
		if approval.Status != StatusPending {
			return nil
		}

		now := time.Now().UTC()
		approval.Status = StatusExpired
		approval.DecisionReason = strings.TrimSpace(reason)
		approval.DecisionAt = &now
		approval.UpdatedAt = now
		if err := tx.Save(&approval).Error; err != nil {
			return err
		}
		changed = true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return &approval, changed, nil
}

func (s *Store) CancelPendingApprovalsByTask(ctx context.Context, taskID string) (int64, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("approval store db cannot be nil")
	}
	trimmedTaskID := strings.TrimSpace(taskID)
	if trimmedTaskID == "" {
		return 0, nil
	}
	now := time.Now().UTC()
	result := s.db.WithContext(ctx).Model(&ToolApproval{}).
		Where("task_id = ?", trimmedTaskID).
		Where("status = ?", StatusPending).
		Updates(map[string]any{
			"status":          StatusCancelled,
			"decision_reason": "task cancelled",
			"updated_at":      now,
		})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

func statusFromDecision(decision Decision) (Status, error) {
	switch decision {
	case DecisionApprove:
		return StatusApproved, nil
	case DecisionReject:
		return StatusRejected, nil
	default:
		return "", fmt.Errorf("unsupported approval decision %q", decision)
	}
}

func newApprovalID() string {
	return "approval_" + uuid.NewString()
}

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique constraint failed") || strings.Contains(message, "duplicate key")
}
