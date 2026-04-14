package migration

import (
	"encoding/json"
	"time"

	"github.com/EquentR/agent_runtime/app/models"
	"github.com/EquentR/agent_runtime/core/agent"
	"github.com/EquentR/agent_runtime/core/approvals"
	"github.com/EquentR/agent_runtime/core/attachments"
	"github.com/EquentR/agent_runtime/core/audit"
	"github.com/EquentR/agent_runtime/core/interactions"
	"github.com/EquentR/agent_runtime/core/memory"
	"github.com/EquentR/agent_runtime/core/prompt"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	"github.com/EquentR/agent_runtime/pkg/migrate"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// to001 初始化迁移，创建数据版本表
var to001 = migrate.NewMigration("0.0.1", func(tx *gorm.DB) error {
	err := tx.AutoMigrate(&migrate.DataVersion{})
	if err != nil {
		return err
	}
	return nil
})

// to002 创建任务快照表与事件流表，为后台任务管理器提供持久化存储。
var to002 = migrate.NewMigration("0.0.2", func(tx *gorm.DB) error {
	return tx.AutoMigrate(&coretasks.Task{}, &coretasks.TaskEvent{})
})

// to003 创建长期记忆表，按 user_id 隔离一条用户摘要记录。
var to003 = migrate.NewMigration("0.0.3", func(tx *gorm.DB) error {
	return tx.AutoMigrate(&memory.LongTermMemory{})
})

// to004 创建 conversation/session 持久化表，为多轮 agent 对话提供历史重载。
var to004 = migrate.NewMigration("0.0.4", func(tx *gorm.DB) error {
	return tx.AutoMigrate(&agent.Conversation{}, &agent.ConversationMessage{})
})

// to005 创建用户和 session 表，为登录注册与 cookie session 提供持久化支持。
var to005 = migrate.NewMigration("0.0.5", func(tx *gorm.DB) error {
	return tx.AutoMigrate(&models.User{}, &models.UserSession{})
})

// to006 创建审计运行、事件与产物表，为回放 MVP 提供持久化证据。
var to006 = migrate.NewMigration("0.0.6", func(tx *gorm.DB) error {
	return tx.AutoMigrate(&audit.Run{}, &audit.Event{}, &audit.Artifact{})
})

// to007 为用户补齐 role 字段，并将存量首个用户回填为管理员。
var to007 = migrate.NewMigration("0.0.7", func(tx *gorm.DB) error {
	if err := tx.AutoMigrate(&models.User{}, &models.UserSession{}); err != nil {
		return err
	}

	var users []models.User
	if err := tx.Order("id asc").Find(&users).Error; err != nil {
		return err
	}
	if len(users) == 0 {
		return nil
	}

	adminFound := false
	for _, user := range users {
		if user.Role == models.UserRoleAdmin {
			adminFound = true
			break
		}
	}
	if adminFound {
		return nil
	}

	if err := tx.Model(&models.User{}).
		Where("id = ?", users[0].ID).
		Updates(map[string]any{"role": models.UserRoleAdmin}).Error; err != nil {
		return err
	}
	if len(users) == 1 {
		return nil
	}

	return tx.Model(&models.User{}).
		Where("role = ? OR role = ''", models.UserRoleAdmin).
		Where("id <> ?", users[0].ID).
		Update("role", models.UserRoleUser).Error
})

// to008 创建 prompt 文档与绑定表，为提示词管理与运行时注入提供持久化支持。
var to008 = migrate.NewMigration("0.0.8", func(tx *gorm.DB) error {
	return tx.AutoMigrate(&prompt.PromptDocument{}, &prompt.PromptBinding{})
})

// to009 为旧版 tasks 表补齐 concurrency_key 列，兼容已部署 SQLite 数据库。
var to009 = migrate.NewMigration("0.0.9", func(tx *gorm.DB) error {
	return tx.AutoMigrate(&coretasks.Task{})
})

// to010 创建工具审批表，为 tool approval 模式提供持久化支持。
var to010 = migrate.NewMigration("0.1.0", func(tx *gorm.DB) error {
	return tx.AutoMigrate(&approvals.ToolApproval{})
})

// to011 创建统一 interaction 表，并从既有工具审批记录回填兼容数据。
var to011 = migrate.NewMigration("0.1.1", func(tx *gorm.DB) error {
	if err := tx.AutoMigrate(&interactions.Interaction{}); err != nil {
		return err
	}
	if !tx.Migrator().HasTable(&approvals.ToolApproval{}) {
		return nil
	}

	var existingApprovals []approvals.ToolApproval
	if err := tx.Order("created_at asc").Order("id asc").Find(&existingApprovals).Error; err != nil {
		return err
	}
	if len(existingApprovals) == 0 {
		return nil
	}

	backfilled := make([]interactions.Interaction, 0, len(existingApprovals))
	for _, approval := range existingApprovals {
		requestJSON, err := buildApprovalInteractionRequestJSON(approval)
		if err != nil {
			return err
		}
		responseJSON, respondedBy, respondedAt, err := buildApprovalInteractionResponse(approval)
		if err != nil {
			return err
		}
		backfilled = append(backfilled, interactions.Interaction{
			ID:             approval.ID,
			TaskID:         approval.TaskID,
			ConversationID: approval.ConversationID,
			StepIndex:      approval.StepIndex,
			ToolCallID:     approval.ToolCallID,
			Kind:           interactions.KindApproval,
			Status:         interactions.Status(approval.Status),
			RequestJSON:    requestJSON,
			ResponseJSON:   responseJSON,
			RespondedBy:    respondedBy,
			RespondedAt:    respondedAt,
			CreatedAt:      approval.CreatedAt,
			UpdatedAt:      approval.UpdatedAt,
		})
	}

	return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&backfilled).Error
})

// to012 为 conversations 表补齐 memory_summary 列，支持跨 task 记忆摘要持久化。
var to012 = migrate.NewMigration("0.1.2", func(tx *gorm.DB) error {
	return tx.AutoMigrate(&agent.Conversation{})
})

// to013 为 conversations 表补齐结构化 memory snapshot 列，支持 reload 使用后端权威值。
var to013 = migrate.NewMigration("0.1.3", func(tx *gorm.DB) error {
	return tx.AutoMigrate(&agent.Conversation{})
})

// to014 创建 conversation_attachments 表，为附件元数据与存储引用提供持久化支持。
var to014 = migrate.NewMigration("0.1.4", func(tx *gorm.DB) error {
	return tx.AutoMigrate(&attachments.Attachment{})
})

type approvalInteractionRequest struct {
	ToolName         string     `json:"tool_name"`
	ArgumentsSummary string     `json:"arguments_summary"`
	RiskLevel        string     `json:"risk_level,omitempty"`
	Reason           string     `json:"reason,omitempty"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
}

type approvalInteractionResponse struct {
	Decision string `json:"decision,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

func buildApprovalInteractionRequestJSON(approval approvals.ToolApproval) ([]byte, error) {
	return json.Marshal(approvalInteractionRequest{
		ToolName:         approval.ToolName,
		ArgumentsSummary: approval.ArgumentsSummary,
		RiskLevel:        approval.RiskLevel,
		Reason:           approval.Reason,
		ExpiresAt:        approval.ExpiresAt,
	})
}

func buildApprovalInteractionResponse(approval approvals.ToolApproval) ([]byte, string, *time.Time, error) {
	response := approvalInteractionResponse{Reason: approval.DecisionReason}
	switch approval.Status {
	case approvals.StatusApproved:
		response.Decision = string(approvals.DecisionApprove)
	case approvals.StatusRejected:
		response.Decision = string(approvals.DecisionReject)
	}
	if response.Decision == "" && response.Reason == "" {
		return nil, "", approval.DecisionAt, nil
	}
	data, err := json.Marshal(response)
	if err != nil {
		return nil, "", nil, err
	}
	return data, approval.DecisionBy, approval.DecisionAt, nil
}
