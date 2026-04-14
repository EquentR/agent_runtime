package types

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

const tokensPerMillion int64 = 1_000_000

const (
	LLMTypeGoogle            = "google"
	LLMTypeOpenAICompletions = "openai_completions"
	LLMTypeOpenAIResponses   = "openai_responses"
)

type Provider interface {
	ProviderName() string
	BaseURL() string
	AuthKey() string
}

type BaseProvider struct {
	Name    string `yaml:"name"`
	BaseUrl string `yaml:"baseUrl"`
	APIKey  string `yaml:"apiKey"`
}

func (p BaseProvider) ProviderName() string {
	return strings.TrimSpace(p.Name)
}

func (p BaseProvider) BaseURL() string {
	return strings.TrimSpace(p.BaseUrl)
}

func (p BaseProvider) AuthKey() string {
	if apiKey := strings.TrimSpace(p.APIKey); apiKey != "" {
		return apiKey
	}
	return ""
}

type BaseModel struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name"`
}

type LLMProvider struct {
	BaseProvider `yaml:",inline"`
	Models       []LLMModel `yaml:"models"`
}

func (p *LLMProvider) UnmarshalYAML(value *yaml.Node) error {
	type rawLLMProvider LLMProvider
	var raw rawLLMProvider
	if err := value.Decode(&raw); err != nil {
		return err
	}
	for _, model := range raw.Models {
		if err := model.Validate(); err != nil {
			return err
		}
	}
	*p = LLMProvider(raw)
	return nil
}

type LLMModel struct {
	BaseModel    `yaml:",inline"`
	Type         string            `yaml:"type"`
	Cost         LLMCostConfig     `yaml:"cost"`
	Context      LLMContextConfig  `yaml:"context"`
	Capabilities ModelCapabilities `yaml:"capabilities" json:"capabilities"`
}

type ModelCapabilities struct {
	Attachments bool `yaml:"attachments" json:"attachments"`
}

type LLMCostConfig struct {
	Input       *float64 `yaml:"input,omitempty"`
	CachedInput *float64 `yaml:"cachedInput,omitempty"`
	Output      *float64 `yaml:"output,omitempty"`
}

type LLMContextConfig struct {
	Max    int64 `yaml:"max"    json:"max"`
	Input  int64 `yaml:"input"  json:"input"`
	Output int64 `yaml:"output" json:"output"`
}

func (m BaseModel) ModelID() string {
	return strings.TrimSpace(m.ID)
}

func (m BaseModel) ModelName() string {
	return strings.TrimSpace(m.Name)
}

func (m *LLMModel) ModelType() string {
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m.Type)
}

func (m LLMModel) Validate() error {
	switch m.ModelType() {
	case LLMTypeGoogle, LLMTypeOpenAICompletions, LLMTypeOpenAIResponses:
		return nil
	case "":
		return fmt.Errorf("llm model %q type is required", m.ModelName())
	default:
		return fmt.Errorf("llm model %q has unsupported type %q", m.ModelName(), m.ModelType())
	}
}

func (m *LLMModel) SupportsAttachments() bool {
	return m != nil && m.Capabilities.Attachments
}

func (p *LLMProvider) FindModel(query string) *LLMModel {
	return findLLMModel(p, query)
}

func findLLMModel(p *LLMProvider, query string) *LLMModel {
	if p == nil {
		return nil
	}
	target := strings.TrimSpace(query)
	if target == "" {
		return nil
	}
	for i := range p.Models {
		if strings.EqualFold(strings.TrimSpace(p.Models[i].ID), target) {
			return &p.Models[i]
		}
	}
	for i := range p.Models {
		if strings.EqualFold(strings.TrimSpace(p.Models[i].Name), target) {
			return &p.Models[i]
		}
	}
	return nil
}

func (m *LLMModel) Pricing() *ModelPricing {
	if m == nil || m.Cost.Input == nil || m.Cost.Output == nil {
		return nil
	}

	pricing := &ModelPricing{
		Input: TokenPrice{
			AmountUSD: *m.Cost.Input,
			PerTokens: tokensPerMillion,
		},
		Output: TokenPrice{
			AmountUSD: *m.Cost.Output,
			PerTokens: tokensPerMillion,
		},
	}
	if m.Cost.CachedInput != nil {
		pricing.CachedInput = &TokenPrice{
			AmountUSD: *m.Cost.CachedInput,
			PerTokens: tokensPerMillion,
		}
	}
	return pricing
}

func (m *LLMModel) ContextWindow() LLMContextConfig {
	if m == nil {
		return LLMContextConfig{}
	}
	return m.Context.Normalized()
}

func (c LLMContextConfig) Normalized() LLMContextConfig {
	normalized := c
	if normalized.Max <= 0 && normalized.Input > 0 && normalized.Output > 0 {
		normalized.Max = normalized.Input + normalized.Output
	}
	if normalized.Max <= 0 {
		return normalized
	}
	if normalized.Input <= 0 && normalized.Output >= 0 && normalized.Max >= normalized.Output {
		normalized.Input = normalized.Max - normalized.Output
	}
	if normalized.Output <= 0 && normalized.Input >= 0 && normalized.Max >= normalized.Input {
		normalized.Output = normalized.Max - normalized.Input
	}
	return normalized
}

type EmbeddingProvider struct {
	BaseProvider `yaml:",inline"`
	Models       []EmbeddingModel `yaml:"models"`
}

type EmbeddingModel struct {
	BaseModel `yaml:",inline"`
	Dimension int              `yaml:"dimension"`
	Context   LLMContextConfig `yaml:"context"`
	Cost      LLMCostConfig    `yaml:"cost"`
}

func (p *EmbeddingProvider) FindModel(query string) *EmbeddingModel {
	if p == nil {
		return nil
	}
	target := strings.TrimSpace(query)
	if target == "" {
		return nil
	}
	for i := range p.Models {
		if strings.EqualFold(p.Models[i].ModelID(), target) {
			return &p.Models[i]
		}
	}
	for i := range p.Models {
		if strings.EqualFold(p.Models[i].ModelName(), target) {
			return &p.Models[i]
		}
	}
	return nil
}

type RerankingProvider struct {
	BaseProvider `yaml:",inline"`
	Models       []RerankingModel `yaml:"models"`
}

type RerankingModel struct {
	BaseModel `yaml:",inline"`
	Context   LLMContextConfig `yaml:"context"`
	Cost      LLMCostConfig    `yaml:"cost"`
}

func (p *RerankingProvider) FindModel(query string) *RerankingModel {
	if p == nil {
		return nil
	}
	target := strings.TrimSpace(query)
	if target == "" {
		return nil
	}
	for i := range p.Models {
		if strings.EqualFold(p.Models[i].ModelID(), target) {
			return &p.Models[i]
		}
	}
	for i := range p.Models {
		if strings.EqualFold(p.Models[i].ModelName(), target) {
			return &p.Models[i]
		}
	}
	return nil
}
