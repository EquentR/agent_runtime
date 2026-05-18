package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/EquentR/agent_runtime/app/logics"
	"github.com/EquentR/agent_runtime/app/models"
	coreagent "github.com/EquentR/agent_runtime/core/agent"
	coretypes "github.com/EquentR/agent_runtime/core/types"
	"github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/glebarez/sqlite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type routerAuthEnvelope struct {
	Code int             `json:"code"`
	OK   bool            `json:"ok"`
	Data json.RawMessage `json:"data"`
}

func TestRouterInitUsesActiveUserGateForCoreRoutesAndKeepsAuthMeSessionOnly(t *testing.T) {
	for _, status := range []string{models.UserStatusPendingEmailVerification, models.UserStatusDisabled} {
		t.Run(status, func(t *testing.T) {
			engine := rest.Init()
			authLogic, sessionID := newRouterAuthGateSubject(t, status)
			Init(engine, "/api/v1", nil, Dependencies{
				AuthLogic: authLogic,
				ModelResolver: &coreagent.ModelResolver{Providers: []coretypes.LLMProvider{{
					BaseProvider: coretypes.BaseProvider{Name: "openai"},
					Models: []coretypes.LLMModel{{
						BaseModel: coretypes.BaseModel{ID: "gpt-5.4", Name: "GPT 5.4"},
						Type:      coretypes.LLMTypeOpenAIResponses,
					}},
				}}},
			})

			meRecorder := httptest.NewRecorder()
			meRequest := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
			meRequest.AddCookie(&http.Cookie{Name: logics.DefaultAuthSessionCookieName, Value: sessionID})
			engine.ServeHTTP(meRecorder, meRequest)
			meEnvelope := decodeRouterAuthEnvelope(t, meRecorder)
			if !meEnvelope.OK {
				t.Fatalf("/auth/me OK = false, code = %d, want session-only access", meEnvelope.Code)
			}

			modelsRecorder := httptest.NewRecorder()
			modelsRequest := httptest.NewRequest(http.MethodGet, "/api/v1/models", nil)
			modelsRequest.AddCookie(&http.Cookie{Name: logics.DefaultAuthSessionCookieName, Value: sessionID})
			engine.ServeHTTP(modelsRecorder, modelsRequest)
			modelsEnvelope := decodeRouterAuthEnvelope(t, modelsRecorder)
			if modelsEnvelope.OK {
				t.Fatal("/models OK = true, want active-user gate denial")
			}
			if modelsEnvelope.Code != http.StatusForbidden {
				t.Fatalf("/models code = %d, want %d", modelsEnvelope.Code, http.StatusForbidden)
			}
		})
	}
}

func newRouterAuthGateSubject(t *testing.T, status string) (*logics.AuthLogic, string) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"_"+status+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	authLogic, err := logics.NewAuthLogic(db, logics.AuthConfig{})
	if err != nil {
		t.Fatalf("NewAuthLogic() error = %v", err)
	}
	if err := authLogic.AutoMigrate(); err != nil {
		t.Fatalf("AuthLogic.AutoMigrate() error = %v", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte("secret-123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword() error = %v", err)
	}
	user := models.User{
		Username:     "blocked_" + status,
		Email:        status + "@example.com",
		DisplayName:  status,
		PasswordHash: string(hash),
		Role:         models.UserRoleUser,
		Status:       status,
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("seed blocked user: %v", err)
	}
	session := models.UserSession{
		ID:        "sess_" + status,
		UserID:    user.ID,
		Username:  user.Username,
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}
	if err := db.Create(&session).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	return authLogic, session.ID
}

func decodeRouterAuthEnvelope(t *testing.T, recorder *httptest.ResponseRecorder) routerAuthEnvelope {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, want 200, body = %s", recorder.Code, recorder.Body.String())
	}
	var envelope routerAuthEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("Unmarshal envelope error = %v, body = %s", err, recorder.Body.String())
	}
	return envelope
}
