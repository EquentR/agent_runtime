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
	ErrInvalidCredentials         = errors.New("用户名或密码错误")
	ErrUsernameTaken              = errors.New("用户名已存在")
	ErrUnauthorized               = errors.New("未登录或会话已失效")
	ErrEmailTaken                 = errors.New("邮箱已存在")
	ErrEmailRequired              = errors.New("邮箱不能为空")
	ErrPublicRegistrationDisabled = errors.New("当前不开放注册")
	ErrEmailVerificationRequired  = errors.New("需要验证邮箱")
	ErrEmailBindingRequired       = errors.New("需要绑定邮箱")
	ErrUserDisabled               = errors.New("用户已被禁用")
	ErrPasswordChangeRequired     = errors.New("需要修改密码")
	ErrMailServiceUnavailable     = errors.New("邮件服务未配置")
)

type AuthConfig struct {
	CookieName string
	SessionTTL time.Duration
}

type RegisterInput struct {
	Username        string
	Email           string
	Password        string
	ConfirmPassword string
	TurnstileToken  string
}

type AuthSettingsReader interface {
	GetPublicRegistration(ctx context.Context) (PublicRegistrationSettings, error)
}

type AuthOption func(*AuthLogic)

type AuthLogic struct {
	db                *gorm.DB
	cookieName        string
	sessionTTL        time.Duration
	now               func() time.Time
	settings          AuthSettingsReader
	emailVerification *EmailVerificationLogic
}

func WithAuthSettings(settings AuthSettingsReader) AuthOption {
	return func(l *AuthLogic) {
		l.settings = settings
	}
}

func WithAuthEmailVerification(verification *EmailVerificationLogic) AuthOption {
	return func(l *AuthLogic) {
		l.emailVerification = verification
	}
}

func WithAuthClock(now func() time.Time) AuthOption {
	return func(l *AuthLogic) {
		if now != nil {
			l.now = now
		}
	}
}

func NewAuthLogic(db *gorm.DB, cfg AuthConfig, opts ...AuthOption) (*AuthLogic, error) {
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
	logic := &AuthLogic{db: db, cookieName: cookieName, sessionTTL: ttl, now: time.Now}
	for _, opt := range opts {
		if opt != nil {
			opt(logic)
		}
	}
	return logic, nil
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

func (l *AuthLogic) EmailVerification() *EmailVerificationLogic {
	if l == nil {
		return nil
	}
	return l.emailVerification
}

func (l *AuthLogic) Register(ctx context.Context, username, password, confirmPassword string) (*models.User, error) {
	return l.register(ctx, RegisterInput{
		Username:        username,
		Password:        password,
		ConfirmPassword: confirmPassword,
	}, true)
}

func (l *AuthLogic) RegisterWithInput(ctx context.Context, input RegisterInput) (*models.User, error) {
	return l.register(ctx, input, false)
}

func (l *AuthLogic) register(ctx context.Context, input RegisterInput, legacyCompatibility bool) (*models.User, error) {
	username := strings.TrimSpace(input.Username)
	if username == "" {
		return nil, fmt.Errorf("用户名不能为空")
	}
	if len(input.Password) < 6 {
		return nil, fmt.Errorf("密码至少需要 6 位")
	}
	if input.Password != input.ConfirmPassword {
		return nil, fmt.Errorf("两次输入的密码不一致")
	}

	email := normalizeAuthEmail(input.Email)
	usernameAsEmail := normalizeAuthEmail(username)
	var user *models.User
	needsVerification := false
	if err := l.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var userCount int64
		if err := tx.Model(&models.User{}).Count(&userCount).Error; err != nil {
			return err
		}
		if legacyCompatibility && userCount == 0 && email == "" {
			email = strings.ToLower(username) + "@legacy.local"
		}
		if email == "" {
			return ErrEmailRequired
		}
		if userCount > 0 {
			if l.settings != nil {
				registration, err := l.settings.GetPublicRegistration(ctx)
				if err != nil {
					return err
				}
				if !registration.Enabled {
					return ErrPublicRegistrationDisabled
				}
			}
			if err := l.emailVerification.CanSendFor(ctx); err != nil {
				return err
			}
		}

		var existing models.User
		err := tx.Where("username = ?", username).Take(&existing).Error
		if err == nil {
			return ErrUsernameTaken
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if usernameAsEmail != "" {
			err = tx.Where("email = ?", usernameAsEmail).Take(&existing).Error
			if err == nil {
				return ErrUsernameTaken
			}
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
		if email != "" {
			err = tx.Where("email = ?", email).Take(&existing).Error
			if err == nil {
				return ErrEmailTaken
			}
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			err = tx.Where("LOWER(username) = ?", email).Take(&existing).Error
			if err == nil {
				return ErrEmailTaken
			}
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
		if err != nil {
			return err
		}

		now := l.now().UTC()
		user = &models.User{
			Username:     username,
			Email:        email,
			DisplayName:  username,
			PasswordHash: string(hash),
			Role:         models.UserRoleUser,
			Status:       models.UserStatusPendingEmailVerification,
		}
		if userCount == 0 {
			user.Role = models.UserRoleAdmin
			user.Status = models.UserStatusActive
			user.EmailVerifiedAt = &now
		} else {
			needsVerification = true
		}
		if err := tx.Create(user).Error; err != nil {
			errText := strings.ToLower(err.Error())
			if strings.Contains(errText, "email") {
				return ErrEmailTaken
			}
			if strings.Contains(errText, "unique") {
				return ErrUsernameTaken
			}
			return err
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if needsVerification {
		if err := l.emailVerification.Send(ctx, SendEmailVerificationInput{
			UserID:  user.ID,
			Email:   user.Email,
			Purpose: EmailVerificationPurposeRegistration,
		}); err != nil {
			return nil, err
		}
	}
	return user, nil
}

func (l *AuthLogic) Login(ctx context.Context, username, password string) (*models.User, *models.UserSession, error) {
	identifier := strings.TrimSpace(username)
	if identifier == "" || password == "" {
		return nil, nil, ErrInvalidCredentials
	}

	var user models.User
	email := normalizeAuthEmail(identifier)
	query := l.db.WithContext(ctx).Where("username = ?", identifier)
	if email != "" {
		query = l.db.WithContext(ctx).Where("username = ? OR email = ?", identifier, email)
	}
	if err := query.Take(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrInvalidCredentials
		}
		return nil, nil, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, nil, ErrInvalidCredentials
	}
	if err := validateUserCanLogin(&user); err != nil {
		return nil, nil, err
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

func validateUserCanLogin(user *models.User) error {
	if user == nil {
		return ErrUnauthorized
	}
	switch user.Status {
	case models.UserStatusDisabled:
		return ErrUserDisabled
	case models.UserStatusPendingEmailVerification:
		return ErrEmailVerificationRequired
	}
	if user.ForcePasswordChange {
		return ErrPasswordChangeRequired
	}
	if user.Status != "" && user.Status != models.UserStatusActive && user.Status != models.UserStatusNeedsEmailBinding {
		return ErrEmailVerificationRequired
	}
	return nil
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

func normalizeAuthEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
