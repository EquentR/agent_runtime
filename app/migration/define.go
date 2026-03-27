package migration

import (
	"github.com/EquentR/agent_runtime/app/models"
	"github.com/EquentR/agent_runtime/core/agent"
	"github.com/EquentR/agent_runtime/core/audit"
	"github.com/EquentR/agent_runtime/core/memory"
	"github.com/EquentR/agent_runtime/core/prompt"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	"github.com/EquentR/agent_runtime/pkg/migrate"
	"gorm.io/gorm"
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

func init() {
	versionMigrations = append(versionMigrations, to008)
	versionMigrations = append(versionMigrations, to009)
}
