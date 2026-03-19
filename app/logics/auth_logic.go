package logics

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/EquentR/agent_runtime/app/models"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const DefaultAuthSessionCookieName = "agent_runtime_session"

var (
	ErrInvalidCredentials = errors.New("用户名或密码错误")
	ErrUsernameTaken      = errors.New("用户名已存在")
	ErrUnauthorized       = errors.New("未登录或会话已失效")
)

type AuthConfig struct {
	CookieName string
	SessionTTL time.Duration
}

type AuthLogic struct {
	db         *gorm.DB
	cookieName string
	sessionTTL time.Duration
	now        func() time.Time
}

func NewAuthLogic(db *gorm.DB, cfg AuthConfig) (*AuthLogic, error) {
	if db == nil {
		return nil, fmt.Errorf("auth db is required")
	}
	cookieName := strings.TrimSpace(cfg.CookieName)
	if cookieName == "" {
		cookieName = DefaultAuthSessionCookieName
	}
	ttl := cfg.SessionTTL
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	return &AuthLogic{db: db, cookieName: cookieName, sessionTTL: ttl, now: time.Now}, nil
}

func (l *AuthLogic) AutoMigrate() error {
	return l.db.AutoMigrate(&models.User{}, &models.UserSession{})
}

func (l *AuthLogic) CookieName() string {
	return l.cookieName
}

func (l *AuthLogic) SessionTTL() time.Duration {
	return l.sessionTTL
}

func (l *AuthLogic) Register(ctx context.Context, username, password, confirmPassword string) (*models.User, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, fmt.Errorf("用户名不能为空")
	}
	if len(password) < 6 {
		return nil, fmt.Errorf("密码至少需要 6 位")
	}
	if password != confirmPassword {
		return nil, fmt.Errorf("两次输入的密码不一致")
	}

	var existing models.User
	err := l.db.WithContext(ctx).Where("username = ?", username).Take(&existing).Error
	if err == nil {
		return nil, ErrUsernameTaken
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	user := &models.User{Username: username, PasswordHash: string(hash)}
	if err := l.db.WithContext(ctx).Create(user).Error; err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, ErrUsernameTaken
		}
		return nil, err
	}
	return user, nil
}

func (l *AuthLogic) Login(ctx context.Context, username, password string) (*models.User, *models.UserSession, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return nil, nil, ErrInvalidCredentials
	}

	var user models.User
	if err := l.db.WithContext(ctx).Where("username = ?", username).Take(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrInvalidCredentials
		}
		return nil, nil, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, nil, ErrInvalidCredentials
	}

	session := &models.UserSession{
		ID:        "sess_" + uuid.NewString(),
		UserID:    user.ID,
		Username:  user.Username,
		ExpiresAt: l.now().UTC().Add(l.sessionTTL),
	}
	if err := l.db.WithContext(ctx).Create(session).Error; err != nil {
		return nil, nil, err
	}
	return &user, session, nil
}

func (l *AuthLogic) CurrentUser(ctx context.Context, sessionID string) (*models.User, *models.UserSession, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, nil, ErrUnauthorized
	}

	var session models.UserSession
	if err := l.db.WithContext(ctx).Where("id = ?", sessionID).Take(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrUnauthorized
		}
		return nil, nil, err
	}
	if session.ExpiresAt.Before(l.now().UTC()) {
		_ = l.db.WithContext(ctx).Delete(&models.UserSession{}, "id = ?", session.ID).Error
		return nil, nil, ErrUnauthorized
	}

	var user models.User
	if err := l.db.WithContext(ctx).First(&user, session.UserID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrUnauthorized
		}
		return nil, nil, err
	}
	return &user, &session, nil
}

func (l *AuthLogic) Logout(ctx context.Context, sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	return l.db.WithContext(ctx).Delete(&models.UserSession{}, "id = ?", sessionID).Error
}
