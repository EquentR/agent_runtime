package agent

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var ErrConversationNotFound = errors.New("conversation not found")

type Conversation struct {
	ID            string     `json:"id" gorm:"type:varchar(64);primaryKey"`
	ProviderID    string     `json:"provider_id" gorm:"type:varchar(128);not null;index"`
	ModelID       string     `json:"model_id" gorm:"type:varchar(128);not null"`
	Title         string     `json:"title" gorm:"type:varchar(255)"`
	LastMessage   string     `json:"last_message" gorm:"type:text"`
	MessageCount  int        `json:"message_count" gorm:"not null;default:0"`
	LastMessageAt *time.Time `json:"last_message_at,omitempty" gorm:"index"`
	CreatedBy     string     `json:"created_by" gorm:"type:varchar(128)"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

func (Conversation) TableName() string {
	return "conversations"
}

type ConversationMessage struct {
	ID             uint64          `json:"id" gorm:"primaryKey;autoIncrement"`
	ConversationID string          `json:"conversation_id" gorm:"type:varchar(64);not null;index:idx_conversation_seq,priority:1"`
	Seq            int64           `json:"seq" gorm:"not null;index:idx_conversation_seq,priority:2"`
	Role           string          `json:"role" gorm:"type:varchar(32);not null"`
	Content        string          `json:"content" gorm:"type:text"`
	MessageJSON    json.RawMessage `json:"message" gorm:"column:message_json;type:blob;not null"`
	TaskID         string          `json:"task_id" gorm:"type:varchar(64);index"`
	CreatedAt      time.Time       `json:"created_at"`
}

const persistedConversationMessageVersion = "v1"

type persistedConversationMessage struct {
	Version string        `json:"version"`
	Message model.Message `json:"message"`
}

func (ConversationMessage) TableName() string {
	return "conversation_messages"
}

type CreateConversationInput struct {
	ID         string
	ProviderID string
	ModelID    string
	Title      string
	CreatedBy  string
}

type EnsureConversationInput struct {
	ID         string
	ProviderID string
	ModelID    string
	Title      string
	CreatedBy  string
}

type ConversationStore struct {
	db *gorm.DB
}

func NewConversationStore(db *gorm.DB) *ConversationStore {
	return &ConversationStore{db: db}
}

func (s *ConversationStore) AutoMigrate() error {
	if s == nil || s.db == nil {
		return errors.New("conversation store db is required")
	}
	return s.db.AutoMigrate(&Conversation{}, &ConversationMessage{})
}

func (s *ConversationStore) GetConversation(ctx context.Context, id string) (*Conversation, error) {
	var conversation Conversation
	err := s.db.WithContext(ctx).First(&conversation, "id = ?", strings.TrimSpace(id)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrConversationNotFound
	}
	if err != nil {
		return nil, err
	}
	return &conversation, nil
}

func (s *ConversationStore) ListConversations(ctx context.Context) ([]Conversation, error) {
	var conversations []Conversation
	if err := s.db.WithContext(ctx).Order("last_message_at desc").Order("updated_at desc").Order("created_at desc").Order("id desc").Find(&conversations).Error; err != nil {
		return nil, err
	}
	return conversations, nil
}

func (s *ConversationStore) CreateConversation(ctx context.Context, input CreateConversationInput) (*Conversation, error) {
	conversationID := strings.TrimSpace(input.ID)
	if conversationID == "" {
		conversationID = "conv_" + uuid.NewString()
	}
	conversation := &Conversation{
		ID:         conversationID,
		ProviderID: strings.TrimSpace(input.ProviderID),
		ModelID:    strings.TrimSpace(input.ModelID),
		Title:      strings.TrimSpace(input.Title),
		CreatedBy:  strings.TrimSpace(input.CreatedBy),
	}
	if err := s.db.WithContext(ctx).Create(conversation).Error; err != nil {
		return nil, err
	}
	return conversation, nil
}

func (s *ConversationStore) EnsureConversation(ctx context.Context, input EnsureConversationInput) (*Conversation, error) {
	if strings.TrimSpace(input.ID) == "" {
		return s.CreateConversation(ctx, CreateConversationInput(input))
	}
	conversation, err := s.GetConversation(ctx, input.ID)
	if err == nil {
		return conversation, nil
	}
	if !errors.Is(err, ErrConversationNotFound) {
		return nil, err
	}
	return s.CreateConversation(ctx, CreateConversationInput(input))
}

func (s *ConversationStore) AppendMessages(ctx context.Context, conversationID string, taskID string, messages []model.Message) error {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" || len(messages) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		conversation, err := s.getConversationTx(tx, conversationID)
		if err != nil {
			return err
		}
		var last ConversationMessage
		seq := int64(0)
		err = tx.Where("conversation_id = ?", conversationID).Order("seq desc").Take(&last).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err == nil {
			seq = last.Seq
		}
		now := time.Now().UTC()
		for _, message := range messages {
			seq++
			if conversation.Title == "" && message.Role == model.RoleUser {
				conversation.Title = summarizeConversationText(message.Content, 40)
			}
			if summary := summarizeConversationText(message.Content, 120); summary != "" {
				conversation.LastMessage = summary
				conversation.LastMessageAt = &now
			}
			conversation.MessageCount++
			raw, err := json.Marshal(cloneMessage(message))
			if err == nil {
				raw, err = json.Marshal(persistedConversationMessage{
					Version: persistedConversationMessageVersion,
					Message: cloneMessage(message),
				})
			}
			if err != nil {
				return err
			}
			record := ConversationMessage{
				ConversationID: conversationID,
				Seq:            seq,
				Role:           message.Role,
				Content:        message.Content,
				MessageJSON:    raw,
				TaskID:         strings.TrimSpace(taskID),
			}
			if err := tx.Create(&record).Error; err != nil {
				return err
			}
		}
		conversation.UpdatedAt = now
		return tx.Save(conversation).Error
	})
}

func (s *ConversationStore) ListMessages(ctx context.Context, conversationID string) ([]model.Message, error) {
	var records []ConversationMessage
	if err := s.db.WithContext(ctx).Where("conversation_id = ?", strings.TrimSpace(conversationID)).Order("seq asc").Find(&records).Error; err != nil {
		return nil, err
	}
	messages := make([]model.Message, 0, len(records))
	for _, record := range records {
		message, err := decodePersistedConversationMessage(record.MessageJSON)
		if err != nil {
			return nil, err
		}
		messages = append(messages, cloneMessage(message))
	}
	return messages, nil
}

func decodePersistedConversationMessage(raw json.RawMessage) (model.Message, error) {
	var envelope persistedConversationMessage
	if err := json.Unmarshal(raw, &envelope); err == nil && envelope.Version != "" {
		return cloneMessage(envelope.Message), nil
	}

	var legacy model.Message
	if err := json.Unmarshal(raw, &legacy); err != nil {
		return model.Message{}, err
	}
	return cloneMessage(legacy), nil
}

func (s *ConversationStore) getConversationTx(tx *gorm.DB, id string) (*Conversation, error) {
	var conversation Conversation
	err := tx.First(&conversation, "id = ?", strings.TrimSpace(id)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrConversationNotFound
	}
	if err != nil {
		return nil, err
	}
	return &conversation, nil
}

var conversationWhitespacePattern = regexp.MustCompile(`\s+`)

func summarizeConversationText(text string, maxRunes int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	text = conversationWhitespacePattern.ReplaceAllString(text, " ")
	if maxRunes <= 0 || utf8.RuneCountInString(text) <= maxRunes {
		return text
	}
	runes := []rune(text)
	return strings.TrimSpace(string(runes[:maxRunes]))
}
