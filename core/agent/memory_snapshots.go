package agent

import "github.com/EquentR/agent_runtime/core/memory"

// MemoryContextSnapshot is the typed public/persisted view of the memory manager state.
type MemoryContextSnapshot struct {
	ShortTermTokens       int64 `json:"short_term_tokens"`
	SummaryTokens         int64 `json:"summary_tokens"`
	RenderedSummaryTokens int64 `json:"rendered_summary_tokens"`
	TotalTokens           int64 `json:"total_tokens"`
	ShortTermLimit        int64 `json:"short_term_limit"`
	SummaryLimit          int64 `json:"summary_limit"`
	MaxContextTokens      int64 `json:"max_context_tokens"`
	HasSummary            bool  `json:"has_summary"`
}

// MemoryCompressionSnapshot is the typed public/persisted view of the latest compression trace.
type MemoryCompressionSnapshot struct {
	TokensBefore                int64 `json:"tokens_before"`
	TokensAfter                 int64 `json:"tokens_after"`
	ShortTermTokensBefore       int64 `json:"short_term_tokens_before"`
	ShortTermTokensAfter        int64 `json:"short_term_tokens_after"`
	SummaryTokensBefore         int64 `json:"summary_tokens_before"`
	SummaryTokensAfter          int64 `json:"summary_tokens_after"`
	RenderedSummaryTokensBefore int64 `json:"rendered_summary_tokens_before"`
	RenderedSummaryTokensAfter  int64 `json:"rendered_summary_tokens_after"`
	TotalTokensBefore           int64 `json:"total_tokens_before"`
	TotalTokensAfter            int64 `json:"total_tokens_after"`
}

func newMemoryContextSnapshot(state memory.ContextState) *MemoryContextSnapshot {
	return &MemoryContextSnapshot{
		ShortTermTokens:       state.ShortTermTokens,
		SummaryTokens:         state.SummaryTokens,
		RenderedSummaryTokens: state.RenderedSummaryTokens,
		TotalTokens:           state.TotalTokens,
		ShortTermLimit:        state.ShortTermLimit,
		SummaryLimit:          state.SummaryLimit,
		MaxContextTokens:      state.MaxContextTokens,
		HasSummary:            state.HasSummary,
	}
}

func newMemoryCompressionSnapshot(trace memory.CompressionTrace) *MemoryCompressionSnapshot {
	return &MemoryCompressionSnapshot{
		TokensBefore:                trace.TokensBefore,
		TokensAfter:                 trace.TokensAfter,
		ShortTermTokensBefore:       trace.ShortTermTokensBefore,
		ShortTermTokensAfter:        trace.ShortTermTokensAfter,
		SummaryTokensBefore:         trace.SummaryTokensBefore,
		SummaryTokensAfter:          trace.SummaryTokensAfter,
		RenderedSummaryTokensBefore: trace.RenderedSummaryBefore,
		RenderedSummaryTokensAfter:  trace.RenderedSummaryAfter,
		TotalTokensBefore:           trace.TotalTokensBefore,
		TotalTokensAfter:            trace.TotalTokensAfter,
	}
}

func cloneMemoryContextSnapshot(snapshot *MemoryContextSnapshot) *MemoryContextSnapshot {
	if snapshot == nil {
		return nil
	}
	cloned := *snapshot
	return &cloned
}

func cloneMemoryCompressionSnapshot(snapshot *MemoryCompressionSnapshot) *MemoryCompressionSnapshot {
	if snapshot == nil {
		return nil
	}
	cloned := *snapshot
	return &cloned
}

func memoryContextPayload(snapshot *MemoryContextSnapshot) map[string]any {
	if snapshot == nil {
		return nil
	}
	return map[string]any{
		"short_term_tokens":       snapshot.ShortTermTokens,
		"summary_tokens":          snapshot.SummaryTokens,
		"rendered_summary_tokens": snapshot.RenderedSummaryTokens,
		"total_tokens":            snapshot.TotalTokens,
		"short_term_limit":        snapshot.ShortTermLimit,
		"summary_limit":           snapshot.SummaryLimit,
		"max_context_tokens":      snapshot.MaxContextTokens,
		"has_summary":             snapshot.HasSummary,
	}
}

func memoryCompressionPayload(snapshot *MemoryCompressionSnapshot) map[string]any {
	if snapshot == nil {
		return nil
	}
	return map[string]any{
		"tokens_before":                  snapshot.TokensBefore,
		"tokens_after":                   snapshot.TokensAfter,
		"short_term_tokens_before":       snapshot.ShortTermTokensBefore,
		"short_term_tokens_after":        snapshot.ShortTermTokensAfter,
		"summary_tokens_before":          snapshot.SummaryTokensBefore,
		"summary_tokens_after":           snapshot.SummaryTokensAfter,
		"rendered_summary_tokens_before": snapshot.RenderedSummaryTokensBefore,
		"rendered_summary_tokens_after":  snapshot.RenderedSummaryTokensAfter,
		"total_tokens_before":            snapshot.TotalTokensBefore,
		"total_tokens_after":             snapshot.TotalTokensAfter,
	}
}

func (r *Runner) rememberMemoryCompressionSnapshot(snapshot *MemoryCompressionSnapshot) {
	if r == nil {
		return
	}
	r.snapshotMu.Lock()
	defer r.snapshotMu.Unlock()
	r.lastMemoryCompression = cloneMemoryCompressionSnapshot(snapshot)
}

func (r *Runner) lastMemoryCompressionSnapshot() *MemoryCompressionSnapshot {
	if r == nil {
		return nil
	}
	r.snapshotMu.RLock()
	defer r.snapshotMu.RUnlock()
	return cloneMemoryCompressionSnapshot(r.lastMemoryCompression)
}
