package commands

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/EquentR/agent_runtime/app/config"
	"github.com/EquentR/agent_runtime/app/logics"
	"github.com/EquentR/agent_runtime/app/models"
	"github.com/EquentR/agent_runtime/app/router"
	coreagent "github.com/EquentR/agent_runtime/core/agent"
	"github.com/EquentR/agent_runtime/core/approvals"
	"github.com/EquentR/agent_runtime/core/attachments"
	coreaudit "github.com/EquentR/agent_runtime/core/audit"
	"github.com/EquentR/agent_runtime/core/interactions"
	model "github.com/EquentR/agent_runtime/core/providers/types"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	coretools "github.com/EquentR/agent_runtime/core/tools"
	builtin "github.com/EquentR/agent_runtime/core/tools/builtin"
	coretypes "github.com/EquentR/agent_runtime/core/types"
	"github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/EquentR/agent_runtime/pkg/secret"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type serveRouterEnvelope struct {
	Code int             `json:"code"`
	OK   bool            `json:"ok"`
	Data json.RawMessage `json:"data"`
}

func assertServeRouteUnauthorized(t *testing.T, handler http.Handler, method string, path string) {
	t.Helper()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, nil)
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("%s %s status = %d, want 200 so route is registered; body = %s", method, path, recorder.Code, recorder.Body.String())
	}
	var envelope serveRouterEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("%s %s envelope unmarshal error = %v, body = %s", method, path, err, recorder.Body.String())
	}
	if envelope.OK {
		t.Fatalf("%s %s OK = true, want unauthorized failure", method, path)
	}
	if envelope.Code != http.StatusUnauthorized {
		t.Fatalf("%s %s envelope.Code = %d, want %d; body = %s", method, path, envelope.Code, http.StatusUnauthorized, recorder.Body.String())
	}
}

func assertServeRouteWithSession(t *testing.T, handler http.Handler, method string, path string, sessionID string, wantCode int) serveRouterEnvelope {
	t.Helper()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, nil)
	request.AddCookie(&http.Cookie{Name: logics.DefaultAuthSessionCookieName, Value: sessionID})
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("%s %s status = %d, want 200; body = %s", method, path, recorder.Code, recorder.Body.String())
	}
	var envelope serveRouterEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("%s %s envelope unmarshal error = %v, body = %s", method, path, err, recorder.Body.String())
	}
	if envelope.Code != wantCode {
		t.Fatalf("%s %s envelope.Code = %d, want %d; body = %s", method, path, envelope.Code, wantCode, recorder.Body.String())
	}
	return envelope
}

func assertServeRouteOKWithSession(t *testing.T, handler http.Handler, method string, path string, sessionID string) serveRouterEnvelope {
	t.Helper()

	envelope := assertServeRouteWithSession(t, handler, method, path, sessionID, http.StatusOK)
	if !envelope.OK {
		t.Fatalf("%s %s OK = false, want true; data = %s", method, path, string(envelope.Data))
	}
	return envelope
}

func TestBuildLLMClientFactoryUsesConfiguredRequestTimeout(t *testing.T) {
	factory := buildLLMClientFactory(50 * time.Millisecond)

	t.Run("responses", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/responses" {
				http.NotFound(w, r)
				return
			}
			time.Sleep(150 * time.Millisecond)
		}))
		defer server.Close()

		provider := &coretypes.LLMProvider{BaseProvider: coretypes.BaseProvider{Name: "openai", BaseUrl: server.URL, APIKey: "test-key"}}
		llmModel := &coretypes.LLMModel{BaseModel: coretypes.BaseModel{ID: "gpt-5.4", Name: "GPT 5.4"}, Type: coretypes.LLMTypeOpenAIResponses}
		client, err := factory(provider, llmModel)
		if err != nil {
			t.Fatalf("factory(responses) error = %v", err)
		}

		_, err = client.Chat(context.Background(), model.ChatRequest{Model: "gpt-5.4", Messages: []model.Message{{Role: model.RoleUser, Content: "hello"}}})
		if err == nil {
			t.Fatal("responses Chat() error = nil, want timeout error")
		}
		lower := strings.ToLower(err.Error())
		if !strings.Contains(lower, "timeout") && !strings.Contains(lower, "deadline") {
			t.Fatalf("responses Chat() error = %v, want timeout/deadline error", err)
		}
	})

	t.Run("completions", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/chat/completions" {
				http.NotFound(w, r)
				return
			}
			time.Sleep(150 * time.Millisecond)
		}))
		defer server.Close()

		provider := &coretypes.LLMProvider{BaseProvider: coretypes.BaseProvider{Name: "openai", BaseUrl: server.URL + "/v1", APIKey: "test-key"}}
		llmModel := &coretypes.LLMModel{BaseModel: coretypes.BaseModel{ID: "glm-5", Name: "GLM-5"}, Type: coretypes.LLMTypeOpenAICompletions}
		client, err := factory(provider, llmModel)
		if err != nil {
			t.Fatalf("factory(completions) error = %v", err)
		}

		_, err = client.Chat(context.Background(), model.ChatRequest{Model: "glm-5", Messages: []model.Message{{Role: model.RoleUser, Content: "hello"}}})
		if err == nil {
			t.Fatal("completions Chat() error = nil, want timeout error")
		}
		lower := strings.ToLower(err.Error())
		if !strings.Contains(lower, "timeout") && !strings.Contains(lower, "deadline") {
			t.Fatalf("completions Chat() error = %v, want timeout/deadline error", err)
		}
	})
}

func TestBuildConfiguredLLMClientFactoryUsesResolvedTimeout(t *testing.T) {
	factory := buildConfiguredLLMClientFactory(&config.Config{LLMRequestTimeout: 50 * time.Millisecond})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		time.Sleep(150 * time.Millisecond)
	}))
	defer server.Close()

	provider := &coretypes.LLMProvider{BaseProvider: coretypes.BaseProvider{Name: "openai", BaseUrl: server.URL + "/v1", APIKey: "test-key"}}
	llmModel := &coretypes.LLMModel{BaseModel: coretypes.BaseModel{ID: "glm-5", Name: "GLM-5"}, Type: coretypes.LLMTypeOpenAICompletions}
	client, err := factory(provider, llmModel)
	if err != nil {
		t.Fatalf("factory(completions) error = %v", err)
	}

	_, err = client.Chat(context.Background(), model.ChatRequest{Model: "glm-5", Messages: []model.Message{{Role: model.RoleUser, Content: "hello"}}})
	if err == nil {
		t.Fatal("completions Chat() error = nil, want timeout error")
	}
	lower := strings.ToLower(err.Error())
	if !strings.Contains(lower, "timeout") && !strings.Contains(lower, "deadline") {
		t.Fatalf("completions Chat() error = %v, want timeout/deadline error", err)
	}
}

func TestPromptBuildRouterDependenciesExposePromptRuntime(t *testing.T) {
	db := newServeTestDB(t)
	promptRuntime, err := initPromptRuntime(db)
	if err != nil {
		t.Fatalf("initPromptRuntime() error = %v", err)
	}

	deps := buildRouterDependencies(nil, nil, nil, nil, 0, nil, nil, nil, promptRuntime.Store, promptRuntime.Resolver, nil, nil, nil)
	if deps.PromptStore != promptRuntime.Store {
		t.Fatalf("PromptStore = %#v, want %#v", deps.PromptStore, promptRuntime.Store)
	}
	if deps.PromptResolver != promptRuntime.Resolver {
		t.Fatalf("PromptResolver = %#v, want %#v", deps.PromptResolver, promptRuntime.Resolver)
	}
}

func TestBuildRouterDependenciesExposeApprovalStore(t *testing.T) {
	db := newServeTestDB(t)
	approvalStore := approvals.NewStore(db)
	deps := buildRouterDependencies(nil, approvalStore, nil, nil, 0, nil, nil, nil, nil, nil, nil, nil, nil)
	if deps.ApprovalStore != approvalStore {
		t.Fatalf("ApprovalStore = %#v, want %#v", deps.ApprovalStore, approvalStore)
	}
}

func TestBuildRouterDependenciesExposeModelLogicAndResolver(t *testing.T) {
	db := newServeTestDB(t)
	if err := db.AutoMigrate(&models.SystemSetting{}, &models.LLMModelOverride{}, &models.CustomLLMModel{}); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	runtime, err := initAuthRuntime(db, config.SecurityConfig{AppSecret: "test-secret"})
	if err != nil {
		t.Fatalf("initAuthRuntime() error = %v", err)
	}
	resolver := &coreagent.ModelResolver{Providers: []coretypes.LLMProvider{{
		BaseProvider: coretypes.BaseProvider{Name: "yaml"},
		Models: []coretypes.LLMModel{{
			BaseModel: coretypes.BaseModel{ID: "global", Name: "Global"},
			Type:      coretypes.LLMTypeOpenAIResponses,
			Scope:     "global",
		}},
	}}}

	deps := buildRouterDependencies(nil, nil, nil, nil, 0, nil, nil, resolver, nil, nil, nil, runtime.AuthLogic, runtime)
	if deps.ModelLogic == nil {
		t.Fatal("deps.ModelLogic = nil, want runtime model logic")
	}
	if deps.ModelResolver == nil {
		t.Fatal("deps.ModelResolver = nil, want model logic resolver")
	}
	if len(deps.ModelResolver.Providers) != 1 || deps.ModelResolver.Providers[0].ProviderName() != "yaml" {
		t.Fatalf("deps.ModelResolver providers = %#v, want yaml provider passthrough", deps.ModelResolver.Providers)
	}
	resolvedModel, err := deps.ModelResolver.ResolveContext(context.Background(), "yaml", "global")
	if err != nil {
		t.Fatalf("deps.ModelResolver.ResolveContext() error = %v", err)
	}
	if resolvedModel.Provider.ProviderName() != "yaml" || resolvedModel.Model.ModelID() != "global" {
		t.Fatalf("resolved model = %#v, want yaml/global", resolvedModel)
	}
	tester := &serveFakeModelTester{}
	runtime.ModelTester = tester
	deps = buildRouterDependencies(nil, nil, nil, nil, 0, nil, nil, resolver, nil, nil, nil, runtime.AuthLogic, runtime)
	if deps.ModelTester != tester {
		t.Fatalf("deps.ModelTester = %#v, want configured tester", deps.ModelTester)
	}
}

func TestBuildRouterDependenciesIncludesPublicAdminBackofficeServices(t *testing.T) {
	db := newServeTestDB(t)
	if err := db.AutoMigrate(&models.User{}, &models.UserSession{}, &models.SystemSetting{}, &models.EmailVerification{}, &models.AdminAuditEvent{}, &models.LLMModelOverride{}, &models.CustomLLMModel{}); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	runtime, err := initAuthRuntime(db, config.SecurityConfig{AppSecret: "test-secret"})
	if err != nil {
		t.Fatalf("initAuthRuntime() error = %v", err)
	}
	resolver := &coreagent.ModelResolver{Providers: []coretypes.LLMProvider{{
		BaseProvider: coretypes.BaseProvider{Name: "yaml"},
		Models: []coretypes.LLMModel{{
			BaseModel: coretypes.BaseModel{ID: "gpt-5.4", Name: "GPT 5.4"},
			Type:      coretypes.LLMTypeOpenAIResponses,
			Scope:     "global",
		}},
	}}}
	modelLogic, err := logics.NewModelLogic(db, resolverProviders(resolver), runtime.SecretCodec)
	if err != nil {
		t.Fatalf("NewModelLogic() error = %v", err)
	}
	modelTester := &serveFakeModelTester{}
	runtime.ModelLogic = modelLogic
	runtime.ModelTester = modelTester

	deps := buildRouterDependencies(nil, nil, nil, nil, 0, nil, nil, resolver, nil, nil, nil, runtime.AuthLogic, runtime)
	if deps.AuthLogic != runtime.AuthLogic {
		t.Fatalf("deps.AuthLogic = %#v, want %#v", deps.AuthLogic, runtime.AuthLogic)
	}
	if deps.UserDB != db {
		t.Fatalf("deps.UserDB = %#v, want auth runtime db", deps.UserDB)
	}
	if deps.AuthSettings != runtime.Settings {
		t.Fatalf("deps.AuthSettings = %#v, want runtime settings", deps.AuthSettings)
	}
	if deps.EmailVerification != runtime.EmailVerification {
		t.Fatalf("deps.EmailVerification = %#v, want runtime email verification", deps.EmailVerification)
	}
	if deps.TurnstileVerifier != runtime.TurnstileVerifier {
		t.Fatalf("deps.TurnstileVerifier = %#v, want runtime turnstile verifier", deps.TurnstileVerifier)
	}
	if deps.AdminAuditLogic == nil {
		t.Fatal("deps.AdminAuditLogic = nil, want admin audit logic")
	}
	if deps.AdminSMTPTester == nil {
		t.Fatal("deps.AdminSMTPTester = nil, want settings-backed SMTP tester")
	}
	if deps.ModelLogic != modelLogic {
		t.Fatalf("deps.ModelLogic = %#v, want runtime model logic", deps.ModelLogic)
	}
	if deps.ModelResolver == nil {
		t.Fatal("deps.ModelResolver = nil, want model logic resolver")
	}
	if deps.ModelTester != modelTester {
		t.Fatalf("deps.ModelTester = %#v, want runtime model tester", deps.ModelTester)
	}

	engine := rest.Init()
	router.Init(engine, "/api/v1", nil, deps)
	assertServeRouteUnauthorized(t, engine, http.MethodGet, "/api/v1/auth/me")
	assertServeRouteUnauthorized(t, engine, http.MethodGet, "/api/v1/users/me")
	assertServeRouteUnauthorized(t, engine, http.MethodGet, "/api/v1/admin/users")
	assertServeRouteUnauthorized(t, engine, http.MethodGet, "/api/v1/admin/settings/smtp")
	assertServeRouteUnauthorized(t, engine, http.MethodGet, "/api/v1/admin/models")
	assertServeRouteUnauthorized(t, engine, http.MethodGet, "/api/v1/admin/audit-events")
	assertServeRouteUnauthorized(t, engine, http.MethodGet, "/api/v1/models")
	assertServeRouteUnauthorized(t, engine, http.MethodPost, "/api/v1/tasks")

	now := time.Now().UTC()
	adminUser := models.User{
		Username:        "startup-admin",
		Email:           "startup-admin@example.com",
		DisplayName:     "startup-admin",
		Role:            models.UserRoleAdmin,
		Status:          models.UserStatusActive,
		EmailVerifiedAt: &now,
	}
	if err := db.Create(&adminUser).Error; err != nil {
		t.Fatalf("seed admin user error = %v", err)
	}
	adminSession := models.UserSession{
		ID:        "sess_startup_admin",
		UserID:    adminUser.ID,
		Username:  adminUser.Username,
		ExpiresAt: now.Add(time.Hour),
	}
	if err := db.Create(&adminSession).Error; err != nil {
		t.Fatalf("seed admin session error = %v", err)
	}
	resolvedModel, err := deps.ModelResolver.ResolveTask(context.Background(), coreagent.RunTaskInput{
		UserID:     strconv.FormatUint(adminUser.ID, 10),
		ProviderID: "yaml",
		ModelID:    "gpt-5.4",
	})
	if err != nil {
		t.Fatalf("deps.ModelResolver.ResolveTask() error = %v", err)
	}
	if resolvedModel.Provider.ProviderName() != "yaml" || resolvedModel.Model.ModelID() != "gpt-5.4" {
		t.Fatalf("resolved model = %#v, want yaml/gpt-5.4", resolvedModel)
	}
	assertServeRouteOKWithSession(t, engine, http.MethodGet, "/api/v1/admin/settings/smtp", adminSession.ID)
	assertServeRouteOKWithSession(t, engine, http.MethodGet, "/api/v1/admin/audit-events", adminSession.ID)
	modelEnvelope := assertServeRouteOKWithSession(t, engine, http.MethodGet, "/api/v1/admin/models", adminSession.ID)
	var modelCatalog struct {
		Providers []struct {
			ID     string `json:"id"`
			Models []struct {
				ID string `json:"id"`
			} `json:"models"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(modelEnvelope.Data, &modelCatalog); err != nil {
		t.Fatalf("admin models data unmarshal error = %v, data = %s", err, string(modelEnvelope.Data))
	}
	hasYAMLModel := false
	for _, provider := range modelCatalog.Providers {
		if provider.ID != "yaml" {
			continue
		}
		for _, model := range provider.Models {
			if model.ID == "gpt-5.4" {
				hasYAMLModel = true
			}
		}
	}
	if !hasYAMLModel {
		t.Fatalf("admin models catalog = %#v, want yaml/gpt-5.4", modelCatalog)
	}
	normalUser := models.User{
		Username:        "startup-user",
		Email:           "startup-user@example.com",
		DisplayName:     "startup-user",
		Role:            models.UserRoleUser,
		Status:          models.UserStatusActive,
		EmailVerifiedAt: &now,
	}
	if err := db.Create(&normalUser).Error; err != nil {
		t.Fatalf("seed normal user error = %v", err)
	}
	normalSession := models.UserSession{
		ID:        "sess_startup_user",
		UserID:    normalUser.ID,
		Username:  normalUser.Username,
		ExpiresAt: now.Add(time.Hour),
	}
	if err := db.Create(&normalSession).Error; err != nil {
		t.Fatalf("seed normal session error = %v", err)
	}
	nonAdminEnvelope := assertServeRouteWithSession(t, engine, http.MethodGet, "/api/v1/admin/audit-events", normalSession.ID, http.StatusForbidden)
	if nonAdminEnvelope.OK {
		t.Fatalf("non-admin /admin/audit-events OK = true, want forbidden; data = %s", string(nonAdminEnvelope.Data))
	}

	pendingUser := models.User{
		Username:    "pending-user",
		Email:       "pending@example.com",
		DisplayName: "pending-user",
		Role:        models.UserRoleUser,
		Status:      models.UserStatusPendingEmailVerification,
	}
	if err := db.Create(&pendingUser).Error; err != nil {
		t.Fatalf("seed pending user error = %v", err)
	}
	pendingSession := models.UserSession{
		ID:        "sess_pending_startup",
		UserID:    pendingUser.ID,
		Username:  pendingUser.Username,
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}
	if err := db.Create(&pendingSession).Error; err != nil {
		t.Fatalf("seed pending session error = %v", err)
	}
	profileRecorder := httptest.NewRecorder()
	profileRequest := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
	profileRequest.AddCookie(&http.Cookie{Name: logics.DefaultAuthSessionCookieName, Value: pendingSession.ID})
	engine.ServeHTTP(profileRecorder, profileRequest)
	var profileEnvelope serveRouterEnvelope
	if err := json.Unmarshal(profileRecorder.Body.Bytes(), &profileEnvelope); err != nil {
		t.Fatalf("profile envelope unmarshal error = %v, body = %s", err, profileRecorder.Body.String())
	}
	if !profileEnvelope.OK {
		t.Fatalf("/users/me with pending user OK = false, code = %d, body = %s", profileEnvelope.Code, profileRecorder.Body.String())
	}
	taskRecorder := httptest.NewRecorder()
	taskRequest := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(`{"task_type":"agent.run","input":{}}`))
	taskRequest.Header.Set("Content-Type", "application/json")
	taskRequest.AddCookie(&http.Cookie{Name: logics.DefaultAuthSessionCookieName, Value: pendingSession.ID})
	engine.ServeHTTP(taskRecorder, taskRequest)
	var taskEnvelope serveRouterEnvelope
	if err := json.Unmarshal(taskRecorder.Body.Bytes(), &taskEnvelope); err != nil {
		t.Fatalf("task envelope unmarshal error = %v, body = %s", err, taskRecorder.Body.String())
	}
	if taskEnvelope.Code != http.StatusForbidden {
		t.Fatalf("/tasks with pending user code = %d, want %d active-user gate; body = %s", taskEnvelope.Code, http.StatusForbidden, taskRecorder.Body.String())
	}
}

func TestServeConfigRequiresAppSecretWhenRuntimeSettingsNeedSecrets(t *testing.T) {
	db := newServeTestDB(t)
	if err := db.AutoMigrate(&models.SystemSetting{}); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}

	_, err := initAuthRuntime(db, config.SecurityConfig{
		SMTP: config.SMTPConfig{
			Enabled:  true,
			Host:     "smtp.example.com",
			Port:     587,
			Username: "smtp-user",
			Password: "smtp-password",
			From:     "noreply@example.com",
		},
		Turnstile: config.TurnstileConfig{
			Enabled:             true,
			SiteKey:             "site-key",
			Secret:              "turnstile-secret",
			ProtectLogin:        true,
			ProtectRegistration: true,
			ProtectVerification: true,
		},
	})
	if err == nil {
		t.Fatal("initAuthRuntime() error = nil, want missing app secret error")
	}
	if !strings.Contains(err.Error(), "app secret is required") {
		t.Fatalf("initAuthRuntime() error = %v, want app secret required", err)
	}
}

func TestInitAuthRuntimeWiresSettingsVerificationAndTurnstile(t *testing.T) {
	db := newServeTestDB(t)
	if err := db.AutoMigrate(&models.SystemSetting{}); err != nil {
		t.Fatalf("settings AutoMigrate() error = %v", err)
	}
	publicRegistrationEnabled := true

	runtime, err := initAuthRuntime(db, config.SecurityConfig{
		AppSecret: "test-secret",
		PublicRegistration: config.PublicRegistrationConfig{
			Enabled: &publicRegistrationEnabled,
		},
		SMTP: config.SMTPConfig{
			Enabled:     false,
			Host:        "smtp.example.com",
			Port:        587,
			Username:    "smtp-user",
			Password:    "smtp-password",
			From:        "noreply@example.com",
			UseStartTLS: true,
		},
		Turnstile: config.TurnstileConfig{
			Enabled:             true,
			SiteKey:             "site-key",
			Secret:              "turnstile-secret",
			ProtectLogin:        true,
			ProtectRegistration: true,
			ProtectVerification: true,
		},
	})
	if err != nil {
		t.Fatalf("initAuthRuntime() error = %v", err)
	}
	if runtime.AuthLogic == nil {
		t.Fatal("runtime.AuthLogic = nil, want auth logic")
	}
	if runtime.Settings == nil {
		t.Fatal("runtime.Settings = nil, want settings logic")
	}
	if runtime.EmailVerification == nil {
		t.Fatal("runtime.EmailVerification = nil, want email verification logic")
	}
	if runtime.TurnstileVerifier == nil {
		t.Fatal("runtime.TurnstileVerifier = nil, want turnstile verifier")
	}

	registration, err := runtime.Settings.GetPublicRegistration(context.Background())
	if err != nil {
		t.Fatalf("GetPublicRegistration() error = %v", err)
	}
	if !registration.Enabled {
		t.Fatal("public registration enabled = false, want YAML default propagated")
	}

	deps := buildRouterDependencies(nil, nil, nil, nil, 0, nil, nil, nil, nil, nil, nil, runtime.AuthLogic, runtime)
	if deps.AuthSettings != runtime.Settings {
		t.Fatalf("deps.AuthSettings = %#v, want runtime settings", deps.AuthSettings)
	}
	if deps.EmailVerification != runtime.EmailVerification {
		t.Fatalf("deps.EmailVerification = %#v, want runtime email verification", deps.EmailVerification)
	}
	if deps.TurnstileVerifier != runtime.TurnstileVerifier {
		t.Fatalf("deps.TurnstileVerifier = %#v, want runtime turnstile verifier", deps.TurnstileVerifier)
	}
	if deps.UserDB != db {
		t.Fatalf("deps.UserDB = %#v, want auth runtime db", deps.UserDB)
	}
	if deps.AdminAuditLogic == nil {
		t.Fatal("deps.AdminAuditLogic = nil, want admin audit logic")
	}
	if deps.AdminSMTPTester == nil {
		t.Fatal("deps.AdminSMTPTester = nil, want settings-backed SMTP tester")
	}
}

func TestSettingsBackedMailSenderRejectsDisabledSMTP(t *testing.T) {
	db := newServeTestDB(t)
	if err := db.AutoMigrate(&models.SystemSetting{}); err != nil {
		t.Fatalf("settings AutoMigrate() error = %v", err)
	}
	settings, err := logics.NewSettingsLogic(db, logics.SettingsDefaults{
		SMTP: logics.SMTPSettings{Enabled: false},
	}, mustServeTestSecretCodec(t))
	if err != nil {
		t.Fatalf("NewSettingsLogic() error = %v", err)
	}
	sender := &settingsBackedMailSender{settings: settings}
	if err := sender.Available(context.Background()); !errors.Is(err, logics.ErrMailServiceUnavailable) {
		t.Fatalf("Available() error = %v, want %v", err, logics.ErrMailServiceUnavailable)
	}
}

func TestInitPromptRuntimeBuildsPromptDependenciesWithoutAutoMigratingTables(t *testing.T) {
	db := newServeTestDB(t)

	runtime, err := initPromptRuntime(db)
	if err != nil {
		t.Fatalf("initPromptRuntime() error = %v", err)
	}
	if runtime.Store == nil {
		t.Fatal("runtime.Store = nil, want prompt store")
	}
	if runtime.Resolver == nil {
		t.Fatal("runtime.Resolver = nil, want prompt resolver")
	}
	if db.Migrator().HasTable("prompt_documents") {
		t.Fatal("prompt_documents table exists, want prompt runtime init to leave schema untouched")
	}
	if db.Migrator().HasTable("prompt_bindings") {
		t.Fatal("prompt_bindings table exists, want prompt runtime init to leave schema untouched")
	}
}

func TestResolveEffectiveWorkspaceRootUsesCurrentDirectoryWhenEmpty(t *testing.T) {
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("Chdir(%q) error = %v", tempDir, err)
	}
	t.Cleanup(func() {
		if chdirErr := os.Chdir(originalWD); chdirErr != nil {
			t.Errorf("restore working directory error = %v", chdirErr)
		}
	})

	resolved, err := resolveEffectiveWorkspaceRoot("")
	if err != nil {
		t.Fatalf("resolveEffectiveWorkspaceRoot() error = %v", err)
	}
	want := filepath.Clean(tempDir)
	if resolved != want {
		t.Fatalf("resolved workspace root = %q, want %q", resolved, want)
	}
}

func TestResolveEffectiveWorkspaceRootCreatesCanonicalConfiguredDirectory(t *testing.T) {
	baseDir := t.TempDir()
	configured := filepath.Join(baseDir, "nested", "..", "workspace")

	resolved, err := resolveEffectiveWorkspaceRoot(configured)
	if err != nil {
		t.Fatalf("resolveEffectiveWorkspaceRoot() error = %v", err)
	}
	want := filepath.Clean(filepath.Join(baseDir, "workspace"))
	if resolved != want {
		t.Fatalf("resolved workspace root = %q, want %q", resolved, want)
	}
	info, err := os.Stat(want)
	if err != nil {
		t.Fatalf("Stat(%q) error = %v", want, err)
	}
	if !info.IsDir() {
		t.Fatalf("workspace root %q is not a directory", want)
	}
	registry, err := newDefaultToolRegistry(resolved, builtin.WebSearchOptions{}, builtin.ImageGenOptions{}, nil, nil, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("newDefaultToolRegistry() error = %v", err)
	}
	if len(registry.List()) == 0 {
		t.Fatal("registry.List() = empty, want builtin tools registered")
	}
}

func TestNewDefaultToolRegistryUsesConfiguredWebSearchOptions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			t.Fatalf("request path = %q, want %q", r.URL.Path, "/search")
		}
		_, _ = w.Write([]byte(`{"results":[{"title":"Tavily Result","url":"https://example.com/tavily","content":"snippet"}]}`))
	}))
	defer server.Close()

	fn := reflect.ValueOf(newDefaultToolRegistry)
	if fn.Type().NumIn() != 6 {
		t.Fatalf("newDefaultToolRegistry arg count = %d, want 6", fn.Type().NumIn())
	}
	if fn.Type().In(1) != reflect.TypeOf(builtin.WebSearchOptions{}) {
		t.Fatalf("newDefaultToolRegistry second arg = %s, want %s", fn.Type().In(1), reflect.TypeOf(builtin.WebSearchOptions{}))
	}

	results := fn.Call([]reflect.Value{
		reflect.ValueOf(t.TempDir()),
		reflect.ValueOf(builtin.WebSearchOptions{
			DefaultProvider: "tavily",
			Tavily:          &builtin.TavilyConfig{APIKey: "tavily-key", BaseURL: server.URL},
		}),
		reflect.ValueOf(builtin.ImageGenOptions{}),
		reflect.ValueOf((*attachments.Store)(nil)),
		reflect.Zero(reflect.TypeOf((*attachments.Storage)(nil)).Elem()),
		reflect.ValueOf(7 * 24 * time.Hour),
	})
	if len(results) != 2 {
		t.Fatalf("newDefaultToolRegistry return count = %d, want 2", len(results))
	}
	if errValue := results[1].Interface(); errValue != nil {
		t.Fatalf("newDefaultToolRegistry() error = %v", errValue)
	}
	registry, ok := results[0].Interface().(*coretools.Registry)
	if !ok {
		t.Fatalf("newDefaultToolRegistry() registry type = %T, want *coretools.Registry", results[0].Interface())
	}

	raw, err := registry.Execute(context.Background(), "web_search", map[string]any{"query": "golang"})
	if err != nil {
		t.Fatalf("registry.Execute(web_search) error = %v", err)
	}
	var result struct {
		Provider string `json:"provider"`
		Results  []struct {
			Title string `json:"title"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(raw.Content), &result); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", raw.Content, err)
	}
	if result.Provider != "tavily" {
		t.Fatalf("result.Provider = %q, want %q", result.Provider, "tavily")
	}
	if len(result.Results) != 1 || result.Results[0].Title != "Tavily Result" {
		t.Fatalf("result.Results = %#v, want one Tavily Result", result.Results)
	}
}

func TestNewDefaultToolRegistryPassesImageGenSentRetention(t *testing.T) {
	options := newDefaultBuiltinOptions(t.TempDir(), builtin.WebSearchOptions{}, builtin.ImageGenOptions{DefaultProvider: "openai"}, nil, nil, 45*24*time.Hour)
	if options.ImageGen.SentRetention != 45*24*time.Hour {
		t.Fatalf("options.ImageGen.SentRetention = %v, want 1080h", options.ImageGen.SentRetention)
	}
	if options.ImageGen.DefaultProvider != "openai" {
		t.Fatalf("options.ImageGen.DefaultProvider = %q, want openai", options.ImageGen.DefaultProvider)
	}
}

func TestBuildAgentRunExecutorDependenciesProvideSkillsResolver(t *testing.T) {
	deps := buildAgentRunExecutorDependencies(nil, nil, nil, nil, nil, nil, nil, nil, t.TempDir(), nil, nil)
	if deps.SkillsResolver == nil {
		t.Fatal("SkillsResolver = nil, want workspace skills resolver")
	}
}

func TestNewTaskManagerUsesConfiguredWorkerCountAndRunnerID(t *testing.T) {
	db := newServeTestDB(t)
	store := coretasks.NewStore(db)
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("task store migrate error = %v", err)
	}

	approvalStore := approvals.NewStore(db)
	interactionStore := interactions.NewStore(db)
	manager := newTaskManager(store, approvalStore, interactionStore, config.TaskManagerConfig{
		WorkerCount: 2,
		RunnerID:    "configured-runner",
	}, nil)

	started := make(chan string, 2)
	release := make(chan struct{})
	var once sync.Once
	if err := manager.RegisterExecutor("blocking", func(ctx context.Context, task *coretasks.Task, runtime *coretasks.Runtime) (any, error) {
		started <- task.ID
		<-release
		return map[string]any{"task_id": task.ID}, nil
	}); err != nil {
		t.Fatalf("RegisterExecutor() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.Start(ctx)

	first, err := manager.CreateTask(ctx, coretasks.CreateTaskInput{TaskType: "blocking", CreatedBy: "tester"})
	if err != nil {
		t.Fatalf("CreateTask(first) error = %v", err)
	}
	second, err := manager.CreateTask(ctx, coretasks.CreateTaskInput{TaskType: "blocking", CreatedBy: "tester"})
	if err != nil {
		t.Fatalf("CreateTask(second) error = %v", err)
	}

	seen := map[string]bool{}
	deadline := time.After(2 * time.Second)
	for len(seen) < 2 {
		select {
		case taskID := <-started:
			seen[taskID] = true
		case <-deadline:
			t.Fatalf("started tasks = %v, want both tasks to start with workerCount=2", seen)
		}
	}

	for _, taskID := range []string{first.ID, second.ID} {
		task, err := manager.GetTask(ctx, taskID)
		if err != nil {
			t.Fatalf("GetTask(%q) error = %v", taskID, err)
		}
		if task.RunnerID != "configured-runner" {
			t.Fatalf("task %s runner_id = %q, want %q", taskID, task.RunnerID, "configured-runner")
		}
		if task.Status != coretasks.StatusRunning {
			t.Fatalf("task %s status = %q, want %q while blocked", taskID, task.Status, coretasks.StatusRunning)
		}
	}

	once.Do(func() { close(release) })
	waitForServeTestTaskStatus(t, ctx, manager, first.ID, coretasks.StatusSucceeded)
	waitForServeTestTaskStatus(t, ctx, manager, second.ID, coretasks.StatusSucceeded)
}

func TestNewTaskManagerPreservesInjectedApprovalStoreForRuntimeUse(t *testing.T) {
	db := newServeTestDB(t)
	store := coretasks.NewStore(db)
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("task store migrate error = %v", err)
	}
	approvalStore := approvals.NewStore(db)
	if err := approvalStore.AutoMigrate(); err != nil {
		t.Fatalf("approval store migrate error = %v", err)
	}
	interactionStore := interactions.NewStore(db)
	if err := interactionStore.AutoMigrate(); err != nil {
		t.Fatalf("interaction store migrate error = %v", err)
	}

	manager := newTaskManager(store, approvalStore, interactionStore, config.TaskManagerConfig{RunnerID: "configured-runner"}, nil)
	task, err := manager.CreateTask(context.Background(), coretasks.CreateTaskInput{TaskType: "agent.run", CreatedBy: "tester"})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	approval, err := manager.CreateApproval(context.Background(), approvals.CreateApprovalInput{
		TaskID:           task.ID,
		ConversationID:   "conv-1",
		StepIndex:        1,
		ToolCallID:       "call-1",
		ToolName:         "bash",
		ArgumentsSummary: "rm -rf /tmp/demo",
		RiskLevel:        "high",
		Reason:           "dangerous filesystem mutation",
	})
	if err != nil {
		t.Fatalf("CreateApproval() error = %v", err)
	}
	if approval == nil || approval.ID == "" {
		t.Fatalf("approval = %#v, want persisted approval", approval)
	}
	listed, err := manager.ListTaskApprovals(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("ListTaskApprovals() error = %v", err)
	}
	if len(listed) != 1 || listed[0].ID != approval.ID {
		t.Fatalf("listed approvals = %#v, want %q", listed, approval.ID)
	}
}

func TestPromptBuildRouterDependenciesPreservesAuditRoutes(t *testing.T) {
	db := newServeTestDB(t)
	auditStore := coreaudit.NewStore(db)
	if err := auditStore.AutoMigrate(); err != nil {
		t.Fatalf("audit store migrate error = %v", err)
	}

	engine := rest.Init()
	router.Init(engine, "/api/v1", nil, router.Dependencies{AuditStore: auditStore})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/audit/runs/run_1", nil)
	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 so the route is registered", recorder.Code)
	}
	var envelope serveRouterEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("Unmarshal() envelope error = %v, body = %s", err, recorder.Body.String())
	}
	if envelope.OK {
		t.Fatal("envelope.OK = true, want unauthorized failure for anonymous request")
	}
	if envelope.Code != http.StatusUnauthorized {
		t.Fatalf("envelope.Code = %d, want %d", envelope.Code, http.StatusUnauthorized)
	}
}

func TestRouterInitRegistersApprovalRoutes(t *testing.T) {
	engine := rest.Init()
	router.Init(engine, "/api/v1", nil, router.Dependencies{})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/task_1/approvals", nil)
	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 so the route is registered", recorder.Code)
	}
	var envelope serveRouterEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("Unmarshal() envelope error = %v, body = %s", err, recorder.Body.String())
	}
	if envelope.OK {
		t.Fatal("envelope.OK = true, want unauthorized failure for anonymous request")
	}
	if envelope.Code != http.StatusUnauthorized {
		t.Fatalf("envelope.Code = %d, want %d", envelope.Code, http.StatusUnauthorized)
	}

	recorder = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/v1/tasks/task_1/approvals/approval_1/decision", nil)
	engine.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("decision status = %d, want 200 so the route is registered", recorder.Code)
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("Unmarshal() decision envelope error = %v, body = %s", err, recorder.Body.String())
	}
	if envelope.Code != http.StatusUnauthorized {
		t.Fatalf("decision envelope.Code = %d, want %d", envelope.Code, http.StatusUnauthorized)
	}
}

func TestInitAuditRuntimeSharesRecorderAcrossTaskAndAgentPaths(t *testing.T) {
	db := newServeTestDB(t)
	runtime, err := initAuditRuntime(db)
	if err != nil {
		t.Fatalf("initAuditRuntime() error = %v", err)
	}
	if runtime.Store == nil {
		t.Fatal("runtime.Store = nil, want audit store")
	}
	if runtime.RunRecorder == nil {
		t.Fatal("runtime.RunRecorder = nil, want audit recorder")
	}
	if runtime.TaskRecorder == nil {
		t.Fatal("runtime.TaskRecorder = nil, want task audit recorder")
	}
	taskRecorder, ok := runtime.TaskRecorder.(*taskAuditRecorder)
	if !ok {
		t.Fatalf("runtime.TaskRecorder type = %T, want *taskAuditRecorder", runtime.TaskRecorder)
	}
	if taskRecorder.recorder != runtime.RunRecorder {
		t.Fatal("task and agent audit paths should share one recorder instance")
	}
}

func TestBuildAgentRunExecutorDependenciesThreadPromptRuntimeAndWorkspaceRoot(t *testing.T) {
	db := newServeTestDB(t)
	promptRuntime, err := initPromptRuntime(db)
	if err != nil {
		t.Fatalf("initPromptRuntime() error = %v", err)
	}
	workspaceRoot, err := resolveEffectiveWorkspaceRoot(t.TempDir())
	if err != nil {
		t.Fatalf("resolveEffectiveWorkspaceRoot() error = %v", err)
	}
	resolver := &coreagent.ModelResolver{}
	conversationStore := coreagent.NewConversationStore(db)
	recorder := coreaudit.NewRecorder(coreaudit.NewStore(db))
	clientFactory := func(*coretypes.LLMProvider, *coretypes.LLMModel) (model.LlmClient, error) {
		return &serveStubClient{answer: "hello"}, nil
	}

	deps := buildAgentRunExecutorDependencies(resolver, conversationStore, nil, nil, nil, nil, nil, promptRuntime.Resolver, workspaceRoot, clientFactory, recorder)
	if deps.Resolver != resolver {
		t.Fatalf("deps.Resolver = %#v, want %#v", deps.Resolver, resolver)
	}
	if deps.ConversationStore != conversationStore {
		t.Fatalf("deps.ConversationStore = %#v, want %#v", deps.ConversationStore, conversationStore)
	}
	if deps.PromptResolver != promptRuntime.Resolver {
		t.Fatalf("deps.PromptResolver = %#v, want %#v", deps.PromptResolver, promptRuntime.Resolver)
	}
	if deps.WorkspaceRoot != workspaceRoot {
		t.Fatalf("deps.WorkspaceRoot = %q, want %q", deps.WorkspaceRoot, workspaceRoot)
	}
	if deps.ClientFactory == nil {
		t.Fatal("deps.ClientFactory = nil, want client factory")
	}
	if deps.AuditRecorder != recorder {
		t.Fatalf("deps.AuditRecorder = %#v, want %#v", deps.AuditRecorder, recorder)
	}
}

func TestRegisterAgentRunExecutorPromptWiringKeepsAuditRecorder(t *testing.T) {
	db := newServeTestDB(t)
	taskStore := coretasks.NewStore(db)
	if err := taskStore.AutoMigrate(); err != nil {
		t.Fatalf("task store migrate error = %v", err)
	}
	conversationStore := coreagent.NewConversationStore(db)
	if err := conversationStore.AutoMigrate(); err != nil {
		t.Fatalf("conversation store migrate error = %v", err)
	}
	auditStore := coreaudit.NewStore(db)
	if err := auditStore.AutoMigrate(); err != nil {
		t.Fatalf("audit store migrate error = %v", err)
	}

	recorder := coreaudit.NewRecorder(auditStore)
	promptRuntime, err := initPromptRuntime(db)
	if err != nil {
		t.Fatalf("initPromptRuntime() error = %v", err)
	}
	if db.Migrator().HasTable("prompt_documents") || db.Migrator().HasTable("prompt_bindings") {
		t.Fatal("prompt tables exist before explicit test migration, want initPromptRuntime to leave schema untouched")
	}
	if err := promptRuntime.Store.AutoMigrate(); err != nil {
		t.Fatalf("prompt store migrate error = %v", err)
	}
	if !db.Migrator().HasTable("prompt_documents") || !db.Migrator().HasTable("prompt_bindings") {
		t.Fatal("prompt tables missing after explicit test migration")
	}
	workspaceRoot, err := resolveEffectiveWorkspaceRoot(t.TempDir())
	if err != nil {
		t.Fatalf("resolveEffectiveWorkspaceRoot() error = %v", err)
	}
	manager := coretasks.NewManager(taskStore, coretasks.ManagerOptions{
		RunnerID:          "serve-test",
		PollInterval:      5 * time.Millisecond,
		LeaseDuration:     100 * time.Millisecond,
		HeartbeatInterval: 20 * time.Millisecond,
		AuditRecorder:     newTaskAuditRecorder(recorder),
	})
	if err := registerAgentRunExecutor(manager, nil, nil, nil, nil, &coreagent.ModelResolver{Providers: []coretypes.LLMProvider{{
		BaseProvider: coretypes.BaseProvider{Name: "openai"},
		Models:       []coretypes.LLMModel{{BaseModel: coretypes.BaseModel{ID: "gpt-5.4", Name: "GPT 5.4"}, Type: coretypes.LLMTypeOpenAIResponses}},
	}}}, conversationStore, nil, promptRuntime.Resolver, workspaceRoot, func(*coretypes.LLMProvider, *coretypes.LLMModel) (model.LlmClient, error) {
		return &serveStubClient{answer: "hello"}, nil
	}, recorder); err != nil {
		t.Fatalf("registerAgentRunExecutor() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.Start(ctx)

	created, err := manager.CreateTask(ctx, coretasks.CreateTaskInput{
		TaskType:  "agent.run",
		CreatedBy: "tester",
		Input: coreagent.RunTaskInput{
			ConversationID: "conv_1",
			ProviderID:     "openai",
			ModelID:        "gpt-5.4",
			Message:        "hi",
			CreatedBy:      "tester",
		},
	})
	if err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	waitForServeTestTaskStatus(t, ctx, manager, created.ID, coretasks.StatusSucceeded)

	run := waitForServeTestAuditRun(t, ctx, auditStore, created.ID)
	eventTypes := waitForServeTestAuditEvents(t, db, created.ID, "conversation.loaded", "user_message.appended", "messages.persisted")
	if len(eventTypes) < 3 {
		t.Fatalf("audit event types = %v, want executor audit events", eventTypes)
	}

	var artifacts []coreaudit.Artifact
	if err := db.WithContext(ctx).Where("run_id = ?", run.ID).Find(&artifacts).Error; err != nil {
		t.Fatalf("load artifacts error = %v", err)
	}
	if !hasServeTestArtifactKind(artifacts, coreaudit.ArtifactKindRequestMessages) {
		t.Fatalf("artifact kinds = %#v, want %q", artifacts, coreaudit.ArtifactKindRequestMessages)
	}
}

type serveStubClient struct{ answer string }

type serveFakeModelTester struct{}

func (serveFakeModelTester) TestModel(context.Context, *coreagent.ResolvedModel) error {
	return nil
}

func (s *serveStubClient) Chat(context.Context, model.ChatRequest) (model.ChatResponse, error) {
	panic("unexpected Chat call")
}

func (s *serveStubClient) ChatStream(context.Context, model.ChatRequest) (model.Stream, error) {
	return &serveStubStream{answer: s.answer}, nil
}

type serveStubStream struct{ answer string }

func (s *serveStubStream) Recv() (string, error) { return "", nil }

func (s *serveStubStream) RecvEvent() (model.StreamEvent, error) {
	if s.answer == "" {
		return model.StreamEvent{}, nil
	}
	answer := s.answer
	s.answer = ""
	return model.StreamEvent{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: answer}}, nil
}

func (s *serveStubStream) FinalMessage() (model.Message, error) {
	return model.Message{Role: model.RoleAssistant, Content: "hello"}, nil
}

func (s *serveStubStream) Close() error                    { return nil }
func (s *serveStubStream) Context() context.Context        { return context.Background() }
func (s *serveStubStream) Stats() *model.StreamStats       { return &model.StreamStats{} }
func (s *serveStubStream) ToolCalls() []coretypes.ToolCall { return nil }
func (s *serveStubStream) ResponseType() model.StreamResponseType {
	return model.StreamResponseUnknown
}
func (s *serveStubStream) FinishReason() string { return "stop" }
func (s *serveStubStream) Reasoning() string    { return "" }

func newServeTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open db error = %v", err)
	}
	return db
}

func mustServeTestSecretCodec(t *testing.T) *secret.Codec {
	t.Helper()
	codec, err := secret.NewCodec("test-secret")
	if err != nil {
		t.Fatalf("secret.NewCodec() error = %v", err)
	}
	return codec
}

func waitForServeTestTaskStatus(t *testing.T, ctx context.Context, manager *coretasks.Manager, taskID string, want coretasks.Status) *coretasks.Task {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, err := manager.GetTask(ctx, taskID)
		if err == nil && task != nil && task.Status == want {
			return task
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("task %s did not reach status %q", taskID, want)
	return nil
}

func waitForServeTestAuditRun(t *testing.T, ctx context.Context, store *coreaudit.Store, taskID string) *coreaudit.Run {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		run, err := store.GetRunByTaskID(ctx, taskID)
		if err == nil && run != nil {
			return run
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("task %s did not create audit run", taskID)
	return nil
}

func waitForServeTestAuditEvents(t *testing.T, db *gorm.DB, taskID string, want ...string) []string {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var events []coreaudit.Event
		if err := db.Where("task_id = ?", taskID).Order("seq asc").Find(&events).Error; err != nil {
			t.Fatalf("load audit events error = %v", err)
		}
		got := make([]string, 0, len(events))
		for _, event := range events {
			got = append(got, event.EventType)
		}
		if containsServeTestEventTypes(got, want...) {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("task %s did not record audit events %v", taskID, want)
	return nil
}

func containsServeTestEventTypes(got []string, want ...string) bool {
	for _, target := range want {
		found := false
		for _, candidate := range got {
			if candidate == target {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func hasServeTestArtifactKind(artifacts []coreaudit.Artifact, kind coreaudit.ArtifactKind) bool {
	for _, artifact := range artifacts {
		if artifact.Kind == kind {
			return true
		}
	}
	return false
}
