package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/EquentR/agent_runtime/app/logics"
	"github.com/EquentR/agent_runtime/app/models"
	coreagent "github.com/EquentR/agent_runtime/core/agent"
	coretypes "github.com/EquentR/agent_runtime/core/types"
	"github.com/EquentR/agent_runtime/pkg/rest"
	"github.com/EquentR/agent_runtime/pkg/secret"
)

func TestAdminModelHandlerAllowsAdminToTestOtherUserModelAndWritesAudit(t *testing.T) {
	deps, _ := newAdminHandlerTestServer(t)
	if err := deps.db.AutoMigrate(&models.LLMModelOverride{}, &models.CustomLLMModel{}); err != nil {
		t.Fatalf("AutoMigrate(model tables) error = %v", err)
	}
	codec, err := secret.NewCodec("test-secret")
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	modelLogic, err := logics.NewModelLogic(deps.db, nil, codec)
	if err != nil {
		t.Fatalf("NewModelLogic() error = %v", err)
	}
	owner := seedAdminHandlerUser(t, deps.db, "owner", "owner@example.com", models.UserRoleUser, models.UserStatusActive, true)
	custom, err := modelLogic.CreateCustomModel(t.Context(), logics.CreateCustomModelInput{
		OwnerUserID:      owner.ID,
		ProviderID:       "owner-provider",
		ModelID:          "owner-model",
		DisplayName:      "Owner Model",
		ProviderType:     coretypes.LLMTypeOpenAICompletions,
		APIKey:           "owner-secret",
		Scope:            "owner",
		Enabled:          true,
		ContextMaxTokens: 32768,
	})
	if err != nil {
		t.Fatalf("CreateCustomModel() error = %v", err)
	}

	engine := rest.Init()
	authMiddleware := NewAuthMiddleware(deps.authLogic)
	NewAdminModelHandler(modelLogic, deps.auditLogic, fakeModelTester{}, authMiddleware.RequireAdmin()).Register(engine.Group("/api/v1"))
	server := httptest.NewServer(engine)
	defer server.Close()

	response := doAdminRequest(t, http.MethodPost, server.URL+"/api/v1/admin/models/custom/"+custom.ID+"/test", nil, deps.adminCookie)
	defer response.Body.Close()
	envelope := decodeEnvelope(t, response.Body)
	if !envelope.OK {
		t.Fatalf("test response OK = false, message = %s, data = %s", envelope.Message, string(envelope.Data))
	}

	var payload struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(envelope.Data, &payload); err != nil {
		t.Fatalf("Unmarshal payload error = %v", err)
	}
	if !payload.OK {
		t.Fatalf("payload.OK = false, want true")
	}

	var events []models.AdminAuditEvent
	if err := deps.db.Where("target_kind = ? AND target_id = ? AND action = ?", "model", custom.ID, "admin.models.custom.test").Find(&events).Error; err != nil {
		t.Fatalf("query audit events error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("audit events = %d, want 1", len(events))
	}
}

func TestAdminModelHandlerAuditsFailedModelTest(t *testing.T) {
	deps, _ := newAdminHandlerTestServer(t)
	if err := deps.db.AutoMigrate(&models.LLMModelOverride{}, &models.CustomLLMModel{}); err != nil {
		t.Fatalf("AutoMigrate(model tables) error = %v", err)
	}
	codec, err := secret.NewCodec("test-secret")
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	modelLogic, err := logics.NewModelLogic(deps.db, nil, codec)
	if err != nil {
		t.Fatalf("NewModelLogic() error = %v", err)
	}
	owner := seedAdminHandlerUser(t, deps.db, "failed-owner", "failed-owner@example.com", models.UserRoleUser, models.UserStatusActive, true)
	custom, err := modelLogic.CreateCustomModel(t.Context(), logics.CreateCustomModelInput{
		OwnerUserID:      owner.ID,
		ProviderID:       "failed-provider",
		ModelID:          "failed-model",
		DisplayName:      "Failed Model",
		ProviderType:     coretypes.LLMTypeOpenAICompletions,
		APIKey:           "owner-secret",
		Scope:            "owner",
		Enabled:          true,
		ContextMaxTokens: 32768,
	})
	if err != nil {
		t.Fatalf("CreateCustomModel() error = %v", err)
	}

	engine := rest.Init()
	authMiddleware := NewAuthMiddleware(deps.authLogic)
	NewAdminModelHandler(modelLogic, deps.auditLogic, failingModelTester{err: errors.New("dial failed")}, authMiddleware.RequireAdmin()).Register(engine.Group("/api/v1"))
	server := httptest.NewServer(engine)
	defer server.Close()

	response := doAdminRequest(t, http.MethodPost, server.URL+"/api/v1/admin/models/custom/"+custom.ID+"/test", nil, deps.adminCookie)
	defer response.Body.Close()
	envelope := decodeEnvelope(t, response.Body)
	if envelope.OK {
		t.Fatal("test response OK = true, want tester failure")
	}

	var event models.AdminAuditEvent
	if err := deps.db.Where("target_kind = ? AND target_id = ? AND action = ?", "model", custom.ID, "admin.models.custom.test").Take(&event).Error; err != nil {
		t.Fatalf("load audit event error = %v", err)
	}
	afterJSON := string(event.AfterJSON)
	if !strings.Contains(afterJSON, `"ok":false`) || !strings.Contains(afterJSON, "dial failed") {
		t.Fatalf("audit after_json = %s, want failed test result", afterJSON)
	}
}

func TestAdminModelHandlerAuditsModelTestResolveFailure(t *testing.T) {
	deps, _ := newAdminHandlerTestServer(t)
	if err := deps.db.AutoMigrate(&models.LLMModelOverride{}, &models.CustomLLMModel{}); err != nil {
		t.Fatalf("AutoMigrate(model tables) error = %v", err)
	}
	codec, err := secret.NewCodec("test-secret")
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	modelLogic, err := logics.NewModelLogic(deps.db, nil, codec)
	if err != nil {
		t.Fatalf("NewModelLogic() error = %v", err)
	}

	engine := rest.Init()
	authMiddleware := NewAuthMiddleware(deps.authLogic)
	NewAdminModelHandler(modelLogic, deps.auditLogic, fakeModelTester{}, authMiddleware.RequireAdmin()).Register(engine.Group("/api/v1"))
	server := httptest.NewServer(engine)
	defer server.Close()

	response := doAdminRequest(t, http.MethodPost, server.URL+"/api/v1/admin/models/custom/missing-model/test", nil, deps.adminCookie)
	defer response.Body.Close()
	envelope := decodeEnvelope(t, response.Body)
	if envelope.OK {
		t.Fatal("test response OK = true, want resolve failure")
	}

	var event models.AdminAuditEvent
	if err := deps.db.Where("target_kind = ? AND target_id = ? AND action = ?", "model", "missing-model", "admin.models.custom.test").Take(&event).Error; err != nil {
		t.Fatalf("load audit event error = %v", err)
	}
	if afterJSON := string(event.AfterJSON); !strings.Contains(afterJSON, `"ok":false`) || !strings.Contains(afterJSON, "模型不存在") {
		t.Fatalf("audit after_json = %s, want resolve failure result", afterJSON)
	}
}

func TestAdminModelHandlerDefaultsCreatedCustomModelOwnerToActor(t *testing.T) {
	deps, _ := newAdminHandlerTestServer(t)
	if err := deps.db.AutoMigrate(&models.LLMModelOverride{}, &models.CustomLLMModel{}); err != nil {
		t.Fatalf("AutoMigrate(model tables) error = %v", err)
	}
	codec, err := secret.NewCodec("test-secret")
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	modelLogic, err := logics.NewModelLogic(deps.db, nil, codec)
	if err != nil {
		t.Fatalf("NewModelLogic() error = %v", err)
	}
	var admin models.User
	if err := deps.db.Take(&admin, "username = ?", "root").Error; err != nil {
		t.Fatalf("load admin: %v", err)
	}

	engine := rest.Init()
	authMiddleware := NewAuthMiddleware(deps.authLogic)
	NewAdminModelHandler(modelLogic, deps.auditLogic, fakeModelTester{}, authMiddleware.RequireAdmin()).Register(engine.Group("/api/v1"))
	server := httptest.NewServer(engine)
	defer server.Close()

	response := doAdminRequest(t, http.MethodPost, server.URL+"/api/v1/admin/models/custom", map[string]any{
		"provider_id":        "admin-provider",
		"model_id":           "admin-model",
		"display_name":       "Admin Model",
		"provider_type":      coretypes.LLMTypeOpenAICompletions,
		"api_key":            "admin-secret",
		"scope":              "admin",
		"context_max_tokens": int64(32768),
	}, deps.adminCookie)
	defer response.Body.Close()
	envelope := decodeEnvelope(t, response.Body)
	if !envelope.OK {
		t.Fatalf("create response OK = false, message = %s, data = %s", envelope.Message, string(envelope.Data))
	}
	var payload struct {
		OwnerUserID uint64 `json:"owner_user_id"`
	}
	if err := json.Unmarshal(envelope.Data, &payload); err != nil {
		t.Fatalf("Unmarshal payload error = %v", err)
	}
	if payload.OwnerUserID != admin.ID {
		t.Fatalf("OwnerUserID = %d, want admin id %d", payload.OwnerUserID, admin.ID)
	}
}

func TestAdminModelHandlerRejectsInvalidCustomModelScopeWithBadRequest(t *testing.T) {
	deps, _ := newAdminHandlerTestServer(t)
	if err := deps.db.AutoMigrate(&models.LLMModelOverride{}, &models.CustomLLMModel{}); err != nil {
		t.Fatalf("AutoMigrate(model tables) error = %v", err)
	}
	codec, err := secret.NewCodec("test-secret")
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	modelLogic, err := logics.NewModelLogic(deps.db, nil, codec)
	if err != nil {
		t.Fatalf("NewModelLogic() error = %v", err)
	}

	engine := rest.Init()
	authMiddleware := NewAuthMiddleware(deps.authLogic)
	NewAdminModelHandler(modelLogic, deps.auditLogic, fakeModelTester{}, authMiddleware.RequireAdmin()).Register(engine.Group("/api/v1"))
	server := httptest.NewServer(engine)
	defer server.Close()

	response := doAdminRequest(t, http.MethodPost, server.URL+"/api/v1/admin/models/custom", map[string]any{
		"provider_id":        "invalid-handler-scope-provider",
		"model_id":           "invalid-handler-scope-model",
		"display_name":       "Invalid Scope",
		"provider_type":      coretypes.LLMTypeOpenAICompletions,
		"api_key":            "admin-secret",
		"scope":              "public",
		"context_max_tokens": int64(32768),
	}, deps.adminCookie)
	defer response.Body.Close()
	envelope := decodeEnvelope(t, response.Body)
	if envelope.Code != http.StatusBadRequest {
		t.Fatalf("envelope.Code = %d, want 400", envelope.Code)
	}
	if envelope.OK {
		t.Fatalf("response OK = true, want invalid scope failure")
	}
}

func TestAdminModelHandlerRejectsCustomModelSelectionConflictWithBadRequest(t *testing.T) {
	deps, _ := newAdminHandlerTestServer(t)
	if err := deps.db.AutoMigrate(&models.LLMModelOverride{}, &models.CustomLLMModel{}); err != nil {
		t.Fatalf("AutoMigrate(model tables) error = %v", err)
	}
	codec, err := secret.NewCodec("test-secret")
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	modelLogic, err := logics.NewModelLogic(deps.db, nil, codec)
	if err != nil {
		t.Fatalf("NewModelLogic() error = %v", err)
	}
	otherOwner := seedAdminHandlerUser(t, deps.db, "model-owner", "model-owner@example.com", models.UserRoleAdmin, models.UserStatusActive, true)

	engine := rest.Init()
	authMiddleware := NewAuthMiddleware(deps.authLogic)
	NewAdminModelHandler(modelLogic, deps.auditLogic, fakeModelTester{}, authMiddleware.RequireAdmin()).Register(engine.Group("/api/v1"))
	server := httptest.NewServer(engine)
	defer server.Close()

	first := doAdminRequest(t, http.MethodPost, server.URL+"/api/v1/admin/models/custom", map[string]any{
		"provider_id":        "conflict-handler-provider",
		"model_id":           "conflict-handler-model",
		"display_name":       "Conflict One",
		"provider_type":      coretypes.LLMTypeOpenAICompletions,
		"api_key":            "admin-secret",
		"scope":              logics.ModelScopeGlobal,
		"context_max_tokens": int64(32768),
	}, deps.adminCookie)
	defer first.Body.Close()
	if envelope := decodeEnvelope(t, first.Body); !envelope.OK {
		t.Fatalf("first create response OK = false, message = %s, data = %s", envelope.Message, string(envelope.Data))
	}

	second := doAdminRequest(t, http.MethodPost, server.URL+"/api/v1/admin/models/custom", map[string]any{
		"owner_user_id":      otherOwner.ID,
		"provider_id":        "conflict-handler-provider",
		"model_id":           "conflict-handler-model",
		"display_name":       "Conflict Two",
		"provider_type":      coretypes.LLMTypeOpenAICompletions,
		"api_key":            "admin-secret",
		"scope":              logics.ModelScopeAdmin,
		"context_max_tokens": int64(32768),
	}, deps.adminCookie)
	defer second.Body.Close()
	envelope := decodeEnvelope(t, second.Body)
	if envelope.Code != http.StatusBadRequest {
		t.Fatalf("envelope.Code = %d, want 400", envelope.Code)
	}
	if envelope.OK {
		t.Fatalf("response OK = true, want selection conflict failure")
	}
}

func TestAdminModelHandlerCanClearCustomModelBaseURL(t *testing.T) {
	deps, _ := newAdminHandlerTestServer(t)
	if err := deps.db.AutoMigrate(&models.LLMModelOverride{}, &models.CustomLLMModel{}); err != nil {
		t.Fatalf("AutoMigrate(model tables) error = %v", err)
	}
	codec, err := secret.NewCodec("test-secret")
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	modelLogic, err := logics.NewModelLogic(deps.db, nil, codec)
	if err != nil {
		t.Fatalf("NewModelLogic() error = %v", err)
	}
	var admin models.User
	if err := deps.db.Take(&admin, "username = ?", "root").Error; err != nil {
		t.Fatalf("load admin: %v", err)
	}
	custom, err := modelLogic.CreateCustomModel(t.Context(), logics.CreateCustomModelInput{
		OwnerUserID:      admin.ID,
		ProviderID:       "base-url-handler-provider",
		ModelID:          "base-url-handler-model",
		DisplayName:      "Base URL Handler",
		ProviderType:     coretypes.LLMTypeOpenAICompletions,
		BaseURL:          "https://api.example.com/v1",
		APIKey:           "admin-secret",
		Scope:            logics.ModelScopeAdmin,
		Enabled:          true,
		ContextMaxTokens: 32768,
	})
	if err != nil {
		t.Fatalf("CreateCustomModel() error = %v", err)
	}

	engine := rest.Init()
	authMiddleware := NewAuthMiddleware(deps.authLogic)
	NewAdminModelHandler(modelLogic, deps.auditLogic, fakeModelTester{}, authMiddleware.RequireAdmin()).Register(engine.Group("/api/v1"))
	server := httptest.NewServer(engine)
	defer server.Close()

	response := doAdminRequest(t, http.MethodPut, server.URL+"/api/v1/admin/models/custom/"+custom.ID, map[string]any{
		"clear_base_url": true,
	}, deps.adminCookie)
	defer response.Body.Close()
	envelope := decodeEnvelope(t, response.Body)
	if !envelope.OK {
		t.Fatalf("update response OK = false, message = %s, data = %s", envelope.Message, string(envelope.Data))
	}
	var payload struct {
		BaseURL string `json:"base_url"`
	}
	if err := json.Unmarshal(envelope.Data, &payload); err != nil {
		t.Fatalf("Unmarshal payload error = %v", err)
	}
	if payload.BaseURL != "" {
		t.Fatalf("BaseURL = %q, want empty", payload.BaseURL)
	}
}

func TestAdminModelHandlerRollsBackCustomCreateWhenAuditFails(t *testing.T) {
	deps, _ := newAdminHandlerTestServerWithoutAuditTable(t)
	if err := deps.db.AutoMigrate(&models.LLMModelOverride{}, &models.CustomLLMModel{}); err != nil {
		t.Fatalf("AutoMigrate(model tables) error = %v", err)
	}
	codec, err := secret.NewCodec("test-secret")
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	modelLogic, err := logics.NewModelLogic(deps.db, nil, codec)
	if err != nil {
		t.Fatalf("NewModelLogic() error = %v", err)
	}

	engine := rest.Init()
	authMiddleware := NewAuthMiddleware(deps.authLogic)
	NewAdminModelHandler(modelLogic, deps.auditLogic, fakeModelTester{}, authMiddleware.RequireAdmin()).Register(engine.Group("/api/v1"))
	server := httptest.NewServer(engine)
	defer server.Close()

	response := doAdminRequest(t, http.MethodPost, server.URL+"/api/v1/admin/models/custom", map[string]any{
		"provider_id":        "rollback-provider",
		"model_id":           "rollback-model",
		"display_name":       "Rollback Model",
		"provider_type":      coretypes.LLMTypeOpenAICompletions,
		"api_key":            "rollback-secret",
		"context_max_tokens": int64(32768),
	}, deps.adminCookie)
	defer response.Body.Close()
	envelope := decodeEnvelope(t, response.Body)
	if envelope.OK {
		t.Fatal("create with failing audit OK = true, want rollback error")
	}

	var count int64
	if err := deps.db.Model(&models.CustomLLMModel{}).Where("provider_id = ?", "rollback-provider").Count(&count).Error; err != nil {
		t.Fatalf("count custom models: %v", err)
	}
	if count != 0 {
		t.Fatalf("custom model count = %d, want rollback to 0", count)
	}
}

func TestAdminModelHandlerYAMLResponsesExposeScopeEnabledAndAuditChanges(t *testing.T) {
	deps, _ := newAdminHandlerTestServer(t)
	if err := deps.db.AutoMigrate(&models.LLMModelOverride{}, &models.CustomLLMModel{}); err != nil {
		t.Fatalf("AutoMigrate(model tables) error = %v", err)
	}
	codec, err := secret.NewCodec("test-secret")
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	modelLogic, err := logics.NewModelLogic(deps.db, []coretypes.LLMProvider{{
		BaseProvider: coretypes.BaseProvider{Name: "yaml"},
		Models: []coretypes.LLMModel{{
			BaseModel: coretypes.BaseModel{ID: "admin-only", Name: "Admin Only"},
			Type:      coretypes.LLMTypeOpenAIResponses,
		}},
	}}, codec)
	if err != nil {
		t.Fatalf("NewModelLogic() error = %v", err)
	}
	engine := rest.Init()
	authMiddleware := NewAuthMiddleware(deps.authLogic)
	NewAdminModelHandler(modelLogic, deps.auditLogic, fakeModelTester{}, authMiddleware.RequireAdmin()).Register(engine.Group("/api/v1"))
	server := httptest.NewServer(engine)
	defer server.Close()

	listResponse := doAdminRequest(t, http.MethodGet, server.URL+"/api/v1/admin/models", nil, deps.adminCookie)
	defer listResponse.Body.Close()
	listEnvelope := decodeEnvelope(t, listResponse.Body)
	if !listEnvelope.OK {
		t.Fatalf("list response OK = false, message = %s", listEnvelope.Message)
	}
	var listPayload struct {
		Providers []struct {
			Models []struct {
				ID      string `json:"id"`
				Scope   string `json:"scope"`
				Enabled bool   `json:"enabled"`
			} `json:"models"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(listEnvelope.Data, &listPayload); err != nil {
		t.Fatalf("Unmarshal list payload error = %v", err)
	}
	if len(listPayload.Providers) != 1 || len(listPayload.Providers[0].Models) != 1 {
		t.Fatalf("list payload = %#v, want one yaml model", listPayload)
	}
	if got := listPayload.Providers[0].Models[0]; got.ID != "admin-only" || got.Scope != logics.ModelScopeAdmin || !got.Enabled {
		t.Fatalf("list model = %#v, want admin-only scope=admin enabled=true", got)
	}

	updateResponse := doAdminRequest(t, http.MethodPatch, server.URL+"/api/v1/admin/models/yaml/yaml/admin-only", map[string]any{
		"enabled": false,
		"scope":   logics.ModelScopeGlobal,
	}, deps.adminCookie)
	defer updateResponse.Body.Close()
	updateEnvelope := decodeEnvelope(t, updateResponse.Body)
	if !updateEnvelope.OK {
		t.Fatalf("update response OK = false, message = %s", updateEnvelope.Message)
	}
	var updated struct {
		ID      string `json:"id"`
		Scope   string `json:"scope"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.Unmarshal(updateEnvelope.Data, &updated); err != nil {
		t.Fatalf("Unmarshal updated payload error = %v", err)
	}
	if updated.ID != "admin-only" || updated.Scope != logics.ModelScopeGlobal || updated.Enabled {
		t.Fatalf("updated model = %#v, want global disabled admin-only", updated)
	}

	var event models.AdminAuditEvent
	if err := deps.db.Where("action = ?", "admin.models.yaml.update").Take(&event).Error; err != nil {
		t.Fatalf("load audit event: %v", err)
	}
	afterJSON := string(event.AfterJSON)
	if !strings.Contains(afterJSON, `"scope":"global"`) || !strings.Contains(afterJSON, `"enabled":false`) {
		t.Fatalf("audit after_json = %s, want scope and enabled fields", afterJSON)
	}
}

type fakeModelTester struct{}

func (fakeModelTester) TestModel(ctx context.Context, resolved *coreagent.ResolvedModel) error {
	return nil
}

type failingModelTester struct {
	err error
}

func (t failingModelTester) TestModel(context.Context, *coreagent.ResolvedModel) error {
	return t.err
}
