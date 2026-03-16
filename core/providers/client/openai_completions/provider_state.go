package openai

import (
	"encoding/json"
	"fmt"
	"strings"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/types"
	goopenai "github.com/sashabaranov/go-openai"
)

const (
	providerName   = "openai_completions"
	messageFormat  = "openai_chat_message.v1"
	messageVersion = "v1"
)

func messageFromProviderState(state *model.ProviderState) (goopenai.ChatCompletionMessage, bool, error) {
	if state == nil || state.Provider != providerName || state.Format != messageFormat {
		return goopenai.ChatCompletionMessage{}, false, nil
	}
	if state.Version != messageVersion {
		return goopenai.ChatCompletionMessage{}, true, fmt.Errorf("unsupported provider state version: %s", state.Version)
	}

	var msg goopenai.ChatCompletionMessage
	if err := json.Unmarshal(state.Payload, &msg); err != nil {
		return goopenai.ChatCompletionMessage{}, true, err
	}
	if msg.Role == "" {
		msg.Role = model.RoleAssistant
	}
	return msg, true, nil
}

func providerStateFromMessage(msg goopenai.ChatCompletionMessage) (*model.ProviderState, error) {
	if msg.Role == "" {
		msg.Role = model.RoleAssistant
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	return &model.ProviderState{
		Provider: providerName,
		Format:   messageFormat,
		Version:  messageVersion,
		Payload:  payload,
	}, nil
}

func finalAssistantMessage(content string, reasoning string, toolCalls []types.ToolCall, state *model.ProviderState) model.Message {
	message := model.Message{
		Role:          model.RoleAssistant,
		Content:       content,
		Reasoning:     strings.TrimSpace(reasoning),
		ProviderState: cloneProviderState(state),
	}
	if len(toolCalls) > 0 {
		message.ToolCalls = append([]types.ToolCall(nil), toolCalls...)
	}
	return message
}

func finalAssistantMessageFromNativeMessage(msg goopenai.ChatCompletionMessage) (model.Message, error) {
	reasoning, content := model.SplitLeadingThinkBlock(msg.Content)
	if msg.ReasoningContent != "" {
		reasoning = msg.ReasoningContent
	}

	toolCalls := make([]types.ToolCall, 0, len(msg.ToolCalls))
	for _, tc := range msg.ToolCalls {
		if tc.Type != "" && tc.Type != goopenai.ToolTypeFunction {
			continue
		}
		toolCalls = append(toolCalls, types.ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}
	if len(toolCalls) == 0 && msg.FunctionCall != nil && msg.FunctionCall.Name != "" {
		toolCalls = append(toolCalls, types.ToolCall{
			Name:      msg.FunctionCall.Name,
			Arguments: msg.FunctionCall.Arguments,
		})
	}

	state, err := providerStateFromMessage(msg)
	if err != nil {
		return model.Message{}, err
	}
	return finalAssistantMessage(content, reasoning, toolCalls, state), nil
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

func promptMessageFromOpenAIMessage(msg goopenai.ChatCompletionMessage) string {
	if len(msg.MultiContent) == 0 {
		return msg.Content
	}

	parts := make([]string, 0, len(msg.MultiContent))
	for _, part := range msg.MultiContent {
		if part.Text != "" {
			parts = append(parts, part.Text)
		}
	}
	if len(parts) == 0 {
		return msg.Content
	}
	return strings.Join(parts, "\n")
}
