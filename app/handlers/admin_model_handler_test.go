package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

type fakeModelTester struct{}

func (fakeModelTester) TestModel(ctx context.Context, resolved *coreagent.ResolvedModel) error {
	return nil
}
