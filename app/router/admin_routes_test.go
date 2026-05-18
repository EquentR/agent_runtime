package router

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/EquentR/agent_runtime/app/logics"
	"github.com/EquentR/agent_runtime/app/models"
	"github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/EquentR/agent_runtime/pkg/secret"
	"github.com/glebarez/sqlite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRouterInitRegistersAdminBackofficeRoutes(t *testing.T) {
	db, authLogic, sessionID := newRouterAdminRouteSubject(t)
	codec, err := secret.NewCodec("test-secret")
	if err != nil {
		t.Fatalf("secret.NewCodec() error = %v", err)
	}
	settings, err := logics.NewSettingsLogic(db, logics.SettingsDefaults{}, codec)
	if err != nil {
		t.Fatalf("NewSettingsLogic() error = %v", err)
	}

	engine := rest.Init()
	Init(engine, "/api/v1", nil, Dependencies{
		AuthLogic:       authLogic,
		UserDB:          db,
		AuthSettings:    settings,
		AdminAuditLogic: logics.NewAdminAuditLogic(db),
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users", bytes.NewReader([]byte(`{
		"username":"managed",
		"email":"managed@example.com",
		"password":"secret-123"
	}`)))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: logics.DefaultAuthSessionCookieName, Value: sessionID})
	engine.ServeHTTP(recorder, request)

	envelope := decodeRouterAuthEnvelope(t, recorder)
	if !envelope.OK {
		t.Fatalf("/admin/users OK = false, code = %d, body = %s", envelope.Code, recorder.Body.String())
	}

	var created struct {
		Username            string `json:"username"`
		Status              string `json:"status"`
		ForcePasswordChange bool   `json:"force_password_change"`
	}
	if err := json.Unmarshal(envelope.Data, &created); err != nil {
		t.Fatalf("Unmarshal created user error = %v", err)
	}
	if created.Username != "managed" || created.Status != models.UserStatusPendingEmailVerification || !created.ForcePasswordChange {
		t.Fatalf("created user = %#v, want managed pending forced user", created)
	}
}

func newRouterAdminRouteSubject(t *testing.T) (*gorm.DB, *logics.AuthLogic, string) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.UserSession{}, &models.SystemSetting{}, &models.AdminAuditEvent{}); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	authLogic, err := logics.NewAuthLogic(db, logics.AuthConfig{})
	if err != nil {
		t.Fatalf("NewAuthLogic() error = %v", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte("secret-123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword() error = %v", err)
	}
	admin := models.User{
		Username:        "root-admin",
		Email:           "root-admin@example.com",
		DisplayName:     "root-admin",
		PasswordHash:    string(hash),
		Role:            models.UserRoleAdmin,
		Status:          models.UserStatusActive,
		EmailVerifiedAt: ptrTime(time.Now().UTC()),
	}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	session := models.UserSession{
		ID:        "sess_admin_routes",
		UserID:    admin.ID,
		Username:  admin.Username,
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}
	if err := db.Create(&session).Error; err != nil {
		t.Fatalf("seed admin session: %v", err)
	}
	return db, authLogic, session.ID
}

func ptrTime(value time.Time) *time.Time {
	return &value
}
