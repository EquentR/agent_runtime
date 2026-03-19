package config

import (
	coretypes "github.com/EquentR/agent_runtime/core/types"
	"github.com/EquentR/agent_runtime/pkg/db"
	"github.com/EquentR/agent_runtime/pkg/log"
	"github.com/EquentR/agent_runtime/pkg/rest"
)

type Config struct {
	WorkspaceDir string                      `yaml:"workspaceDir"`
	Server       rest.Config                 `yaml:"server"`
	Sqlite       db.Database                 `yaml:"sqlite"`
	Log          log.Config                  `yaml:"log"`
	LLM          []coretypes.LLMProvider     `yaml:"llmProviders"`
	Embedding    coretypes.EmbeddingProvider `yaml:"embeddingProvider"`
	Rerank       coretypes.RerankingProvider `yaml:"rerankProvider"`
}
