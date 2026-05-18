package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/EquentR/agent_runtime/app/logics"
	"github.com/EquentR/agent_runtime/app/models"
	"github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/EquentR/agent_runtime/pkg/secret"
	"github.com/glebarez/sqlite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func TestAdminUserHandlerListsAndFiltersUsers(t *testing.T) {
	deps, server := newAdminHandlerTestServer(t)
	adminCookie := deps.adminCookie

	alice := seedAdminHandlerUser(t, deps.db, "alice", "alice@example.com", models.UserRoleUser, models.UserStatusActive, true)
	seedAdminHandlerUser(t, deps.db, "bob", "bob@example.com", models.UserRoleUser, models.UserStatusDisabled, true)

	listResponse := doAdminRequest(t, http.MethodGet, server.URL+"/api/v1/admin/users?q=alice&status=active", nil, adminCookie)
	defer listResponse.Body.Close()
	users := decodeAdminUserListResponse(t, listResponse.Body)
	if len(users) != 1 {
		t.Fatalf("len(users) = %d, want 1", len(users))
	}
	if users[0].ID != alice.ID || users[0].Username != "alice" || users[0].Email != "alice@example.com" {
		t.Fatalf("listed user = %#v, want alice", users[0])
	}

	getResponse := doAdminRequest(t, http.MethodGet, server.URL+"/api/v1/admin/users/"+strconv.FormatUint(alice.ID, 10), nil, adminCookie)
	defer getResponse.Body.Close()
	got := decodeAdminUserResponse(t, getResponse.Body)
	if got.ID != alice.ID || got.Username != "alice" {
		t.Fatalf("got user = %#v, want alice", got)
	}
}

func TestAdminUserHandlerUpdatesRoleStatusEmailAndVerification(t *testing.T) {
	deps, server := newAdminHandlerTestServer(t)
	target := seedAdminHandlerUser(t, deps.db, "target", "target@example.com", models.UserRoleUser, models.UserStatusPendingEmailVerification, false)

	updateResponse := doAdminRequest(t, http.MethodPatch, fmt.Sprintf("%s/api/v1/admin/users/%d", server.URL, target.ID), map[string]any{
		"role":                  models.UserRoleAdmin,
		"status":                models.UserStatusActive,
		"email":                 "updated@example.com",
		"display_name":          "Updated Target",
		"email_verified":        true,
		"force_password_change": true,
	}, deps.adminCookie)
	defer updateResponse.Body.Close()
	updated := decodeAdminUserResponse(t, updateResponse.Body)
	if updated.Role != models.UserRoleAdmin || updated.Status != models.UserStatusActive {
		t.Fatalf("updated role/status = %q/%q, want admin/active", updated.Role, updated.Status)
	}
	if updated.Email != "updated@example.com" || updated.DisplayName != "Updated Target" {
		t.Fatalf("updated email/display = %q/%q", updated.Email, updated.DisplayName)
	}
	if updated.EmailVerifiedAt == nil {
		t.Fatal("updated.EmailVerifiedAt = nil, want verified timestamp")
	}
	if !updated.ForcePasswordChange {
		t.Fatal("updated.ForcePasswordChange = false, want true")
	}
	reloaded := loadAdminHandlerUser(t, deps.db, target.ID)
	if reloaded.Username != "target" {
		t.Fatalf("reloaded.Username = %q, want stable target username", reloaded.Username)
	}
}

func TestAdminUserHandlerChangingEmailClearsVerificationByDefault(t *testing.T) {
	deps, server := newAdminHandlerTestServer(t)
	target := seedAdminHandlerUser(t, deps.db, "verified", "verified@example.com", models.UserRoleUser, models.UserStatusActive, true)

	updateResponse := doAdminRequest(t, http.MethodPatch, fmt.Sprintf("%s/api/v1/admin/users/%d", server.URL, target.ID), map[string]any{
		"email": "new-verified@example.com",
	}, deps.adminCookie)
	defer updateResponse.Body.Close()
	updated := decodeAdminUserResponse(t, updateResponse.Body)
	if updated.EmailVerifiedAt != nil {
		t.Fatalf("updated.EmailVerifiedAt = %v, want nil after email change", *updated.EmailVerifiedAt)
	}
	if updated.Status != models.UserStatusPendingEmailVerification {
		t.Fatalf("updated.Status = %q, want %q", updated.Status, models.UserStatusPendingEmailVerification)
	}

	activeResponse := doAdminRequest(t, http.MethodPatch, fmt.Sprintf("%s/api/v1/admin/users/%d", server.URL, target.ID), map[string]any{
		"email":  "second-verified@example.com",
		"status": models.UserStatusActive,
	}, deps.adminCookie)
	defer activeResponse.Body.Close()
	activeUpdate := decodeAdminUserResponse(t, activeResponse.Body)
	if activeUpdate.EmailVerifiedAt != nil {
		t.Fatalf("activeUpdate.EmailVerifiedAt = %v, want nil after second email change", *activeUpdate.EmailVerifiedAt)
	}
	if activeUpdate.Status != models.UserStatusPendingEmailVerification {
		t.Fatalf("activeUpdate.Status = %q, want %q despite requested active without verification", activeUpdate.Status, models.UserStatusPendingEmailVerification)
	}
}

func TestAdminUserHandlerRejectsUsernameEmailCrossFieldConflicts(t *testing.T) {
	deps, server := newAdminHandlerTestServer(t)
	seedAdminHandlerUser(t, deps.db, "Taken@Example.COM", "owner@example.com", models.UserRoleUser, models.UserStatusActive, true)
	target := seedAdminHandlerUser(t, deps.db, "target", "target@example.com", models.UserRoleUser, models.UserStatusActive, true)

	createResponse := doAdminRequest(t, http.MethodPost, server.URL+"/api/v1/admin/users", map[string]any{
		"username": "created-conflict",
		"email":    "taken@example.com",
		"password": "initial-secret",
	}, deps.adminCookie)
	defer createResponse.Body.Close()
	createEnvelope := decodeEnvelope(t, createResponse.Body)
	if createEnvelope.OK {
		t.Fatal("create with email matching existing username OK = true, want conflict")
	}

	updateResponse := doAdminRequest(t, http.MethodPatch, fmt.Sprintf("%s/api/v1/admin/users/%d", server.URL, target.ID), map[string]any{
		"email": "taken@example.com",
	}, deps.adminCookie)
	defer updateResponse.Body.Close()
	updateEnvelope := decodeEnvelope(t, updateResponse.Body)
	if updateEnvelope.OK {
		t.Fatal("update email matching existing username OK = true, want conflict")
	}
}

func TestAdminUserMutationRollsBackWhenAuditFails(t *testing.T) {
	deps, server := newAdminHandlerTestServerWithoutAuditTable(t)
	target := seedAdminHandlerUser(t, deps.db, "rollback", "rollback@example.com", models.UserRoleUser, models.UserStatusActive, true)

	response := doAdminRequest(t, http.MethodPatch, fmt.Sprintf("%s/api/v1/admin/users/%d", server.URL, target.ID), map[string]any{
		"status": models.UserStatusDisabled,
	}, deps.adminCookie)
	defer response.Body.Close()
	envelope := decodeEnvelope(t, response.Body)
	if envelope.OK {
		t.Fatal("update with failing audit OK = true, want false")
	}

	reloaded := loadAdminHandlerUser(t, deps.db, target.ID)
	if reloaded.Status != models.UserStatusActive {
		t.Fatalf("reloaded.Status = %q, want rollback to %q", reloaded.Status, models.UserStatusActive)
	}
}

func TestAdminUserHandlerResetsPasswordAndForcesPasswordChange(t *testing.T) {
	deps, server := newAdminHandlerTestServer(t)
	target := seedAdminHandlerUser(t, deps.db, "resetme", "resetme@example.com", models.UserRoleUser, models.UserStatusActive, true)
	oldHash := target.PasswordHash

	resetResponse := doAdminRequest(t, http.MethodPost, fmt.Sprintf("%s/api/v1/admin/users/%d/reset-password", server.URL, target.ID), map[string]any{
		"password": "new-secret-123",
	}, deps.adminCookie)
	defer resetResponse.Body.Close()
	reset := decodeAdminUserResponse(t, resetResponse.Body)
	if !reset.ForcePasswordChange {
		t.Fatal("reset.ForcePasswordChange = false, want true")
	}
	reloaded := loadAdminHandlerUser(t, deps.db, target.ID)
	if reloaded.PasswordHash == oldHash {
		t.Fatal("password hash did not change")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(reloaded.PasswordHash), []byte("new-secret-123")); err != nil {
		t.Fatalf("new password hash compare error = %v", err)
	}
}

func TestAdminUserHandlerRejectsNonAdmin(t *testing.T) {
	deps, server := newAdminHandlerTestServer(t)
	user := seedAdminHandlerUser(t, deps.db, "regular", "regular@example.com", models.UserRoleUser, models.UserStatusActive, true)
	_, regularSession, err := deps.authLogic.Login(context.Background(), user.Username, "secret-123")
	if err != nil {
		t.Fatalf("Login(regular) error = %v", err)
	}

	response := doAdminRequest(t, http.MethodGet, server.URL+"/api/v1/admin/users", nil, &http.Cookie{Name: authSessionCookieName, Value: regularSession.ID})
	defer response.Body.Close()
	envelope := decodeEnvelope(t, response.Body)
	if envelope.OK {
		t.Fatal("non-admin admin/users ok = true, want false")
	}
}

func TestAdminUserMutationsWriteAuditEvents(t *testing.T) {
	deps, server := newAdminHandlerTestServer(t)
	target := seedAdminHandlerUser(t, deps.db, "audited", "audited@example.com", models.UserRoleUser, models.UserStatusActive, true)

	createResponse := doAdminRequest(t, http.MethodPost, server.URL+"/api/v1/admin/users", map[string]any{
		"username": "created-by-admin",
		"email":    "created-by-admin@example.com",
		"password": "initial-secret",
	}, deps.adminCookie)
	defer createResponse.Body.Close()
	created := decodeAdminUserResponse(t, createResponse.Body)
	if !created.ForcePasswordChange || created.Status != models.UserStatusPendingEmailVerification {
		t.Fatalf("created user = %#v, want forced password change and pending verification", created)
	}

	updateResponse := doAdminRequest(t, http.MethodPatch, fmt.Sprintf("%s/api/v1/admin/users/%d", server.URL, target.ID), map[string]any{
		"status": models.UserStatusDisabled,
	}, deps.adminCookie)
	defer updateResponse.Body.Close()
	_ = decodeAdminUserResponse(t, updateResponse.Body)

	resetResponse := doAdminRequest(t, http.MethodPost, fmt.Sprintf("%s/api/v1/admin/users/%d/reset-password", server.URL, target.ID), map[string]any{
		"password": "another-secret",
	}, deps.adminCookie)
	defer resetResponse.Body.Close()
	_ = decodeAdminUserResponse(t, resetResponse.Body)

	var events []models.AdminAuditEvent
	if err := deps.db.Order("id asc").Find(&events).Error; err != nil {
		t.Fatalf("load audit events: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("len(audit events) = %d, want 3", len(events))
	}
	wantActions := []string{"admin.users.create", "admin.users.update", "admin.users.reset_password"}
	for idx, want := range wantActions {
		if events[idx].Action != want {
			t.Fatalf("events[%d].Action = %q, want %q", idx, events[idx].Action, want)
		}
	}
	stored := string(events[0].AfterJSON) + string(events[2].AfterJSON)
	if bytes.Contains([]byte(stored), []byte("initial-secret")) || bytes.Contains([]byte(stored), []byte("another-secret")) {
		t.Fatalf("audit JSON leaked password: %s", stored)
	}
}

type adminHandlerTestDeps struct {
	db          *gorm.DB
	authLogic   *logics.AuthLogic
	settings    *logics.SettingsLogic
	auditLogic  *logics.AdminAuditLogic
	adminCookie *http.Cookie
}

type adminUserTestResponse struct {
	ID                  uint64  `json:"id"`
	Username            string  `json:"username"`
	Email               string  `json:"email"`
	DisplayName         string  `json:"display_name"`
	Role                string  `json:"role"`
	Status              string  `json:"status"`
	EmailVerifiedAt     *string `json:"email_verified_at"`
	ForcePasswordChange bool    `json:"force_password_change"`
}

func newAdminHandlerTestServer(t *testing.T) (*adminHandlerTestDeps, *httptest.Server) {
	return newAdminHandlerTestServerWithAuditTable(t, true)
}

func newAdminHandlerTestServerWithoutAuditTable(t *testing.T) (*adminHandlerTestDeps, *httptest.Server) {
	return newAdminHandlerTestServerWithAuditTable(t, false)
}

func newAdminHandlerTestServerWithAuditTable(t *testing.T, migrateAuditTable bool) (*adminHandlerTestDeps, *httptest.Server) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	migrations := []any{&models.User{}, &models.UserSession{}, &models.SystemSetting{}, &models.EmailVerification{}}
	if migrateAuditTable {
		migrations = append(migrations, &models.AdminAuditEvent{})
	}
	if err := db.AutoMigrate(migrations...); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	codec, err := secret.NewCodec("test-secret")
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	settings, err := logics.NewSettingsLogic(db, logics.SettingsDefaults{}, codec)
	if err != nil {
		t.Fatalf("NewSettingsLogic() error = %v", err)
	}
	emailVerification, err := logics.NewEmailVerificationLogic(db, logics.EmailVerificationConfig{
		Sender: &fakeHandlerMailSender{},
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
	auditLogic := logics.NewAdminAuditLogic(db)
	admin := seedAdminHandlerUser(t, db, "root", "root@example.com", models.UserRoleAdmin, models.UserStatusActive, true)
	_, session, err := authLogic.Login(context.Background(), admin.Username, "secret-123")
	if err != nil {
		t.Fatalf("Login(admin) error = %v", err)
	}

	authMiddleware := NewAuthMiddleware(authLogic)
	engine := rest.Init()
	group := engine.Group("/api/v1")
	NewAdminUserHandler(db, auditLogic, emailVerification, authMiddleware.RequireAdmin()).Register(group)
	NewAdminSettingsHandler(settings, auditLogic, &fakeAdminSettingsSMTPTester{}, authMiddleware.RequireAdmin()).Register(group)
	NewAdminAuditEventHandler(auditLogic, authMiddleware.RequireAdmin()).Register(group)
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)

	return &adminHandlerTestDeps{
		db:          db,
		authLogic:   authLogic,
		settings:    settings,
		auditLogic:  auditLogic,
		adminCookie: &http.Cookie{Name: authSessionCookieName, Value: session.ID},
	}, server
}

func seedAdminHandlerUser(t *testing.T, db *gorm.DB, username string, email string, role string, status string, verified bool) models.User {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("secret-123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword() error = %v", err)
	}
	var verifiedAt *time.Time
	if verified {
		now := time.Now().UTC()
		verifiedAt = &now
	}
	user := models.User{
		Username:        username,
		Email:           email,
		DisplayName:     username,
		PasswordHash:    string(hash),
		Role:            role,
		Status:          status,
		EmailVerifiedAt: verifiedAt,
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("seed user %q: %v", username, err)
	}
	return user
}

func loadAdminHandlerUser(t *testing.T, db *gorm.DB, id uint64) models.User {
	t.Helper()
	var user models.User
	if err := db.First(&user, id).Error; err != nil {
		t.Fatalf("load user %d: %v", id, err)
	}
	return user
}

func doAdminRequest(t *testing.T, method string, url string, payload map[string]any, cookie *http.Cookie) *http.Response {
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

func decodeAdminUserResponse(t *testing.T, body io.Reader) adminUserTestResponse {
	t.Helper()
	envelope := decodeEnvelope(t, body)
	if !envelope.OK {
		t.Fatalf("response ok = false, message = %s, raw data = %s", envelope.Message, string(envelope.Data))
	}
	var user adminUserTestResponse
	if err := json.Unmarshal(envelope.Data, &user); err != nil {
		t.Fatalf("Unmarshal(user) error = %v", err)
	}
	return user
}

func decodeAdminUserListResponse(t *testing.T, body io.Reader) []adminUserTestResponse {
	t.Helper()
	envelope := decodeEnvelope(t, body)
	if !envelope.OK {
		t.Fatalf("response ok = false, message = %s, raw data = %s", envelope.Message, string(envelope.Data))
	}
	var users []adminUserTestResponse
	if err := json.Unmarshal(envelope.Data, &users); err != nil {
		t.Fatalf("Unmarshal(users) error = %v", err)
	}
	return users
}
