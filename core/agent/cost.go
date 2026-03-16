package agent

import (
	model "github.com/EquentR/agent_runtime/core/providers/types"
	coretypes "github.com/EquentR/agent_runtime/core/types"
)

func addUsage(left model.TokenUsage, right model.TokenUsage) model.TokenUsage {
	return model.TokenUsage{
		PromptTokens:       left.PromptTokens + right.PromptTokens,
		CachedPromptTokens: left.CachedPromptTokens + right.CachedPromptTokens,
		CompletionTokens:   left.CompletionTokens + right.CompletionTokens,
		TotalTokens:        left.TotalTokens + right.TotalTokens,
	}
}

func pricingFromOptions(options Options) *coretypes.ModelPricing {
	if options.LLMModel == nil {
		return nil
	}
	return options.LLMModel.Pricing()
}

func breakdownFromUsage(pricing *coretypes.ModelPricing, usage model.TokenUsage) coretypes.CostBreakdown {
	if pricing == nil {
		return coretypes.CostBreakdown{}
	}
	uncached := usage.PromptTokens - usage.CachedPromptTokens
	if uncached < 0 {
		uncached = 0
	}
	cachePrice := pricing.Input
	if pricing.CachedInput != nil {
		cachePrice = *pricing.CachedInput
	}
	breakdown := coretypes.CostBreakdown{
		UncachedPromptTokens: uncached,
		CachedPromptTokens:   usage.CachedPromptTokens,
		CompletionTokens:     usage.CompletionTokens,
		InputCostUSD:         pricing.Input.CostForTokens(uncached),
		CachedInputCostUSD:   cachePrice.CostForTokens(usage.CachedPromptTokens),
		OutputCostUSD:        pricing.Output.CostForTokens(usage.CompletionTokens),
	}
	breakdown.TotalCostUSD = breakdown.InputCostUSD + breakdown.CachedInputCostUSD + breakdown.OutputCostUSD
	return breakdown
}
