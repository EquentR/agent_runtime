package logics

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/EquentR/agent_runtime/app/models"
	coreagent "github.com/EquentR/agent_runtime/core/agent"
	coretypes "github.com/EquentR/agent_runtime/core/types"
	"github.com/EquentR/agent_runtime/pkg/secret"
	"gorm.io/gorm"
)

const (
	ModelScopeAdmin  = "admin"
	ModelScopeGlobal = "global"
	ModelScopeOwner  = "owner"
)

var (
	ErrModelNotFound        = errors.New("模型不存在")
	ErrModelUnauthorized    = errors.New("无权使用该模型")
	ErrModelContextTooSmall = errors.New("模型 context 上限不能小于 4")
)

type ModelLogic struct {
	db        *gorm.DB
	providers []coretypes.LLMProvider
	codec     *secret.Codec
}

type CreateCustomModelInput struct {
	OwnerUserID      uint64
	ProviderID       string
	ModelID          string
	DisplayName      string
	ProviderType     string
	BaseURL          string
	APIKey           string
	Scope            string
	Enabled          bool
	ContextMaxTokens int64
	Capabilities     coretypes.ModelCapabilities
	Cost             *coretypes.ModelPricing
}

type UpdateCustomModelInput struct {
	OwnerUserID      *uint64
	ProviderID       string
	ModelID          string
	DisplayName      string
	ProviderType     string
	BaseURL          string
	APIKey           string
	ClearAPIKey      bool
	Scope            string
	Enabled          *bool
	ContextMaxTokens int64
	Capabilities     coretypes.ModelCapabilities
	Cost             *coretypes.ModelPricing
}

type UpdateYAMLModelOverrideInput struct {
	ProviderID string
	ModelID    string
	Enabled    *bool
	Scope      string
	UpdatedBy  string
}

type CustomModelResponse struct {
	ID               string                      `json:"id"`
	OwnerUserID      uint64                      `json:"owner_user_id"`
	ProviderID       string                      `json:"provider_id"`
	ModelID          string                      `json:"model_id"`
	DisplayName      string                      `json:"display_name"`
	ProviderType     string                      `json:"provider_type"`
	BaseURL          string                      `json:"base_url"`
	APIKey           string                      `json:"api_key,omitempty"`
	APIKeyMasked     string                      `json:"api_key_masked,omitempty"`
	Scope            string                      `json:"scope"`
	Enabled          bool                        `json:"enabled"`
	ContextMaxTokens int64                       `json:"context_max_tokens"`
	Context          coretypes.LLMContextConfig  `json:"context"`
	Capabilities     coretypes.ModelCapabilities `json:"capabilities"`
	Cost             *coretypes.ModelPricing     `json:"cost,omitempty"`
	CreatedAt        time.Time                   `json:"created_at"`
	UpdatedAt        time.Time                   `json:"updated_at"`
}

func NewModelLogic(db *gorm.DB, providers []coretypes.LLMProvider, codec *secret.Codec) (*ModelLogic, error) {
	if db == nil {
		return nil, fmt.Errorf("model db is required")
	}
	if codec == nil {
		return nil, fmt.Errorf("model secret codec is required")
	}
	return &ModelLogic{db: db, providers: cloneLLMProviders(providers), codec: codec}, nil
}

func (l *ModelLogic) DB() *gorm.DB {
	if l == nil {
		return nil
	}
	return l.db
}

func (l *ModelLogic) WithDB(db *gorm.DB) *ModelLogic {
	if l == nil {
		return nil
	}
	clone := *l
	clone.db = db
	return &clone
}

func (l *ModelLogic) Transaction(ctx context.Context, fn func(*ModelLogic) error) error {
	if l == nil || l.db == nil {
		return fmt.Errorf("model db is required")
	}
	if fn == nil {
		return nil
	}
	return l.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(l.WithDB(tx))
	})
}

func (l *ModelLogic) Resolver() *coreagent.ModelResolver {
	if l == nil {
		return nil
	}
	return &coreagent.ModelResolver{
		Providers: l.providers,
		ResolveTaskFunc: func(ctx context.Context, input coreagent.RunTaskInput) (*coreagent.ResolvedModel, error) {
			user, err := l.resolveTaskUser(ctx, input)
			if err != nil {
				return nil, err
			}
			return l.ResolveForUse(ctx, user, input.ProviderID, input.ModelID)
		},
	}
}

func (l *ModelLogic) ListYAMLModels(ctx context.Context) (coreagent.ModelCatalog, error) {
	providers, err := l.yamlProvidersWithOverrides(ctx)
	if err != nil {
		return coreagent.ModelCatalog{}, err
	}
	return (&coreagent.ModelResolver{Providers: providers}).Catalog(), nil
}

func (l *ModelLogic) UpdateYAMLModelOverride(ctx context.Context, input UpdateYAMLModelOverrideInput) (coreagent.ModelOptionEntry, error) {
	if l == nil || l.db == nil {
		return coreagent.ModelOptionEntry{}, fmt.Errorf("model db is required")
	}
	providerID := strings.TrimSpace(input.ProviderID)
	modelID := strings.TrimSpace(input.ModelID)
	if providerID == "" || modelID == "" {
		return coreagent.ModelOptionEntry{}, fmt.Errorf("provider_id and model_id are required")
	}
	provider, model := findProviderModel(l.providers, providerID, modelID)
	if provider == nil || model == nil {
		return coreagent.ModelOptionEntry{}, ErrModelNotFound
	}
	scope := normalizeScope(firstNonEmptyString(input.Scope, model.EffectiveScope()), ModelScopeAdmin)
	enabled := model.IsEnabled()
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	now := time.Now().UTC()
	row := models.LLMModelOverride{
		ProviderID: providerID,
		ModelID:    modelID,
		Enabled:    enabled,
		Scope:      scope,
		UpdatedBy:  strings.TrimSpace(input.UpdatedBy),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	err := l.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing models.LLMModelOverride
		err := tx.Where("provider_id = ? AND model_id = ?", providerID, modelID).Take(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return tx.Create(&row).Error
		}
		if err != nil {
			return err
		}
		return tx.Model(&models.LLMModelOverride{}).
			Where("provider_id = ? AND model_id = ?", providerID, modelID).
			Updates(map[string]any{"enabled": enabled, "scope": scope, "updated_by": row.UpdatedBy, "updated_at": now}).Error
	})
	if err != nil {
		return coreagent.ModelOptionEntry{}, err
	}
	updated := *model
	updated.Scope = scope
	updated.Enabled = &enabled
	return modelOptionFromLLMModel(&updated), nil
}

func (l *ModelLogic) ListCustomModels(ctx context.Context) ([]CustomModelResponse, error) {
	return l.listCustomModels(ctx, 0)
}

func (l *ModelLogic) ListCustomModelsForUser(ctx context.Context, user models.User) ([]CustomModelResponse, error) {
	return l.listCustomModels(ctx, user.ID)
}

func (l *ModelLogic) CreateCustomModel(ctx context.Context, input CreateCustomModelInput) (CustomModelResponse, error) {
	if l == nil || l.db == nil {
		return CustomModelResponse{}, fmt.Errorf("model db is required")
	}
	if err := validateCreateCustomModelInput(input); err != nil {
		return CustomModelResponse{}, err
	}
	if input.ContextMaxTokens < 4 {
		return CustomModelResponse{}, ErrModelContextTooSmall
	}
	apiKey := strings.TrimSpace(input.APIKey)
	if apiKey == "" {
		return CustomModelResponse{}, fmt.Errorf("api key is required")
	}
	encrypted, err := l.codec.EncryptString(apiKey)
	if err != nil {
		return CustomModelResponse{}, err
	}
	row, err := l.customRowFromCreateInput(input, encrypted)
	if err != nil {
		return CustomModelResponse{}, err
	}
	if err := l.db.WithContext(ctx).Create(&row).Error; err != nil {
		return CustomModelResponse{}, err
	}
	return l.customModelResponse(row)
}

func (l *ModelLogic) UpdateCustomModel(ctx context.Context, id string, input UpdateCustomModelInput) (CustomModelResponse, error) {
	if l == nil || l.db == nil {
		return CustomModelResponse{}, fmt.Errorf("model db is required")
	}
	var existing models.CustomLLMModel
	if err := l.db.WithContext(ctx).Take(&existing, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return CustomModelResponse{}, ErrModelNotFound
		}
		return CustomModelResponse{}, err
	}
	if input.ContextMaxTokens > 0 && input.ContextMaxTokens < 4 {
		return CustomModelResponse{}, ErrModelContextTooSmall
	}
	if providerType := strings.TrimSpace(input.ProviderType); providerType != "" && !isSupportedCustomProviderType(providerType) {
		return CustomModelResponse{}, fmt.Errorf("unsupported provider_type %q", providerType)
	}
	updates, err := l.customUpdatesFromInput(existing, input)
	if err != nil {
		return CustomModelResponse{}, err
	}
	if err := l.db.WithContext(ctx).Model(&models.CustomLLMModel{}).Where("id = ?", existing.ID).Updates(updates).Error; err != nil {
		return CustomModelResponse{}, err
	}
	var updated models.CustomLLMModel
	if err := l.db.WithContext(ctx).Take(&updated, "id = ?", existing.ID).Error; err != nil {
		return CustomModelResponse{}, err
	}
	return l.customModelResponse(updated)
}

func (l *ModelLogic) DeleteCustomModel(ctx context.Context, id string) error {
	if l == nil || l.db == nil {
		return fmt.Errorf("model db is required")
	}
	result := l.db.WithContext(ctx).Delete(&models.CustomLLMModel{}, "id = ?", strings.TrimSpace(id))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrModelNotFound
	}
	return nil
}

func (l *ModelLogic) GetCustomModel(ctx context.Context, id string) (CustomModelResponse, error) {
	var row models.CustomLLMModel
	if err := l.db.WithContext(ctx).Take(&row, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return CustomModelResponse{}, ErrModelNotFound
		}
		return CustomModelResponse{}, err
	}
	return l.customModelResponse(row)
}

func (l *ModelLogic) CatalogForUser(ctx context.Context, user models.User) (coreagent.ModelCatalog, error) {
	yamlProviders, err := l.yamlProvidersWithOverrides(ctx)
	if err != nil {
		return coreagent.ModelCatalog{}, err
	}
	filtered := make([]coretypes.LLMProvider, 0, len(yamlProviders))
	for i := range yamlProviders {
		provider := yamlProviders[i]
		modelsOut := make([]coretypes.LLMModel, 0, len(provider.Models))
		for j := range provider.Models {
			model := provider.Models[j]
			if !model.IsEnabled() {
				continue
			}
			if userCanUseYAMLModel(user, &model) {
				modelsOut = append(modelsOut, model)
			}
		}
		if len(modelsOut) > 0 {
			provider.Models = modelsOut
			filtered = append(filtered, provider)
		}
	}
	customProviders, err := l.customProvidersForUse(ctx, user)
	if err != nil {
		return coreagent.ModelCatalog{}, err
	}
	filtered = append(filtered, customProviders...)
	return (&coreagent.ModelResolver{Providers: filtered}).Catalog(), nil
}

func (l *ModelLogic) ResolveForUse(ctx context.Context, user models.User, providerID string, modelID string) (*coreagent.ResolvedModel, error) {
	catalog, err := l.CatalogForUser(ctx, user)
	if err != nil {
		return nil, err
	}
	if !catalogHasModel(catalog, providerID, modelID) {
		return nil, ErrModelUnauthorized
	}
	yamlProviders, err := l.yamlProvidersWithOverrides(ctx)
	if err != nil {
		return nil, err
	}
	if resolved, err := (&coreagent.ModelResolver{Providers: yamlProviders}).ResolveContext(ctx, providerID, modelID); err == nil {
		if resolved.Model != nil && resolved.Model.IsEnabled() && userCanUseYAMLModel(user, resolved.Model) {
			return resolved, nil
		}
	}
	return l.resolveCustomForUse(ctx, user, providerID, modelID)
}

func (l *ModelLogic) resolveTaskUser(ctx context.Context, input coreagent.RunTaskInput) (models.User, error) {
	if l == nil || l.db == nil {
		return models.User{}, fmt.Errorf("model db is required")
	}
	var user models.User
	userID := parseUint64(input.UserID)
	query := l.db.WithContext(ctx)
	if userID != 0 {
		if err := query.Take(&user, "id = ?", userID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return models.User{}, ErrModelUnauthorized
			}
			return models.User{}, err
		}
		return user, nil
	}
	username := strings.TrimSpace(input.CreatedBy)
	if username == "" {
		return models.User{}, ErrModelUnauthorized
	}
	if err := query.Take(&user, "username = ?", username).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.User{}, ErrModelUnauthorized
		}
		return models.User{}, err
	}
	return user, nil
}

func (l *ModelLogic) ResolveCustomForAdmin(ctx context.Context, id string) (*coreagent.ResolvedModel, error) {
	var row models.CustomLLMModel
	if err := l.db.WithContext(ctx).Take(&row, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrModelNotFound
		}
		return nil, err
	}
	return l.resolvedCustom(row)
}

func (l *ModelLogic) ResolveCustomForOwner(ctx context.Context, user models.User, id string) (*coreagent.ResolvedModel, error) {
	var row models.CustomLLMModel
	if err := l.db.WithContext(ctx).Where("id = ? AND owner_user_id = ?", strings.TrimSpace(id), user.ID).Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrModelUnauthorized
		}
		return nil, err
	}
	return l.resolvedCustom(row)
}

func (l *ModelLogic) yamlProvidersWithOverrides(ctx context.Context) ([]coretypes.LLMProvider, error) {
	providers := cloneLLMProviders(l.providers)
	overrides := []models.LLMModelOverride{}
	if l != nil && l.db != nil {
		if err := l.db.WithContext(ctx).Find(&overrides).Error; err != nil {
			return nil, err
		}
	}
	byKey := make(map[string]models.LLMModelOverride, len(overrides))
	for _, override := range overrides {
		byKey[modelKey(override.ProviderID, override.ModelID)] = override
	}
	for i := range providers {
		for j := range providers[i].Models {
			override, ok := byKey[modelKey(providers[i].ProviderName(), providers[i].Models[j].ModelID())]
			if !ok {
				continue
			}
			providers[i].Models[j].Scope = normalizeScope(override.Scope, ModelScopeAdmin)
			enabled := override.Enabled
			providers[i].Models[j].Enabled = &enabled
		}
	}
	return providers, nil
}

func (l *ModelLogic) listCustomModels(ctx context.Context, ownerUserID uint64) ([]CustomModelResponse, error) {
	if l == nil || l.db == nil {
		return nil, fmt.Errorf("model db is required")
	}
	query := l.db.WithContext(ctx).Model(&models.CustomLLMModel{})
	if ownerUserID != 0 {
		query = query.Where("owner_user_id = ?", ownerUserID)
	}
	var rows []models.CustomLLMModel
	if err := query.Order("updated_at desc").Order("id desc").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]CustomModelResponse, 0, len(rows))
	for _, row := range rows {
		response, err := l.customModelResponse(row)
		if err != nil {
			return nil, err
		}
		out = append(out, response)
	}
	return out, nil
}

func (l *ModelLogic) customProvidersForUse(ctx context.Context, user models.User) ([]coretypes.LLMProvider, error) {
	if l == nil || l.db == nil || user.ID == 0 {
		return nil, nil
	}
	query := l.db.WithContext(ctx).Where("enabled = ?", true)
	if isAdminModelUser(user) {
		query = query.Where("owner_user_id = ? AND scope IN ?", user.ID, []string{ModelScopeOwner, ModelScopeAdmin, ModelScopeGlobal})
	} else {
		query = query.Where("owner_user_id = ? AND scope = ?", user.ID, ModelScopeOwner)
	}
	var rows []models.CustomLLMModel
	if err := query.Order("provider_id asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	providers := make([]coretypes.LLMProvider, 0, len(rows))
	for _, row := range rows {
		resolved, err := l.resolvedCustom(row)
		if err != nil {
			return nil, err
		}
		providers = append(providers, *resolved.Provider)
	}
	return providers, nil
}

func (l *ModelLogic) resolveCustomForUse(ctx context.Context, user models.User, providerID string, modelID string) (*coreagent.ResolvedModel, error) {
	query := l.db.WithContext(ctx).
		Where("provider_id = ? AND model_id = ? AND enabled = ?", strings.TrimSpace(providerID), strings.TrimSpace(modelID), true)
	if isAdminModelUser(user) {
		query = query.Where("owner_user_id = ?", user.ID)
	} else {
		query = query.Where("owner_user_id = ? AND scope = ?", user.ID, ModelScopeOwner)
	}
	var row models.CustomLLMModel
	if err := query.Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrModelUnauthorized
		}
		return nil, err
	}
	return l.resolvedCustom(row)
}

func (l *ModelLogic) resolvedCustom(row models.CustomLLMModel) (*coreagent.ResolvedModel, error) {
	apiKey := ""
	if strings.TrimSpace(row.EncryptedAPIKey) != "" {
		decrypted, err := l.codec.DecryptString(row.EncryptedAPIKey)
		if err != nil {
			return nil, err
		}
		apiKey = decrypted
	}
	capabilities, err := decodeModelCapabilities(row.CapabilitiesJSON)
	if err != nil {
		return nil, err
	}
	pricing, err := decodeModelPricing(row.CostJSON)
	if err != nil {
		return nil, err
	}
	provider := &coretypes.LLMProvider{
		BaseProvider: coretypes.BaseProvider{
			Name:    strings.TrimSpace(row.ProviderID),
			BaseUrl: strings.TrimSpace(row.BaseURL),
			APIKey:  apiKey,
		},
		Models: []coretypes.LLMModel{{
			BaseModel:    coretypes.BaseModel{ID: strings.TrimSpace(row.ModelID), Name: firstNonEmptyString(row.DisplayName, row.ModelID)},
			Type:         strings.TrimSpace(row.ProviderType),
			Scope:        normalizeScope(row.Scope, ModelScopeOwner),
			Enabled:      boolPtr(row.Enabled),
			Context:      customContextBudget(row.ContextMaxTokens),
			Capabilities: capabilities,
		}},
	}
	if pricing != nil {
		provider.Models[0].Cost = pricingToCostConfig(*pricing)
	}
	return &coreagent.ResolvedModel{Provider: provider, Model: &provider.Models[0]}, nil
}

func (l *ModelLogic) customRowFromCreateInput(input CreateCustomModelInput, encryptedAPIKey string) (models.CustomLLMModel, error) {
	now := time.Now().UTC()
	capabilitiesJSON, err := json.Marshal(input.Capabilities)
	if err != nil {
		return models.CustomLLMModel{}, err
	}
	costJSON, err := marshalOptionalPricing(input.Cost)
	if err != nil {
		return models.CustomLLMModel{}, err
	}
	return models.CustomLLMModel{
		ID:               newCustomModelID(),
		OwnerUserID:      input.OwnerUserID,
		ProviderID:       strings.TrimSpace(input.ProviderID),
		ModelID:          strings.TrimSpace(input.ModelID),
		DisplayName:      strings.TrimSpace(input.DisplayName),
		ProviderType:     strings.TrimSpace(input.ProviderType),
		BaseURL:          strings.TrimSpace(input.BaseURL),
		EncryptedAPIKey:  encryptedAPIKey,
		Scope:            normalizeScope(input.Scope, ModelScopeOwner),
		Enabled:          input.Enabled,
		ContextMaxTokens: input.ContextMaxTokens,
		CapabilitiesJSON: capabilitiesJSON,
		CostJSON:         costJSON,
		CreatedAt:        now,
		UpdatedAt:        now,
	}, nil
}

func (l *ModelLogic) customUpdatesFromInput(existing models.CustomLLMModel, input UpdateCustomModelInput) (map[string]any, error) {
	updates := map[string]any{"updated_at": time.Now().UTC()}
	if input.OwnerUserID != nil {
		updates["owner_user_id"] = *input.OwnerUserID
	}
	if strings.TrimSpace(input.ProviderID) != "" {
		updates["provider_id"] = strings.TrimSpace(input.ProviderID)
	}
	if strings.TrimSpace(input.ModelID) != "" {
		updates["model_id"] = strings.TrimSpace(input.ModelID)
	}
	if strings.TrimSpace(input.DisplayName) != "" {
		updates["display_name"] = strings.TrimSpace(input.DisplayName)
	}
	if strings.TrimSpace(input.ProviderType) != "" {
		updates["provider_type"] = strings.TrimSpace(input.ProviderType)
	}
	if strings.TrimSpace(input.BaseURL) != "" {
		updates["base_url"] = strings.TrimSpace(input.BaseURL)
	}
	if input.ClearAPIKey {
		updates["encrypted_api_key"] = ""
	} else if strings.TrimSpace(input.APIKey) != "" {
		encrypted, err := l.codec.EncryptString(strings.TrimSpace(input.APIKey))
		if err != nil {
			return nil, err
		}
		updates["encrypted_api_key"] = encrypted
	}
	if strings.TrimSpace(input.Scope) != "" {
		updates["scope"] = normalizeScope(input.Scope, ModelScopeOwner)
	}
	if input.Enabled != nil {
		updates["enabled"] = *input.Enabled
	}
	if input.ContextMaxTokens > 0 {
		updates["context_max_tokens"] = input.ContextMaxTokens
	}
	capabilitiesJSON, err := json.Marshal(input.Capabilities)
	if err != nil {
		return nil, err
	}
	if string(capabilitiesJSON) != "{}" || len(existing.CapabilitiesJSON) > 0 {
		updates["capabilities_json"] = capabilitiesJSON
	}
	if input.Cost != nil {
		costJSON, err := marshalOptionalPricing(input.Cost)
		if err != nil {
			return nil, err
		}
		updates["cost_json"] = costJSON
	}
	return updates, nil
}

func (l *ModelLogic) customModelResponse(row models.CustomLLMModel) (CustomModelResponse, error) {
	capabilities, err := decodeModelCapabilities(row.CapabilitiesJSON)
	if err != nil {
		return CustomModelResponse{}, err
	}
	pricing, err := decodeModelPricing(row.CostJSON)
	if err != nil {
		return CustomModelResponse{}, err
	}
	apiKeyMasked := ""
	if strings.TrimSpace(row.EncryptedAPIKey) != "" {
		decrypted, err := l.codec.DecryptString(row.EncryptedAPIKey)
		if err != nil {
			return CustomModelResponse{}, err
		}
		apiKeyMasked = secret.MaskSecret(decrypted)
	}
	return CustomModelResponse{
		ID:               row.ID,
		OwnerUserID:      row.OwnerUserID,
		ProviderID:       row.ProviderID,
		ModelID:          row.ModelID,
		DisplayName:      row.DisplayName,
		ProviderType:     row.ProviderType,
		BaseURL:          row.BaseURL,
		APIKeyMasked:     apiKeyMasked,
		Scope:            normalizeScope(row.Scope, ModelScopeOwner),
		Enabled:          row.Enabled,
		ContextMaxTokens: row.ContextMaxTokens,
		Context:          customContextBudget(row.ContextMaxTokens),
		Capabilities:     capabilities,
		Cost:             pricing,
		CreatedAt:        row.CreatedAt,
		UpdatedAt:        row.UpdatedAt,
	}, nil
}

func validateCreateCustomModelInput(input CreateCustomModelInput) error {
	if input.OwnerUserID == 0 {
		return fmt.Errorf("owner_user_id is required")
	}
	if strings.TrimSpace(input.ProviderID) == "" {
		return fmt.Errorf("provider_id is required")
	}
	if strings.TrimSpace(input.ModelID) == "" {
		return fmt.Errorf("model_id is required")
	}
	if strings.TrimSpace(input.DisplayName) == "" {
		return fmt.Errorf("display_name is required")
	}
	providerType := strings.TrimSpace(input.ProviderType)
	if providerType == "" {
		return fmt.Errorf("provider_type is required")
	}
	if !isSupportedCustomProviderType(providerType) {
		return fmt.Errorf("unsupported provider_type %q", providerType)
	}
	return nil
}

func isSupportedCustomProviderType(providerType string) bool {
	switch strings.TrimSpace(providerType) {
	case coretypes.LLMTypeOpenAIResponses, coretypes.LLMTypeOpenAICompletions, coretypes.LLMTypeGoogle:
		return true
	default:
		return false
	}
}

func modelOptionFromLLMModel(llmModel *coretypes.LLMModel) coreagent.ModelOptionEntry {
	if llmModel == nil {
		return coreagent.ModelOptionEntry{}
	}
	ctx := llmModel.ContextWindow()
	return coreagent.ModelOptionEntry{
		ID:   llmModel.ModelID(),
		Name: firstNonEmptyString(llmModel.ModelName(), llmModel.ModelID()),
		Type: llmModel.ModelType(),
		Context: coreagent.ModelContext{
			Max:    ctx.Max,
			Input:  ctx.Input,
			Output: ctx.Output,
		},
		Cost:         llmModel.Pricing(),
		Capabilities: llmModel.Capabilities,
	}
}

func customContextBudget(maxTokens int64) coretypes.LLMContextConfig {
	output := maxTokens / 4
	if output > 8192 {
		output = 8192
	}
	return coretypes.LLMContextConfig{Max: maxTokens, Input: maxTokens - output, Output: output}
}

func userCanUseYAMLModel(user models.User, llmModel *coretypes.LLMModel) bool {
	if llmModel == nil || !llmModel.IsEnabled() {
		return false
	}
	switch normalizeScope(llmModel.EffectiveScope(), ModelScopeAdmin) {
	case ModelScopeGlobal:
		return true
	case ModelScopeAdmin:
		return isAdminModelUser(user)
	default:
		return false
	}
}

func isAdminModelUser(user models.User) bool {
	return user.Role == models.UserRoleAdmin
}

func catalogHasModel(catalog coreagent.ModelCatalog, providerID string, modelID string) bool {
	for _, provider := range catalog.Providers {
		if !strings.EqualFold(provider.ID, strings.TrimSpace(providerID)) {
			continue
		}
		for _, model := range provider.Models {
			if strings.EqualFold(model.ID, strings.TrimSpace(modelID)) {
				return true
			}
		}
	}
	return false
}

func findProviderModel(providers []coretypes.LLMProvider, providerID string, modelID string) (*coretypes.LLMProvider, *coretypes.LLMModel) {
	for i := range providers {
		if !strings.EqualFold(providers[i].ProviderName(), strings.TrimSpace(providerID)) {
			continue
		}
		model := providers[i].FindModel(modelID)
		if model != nil {
			return &providers[i], model
		}
	}
	return nil, nil
}

func cloneLLMProviders(providers []coretypes.LLMProvider) []coretypes.LLMProvider {
	out := make([]coretypes.LLMProvider, len(providers))
	for i := range providers {
		out[i] = providers[i]
		out[i].Models = append([]coretypes.LLMModel(nil), providers[i].Models...)
	}
	return out
}

func normalizeScope(scope string, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case ModelScopeAdmin:
		return ModelScopeAdmin
	case ModelScopeGlobal:
		return ModelScopeGlobal
	case ModelScopeOwner:
		return ModelScopeOwner
	default:
		return fallback
	}
}

func modelKey(providerID string, modelID string) string {
	return strings.ToLower(strings.TrimSpace(providerID)) + "\x00" + strings.ToLower(strings.TrimSpace(modelID))
}

func boolPtr(value bool) *bool {
	return &value
}

func newCustomModelID() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return "custom_" + hex.EncodeToString(raw[:])
	}
	return fmt.Sprintf("custom_%d", time.Now().UTC().UnixNano())
}

func marshalOptionalPricing(pricing *coretypes.ModelPricing) ([]byte, error) {
	if pricing == nil {
		return nil, nil
	}
	return json.Marshal(pricing)
}

func decodeModelCapabilities(raw []byte) (coretypes.ModelCapabilities, error) {
	if len(raw) == 0 {
		return coretypes.ModelCapabilities{}, nil
	}
	var capabilities coretypes.ModelCapabilities
	if err := json.Unmarshal(raw, &capabilities); err != nil {
		return coretypes.ModelCapabilities{}, err
	}
	return capabilities, nil
}

func decodeModelPricing(raw []byte) (*coretypes.ModelPricing, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var pricing coretypes.ModelPricing
	if err := json.Unmarshal(raw, &pricing); err != nil {
		return nil, err
	}
	return &pricing, nil
}

func pricingToCostConfig(pricing coretypes.ModelPricing) coretypes.LLMCostConfig {
	result := coretypes.LLMCostConfig{
		Input:  floatPtr(pricing.Input.AmountUSD),
		Output: floatPtr(pricing.Output.AmountUSD),
	}
	if pricing.CachedInput != nil {
		result.CachedInput = floatPtr(pricing.CachedInput.AmountUSD)
	}
	return result
}

func floatPtr(value float64) *float64 {
	if math.IsNaN(value) {
		value = 0
	}
	return &value
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func parseUint64(value string) uint64 {
	var result uint64
	for _, ch := range strings.TrimSpace(value) {
		if ch < '0' || ch > '9' {
			return 0
		}
		result = result*10 + uint64(ch-'0')
	}
	return result
}
