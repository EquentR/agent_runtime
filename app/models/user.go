package models

import "time"

type User struct {
	ID           uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	Username     string    `json:"username" gorm:"type:varchar(128);not null;uniqueIndex"`
	PasswordHash string    `json:"-" gorm:"type:varchar(255);not null"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
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
