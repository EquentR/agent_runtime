package memory

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const defaultLongTermInstruction = "你负责将用户跨会话真正值得记住的信息整理成长期记忆。请只保留稳定偏好、长期约束、持续有效的背景事实，以及用户明确要求记忆的内容；删除一次性任务细节、临时状态、寒暄、冗长过程和模型推理。输出半结构化中文摘要，只在有内容时输出以下章节：User Preferences、User Constraints、Persistent Facts、Explicit Memory。不要输出代码块。"

var ErrEmptyUserID = errors.New("memory user id is required")

type LongTermCompressionRequest struct {
	UserID          string
	PreviousSummary string
	Messages        []model.Message
	Instruction     string
}

type LongTermCompressor func(ctx context.Context, request LongTermCompressionRequest) (string, error)

type LongTermFlushRequest struct {
	UserID      string
	Messages    []model.Message
	Instruction string
}

type LongTermOptions struct {
	Compressor  LongTermCompressor
	Instruction string
}

type LongTermMemory struct {
	ID        uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	UserID    string    `json:"user_id" gorm:"column:user_id;type:varchar(128);not null;uniqueIndex"`
	Summary   string    `json:"summary" gorm:"type:text;not null;default:''"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (LongTermMemory) TableName() string {
	return "long_term_memories"
}

type LongTermManager struct {
	mu          sync.Mutex
	db          *gorm.DB
	compressor  LongTermCompressor
	instruction string
}

func NewLongTermManager(db *gorm.DB, options LongTermOptions) (*LongTermManager, error) {
	if db == nil {
		return nil, errors.New("memory db is required")
	}

	compressor := options.Compressor
	if compressor == nil {
		compressor = newDefaultLongTermCompressor()
	}

	instruction := strings.TrimSpace(options.Instruction)
	if instruction == "" {
		instruction = defaultLongTermInstruction
	}

	return &LongTermManager{
		db:          db,
		compressor:  compressor,
		instruction: instruction,
	}, nil
}

func (m *LongTermManager) GetSummary(ctx context.Context, userID string) (string, error) {
	record, err := m.getOrCreate(ctx, userID)
	if err != nil {
		return "", err
	}
	return record.Summary, nil
}

func (m *LongTermManager) Flush(ctx context.Context, request LongTermFlushRequest) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	record, err := m.getOrCreate(ctx, request.UserID)
	if err != nil {
		return "", err
	}
	if len(request.Messages) == 0 {
		return record.Summary, nil
	}

	instruction := strings.TrimSpace(request.Instruction)
	if instruction == "" {
		instruction = m.instruction
	}

	summary, err := m.compressor(ctx, LongTermCompressionRequest{
		UserID:          strings.TrimSpace(request.UserID),
		PreviousSummary: record.Summary,
		Messages:        cloneMessages(request.Messages),
		Instruction:     instruction,
	})
	if err != nil {
		return "", err
	}
	summary = strings.TrimSpace(summary)

	if err := m.db.WithContext(ctx).
		Model(&LongTermMemory{}).
		Where("user_id = ?", record.UserID).
		Update("summary", summary).Error; err != nil {
		return "", err
	}
	return summary, nil
}

func (m *LongTermManager) getOrCreate(ctx context.Context, userID string) (*LongTermMemory, error) {
	normalized := strings.TrimSpace(userID)
	if normalized == "" {
		return nil, ErrEmptyUserID
	}

	seed := &LongTermMemory{UserID: normalized, Summary: ""}
	if err := m.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}},
			DoNothing: true,
		}).
		Create(seed).Error; err != nil {
		return nil, err
	}

	record := &LongTermMemory{}
	if err := m.db.WithContext(ctx).Where("user_id = ?", normalized).First(record).Error; err != nil {
		return nil, err
	}
	return record, nil
}

func newDefaultLongTermCompressor() LongTermCompressor {
	return func(_ context.Context, request LongTermCompressionRequest) (string, error) {
		sections := make([]string, 0, len(request.Messages)+1)
		if summary := compactWhitespace(request.PreviousSummary); summary != "" {
			sections = append(sections, summary)
		}
		for _, message := range request.Messages {
			if compact := summarizeMessage(message); compact != "" {
				sections = append(sections, compact)
			}
		}
		return strings.Join(sections, "\n"), nil
	}
}
