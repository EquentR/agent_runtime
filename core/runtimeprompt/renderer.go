package runtimeprompt

import (
	"encoding/json"
	"strings"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	coretypes "github.com/EquentR/agent_runtime/core/types"
)

type Renderer struct{}

func NewRenderer() *Renderer {
	return &Renderer{}
}

func (r *Renderer) Render(result BuildResult) []model.Message {
	request := make([]model.Message, 0, len(result.Envelope.Segments)+len(result.Body))
	request = appendSegmentMessages(request, result.Envelope.Segments, PhaseSession)
	request = appendSegmentMessages(request, result.Envelope.Segments, PhaseStepPreModel)

	body := cloneMessages(result.Body)
	if result.AfterToolTurn {
		body = injectToolResultPromptMessages(body, segmentMessages(result.Envelope.Segments, PhaseToolResult))
	}
	request = append(request, body...)
	return request
}

func appendSegmentMessages(dst []model.Message, segments []Segment, phase string) []model.Message {
	return append(dst, segmentMessages(segments, phase)...)
}

func segmentMessages(segments []Segment, phase string) []model.Message {
	messages := make([]model.Message, 0)
	for _, segment := range segments {
		if strings.TrimSpace(segment.Phase) != phase || strings.TrimSpace(segment.Content) == "" {
			continue
		}
		role := strings.TrimSpace(segment.Role)
		if role == "" {
			role = model.RoleSystem
		}
		messages = append(messages, model.Message{Role: role, Content: segment.Content})
	}
	return messages
}

func injectToolResultPromptMessages(conversation []model.Message, prompts []model.Message) []model.Message {
	if len(conversation) == 0 {
		return appendPromptMessages(conversation, prompts)
	}

	firstTrailingTool := len(conversation)
	for firstTrailingTool > 0 && conversation[firstTrailingTool-1].Role == model.RoleTool {
		firstTrailingTool--
	}
	if firstTrailingTool == len(conversation) {
		return appendPromptMessages(conversation, prompts)
	}
	if firstTrailingTool == 0 || conversation[firstTrailingTool-1].Role != model.RoleAssistant {
		return appendPromptMessages(conversation, prompts)
	}

	result := make([]model.Message, 0, len(conversation)+len(prompts))
	result = append(result, conversation[:firstTrailingTool]...)
	result = appendPromptMessages(result, prompts)
	result = append(result, conversation[firstTrailingTool:]...)
	return result
}

func appendPromptMessages(dst []model.Message, prompts []model.Message) []model.Message {
	for _, prompt := range prompts {
		if strings.TrimSpace(prompt.Content) == "" {
			continue
		}
		dst = append(dst, cloneMessage(prompt))
	}
	return dst
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
