package models

import "time"

type SystemSetting struct {
	Key       string    `json:"key" gorm:"type:varchar(128);primaryKey"`
	ValueJSON []byte    `json:"value_json" gorm:"type:blob;not null"`
	Encrypted bool      `json:"encrypted" gorm:"not null;default:false"`
	UpdatedBy string    `json:"updated_by" gorm:"type:varchar(128);index"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (SystemSetting) TableName() string {
	return "system_settings"
}

type EmailVerification struct {
	ID          string     `json:"id" gorm:"type:varchar(128);primaryKey"`
	UserID      uint64     `json:"user_id" gorm:"not null;index"`
	Email       string     `json:"email" gorm:"type:varchar(255);not null;index"`
	Purpose     string     `json:"purpose" gorm:"type:varchar(64);not null;index"`
	CodeHash    string     `json:"-" gorm:"type:varchar(255);not null"`
	Attempts    int        `json:"attempts" gorm:"not null;default:0"`
	MaxAttempts int        `json:"max_attempts" gorm:"not null;default:5"`
	ExpiresAt   time.Time  `json:"expires_at" gorm:"not null;index"`
	LastSentAt  time.Time  `json:"last_sent_at" gorm:"not null"`
	ConsumedAt  *time.Time `json:"consumed_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (EmailVerification) TableName() string {
	return "email_verifications"
}

type AdminAuditEvent struct {
	ID            uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	ActorID       uint64    `json:"actor_id" gorm:"index"`
	ActorUsername string    `json:"actor_username" gorm:"type:varchar(128);index"`
	TargetKind    string    `json:"target_kind" gorm:"type:varchar(64);not null;index"`
	TargetID      string    `json:"target_id" gorm:"type:varchar(128);not null;index"`
	Action        string    `json:"action" gorm:"type:varchar(128);not null;index"`
	BeforeJSON    []byte    `json:"before_json" gorm:"type:blob"`
	AfterJSON     []byte    `json:"after_json" gorm:"type:blob"`
	IPAddress     string    `json:"ip_address" gorm:"type:varchar(64)"`
	UserAgent     string    `json:"user_agent" gorm:"type:varchar(255)"`
	CreatedAt     time.Time `json:"created_at"`
}

func (AdminAuditEvent) TableName() string {
	return "admin_audit_events"
}

type LLMModelOverride struct {
	ID         uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	ProviderID string    `json:"provider_id" gorm:"type:varchar(128);not null;uniqueIndex:idx_llm_model_override"`
	ModelID    string    `json:"model_id" gorm:"type:varchar(128);not null;uniqueIndex:idx_llm_model_override"`
	Enabled    bool      `json:"enabled" gorm:"not null;default:true"`
	Scope      string    `json:"scope" gorm:"type:varchar(32);not null;default:admin;index"`
	UpdatedBy  string    `json:"updated_by" gorm:"type:varchar(128)"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (LLMModelOverride) TableName() string {
	return "llm_model_overrides"
}

type CustomLLMModel struct {
	ID               string    `json:"id" gorm:"type:varchar(128);primaryKey"`
	OwnerUserID      uint64    `json:"owner_user_id" gorm:"not null;index"`
	ProviderID       string    `json:"provider_id" gorm:"type:varchar(128);not null;index:idx_custom_model_owner_provider,unique"`
	ModelID          string    `json:"model_id" gorm:"type:varchar(128);not null"`
	DisplayName      string    `json:"display_name" gorm:"type:varchar(128);not null"`
	ProviderType     string    `json:"provider_type" gorm:"type:varchar(64);not null"`
	BaseURL          string    `json:"base_url" gorm:"type:varchar(512)"`
	EncryptedAPIKey  string    `json:"-" gorm:"type:text;not null"`
	Scope            string    `json:"scope" gorm:"type:varchar(32);not null;default:owner;index"`
	Enabled          bool      `json:"enabled" gorm:"not null;default:true"`
	ContextMaxTokens int64     `json:"context_max_tokens" gorm:"not null"`
	CapabilitiesJSON []byte    `json:"capabilities_json" gorm:"type:blob"`
	CostJSON         []byte    `json:"cost_json" gorm:"type:blob"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func (CustomLLMModel) TableName() string {
	return "custom_llm_models"
}
