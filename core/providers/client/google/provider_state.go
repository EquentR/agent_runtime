package google

import (
	"encoding/json"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/types"
	genai "google.golang.org/genai"
)

const (
	providerName  = "google_genai"
	contentFormat = "google_genai_content.v1"
	contentV1     = "v1"
)

func contentFromProviderState(state *model.ProviderState) (*genai.Content, bool, error) {
	if state == nil {
		return nil, false, nil
	}
	if state.Provider != providerName || state.Format != contentFormat || state.Version != contentV1 {
		return nil, false, nil
	}
	var content genai.Content
	if err := json.Unmarshal(state.Payload, &content); err != nil {
		return nil, true, err
	}
	return &content, true, nil
}

func providerStateFromContent(content *genai.Content) (*model.ProviderState, error) {
	if content == nil {
		return nil, nil
	}
	payload, err := json.Marshal(content)
	if err != nil {
		return nil, err
	}
	return &model.ProviderState{
		Provider: providerName,
		Format:   contentFormat,
		Version:  contentV1,
		Payload:  payload,
	}, nil
}

func finalAssistantMessageFromContent(content string, reasoning string, toolCalls []types.ToolCall, state *model.ProviderState) model.Message {
	return model.Message{
		Role:          model.RoleAssistant,
		Content:       content,
		Reasoning:     reasoning,
		ToolCalls:     cloneToolCalls(toolCalls),
		ProviderState: cloneProviderState(state),
	}
}

func finalAssistantMessageFromObservedContent(observed *genai.Content) (model.Message, error) {
	content, reasoning, toolCalls, err := extractContentAndToolCalls(observed)
	if err != nil {
		return model.Message{}, err
	}
	state, err := providerStateFromContent(cloneGenAIContent(observed))
	if err != nil {
		return model.Message{}, err
	}
	return finalAssistantMessageFromContent(content, reasoning, toolCalls, state), nil
}

func cloneModelMessage(message model.Message) model.Message {
	message.ReasoningItems = append([]model.ReasoningItem(nil), message.ReasoningItems...)
	message.Attachments = cloneAttachments(message.Attachments)
	message.ToolCalls = cloneToolCalls(message.ToolCalls)
	message.ProviderState = cloneProviderState(message.ProviderState)
	return message
}

func cloneAttachments(attachments []model.Attachment) []model.Attachment {
	if len(attachments) == 0 {
		return nil
	}
	out := make([]model.Attachment, len(attachments))
	for i, attachment := range attachments {
		out[i] = attachment
		if len(attachment.Data) > 0 {
			out[i].Data = append([]byte(nil), attachment.Data...)
		}
	}
	return out
}

func cloneToolCalls(toolCalls []types.ToolCall) []types.ToolCall {
	if len(toolCalls) == 0 {
		return nil
	}
	out := make([]types.ToolCall, len(toolCalls))
	for i, toolCall := range toolCalls {
		out[i] = toolCall
		if len(toolCall.ThoughtSignature) > 0 {
			out[i].ThoughtSignature = append([]byte(nil), toolCall.ThoughtSignature...)
		}
	}
	return out
}

func cloneProviderState(state *model.ProviderState) *model.ProviderState {
	if state == nil {
		return nil
	}
	cloned := *state
	if len(state.Payload) > 0 {
		cloned.Payload = append(json.RawMessage(nil), state.Payload...)
	}
	return &cloned
}

func cloneGenAIContent(content *genai.Content) *genai.Content {
	if content == nil {
		return nil
	}
	cloned := &genai.Content{Role: content.Role}
	if len(content.Parts) > 0 {
		cloned.Parts = make([]*genai.Part, 0, len(content.Parts))
		for _, part := range content.Parts {
			cloned.Parts = append(cloned.Parts, cloneGenAIPart(part))
		}
	}
	return cloned
}

func accumulateObservedStreamPart(finalContent *genai.Content, toolCallAccumulator *streamToolCallAccumulator, part *genai.Part) {
	if finalContent == nil || part == nil {
		return
	}
	finalContent.Parts = append(finalContent.Parts, cloneGenAIPart(part))
	if toolCallAccumulator != nil && part.FunctionCall != nil {
		toolCallAccumulator.Append([]*genai.Part{part})
	}
}
