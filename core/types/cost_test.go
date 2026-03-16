package types_test

import (
	"testing"

	coretypes "github.com/EquentR/agent_runtime/core/types"
)

func TestTokenPriceCostForTokens(t *testing.T) {
	price := coretypes.TokenPrice{AmountUSD: 1.25, PerTokens: 1_000_000}
	if got := price.CostForTokens(1_500_000); got != 1.875 {
		t.Fatalf("CostForTokens() = %v, want 1.875", got)
	}
}

func TestCostBreakdownAdd(t *testing.T) {
	left := coretypes.CostBreakdown{UncachedPromptTokens: 10, InputCostUSD: 1.5, TotalCostUSD: 1.5}
	right := coretypes.CostBreakdown{CachedPromptTokens: 5, OutputCostUSD: 2.5, TotalCostUSD: 2.5}
	got := left.Add(right)
	if got.UncachedPromptTokens != 10 || got.CachedPromptTokens != 5 {
		t.Fatalf("Add() token counts = %#v, want merged counts", got)
	}
	if got.TotalCostUSD != 4.0 {
		t.Fatalf("Add() TotalCostUSD = %v, want 4.0", got.TotalCostUSD)
	}
}
