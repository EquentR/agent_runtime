package openai_chat

import (
	"encoding/json"
	"reflect"
	"testing"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	coretypes "github.com/EquentR/agent_runtime/core/types"
	"github.com/openai/openai-go/v3"
)

func TestProviderStateRoundTripPreservesSameRunToolTurnMessages(t *testing.T) {
	forced := []model.Message{
		{Role: model.RoleSystem, Content: "session prompt"},
		{Role: model.RoleSystem, Content: "forced block"},
	}
	userMessage := model.Message{Role: model.RoleUser, Content: "first"}
	assistantMessage := model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{
		ID:        "call_1",
		Name:      "lookup_weather",
		Arguments: `{"city":"Shanghai"}`,
	}}}
	toolResult := model.Message{Role: model.RoleTool, ToolCallId: "call_1", Content: "sunny"}

	sameRunMessages, _, err := buildOpenAIChatMessages([]model.Message{forced[0], forced[1], userMessage, assistantMessage, toolResult})
	if err != nil {
		t.Fatalf("buildOpenAIChatMessages(same-run) error = %v", err)
	}
	sameRunPayload := decodeChatMessages(t, sameRunMessages)

	state := chatMessageStateFromParts("", "", "", assistantMessage.ToolCalls)
	providerState, err := providerStateFromChatMessageState(state)
	if err != nil {
		t.Fatalf("providerStateFromChatMessageState() error = %v", err)
	}
	replayedAssistant := assistantMessage
	replayedAssistant.ProviderState = cloneProviderState(providerState)
	replayedMessages, _, err := buildOpenAIChatMessages([]model.Message{forced[0], forced[1], userMessage, replayedAssistant, toolResult})
	if err != nil {
		t.Fatalf("buildOpenAIChatMessages(replay) error = %v", err)
	}
	replayedPayload := decodeChatMessages(t, replayedMessages)

	if !reflect.DeepEqual(replayedPayload, sameRunPayload) {
		t.Fatalf("replayed messages differ from same-run tool turn messages:\nsame-run=%#v\nreplay=%#v", sameRunPayload, replayedPayload)
	}
}

func decodeChatMessages(t *testing.T, messages []openai.ChatCompletionMessageParamUnion) []map[string]any {
	t.Helper()
	raw, err := json.Marshal(messages)
	if err != nil {
		t.Fatalf("json.Marshal(messages) error = %v", err)
	}
	var payload []map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("json.Unmarshal(messages) error = %v", err)
	}
	return payload
}
