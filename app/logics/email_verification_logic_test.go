package logics

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/EquentR/agent_runtime/app/models"
	"github.com/EquentR/agent_runtime/pkg/mail"
	"github.com/glebarez/sqlite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type fakeVerificationMailSender struct {
	messages []mail.Message
	err      error
}

func (s *fakeVerificationMailSender) Send(ctx context.Context, message mail.Message) error {
	if s.err != nil {
		return s.err
	}
	s.messages = append(s.messages, message)
	return nil
}

func TestEmailVerificationAcceptsSixDigitCodeAndActivatesUser(t *testing.T) {
	now := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	subject := newEmailVerificationTestSubject(t, now, "123456")
	user := seedEmailVerificationUser(t, subject.db, "pending", "pending@example.com", models.UserStatusPendingEmailVerification)

	if err := subject.logic.Send(context.Background(), SendEmailVerificationInput{
		UserID:  user.ID,
		Email:   user.Email,
		Purpose: EmailVerificationPurposeRegistration,
	}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if len(subject.mailer.messages) != 1 {
		t.Fatalf("sent messages = %d, want 1", len(subject.mailer.messages))
	}
	if !strings.Contains(subject.mailer.messages[0].Body, "123456") {
		t.Fatalf("message body = %q, want generated code", subject.mailer.messages[0].Body)
	}

	var stored models.EmailVerification
	if err := subject.db.Where("user_id = ?", user.ID).Take(&stored).Error; err != nil {
		t.Fatalf("load verification: %v", err)
	}
	if stored.CodeHash == "123456" {
		t.Fatal("verification code stored in plaintext")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(stored.CodeHash), []byte("123456")); err != nil {
		t.Fatalf("stored code hash mismatch: %v", err)
	}
	if got := stored.ExpiresAt.Sub(now); got != 10*time.Minute {
		t.Fatalf("expiry duration = %s, want 10m", got)
	}

	activated, err := subject.logic.Verify(context.Background(), VerifyEmailInput{
		UserID:  user.ID,
		Email:   user.Email,
		Purpose: EmailVerificationPurposeRegistration,
		Code:    "123456",
	})
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if activated.Status != models.UserStatusActive {
		t.Fatalf("activated status = %q, want %q", activated.Status, models.UserStatusActive)
	}
	if activated.EmailVerifiedAt == nil {
		t.Fatal("activated EmailVerifiedAt = nil, want timestamp")
	}

	var consumed models.EmailVerification
	if err := subject.db.First(&consumed, "id = ?", stored.ID).Error; err != nil {
		t.Fatalf("reload verification: %v", err)
	}
	if consumed.ConsumedAt == nil {
		t.Fatal("ConsumedAt = nil, want consumed timestamp")
	}
}

func TestEmailVerificationRejectsExpiredCodeAndTooManyAttempts(t *testing.T) {
	now := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	subject := newEmailVerificationTestSubject(t, now, "123456")
	expiredUser := seedEmailVerificationUser(t, subject.db, "expired", "expired@example.com", models.UserStatusPendingEmailVerification)

	if err := subject.logic.Send(context.Background(), SendEmailVerificationInput{
		UserID:  expiredUser.ID,
		Email:   expiredUser.Email,
		Purpose: EmailVerificationPurposeRegistration,
	}); err != nil {
		t.Fatalf("Send(expired) error = %v", err)
	}
	subject.now = now.Add(11 * time.Minute)
	_, err := subject.logic.Verify(context.Background(), VerifyEmailInput{
		UserID:  expiredUser.ID,
		Email:   expiredUser.Email,
		Purpose: EmailVerificationPurposeRegistration,
		Code:    "123456",
	})
	if !errors.Is(err, ErrEmailVerificationExpired) {
		t.Fatalf("Verify(expired) error = %v, want %v", err, ErrEmailVerificationExpired)
	}

	subject.now = now
	lockedUser := seedEmailVerificationUser(t, subject.db, "locked", "locked@example.com", models.UserStatusPendingEmailVerification)
	if err := subject.logic.Send(context.Background(), SendEmailVerificationInput{
		UserID:  lockedUser.ID,
		Email:   lockedUser.Email,
		Purpose: EmailVerificationPurposeRegistration,
	}); err != nil {
		t.Fatalf("Send(locked) error = %v", err)
	}

	for attempt := 1; attempt <= 5; attempt++ {
		_, err = subject.logic.Verify(context.Background(), VerifyEmailInput{
			UserID:  lockedUser.ID,
			Email:   lockedUser.Email,
			Purpose: EmailVerificationPurposeRegistration,
			Code:    "000000",
		})
		if attempt < 5 && !errors.Is(err, ErrEmailVerificationInvalidCode) {
			t.Fatalf("Verify(bad code attempt %d) error = %v, want %v", attempt, err, ErrEmailVerificationInvalidCode)
		}
		if attempt == 5 && !errors.Is(err, ErrEmailVerificationTooManyAttempts) {
			t.Fatalf("Verify(bad code attempt %d) error = %v, want %v", attempt, err, ErrEmailVerificationTooManyAttempts)
		}
	}

	_, err = subject.logic.Verify(context.Background(), VerifyEmailInput{
		UserID:  lockedUser.ID,
		Email:   lockedUser.Email,
		Purpose: EmailVerificationPurposeRegistration,
		Code:    "123456",
	})
	if !errors.Is(err, ErrEmailVerificationTooManyAttempts) {
		t.Fatalf("Verify(after lockout) error = %v, want %v", err, ErrEmailVerificationTooManyAttempts)
	}
}

func TestEmailVerificationDoesNotActivateDisabledUser(t *testing.T) {
	now := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	subject := newEmailVerificationTestSubject(t, now, "123456")
	user := seedEmailVerificationUser(t, subject.db, "disabled", "disabled@example.com", models.UserStatusDisabled)

	if err := subject.logic.Send(context.Background(), SendEmailVerificationInput{
		UserID:  user.ID,
		Email:   user.Email,
		Purpose: EmailVerificationPurposeRegistration,
	}); !errors.Is(err, ErrEmailVerificationInvalidState) {
		t.Fatalf("Send(disabled) error = %v, want %v", err, ErrEmailVerificationInvalidState)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte("123456"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword() error = %v", err)
	}
	if err := subject.db.Create(&models.EmailVerification{
		ID:          "ev_disabled",
		UserID:      user.ID,
		Email:       user.Email,
		Purpose:     EmailVerificationPurposeRegistration,
		CodeHash:    string(hash),
		MaxAttempts: 5,
		ExpiresAt:   now.Add(10 * time.Minute),
		LastSentAt:  now,
	}).Error; err != nil {
		t.Fatalf("seed disabled verification: %v", err)
	}

	_, err = subject.logic.Verify(context.Background(), VerifyEmailInput{
		UserID:  user.ID,
		Email:   user.Email,
		Purpose: EmailVerificationPurposeRegistration,
		Code:    "123456",
	})
	if !errors.Is(err, ErrEmailVerificationInvalidState) {
		t.Fatalf("Verify(disabled) error = %v, want %v", err, ErrEmailVerificationInvalidState)
	}

	var stored models.User
	if err := subject.db.First(&stored, user.ID).Error; err != nil {
		t.Fatalf("reload disabled user: %v", err)
	}
	if stored.Status != models.UserStatusDisabled {
		t.Fatalf("disabled user status = %q, want %q", stored.Status, models.UserStatusDisabled)
	}
	if stored.EmailVerifiedAt != nil {
		t.Fatalf("disabled user EmailVerifiedAt = %v, want nil", stored.EmailVerifiedAt)
	}
}

func TestEmailVerificationRejectsMismatchedUserEmail(t *testing.T) {
	now := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	subject := newEmailVerificationTestSubject(t, now, "123456")
	user := seedEmailVerificationUser(t, subject.db, "pending", "pending@example.com", models.UserStatusPendingEmailVerification)

	err := subject.logic.SendByEmail(context.Background(), SendEmailVerificationInput{
		UserID:  user.ID,
		Email:   "attacker@example.com",
		Purpose: EmailVerificationPurposeRegistration,
	})
	if !errors.Is(err, ErrEmailVerificationNotFound) {
		t.Fatalf("SendByEmail(mismatched email) error = %v, want %v", err, ErrEmailVerificationNotFound)
	}
	if len(subject.mailer.messages) != 0 {
		t.Fatalf("sent messages = %d, want 0", len(subject.mailer.messages))
	}

	_, err = subject.logic.Verify(context.Background(), VerifyEmailInput{
		UserID:  user.ID,
		Email:   "attacker@example.com",
		Purpose: EmailVerificationPurposeRegistration,
		Code:    "123456",
	})
	if !errors.Is(err, ErrEmailVerificationNotFound) {
		t.Fatalf("Verify(mismatched email) error = %v, want %v", err, ErrEmailVerificationNotFound)
	}

	var stored models.User
	if err := subject.db.First(&stored, user.ID).Error; err != nil {
		t.Fatalf("reload pending user: %v", err)
	}
	if stored.Email != user.Email || stored.Status != models.UserStatusPendingEmailVerification || stored.EmailVerifiedAt != nil {
		t.Fatalf("user after attack = %#v, want unchanged pending user", stored)
	}
}

func TestEmailVerificationRejectsBindingEmailConflictingWithUsernameCaseInsensitively(t *testing.T) {
	now := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	subject := newEmailVerificationTestSubject(t, now, "123456")
	bindingUser := seedEmailVerificationUser(t, subject.db, "legacy", "", models.UserStatusNeedsEmailBinding)
	_ = seedEmailVerificationUser(t, subject.db, "Alice@Example.COM", "owner@example.com", models.UserStatusActive)

	err := subject.logic.Send(context.Background(), SendEmailVerificationInput{
		UserID:  bindingUser.ID,
		Email:   "alice@example.com",
		Purpose: EmailVerificationPurposeEmailBinding,
	})
	if !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("Send(binding email conflicts with username) error = %v, want %v", err, ErrEmailTaken)
	}
	if len(subject.mailer.messages) != 0 {
		t.Fatalf("sent messages = %d, want 0", len(subject.mailer.messages))
	}
}

func TestEmailVerificationEnforcesResendCooldown(t *testing.T) {
	now := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
	subject := newEmailVerificationTestSubject(t, now, "123456")
	user := seedEmailVerificationUser(t, subject.db, "pending", "pending@example.com", models.UserStatusPendingEmailVerification)

	input := SendEmailVerificationInput{
		UserID:  user.ID,
		Email:   user.Email,
		Purpose: EmailVerificationPurposeRegistration,
	}
	if err := subject.logic.Send(context.Background(), input); err != nil {
		t.Fatalf("Send(first) error = %v", err)
	}

	subject.now = now.Add(30 * time.Second)
	err := subject.logic.Send(context.Background(), input)
	if !errors.Is(err, ErrEmailVerificationCooldown) {
		t.Fatalf("Send(cooldown) error = %v, want %v", err, ErrEmailVerificationCooldown)
	}
	if len(subject.mailer.messages) != 1 {
		t.Fatalf("sent messages after cooldown rejection = %d, want 1", len(subject.mailer.messages))
	}

	subject.now = now.Add(61 * time.Second)
	if err := subject.logic.Send(context.Background(), input); err != nil {
		t.Fatalf("Send(after cooldown) error = %v", err)
	}
	if len(subject.mailer.messages) != 2 {
		t.Fatalf("sent messages after cooldown = %d, want 2", len(subject.mailer.messages))
	}
}

type emailVerificationTestSubject struct {
	db     *gorm.DB
	logic  *EmailVerificationLogic
	mailer *fakeVerificationMailSender
	now    time.Time
}

func newEmailVerificationTestSubject(t *testing.T, now time.Time, code string) *emailVerificationTestSubject {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.EmailVerification{}); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}

	subject := &emailVerificationTestSubject{
		db:     db,
		mailer: &fakeVerificationMailSender{},
		now:    now,
	}
	logic, err := NewEmailVerificationLogic(db, EmailVerificationConfig{
		Sender: subject.mailer,
		Now: func() time.Time {
			return subject.now
		},
		CodeGenerator: func() (string, error) {
			return code, nil
		},
	})
	if err != nil {
		t.Fatalf("NewEmailVerificationLogic() error = %v", err)
	}
	subject.logic = logic
	return subject
}

func seedEmailVerificationUser(t *testing.T, db *gorm.DB, username string, email string, status string) models.User {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte("secret-123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword() error = %v", err)
	}
	user := models.User{
		Username:     username,
		Email:        email,
		DisplayName:  username,
		PasswordHash: string(hash),
		Role:         models.UserRoleUser,
		Status:       status,
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("seed verification user %q: %v", username, err)
	}
	return user
}
