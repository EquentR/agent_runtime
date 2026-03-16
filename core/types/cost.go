package types

// TokenPrice 描述某一类 token 在固定计费单位下对应的价格。
type TokenPrice struct {
	AmountUSD float64 `json:"amount_usd"`
	PerTokens int64   `json:"per_tokens"`
}

// ModelPricing 汇总模型的输入/输出价格；缓存输入价格保持可选，是因为有些 provider
// 不单独返回它，或者直接按普通输入价格计费。
type ModelPricing struct {
	Input       TokenPrice  `json:"input"`
	CachedInput *TokenPrice `json:"cached_input,omitempty"`
	Output      TokenPrice  `json:"output"`
}

// CostBreakdown 记录一次或多次请求在拆分缓存/未缓存 prompt token 之后的数量明细
// 与对应美元费用。
type CostBreakdown struct {
	UncachedPromptTokens int64   `json:"uncached_prompt_tokens"`
	CachedPromptTokens   int64   `json:"cached_prompt_tokens"`
	CompletionTokens     int64   `json:"completion_tokens"`
	InputCostUSD         float64 `json:"input_cost_usd"`
	CachedInputCostUSD   float64 `json:"cached_input_cost_usd"`
	OutputCostUSD        float64 `json:"output_cost_usd"`
	TotalCostUSD         float64 `json:"total_cost_usd"`
}

func (p TokenPrice) CostForTokens(tokens int64) float64 {
	if tokens <= 0 || p.PerTokens <= 0 {
		return 0
	}
	return float64(tokens) * p.AmountUSD / float64(p.PerTokens)
}

func (b CostBreakdown) Add(other CostBreakdown) CostBreakdown {
	return CostBreakdown{
		UncachedPromptTokens: b.UncachedPromptTokens + other.UncachedPromptTokens,
		CachedPromptTokens:   b.CachedPromptTokens + other.CachedPromptTokens,
		CompletionTokens:     b.CompletionTokens + other.CompletionTokens,
		InputCostUSD:         b.InputCostUSD + other.InputCostUSD,
		CachedInputCostUSD:   b.CachedInputCostUSD + other.CachedInputCostUSD,
		OutputCostUSD:        b.OutputCostUSD + other.OutputCostUSD,
		TotalCostUSD:         b.TotalCostUSD + other.TotalCostUSD,
	}
}
