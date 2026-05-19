package logics

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/EquentR/agent_runtime/app/models"
	coreagent "github.com/EquentR/agent_runtime/core/agent"
	coretypes "github.com/EquentR/agent_runtime/core/types"
	"github.com/EquentR/agent_runtime/pkg/secret"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestModelLogicFiltersCatalogByUserRoleAndOwnership(t *testing.T) {
	logic := newModelLogicForTest(t, []coretypes.LLMProvider{{
		BaseProvider: coretypes.BaseProvider{Name: "yaml"},
		Models: []coretypes.LLMModel{
			{BaseModel: coretypes.BaseModel{ID: "admin-only", Name: "Admin Only"}, Type: coretypes.LLMTypeOpenAIResponses},
			{BaseModel: coretypes.BaseModel{ID: "global", Name: "Global"}, Type: coretypes.LLMTypeOpenAIResponses, Scope: "global"},
		},
	}})
	admin := models.User{ID: 1, Username: "root", Role: models.UserRoleAdmin}
	alice := models.User{ID: 2, Username: "alice", Role: models.UserRoleUser}
	bob := models.User{ID: 3, Username: "bob", Role: models.UserRoleUser}
	_, err := logic.CreateCustomModel(context.Background(), CreateCustomModelInput{
		OwnerUserID:      alice.ID,
		ProviderID:       "alice-provider",
		ModelID:          "alice-model",
		DisplayName:      "Alice Model",
		ProviderType:     coretypes.LLMTypeOpenAICompletions,
		APIKey:           "alice-secret",
		Scope:            "owner",
		Enabled:          true,
		ContextMaxTokens: 32768,
	})
	if err != nil {
		t.Fatalf("CreateCustomModel(alice) error = %v", err)
	}
	_, err = logic.CreateCustomModel(context.Background(), CreateCustomModelInput{
		OwnerUserID:      bob.ID,
		ProviderID:       "bob-provider",
		ModelID:          "bob-model",
		DisplayName:      "Bob Model",
		ProviderType:     coretypes.LLMTypeOpenAICompletions,
		APIKey:           "bob-secret",
		Scope:            "owner",
		Enabled:          true,
		ContextMaxTokens: 32768,
	})
	if err != nil {
		t.Fatalf("CreateCustomModel(bob) error = %v", err)
	}

	adminCatalog, err := logic.CatalogForUser(context.Background(), admin)
	if err != nil {
		t.Fatalf("CatalogForUser(admin) error = %v", err)
	}
	assertModelCatalogContains(t, adminCatalog, "yaml", "admin-only")
	assertModelCatalogContains(t, adminCatalog, "yaml", "global")
	assertModelCatalogNotContains(t, adminCatalog, "alice-provider", "alice-model")
	assertModelCatalogNotContains(t, adminCatalog, "bob-provider", "bob-model")

	aliceCatalog, err := logic.CatalogForUser(context.Background(), alice)
	if err != nil {
		t.Fatalf("CatalogForUser(alice) error = %v", err)
	}
	assertModelCatalogNotContains(t, aliceCatalog, "yaml", "admin-only")
	assertModelCatalogContains(t, aliceCatalog, "yaml", "global")
	assertModelCatalogContains(t, aliceCatalog, "alice-provider", "alice-model")
	assertModelCatalogNotContains(t, aliceCatalog, "bob-provider", "bob-model")
}

func TestModelLogicSharesCustomModelsByScope(t *testing.T) {
	logic := newModelLogicForTest(t, nil)
	ownerAdmin := models.User{ID: 1, Username: "root", Role: models.UserRoleAdmin}
	otherAdmin := models.User{ID: 2, Username: "ops", Role: models.UserRoleAdmin}
	alice := models.User{ID: 3, Username: "alice", Role: models.UserRoleUser}

	_, err := logic.CreateCustomModel(context.Background(), CreateCustomModelInput{
		OwnerUserID:      ownerAdmin.ID,
		ProviderID:       "shared-global",
		ModelID:          "global-model",
		DisplayName:      "Shared Global",
		ProviderType:     coretypes.LLMTypeOpenAICompletions,
		APIKey:           "global-secret",
		Scope:            ModelScopeGlobal,
		Enabled:          true,
		ContextMaxTokens: 32768,
	})
	if err != nil {
		t.Fatalf("CreateCustomModel(global) error = %v", err)
	}
	_, err = logic.CreateCustomModel(context.Background(), CreateCustomModelInput{
		OwnerUserID:      ownerAdmin.ID,
		ProviderID:       "shared-admin",
		ModelID:          "admin-model",
		DisplayName:      "Shared Admin",
		ProviderType:     coretypes.LLMTypeOpenAICompletions,
		APIKey:           "admin-secret",
		Scope:            ModelScopeAdmin,
		Enabled:          true,
		ContextMaxTokens: 32768,
	})
	if err != nil {
		t.Fatalf("CreateCustomModel(admin) error = %v", err)
	}

	aliceCatalog, err := logic.CatalogForUser(context.Background(), alice)
	if err != nil {
		t.Fatalf("CatalogForUser(alice) error = %v", err)
	}
	assertModelCatalogContains(t, aliceCatalog, "shared-global", "global-model")
	assertModelCatalogNotContains(t, aliceCatalog, "shared-admin", "admin-model")
	if _, err := logic.ResolveForUse(context.Background(), alice, "shared-global", "global-model"); err != nil {
		t.Fatalf("ResolveForUse(alice global) error = %v", err)
	}
	if _, err := logic.ResolveForUse(context.Background(), alice, "shared-admin", "admin-model"); !errors.Is(err, ErrModelUnauthorized) {
		t.Fatalf("ResolveForUse(alice admin) error = %v, want ErrModelUnauthorized", err)
	}

	adminCatalog, err := logic.CatalogForUser(context.Background(), otherAdmin)
	if err != nil {
		t.Fatalf("CatalogForUser(otherAdmin) error = %v", err)
	}
	assertModelCatalogNotContains(t, adminCatalog, "shared-global", "global-model")
	assertModelCatalogNotContains(t, adminCatalog, "shared-admin", "admin-model")
	if _, err := logic.ResolveForUse(context.Background(), otherAdmin, "shared-global", "global-model"); !errors.Is(err, ErrModelUnauthorized) {
		t.Fatalf("ResolveForUse(otherAdmin global) error = %v, want ErrModelUnauthorized", err)
	}
	if _, err := logic.ResolveForUse(context.Background(), otherAdmin, "shared-admin", "admin-model"); !errors.Is(err, ErrModelUnauthorized) {
		t.Fatalf("ResolveForUse(otherAdmin admin) error = %v, want ErrModelUnauthorized", err)
	}

	ownerCatalog, err := logic.CatalogForUser(context.Background(), ownerAdmin)
	if err != nil {
		t.Fatalf("CatalogForUser(ownerAdmin) error = %v", err)
	}
	assertModelCatalogContains(t, ownerCatalog, "shared-global", "global-model")
	assertModelCatalogContains(t, ownerCatalog, "shared-admin", "admin-model")
	if _, err := logic.ResolveForUse(context.Background(), ownerAdmin, "shared-admin", "admin-model"); err != nil {
		t.Fatalf("ResolveForUse(ownerAdmin admin) error = %v", err)
	}
}

func TestModelLogicYAMLOverridePatchPreservesUnspecifiedFields(t *testing.T) {
	enabled := true
	logic := newModelLogicForTest(t, []coretypes.LLMProvider{{
		BaseProvider: coretypes.BaseProvider{Name: "yaml"},
		Models: []coretypes.LLMModel{{
			BaseModel: coretypes.BaseModel{ID: "shared", Name: "Shared"},
			Type:      coretypes.LLMTypeOpenAIResponses,
			Scope:     ModelScopeGlobal,
			Enabled:   &enabled,
		}},
	}})
	disabled := false
	if _, err := logic.UpdateYAMLModelOverride(context.Background(), UpdateYAMLModelOverrideInput{
		ProviderID: "yaml",
		ModelID:    "shared",
		Scope:      ModelScopeAdmin,
		Enabled:    &disabled,
	}); err != nil {
		t.Fatalf("UpdateYAMLModelOverride(initial) error = %v", err)
	}

	reEnabled := true
	enabledOnly, err := logic.UpdateYAMLModelOverride(context.Background(), UpdateYAMLModelOverrideInput{
		ProviderID: "yaml",
		ModelID:    "shared",
		Enabled:    &reEnabled,
	})
	if err != nil {
		t.Fatalf("UpdateYAMLModelOverride(enabled-only) error = %v", err)
	}
	if enabledOnly.Scope != ModelScopeAdmin || !enabledOnly.Enabled {
		t.Fatalf("enabled-only update = %#v, want preserved scope=admin enabled=true", enabledOnly)
	}

	scopeOnly, err := logic.UpdateYAMLModelOverride(context.Background(), UpdateYAMLModelOverrideInput{
		ProviderID: "yaml",
		ModelID:    "shared",
		Scope:      ModelScopeGlobal,
	})
	if err != nil {
		t.Fatalf("UpdateYAMLModelOverride(scope-only) error = %v", err)
	}
	if scopeOnly.Scope != ModelScopeGlobal || !scopeOnly.Enabled {
		t.Fatalf("scope-only update = %#v, want scope=global preserved enabled=true", scopeOnly)
	}

	_, err = logic.UpdateYAMLModelOverride(context.Background(), UpdateYAMLModelOverrideInput{
		ProviderID: "yaml",
		ModelID:    "shared",
		Scope:      "public",
	})
	if !errors.Is(err, ErrModelInvalidScope) {
		t.Fatalf("UpdateYAMLModelOverride(invalid scope) error = %v, want ErrModelInvalidScope", err)
	}
}

func TestModelLogicCustomModelContextBudgetDefaultsOutputToQuarterCappedAt8192(t *testing.T) {
	logic := newModelLogicForTest(t, nil)
	created, err := logic.CreateCustomModel(context.Background(), CreateCustomModelInput{
		OwnerUserID:      1,
		ProviderID:       "custom",
		ModelID:          "large",
		DisplayName:      "Large",
		ProviderType:     coretypes.LLMTypeOpenAICompletions,
		APIKey:           "secret",
		ContextMaxTokens: 100000,
	})
	if err != nil {
		t.Fatalf("CreateCustomModel() error = %v", err)
	}
	if created.Context.Max != 100000 || created.Context.Output != 8192 || created.Context.Input != 91808 {
		t.Fatalf("created.Context = %#v, want max=100000 input=91808 output=8192", created.Context)
	}

	small, err := logic.CreateCustomModel(context.Background(), CreateCustomModelInput{
		OwnerUserID:      1,
		ProviderID:       "small-provider",
		ModelID:          "small",
		DisplayName:      "Small",
		ProviderType:     coretypes.LLMTypeOpenAICompletions,
		APIKey:           "secret",
		ContextMaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("CreateCustomModel(small) error = %v", err)
	}
	if small.Context.Max != 100 || small.Context.Output != 25 || small.Context.Input != 75 {
		t.Fatalf("small.Context = %#v, want max=100 input=75 output=25", small.Context)
	}
}

func TestModelLogicRejectsCustomModelContextBelowFour(t *testing.T) {
	logic := newModelLogicForTest(t, nil)
	_, err := logic.CreateCustomModel(context.Background(), CreateCustomModelInput{
		OwnerUserID:      1,
		ProviderID:       "too-small",
		ModelID:          "tiny",
		DisplayName:      "Tiny",
		ProviderType:     coretypes.LLMTypeOpenAICompletions,
		APIKey:           "secret",
		ContextMaxTokens: 3,
	})
	if !errors.Is(err, ErrModelContextTooSmall) {
		t.Fatalf("CreateCustomModel() error = %v, want ErrModelContextTooSmall", err)
	}
}

func TestModelLogicRejectsCustomModelMissingRequiredFields(t *testing.T) {
	logic := newModelLogicForTest(t, nil)
	_, err := logic.CreateCustomModel(context.Background(), CreateCustomModelInput{
		OwnerUserID:      1,
		ProviderID:       "  ",
		ModelID:          "model",
		DisplayName:      "Model",
		ProviderType:     coretypes.LLMTypeOpenAICompletions,
		APIKey:           "secret",
		ContextMaxTokens: 32768,
	})
	if err == nil {
		t.Fatal("CreateCustomModel(empty provider_id) error = nil, want validation error")
	}

	_, err = logic.CreateCustomModel(context.Background(), CreateCustomModelInput{
		OwnerUserID:      1,
		ProviderID:       "provider",
		ModelID:          "model",
		DisplayName:      "Model",
		ProviderType:     "unsupported",
		APIKey:           "secret",
		ContextMaxTokens: 32768,
	})
	if err == nil {
		t.Fatal("CreateCustomModel(unsupported provider_type) error = nil, want validation error")
	}
}

func TestModelLogicRejectsCustomProviderIDCollidingWithYAMLProvider(t *testing.T) {
	logic := newModelLogicForTest(t, []coretypes.LLMProvider{{
		BaseProvider: coretypes.BaseProvider{Name: "openai"},
		Models: []coretypes.LLMModel{{
			BaseModel: coretypes.BaseModel{ID: "gpt", Name: "GPT"},
			Type:      coretypes.LLMTypeOpenAIResponses,
		}},
	}})
	_, err := logic.CreateCustomModel(context.Background(), CreateCustomModelInput{
		OwnerUserID:      1,
		ProviderID:       " OpenAI ",
		ModelID:          "custom",
		DisplayName:      "Custom",
		ProviderType:     coretypes.LLMTypeOpenAICompletions,
		APIKey:           "secret",
		ContextMaxTokens: 32768,
	})
	if !errors.Is(err, ErrModelProviderConflict) {
		t.Fatalf("CreateCustomModel() error = %v, want ErrModelProviderConflict", err)
	}

	created, err := logic.CreateCustomModel(context.Background(), CreateCustomModelInput{
		OwnerUserID:      1,
		ProviderID:       "custom-openai",
		ModelID:          "custom",
		DisplayName:      "Custom",
		ProviderType:     coretypes.LLMTypeOpenAICompletions,
		APIKey:           "secret",
		ContextMaxTokens: 32768,
	})
	if err != nil {
		t.Fatalf("CreateCustomModel(custom-openai) error = %v", err)
	}
	_, err = logic.UpdateCustomModel(context.Background(), created.ID, UpdateCustomModelInput{
		ProviderID: "openai",
	})
	if !errors.Is(err, ErrModelProviderConflict) {
		t.Fatalf("UpdateCustomModel() error = %v, want ErrModelProviderConflict", err)
	}
}

func TestModelLogicRejectsSharedCustomModelSelectionCollisions(t *testing.T) {
	logic := newModelLogicForTest(t, nil)
	_, err := logic.CreateCustomModel(context.Background(), CreateCustomModelInput{
		OwnerUserID:      1,
		ProviderID:       "shared-provider",
		ModelID:          "shared-model",
		DisplayName:      "Shared One",
		ProviderType:     coretypes.LLMTypeOpenAICompletions,
		APIKey:           "secret-one",
		Scope:            ModelScopeGlobal,
		Enabled:          true,
		ContextMaxTokens: 32768,
	})
	if err != nil {
		t.Fatalf("CreateCustomModel(first shared) error = %v", err)
	}

	_, err = logic.CreateCustomModel(context.Background(), CreateCustomModelInput{
		OwnerUserID:      2,
		ProviderID:       " SHARED-PROVIDER ",
		ModelID:          " Shared-Model ",
		DisplayName:      "Shared Two",
		ProviderType:     coretypes.LLMTypeOpenAICompletions,
		APIKey:           "secret-two",
		Scope:            ModelScopeAdmin,
		Enabled:          true,
		ContextMaxTokens: 32768,
	})
	if !errors.Is(err, ErrModelSelectionConflict) {
		t.Fatalf("CreateCustomModel(second shared same provider/model) error = %v, want ErrModelSelectionConflict", err)
	}

	_, err = logic.CreateCustomModel(context.Background(), CreateCustomModelInput{
		OwnerUserID:      2,
		ProviderID:       "shared-provider",
		ModelID:          "shared-model",
		DisplayName:      "Owner Collision",
		ProviderType:     coretypes.LLMTypeOpenAICompletions,
		APIKey:           "secret-owner",
		Scope:            ModelScopeOwner,
		Enabled:          true,
		ContextMaxTokens: 32768,
	})
	if !errors.Is(err, ErrModelSelectionConflict) {
		t.Fatalf("CreateCustomModel(owner same provider/model as shared) error = %v, want ErrModelSelectionConflict", err)
	}

	ownerOnly, err := logic.CreateCustomModel(context.Background(), CreateCustomModelInput{
		OwnerUserID:      2,
		ProviderID:       "owner-provider",
		ModelID:          "owner-model",
		DisplayName:      "Owner Only",
		ProviderType:     coretypes.LLMTypeOpenAICompletions,
		APIKey:           "secret-three",
		Scope:            ModelScopeOwner,
		Enabled:          true,
		ContextMaxTokens: 32768,
	})
	if err != nil {
		t.Fatalf("CreateCustomModel(owner) error = %v", err)
	}
	_, err = logic.UpdateCustomModel(context.Background(), ownerOnly.ID, UpdateCustomModelInput{
		ProviderID: "shared-provider",
		ModelID:    "shared-model",
		Scope:      ModelScopeGlobal,
	})
	if !errors.Is(err, ErrModelSelectionConflict) {
		t.Fatalf("UpdateCustomModel(owner to colliding global) error = %v, want ErrModelSelectionConflict", err)
	}
}

func TestModelLogicRejectsDuplicateVisibleCustomSelectionsAtRuntime(t *testing.T) {
	logic := newModelLogicForTest(t, nil)
	rows := []models.CustomLLMModel{
		{
			ID:               "dirty_shared_1",
			OwnerUserID:      1,
			ProviderID:       "dirty-provider",
			ModelID:          "dirty-model",
			DisplayName:      "Dirty One",
			ProviderType:     coretypes.LLMTypeOpenAICompletions,
			Scope:            ModelScopeGlobal,
			Enabled:          true,
			ContextMaxTokens: 32768,
			CreatedAt:        time.Now().UTC(),
			UpdatedAt:        time.Now().UTC(),
		},
		{
			ID:               "dirty_shared_2",
			OwnerUserID:      2,
			ProviderID:       "DIRTY-PROVIDER",
			ModelID:          "Dirty-Model",
			DisplayName:      "Dirty Two",
			ProviderType:     coretypes.LLMTypeOpenAICompletions,
			Scope:            ModelScopeGlobal,
			Enabled:          true,
			ContextMaxTokens: 32768,
			CreatedAt:        time.Now().UTC(),
			UpdatedAt:        time.Now().UTC(),
		},
	}
	if err := logic.DB().Create(&rows).Error; err != nil {
		t.Fatalf("seed duplicate custom models: %v", err)
	}
	user := models.User{ID: 3, Username: "alice", Role: models.UserRoleUser}

	_, err := logic.CatalogForUser(context.Background(), user)
	if !errors.Is(err, ErrModelSelectionConflict) {
		t.Fatalf("CatalogForUser(duplicate visible custom selections) error = %v, want ErrModelSelectionConflict", err)
	}
	_, err = logic.ResolveForUse(context.Background(), user, "dirty-provider", "dirty-model")
	if !errors.Is(err, ErrModelSelectionConflict) {
		t.Fatalf("ResolveForUse(duplicate visible custom selections) error = %v, want ErrModelSelectionConflict", err)
	}
}

func TestModelLogicRejectsInvalidCustomModelScope(t *testing.T) {
	logic := newModelLogicForTest(t, nil)
	_, err := logic.CreateCustomModel(context.Background(), CreateCustomModelInput{
		OwnerUserID:      1,
		ProviderID:       "invalid-scope-provider",
		ModelID:          "invalid-scope-model",
		DisplayName:      "Invalid Scope",
		ProviderType:     coretypes.LLMTypeOpenAICompletions,
		APIKey:           "secret",
		Scope:            "public",
		Enabled:          true,
		ContextMaxTokens: 32768,
	})
	if !errors.Is(err, ErrModelInvalidScope) {
		t.Fatalf("CreateCustomModel(invalid scope) error = %v, want ErrModelInvalidScope", err)
	}

	created, err := logic.CreateCustomModel(context.Background(), CreateCustomModelInput{
		OwnerUserID:      1,
		ProviderID:       "valid-scope-provider",
		ModelID:          "valid-scope-model",
		DisplayName:      "Valid Scope",
		ProviderType:     coretypes.LLMTypeOpenAICompletions,
		APIKey:           "secret",
		Scope:            ModelScopeOwner,
		Enabled:          true,
		ContextMaxTokens: 32768,
	})
	if err != nil {
		t.Fatalf("CreateCustomModel(valid) error = %v", err)
	}
	_, err = logic.UpdateCustomModel(context.Background(), created.ID, UpdateCustomModelInput{Scope: "public"})
	if !errors.Is(err, ErrModelInvalidScope) {
		t.Fatalf("UpdateCustomModel(invalid scope) error = %v, want ErrModelInvalidScope", err)
	}
}

func TestModelLogicUpdateCustomModelCanClearBaseURL(t *testing.T) {
	logic := newModelLogicForTest(t, nil)
	created, err := logic.CreateCustomModel(context.Background(), CreateCustomModelInput{
		OwnerUserID:      1,
		ProviderID:       "base-url-provider",
		ModelID:          "base-url-model",
		DisplayName:      "Base URL",
		ProviderType:     coretypes.LLMTypeOpenAICompletions,
		BaseURL:          "https://api.example.com/v1",
		APIKey:           "secret",
		Scope:            ModelScopeOwner,
		Enabled:          true,
		ContextMaxTokens: 32768,
	})
	if err != nil {
		t.Fatalf("CreateCustomModel() error = %v", err)
	}
	updated, err := logic.UpdateCustomModel(context.Background(), created.ID, UpdateCustomModelInput{
		ClearBaseURL: true,
	})
	if err != nil {
		t.Fatalf("UpdateCustomModel(ClearBaseURL) error = %v", err)
	}
	if updated.BaseURL != "" {
		t.Fatalf("updated.BaseURL = %q, want empty", updated.BaseURL)
	}

	var stored models.CustomLLMModel
	if err := logic.DB().Take(&stored, "id = ?", created.ID).Error; err != nil {
		t.Fatalf("load stored custom model: %v", err)
	}
	if stored.BaseURL != "" {
		t.Fatalf("stored.BaseURL = %q, want empty", stored.BaseURL)
	}
}

func TestModelLogicMasksCustomModelAPIKey(t *testing.T) {
	logic := newModelLogicForTest(t, nil)
	created, err := logic.CreateCustomModel(context.Background(), CreateCustomModelInput{
		OwnerUserID:      1,
		ProviderID:       "masked-provider",
		ModelID:          "masked",
		DisplayName:      "Masked",
		ProviderType:     coretypes.LLMTypeOpenAICompletions,
		APIKey:           "sk-1234567890",
		ContextMaxTokens: 32768,
	})
	if err != nil {
		t.Fatalf("CreateCustomModel() error = %v", err)
	}
	if created.APIKey != "" {
		t.Fatalf("created.APIKey = %q, want omitted plaintext", created.APIKey)
	}
	if created.APIKeyMasked != "sk-1****7890" {
		t.Fatalf("created.APIKeyMasked = %q, want masked key", created.APIKeyMasked)
	}

	var stored models.CustomLLMModel
	if err := logic.DB().First(&stored, "id = ?", created.ID).Error; err != nil {
		t.Fatalf("load stored custom model: %v", err)
	}
	if stored.EncryptedAPIKey == "sk-1234567890" {
		t.Fatal("stored EncryptedAPIKey equals plaintext, want encrypted value")
	}
	resolved, err := logic.ResolveForUse(context.Background(), models.User{ID: 1, Username: "alice", Role: models.UserRoleUser}, "masked-provider", "masked")
	if err != nil {
		t.Fatalf("ResolveForUse() error = %v", err)
	}
	if resolved.Provider == nil || resolved.Provider.AuthKey() != "sk-1234567890" {
		t.Fatalf("resolved provider auth key = %q, want decrypted API key", resolved.Provider.AuthKey())
	}
}

func TestModelLogicResolverLoadsTaskUserIdentityFromDB(t *testing.T) {
	logic := newModelLogicForTest(t, []coretypes.LLMProvider{{
		BaseProvider: coretypes.BaseProvider{Name: "yaml"},
		Models: []coretypes.LLMModel{
			{BaseModel: coretypes.BaseModel{ID: "admin-only", Name: "Admin Only"}, Type: coretypes.LLMTypeOpenAIResponses},
		},
	}})
	if err := logic.DB().AutoMigrate(&models.User{}); err != nil {
		t.Fatalf("AutoMigrate(users) error = %v", err)
	}
	verifiedAt := time.Now().UTC()
	admin := models.User{
		Username:        "root",
		Email:           "root@example.com",
		DisplayName:     "root",
		PasswordHash:    "hash",
		Role:            models.UserRoleAdmin,
		Status:          models.UserStatusActive,
		EmailVerifiedAt: &verifiedAt,
	}
	if err := logic.DB().Create(&admin).Error; err != nil {
		t.Fatalf("seed admin: %v", err)
	}

	resolver := logic.Resolver()
	if _, err := resolver.ResolveTask(context.Background(), coreagent.RunTaskInput{
		UserID:     strconv.FormatUint(admin.ID, 10),
		ProviderID: "yaml",
		ModelID:    "admin-only",
		CreatedBy:  "ignored",
	}); err != nil {
		t.Fatalf("ResolveTask(user_id) error = %v, want admin model resolved", err)
	}
	if _, err := resolver.ResolveTask(context.Background(), coreagent.RunTaskInput{
		ProviderID: "yaml",
		ModelID:    "admin-only",
		CreatedBy:  admin.Username,
	}); err != nil {
		t.Fatalf("ResolveTask(created_by) error = %v, want admin model resolved", err)
	}
}

func newModelLogicForTest(t *testing.T, providers []coretypes.LLMProvider) *ModelLogic {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	if err := db.AutoMigrate(&models.LLMModelOverride{}, &models.CustomLLMModel{}); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	codec, err := secret.NewCodec("test-secret")
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	logic, err := NewModelLogic(db, providers, codec)
	if err != nil {
		t.Fatalf("NewModelLogic() error = %v", err)
	}
	return logic
}

func assertModelCatalogContains(t *testing.T, catalog coreagent.ModelCatalog, providerID string, modelID string) {
	t.Helper()
	if !modelCatalogHas(catalog, providerID, modelID) {
		t.Fatalf("catalog = %#v, want %s/%s", catalog, providerID, modelID)
	}
}

func assertModelCatalogNotContains(t *testing.T, catalog coreagent.ModelCatalog, providerID string, modelID string) {
	t.Helper()
	if modelCatalogHas(catalog, providerID, modelID) {
		t.Fatalf("catalog = %#v, want no %s/%s", catalog, providerID, modelID)
	}
}

func modelCatalogHas(catalog coreagent.ModelCatalog, providerID string, modelID string) bool {
	for _, provider := range catalog.Providers {
		if provider.ID != providerID {
			continue
		}
		for _, model := range provider.Models {
			if model.ID == modelID {
				return true
			}
		}
	}
	return false
}
