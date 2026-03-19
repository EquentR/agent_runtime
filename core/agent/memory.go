package agent

import (
	"context"
	"encoding/json"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	coretypes "github.com/EquentR/agent_runtime/core/types"
)

func (r *Runner) buildRequestMessages(ctx context.Context, input []model.Message) ([]model.Message, error) {
	conversation := cloneMessages(input)
	if r.options.Memory != nil {
		r.options.Memory.AddMessages(conversation)
		contextMessages, err := r.options.Memory.ContextMessages(ctx)
		if err != nil {
			return nil, err
		}
		conversation = contextMessages
	}
	return prependSystemPrompt(r.options.SystemPrompt, conversation), nil
}

func prependSystemPrompt(systemPrompt string, messages []model.Message) []model.Message {
	conversation := cloneMessages(messages)
	if systemPrompt == "" {
		return conversation
	}
	conversation = append([]model.Message{{
		Role:    model.RoleSystem,
		Content: systemPrompt,
	}}, conversation...)
	return conversation
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
