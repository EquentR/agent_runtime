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
