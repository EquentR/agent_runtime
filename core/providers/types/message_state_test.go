package model

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/EquentR/agent_runtime/core/types"
)

func TestMessageClonePreservesProviderState(t *testing.T) {
	raw := json.RawMessage(`{"role":"assistant","content":"hello"}`)
	msg := Message{
		Role:    RoleAssistant,
		Content: "hello",
		ProviderState: &ProviderState{
			Provider: "openai_completions",
			Format:   "openai_chat_message.v1",
			Version:  "v1",
			Payload:  raw,
		},
	}

	cloned := cloneMessage(msg)
	cloned.ProviderState.Payload[0] = 'x'

	if string(msg.ProviderState.Payload) != string(raw) {
		t.Fatalf("provider payload mutated = %s", string(msg.ProviderState.Payload))
	}
}

func TestMessageClonePreservesProviderData(t *testing.T) {
	msg := Message{
		Role:         RoleAssistant,
		Content:      "hello",
		ProviderData: map[string]any{"type": "openai_responses.output.v1", "output_json": `[{"type":"message","id":"msg_1"}]`},
	}

	cloned := cloneMessage(msg)
	clonedData := cloned.ProviderData.(map[string]any)
	clonedData["type"] = "changed"

	originalData := msg.ProviderData.(map[string]any)
	if originalData["type"] != "openai_responses.output.v1" {
		t.Fatalf("provider data mutated = %#v", originalData)
	}
}

func TestChatResponseSyncFieldsFromMessage(t *testing.T) {
	resp := ChatResponse{
		Message: Message{
			Role:      RoleAssistant,
			Content:   "hi",
			Reasoning: "plan",
			ReasoningItems: []ReasoningItem{{
				ID:      "rs_1",
				Summary: []ReasoningSummary{{Text: "step"}},
			}},
			ToolCalls: []types.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: "{}"}},
		},
	}

	resp.SyncFieldsFromMessage()
	resp.Message.ReasoningItems[0].Summary[0].Text = "changed"
	resp.Message.ToolCalls[0].ID = "changed"

	if resp.Content != "hi" || resp.Reasoning != "plan" {
		t.Fatalf("response projection = %#v", resp)
	}
	if resp.ReasoningItems[0].Summary[0].Text != "step" {
		t.Fatalf("reasoning items should be cloned, got %#v", resp.ReasoningItems)
	}
	if resp.ToolCalls[0].ID != "call_1" {
		t.Fatalf("tool calls should be cloned, got %#v", resp.ToolCalls)
	}
}

func TestChatResponseSyncMessageFromFields(t *testing.T) {
	resp := ChatResponse{
		Content:   "hi",
		Reasoning: "plan",
		ReasoningItems: []ReasoningItem{{
			ID:      "rs_1",
			Summary: []ReasoningSummary{{Text: "step"}},
		}},
		ToolCalls: []types.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: "{}"}},
	}

	resp.SyncMessageFromFields()
	resp.ReasoningItems[0].Summary[0].Text = "changed"
	resp.ToolCalls[0].ID = "changed"

	if resp.Message.Role != RoleAssistant {
		t.Fatalf("message role = %q, want %q", resp.Message.Role, RoleAssistant)
	}
	if resp.Message.Content != "hi" || resp.Message.Reasoning != "plan" {
		t.Fatalf("message projection = %#v", resp.Message)
	}
	if resp.Message.ReasoningItems[0].Summary[0].Text != "step" {
		t.Fatalf("message reasoning items should be cloned, got %#v", resp.Message.ReasoningItems)
	}
	if resp.Message.ToolCalls[0].ID != "call_1" {
		t.Fatalf("message tool calls should be cloned, got %#v", resp.Message.ToolCalls)
	}
}

func TestStreamEventPayloadFieldsUseValues(t *testing.T) {
	streamEventType := reflect.TypeOf(StreamEvent{})
	for _, fieldName := range []string{"ToolCall", "Usage", "Message"} {
		field, ok := streamEventType.FieldByName(fieldName)
		if !ok {
			t.Fatalf("field %s not found", fieldName)
		}
		if field.Type.Kind() == reflect.Ptr {
			t.Fatalf("field %s should use value semantics, got pointer %v", fieldName, field.Type)
		}
	}
}
