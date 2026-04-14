package openai_responses

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/types"
	"github.com/openai/openai-go/v3/responses"
)

func TestBuildResponseInputEncodesImageAttachmentForUserMessage(t *testing.T) {
	input, _, err := buildResponseInput([]model.Message{{
		Role:    model.RoleUser,
		Content: "look at this",
		Attachments: []model.Attachment{{
			FileName: "image.png",
			MimeType: "image/png",
			Data:     []byte{0x89, 0x50, 0x4e, 0x47},
		}},
	}}, "system")
	if err != nil {
		t.Fatalf("buildResponseInput() error = %v", err)
	}

	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("json.Marshal(input) error = %v", err)
	}
	var payload []map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("json.Unmarshal(payload) error = %v", err)
	}
	if len(payload) != 1 {
		t.Fatalf("len(payload) = %d, want 1", len(payload))
	}
	content, ok := payload[0]["content"].([]any)
	if !ok || len(content) != 2 {
		t.Fatalf("content = %#v, want two content items", payload[0]["content"])
	}
	imageItem, ok := content[1].(map[string]any)
	if !ok {
		t.Fatalf("image item = %#v, want object", content[1])
	}
	if imageItem["type"] != "input_image" {
		t.Fatalf("image item type = %#v, want input_image", imageItem["type"])
	}
	wantURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte{0x89, 0x50, 0x4e, 0x47})
	if imageItem["image_url"] != wantURL {
		t.Fatalf("image_url = %#v, want %q", imageItem["image_url"], wantURL)
	}
}

func TestBuildResponseInputEncodesTextAttachmentForUserMessage(t *testing.T) {
	input, _, err := buildResponseInput([]model.Message{{
		Role:    model.RoleUser,
		Content: "summarize this",
		Attachments: []model.Attachment{{
			FileName: "note.txt",
			MimeType: "text/plain",
			Data:     []byte("hello"),
		}},
	}}, "system")
	if err != nil {
		t.Fatalf("buildResponseInput() error = %v", err)
	}

	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("json.Marshal(input) error = %v", err)
	}
	var payload []map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("json.Unmarshal(payload) error = %v", err)
	}
	content, ok := payload[0]["content"].([]any)
	if !ok || len(content) != 2 {
		t.Fatalf("content = %#v, want two content items", payload[0]["content"])
	}
	fileItem, ok := content[1].(map[string]any)
	if !ok {
		t.Fatalf("file item = %#v, want object", content[1])
	}
	if fileItem["type"] != "input_file" {
		t.Fatalf("file item type = %#v, want input_file", fileItem["type"])
	}
	if fileItem["filename"] != "note.txt" {
		t.Fatalf("filename = %#v, want note.txt", fileItem["filename"])
	}
	wantData := base64.StdEncoding.EncodeToString([]byte("hello"))
	if fileItem["file_data"] != wantData {
		t.Fatalf("file_data = %#v, want %q", fileItem["file_data"], wantData)
	}
}

func TestBuildResponseInputRejectsUnsupportedAttachmentType(t *testing.T) {
	_, _, err := buildResponseInput([]model.Message{{
		Role: model.RoleUser,
		Attachments: []model.Attachment{{
			FileName: "archive.zip",
			MimeType: "application/zip",
			Data:     []byte{0x50, 0x4b, 0x03, 0x04},
		}},
	}}, "system")
	if err == nil || !strings.Contains(err.Error(), "unsupported attachment type") {
		t.Fatalf("buildResponseInput() error = %v, want unsupported attachment type", err)
	}
}

func TestBuildResponseRequestParams_MessageAndToolMapping(t *testing.T) {
	temp := float32(0.4)
	topP := float32(0.9)

	req := model.ChatRequest{
		Model:     "gpt-4o-mini",
		MaxTokens: 128,
		Sampling: model.SamplingParams{
			Temperature: &temp,
			TopP:        &topP,
		},
		Messages: []model.Message{
			{Role: model.RoleSystem, Content: "You are helpful"},
			{Role: model.RoleAssistant, Content: "I will call a tool", ToolCalls: []types.ToolCall{{
				ID:        "call_1",
				Name:      "lookup_weather",
				Arguments: `{"city":"Beijing"}`,
			}}},
			{Role: model.RoleTool, ToolCallId: "call_1", Content: `{"temp":26}`},
			{Role: model.RoleUser, Content: "继续"},
		},
		Tools: []types.Tool{{
			Name:        "lookup_weather",
			Description: "查询天气",
			Parameters: types.JSONSchema{
				Type: "object",
				Properties: map[string]types.SchemaProperty{
					"city": {Type: "string", Description: "城市名"},
				},
				Required: []string{"city"},
			},
		}},
		ToolChoice: types.ToolChoice{Type: types.ToolForce, Name: "lookup_weather"},
	}

	params, err := buildResponseRequestParams(req)
	if err != nil {
		t.Fatalf("buildResponseRequestParams() error = %v", err)
	}

	var payload map[string]any
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("json.Marshal(params) error = %v", err)
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("json.Unmarshal(payload) error = %v", err)
	}

	if got, _ := payload["model"].(string); got != "gpt-4o-mini" {
		t.Fatalf("model = %q, want gpt-4o-mini", got)
	}
	if got, _ := payload["max_output_tokens"].(float64); int64(got) != 128 {
		t.Fatalf("max_output_tokens = %v, want 128", got)
	}
	if got, _ := payload["temperature"].(float64); got < 0.399 || got > 0.401 {
		t.Fatalf("temperature = %v, want 0.4", got)
	}
	if got, _ := payload["top_p"].(float64); got < 0.899 || got > 0.901 {
		t.Fatalf("top_p = %v, want 0.9", got)
	}
	if got, _ := payload["store"].(bool); got != false {
		t.Fatalf("store = %v, want false", got)
	}

	input, ok := payload["input"].([]any)
	if !ok {
		t.Fatalf("input type = %T, want []any", payload["input"])
	}
	if len(input) != 5 {
		t.Fatalf("len(input) = %d, want 5", len(input))
	}

	functionCallCount := 0
	functionOutputCount := 0
	for _, item := range input {
		obj, _ := item.(map[string]any)
		typ, _ := obj["type"].(string)
		switch typ {
		case "function_call":
			functionCallCount++
		case "function_call_output":
			functionOutputCount++
		}
	}
	if functionCallCount != 1 {
		t.Fatalf("function_call item count = %d, want 1", functionCallCount)
	}
	if functionOutputCount != 1 {
		t.Fatalf("function_call_output item count = %d, want 1", functionOutputCount)
	}

	if _, exists := payload["tools"]; exists {
		t.Fatalf("tools should be omitted for continuation payload, got %#v", payload["tools"])
	}
}

func TestBuildResponseRequestParams_ReplaysAssistantReasoningItems(t *testing.T) {
	req := model.ChatRequest{
		Model: "gpt-5.4",
		Messages: []model.Message{{
			Role:      model.RoleAssistant,
			Reasoning: "plan first",
			ReasoningItems: []model.ReasoningItem{{
				ID:               "rs_1",
				Summary:          []model.ReasoningSummary{{Text: "plan first"}},
				EncryptedContent: "enc_123",
			}},
			ToolCalls: []types.ToolCall{{
				ID:        "call_1",
				Name:      "lookup_weather",
				Arguments: `{"city":"Beijing"}`,
			}},
		}},
	}

	params, err := buildResponseRequestParams(req)
	if err != nil {
		t.Fatalf("buildResponseRequestParams() error = %v", err)
	}

	var payload map[string]any
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("json.Marshal(params) error = %v", err)
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("json.Unmarshal(payload) error = %v", err)
	}

	input, ok := payload["input"].([]any)
	if !ok || len(input) != 2 {
		t.Fatalf("input = %#v, want 2 items", payload["input"])
	}
	first, _ := input[0].(map[string]any)
	if first["type"] != "reasoning" || first["id"] != "rs_1" {
		t.Fatalf("replayed reasoning item = %#v", first)
	}
	if first["encrypted_content"] != "enc_123" {
		t.Fatalf("encrypted_content = %v, want enc_123", first["encrypted_content"])
	}
}

func TestBuildResponseRequestParams_UsesDeveloperRoleForReasoningModelSystemPrompt(t *testing.T) {
	temp := float32(0.4)
	topP := float32(0.9)

	params, err := buildResponseRequestParams(model.ChatRequest{
		Model: "gpt-5.4",
		Messages: []model.Message{
			{Role: model.RoleSystem, Content: "You are helpful"},
			{Role: model.RoleUser, Content: "hi"},
		},
		Sampling: model.SamplingParams{
			Temperature: &temp,
			TopP:        &topP,
		},
	})
	if err != nil {
		t.Fatalf("buildResponseRequestParams() error = %v", err)
	}

	var payload map[string]any
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("json.Marshal(params) error = %v", err)
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("json.Unmarshal(payload) error = %v", err)
	}

	input, ok := payload["input"].([]any)
	if !ok || len(input) != 2 {
		t.Fatalf("input = %#v, want 2 items", payload["input"])
	}
	first, _ := input[0].(map[string]any)
	if got, _ := first["role"].(string); got != "developer" {
		t.Fatalf("system role = %q, want developer", got)
	}
	if _, exists := payload["reasoning"]; !exists {
		t.Fatalf("reasoning should be present for reasoning model payload")
	}
	if _, exists := payload["temperature"]; exists {
		t.Fatalf("temperature should be omitted for reasoning model payload")
	}
	if _, exists := payload["top_p"]; exists {
		t.Fatalf("top_p should be omitted for reasoning model payload")
	}
}

func TestBuildResponseRequestParams_RemovesSystemPromptForO1Mini(t *testing.T) {
	params, err := buildResponseRequestParams(model.ChatRequest{
		Model: "o1-mini",
		Messages: []model.Message{
			{Role: model.RoleSystem, Content: "You are helpful"},
			{Role: model.RoleUser, Content: "hi"},
		},
	})
	if err != nil {
		t.Fatalf("buildResponseRequestParams() error = %v", err)
	}

	var payload map[string]any
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("json.Marshal(params) error = %v", err)
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("json.Unmarshal(payload) error = %v", err)
	}

	input, ok := payload["input"].([]any)
	if !ok || len(input) != 1 {
		t.Fatalf("input = %#v, want 1 user item", payload["input"])
	}
	first, _ := input[0].(map[string]any)
	if got, _ := first["role"].(string); got != "user" {
		t.Fatalf("first role = %q, want user", got)
	}
}

func TestBuildResponseRequestParams_OmitsReasoningForNonReasoningModel(t *testing.T) {
	params, err := buildResponseRequestParams(model.ChatRequest{
		Model:    "gpt-4o-mini",
		Messages: []model.Message{{Role: model.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("buildResponseRequestParams() error = %v", err)
	}

	var payload map[string]any
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("json.Marshal(params) error = %v", err)
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("json.Unmarshal(payload) error = %v", err)
	}

	if _, exists := payload["reasoning"]; exists {
		t.Fatalf("reasoning should be omitted for non-reasoning model payload")
	}
}

func TestBuildResponseRequestParams_ReplaysStructuredItemsFromResponseState(t *testing.T) {
	params, err := buildResponseRequestParams(model.ChatRequest{
		Model: "gpt-5.4",
		Messages: []model.Message{{
			Role: model.RoleAssistant,
			ProviderState: &model.ProviderState{
				Provider:   providerName,
				Format:     responseStateFormat,
				Version:    messageVersion,
				ResponseID: "resp_items_1",
				Payload:    json.RawMessage(`{"response_id":"resp_items_1","output":[{"type":"reasoning","id":"rs_1","summary":[{"text":"plan first"}]},{"type":"message","id":"msg_1","status":"completed","content":[{"type":"output_text","text":"hello"}]},{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup_weather","arguments":"{}"}],"items":[{"id":"rs_1","type":"reasoning","encrypted_content":"enc_1"},{"id":"msg_1","type":"message"},{"id":"fc_1","type":"function_call","call_id":"call_1","name":"lookup_weather"}]}`),
			},
		}},
	})
	if err != nil {
		t.Fatalf("buildResponseRequestParams() error = %v", err)
	}

	var payload map[string]any
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("json.Marshal(params) error = %v", err)
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("json.Unmarshal(payload) error = %v", err)
	}

	input, ok := payload["input"].([]any)
	if !ok || len(input) != 3 {
		t.Fatalf("input = %#v, want 3 structured replay items", payload["input"])
	}
	types := make([]string, 0, len(input))
	for _, raw := range input {
		item, _ := raw.(map[string]any)
		types = append(types, item["type"].(string))
		if item["type"] == "item_reference" {
			t.Fatalf("input should not contain item_reference replay, got %#v", item)
		}
	}
	if got := strings.Join(types, ","); got != "reasoning,message,function_call" {
		t.Fatalf("input types = %q, want reasoning,message,function_call", got)
	}
}

func TestBuildResponseRequestParams_ResponseStateToolContinuationDropsNonFunctionItems(t *testing.T) {
	params, err := buildResponseRequestParams(model.ChatRequest{
		Model: "gpt-5.4",
		Messages: []model.Message{
			{Role: model.RoleUser, Content: "list core/rag"},
			{
				Role: model.RoleAssistant,
				ProviderState: &model.ProviderState{
					Provider:   providerName,
					Format:     responseStateFormat,
					Version:    messageVersion,
					ResponseID: "resp_tool_2",
					Payload:    json.RawMessage(`{"response_id":"resp_tool_2","output":[{"type":"reasoning","id":"rs_1","summary":[{"text":"plan"}]},{"type":"message","id":"msg_1","status":"completed","content":[{"type":"output_text","text":"thinking"}]},{"type":"function_call","id":"fc_1","call_id":"call_1","name":"list_files","arguments":"{\"path\":\"core/rag\"}"}],"items":[{"id":"rs_1","type":"reasoning"},{"id":"msg_1","type":"message"},{"id":"fc_1","type":"function_call","call_id":"call_1","name":"list_files"}]}`),
				},
			},
			{Role: model.RoleTool, ToolCallId: "call_1", Content: `{"entries":[]}`},
		},
	})
	if err != nil {
		t.Fatalf("buildResponseRequestParams() error = %v", err)
	}

	var payload map[string]any
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("json.Marshal(params) error = %v", err)
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("json.Unmarshal(payload) error = %v", err)
	}

	input, ok := payload["input"].([]any)
	if !ok || len(input) != 3 {
		t.Fatalf("input = %#v, want user + function_call + function_call_output", payload["input"])
	}
	if second, _ := input[1].(map[string]any); second["type"] != "function_call" {
		t.Fatalf("input[1] = %#v, want function_call", second)
	}
	if third, _ := input[2].(map[string]any); third["type"] != "function_call_output" {
		t.Fatalf("input[2] = %#v, want function_call_output", third)
	}
}

func TestBuildResponseRequestParams_ReplaysRawOutputForToolContinuation(t *testing.T) {
	params, err := buildResponseRequestParams(model.ChatRequest{
		Model: "gpt-5.4",
		Messages: []model.Message{
			{Role: model.RoleUser, Content: "list core/rag"},
			{
				Role:         model.RoleAssistant,
				ProviderData: map[string]any{"type": rawOutputSnapshotType, "response_id": "resp_tool_1", "output_json": `[{"type":"reasoning","id":"rs_1","summary":[{"text":"plan"}]},{"type":"function_call","call_id":"call_1","name":"list_files","arguments":"{\"path\":\"core/rag\"}"}]`},
				ToolCalls:    []types.ToolCall{{ID: "call_1", Name: "list_files", Arguments: `{"path":"core/rag"}`}},
			},
			{Role: model.RoleTool, ToolCallId: "call_1", Content: `{"entries":[{"path":"core/rag/README.md","type":"file"}]}`},
		},
	})
	if err != nil {
		t.Fatalf("buildResponseRequestParams() error = %v", err)
	}

	var payload map[string]any
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("json.Marshal(params) error = %v", err)
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("json.Unmarshal(payload) error = %v", err)
	}

	input, ok := payload["input"].([]any)
	if !ok || len(input) != 3 {
		t.Fatalf("input = %#v, want 3 items", payload["input"])
	}
	if first, _ := input[0].(map[string]any); first["type"] == "reasoning" {
		t.Fatalf("input[0] = %#v, reasoning should be omitted for tool continuation", first)
	}
	if _, exists := payload["tools"]; exists {
		t.Fatalf("tools should be omitted for tool continuation, got %#v", payload["tools"])
	}
}

func TestBuildResponseRequestParams_KeepsToolsForNewUserTurnAfterToolContinuation(t *testing.T) {
	params, err := buildResponseRequestParams(model.ChatRequest{
		Model: "gpt-5.4",
		Messages: []model.Message{
			{Role: model.RoleUser, Content: "read README"},
			{
				Role:         model.RoleAssistant,
				ProviderData: map[string]any{"type": rawOutputSnapshotType, "response_id": "resp_tool_1", "output_json": `[{"type":"reasoning","id":"rs_1","summary":[{"text":"plan"}]},{"type":"function_call","call_id":"call_1","name":"read_file","arguments":"{\"path\":\"README.md\"}"}]`},
				ToolCalls:    []types.ToolCall{{ID: "call_1", Name: "read_file", Arguments: `{"path":"README.md"}`}},
			},
			{Role: model.RoleTool, ToolCallId: "call_1", Content: `{"content":"readme body"}`},
			{Role: model.RoleAssistant, Content: "README summary"},
			{Role: model.RoleUser, Content: "also read AGENTS.md"},
		},
		Tools: []types.Tool{{
			Name:        "read_file",
			Description: "读取文件",
			Parameters: types.JSONSchema{
				Type: "object",
				Properties: map[string]types.SchemaProperty{
					"path": {Type: "string", Description: "文件路径"},
				},
				Required: []string{"path"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("buildResponseRequestParams() error = %v", err)
	}

	var payload map[string]any
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("json.Marshal(params) error = %v", err)
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("json.Unmarshal(payload) error = %v", err)
	}

	tools, ok := payload["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v, want length 1", payload["tools"])
	}
	tool0, _ := tools[0].(map[string]any)
	if got, _ := tool0["name"].(string); got != "read_file" {
		t.Fatalf("tool name = %q, want read_file", got)
	}
}

func TestExtractChatResponse_PopulatesFinalMessageProviderState(t *testing.T) {
	resp := &responses.Response{
		ID: "resp_1",
		Output: []responses.ResponseOutputItemUnion{
			{Type: "reasoning", ID: "rs_1", EncryptedContent: "enc_123", Summary: []responses.ResponseReasoningItemSummary{{Text: "plan first"}}},
			{Type: "message", ID: "msg_1", Content: []responses.ResponseOutputMessageContentUnion{{Type: "output_text", Text: "hello world"}}},
			{Type: "function_call", ID: "fc_1", CallID: "call_1", Name: "lookup_weather", Arguments: responses.ResponseOutputItemUnionArguments{OfString: `{"city":"Beijing"}`}},
		},
		Usage: responses.ResponseUsage{InputTokens: 3, OutputTokens: 4, TotalTokens: 7},
	}

	got, err := extractChatResponse(resp)
	if err != nil {
		t.Fatalf("extractChatResponse() error = %v", err)
	}
	if got.Message.ProviderState == nil {
		t.Fatal("got.Message.ProviderState = nil, want provider state")
	}
	if got.Message.ProviderState.Provider != providerName {
		t.Fatalf("provider = %q, want %q", got.Message.ProviderState.Provider, providerName)
	}
	if got.Content != "hello world" || got.Reasoning != "plan first" {
		t.Fatalf("response = %#v", got)
	}
}
