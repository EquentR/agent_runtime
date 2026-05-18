package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/EquentR/agent_runtime/app/logics"
	"github.com/EquentR/agent_runtime/app/models"
	"github.com/EquentR/agent_runtime/pkg/mail"
	"github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/glebarez/sqlite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func TestUserHandlerReturnsProfileAndRequiredActions(t *testing.T) {
	deps, server := newUserHandlerTestServer(t)
	user := seedUserHandlerUser(t, deps.db, userHandlerSeedUser{
		username:            "legacy",
		email:               "",
		status:              models.UserStatusNeedsEmailBinding,
		forcePasswordChange: true,
		verified:            false,
	})
	session := createUserHandlerSession(t, deps.db, user)

	response := doUserRequest(t, http.MethodGet, server.URL+"/api/v1/users/me", nil, session)
	defer response.Body.Close()
	profile := decodeUserProfileResponse(t, response.Body)

	if profile.ID != user.ID || profile.Username != "legacy" {
		t.Fatalf("profile identity = %#v, want seeded user", profile)
	}
	if profile.Email != "" || profile.DisplayName != "legacy" || profile.Status != models.UserStatusNeedsEmailBinding {
		t.Fatalf("profile fields = %#v, want needs email binding legacy user", profile)
	}
	if profile.EmailVerified {
		t.Fatal("profile.EmailVerified = true, want false")
	}
	if !profile.ForcePasswordChange {
		t.Fatal("profile.ForcePasswordChange = false, want true")
	}
	wantActions := []string{"bind_email", "change_password"}
	if !reflect.DeepEqual(profile.RequiredActions, wantActions) {
		t.Fatalf("required actions = %#v, want %#v", profile.RequiredActions, wantActions)
	}
}

func TestUserHandlerUpdatesDisplayName(t *testing.T) {
	deps, server := newUserHandlerTestServer(t)
	user := seedUserHandlerUser(t, deps.db, userHandlerSeedUser{
		username: "alice",
		email:    "alice@example.com",
		status:   models.UserStatusActive,
		verified: true,
	})
	session := createUserHandlerSession(t, deps.db, user)

	response := doUserRequest(t, http.MethodPatch, server.URL+"/api/v1/users/me", map[string]any{
		"display_name": " Alice Doe ",
	}, session)
	defer response.Body.Close()
	profile := decodeUserProfileResponse(t, response.Body)

	if profile.DisplayName != "Alice Doe" {
		t.Fatalf("profile.DisplayName = %q, want trimmed Alice Doe", profile.DisplayName)
	}
	reloaded := loadUserHandlerUser(t, deps.db, user.ID)
	if reloaded.DisplayName != "Alice Doe" {
		t.Fatalf("reloaded.DisplayName = %q, want Alice Doe", reloaded.DisplayName)
	}
}

func TestUserHandlerChangesPassword(t *testing.T) {
	deps, server := newUserHandlerTestServer(t)
	user := seedUserHandlerUser(t, deps.db, userHandlerSeedUser{
		username:            "forced",
		email:               "forced@example.com",
		status:              models.UserStatusActive,
		forcePasswordChange: true,
		verified:            true,
	})
	session := createUserHandlerSession(t, deps.db, user)
	oldHash := user.PasswordHash

	response := doUserRequest(t, http.MethodPost, server.URL+"/api/v1/users/me/password", map[string]any{
		"current_password": "secret-123",
		"password":         "new-secret-123",
		"confirm_password": "new-secret-123",
	}, session)
	defer response.Body.Close()
	profile := decodeUserProfileResponse(t, response.Body)

	if profile.ForcePasswordChange {
		t.Fatal("profile.ForcePasswordChange = true, want cleared after password change")
	}
	reloaded := loadUserHandlerUser(t, deps.db, user.ID)
	if reloaded.PasswordHash == oldHash {
		t.Fatal("password hash did not change")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(reloaded.PasswordHash), []byte("new-secret-123")); err != nil {
		t.Fatalf("new password hash compare error = %v", err)
	}
	if _, _, err := deps.authLogic.Login(context.Background(), user.Username, "new-secret-123"); err != nil {
		t.Fatalf("Login(new password) error = %v", err)
	}
}

func TestUserHandlerStartsAndVerifiesEmailBinding(t *testing.T) {
	deps, server := newUserHandlerTestServer(t)
	user := seedUserHandlerUser(t, deps.db, userHandlerSeedUser{
		username: "legacy",
		status:   models.UserStatusNeedsEmailBinding,
		verified: false,
	})
	session := createUserHandlerSession(t, deps.db, user)

	start := doUserRequest(t, http.MethodPost, server.URL+"/api/v1/users/me/email-verification", map[string]any{
		"email": " Bound@Example.COM ",
	}, session)
	defer start.Body.Close()
	envelope := decodeEnvelope(t, start.Body)
	if !envelope.OK {
		t.Fatalf("start email binding ok = false, message = %s", envelope.Message)
	}
	if len(deps.mailer.messages) != 1 || deps.mailer.messages[0].To != "bound@example.com" {
		t.Fatalf("sent messages = %#v, want one normalized email verification", deps.mailer.messages)
	}

	confirm := doUserRequest(t, http.MethodPost, server.URL+"/api/v1/users/me/email-verification/confirm", map[string]any{
		"email": "bound@example.com",
		"code":  "123456",
	}, session)
	defer confirm.Body.Close()
	profile := decodeUserProfileResponse(t, confirm.Body)

	if profile.Email != "bound@example.com" || profile.Status != models.UserStatusActive {
		t.Fatalf("profile after verification = %#v, want active bound email", profile)
	}
	if !profile.EmailVerified || profile.EmailVerifiedAt == nil {
		t.Fatalf("profile email verification = %v/%v, want verified timestamp", profile.EmailVerified, profile.EmailVerifiedAt)
	}
	if len(profile.RequiredActions) != 0 {
		t.Fatalf("required actions after binding = %#v, want empty", profile.RequiredActions)
	}
}

func TestUserHandlerStartsAndVerifiesPendingRegistrationEmail(t *testing.T) {
	deps, server := newUserHandlerTestServer(t)
	user := seedUserHandlerUser(t, deps.db, userHandlerSeedUser{
		username: "pending",
		email:    "pending@example.com",
		status:   models.UserStatusPendingEmailVerification,
		verified: false,
	})
	session := createUserHandlerSession(t, deps.db, user)

	start := doUserRequest(t, http.MethodPost, server.URL+"/api/v1/users/me/email-verification", map[string]any{
		"email": " pending@example.com ",
	}, session)
	defer start.Body.Close()
	if !decodeEnvelope(t, start.Body).OK {
		t.Fatal("start pending registration verification ok = false, want true")
	}
	if len(deps.mailer.messages) != 1 || deps.mailer.messages[0].To != "pending@example.com" {
		t.Fatalf("sent messages = %#v, want registration verification to pending email", deps.mailer.messages)
	}

	confirm := doUserRequest(t, http.MethodPost, server.URL+"/api/v1/users/me/email-verification/confirm", map[string]any{
		"email": "pending@example.com",
		"code":  "123456",
	}, session)
	defer confirm.Body.Close()
	profile := decodeUserProfileResponse(t, confirm.Body)

	if profile.Status != models.UserStatusActive || !profile.EmailVerified {
		t.Fatalf("profile after registration verification = %#v, want active verified user", profile)
	}
	if len(profile.RequiredActions) != 0 {
		t.Fatalf("required actions after registration verification = %#v, want empty", profile.RequiredActions)
	}
}

type userHandlerTestDeps struct {
	db                *gorm.DB
	authLogic         *logics.AuthLogic
	emailVerification *logics.EmailVerificationLogic
	mailer            *userHandlerMailSender
}

type userHandlerProfileResponse struct {
	ID                  uint64   `json:"id"`
	Username            string   `json:"username"`
	Email               string   `json:"email"`
	DisplayName         string   `json:"display_name"`
	Role                string   `json:"role"`
	Status              string   `json:"status"`
	EmailVerified       bool     `json:"email_verified"`
	EmailVerifiedAt     *string  `json:"email_verified_at"`
	ForcePasswordChange bool     `json:"force_password_change"`
	RequiredActions     []string `json:"required_actions"`
}

type userHandlerSeedUser struct {
	username            string
	email               string
	status              string
	forcePasswordChange bool
	verified            bool
}

type userHandlerMailSender struct {
	messages []mail.Message
}

func (s *userHandlerMailSender) Send(ctx context.Context, message mail.Message) error {
	s.messages = append(s.messages, message)
	return nil
}

func newUserHandlerTestServer(t *testing.T) (*userHandlerTestDeps, *httptest.Server) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.UserSession{}, &models.EmailVerification{}); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	mailer := &userHandlerMailSender{}
	emailVerification, err := logics.NewEmailVerificationLogic(db, logics.EmailVerificationConfig{
		Sender: mailer,
		CodeGenerator: func() (string, error) {
			return "123456", nil
		},
	})
	if err != nil {
		t.Fatalf("NewEmailVerificationLogic() error = %v", err)
	}
	authLogic, err := logics.NewAuthLogic(db, logics.AuthConfig{CookieName: authSessionCookieName}, logics.WithAuthEmailVerification(emailVerification))
	if err != nil {
		t.Fatalf("NewAuthLogic() error = %v", err)
	}

	authMiddleware := NewAuthMiddleware(authLogic)
	engine := rest.Init()
	group := engine.Group("/api/v1")
	NewUserHandler(db, emailVerification, authMiddleware.RequireSession()).Register(group)
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)

	return &userHandlerTestDeps{
		db:                db,
		authLogic:         authLogic,
		emailVerification: emailVerification,
		mailer:            mailer,
	}, server
}

func seedUserHandlerUser(t *testing.T, db *gorm.DB, input userHandlerSeedUser) models.User {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("secret-123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword() error = %v", err)
	}
	status := strings.TrimSpace(input.status)
	if status == "" {
		status = models.UserStatusActive
	}
	displayName := strings.TrimSpace(input.username)
	var verifiedAt *time.Time
	if input.verified {
		now := time.Now().UTC()
		verifiedAt = &now
	}
	user := models.User{
		Username:            strings.TrimSpace(input.username),
		Email:               strings.ToLower(strings.TrimSpace(input.email)),
		DisplayName:         displayName,
		PasswordHash:        string(hash),
		Role:                models.UserRoleUser,
		Status:              status,
		ForcePasswordChange: input.forcePasswordChange,
		EmailVerifiedAt:     verifiedAt,
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("seed user %q: %v", input.username, err)
	}
	return user
}

func createUserHandlerSession(t *testing.T, db *gorm.DB, user models.User) *http.Cookie {
	t.Helper()
	session := models.UserSession{
		ID:        fmt.Sprintf("sess_%d_%s", user.ID, user.Username),
		UserID:    user.ID,
		Username:  user.Username,
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}
	if err := db.Create(&session).Error; err != nil {
		t.Fatalf("create session for %q: %v", user.Username, err)
	}
	return &http.Cookie{Name: authSessionCookieName, Value: session.ID}
}

func loadUserHandlerUser(t *testing.T, db *gorm.DB, id uint64) models.User {
	t.Helper()
	var user models.User
	if err := db.First(&user, id).Error; err != nil {
		t.Fatalf("load user %d: %v", id, err)
	}
	return user
}

func doUserRequest(t *testing.T, method string, url string, payload map[string]any, cookie *http.Cookie) *http.Response {
	t.Helper()
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("json.Marshal() error = %v", err)
		}
		body = bytes.NewReader(raw)
	}
	request, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	return response
}

func decodeUserProfileResponse(t *testing.T, body io.Reader) userHandlerProfileResponse {
	t.Helper()
	envelope := decodeEnvelope(t, body)
	if !envelope.OK {
		t.Fatalf("response ok = false, message = %s, raw data = %s", envelope.Message, string(envelope.Data))
	}
	var profile userHandlerProfileResponse
	if err := json.Unmarshal(envelope.Data, &profile); err != nil {
		t.Fatalf("Unmarshal(profile) error = %v", err)
	}
	return profile
}
