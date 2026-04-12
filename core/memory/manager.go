package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"unicode/utf8"

	corelog "github.com/EquentR/agent_runtime/core/log"
	providertools "github.com/EquentR/agent_runtime/core/providers/tools"
	model "github.com/EquentR/agent_runtime/core/providers/types"
	coretypes "github.com/EquentR/agent_runtime/core/types"
)

const (
	DefaultMaxContextTokens int64 = 100_000

	shortTermBudgetNumerator      int64 = 70
	contextBudgetDenominator      int64 = 100
	summaryTargetBudgetNumerator  int64 = 80
	defaultSummaryPromptTemplate        = "以下为当前会话的压缩记忆，仅在相关时参考：\n%s"
	defaultCompressionInstruction       = "你负责将当前会话的短期上下文压缩成一段供后续对话继续使用的工作记忆。请保留仍然有效的用户目标、约束、重要事实、已完成进展、未完成事项，以及后续推理需要依赖的关键信息；删除寒暄、逐字复述、冗长过程、临时噪声和不必要的细节。输出精炼中文摘要，直接给未来模型阅读，不要写解释，不要使用代码块。"
	defaultTargetSummaryTokens    int64 = 8_000
	defaultMaxSummaryTokens       int64 = 9_000
	truncationMarker                    = "..."
)

type TokenCounter interface {
	Count(text string) int
	CountMessages(messages []string) int
}

type CompressionRequest struct {
	PreviousSummary     string
	Messages            []model.Message
	Instruction         string
	TargetSummaryTokens int64
	MaxSummaryTokens    int64
}

type Compressor func(ctx context.Context, request CompressionRequest) (string, error)

type Options struct {
	Model                  *coretypes.LLMModel
	MaxContextTokens       int64
	Counter                TokenCounter
	Compressor             Compressor
	SummaryTemplate        string
	CompressionInstruction string
	TargetSummaryTokens    int64
	MaxSummaryTokens       int64
}

type Manager struct {
	mu                     sync.RWMutex
	summary                string
	shortTerm              []model.Message
	maxContextTokens       int64
	shortTermLimitTokens   int64
	summaryLimitTokens     int64
	targetSummaryTokens    int64
	maxSummaryTokens       int64
	counter                TokenCounter
	compressor             Compressor
	summaryTemplate        string
	compressionInstruction string
}

func NewManager(options Options) (*Manager, error) {
	maxContextTokens := resolveMaxContextTokens(options.MaxContextTokens, options.Model)
	shortTermLimitTokens, summaryLimitTokens := splitContextBudget(maxContextTokens)

	counter := options.Counter
	if counter == nil {
		counter = NewTokenCounterForModel(options.Model)
	}
	if counter == nil {
		return nil, fmt.Errorf("memory token counter is required")
	}

	compressor := options.Compressor
	if compressor == nil {
		compressor = newDefaultCompressor(counter)
	}

	summaryTemplate := strings.TrimSpace(options.SummaryTemplate)
	if summaryTemplate == "" {
		summaryTemplate = defaultSummaryPromptTemplate
	}

	compressionInstruction := strings.TrimSpace(options.CompressionInstruction)
	if compressionInstruction == "" {
		compressionInstruction = defaultCompressionInstruction
	}

	targetSummaryTokens, maxSummaryTokens := resolveSummaryCompressionBudgets(summaryLimitTokens, options.TargetSummaryTokens, options.MaxSummaryTokens)

	return &Manager{
		maxContextTokens:       maxContextTokens,
		shortTermLimitTokens:   shortTermLimitTokens,
		summaryLimitTokens:     summaryLimitTokens,
		targetSummaryTokens:    targetSummaryTokens,
		maxSummaryTokens:       maxSummaryTokens,
		counter:                counter,
		compressor:             compressor,
		summaryTemplate:        summaryTemplate,
		compressionInstruction: compressionInstruction,
	}, nil
}

func (m *Manager) AddMessage(message model.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.shortTerm = append(m.shortTerm, cloneMessage(message))
}

func (m *Manager) AddMessages(messages []model.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, message := range messages {
		m.shortTerm = append(m.shortTerm, cloneMessage(message))
	}
}

func (m *Manager) ShortTermMessages() []model.Message {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return cloneMessages(m.shortTerm)
}

func (m *Manager) Summary() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.summary
}

// LoadSummary restores a previously persisted compression summary into this manager.
// It should be called before the first AddMessage to seed cross-task continuity.
func (m *Manager) LoadSummary(summary string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.summary = strings.TrimSpace(summary)
}

func (m *Manager) MaxContextTokens() int64 {
	return m.maxContextTokens
}

func (m *Manager) ShortTermLimitTokens() int64 {
	return m.shortTermLimitTokens
}

func (m *Manager) SummaryLimitTokens() int64 {
	return m.summaryLimitTokens
}

func (m *Manager) TokenCounter() TokenCounter {
	return m.counter
}

func (m *Manager) ClearShortTerm() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.shortTerm = nil
}

// ContextState holds a snapshot of the memory manager's current budget and usage.
type ContextState struct {
	ShortTermTokens       int64
	SummaryTokens         int64
	RenderedSummaryTokens int64
	TotalTokens           int64
	ShortTermLimit        int64
	SummaryLimit          int64
	MaxContextTokens      int64
	HasSummary            bool
}

// ContextState returns a point-in-time snapshot of the memory manager's budget
// allocation and current token usage.
func (m *Manager) ContextState() ContextState {
	m.mu.RLock()
	defer m.mu.RUnlock()

	summaryTokens, renderedSummaryTokens, totalTokens := m.contextStateUsageLocked()

	return ContextState{
		ShortTermTokens:       m.estimateShortTermTokensLocked(),
		SummaryTokens:         summaryTokens,
		RenderedSummaryTokens: renderedSummaryTokens,
		TotalTokens:           totalTokens,
		ShortTermLimit:        m.shortTermLimitTokens,
		SummaryLimit:          m.summaryLimitTokens,
		MaxContextTokens:      m.maxContextTokens,
		HasSummary:            m.summary != "",
	}
}

type CompressionTrace struct {
	Attempted             bool
	Succeeded             bool
	TokensBefore          int64
	TokensAfter           int64
	ShortTermTokensBefore int64
	ShortTermTokensAfter  int64
	SummaryTokensBefore   int64
	SummaryTokensAfter    int64
	RenderedSummaryBefore int64
	RenderedSummaryAfter  int64
	TotalTokensBefore     int64
	TotalTokensAfter      int64
}

type RuntimeContext struct {
	Recap *model.Message
	Tail  []model.Message
}

func (m *Manager) RuntimeContext(ctx context.Context) (RuntimeContext, error) {
	state, _, err := m.RuntimeContextWithReserve(ctx, 0)
	return state, err
}

func (m *Manager) RuntimeContextWithReserve(ctx context.Context, reserveTokens int64) (RuntimeContext, CompressionTrace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	trace := CompressionTrace{}
	m.populateCompressionTraceLocked(&trace, true)
	if m.requiresCompressionLocked(reserveTokens) {
		trace.Attempted = true
		corelog.Info("memory compression triggered", corelog.String("component", "memory"), corelog.String("module", "manager"), corelog.Int("short_term_messages", len(m.shortTerm)), corelog.Int64("short_term_limit_tokens", m.shortTermLimitTokens), corelog.Int64("reserve_tokens", reserveTokens))
		if err := m.compressLocked(ctx); err != nil {
			corelog.Error("memory compression failed", corelog.String("component", "memory"), corelog.String("module", "manager"), corelog.Err(err))
			return RuntimeContext{}, trace, err
		}
		trace.Succeeded = true
	}
	if err := m.validateContextBudgetLocked(reserveTokens); err != nil {
		return RuntimeContext{}, trace, err
	}
	m.populateCompressionTraceLocked(&trace, false)
	return m.runtimeContextLocked(), trace, nil
}

func (m *Manager) ContextMessages(ctx context.Context) ([]model.Message, error) {
	state, _, err := m.RuntimeContextWithReserve(ctx, 0)
	if err != nil {
		return nil, err
	}

	out := make([]model.Message, 0, len(state.Tail)+1)
	if state.Recap != nil {
		out = append(out, cloneMessage(*state.Recap))
	}
	out = append(out, cloneMessages(state.Tail)...)
	return out, nil
}

func (m *Manager) requiresCompressionLocked(reserveTokens int64) bool {
	if len(m.shortTerm) == 0 {
		return false
	}
	if m.estimateShortTermTokensLocked()+reserveTokens <= m.shortTermLimitTokens {
		return false
	}
	compressible, preservedTail := splitMessagesForCompression(m.shortTerm)
	if len(compressible) > 0 {
		return true
	}
	// The initial split put everything into the preserved tail (the first
	// message is the only replayable boundary).  If the tail exceeds budget,
	// adaptSplitForBudget will strip ProviderState to create compressible
	// messages, so we should still trigger compression.
	return len(preservedTail) > 0 && validateShortTermBudget(m.counter, preservedTail, m.shortTermLimitTokens) != nil
}

func (m *Manager) estimateShortTermTokensLocked() int64 {
	if len(m.shortTerm) == 0 {
		return 0
	}
	payloads := make([]string, 0, len(m.shortTerm))
	for _, message := range m.shortTerm {
		payload := budgetPayloadForMessage(message)
		if payload == "" {
			continue
		}
		payloads = append(payloads, payload)
	}
	if len(payloads) == 0 {
		return 0
	}
	return int64(m.counter.CountMessages(payloads))
}

func (m *Manager) compressLocked(ctx context.Context) error {
	compressible, preservedTail := splitMessagesForCompression(m.shortTerm)
	if len(compressible) == 0 && len(preservedTail) == 0 {
		return nil
	}

	// When the preserved replayable tail exceeds the short-term budget, we
	// iteratively strip ProviderState from the oldest replayable boundary in
	// the tail, moving the split point forward.  This sacrifices provider-native
	// replay fidelity for older turns but guarantees that compression can always
	// produce a tail that fits within budget.
	if len(compressible) == 0 && len(preservedTail) > 0 {
		// All messages are in the tail (first message is the replayable boundary).
		// Only adapt if the tail actually exceeds budget.
		if validateShortTermBudget(m.counter, preservedTail, m.shortTermLimitTokens) == nil {
			return nil
		}
	}
	compressible, preservedTail = adaptSplitForBudget(m.counter, m.shortTerm, m.shortTermLimitTokens)
	if len(compressible) == 0 && len(preservedTail) == 0 {
		return nil
	}

	summary := strings.TrimSpace(m.summary)
	if len(compressible) > 0 {
		request := CompressionRequest{
			PreviousSummary:     m.summary,
			Messages:            cloneMessages(compressible),
			Instruction:         m.compressionInstruction,
			TargetSummaryTokens: m.targetSummaryTokens,
			MaxSummaryTokens:    m.maxSummaryTokens,
		}
		var err error
		summary, err = m.compressor(ctx, request)
		if err != nil {
			return err
		}
		summary = strings.TrimSpace(summary)
		if m.maxSummaryTokens > 0 && summary != "" && int64(m.counter.Count(summary)) > m.maxSummaryTokens {
			summary = limitTextByTokens(summary, m.maxSummaryTokens, m.counter)
		}
	}
	if err := validateShortTermBudget(m.counter, preservedTail, m.shortTermLimitTokens); err != nil {
		compressedLatestUser, compressErr := m.compressOversizedLatestUserLocked(ctx, preservedTail)
		if compressErr != nil {
			return err
		}
		preservedTail = compressedLatestUser
	}

	m.summary = summary
	m.shortTerm = cloneMessages(preservedTail)
	corelog.Info("memory compression finished", corelog.String("component", "memory"), corelog.String("module", "manager"), corelog.Int("preserved_tail_messages", len(preservedTail)), corelog.Int("summary_length", len(summary)))
	return nil
}

func (m *Manager) validateContextBudgetLocked(reserveTokens int64) error {
	if err := validateShortTermBudget(m.counter, m.shortTerm, m.shortTermLimitTokens); err != nil {
		return err
	}
	if reserveTokens > 0 && m.estimateShortTermTokensLocked()+reserveTokens > m.maxContextTokens {
		return fmt.Errorf("runtime context exceeds request budget: %d > %d", m.estimateShortTermTokensLocked()+reserveTokens, m.maxContextTokens)
	}
	if m.summary != "" && m.summaryLimitTokens > 0 && m.estimateRenderedSummaryTokensLocked() > m.summaryLimitTokens {
		return errors.New("memory summary exceeds summary budget after rendering")
	}
	return nil
}

func validateShortTermBudget(counter TokenCounter, messages []model.Message, budget int64) error {
	if budget <= 0 {
		return nil
	}
	payloads := make([]string, 0, len(messages))
	for _, message := range messages {
		payload := budgetPayloadForMessage(message)
		if payload == "" {
			continue
		}
		payloads = append(payloads, payload)
	}
	if len(payloads) == 0 {
		return nil
	}
	used := int64(counter.CountMessages(payloads))
	if used > budget {
		return fmt.Errorf("preserved replayable tail exceeds short-term budget: %d > %d", used, budget)
	}
	return nil
}

func (m *Manager) runtimeContextLocked() RuntimeContext {
	state := RuntimeContext{Tail: cloneMessages(m.shortTerm)}
	if m.summary != "" {
		recap := model.Message{
			Role:    model.RoleAssistant,
			Content: renderSummaryWithinBudget(m.summaryTemplate, m.summary, m.summaryLimitTokens, m.counter),
		}
		state.Recap = &recap
	}
	return state
}

func (m *Manager) contextStateUsageLocked() (summaryTokens int64, renderedSummaryTokens int64, totalTokens int64) {
	if m.summary != "" && m.counter != nil {
		summaryTokens = int64(m.counter.Count(m.summary))
		renderedSummaryTokens = m.estimateRenderedSummaryTokensLocked()
	}
	totalTokens = renderedSummaryTokens + m.estimateShortTermTokensLocked()
	return summaryTokens, renderedSummaryTokens, totalTokens
}

func (m *Manager) estimateRenderedSummaryTokensLocked() int64 {
	if m.summary == "" || m.counter == nil {
		return 0
	}
	rendered := renderSummaryWithinBudget(m.summaryTemplate, m.summary, m.summaryLimitTokens, m.counter)
	if rendered == "" {
		return 0
	}
	return int64(m.counter.Count(rendered))
}

func (m *Manager) populateCompressionTraceLocked(trace *CompressionTrace, before bool) {
	if trace == nil {
		return
	}
	summaryTokens, renderedSummaryTokens, totalTokens := m.contextStateUsageLocked()
	shortTermTokens := m.estimateShortTermTokensLocked()
	if before {
		trace.TokensBefore = shortTermTokens
		trace.ShortTermTokensBefore = shortTermTokens
		trace.SummaryTokensBefore = summaryTokens
		trace.RenderedSummaryBefore = renderedSummaryTokens
		trace.TotalTokensBefore = totalTokens
		return
	}
	trace.TokensAfter = shortTermTokens
	trace.ShortTermTokensAfter = shortTermTokens
	trace.SummaryTokensAfter = summaryTokens
	trace.RenderedSummaryAfter = renderedSummaryTokens
	trace.TotalTokensAfter = totalTokens
}

func (m *Manager) contextMessagesLocked() []model.Message {
	state := m.runtimeContextLocked()
	out := make([]model.Message, 0, len(state.Tail)+1)
	if state.Recap != nil {
		out = append(out, cloneMessage(*state.Recap))
	}
	out = append(out, cloneMessages(state.Tail)...)
	return out
}

func resolveMaxContextTokens(explicit int64, llmModel *coretypes.LLMModel) int64 {
	if explicit > 0 {
		return explicit
	}
	if llmModel != nil {
		contextWindow := llmModel.ContextWindow()
		if contextWindow.Input > 0 {
			return contextWindow.Input
		}
		if contextWindow.Max > 0 {
			return contextWindow.Max
		}
	}
	return DefaultMaxContextTokens
}

func splitContextBudget(maxContextTokens int64) (int64, int64) {
	if maxContextTokens <= 1 {
		return maxContextTokens, 0
	}
	shortTermLimitTokens := maxContextTokens * shortTermBudgetNumerator / contextBudgetDenominator
	if shortTermLimitTokens <= 0 {
		shortTermLimitTokens = 1
	}
	if shortTermLimitTokens >= maxContextTokens {
		shortTermLimitTokens = maxContextTokens - 1
	}
	summaryLimitTokens := maxContextTokens - shortTermLimitTokens
	return shortTermLimitTokens, summaryLimitTokens
}

func resolveSummaryCompressionBudgets(summaryLimitTokens, explicitTarget, explicitMax int64) (int64, int64) {
	if summaryLimitTokens <= 0 {
		return 0, 0
	}

	maxSummaryTokens := explicitMax
	if maxSummaryTokens <= 0 {
		maxSummaryTokens = summaryLimitTokens
		if maxSummaryTokens > defaultMaxSummaryTokens {
			maxSummaryTokens = defaultMaxSummaryTokens
		}
	}
	if maxSummaryTokens > summaryLimitTokens {
		maxSummaryTokens = summaryLimitTokens
	}
	if maxSummaryTokens <= 0 {
		maxSummaryTokens = summaryLimitTokens
	}

	targetSummaryTokens := explicitTarget
	if targetSummaryTokens <= 0 {
		targetSummaryTokens = summaryLimitTokens * summaryTargetBudgetNumerator / contextBudgetDenominator
		if targetSummaryTokens <= 0 {
			targetSummaryTokens = maxSummaryTokens
		}
		if targetSummaryTokens > defaultTargetSummaryTokens {
			targetSummaryTokens = defaultTargetSummaryTokens
		}
	}
	if targetSummaryTokens > maxSummaryTokens {
		targetSummaryTokens = maxSummaryTokens
	}
	if targetSummaryTokens <= 0 {
		targetSummaryTokens = maxSummaryTokens
	}

	return targetSummaryTokens, maxSummaryTokens
}

func newDefaultTokenCounter(llmModel *coretypes.LLMModel) TokenCounter {
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

func newDefaultCompressor(counter TokenCounter) Compressor {
	return func(_ context.Context, request CompressionRequest) (string, error) {
		parts := make([]string, 0, len(request.Messages)+1)
		if summary := compactWhitespace(request.PreviousSummary); summary != "" {
			parts = append(parts, "summary: "+summary)
		}
		for _, message := range request.Messages {
			if compact := summarizeMessage(message); compact != "" {
				parts = append(parts, compact)
			}
		}

		joined := strings.Join(parts, "\n")
		if request.TargetSummaryTokens > 0 {
			joined = limitTextByTokens(joined, request.TargetSummaryTokens, counter)
		}
		if request.MaxSummaryTokens > 0 {
			joined = limitTextByTokens(joined, request.MaxSummaryTokens, counter)
		}
		return joined, nil
	}
}

func renderSummary(template, summary string) string {
	if summary == "" {
		return ""
	}
	if strings.Contains(template, "%s") {
		return fmt.Sprintf(template, summary)
	}
	if template == "" {
		return summary
	}
	return template + "\n" + summary
}

func renderSummaryWithinBudget(template, summary string, budget int64, counter TokenCounter) string {
	rendered := renderSummary(template, summary)
	if budget <= 0 || counter == nil || rendered == "" {
		return rendered
	}
	if int64(counter.Count(rendered)) <= budget {
		return rendered
	}
	if int64(counter.Count(summary)) <= budget {
		return summary
	}
	return limitTextByTokens(summary, budget, counter)
}

func budgetPayloadForMessage(message model.Message) string {
	parts := make([]string, 0, 6)
	if message.Content != "" {
		parts = append(parts, message.Content)
	}
	if message.Reasoning != "" {
		parts = append(parts, message.Reasoning)
	}
	if reasoningSummary := joinReasoningSummaries(message.ReasoningItems); reasoningSummary != "" {
		parts = append(parts, reasoningSummary)
	}
	if toolCalls := joinToolCalls(message.ToolCalls); toolCalls != "" {
		parts = append(parts, toolCalls)
	}
	if attachments := attachmentBudgetPayload(message.Attachments); attachments != "" {
		parts = append(parts, attachments)
	}
	if providerState := providerStateBudgetPayload(message.ProviderState); providerState != "" {
		parts = append(parts, providerState)
	}
	if message.Role == model.RoleTool && message.ToolCallId != "" {
		parts = append(parts, message.ToolCallId)
	}
	if len(parts) == 0 {
		return ""
	}
	role := message.Role
	if role == "" {
		role = "message"
	}
	return role + ": " + strings.Join(parts, "\n")
}

func providerStateBudgetPayload(state *model.ProviderState) string {
	if state == nil || len(state.Payload) == 0 {
		return ""
	}
	parts := []string{"provider_state"}
	if provider := compactWhitespace(state.Provider); provider != "" {
		parts = append(parts, provider)
	}
	if format := compactWhitespace(state.Format); format != "" {
		parts = append(parts, format)
	}
	if version := compactWhitespace(state.Version); version != "" {
		parts = append(parts, version)
	}
	parts = append(parts, string(state.Payload))
	return strings.Join(parts, ":")
}

func splitMessagesForCompression(messages []model.Message) ([]model.Message, []model.Message) {
	if len(messages) == 0 {
		return nil, nil
	}
	if messages[len(messages)-1].Role == model.RoleUser {
		lastUserIndex := len(messages) - 1
		if lastUserIndex == 0 {
			return nil, cloneMessages(messages)
		}
		return cloneMessages(messages[:lastUserIndex]), cloneMessages(messages[lastUserIndex:])
	}
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == model.RoleAssistant && messages[i].ProviderState != nil {
			if i == 0 {
				return nil, cloneMessages(messages)
			}
			return cloneMessages(messages[:i]), cloneMessages(messages[i:])
		}
	}
	return cloneMessages(messages), nil
}

func (m *Manager) compressOversizedLatestUserLocked(ctx context.Context, preservedTail []model.Message) ([]model.Message, error) {
	if len(preservedTail) != 1 || preservedTail[0].Role != model.RoleUser {
		return nil, fmt.Errorf("preserved replayable tail exceeds short-term budget")
	}
	request := CompressionRequest{
		Messages:            []model.Message{cloneMessage(preservedTail[0])},
		Instruction:         m.compressionInstruction,
		TargetSummaryTokens: m.targetSummaryTokens,
		MaxSummaryTokens:    m.maxSummaryTokens,
	}
	replay, err := m.compressor(ctx, request)
	if err != nil {
		return nil, err
	}
	replay = strings.TrimSpace(replay)
	if replay == "" {
		return nil, fmt.Errorf("compressed latest user replay is empty")
	}
	message := model.Message{Role: model.RoleUser, Content: replay}
	if err := validateShortTermBudget(m.counter, []model.Message{message}, m.shortTermLimitTokens); err != nil {
		return nil, err
	}
	return []model.Message{message}, nil
}

// adaptSplitForBudget adjusts the compression split point so that the
// preserved replayable tail fits within the short-term token budget.
//
// It starts with the normal split (last assistant+ProviderState boundary) and,
// if the tail exceeds budget, iteratively strips ProviderState from the oldest
// replayable boundary in the working copy.  Removing ProviderState from a
// message makes it no longer a replay boundary, so the next call to
// splitMessagesForCompression will advance the split point forward (or
// eliminate the tail entirely if no boundaries remain).
//
// The trade-off is controlled loss of provider-native replay fidelity for
// older turns; the most recent replayable boundary is stripped last.
func adaptSplitForBudget(counter TokenCounter, messages []model.Message, budget int64) ([]model.Message, []model.Message) {
	compressible, preservedTail := splitMessagesForCompression(messages)
	if validateShortTermBudget(counter, preservedTail, budget) == nil {
		return compressible, preservedTail
	}

	// Work on a mutable copy so we can strip ProviderState without touching
	// the caller's slice.
	working := cloneMessages(messages)
	for {
		// Find the oldest assistant+ProviderState in the current tail region
		// and strip it.  We identify the tail start in the working copy by
		// re-splitting each iteration.
		_, tail := splitMessagesForCompression(working)
		if len(tail) == 0 {
			break
		}
		stripped := false
		for j := 0; j < len(tail); j++ {
			if tail[j].Role == model.RoleAssistant && tail[j].ProviderState != nil {
				// Map back to the working slice.  The tail is a clone, so we
				// need to find the corresponding index in working.
				tailOffset := len(working) - len(tail)
				working[tailOffset+j].ProviderState = nil
				stripped = true
				corelog.Info("adaptive split: stripped ProviderState from replayable boundary",
					corelog.String("component", "memory"),
					corelog.String("module", "manager"),
					corelog.Int("message_index", tailOffset+j))
				break
			}
		}
		if !stripped {
			break
		}

		compressible, preservedTail = splitMessagesForCompression(working)
		if validateShortTermBudget(counter, preservedTail, budget) == nil {
			return compressible, preservedTail
		}
	}
	return compressible, preservedTail
}

func summarizeMessage(message model.Message) string {
	parts := make([]string, 0, 5)
	if content := compactWhitespace(message.Content); content != "" {
		parts = append(parts, content)
	}
	if reasoning := compactWhitespace(message.Reasoning); reasoning != "" {
		parts = append(parts, "reasoning="+reasoning)
	}
	if reasoningSummary := compactWhitespace(joinReasoningSummaries(message.ReasoningItems)); reasoningSummary != "" {
		parts = append(parts, "reasoning_summary="+reasoningSummary)
	}
	if toolCalls := compactWhitespace(joinToolCalls(message.ToolCalls)); toolCalls != "" {
		parts = append(parts, "tool_calls="+toolCalls)
	}
	if attachments := compactWhitespace(attachmentBudgetPayload(message.Attachments)); attachments != "" {
		parts = append(parts, attachments)
	}
	if message.Role == model.RoleTool && message.ToolCallId != "" {
		parts = append(parts, "tool_call_id="+message.ToolCallId)
	}
	if len(parts) == 0 {
		return ""
	}
	role := compactWhitespace(message.Role)
	if role == "" {
		role = "message"
	}
	return role + ": " + strings.Join(parts, " | ")
}

func joinReasoningSummaries(items []model.ReasoningItem) string {
	if len(items) == 0 {
		return ""
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		summaryParts := make([]string, 0, len(item.Summary))
		for _, part := range item.Summary {
			if text := compactWhitespace(part.Text); text != "" {
				summaryParts = append(summaryParts, text)
			}
		}
		if len(summaryParts) == 0 {
			continue
		}
		if item.ID != "" {
			parts = append(parts, item.ID+":"+strings.Join(summaryParts, ","))
			continue
		}
		parts = append(parts, strings.Join(summaryParts, ","))
	}
	return strings.Join(parts, "; ")
}

func joinToolCalls(toolCalls []coretypes.ToolCall) string {
	if len(toolCalls) == 0 {
		return ""
	}
	parts := make([]string, 0, len(toolCalls))
	for _, toolCall := range toolCalls {
		label := compactWhitespace(toolCall.Name)
		if label == "" {
			label = compactWhitespace(toolCall.ID)
		}
		arguments := compactWhitespace(toolCall.Arguments)
		if label == "" && arguments == "" {
			continue
		}
		if arguments != "" {
			parts = append(parts, label+"("+arguments+")")
			continue
		}
		parts = append(parts, label)
	}
	return strings.Join(parts, ", ")
}

func attachmentBudgetPayload(attachments []model.Attachment) string {
	if len(attachments) == 0 {
		return ""
	}
	parts := make([]string, 0, len(attachments))
	for _, attachment := range attachments {
		payload := attachmentPromptText(attachment)
		if payload == "" {
			continue
		}
		parts = append(parts, payload)
	}
	return strings.Join(parts, "\n")
}

func attachmentPromptText(attachment model.Attachment) string {
	mimeType := strings.TrimSpace(attachment.MimeType)
	if mimeType == "" {
		mimeType = http.DetectContentType(attachment.Data)
	}
	if strings.HasPrefix(mimeType, "image/") {
		return "[image attachment]"
	}
	if isTextMimeType(mimeType) || utf8.Valid(attachment.Data) {
		fileName := strings.TrimSpace(attachment.FileName)
		if fileName == "" {
			fileName = "attachment.txt"
		}
		return "[附件:" + fileName + "]\n" + string(attachment.Data)
	}
	if fileName := strings.TrimSpace(attachment.FileName); fileName != "" {
		return "[attachment:" + fileName + "]"
	}
	return "[attachment]"
}

func isTextMimeType(mimeType string) bool {
	if strings.HasPrefix(mimeType, "text/") {
		return true
	}
	if mimeType == "application/json" || strings.HasSuffix(mimeType, "+json") {
		return true
	}
	if mimeType == "application/xml" || strings.HasSuffix(mimeType, "+xml") {
		return true
	}
	return false
}

func compactWhitespace(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func limitTextByTokens(text string, maxTokens int64, counter TokenCounter) string {
	text = strings.TrimSpace(text)
	if text == "" || maxTokens <= 0 || counter == nil {
		if maxTokens <= 0 {
			return ""
		}
		return text
	}
	if int64(counter.Count(text)) <= maxTokens {
		return text
	}

	runes := []rune(text)
	low, high := 0, len(runes)/2
	best := ""
	for low <= high {
		keep := (low + high) / 2
		candidate := truncationMarker
		if keep > 0 {
			candidate = string(runes[:keep]) + truncationMarker + string(runes[len(runes)-keep:])
		}
		if int64(counter.Count(candidate)) <= maxTokens {
			best = candidate
			low = keep + 1
			continue
		}
		high = keep - 1
	}
	if best != "" {
		return best
	}

	for keep := len(runes); keep >= 0; keep-- {
		candidate := string(runes[:keep])
		if int64(counter.Count(candidate)) <= maxTokens {
			return candidate
		}
	}
	return ""
}

func cloneMessages(messages []model.Message) []model.Message {
	if len(messages) == 0 {
		return nil
	}
	cloned := make([]model.Message, 0, len(messages))
	for _, message := range messages {
		cloned = append(cloned, cloneMessage(message))
	}
	return cloned
}

func cloneMessage(message model.Message) model.Message {
	cloned := message
	if message.Usage != nil {
		usage := *message.Usage
		cloned.Usage = &usage
	}

	if len(message.Attachments) > 0 {
		cloned.Attachments = make([]model.Attachment, 0, len(message.Attachments))
		for _, attachment := range message.Attachments {
			clonedAttachment := attachment
			if len(attachment.Data) > 0 {
				clonedAttachment.Data = append([]byte(nil), attachment.Data...)
			}
			cloned.Attachments = append(cloned.Attachments, clonedAttachment)
		}
	}

	if len(message.ReasoningItems) > 0 {
		cloned.ReasoningItems = make([]model.ReasoningItem, 0, len(message.ReasoningItems))
		for _, item := range message.ReasoningItems {
			clonedItem := item
			if len(item.Summary) > 0 {
				clonedItem.Summary = append([]model.ReasoningSummary(nil), item.Summary...)
			}
			cloned.ReasoningItems = append(cloned.ReasoningItems, clonedItem)
		}
	}

	if len(message.ToolCalls) > 0 {
		cloned.ToolCalls = make([]coretypes.ToolCall, 0, len(message.ToolCalls))
		for _, toolCall := range message.ToolCalls {
			clonedToolCall := toolCall
			if len(toolCall.ThoughtSignature) > 0 {
				clonedToolCall.ThoughtSignature = append([]byte(nil), toolCall.ThoughtSignature...)
			}
			cloned.ToolCalls = append(cloned.ToolCalls, clonedToolCall)
		}
	}

	if message.ProviderState != nil {
		cloned.ProviderState = &model.ProviderState{
			Provider:   message.ProviderState.Provider,
			Format:     message.ProviderState.Format,
			Version:    message.ProviderState.Version,
			ResponseID: message.ProviderState.ResponseID,
		}
		if len(message.ProviderState.Payload) > 0 {
			cloned.ProviderState.Payload = append([]byte(nil), message.ProviderState.Payload...)
		}
	}

	if message.ProviderData != nil {
		cloned.ProviderData = cloneProviderData(message.ProviderData)
	}

	return cloned
}

func cloneProviderData(value any) any {
	if value == nil {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var cloned any
	if err := json.Unmarshal(raw, &cloned); err != nil {
		return value
	}
	return cloned
}
