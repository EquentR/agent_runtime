package logics

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/EquentR/agent_runtime/app/models"
	"github.com/EquentR/agent_runtime/pkg/secret"
	"gorm.io/gorm"
)

type AdminAuditLogic struct {
	db *gorm.DB
}

type RecordAdminAuditInput struct {
	Actor      models.User
	TargetKind string
	TargetID   string
	Action     string
	Before     any
	After      any
	IPAddress  string
	UserAgent  string
}

type AdminAuditFilter struct {
	ActorID       uint64
	ActorUsername string
	TargetKind    string
	TargetID      string
	Action        string
	CreatedAfter  *time.Time
	CreatedBefore *time.Time
	Limit         int
}

func NewAdminAuditLogic(db *gorm.DB) *AdminAuditLogic {
	return &AdminAuditLogic{db: db}
}

func (l *AdminAuditLogic) WithDB(db *gorm.DB) *AdminAuditLogic {
	if l == nil {
		return NewAdminAuditLogic(db)
	}
	clone := *l
	clone.db = db
	return &clone
}

func (l *AdminAuditLogic) Record(ctx context.Context, input RecordAdminAuditInput) error {
	if l == nil || l.db == nil {
		return fmt.Errorf("admin audit db is required")
	}
	targetKind := strings.TrimSpace(input.TargetKind)
	if targetKind == "" {
		return fmt.Errorf("admin audit target kind is required")
	}
	targetID := strings.TrimSpace(input.TargetID)
	if targetID == "" {
		return fmt.Errorf("admin audit target id is required")
	}
	action := strings.TrimSpace(input.Action)
	if action == "" {
		return fmt.Errorf("admin audit action is required")
	}

	beforeJSON, err := marshalAdminAuditSnapshot(input.Before)
	if err != nil {
		return fmt.Errorf("encode admin audit before snapshot: %w", err)
	}
	afterJSON, err := marshalAdminAuditSnapshot(input.After)
	if err != nil {
		return fmt.Errorf("encode admin audit after snapshot: %w", err)
	}

	event := models.AdminAuditEvent{
		ActorID:       input.Actor.ID,
		ActorUsername: strings.TrimSpace(input.Actor.Username),
		ActorEmail:    strings.TrimSpace(input.Actor.Email),
		TargetKind:    targetKind,
		TargetID:      targetID,
		Action:        action,
		BeforeJSON:    beforeJSON,
		AfterJSON:     afterJSON,
		IPAddress:     strings.TrimSpace(input.IPAddress),
		UserAgent:     strings.TrimSpace(input.UserAgent),
		CreatedAt:     time.Now().UTC(),
	}
	return l.db.WithContext(ctx).Create(&event).Error
}

func (l *AdminAuditLogic) List(ctx context.Context, filter AdminAuditFilter) ([]models.AdminAuditEvent, error) {
	if l == nil || l.db == nil {
		return nil, fmt.Errorf("admin audit db is required")
	}
	query := l.db.WithContext(ctx).Model(&models.AdminAuditEvent{})
	if filter.ActorID != 0 {
		query = query.Where("actor_id = ?", filter.ActorID)
	}
	if actorUsername := strings.TrimSpace(filter.ActorUsername); actorUsername != "" {
		query = query.Where("actor_username = ?", actorUsername)
	}
	if targetKind := strings.TrimSpace(filter.TargetKind); targetKind != "" {
		query = query.Where("target_kind = ?", targetKind)
	}
	if targetID := strings.TrimSpace(filter.TargetID); targetID != "" {
		query = query.Where("target_id = ?", targetID)
	}
	if action := strings.TrimSpace(filter.Action); action != "" {
		query = query.Where("action = ?", action)
	}
	if filter.CreatedAfter != nil {
		query = query.Where("created_at >= ?", filter.CreatedAfter.UTC())
	}
	if filter.CreatedBefore != nil {
		query = query.Where("created_at < ?", filter.CreatedBefore.UTC())
	}
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var events []models.AdminAuditEvent
	if err := query.Order("created_at desc").Order("id desc").Limit(limit).Find(&events).Error; err != nil {
		return nil, err
	}
	return events, nil
}

func marshalAdminAuditSnapshot(value any) ([]byte, error) {
	if value == nil {
		return nil, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, err
	}
	return json.Marshal(sanitizeAdminAuditValue("", decoded))
}

func sanitizeAdminAuditValue(key string, value any) any {
	if isAdminAuditSensitiveKey(key) {
		if text, ok := value.(string); ok && text != "" {
			return secret.MaskSecret(text)
		}
		if value == nil {
			return nil
		}
		return "****"
	}
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for childKey, childValue := range typed {
			result[childKey] = sanitizeAdminAuditValue(childKey, childValue)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for idx, item := range typed {
			result[idx] = sanitizeAdminAuditValue("", item)
		}
		return result
	default:
		return value
	}
}

func isAdminAuditSensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	if normalized == "" {
		return false
	}
	compact := strings.NewReplacer("_", "", "-", "", " ", "").Replace(normalized)
	return strings.Contains(compact, "password") ||
		strings.Contains(compact, "secret") ||
		strings.Contains(compact, "apikey")
}
