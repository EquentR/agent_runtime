package memory

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"unicode/utf8"

	providertools "github.com/EquentR/agent_runtime/core/providers/tools"
	model "github.com/EquentR/agent_runtime/core/providers/types"
	coretypes "github.com/EquentR/agent_runtime/core/types"
)

const (
	DefaultMaxContextTokens int64 = 100_000

	shortTermBudgetNumerator     int64 = 70
	contextBudgetDenominator     int64 = 100
	defaultSummaryPromptTemplate       = "以下为当前会话的压缩记忆，仅在相关时参考：\n%s"
	truncationMarker                   = "..."
)

type TokenCounter interface {
	Count(text string) int
	CountMessages(messages []string) int
}

type CompressionRequest struct {
	PreviousSummary  string
	Messages         []model.Message
	MaxSummaryTokens int64
}

type Compressor func(ctx context.Context, request CompressionRequest) (string, error)

type Options struct {
	Model            *coretypes.LLMModel
	MaxContextTokens int64
	Counter          TokenCounter
	Compressor       Compressor
	SummaryTemplate  string
}

type Manager struct {
	mu                   sync.RWMutex
	summary              string
	shortTerm            []model.Message
	maxContextTokens     int64
	shortTermLimitTokens int64
	summaryLimitTokens   int64
	counter              TokenCounter
	compressor           Compressor
	summaryTemplate      string
}

func NewManager(options Options) (*Manager, error) {
	maxContextTokens := resolveMaxContextTokens(options.MaxContextTokens, options.Model)
	shortTermLimitTokens, summaryLimitTokens := splitContextBudget(maxContextTokens)

	counter := options.Counter
	if counter == nil {
		counter = newDefaultTokenCounter(options.Model)
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

	return &Manager{
		maxContextTokens:     maxContextTokens,
		shortTermLimitTokens: shortTermLimitTokens,
		summaryLimitTokens:   summaryLimitTokens,
		counter:              counter,
		compressor:           compressor,
		summaryTemplate:      summaryTemplate,
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

func (m *Manager) MaxContextTokens() int64 {
	return m.maxContextTokens
}

func (m *Manager) ShortTermLimitTokens() int64 {
	return m.shortTermLimitTokens
}

func (m *Manager) SummaryLimitTokens() int64 {
	return m.summaryLimitTokens
}

func (m *Manager) ClearShortTerm() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.shortTerm = nil
}

func (m *Manager) ContextMessages(ctx context.Context) ([]model.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.requiresCompressionLocked() {
		if err := m.compressLocked(ctx); err != nil {
			return nil, err
		}
	}

	return m.contextMessagesLocked(), nil
}

func (m *Manager) requiresCompressionLocked() bool {
	if len(m.shortTerm) == 0 {
		return false
	}
	return m.estimateShortTermTokensLocked() > m.shortTermLimitTokens
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
	request := CompressionRequest{
		PreviousSummary:  m.summary,
		Messages:         cloneMessages(m.shortTerm),
		MaxSummaryTokens: m.summaryLimitTokens,
	}
	summary, err := m.compressor(ctx, request)
	if err != nil {
		return err
	}
	summary = strings.TrimSpace(summary)
	if m.summaryLimitTokens > 0 && summary != "" && int64(m.counter.Count(summary)) > m.summaryLimitTokens {
		summary = limitTextByTokens(summary, m.summaryLimitTokens, m.counter)
	}

	m.summary = summary
	m.shortTerm = nil
	return nil
}

func (m *Manager) contextMessagesLocked() []model.Message {
	out := make([]model.Message, 0, len(m.shortTerm)+1)
	if m.summary != "" {
		out = append(out, model.Message{
			Role:    model.RoleSystem,
			Content: renderSummaryWithinBudget(m.summaryTemplate, m.summary, m.summaryLimitTokens, m.counter),
		})
	}
	out = append(out, cloneMessages(m.shortTerm)...)
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
	parts := make([]string, 0, 5)
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

	return cloned
}
