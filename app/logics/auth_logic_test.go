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

type fakeAuthMailSender struct {
	messages []mail.Message
	err      error
}

func (s *fakeAuthMailSender) Send(ctx context.Context, message mail.Message) error {
	if s.err != nil {
		return s.err
	}
	s.messages = append(s.messages, message)
	return nil
}

func TestAuthLogicRegisterAssignsAdminToFirstUser(t *testing.T) {
	logic := newAuthLogicTestSubject(t)

	user, err := logic.RegisterWithInput(context.Background(), RegisterInput{
		Username:        "alice",
		Email:           "alice@example.com",
		Password:        "secret-123",
		ConfirmPassword: "secret-123",
	})
	if err != nil {
		t.Fatalf("RegisterWithInput() error = %v", err)
	}
	if user.Role != models.UserRoleAdmin {
		t.Fatalf("first user role = %q, want %q", user.Role, models.UserRoleAdmin)
	}

	stored := loadAuthTestUser(t, logic.db, user.ID)
	if stored.Role != models.UserRoleAdmin {
		t.Fatalf("stored first user role = %q, want %q", stored.Role, models.UserRoleAdmin)
	}
}

func TestAuthLogicRegisterAssignsUserRoleToLaterUsers(t *testing.T) {
	logic := newAuthLogicTestSubject(t, withAuthTestMailer(&fakeAuthMailSender{}, "123456"))

	firstUser, err := logic.RegisterWithInput(context.Background(), RegisterInput{
		Username:        "alice",
		Email:           "alice@example.com",
		Password:        "secret-123",
		ConfirmPassword: "secret-123",
	})
	if err != nil {
		t.Fatalf("RegisterWithInput(first) error = %v", err)
	}
	if firstUser.Role != models.UserRoleAdmin {
		t.Fatalf("first user role = %q, want %q", firstUser.Role, models.UserRoleAdmin)
	}

	secondUser, err := logic.RegisterWithInput(context.Background(), RegisterInput{
		Username:        "bob",
		Email:           "bob@example.com",
		Password:        "secret-123",
		ConfirmPassword: "secret-123",
	})
	if err != nil {
		t.Fatalf("RegisterWithInput(second) error = %v", err)
	}
	if secondUser.Role != models.UserRoleUser {
		t.Fatalf("second user role = %q, want %q", secondUser.Role, models.UserRoleUser)
	}

	stored := loadAuthTestUser(t, logic.db, secondUser.ID)
	if stored.Role != models.UserRoleUser {
		t.Fatalf("stored second user role = %q, want %q", stored.Role, models.UserRoleUser)
	}
}

func TestAuthLogicFirstUserRequiresEmailAndMarksVerifiedAdmin(t *testing.T) {
	logic := newAuthLogicTestSubject(t)

	_, err := logic.RegisterWithInput(context.Background(), RegisterInput{
		Username:        "alice",
		Password:        "secret-123",
		ConfirmPassword: "secret-123",
	})
	if !errors.Is(err, ErrEmailRequired) {
		t.Fatalf("RegisterWithInput(missing email) error = %v, want %v", err, ErrEmailRequired)
	}

	user, err := logic.RegisterWithInput(context.Background(), RegisterInput{
		Username:        "alice",
		Email:           "Alice@Example.COM",
		Password:        "secret-123",
		ConfirmPassword: "secret-123",
	})
	if err != nil {
		t.Fatalf("RegisterWithInput(first) error = %v", err)
	}
	if user.Role != models.UserRoleAdmin {
		t.Fatalf("first user role = %q, want %q", user.Role, models.UserRoleAdmin)
	}
	if user.Status != models.UserStatusActive {
		t.Fatalf("first user status = %q, want %q", user.Status, models.UserStatusActive)
	}
	if user.Email != "alice@example.com" {
		t.Fatalf("first user email = %q, want normalized email", user.Email)
	}
	if user.EmailVerifiedAt == nil {
		t.Fatal("first user EmailVerifiedAt = nil, want verified timestamp")
	}
}

func TestAuthLogicLegacyRegisterOnlySynthesizesEmailForEmptyUserTable(t *testing.T) {
	logic := newAuthLogicTestSubject(t)
	ctx := context.Background()

	firstUser, err := logic.Register(ctx, "legacy_admin", "secret-123", "secret-123")
	if err != nil {
		t.Fatalf("Register(first legacy) error = %v", err)
	}
	if firstUser.Email != "legacy_admin@legacy.local" {
		t.Fatalf("first legacy email = %q, want synthesized legacy email", firstUser.Email)
	}
	if firstUser.Role != models.UserRoleAdmin {
		t.Fatalf("first legacy role = %q, want %q", firstUser.Role, models.UserRoleAdmin)
	}
	if firstUser.Status != models.UserStatusActive {
		t.Fatalf("first legacy status = %q, want %q", firstUser.Status, models.UserStatusActive)
	}
	if firstUser.EmailVerifiedAt == nil {
		t.Fatal("first legacy EmailVerifiedAt = nil, want verified timestamp")
	}

	_, err = logic.Register(ctx, "legacy_bob", "secret-123", "secret-123")
	if !errors.Is(err, ErrEmailRequired) {
		t.Fatalf("Register(second legacy without email) error = %v, want %v", err, ErrEmailRequired)
	}

	var count int64
	if err := logic.db.Model(&models.User{}).Where("username = ?", "legacy_bob").Count(&count).Error; err != nil {
		t.Fatalf("count second legacy user: %v", err)
	}
	if count != 0 {
		t.Fatalf("second legacy users created = %d, want 0", count)
	}
}

func TestAuthLogicRegisterCreatesPendingUserAndSendsVerification(t *testing.T) {
	mailer := &fakeAuthMailSender{}
	logic := newAuthLogicTestSubject(t, withAuthTestMailer(mailer, "123456"))

	if _, err := logic.RegisterWithInput(context.Background(), RegisterInput{
		Username:        "admin",
		Email:           "admin@example.com",
		Password:        "secret-123",
		ConfirmPassword: "secret-123",
	}); err != nil {
		t.Fatalf("RegisterWithInput(first) error = %v", err)
	}

	user, err := logic.RegisterWithInput(context.Background(), RegisterInput{
		Username:        "bob",
		Email:           "Bob@Example.COM",
		Password:        "secret-123",
		ConfirmPassword: "secret-123",
	})
	if err != nil {
		t.Fatalf("RegisterWithInput(second) error = %v", err)
	}
	if user.Role != models.UserRoleUser {
		t.Fatalf("second user role = %q, want %q", user.Role, models.UserRoleUser)
	}
	if user.Status != models.UserStatusPendingEmailVerification {
		t.Fatalf("second user status = %q, want %q", user.Status, models.UserStatusPendingEmailVerification)
	}
	if user.EmailVerifiedAt != nil {
		t.Fatalf("second user EmailVerifiedAt = %v, want nil", user.EmailVerifiedAt)
	}
	if user.Email != "bob@example.com" {
		t.Fatalf("second user email = %q, want normalized email", user.Email)
	}
	if len(mailer.messages) != 1 {
		t.Fatalf("sent messages = %d, want 1", len(mailer.messages))
	}
	if mailer.messages[0].To != "bob@example.com" {
		t.Fatalf("verification recipient = %q, want bob@example.com", mailer.messages[0].To)
	}
	if !strings.Contains(mailer.messages[0].Body, "123456") {
		t.Fatalf("verification body = %q, want generated code", mailer.messages[0].Body)
	}

	var verification models.EmailVerification
	if err := logic.db.Where("user_id = ? AND email = ?", user.ID, user.Email).Take(&verification).Error; err != nil {
		t.Fatalf("load verification row: %v", err)
	}
	if verification.CodeHash == "123456" {
		t.Fatal("verification code stored in plaintext")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(verification.CodeHash), []byte("123456")); err != nil {
		t.Fatalf("stored verification hash does not match generated code: %v", err)
	}
}

func TestAuthLogicRejectsPublicRegistrationWhenDisabled(t *testing.T) {
	settings := &fakeAuthSettings{publicRegistration: PublicRegistrationSettings{Enabled: true}}
	mailer := &fakeAuthMailSender{}
	logic := newAuthLogicTestSubject(t, withAuthTestSettings(settings), withAuthTestMailer(mailer, "123456"))

	if _, err := logic.RegisterWithInput(context.Background(), RegisterInput{
		Username:        "admin",
		Email:           "admin@example.com",
		Password:        "secret-123",
		ConfirmPassword: "secret-123",
	}); err != nil {
		t.Fatalf("RegisterWithInput(first) error = %v", err)
	}

	settings.publicRegistration.Enabled = false
	_, err := logic.RegisterWithInput(context.Background(), RegisterInput{
		Username:        "bob",
		Email:           "bob@example.com",
		Password:        "secret-123",
		ConfirmPassword: "secret-123",
	})
	if !errors.Is(err, ErrPublicRegistrationDisabled) {
		t.Fatalf("RegisterWithInput(disabled) error = %v, want %v", err, ErrPublicRegistrationDisabled)
	}
	if len(mailer.messages) != 0 {
		t.Fatalf("sent messages = %d, want 0", len(mailer.messages))
	}
}

func TestAuthLogicRejectsSecondRegistrationWhenSMTPUnavailable(t *testing.T) {
	logic := newAuthLogicTestSubject(t)

	if _, err := logic.RegisterWithInput(context.Background(), RegisterInput{
		Username:        "admin",
		Email:           "admin@example.com",
		Password:        "secret-123",
		ConfirmPassword: "secret-123",
	}); err != nil {
		t.Fatalf("RegisterWithInput(first) error = %v", err)
	}

	_, err := logic.RegisterWithInput(context.Background(), RegisterInput{
		Username:        "bob",
		Email:           "bob@example.com",
		Password:        "secret-123",
		ConfirmPassword: "secret-123",
	})
	if !errors.Is(err, ErrMailServiceUnavailable) {
		t.Fatalf("RegisterWithInput(second without mailer) error = %v, want %v", err, ErrMailServiceUnavailable)
	}
}

func TestAuthLogicRejectsUsernameAndEmailCrossFieldConflicts(t *testing.T) {
	mailer := &fakeAuthMailSender{}
	logic := newAuthLogicTestSubject(t, withAuthTestMailer(mailer, "123456"))

	if _, err := logic.RegisterWithInput(context.Background(), RegisterInput{
		Username:        "alice",
		Email:           "alice@example.com",
		Password:        "secret-123",
		ConfirmPassword: "secret-123",
	}); err != nil {
		t.Fatalf("RegisterWithInput(first) error = %v", err)
	}

	_, err := logic.RegisterWithInput(context.Background(), RegisterInput{
		Username:        "alice@example.com",
		Email:           "bob@example.com",
		Password:        "secret-123",
		ConfirmPassword: "secret-123",
	})
	if !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("RegisterWithInput(username conflicts with email) error = %v, want %v", err, ErrUsernameTaken)
	}

	_, err = logic.RegisterWithInput(context.Background(), RegisterInput{
		Username:        "bob",
		Email:           "alice",
		Password:        "secret-123",
		ConfirmPassword: "secret-123",
	})
	if !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("RegisterWithInput(email conflicts with username) error = %v, want %v", err, ErrEmailTaken)
	}
}

func TestAuthLogicLoginAcceptsUsernameOrEmail(t *testing.T) {
	logic := newAuthLogicTestSubject(t)

	if _, err := logic.RegisterWithInput(context.Background(), RegisterInput{
		Username:        "alice",
		Email:           "Alice@Example.COM",
		Password:        "secret-123",
		ConfirmPassword: "secret-123",
	}); err != nil {
		t.Fatalf("RegisterWithInput() error = %v", err)
	}

	byUsername, usernameSession, err := logic.Login(context.Background(), "alice", "secret-123")
	if err != nil {
		t.Fatalf("Login(username) error = %v", err)
	}
	if byUsername.Username != "alice" || usernameSession.UserID != byUsername.ID {
		t.Fatalf("Login(username) returned user=%#v session=%#v", byUsername, usernameSession)
	}

	byEmail, emailSession, err := logic.Login(context.Background(), "alice@example.com", "secret-123")
	if err != nil {
		t.Fatalf("Login(email) error = %v", err)
	}
	if byEmail.Username != "alice" || emailSession.UserID != byEmail.ID {
		t.Fatalf("Login(email) returned user=%#v session=%#v", byEmail, emailSession)
	}
}

func TestAuthLogicLoginRejectsPendingDisabledAndNeedsBindingUsers(t *testing.T) {
	logic := newAuthLogicTestSubject(t)
	ctx := context.Background()

	seedAuthLoginUser(t, logic.db, "pending", "pending@example.com", models.UserStatusPendingEmailVerification, false, nil)
	_, _, err := logic.Login(ctx, "pending", "secret-123")
	if !errors.Is(err, ErrEmailVerificationRequired) {
		t.Fatalf("Login(pending) error = %v, want %v", err, ErrEmailVerificationRequired)
	}

	now := time.Now().UTC()
	seedAuthLoginUser(t, logic.db, "disabled", "disabled@example.com", models.UserStatusDisabled, false, &now)
	_, _, err = logic.Login(ctx, "disabled", "secret-123")
	if !errors.Is(err, ErrUserDisabled) {
		t.Fatalf("Login(disabled) error = %v, want %v", err, ErrUserDisabled)
	}

	seedAuthLoginUser(t, logic.db, "binding", "", models.UserStatusNeedsEmailBinding, false, nil)
	_, _, err = logic.Login(ctx, "binding", "secret-123")
	if !errors.Is(err, ErrEmailBindingRequired) {
		t.Fatalf("Login(needs binding) error = %v, want %v", err, ErrEmailBindingRequired)
	}

	seedAuthLoginUser(t, logic.db, "force", "force@example.com", models.UserStatusActive, true, &now)
	_, _, err = logic.Login(ctx, "force", "secret-123")
	if !errors.Is(err, ErrPasswordChangeRequired) {
		t.Fatalf("Login(force password change) error = %v, want %v", err, ErrPasswordChangeRequired)
	}
}

func TestAuthLogicCurrentUserIncludesRole(t *testing.T) {
	logic := newAuthLogicTestSubject(t)

	registered, err := logic.RegisterWithInput(context.Background(), RegisterInput{
		Username:        "alice",
		Email:           "alice@example.com",
		Password:        "secret-123",
		ConfirmPassword: "secret-123",
	})
	if err != nil {
		t.Fatalf("RegisterWithInput() error = %v", err)
	}

	loggedIn, session, err := logic.Login(context.Background(), "alice", "secret-123")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if loggedIn.Role != registered.Role {
		t.Fatalf("Login() role = %q, want %q", loggedIn.Role, registered.Role)
	}

	current, currentSession, err := logic.CurrentUser(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("CurrentUser() error = %v", err)
	}
	if currentSession.ID != session.ID {
		t.Fatalf("CurrentUser() session id = %q, want %q", currentSession.ID, session.ID)
	}
	if current.Role != registered.Role {
		t.Fatalf("CurrentUser() role = %q, want %q", current.Role, registered.Role)
	}
}

type authLogicTestOption func(*authLogicTestConfig)

type authLogicTestConfig struct {
	settings AuthSettingsReader
	mailer   *fakeAuthMailSender
	code     string
}

type fakeAuthSettings struct {
	publicRegistration PublicRegistrationSettings
}

func (s *fakeAuthSettings) GetPublicRegistration(ctx context.Context) (PublicRegistrationSettings, error) {
	return s.publicRegistration, nil
}

func withAuthTestSettings(settings AuthSettingsReader) authLogicTestOption {
	return func(cfg *authLogicTestConfig) {
		cfg.settings = settings
	}
}

func withAuthTestMailer(sender *fakeAuthMailSender, code string) authLogicTestOption {
	return func(cfg *authLogicTestConfig) {
		cfg.mailer = sender
		cfg.code = code
	}
}

func newAuthLogicTestSubject(t *testing.T, opts ...authLogicTestOption) *AuthLogic {
	t.Helper()

	var cfg authLogicTestConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}

	var authOptions []AuthOption
	if cfg.settings != nil {
		authOptions = append(authOptions, WithAuthSettings(cfg.settings))
	}
	if cfg.mailer != nil {
		code := cfg.code
		if code == "" {
			code = "123456"
		}
		verification, err := NewEmailVerificationLogic(db, EmailVerificationConfig{
			Sender: cfg.mailer,
			CodeGenerator: func() (string, error) {
				return code, nil
			},
		})
		if err != nil {
			t.Fatalf("NewEmailVerificationLogic() error = %v", err)
		}
		authOptions = append(authOptions, WithAuthEmailVerification(verification))
	}
	logic, err := NewAuthLogic(db, AuthConfig{}, authOptions...)
	if err != nil {
		t.Fatalf("NewAuthLogic() error = %v", err)
	}
	if err := logic.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	if cfg.mailer != nil {
		if err := db.AutoMigrate(&models.EmailVerification{}); err != nil {
			t.Fatalf("EmailVerification AutoMigrate() error = %v", err)
		}
	}
	return logic
}

func loadAuthTestUser(t *testing.T, db *gorm.DB, userID uint64) models.User {
	t.Helper()

	var user models.User
	if err := db.First(&user, userID).Error; err != nil {
		t.Fatalf("db.First(user) error = %v", err)
	}
	return user
}

func seedAuthLoginUser(t *testing.T, db *gorm.DB, username string, email string, status string, forcePasswordChange bool, verifiedAt *time.Time) models.User {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte("secret-123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword() error = %v", err)
	}
	user := models.User{
		Username:            username,
		Email:               email,
		DisplayName:         username,
		PasswordHash:        string(hash),
		Role:                models.UserRoleUser,
		Status:              status,
		ForcePasswordChange: forcePasswordChange,
		EmailVerifiedAt:     verifiedAt,
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("seed user %q: %v", username, err)
	}
	return user
}
