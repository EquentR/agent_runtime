package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	coreagent "github.com/EquentR/agent_runtime/core/agent"
	coretypes "github.com/EquentR/agent_runtime/core/types"
	"github.com/EquentR/agent_runtime/pkg/rest"
)

func TestModelCatalogHandlerReturnsConfiguredProvidersAndDefaultSelection(t *testing.T) {
	engine := rest.Init()
	resolver := &coreagent.ModelResolver{Providers: []coretypes.LLMProvider{
		{
			BaseProvider: coretypes.BaseProvider{Name: "openai"},
			Models: []coretypes.LLMModel{
				{BaseModel: coretypes.BaseModel{ID: "gpt-5.4", Name: "GPT 5.4"}, Type: coretypes.LLMTypeOpenAIResponses},
				{BaseModel: coretypes.BaseModel{ID: "gpt-4.1-mini", Name: "GPT 4.1 Mini"}, Type: coretypes.LLMTypeOpenAIResponses},
			},
		},
		{
			BaseProvider: coretypes.BaseProvider{Name: "google"},
			Models: []coretypes.LLMModel{
				{BaseModel: coretypes.BaseModel{ID: "gemini-2.5-flash", Name: "Gemini 2.5 Flash"}, Type: coretypes.LLMTypeGoogle},
			},
		},
	}}
	NewModelCatalogHandler(resolver).Register(engine.Group("/api/v1"))
	server := httptest.NewServer(engine)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/models")
	if err != nil {
		t.Fatalf("http.Get() error = %v", err)
	}
	defer resp.Body.Close()

	var envelope taskTestResponse
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("Decode() envelope error = %v", err)
	}
	if !envelope.OK {
		t.Fatalf("response ok = false, raw data = %s", string(envelope.Data))
	}

	var payload struct {
		DefaultProviderID string `json:"default_provider_id"`
		DefaultModelID    string `json:"default_model_id"`
		Providers         []struct {
			ID     string `json:"id"`
			Models []struct {
				ID string `json:"id"`
			} `json:"models"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(envelope.Data, &payload); err != nil {
		t.Fatalf("Unmarshal() payload error = %v", err)
	}
	if payload.DefaultProviderID != "openai" || payload.DefaultModelID != "gpt-5.4" {
		t.Fatalf("default selection = %#v, want openai/gpt-5.4", payload)
	}
	if len(payload.Providers) != 2 {
		t.Fatalf("len(providers) = %d, want 2", len(payload.Providers))
	}
	if payload.Providers[0].ID != "openai" || len(payload.Providers[0].Models) != 2 {
		t.Fatalf("providers[0] = %#v, want openai with two models", payload.Providers[0])
	}
	if payload.Providers[1].ID != "google" || payload.Providers[1].Models[0].ID != "gemini-2.5-flash" {
		t.Fatalf("providers[1] = %#v, want google/gemini-2.5-flash", payload.Providers[1])
	}
}

func TestModelCatalogHandlerDoesNotExposeTaskTwoMemoryContextWindowOverreach(t *testing.T) {
	engine := rest.Init()
	inputCost := 0.5
	outputCost := 1.5
	cachedInputCost := 0.25
	resolver := &coreagent.ModelResolver{Providers: []coretypes.LLMProvider{{
		BaseProvider: coretypes.BaseProvider{Name: "openai"},
		Models: []coretypes.LLMModel{{
			BaseModel: coretypes.BaseModel{ID: "gpt-5.4", Name: "GPT 5.4"},
			Type:      coretypes.LLMTypeOpenAIResponses,
			Context:   coretypes.LLMContextConfig{Max: 128000, Input: 96000, Output: 4000},
			Cost:      coretypes.LLMCostConfig{Input: &inputCost, Output: &outputCost, CachedInput: &cachedInputCost},
		}},
	}}}
	NewModelCatalogHandler(resolver).Register(engine.Group("/api/v1"))
	server := httptest.NewServer(engine)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/v1/models")
	if err != nil {
		t.Fatalf("http.Get() error = %v", err)
	}
	defer resp.Body.Close()

	var envelope taskTestResponse
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("Decode() envelope error = %v", err)
	}
	if !envelope.OK {
		t.Fatalf("response ok = false, raw data = %s", string(envelope.Data))
	}

	var payload map[string]any
	if err := json.Unmarshal(envelope.Data, &payload); err != nil {
		t.Fatalf("Unmarshal() payload error = %v", err)
	}
	providers, ok := payload["providers"].([]any)
	if !ok || len(providers) != 1 {
		t.Fatalf("providers = %#v, want one provider", payload["providers"])
	}
	provider, ok := providers[0].(map[string]any)
	if !ok {
		t.Fatalf("provider = %#v, want object", providers[0])
	}
	models, ok := provider["models"].([]any)
	if !ok || len(models) != 1 {
		t.Fatalf("models = %#v, want one model", provider["models"])
	}
	modelEntry, ok := models[0].(map[string]any)
	if !ok {
		t.Fatalf("model = %#v, want object", models[0])
	}
	if _, exists := modelEntry["context_window"]; exists {
		t.Fatalf("model catalog entry = %#v, want no context_window overreach in Task 2", modelEntry)
	}
	context, ok := modelEntry["context"].(map[string]any)
	if !ok {
		t.Fatalf("model context = %#v, want object", modelEntry["context"])
	}
	if got := int64(context["max"].(float64)); got != 128000 {
		t.Fatalf("context.max = %d, want 128000", got)
	}
	if got := int64(context["input"].(float64)); got != 96000 {
		t.Fatalf("context.input = %d, want 96000", got)
	}
	if got := int64(context["output"].(float64)); got != 4000 {
		t.Fatalf("context.output = %d, want 4000", got)
	}
	cost, ok := modelEntry["cost"].(map[string]any)
	if !ok {
		t.Fatalf("model cost = %#v, want object", modelEntry["cost"])
	}
	input, ok := cost["input"].(map[string]any)
	if !ok {
		t.Fatalf("cost.input = %#v, want object", cost["input"])
	}
	if got := input["amount_usd"].(float64); got != inputCost {
		t.Fatalf("cost.input.amount_usd = %v, want %v", got, inputCost)
	}
}
