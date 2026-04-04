package config

import (
	"time"

	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	builtin "github.com/EquentR/agent_runtime/core/tools/builtin"
	coretypes "github.com/EquentR/agent_runtime/core/types"
	"github.com/EquentR/agent_runtime/pkg/db"
	"github.com/EquentR/agent_runtime/pkg/log"
	"github.com/EquentR/agent_runtime/pkg/rest"
)

const defaultLLMRequestTimeout = 10 * time.Minute

type Config struct {
	WorkspaceDir      string                      `yaml:"workspaceDir"`
	Server            rest.Config                 `yaml:"server"`
	Sqlite            db.Database                 `yaml:"sqlite"`
	Log               log.Config                  `yaml:"log"`
	Tasks             TaskManagerConfig           `yaml:"tasks"`
	Tools             ToolsConfig                 `yaml:"tools"`
	LLMRequestTimeout time.Duration               `yaml:"llmRequestTimeout"`
	LLM               []coretypes.LLMProvider     `yaml:"llmProviders"`
	Embedding         coretypes.EmbeddingProvider `yaml:"embeddingProvider"`
	Rerank            coretypes.RerankingProvider `yaml:"rerankProvider"`
}

func (c Config) ResolvedLLMRequestTimeout() time.Duration {
	if c.LLMRequestTimeout > 0 {
		return c.LLMRequestTimeout
	}
	return defaultLLMRequestTimeout
}

type TaskManagerConfig struct {
	WorkerCount int    `yaml:"workerCount"`
	RunnerID    string `yaml:"runnerId"`
}

func (c TaskManagerConfig) ManagerOptions(auditRecorder coretasks.AuditRecorder) coretasks.ManagerOptions {
	return coretasks.ManagerOptions{
		RunnerID:      c.RunnerID,
		WorkerCount:   c.WorkerCount,
		AuditRecorder: auditRecorder,
	}
}

type ToolsConfig struct {
	WebSearch WebSearchConfig `yaml:"webSearch"`
}

type WebSearchConfig struct {
	DefaultProvider string             `yaml:"defaultProvider"`
	Tavily          *WebSearchProvider `yaml:"tavily"`
	SerpAPI         *WebSearchProvider `yaml:"serpApi"`
	Bing            *WebSearchProvider `yaml:"bing"`
}

func (c WebSearchConfig) BuiltinOptions() builtin.WebSearchOptions {
	return builtin.WebSearchOptions{
		DefaultProvider: c.DefaultProvider,
		Tavily:          toTavilyConfig(c.Tavily),
		SerpAPI:         toSerpAPIConfig(c.SerpAPI),
		Bing:            toBingConfig(c.Bing),
	}
}

type WebSearchProvider struct {
	APIKey  string `yaml:"apiKey"`
	BaseURL string `yaml:"baseUrl"`
}

func toTavilyConfig(provider *WebSearchProvider) *builtin.TavilyConfig {
	if provider == nil {
		return nil
	}
	return &builtin.TavilyConfig{APIKey: provider.APIKey, BaseURL: provider.BaseURL}
}

func toSerpAPIConfig(provider *WebSearchProvider) *builtin.SerpAPIConfig {
	if provider == nil {
		return nil
	}
	return &builtin.SerpAPIConfig{APIKey: provider.APIKey, BaseURL: provider.BaseURL}
}

func toBingConfig(provider *WebSearchProvider) *builtin.BingConfig {
	if provider == nil {
		return nil
	}
	return &builtin.BingConfig{APIKey: provider.APIKey, BaseURL: provider.BaseURL}
}
