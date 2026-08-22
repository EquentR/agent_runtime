package openai_responses

import (
	"encoding/json"
	"fmt"
	"strings"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/types"
	"github.com/openai/openai-go/v3/responses"
)

const (
	providerName        = "openai_responses"
	responseStateFormat = "openai_response_state.v1"
	outputItemsFormat   = "openai_response_output_items.v1"
	messageVersion      = "v1"
)

type persistedResponseState struct {
	ResponseID   string                  `json:"response_id,omitempty"`
	Conversation string                  `json:"conversation,omitempty"`
	Output       []persistedOutputItem   `json:"output"`
	Items        []persistedResponseItem `json:"items,omitempty"`
}

type persistedOutputItem struct {
	ID               string                                   `json:"id,omitempty"`
	Type             string                                   `json:"type,omitempty"`
	Role             string                                   `json:"role,omitempty"`
	Status           string                                   `json:"status,omitempty"`
	Content          []persistedOutputMessageContent          `json:"content,omitempty"`
	Summary          []responses.ResponseReasoningItemSummary `json:"summary,omitempty"`
	EncryptedContent string                                   `json:"encrypted_content,omitempty"`
	CallID           string                                   `json:"call_id,omitempty"`
	Name             string                                   `json:"name,omitempty"`
	Arguments        json.RawMessage                          `json:"arguments,omitempty"`
}

type persistedOutputMessageContent struct {
	Type    string `json:"type,omitempty"`
	Text    string `json:"text,omitempty"`
	Refusal string `json:"refusal,omitempty"`
}

type persistedResponseItem struct {
	ID               string `json:"id,omitempty"`
	Type             string `json:"type,omitempty"`
	CallID           string `json:"call_id,omitempty"`
	Name             string `json:"name,omitempty"`
	EncryptedContent string `json:"encrypted_content,omitempty"`
}

const rawOutputSnapshotType = "openai_responses.output.v1"

type rawOutputSnapshot struct {
	Type       string `json:"type"`
	ResponseID string `json:"response_id,omitempty"`
	OutputJSON string `json:"output_json"`
}

func outputItemsFromProviderState(state *model.ProviderState) ([]responses.ResponseInputItemUnionParam, bool, error) {
	items, ok, err := outputArchiveFromProviderState(state)
	if err != nil || !ok {
		return nil, ok, err
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

func outputArchiveFromProviderState(state *model.ProviderState) ([]responses.ResponseOutputItemUnion, bool, error) {
	if state == nil || state.Provider != providerName {
		return nil, false, nil
	}
	if state.Version != messageVersion {
		return nil, true, fmt.Errorf("unsupported provider state version: %s", state.Version)
	}

	switch state.Format {
	case responseStateFormat:
		var persisted persistedResponseState
		if err := json.Unmarshal(state.Payload, &persisted); err != nil {
			return nil, true, err
		}
		if len(persisted.Output) == 0 {
			return nil, true, nil
		}
		items := make([]responses.ResponseOutputItemUnion, 0, len(persisted.Output))
		for _, persistedItem := range persisted.Output {
			item, err := persistedOutputItemToResponse(persistedItem)
			if err != nil {
				return nil, true, err
			}
			items = append(items, item)
		}
		return items, true, nil
	case outputItemsFormat:
		var items []responses.ResponseOutputItemUnion
		if err := json.Unmarshal(state.Payload, &items); err != nil {
			return nil, true, err
		}
		return items, true, nil
	default:
		return nil, false, nil
	}
}

func providerStateFromOutputItems(responseID string, items []responses.ResponseOutputItemUnion) (*model.ProviderState, error) {
	persisted := make([]persistedOutputItem, 0, len(items))
	for _, item := range items {
		persistedItem, err := responseOutputItemToPersisted(item)
		if err != nil {
			return nil, err
		}
		persisted = append(persisted, persistedItem)
	}
	payload, err := json.Marshal(persistedResponseState{ResponseID: strings.TrimSpace(responseID), Output: persisted, Items: persistedResponseItems(items)})
	if err != nil {
		return nil, err
	}
	return &model.ProviderState{
		Provider:   providerName,
		Format:     responseStateFormat,
		Version:    messageVersion,
		ResponseID: strings.TrimSpace(responseID),
		Payload:    payload,
	}, nil
}

func responseOutputItemToPersisted(item responses.ResponseOutputItemUnion) (persistedOutputItem, error) {
	persisted := persistedOutputItem{
		ID:               strings.TrimSpace(item.ID),
		Type:             item.Type,
		Role:             item.Role,
		Status:           item.Status,
		EncryptedContent: strings.TrimSpace(item.EncryptedContent),
	}
	switch item.Type {
	case "message":
		persisted.Content = make([]persistedOutputMessageContent, 0, len(item.Content))
		for _, part := range item.Content {
			content := persistedOutputMessageContent{Type: part.Type, Text: part.Text, Refusal: part.Refusal}
			persisted.Content = append(persisted.Content, content)
		}
	case "reasoning":
		persisted.Summary = append([]responses.ResponseReasoningItemSummary(nil), item.Summary...)
	case "function_call":
		persisted.CallID = strings.TrimSpace(item.CallID)
		persisted.Name = strings.TrimSpace(item.Name)
		raw, err := json.Marshal(item.Arguments.OfString)
		if err != nil {
			raw = json.RawMessage(`""`)
		}
		persisted.Arguments = raw
	default:
		return persistedOutputItem{}, fmt.Errorf("unsupported output item type in provider state: %s", item.Type)
	}
	return persisted, nil
}

func persistedOutputItemToResponse(persisted persistedOutputItem) (responses.ResponseOutputItemUnion, error) {
	item := responses.ResponseOutputItemUnion{
		ID:               strings.TrimSpace(persisted.ID),
		Type:             persisted.Type,
		Role:             persisted.Role,
		Status:           persisted.Status,
		EncryptedContent: strings.TrimSpace(persisted.EncryptedContent),
	}
	switch persisted.Type {
	case "message":
		item.Content = make([]responses.ResponseOutputMessageContentUnion, 0, len(persisted.Content))
		for _, part := range persisted.Content {
			switch part.Type {
			case "", "output_text":
				item.Content = append(item.Content, responses.ResponseOutputMessageContentUnion{Type: "output_text", Text: part.Text})
			case "refusal":
				item.Content = append(item.Content, responses.ResponseOutputMessageContentUnion{Type: "refusal", Refusal: part.Refusal})
			default:
				return responses.ResponseOutputItemUnion{}, fmt.Errorf("unsupported persisted output message content type: %s", part.Type)
			}
		}
	case "reasoning":
		item.Summary = append([]responses.ResponseReasoningItemSummary(nil), persisted.Summary...)
	case "function_call":
		item.CallID = strings.TrimSpace(persisted.CallID)
		item.Name = strings.TrimSpace(persisted.Name)
		if len(persisted.Arguments) > 0 {
			var arguments string
			if err := json.Unmarshal(persisted.Arguments, &arguments); err != nil {
				// Legacy payloads stored the SDK union object as arguments.
				var legacy struct {
					OfString string `json:"OfString"`
				}
				if legacyErr := json.Unmarshal(persisted.Arguments, &legacy); legacyErr != nil {
					return responses.ResponseOutputItemUnion{}, err
				}
				arguments = legacy.OfString
			}
			item.Arguments.OfString = arguments
		}
	default:
		return responses.ResponseOutputItemUnion{}, fmt.Errorf("unsupported persisted output item type: %s", persisted.Type)
	}
	return item, nil
}

func persistedResponseItems(items []responses.ResponseOutputItemUnion) []persistedResponseItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]persistedResponseItem, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.ID) == "" && strings.TrimSpace(item.CallID) == "" {
			continue
		}
		out = append(out, persistedResponseItem{
			ID:               strings.TrimSpace(item.ID),
			Type:             item.Type,
			CallID:           strings.TrimSpace(item.CallID),
			Name:             strings.TrimSpace(item.Name),
			EncryptedContent: strings.TrimSpace(item.EncryptedContent),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func finalAssistantMessageFromResponse(content string, reasoning string, reasoningItems []model.ReasoningItem, toolCalls []types.ToolCall, state *model.ProviderState) model.Message {
	message := model.Message{
		Role:           model.RoleAssistant,
		Content:        content,
		Reasoning:      strings.TrimSpace(reasoning),
		ReasoningItems: cloneReasoningItems(reasoningItems),
		ProviderState:  cloneProviderState(state),
		ProviderData:   rawOutputSnapshotFromState(state),
	}
	if len(toolCalls) > 0 {
		message.ToolCalls = append([]types.ToolCall(nil), toolCalls...)
	}
	return message
}

func rawOutputSnapshotFromState(state *model.ProviderState) any {
	if state == nil {
		return nil
	}
	items, ok, err := outputArchiveFromProviderState(state)
	if err != nil || !ok {
		return nil
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return nil
	}
	return rawOutputSnapshot{Type: rawOutputSnapshotType, ResponseID: strings.TrimSpace(state.ResponseID), OutputJSON: string(raw)}
}

func rawOutputItemsFromProviderData(value any) ([]responses.ResponseOutputItemUnion, string, bool, error) {
	if value == nil {
		return nil, "", false, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, "", false, err
	}
	var snapshot rawOutputSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil, "", false, err
	}
	if snapshot.Type != rawOutputSnapshotType || strings.TrimSpace(snapshot.OutputJSON) == "" {
		return nil, "", false, nil
	}
	var items []responses.ResponseOutputItemUnion
	if err := json.Unmarshal([]byte(snapshot.OutputJSON), &items); err != nil {
		return nil, "", true, err
	}
	return items, strings.TrimSpace(snapshot.ResponseID), true, nil
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
		return responses.ResponseInputItemParamOfFunctionCall(item.Arguments.OfString, item.CallID, item.Name), nil
	default:
		return responses.ResponseInputItemUnionParam{}, fmt.Errorf("unsupported output item type in provider state: %s", item.Type)
	}
}
