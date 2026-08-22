package openai_responses

import (
	"encoding/json"
	"reflect"
	"testing"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	coretypes "github.com/EquentR/agent_runtime/core/types"
	"github.com/openai/openai-go/v3/responses"
)

func TestProviderStateRoundTripPreservesSameRunToolTurnInput(t *testing.T) {
	forced := []model.Message{
		{Role: model.RoleSystem, Content: "session prompt"},
		{Role: model.RoleSystem, Content: "forced block"},
	}
	userMessage := model.Message{Role: model.RoleUser, Content: "first"}
	assistantMessage := model.Message{Role: model.RoleAssistant, Content: "", ToolCalls: []coretypes.ToolCall{{
		ID:        "call_1",
		Name:      "lookup_weather",
		Arguments: `{"city":"Shanghai"}`,
	}}}
	toolResult := model.Message{Role: model.RoleTool, ToolCallId: "call_1", Content: "sunny"}

	// This is the normalized input the runner sends for the second model call
	// inside the same turn: no provider state, and no assistant message when the
	// assistant turn only contains tool calls.
	sameRunMessages, _, err := buildResponseInput(
		[]model.Message{forced[0], forced[1], userMessage, assistantMessage, toolResult},
		"system",
	)
	if err != nil {
		t.Fatalf("buildResponseInput(same-run) error = %v", err)
	}
	sameRunRaw, err := json.Marshal(sameRunMessages)
	if err != nil {
		t.Fatalf("json.Marshal(same-run input) error = %v", err)
	}

	// The persisted assistant message carries provider state produced by
	// providerStateFromOutputItems. It must replay to exactly the same input.
	state, err := providerStateFromOutputItems("resp_1", []responses.ResponseOutputItemUnion{
		{Type: "function_call", ID: "fc_1", CallID: "call_1", Name: "lookup_weather", Arguments: responses.ResponseOutputItemUnionArguments{OfString: `{"city":"Shanghai"}`}},
	})
	if err != nil {
		t.Fatalf("providerStateFromOutputItems() error = %v", err)
	}
	replayedAssistant := assistantMessage
	replayedAssistant.ProviderState = cloneProviderState(state)
	replayedMessages, _, err := buildResponseInput(
		[]model.Message{forced[0], forced[1], userMessage, replayedAssistant, toolResult},
		"system",
	)
	if err != nil {
		t.Fatalf("buildResponseInput(replay) error = %v", err)
	}
	replayedRaw, err := json.Marshal(replayedMessages)
	if err != nil {
		t.Fatalf("json.Marshal(replayed input) error = %v", err)
	}

	if !reflect.DeepEqual(replayedMessages, sameRunMessages) {
		t.Fatalf("replayed input differs from same-run tool turn input:\nsame-run=%s\nreplay=%s", sameRunRaw, replayedRaw)
	}
	replayedFunctionCall := findFunctionCallInput(t, replayedMessages)
	if got := replayedFunctionCall["arguments"]; got != `{"city":"Shanghai"}` {
		t.Fatalf("replayed function_call arguments = %#v, want %q", got, `{"city":"Shanghai"}`)
	}
}

func findFunctionCallInput(t *testing.T, messages []responses.ResponseInputItemUnionParam) map[string]any {
	t.Helper()
	raw, err := json.Marshal(messages)
	if err != nil {
		t.Fatalf("json.Marshal(input) error = %v", err)
	}
	var items []map[string]any
	if err := json.Unmarshal(raw, &items); err != nil {
		t.Fatalf("json.Unmarshal(input) error = %v", err)
	}
	for _, item := range items {
		if item["type"] == "function_call" {
			return item
		}
	}
	t.Fatalf("input = %#v, want function_call item", items)
	return nil
}
