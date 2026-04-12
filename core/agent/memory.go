package agent

import (
	"context"
	"encoding/json"
	"time"

	"github.com/EquentR/agent_runtime/core/forcedprompt"
	"github.com/EquentR/agent_runtime/core/memory"
	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/runtimeprompt"
	coretypes "github.com/EquentR/agent_runtime/core/types"
)

type preparedConversationContext struct {
	Memory           memory.RuntimeContext
	ConversationBody []model.Message
}

func (r *Runner) prepareConversationContextWithPersistedCount(ctx context.Context, input []model.Message, persistedCount int) (preparedConversationContext, error) {
	conversation := cloneMessages(input)
	prepared := preparedConversationContext{ConversationBody: conversation}
	if r.options.Memory == nil {
		prepared.Memory = memory.RuntimeContext{Tail: cloneMessages(conversation)}
		return prepared, nil
	}

	newMessages := unpersistedConversationTail(conversation, persistedCount)
	if len(newMessages) > 0 {
		r.options.Memory.AddMessages(newMessages)
	}
	memoryContext, trace, err := r.options.Memory.RuntimeContextWithReserve(ctx, 0)
	if err != nil {
		return preparedConversationContext{}, err
	}
	if trace.Succeeded {
		r.emitMemoryCompressed(ctx, trace)
		r.emitMemoryContextStateFromManager(ctx)
	}
	prepared.Memory = memoryContext
	return prepared, nil
}

func (r *Runner) buildRequestMessages(runtimeContext memory.RuntimeContext, afterToolTurn bool) (runtimeprompt.BuildResult, []model.Message, error) {
	builder := r.options.RuntimePromptBuilder
	if builder == nil {
		builder = runtimeprompt.NewBuilder(forcedprompt.NewProvider())
	}
	now := time.Now
	if r.options.Now != nil {
		now = r.options.Now
	}
	conversationBody := make([]model.Message, 0, len(r.options.ConversationPrelude)+len(runtimeContext.Tail)+1)
	if runtimeContext.Recap != nil {
		conversationBody = append(conversationBody, cloneMessage(*runtimeContext.Recap))
	}
	conversationBody = append(conversationBody, cloneMessages(r.options.ConversationPrelude)...)
	conversationBody = append(conversationBody, cloneMessages(runtimeContext.Tail)...)
	buildResult, err := builder.Build(runtimeprompt.BuildInput{
		Time:               now(),
		ConversationBody:   conversationBody,
		ResolvedPrompt:     r.options.ResolvedPrompt,
		AfterToolTurn:      afterToolTurn,
		LegacySystemPrompt: r.options.SystemPrompt,
	})
	if err != nil {
		return runtimeprompt.BuildResult{}, nil, err
	}
	messages := runtimeprompt.NewRenderer().Render(buildResult)
	return buildResult, messages, nil
}

func unpersistedConversationTail(messages []model.Message, persistedCount int) []model.Message {
	if len(messages) == 0 {
		return nil
	}
	if persistedCount <= 0 {
		return cloneMessages(messages)
	}
	if persistedCount >= len(messages) {
		return nil
	}
	return cloneMessages(messages[persistedCount:])
}

func cloneMessages(messages []model.Message) []model.Message {
	if len(messages) == 0 {
		return nil
	}
	cloned := make([]model.Message, len(messages))
	for i, message := range messages {
		cloned[i] = cloneMessage(message)
	}
	return cloned
}

func cloneMessage(message model.Message) model.Message {
	message.Usage = cloneTokenUsage(message.Usage)
	message.ReasoningItems = cloneReasoningItems(message.ReasoningItems)
	message.Attachments = cloneAttachments(message.Attachments)
	message.ToolCalls = cloneToolCalls(message.ToolCalls)
	message.ProviderState = cloneProviderState(message.ProviderState)
	message.ProviderData = cloneProviderData(message.ProviderData)
	return message
}

func cloneTokenUsage(usage *model.TokenUsage) *model.TokenUsage {
	if usage == nil {
		return nil
	}
	cloned := *usage
	return &cloned
}

func cloneReasoningItems(items []model.ReasoningItem) []model.ReasoningItem {
	if len(items) == 0 {
		return nil
	}
	cloned := make([]model.ReasoningItem, len(items))
	for i, item := range items {
		cloned[i] = item
		if len(item.Summary) > 0 {
			cloned[i].Summary = append([]model.ReasoningSummary(nil), item.Summary...)
		}
	}
	return cloned
}

func cloneAttachments(attachments []model.Attachment) []model.Attachment {
	if len(attachments) == 0 {
		return nil
	}
	cloned := make([]model.Attachment, len(attachments))
	for i, attachment := range attachments {
		cloned[i] = attachment
		if len(attachment.Data) > 0 {
			cloned[i].Data = append([]byte(nil), attachment.Data...)
		}
	}
	return cloned
}

func cloneToolCalls(toolCalls []coretypes.ToolCall) []coretypes.ToolCall {
	if len(toolCalls) == 0 {
		return nil
	}
	cloned := make([]coretypes.ToolCall, len(toolCalls))
	for i, toolCall := range toolCalls {
		cloned[i] = toolCall
		if len(toolCall.ThoughtSignature) > 0 {
			cloned[i].ThoughtSignature = append([]byte(nil), toolCall.ThoughtSignature...)
		}
	}
	return cloned
}

func cloneProviderState(state *model.ProviderState) *model.ProviderState {
	if state == nil {
		return nil
	}
	cloned := *state
	if len(state.Payload) > 0 {
		cloned.Payload = append([]byte(nil), state.Payload...)
	}
	return &cloned
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

func normalizeAssistantMessage(resp model.ChatResponse) model.Message {
	message := cloneMessage(resp.Message)
	if message.Role == "" && (resp.Content != "" || resp.Reasoning != "" || len(resp.ReasoningItems) > 0 || len(resp.ToolCalls) > 0) {
		resp.SyncMessageFromFields()
		message = cloneMessage(resp.Message)
	}
	if message.Role == "" {
		message.Role = model.RoleAssistant
	}
	return message
}
