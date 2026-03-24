package prompt

import "time"

type PromptDocument struct {
	ID          string    `json:"id" gorm:"type:varchar(64);primaryKey"`
	Name        string    `json:"name" gorm:"type:varchar(255);not null;index"`
	Description string    `json:"description" gorm:"type:text"`
	Content     string    `json:"content" gorm:"type:text;not null"`
	Scope       string    `json:"scope" gorm:"type:varchar(64);not null;index"`
	Status      string    `json:"status" gorm:"type:varchar(32);not null;default:active;index"`
	CreatedBy   string    `json:"created_by" gorm:"type:varchar(128)"`
	UpdatedBy   string    `json:"updated_by" gorm:"type:varchar(128)"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (PromptDocument) TableName() string {
	return "prompt_documents"
}

type PromptBinding struct {
	ID         uint64          `json:"id" gorm:"primaryKey;autoIncrement"`
	PromptID   string          `json:"prompt_id" gorm:"type:varchar(64);not null;index"`
	Prompt     *PromptDocument `json:"-" gorm:"foreignKey:PromptID;references:ID"`
	Scene      string          `json:"scene" gorm:"type:varchar(128);not null;index:idx_prompt_binding_scene_phase,priority:1"`
	Phase      string          `json:"phase" gorm:"type:varchar(64);not null;index:idx_prompt_binding_scene_phase,priority:2"`
	IsDefault  bool            `json:"is_default" gorm:"not null;default:false;index"`
	Priority   int             `json:"priority" gorm:"not null;default:0;index"`
	ProviderID string          `json:"provider_id,omitempty" gorm:"type:varchar(128);index"`
	ModelID    string          `json:"model_id,omitempty" gorm:"type:varchar(128);index"`
	Status     string          `json:"status" gorm:"type:varchar(32);not null;default:active;index"`
	CreatedBy  string          `json:"created_by" gorm:"type:varchar(128)"`
	UpdatedBy  string          `json:"updated_by" gorm:"type:varchar(128)"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

func (PromptBinding) TableName() string {
	return "prompt_bindings"
}
