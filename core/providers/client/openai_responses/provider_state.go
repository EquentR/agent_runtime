package openai_official

import (
	"encoding/json"
	"fmt"
	"strings"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/types"
	"github.com/openai/openai-go/responses"
)

const (
	providerName      = "openai_responses"
	outputItemsFormat = "openai_response_output_items.v1"
	messageVersion    = "v1"
)

func outputItemsFromProviderState(state *model.ProviderState) ([]responses.ResponseInputItemUnionParam, bool, error) {
	if state == nil || state.Provider != providerName || state.Format != outputItemsFormat {
		return nil, false, nil
	}
	if state.Version != messageVersion {
		return nil, true, fmt.Errorf("unsupported provider state version: %s", state.Version)
	}

	var items []responses.ResponseOutputItemUnion
	if err := json.Unmarshal(state.Payload, &items); err != nil {
		return nil, true, err
	}
	params := make([]responses.ResponseInputItemUnionParam, 0, len(items))
	for _, item := range items {
		param, err := responseOutputItemToInputParam(item)
		if err != nil {
			return nil, true, err
		}
		params = append(params, param)
	}
	return params, true, nil
}

func providerStateFromOutputItems(responseID string, items []responses.ResponseOutputItemUnion) (*model.ProviderState, error) {
	payload, err := json.Marshal(items)
	if err != nil {
		return nil, err
	}
	return &model.ProviderState{
		Provider: providerName,
		Format:   outputItemsFormat,
		Version:  messageVersion,
		Payload:  payload,
	}, nil
}

func finalAssistantMessageFromResponse(content string, reasoning string, reasoningItems []model.ReasoningItem, toolCalls []types.ToolCall, state *model.ProviderState) model.Message {
	message := model.Message{
		Role:           model.RoleAssistant,
		Content:        content,
		Reasoning:      strings.TrimSpace(reasoning),
		ReasoningItems: cloneReasoningItems(reasoningItems),
		ProviderState:  cloneProviderState(state),
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

func cloneReasoningItems(items []model.ReasoningItem) []model.ReasoningItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]model.ReasoningItem, len(items))
	for i, item := range items {
		out[i] = item
		if len(item.Summary) > 0 {
			out[i].Summary = append([]model.ReasoningSummary(nil), item.Summary...)
		}
	}
	return out
}

func responseOutputItemToInputParam(item responses.ResponseOutputItemUnion) (responses.ResponseInputItemUnionParam, error) {
	switch item.Type {
	case "message":
		content := make([]responses.ResponseOutputMessageContentUnionParam, 0, len(item.Content))
		for _, part := range item.Content {
			switch part.Type {
			case "", "output_text":
				content = append(content, responses.ResponseOutputMessageContentUnionParam{
					OfOutputText: &responses.ResponseOutputTextParam{Text: part.Text},
				})
			case "refusal":
				content = append(content, responses.ResponseOutputMessageContentUnionParam{
					OfRefusal: &responses.ResponseOutputRefusalParam{Refusal: part.Refusal},
				})
			default:
				return responses.ResponseInputItemUnionParam{}, fmt.Errorf("unsupported output message content type: %s", part.Type)
			}
		}
		status := responses.ResponseOutputMessageStatus(item.Status)
		if status == "" {
			status = responses.ResponseOutputMessageStatusCompleted
		}
		return responses.ResponseInputItemParamOfOutputMessage(content, item.ID, status), nil
	case "reasoning":
		return modelReasoningItemToResponse(responseReasoningItemToModel(item)), nil
	case "function_call":
		return responses.ResponseInputItemParamOfFunctionCall(item.Arguments, item.CallID, item.Name), nil
	default:
		return responses.ResponseInputItemUnionParam{}, fmt.Errorf("unsupported output item type in provider state: %s", item.Type)
	}
}
