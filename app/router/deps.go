package router

import (
	"context"
	"time"

	"github.com/EquentR/agent_runtime/app/logics"
	coreagent "github.com/EquentR/agent_runtime/core/agent"
	"github.com/EquentR/agent_runtime/core/approvals"
	"github.com/EquentR/agent_runtime/core/attachments"
	coreaudit "github.com/EquentR/agent_runtime/core/audit"
	"github.com/EquentR/agent_runtime/core/interactions"
	coreprompt "github.com/EquentR/agent_runtime/core/prompt"
	coreskills "github.com/EquentR/agent_runtime/core/skills"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	"github.com/EquentR/agent_runtime/pkg/mail"
	"gorm.io/gorm"
)

// Dependencies 汇总路由层需要的跨模块依赖。
type Dependencies struct {
	TaskManager        *coretasks.Manager
	ApprovalStore      *approvals.Store
	AttachmentStore    *attachments.Store
	AttachmentStorage  attachments.Storage
	AttachmentDraftTTL time.Duration
	InteractionStore   *interactions.Store
	ConversationStore  *coreagent.ConversationStore
	AuditStore         *coreaudit.Store
	ModelResolver      *coreagent.ModelResolver
	ModelLogic         *logics.ModelLogic
	ModelTester        ModelTester
	PromptStore        *coreprompt.Store
	PromptResolver     *coreprompt.Resolver
	SkillLoader        *coreskills.Loader
	AuthLogic          *logics.AuthLogic
	UserDB             *gorm.DB
	AuthSettings       *logics.SettingsLogic
	EmailVerification  *logics.EmailVerificationLogic
	TurnstileVerifier  logics.TurnstileVerifier
	AdminAuditLogic    *logics.AdminAuditLogic
	AdminSMTPTester    AdminSMTPTester
}

type AdminSMTPTester interface {
	Send(ctx context.Context, message mail.Message) error
}

type ModelTester interface {
	TestModel(ctx context.Context, resolved *coreagent.ResolvedModel) error
}
