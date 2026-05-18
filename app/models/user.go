package models

import "time"

const (
	UserRoleAdmin = "admin"
	UserRoleUser  = "user"
)

const (
	UserStatusPendingEmailVerification = "pending_email_verification"
	UserStatusActive                   = "active"
	UserStatusDisabled                 = "disabled"
	UserStatusNeedsEmailBinding        = "needs_email_binding"
)

type User struct {
	ID                  uint64     `json:"id" gorm:"primaryKey;autoIncrement"`
	Username            string     `json:"username" gorm:"type:varchar(128);not null;uniqueIndex"`
	PasswordHash        string     `json:"-" gorm:"type:varchar(255);not null"`
	Role                string     `json:"role" gorm:"type:varchar(32);not null;default:user"`
	Email               string     `json:"email" gorm:"type:varchar(255);uniqueIndex:,where:email <> ''"`
	DisplayName         string     `json:"display_name" gorm:"type:varchar(128)"`
	Status              string     `json:"status" gorm:"type:varchar(64);not null;default:active;index"`
	EmailVerifiedAt     *time.Time `json:"email_verified_at"`
	ForcePasswordChange bool       `json:"force_password_change" gorm:"not null;default:false"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

func (User) TableName() string {
	return "users"
}

type UserSession struct {
	ID        string    `json:"id" gorm:"type:varchar(128);primaryKey"`
	UserID    uint64    `json:"user_id" gorm:"not null;index"`
	Username  string    `json:"username" gorm:"type:varchar(128);not null;index"`
	ExpiresAt time.Time `json:"expires_at" gorm:"not null;index"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (UserSession) TableName() string {
	return "user_sessions"
}
