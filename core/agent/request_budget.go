package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	coreaudit "github.com/EquentR/agent_runtime/core/audit"
	"github.com/EquentR/agent_runtime/core/memory"
	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/runtimeprompt"
)

var ErrContextBudgetExceeded = errors.New("request context exceeds model budget")

type requestBudgetDecision struct {
	CompressionAttempted        bool   `json:"compression_attempted"`
	CompressionSucceeded        bool   `json:"compression_succeeded"`
	HasSummaryBefore            bool   `json:"has_summary_before"`
	ShortTermTokensBefore       int64  `json:"short_term_tokens_before"`
	SummaryTokensBefore         int64  `json:"summary_tokens_before"`
	RenderedSummaryTokensBefore int64  `json:"rendered_summary_tokens_before"`
	TotalTokensBefore           int64  `json:"total_tokens_before"`
	TrimApplied                 bool   `json:"trim_applied"`
	FinalPath                   string `json:"final_path"`
	TokenCount                  int64  `json:"token_count"`
	MessageCount                int    `json:"message_count"`
}

func (r *Runner) buildBudgetedRequest(ctx context.Context, extraBody []model.Message, afterToolTurn bool) (runtimeprompt.BuildResult, []model.Message, requestBudgetDecision, error) {
	runtimeContext := memory.RuntimeContext{}
	if r.options.Memory == nil {
		runtimeContext.Tail = append(runtimeContext.Tail, cloneMessages(extraBody)...)
		return r.buildBudgetedRequestFromContext(ctx, runtimeContext, afterToolTurn)
	}

	beforeState := r.options.Memory.ContextState()
	state, trace, err := r.options.Memory.RuntimeContextWithReserve(ctx, 0)
	if err != nil {
		decision := requestBudgetDecision{}
		decision.applyBeforeState(beforeState)
		decision.applyCompressionTrace(trace)
		if trace.Succeeded {
			r.emitMemoryCompressed(ctx, trace)
			r.emitMemoryContextStateFromManager(ctx)
		}
		return runtimeprompt.BuildResult{}, nil, decision, err
	}
	compressionAttempted := trace.Attempted
	compressionSucceeded := trace.Succeeded
	state.Tail = append(state.Tail, cloneMessages(extraBody)...)
	buildResult, requestMessages, decision, err := r.buildBudgetedRequestFromContext(ctx, state, afterToolTurn)
	decision.applyBeforeState(beforeState)
	decision.applyCompressionTrace(trace)
	if err == nil {
		if compressionSucceeded && decision.FinalPath == "direct" {
			decision.FinalPath = "compressed"
		}
		if trace.Succeeded {
			r.emitMemoryCompressed(ctx, trace)
		}
		r.emitMemoryContextStateFromManager(ctx)
		return buildResult, requestMessages, decision, nil
	}
	if !errors.Is(err, ErrContextBudgetExceeded) {
		if trace.Succeeded {
			r.emitMemoryCompressed(ctx, trace)
			r.emitMemoryContextStateFromManager(ctx)
		}
		return buildResult, requestMessages, decision, err
	}
	if trace.Succeeded {
		r.emitMemoryCompressed(ctx, trace)
		r.emitMemoryContextStateFromManager(ctx)
	}

	reserve := requestMessageOverhead(r.requestTokenCounter(), state, requestMessages)
	state, trace, err = r.options.Memory.RuntimeContextWithReserve(ctx, reserve)
	compressionAttempted = compressionAttempted || trace.Attempted
	compressionSucceeded = compressionSucceeded || trace.Succeeded
	if err != nil {
		decision := requestBudgetDecision{}
		decision.applyBeforeState(beforeState)
		decision.CompressionAttempted = compressionAttempted
		decision.CompressionSucceeded = compressionSucceeded
		if trace.Succeeded {
			r.emitMemoryCompressed(ctx, trace)
			r.emitMemoryContextStateFromManager(ctx)
		}
		return runtimeprompt.BuildResult{}, nil, decision, err
	}
	if trace.Succeeded {
		r.emitMemoryCompressed(ctx, trace)
		r.emitMemoryContextStateFromManager(ctx)
	}
	state.Tail = append(state.Tail, cloneMessages(extraBody)...)
	buildResult, requestMessages, decision, err = r.buildBudgetedRequestFromContext(ctx, state, afterToolTurn)
	decision.applyBeforeState(beforeState)
	decision.CompressionAttempted = compressionAttempted
	decision.CompressionSucceeded = compressionSucceeded
	if err == nil && compressionSucceeded && decision.FinalPath == "direct" {
		decision.FinalPath = "compressed"
	}
	if err == nil && !trace.Succeeded {
		r.emitMemoryContextStateFromManager(ctx)
	}
	return buildResult, requestMessages, decision, err
}

func (r *Runner) buildBudgetedRequestFromContext(_ context.Context, runtimeContext memory.RuntimeContext, afterToolTurn bool) (runtimeprompt.BuildResult, []model.Message, requestBudgetDecision, error) {
	buildResult, requestMessages, err := r.buildRequestMessages(runtimeContext, afterToolTurn)
	if err != nil {
		return runtimeprompt.BuildResult{}, nil, requestBudgetDecision{}, err
	}
	counter := r.requestTokenCounter()
	tokenCount := memory.CountMessageTokens(counter, requestMessages)
	decision := requestBudgetDecision{
		FinalPath:    "direct",
		TokenCount:   tokenCount,
		MessageCount: len(requestMessages),
	}
	maxTokens := r.requestMaxContextTokens()
	if maxTokens <= 0 || tokenCount <= maxTokens {
		return buildResult, requestMessages, decision, nil
	}

	trimmed, changed := trimMessagesForBudget(requestMessages, maxTokens, counter)
	decision.TrimApplied = changed
	if changed {
		decision.FinalPath = "trimmed"
		decision.TokenCount = memory.CountMessageTokens(counter, trimmed)
		decision.MessageCount = len(trimmed)
		return buildResult, trimmed, decision, nil
	}

	decision.FinalPath = "blocked"
	return buildResult, requestMessages, decision, fmt.Errorf("%w: %d > %d", ErrContextBudgetExceeded, decision.TokenCount, maxTokens)
}

func requestMessageOverhead(counter memory.TokenCounter, runtimeContext memory.RuntimeContext, requestMessages []model.Message) int64 {
	if counter == nil {
		return 0
	}
	overhead := memory.CountMessageTokens(counter, requestMessages) - memory.CountRuntimeContextTokens(counter, runtimeContext)
	if overhead < 0 {
		return 0
	}
	return overhead
}

func trimMessagesForBudget(messages []model.Message, maxTokens int64, counter memory.TokenCounter) ([]model.Message, bool) {
	trimmed := cloneMessages(messages)
	changed := false
	for i := range trimmed {
		if maxTokens > 0 && memory.CountMessageTokens(counter, trimmed) <= maxTokens {
			break
		}
		if trimmed[i].Role != model.RoleTool {
			continue
		}
		replacement := "[trimmed tool output to fit request budget]"
		if strings.TrimSpace(trimmed[i].Content) == replacement {
			continue
		}
		trimmed[i].Content = replacement
		changed = true
	}
	return trimmed, changed
}

func (r *Runner) requestTokenCounter() memory.TokenCounter {
	if r.options.Memory != nil && r.options.Memory.TokenCounter() != nil {
		return r.options.Memory.TokenCounter()
	}
	return memory.NewTokenCounterForModel(r.options.LLMModel)
}

func (r *Runner) requestMaxContextTokens() int64 {
	if r.options.LLMModel != nil {
		return r.options.LLMModel.ContextWindow().Max
	}
	return 0
}

func (r *Runner) recordRequestBudgetDecision(ctx context.Context, step int, decision requestBudgetDecision) {
	artifactID := r.attachAuditArtifact(ctx, coreaudit.ArtifactKindRequestBudgetDecision, decision)
	r.appendAuditEvent(ctx, step, coreaudit.PhaseRequest, "request.budgeted", map[string]any{
		"compression_attempted":          decision.CompressionAttempted,
		"compression_succeeded":          decision.CompressionSucceeded,
		"has_summary_before":             decision.HasSummaryBefore,
		"short_term_tokens_before":       decision.ShortTermTokensBefore,
		"summary_tokens_before":          decision.SummaryTokensBefore,
		"rendered_summary_tokens_before": decision.RenderedSummaryTokensBefore,
		"total_tokens_before":            decision.TotalTokensBefore,
		"trim_applied":                   decision.TrimApplied,
		"final_path":                     decision.FinalPath,
		"token_count":                    decision.TokenCount,
		"message_count":                  decision.MessageCount,
	}, artifactID)
}

func (d *requestBudgetDecision) applyBeforeState(state memory.ContextState) {
	if d == nil {
		return
	}
	d.HasSummaryBefore = state.HasSummary
	d.ShortTermTokensBefore = state.ShortTermTokens
	d.SummaryTokensBefore = state.SummaryTokens
	d.RenderedSummaryTokensBefore = state.RenderedSummaryTokens
	d.TotalTokensBefore = state.TotalTokens
}

func (d *requestBudgetDecision) applyCompressionTrace(trace memory.CompressionTrace) {
	if d == nil {
		return
	}
	d.CompressionAttempted = trace.Attempted
	d.CompressionSucceeded = trace.Succeeded
}
