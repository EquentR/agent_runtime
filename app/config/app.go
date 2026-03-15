package config

import (
	"github.com/EquentR/agent_runtime/pkg/db"
	"github.com/EquentR/agent_runtime/pkg/log"
	"github.com/EquentR/agent_runtime/pkg/rest"
)

type Config struct {
	Server rest.Config `yaml:"server"`
	Sqlite db.Database `yaml:"sqlite"`
	Log    log.Config  `yaml:"log"`
	//LLM       LLMProvider       `yaml:"llmProvider"`
	//Embedding EmbeddingProvider `yaml:"embeddingProvider"`
	//Rerank    RerankingProvider `yaml:"rerankProvider"`
}
