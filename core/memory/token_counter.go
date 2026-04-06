package memory

import (
	"strings"

	providertools "github.com/EquentR/agent_runtime/core/providers/tools"
	coretypes "github.com/EquentR/agent_runtime/core/types"
)

func NewTokenCounterForModel(llmModel *coretypes.LLMModel) TokenCounter {
	if llmModel != nil {
		modelName := strings.TrimSpace(llmModel.ModelName())
		if modelName == "" {
			modelName = strings.TrimSpace(llmModel.ModelID())
		}
		if modelName != "" {
			counter, err := providertools.NewTokenCounter(providertools.CountModeTokenizer, modelName)
			if err == nil {
				return counter
			}
		}
	}

	counter, err := providertools.NewCl100kTokenCounter()
	if err == nil {
		return counter
	}

	counter, err = providertools.NewTokenCounter(providertools.CountModeRune, "")
	if err == nil {
		return counter
	}
	return nil
}
