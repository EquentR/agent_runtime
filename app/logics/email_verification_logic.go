package logics

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/EquentR/agent_runtime/app/models"
	"github.com/EquentR/agent_runtime/pkg/mail"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const (
	EmailVerificationPurposeRegistration = "registration"
	EmailVerificationPurposeEmailBinding = "email_binding"
)

var (
	ErrEmailVerificationNotFound        = errors.New("验证码不存在")
	ErrEmailVerificationInvalidCode     = errors.New("验证码错误")
	ErrEmailVerificationExpired         = errors.New("验证码已过期")
	ErrEmailVerificationTooManyAttempts = errors.New("验证码尝试次数过多")
	ErrEmailVerificationCooldown        = errors.New("验证码发送过于频繁")
	ErrEmailVerificationInvalidState    = errors.New("当前用户状态不能验证邮箱")
)

type EmailVerificationConfig struct {
	Sender         mail.Sender
	Now            func() time.Time
	CodeGenerator  func() (string, error)
	Expiry         time.Duration
	ResendCooldown time.Duration
	MaxAttempts    int
}

type SendEmailVerificationInput struct {
	UserID  uint64
	Email   string
	Purpose string
}

type VerifyEmailInput struct {
	UserID  uint64
	Email   string
	Purpose string
	Code    string
}

type EmailVerificationLogic struct {
	db             *gorm.DB
	sender         mail.Sender
	now            func() time.Time
	codeGenerator  func() (string, error)
	expiry         time.Duration
	resendCooldown time.Duration
	maxAttempts    int
}

func NewEmailVerificationLogic(db *gorm.DB, cfg EmailVerificationConfig) (*EmailVerificationLogic, error) {
	if db == nil {
		return nil, fmt.Errorf("email verification db is required")
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	codeGenerator := cfg.CodeGenerator
	if codeGenerator == nil {
		codeGenerator = generateSixDigitCode
	}
	expiry := cfg.Expiry
	if expiry <= 0 {
		expiry = 10 * time.Minute
	}
	resendCooldown := cfg.ResendCooldown
	if resendCooldown <= 0 {
		resendCooldown = time.Minute
	}
	maxAttempts := cfg.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	return &EmailVerificationLogic{
		db:             db,
		sender:         cfg.Sender,
		now:            now,
		codeGenerator:  codeGenerator,
		expiry:         expiry,
		resendCooldown: resendCooldown,
		maxAttempts:    maxAttempts,
	}, nil
}

func (l *EmailVerificationLogic) CanSend() bool {
	return l != nil && l.sender != nil
}

func (l *EmailVerificationLogic) CanSendFor(ctx context.Context) error {
	if !l.CanSend() {
		return ErrMailServiceUnavailable
	}
	if checker, ok := l.sender.(interface {
		Available(context.Context) error
	}); ok {
		if err := checker.Available(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (l *EmailVerificationLogic) Send(ctx context.Context, input SendEmailVerificationInput) error {
	if err := l.CanSendFor(ctx); err != nil {
		return err
	}
	email := normalizeAuthEmail(input.Email)
	if email == "" {
		return ErrEmailRequired
	}
	purpose := normalizeEmailVerificationPurpose(input.Purpose)
	now := l.now().UTC()

	var code string
	var row models.EmailVerification
	if err := l.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user models.User
		if err := tx.First(&user, input.UserID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrEmailVerificationNotFound
			}
			return err
		}
		if err := validateEmailVerificationTarget(tx, &user, email, purpose); err != nil {
			return err
		}

		var existing models.EmailVerification
		err := tx.Where("user_id = ? AND email = ? AND purpose = ? AND consumed_at IS NULL", input.UserID, email, purpose).
			Order("created_at DESC").
			Take(&existing).Error
		if err == nil && now.Sub(existing.LastSentAt) < l.resendCooldown {
			return ErrEmailVerificationCooldown
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		generated, err := l.codeGenerator()
		if err != nil {
			return err
		}
		if !isSixDigitCode(generated) {
			return fmt.Errorf("email verification code generator returned invalid code")
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(generated), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		code = generated
		row = models.EmailVerification{
			ID:          "ev_" + uuid.NewString(),
			UserID:      input.UserID,
			Email:       email,
			Purpose:     purpose,
			CodeHash:    string(hash),
			Attempts:    0,
			MaxAttempts: l.maxAttempts,
			ExpiresAt:   now.Add(l.expiry),
			LastSentAt:  now,
		}
		return tx.Create(&row).Error
	}); err != nil {
		return err
	}

	return l.sender.Send(ctx, mail.Message{
		To:      email,
		Subject: "邮箱验证码",
		Body:    fmt.Sprintf("您的邮箱验证码是 %s，10 分钟内有效。", code),
	})
}

func (l *EmailVerificationLogic) SendByEmail(ctx context.Context, input SendEmailVerificationInput) error {
	purpose := normalizeEmailVerificationPurpose(input.Purpose)
	if purpose != EmailVerificationPurposeRegistration {
		return ErrEmailVerificationInvalidState
	}
	email := normalizeAuthEmail(input.Email)
	if email == "" {
		return ErrEmailRequired
	}
	if input.UserID != 0 {
		var user models.User
		if err := l.db.WithContext(ctx).First(&user, input.UserID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrEmailVerificationNotFound
			}
			return err
		}
		if normalizeAuthEmail(user.Email) != email {
			return ErrEmailVerificationNotFound
		}
		input.Email = email
		input.Purpose = purpose
		return l.Send(ctx, input)
	}
	var user models.User
	if err := l.db.WithContext(ctx).Where("email = ?", email).Take(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrEmailVerificationNotFound
		}
		return err
	}
	input.UserID = user.ID
	input.Email = normalizeAuthEmail(user.Email)
	input.Purpose = purpose
	return l.Send(ctx, input)
}

func (l *EmailVerificationLogic) Verify(ctx context.Context, input VerifyEmailInput) (*models.User, error) {
	if l == nil {
		return nil, ErrEmailVerificationNotFound
	}
	email := normalizeAuthEmail(input.Email)
	if email == "" {
		return nil, ErrEmailRequired
	}
	purpose := normalizeEmailVerificationPurpose(input.Purpose)
	code := strings.TrimSpace(input.Code)
	now := l.now().UTC()

	var user models.User
	var verifyErr error
	if err := l.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var verification models.EmailVerification
		err := tx.Where("user_id = ? AND email = ? AND purpose = ? AND consumed_at IS NULL", input.UserID, email, purpose).
			Order("created_at DESC").
			Take(&verification).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrEmailVerificationNotFound
		}
		if err != nil {
			return err
		}
		maxAttempts := verification.MaxAttempts
		if maxAttempts <= 0 || maxAttempts > l.maxAttempts {
			maxAttempts = l.maxAttempts
		}
		if verification.Attempts >= maxAttempts {
			return ErrEmailVerificationTooManyAttempts
		}
		if !now.Before(verification.ExpiresAt) {
			return ErrEmailVerificationExpired
		}
		if err := bcrypt.CompareHashAndPassword([]byte(verification.CodeHash), []byte(code)); err != nil {
			attempts := verification.Attempts + 1
			if err := tx.Model(&models.EmailVerification{}).Where("id = ?", verification.ID).Update("attempts", attempts).Error; err != nil {
				return err
			}
			if attempts >= maxAttempts {
				verifyErr = ErrEmailVerificationTooManyAttempts
				return nil
			}
			verifyErr = ErrEmailVerificationInvalidCode
			return nil
		}

		if err := tx.First(&user, input.UserID).Error; err != nil {
			return err
		}
		if err := validateEmailVerificationTarget(tx, &user, verification.Email, purpose); err != nil {
			return err
		}

		consumedAt := now
		if err := tx.Model(&models.EmailVerification{}).Where("id = ?", verification.ID).Updates(map[string]any{
			"consumed_at": &consumedAt,
			"updated_at":  now,
		}).Error; err != nil {
			return err
		}

		user.Email = verification.Email
		user.EmailVerifiedAt = &consumedAt
		switch purpose {
		case EmailVerificationPurposeRegistration, EmailVerificationPurposeEmailBinding:
			user.Status = models.UserStatusActive
		}
		return tx.Save(&user).Error
	}); err != nil {
		return nil, err
	}
	if verifyErr != nil {
		return nil, verifyErr
	}
	return &user, nil
}

func generateSixDigitCode() (string, error) {
	value, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", value.Int64()), nil
}

func normalizeEmailVerificationPurpose(purpose string) string {
	purpose = strings.TrimSpace(purpose)
	if purpose == "" {
		return EmailVerificationPurposeRegistration
	}
	return purpose
}

func isSixDigitCode(code string) bool {
	if len(code) != 6 {
		return false
	}
	for _, char := range code {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func canApplyEmailVerificationPurpose(status string, purpose string) bool {
	switch purpose {
	case EmailVerificationPurposeRegistration:
		return status == models.UserStatusPendingEmailVerification
	case EmailVerificationPurposeEmailBinding:
		return status == models.UserStatusNeedsEmailBinding
	default:
		return false
	}
}

func validateEmailVerificationTarget(tx *gorm.DB, user *models.User, email string, purpose string) error {
	if user == nil {
		return ErrEmailVerificationNotFound
	}
	email = normalizeAuthEmail(email)
	if email == "" {
		return ErrEmailRequired
	}
	if !canApplyEmailVerificationPurpose(user.Status, purpose) {
		return ErrEmailVerificationInvalidState
	}
	switch purpose {
	case EmailVerificationPurposeRegistration:
		if normalizeAuthEmail(user.Email) != email {
			return ErrEmailVerificationNotFound
		}
	case EmailVerificationPurposeEmailBinding:
		var existing models.User
		err := tx.Where("email = ? AND id <> ?", email, user.ID).Take(&existing).Error
		if err == nil {
			return ErrEmailTaken
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	default:
		return ErrEmailVerificationInvalidState
	}
	return nil
}
