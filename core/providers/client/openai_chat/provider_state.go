package openai_chat

import (
	"encoding/json"
	"fmt"
	"strings"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/types"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
)

const (
	providerName   = "openai_chat"
	messageFormat  = "openai_chat_message.v1"
	messageVersion = "v1"
)

type chatMessageState struct {
	Role             string              `json:"role,omitempty"`
	Content          string              `json:"content,omitempty"`
	ReasoningContent string              `json:"reasoning_content,omitempty"`
	Refusal          string              `json:"refusal,omitempty"`
	ToolCalls        []chatToolCallState `json:"tool_calls,omitempty"`
}

type chatToolCallState struct {
	ID       string                `json:"id,omitempty"`
	Type     string                `json:"type,omitempty"`
	Function chatFunctionCallState `json:"function,omitempty"`
}

type chatFunctionCallState struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

func messageParamFromProviderState(state *model.ProviderState) (openai.ChatCompletionMessageParamUnion, chatMessageState, bool, error) {
	if state == nil || state.Provider != providerName || state.Format != messageFormat {
		return openai.ChatCompletionMessageParamUnion{}, chatMessageState{}, false, nil
	}
	if state.Version != messageVersion {
		return openai.ChatCompletionMessageParamUnion{}, chatMessageState{}, true, fmt.Errorf("unsupported provider state version: %s", state.Version)
	}

	var message chatMessageState
	if err := json.Unmarshal(state.Payload, &message); err != nil {
		return openai.ChatCompletionMessageParamUnion{}, chatMessageState{}, true, err
	}
	if message.Role == "" {
		message.Role = model.RoleAssistant
	}
	outbound := message
	outbound.ReasoningContent = ""
	payload, err := json.Marshal(outbound)
	if err != nil {
		return openai.ChatCompletionMessageParamUnion{}, chatMessageState{}, true, err
	}
	return param.Override[openai.ChatCompletionMessageParamUnion](json.RawMessage(payload)), message, true, nil
}

func providerStateFromChatMessageState(message chatMessageState) (*model.ProviderState, error) {
	if message.Role == "" {
		message.Role = model.RoleAssistant
	}
	payload, err := json.Marshal(message)
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

func chatMessageStateFromParts(content string, reasoning string, refusal string, toolCalls []types.ToolCall) chatMessageState {
	state := chatMessageState{
		Role:             model.RoleAssistant,
		Content:          content,
		ReasoningContent: strings.TrimSpace(reasoning),
		Refusal:          refusal,
	}
	if len(toolCalls) > 0 {
		state.ToolCalls = make([]chatToolCallState, 0, len(toolCalls))
		for _, call := range toolCalls {
			state.ToolCalls = append(state.ToolCalls, chatToolCallState{
				ID:   call.ID,
				Type: "function",
				Function: chatFunctionCallState{
					Name:      call.Name,
					Arguments: call.Arguments,
				},
			})
		}
	}
	return state
}

func modelToolCallsFromState(calls []chatToolCallState) []types.ToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]types.ToolCall, 0, len(calls))
	for _, call := range calls {
		if call.Type != "" && call.Type != "function" {
			continue
		}
		out = append(out, types.ToolCall{
			ID:        call.ID,
			Name:      call.Function.Name,
			Arguments: call.Function.Arguments,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func finalAssistantMessageFromState(state chatMessageState) (model.Message, error) {
	reasoning, content := model.SplitLeadingThinkBlock(state.Content)
	if strings.TrimSpace(state.ReasoningContent) != "" {
		reasoning = state.ReasoningContent
	}
	if content == "" && state.Refusal != "" {
		content = state.Refusal
	}
	providerState, err := providerStateFromChatMessageState(state)
	if err != nil {
		return model.Message{}, err
	}
	return finalAssistantMessage(content, reasoning, modelToolCallsFromState(state.ToolCalls), providerState), nil
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
