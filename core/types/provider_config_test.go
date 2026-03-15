package types

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLLMProviderPricingUsesPerMillionTokens(t *testing.T) {
	provider := mustLoadLLMProvider(t, `
llmProvider:
  name: openai
  models:
    - id: gpt54
      name: gpt-5.4
      type: openai_responses
      cost:
        input: 1.25
        cachedInput: 0.125
        output: 10
`)

	model := provider.FindModel("gpt54")
	if model == nil {
		t.Fatalf("FindModel() = nil, want gpt54 model")
	}
	pricing := model.Pricing()
	if pricing == nil {
		t.Fatalf("pricing = nil, want configured pricing")
	}
	if pricing.Input.AmountUSD != 1.25 || pricing.Input.PerTokens != 1_000_000 {
		t.Fatalf("input pricing = %#v, want amount=1.25 perTokens=1_000_000", pricing.Input)
	}
	if pricing.CachedInput == nil {
		t.Fatalf("cached input pricing = nil, want non-nil")
	}
	if pricing.CachedInput.AmountUSD != 0.125 || pricing.CachedInput.PerTokens != 1_000_000 {
		t.Fatalf("cached pricing = %#v, want amount=0.125 perTokens=1_000_000", pricing.CachedInput)
	}
	if pricing.Output.AmountUSD != 10 || pricing.Output.PerTokens != 1_000_000 {
		t.Fatalf("output pricing = %#v, want amount=10 perTokens=1_000_000", pricing.Output)
	}
}

func TestLLMProviderPricingDistinguishesUnsetAndZeroCachedInput(t *testing.T) {
	withoutCached := mustLoadLLMProvider(t, `
llmProvider:
  models:
    - id: flash
      name: gemini-2.5-flash
      type: google
      cost:
        input: 1
        output: 2
`)
	pricingWithoutCached := withoutCached.FindModel("flash").Pricing()
	if pricingWithoutCached == nil {
		t.Fatalf("pricingWithoutCached = nil, want configured pricing")
	}
	if pricing := pricingWithoutCached; pricing.CachedInput != nil {
		t.Fatalf("pricing.CachedInput = %#v, want nil when cachedInput is omitted", pricing.CachedInput)
	}

	zeroCached := mustLoadLLMProvider(t, `
llmProvider:
  models:
    - id: flash
      name: gemini-2.5-flash
      type: google
      cost:
        input: 1
        cachedInput: 0
        output: 2
`)
	pricing := zeroCached.FindModel("flash").Pricing()
	if pricing == nil {
		t.Fatalf("pricing = nil, want configured pricing")
	}
	if pricing.CachedInput == nil {
		t.Fatalf("pricing.CachedInput = nil, want explicit zero price")
	}
	if pricing.CachedInput.AmountUSD != 0 || pricing.CachedInput.PerTokens != 1_000_000 {
		t.Fatalf("pricing.CachedInput = %#v, want zero-priced 1M unit", pricing.CachedInput)
	}
}

func TestLLMProviderPricingReturnsNilWhenRequiredPricesMissing(t *testing.T) {
	provider := mustLoadLLMProvider(t, `
llmProvider:
  models:
    - id: missing
      name: gpt-missing
      type: openai_completions
      cost:
        cachedInput: 0.1
`)

	if pricing := provider.FindModel("missing").Pricing(); pricing != nil {
		t.Fatalf("pricing = %#v, want nil when input/output prices are omitted", pricing)
	}
}

func TestLLMProviderContextWindowNormalizesDerivedLimits(t *testing.T) {
	provider := mustLoadLLMProvider(t, `
llmProvider:
  models:
    - id: gpt54
      name: gpt-5.4
      type: openai_responses
      context:
        max: 128000
        output: 8000
`)

	ctx := provider.FindModel("gpt54").ContextWindow()
	if ctx.Max != 128000 {
		t.Fatalf("ctx.Max = %d, want 128000", ctx.Max)
	}
	if ctx.Input != 120000 {
		t.Fatalf("ctx.Input = %d, want 120000", ctx.Input)
	}
	if ctx.Output != 8000 {
		t.Fatalf("ctx.Output = %d, want 8000", ctx.Output)
	}
}

func TestBaseProviderSupportsAPIKeyAliasAndProviderName(t *testing.T) {
	provider := mustLoadLLMProvider(t, `
llmProvider:
  name: openai
  baseUrl: https://example.com/v1
  apiKey: test-key
  models:
    - id: gpt54
      name: gpt-5.4
      type: openai_responses
`)

	if provider.ProviderName() != "openai" {
		t.Fatalf("provider.ProviderName() = %q, want %q", provider.ProviderName(), "openai")
	}
	if provider.AuthKey() != "test-key" {
		t.Fatalf("provider.AuthKey() = %q, want %q", provider.AuthKey(), "test-key")
	}
}

func TestLLMProviderFindModelMatchesByIDOrName(t *testing.T) {
	provider := mustLoadLLMProvider(t, `
llmProvider:
  name: openai
  models:
    - id: gpt54
      name: gpt-5.4
      type: openai_responses
    - id: mini
      name: gpt-5-mini
      type: openai_completions
`)

	if got := provider.FindModel("gpt54"); got == nil || got.Name != "gpt-5.4" {
		t.Fatalf("FindModel(id) = %#v, want gpt-5.4", got)
	}
	if got := provider.FindModel("gpt-5-mini"); got == nil || got.ID != "mini" {
		t.Fatalf("FindModel(name) = %#v, want id mini", got)
	}
}

func TestLLMProviderExposesProviderNameAndPackageAlignedModelType(t *testing.T) {
	provider := mustLoadLLMProvider(t, `
llmProvider:
  name: openai-compatible
  models:
    - id: deepseek-chat
      name: deepseek-chat
      type: openai_completions
`)

	if provider.ProviderName() != "openai-compatible" {
		t.Fatalf("provider.ProviderName() = %q, want %q", provider.ProviderName(), "openai-compatible")
	}
	model := provider.FindModel("deepseek-chat")
	if model == nil {
		t.Fatalf("FindModel() = nil, want deepseek-chat")
	}
	if model.ModelType() != "openai_completions" {
		t.Fatalf("model.ModelType() = %q, want %q", model.ModelType(), "openai_completions")
	}
}

func TestLLMProviderRejectsUnknownModelTypeAtYAMLLoad(t *testing.T) {
	var cfg struct {
		LLM LLMProvider `yaml:"llmProvider"`
	}
	err := yaml.Unmarshal([]byte(`
llmProvider:
  name: custom
  models:
    - id: unsupported
      name: unsupported
      type: responses
`), &cfg)
	if err == nil {
		t.Fatalf("yaml.Unmarshal() error = nil, want invalid model type error")
	}
}

func TestEmbeddingProviderSupportsMultipleModels(t *testing.T) {
	provider := mustLoadEmbeddingProvider(t, `
embeddingProvider:
  name: openai
  baseUrl: https://example.com/v1
  apiKey: embed-key
  models:
    - id: embed-small
      name: text-embedding-3-small
      dimension: 1536
    - id: embed-large
      name: text-embedding-3-large
      dimension: 3072
`)

	if provider.ProviderName() != "openai" {
		t.Fatalf("provider.ProviderName() = %q, want openai", provider.ProviderName())
	}
	if provider.AuthKey() != "embed-key" {
		t.Fatalf("provider.AuthKey() = %q, want embed-key", provider.AuthKey())
	}
	model := provider.FindModel("text-embedding-3-large")
	if model == nil {
		t.Fatalf("FindModel() = nil, want text-embedding-3-large")
	}
	if model.Dimension != 3072 {
		t.Fatalf("model.Dimension = %d, want 3072", model.Dimension)
	}
}

func TestRerankingProviderSupportsMultipleModels(t *testing.T) {
	provider := mustLoadRerankingProvider(t, `
rerankProvider:
  name: cohere
  baseUrl: https://example.com/rerank
  apiKey: rerank-key
  models:
    - id: rerank-v3
      name: rerank-v3.5
    - id: rerank-multilingual
      name: rerank-multilingual-v3.0
`)

	if provider.ProviderName() != "cohere" {
		t.Fatalf("provider.ProviderName() = %q, want cohere", provider.ProviderName())
	}
	if provider.AuthKey() != "rerank-key" {
		t.Fatalf("provider.AuthKey() = %q, want rerank-key", provider.AuthKey())
	}
	model := provider.FindModel("rerank-v3")
	if model == nil || model.Name != "rerank-v3.5" {
		t.Fatalf("FindModel() = %#v, want rerank-v3.5", model)
	}
}

func mustLoadLLMProvider(t *testing.T, raw string) LLMProvider {
	t.Helper()

	var cfg struct {
		LLM LLMProvider `yaml:"llmProvider"`
	}
	if err := yaml.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}
	return cfg.LLM
}

func mustLoadEmbeddingProvider(t *testing.T, raw string) EmbeddingProvider {
	t.Helper()

	var cfg struct {
		Embedding EmbeddingProvider `yaml:"embeddingProvider"`
	}
	if err := yaml.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}
	return cfg.Embedding
}

func mustLoadRerankingProvider(t *testing.T, raw string) RerankingProvider {
	t.Helper()

	var cfg struct {
		Rerank RerankingProvider `yaml:"rerankProvider"`
	}
	if err := yaml.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}
	return cfg.Rerank
}
