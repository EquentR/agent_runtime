package openai_official

import (
	"encoding/json"
	"testing"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/types"
	"github.com/openai/openai-go/responses"
)

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

	reasoning, ok := payload["reasoning"].(map[string]any)
	if !ok {
		t.Fatalf("reasoning type = %T, want map[string]any", payload["reasoning"])
	}
	if reasoning["summary"] != "auto" {
		t.Fatalf("reasoning.summary = %v, want auto", reasoning["summary"])
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
			if obj["name"] != "lookup_weather" {
				t.Fatalf("function_call.name = %v, want lookup_weather", obj["name"])
			}
			if obj["call_id"] != "call_1" {
				t.Fatalf("function_call.call_id = %v, want call_1", obj["call_id"])
			}
		case "function_call_output":
			functionOutputCount++
			if obj["call_id"] != "call_1" {
				t.Fatalf("function_call_output.call_id = %v, want call_1", obj["call_id"])
			}
		}
	}
	if functionCallCount != 1 {
		t.Fatalf("function_call item count = %d, want 1", functionCallCount)
	}
	if functionOutputCount != 1 {
		t.Fatalf("function_call_output item count = %d, want 1", functionOutputCount)
	}

	tools, ok := payload["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v, want length 1", payload["tools"])
	}
	tool0 := tools[0].(map[string]any)
	if tool0["type"] != "function" {
		t.Fatalf("tool.type = %v, want function", tool0["type"])
	}
	if tool0["name"] != "lookup_weather" {
		t.Fatalf("tool.name = %v, want lookup_weather", tool0["name"])
	}
	if tool0["strict"] != true {
		t.Fatalf("tool.strict = %v, want true", tool0["strict"])
	}
	paramsObj, ok := tool0["parameters"].(map[string]any)
	if !ok {
		t.Fatalf("tool.parameters type = %T, want map[string]any", tool0["parameters"])
	}
	if paramsObj["additionalProperties"] != false {
		t.Fatalf("tool.parameters.additionalProperties = %v, want false", paramsObj["additionalProperties"])
	}

	toolChoice, ok := payload["tool_choice"].(map[string]any)
	if !ok {
		t.Fatalf("tool_choice type = %T, want map[string]any", payload["tool_choice"])
	}
	if toolChoice["type"] != "function" || toolChoice["name"] != "lookup_weather" {
		t.Fatalf("tool_choice = %#v, want function lookup_weather", toolChoice)
	}
}

func TestBuildResponseRequestParams_ReplaysAssistantReasoningItems(t *testing.T) {
	req := model.ChatRequest{
		Model: "gpt-5.4",
		Messages: []model.Message{{
			Role:      model.RoleAssistant,
			Reasoning: "plan first",
			ReasoningItems: []model.ReasoningItem{{
				ID: "rs_1",
				Summary: []model.ReasoningSummary{{
					Text: "plan first",
				}},
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
	if !ok {
		t.Fatalf("input type = %T, want []any", payload["input"])
	}
	if len(input) != 2 {
		t.Fatalf("len(input) = %d, want 2", len(input))
	}

	reasoning, _ := input[0].(map[string]any)
	if reasoning["type"] != "reasoning" {
		t.Fatalf("reasoning item type = %v, want reasoning", reasoning["type"])
	}
	if reasoning["id"] != "rs_1" {
		t.Fatalf("reasoning item id = %v, want rs_1", reasoning["id"])
	}
	if reasoning["encrypted_content"] != "enc_123" {
		t.Fatalf("reasoning item encrypted_content = %v, want enc_123", reasoning["encrypted_content"])
	}
	summary, ok := reasoning["summary"].([]any)
	if !ok || len(summary) != 1 {
		t.Fatalf("reasoning item summary = %#v, want one summary part", reasoning["summary"])
	}
	summaryPart, _ := summary[0].(map[string]any)
	if summaryPart["text"] != "plan first" {
		t.Fatalf("reasoning summary text = %v, want plan first", summaryPart["text"])
	}
}

func TestBuildResponseRequestParams_ReplaysProviderStateOutputItems(t *testing.T) {
	params, err := buildResponseRequestParams(model.ChatRequest{
		Model: "gpt-5.4",
		Messages: []model.Message{{
			Role: model.RoleAssistant,
			ProviderState: &model.ProviderState{
				Provider:   "openai_responses",
				Format:     "openai_response_output_items.v1",
				Version:    "v1",
				ResponseID: "resp_1",
				Payload: json.RawMessage(`[
					{"type":"reasoning","id":"rs_1","summary":[{"text":"raw plan"}]},
					{"type":"function_call","call_id":"call_1","name":"lookup_weather","arguments":"{}"}
				]`),
			},
			Content:   "normalized text",
			Reasoning: "normalized reasoning",
			ToolCalls: []types.ToolCall{{ID: "call_norm", Name: "normalized_tool", Arguments: `{"city":"Shanghai"}`}},
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
	if !ok {
		t.Fatalf("input type = %T, want []any", payload["input"])
	}
	if len(input) != 2 {
		t.Fatalf("len(input) = %d, want 2", len(input))
	}
	first, _ := input[0].(map[string]any)
	if first["type"] != "reasoning" || first["id"] != "rs_1" {
		t.Fatalf("first replayed item = %#v", first)
	}
	summary, ok := first["summary"].([]any)
	if !ok || len(summary) != 1 {
		t.Fatalf("first replayed summary = %#v", first["summary"])
	}
	summaryPart, _ := summary[0].(map[string]any)
	if summaryPart["text"] != "raw plan" {
		t.Fatalf("summary text = %v, want raw plan", summaryPart["text"])
	}

	second, _ := input[1].(map[string]any)
	if second["type"] != "function_call" || second["call_id"] != "call_1" || second["name"] != "lookup_weather" {
		t.Fatalf("second replayed item = %#v", second)
	}
	if second["arguments"] != "{}" {
		t.Fatalf("second replayed arguments = %v, want {}", second["arguments"])
	}
}

func TestBuildResponseRequestParams_UsesPreviousResponseIDForToolContinuation(t *testing.T) {
	params, err := buildResponseRequestParams(model.ChatRequest{
		Model: "gpt-5.4",
		Messages: []model.Message{
			{Role: model.RoleUser, Content: "list core/rag"},
			{
				Role: model.RoleAssistant,
				ProviderState: &model.ProviderState{
					Provider:   "openai_responses",
					Format:     "openai_response_output_items.v1",
					Version:    "v1",
					ResponseID: "resp_tool_1",
					Payload: json.RawMessage(`[
						{"type":"reasoning","id":"rs_1","summary":[{"text":"plan"}]},
						{"type":"function_call","call_id":"call_1","name":"list_files","arguments":"{\"path\":\"core/rag\"}"}
					]`),
				},
				ToolCalls: []types.ToolCall{{ID: "call_1", Name: "list_files", Arguments: `{"path":"core/rag"}`}},
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

	if got, _ := payload["previous_response_id"].(string); got != "resp_tool_1" {
		t.Fatalf("previous_response_id = %q, want %q", got, "resp_tool_1")
	}
	input, ok := payload["input"].([]any)
	if !ok {
		t.Fatalf("input type = %T, want []any", payload["input"])
	}
	if len(input) != 1 {
		t.Fatalf("len(input) = %d, want 1", len(input))
	}
	item, _ := input[0].(map[string]any)
	if item["type"] != "function_call_output" {
		t.Fatalf("input item = %#v, want function_call_output", item)
	}
	if item["call_id"] != "call_1" {
		t.Fatalf("call_id = %v, want call_1", item["call_id"])
	}
}

func TestBuildResponseRequestParams_ReplaysFullHistoryWhenPreviousResponseIDDisabled(t *testing.T) {
	params, err := buildResponseRequestParamsWithOptions(model.ChatRequest{
		Model: "gpt-5.4",
		Messages: []model.Message{
			{Role: model.RoleUser, Content: "list core/rag"},
			{
				Role: model.RoleAssistant,
				ProviderState: &model.ProviderState{
					Provider:   "openai_responses",
					Format:     "openai_response_output_items.v1",
					Version:    "v1",
					ResponseID: "resp_tool_1",
					Payload: json.RawMessage(`[
						{"type":"reasoning","id":"rs_1","summary":[{"text":"plan"}]},
						{"type":"function_call","call_id":"call_1","name":"list_files","arguments":"{\"path\":\"core/rag\"}"}
					]`),
				},
				ToolCalls: []types.ToolCall{{ID: "call_1", Name: "list_files", Arguments: `{"path":"core/rag"}`}},
			},
			{Role: model.RoleTool, ToolCallId: "call_1", Content: `{"entries":[{"path":"core/rag/README.md","type":"file"}]}`},
		},
	}, requestBuildOptions{SupportsPreviousResponseID: false})
	if err != nil {
		t.Fatalf("buildResponseRequestParamsWithOptions() error = %v", err)
	}

	var payload map[string]any
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("json.Marshal(params) error = %v", err)
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("json.Unmarshal(payload) error = %v", err)
	}

	if _, exists := payload["previous_response_id"]; exists {
		t.Fatalf("previous_response_id should be omitted, got %#v", payload["previous_response_id"])
	}
	input, ok := payload["input"].([]any)
	if !ok {
		t.Fatalf("input type = %T, want []any", payload["input"])
	}
	if len(input) != 4 {
		t.Fatalf("len(input) = %d, want 4", len(input))
	}
	if item, _ := input[2].(map[string]any); item["type"] != "function_call" {
		t.Fatalf("input[2] = %#v, want function_call", item)
	}
	if item, _ := input[3].(map[string]any); item["type"] != "function_call_output" {
		t.Fatalf("input[3] = %#v, want function_call_output", item)
	}
}

func TestBuildResponseRequestParams_ReplaysProviderStateMessageOutputItem(t *testing.T) {
	params, err := buildResponseRequestParams(model.ChatRequest{
		Model: "gpt-5.4",
		Messages: []model.Message{{
			Role: model.RoleAssistant,
			ProviderState: &model.ProviderState{
				Provider: "openai_responses",
				Format:   "openai_response_output_items.v1",
				Version:  "v1",
				Payload: json.RawMessage(`[
					{"type":"message","id":"msg_1","status":"completed","content":[{"type":"output_text","text":"raw text"}]},
					{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup_weather","arguments":"{}"}
				]`),
			},
			Content:   "normalized text",
			Reasoning: "normalized reasoning",
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
	if !ok {
		t.Fatalf("input type = %T, want []any", payload["input"])
	}
	if len(input) != 2 {
		t.Fatalf("len(input) = %d, want 2", len(input))
	}

	message, _ := input[0].(map[string]any)
	if message["type"] != "message" || message["id"] != "msg_1" {
		t.Fatalf("replayed message item = %#v", message)
	}
	content, ok := message["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("replayed message content = %#v", message["content"])
	}
	contentPart, _ := content[0].(map[string]any)
	if contentPart["type"] != "output_text" || contentPart["text"] != "raw text" {
		t.Fatalf("replayed message content part = %#v", contentPart)
	}
}

func TestBuildResponseRequestParams_RejectsUnsupportedProviderStateVersion(t *testing.T) {
	_, err := buildResponseRequestParams(model.ChatRequest{
		Model: "gpt-5.4",
		Messages: []model.Message{{
			Role: model.RoleAssistant,
			ProviderState: &model.ProviderState{
				Provider: "openai_responses",
				Format:   "openai_response_output_items.v1",
				Version:  "v2",
				Payload:  json.RawMessage(`[]`),
			},
		}},
	})
	if err == nil {
		t.Fatal("expected error for unsupported provider state version")
	}
}

func TestModelToolChoiceToResponseVariants(t *testing.T) {
	tests := []struct {
		name      string
		choice    types.ToolChoice
		wantType  string
		wantValue string
	}{
		{name: "auto", choice: types.ToolChoice{Type: types.ToolAuto}, wantType: "string", wantValue: "auto"},
		{name: "none", choice: types.ToolChoice{Type: types.ToolNone}, wantType: "string", wantValue: "none"},
		{name: "force required", choice: types.ToolChoice{Type: types.ToolForce}, wantType: "string", wantValue: "required"},
		{name: "force named", choice: types.ToolChoice{Type: types.ToolForce, Name: "lookup_weather"}, wantType: "map", wantValue: "lookup_weather"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u, err := modelToolChoiceToResponse(tc.choice)
			if err != nil {
				t.Fatalf("modelToolChoiceToResponse() error = %v", err)
			}

			encoded, err := json.Marshal(u)
			if err != nil {
				t.Fatalf("json.Marshal(tool_choice) error = %v", err)
			}

			if tc.wantType == "string" {
				var got string
				if err := json.Unmarshal(encoded, &got); err != nil {
					t.Fatalf("json.Unmarshal string error = %v, raw=%s", err, string(encoded))
				}
				if got != tc.wantValue {
					t.Fatalf("tool choice = %q, want %q", got, tc.wantValue)
				}
				return
			}

			var got map[string]any
			if err := json.Unmarshal(encoded, &got); err != nil {
				t.Fatalf("json.Unmarshal map error = %v, raw=%s", err, string(encoded))
			}
			if got["type"] != "function" || got["name"] != tc.wantValue {
				t.Fatalf("tool choice map = %#v, want function name=%s", got, tc.wantValue)
			}
		})
	}
}

func TestBuildResponseRequestParams_OptionalToolUsesNonStrictSchema(t *testing.T) {
	req := model.ChatRequest{
		Model: "gpt-5.4",
		Messages: []model.Message{{
			Role:    model.RoleUser,
			Content: "Say hello",
		}},
		Tools: []types.Tool{{
			Name:        "hello_world",
			Description: "Say hello to someone",
			Parameters: types.JSONSchema{
				Type: "object",
				Properties: map[string]types.SchemaProperty{
					"name":  {Type: "string", Description: "Name to greet"},
					"title": {Type: "string", Description: "Optional title"},
				},
				Required: []string{"name"},
			},
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

	tools, ok := payload["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v, want length 1", payload["tools"])
	}
	tool0 := tools[0].(map[string]any)
	if tool0["strict"] != false {
		t.Fatalf("tool.strict = %v, want false", tool0["strict"])
	}
	paramsObj, ok := tool0["parameters"].(map[string]any)
	if !ok {
		t.Fatalf("tool.parameters type = %T, want map[string]any", tool0["parameters"])
	}
	required, ok := paramsObj["required"].([]any)
	if !ok || len(required) != 1 || required[0] != "name" {
		t.Fatalf("tool.parameters.required = %#v, want [name]", paramsObj["required"])
	}
}

func TestBuildResponseRequestParams_ArrayToolParameterIncludesItems(t *testing.T) {
	req := model.ChatRequest{
		Model: "gpt-5.4",
		Messages: []model.Message{{
			Role:    model.RoleUser,
			Content: "Check command version",
		}},
		Tools: []types.Tool{{
			Name:        "check_command",
			Description: "Check command existence",
			Parameters: types.JSONSchema{
				Type: "object",
				Properties: map[string]types.SchemaProperty{
					"name": {Type: "string"},
					"version_args": {
						Type:  "array",
						Items: &types.SchemaProperty{Type: "string"},
					},
				},
				Required: []string{"name"},
			},
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

	tools, ok := payload["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v, want length 1", payload["tools"])
	}
	tool0 := tools[0].(map[string]any)
	paramsObj, ok := tool0["parameters"].(map[string]any)
	if !ok {
		t.Fatalf("tool.parameters type = %T, want map[string]any", tool0["parameters"])
	}
	properties, ok := paramsObj["properties"].(map[string]any)
	if !ok {
		t.Fatalf("tool.parameters.properties type = %T, want map[string]any", paramsObj["properties"])
	}
	versionArgs, ok := properties["version_args"].(map[string]any)
	if !ok {
		t.Fatalf("version_args schema type = %T, want map[string]any", properties["version_args"])
	}
	items, ok := versionArgs["items"].(map[string]any)
	if !ok {
		t.Fatalf("version_args.items type = %T, want map[string]any", versionArgs["items"])
	}
	if items["type"] != "string" {
		t.Fatalf("version_args.items.type = %v, want string", items["type"])
	}
}

func TestBuildResponseRequestParams_NoArgToolNormalizesEmptySchema(t *testing.T) {
	req := model.ChatRequest{
		Model: "gpt-5.4",
		Messages: []model.Message{{
			Role:    model.RoleUser,
			Content: "Generate a UUID",
		}},
		Tools: []types.Tool{{
			Name:        "generate_uuid",
			Description: "Generate a new UUID",
			Parameters: types.JSONSchema{
				Type: "object",
			},
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

	tools, ok := payload["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v, want length 1", payload["tools"])
	}
	tool0 := tools[0].(map[string]any)
	if tool0["strict"] != true {
		t.Fatalf("tool.strict = %v, want true", tool0["strict"])
	}
	paramsObj, ok := tool0["parameters"].(map[string]any)
	if !ok {
		t.Fatalf("tool.parameters type = %T, want map[string]any", tool0["parameters"])
	}
	properties, ok := paramsObj["properties"].(map[string]any)
	if !ok || len(properties) != 0 {
		t.Fatalf("tool.parameters.properties = %#v, want empty map", paramsObj["properties"])
	}
	required, ok := paramsObj["required"].([]any)
	if !ok || len(required) != 0 {
		t.Fatalf("tool.parameters.required = %#v, want empty array", paramsObj["required"])
	}
}

func TestExtractChatResponse_WithTextToolCallsAndUsage(t *testing.T) {
	resp := &responses.Response{
		Output: []responses.ResponseOutputItemUnion{
			{
				Type: "message",
				Content: []responses.ResponseOutputMessageContentUnion{
					{Type: "output_text", Text: "hello "},
					{Type: "output_text", Text: "world"},
				},
			},
			{
				Type:      "function_call",
				CallID:    "call_1",
				Name:      "lookup_weather",
				Arguments: `{"city":"Beijing"}`,
			},
		},
		Usage: responses.ResponseUsage{
			InputTokens: 11,
			InputTokensDetails: responses.ResponseUsageInputTokensDetails{
				CachedTokens: 5,
			},
			OutputTokens: 7,
			TotalTokens:  18,
		},
	}

	got, err := extractChatResponse(resp)
	if err != nil {
		t.Fatalf("extractChatResponse() error = %v", err)
	}

	if got.Content != "hello world" {
		t.Fatalf("content = %q, want %q", got.Content, "hello world")
	}
	if len(got.ToolCalls) != 1 {
		t.Fatalf("len(tool calls) = %d, want 1", len(got.ToolCalls))
	}
	if got.ToolCalls[0].ID != "call_1" || got.ToolCalls[0].Name != "lookup_weather" {
		t.Fatalf("tool call = %#v, want id=call_1 name=lookup_weather", got.ToolCalls[0])
	}
	if got.Usage.PromptTokens != 11 || got.Usage.CompletionTokens != 7 || got.Usage.TotalTokens != 18 {
		t.Fatalf("usage = %#v, want {11,7,18}", got.Usage)
	}
	if got.Usage.CachedPromptTokens != 5 {
		t.Fatalf("cached prompt tokens = %d, want 5", got.Usage.CachedPromptTokens)
	}
}

func TestExtractChatResponse_CollectsReasoningAndStripsLeadingThinkBlock(t *testing.T) {
	resp := &responses.Response{
		Output: []responses.ResponseOutputItemUnion{
			{
				Type: "reasoning",
				Summary: []responses.ResponseReasoningItemSummary{{
					Text: "plan first",
				}},
			},
			{
				Type:    "message",
				Content: []responses.ResponseOutputMessageContentUnion{{Type: "output_text", Text: "<think>shadow</think>Final answer"}},
			},
		},
	}

	got, err := extractChatResponse(resp)
	if err != nil {
		t.Fatalf("extractChatResponse() error = %v", err)
	}
	if got.Reasoning != "plan first" {
		t.Fatalf("reasoning = %q, want %q", got.Reasoning, "plan first")
	}
	if got.Content != "Final answer" {
		t.Fatalf("content = %q, want %q", got.Content, "Final answer")
	}
}

func TestExtractChatResponse_PreservesReasoningItems(t *testing.T) {
	resp := &responses.Response{
		Output: []responses.ResponseOutputItemUnion{
			{
				Type:             "reasoning",
				ID:               "rs_1",
				EncryptedContent: "enc_123",
				Summary: []responses.ResponseReasoningItemSummary{{
					Text: "plan first",
				}},
			},
			{
				Type:      "function_call",
				CallID:    "call_1",
				Name:      "lookup_weather",
				Arguments: `{"city":"Beijing"}`,
			},
		},
	}

	got, err := extractChatResponse(resp)
	if err != nil {
		t.Fatalf("extractChatResponse() error = %v", err)
	}
	if len(got.ReasoningItems) != 1 {
		t.Fatalf("len(reasoning items) = %d, want 1", len(got.ReasoningItems))
	}
	if got.ReasoningItems[0].ID != "rs_1" {
		t.Fatalf("reasoning item id = %q, want rs_1", got.ReasoningItems[0].ID)
	}
	if got.ReasoningItems[0].EncryptedContent != "enc_123" {
		t.Fatalf("reasoning item encrypted content = %q, want enc_123", got.ReasoningItems[0].EncryptedContent)
	}
	if len(got.ReasoningItems[0].Summary) != 1 || got.ReasoningItems[0].Summary[0].Text != "plan first" {
		t.Fatalf("reasoning item summary = %#v, want [plan first]", got.ReasoningItems[0].Summary)
	}
}

func TestExtractChatResponse_PopulatesFinalMessageProviderState(t *testing.T) {
	resp := &responses.Response{
		Output: []responses.ResponseOutputItemUnion{
			{
				Type:             "reasoning",
				ID:               "rs_1",
				EncryptedContent: "enc_123",
				Summary: []responses.ResponseReasoningItemSummary{{
					Text: "plan first",
				}},
			},
			{
				Type:    "message",
				ID:      "msg_1",
				Content: []responses.ResponseOutputMessageContentUnion{{Type: "output_text", Text: "hello world"}},
			},
			{
				Type:      "function_call",
				ID:        "fc_1",
				CallID:    "call_1",
				Name:      "lookup_weather",
				Arguments: `{"city":"Beijing"}`,
			},
		},
		Usage: responses.ResponseUsage{InputTokens: 3, OutputTokens: 4, TotalTokens: 7},
	}

	got, err := extractChatResponse(resp)
	if err != nil {
		t.Fatalf("extractChatResponse() error = %v", err)
	}
	if got.Message.Role != model.RoleAssistant {
		t.Fatalf("got.Message.Role = %q, want %q", got.Message.Role, model.RoleAssistant)
	}
	if got.Content != "hello world" || got.Message.Content != "hello world" {
		t.Fatalf("response content/message = %#v", got)
	}
	if got.Reasoning != "plan first" || got.Message.Reasoning != "plan first" {
		t.Fatalf("response reasoning/message = %#v", got)
	}
	if len(got.ToolCalls) != 1 || len(got.Message.ToolCalls) != 1 {
		t.Fatalf("response tool calls = %#v", got)
	}
	if got.Message.ProviderState == nil {
		t.Fatal("got.Message.ProviderState = nil, want provider state")
	}
	if got.Message.ProviderState.Provider != "openai_responses" {
		t.Fatalf("provider = %q, want %q", got.Message.ProviderState.Provider, "openai_responses")
	}
	if got.Message.ProviderState.Format != "openai_response_output_items.v1" {
		t.Fatalf("format = %q, want %q", got.Message.ProviderState.Format, "openai_response_output_items.v1")
	}
	if got.Message.ProviderState.Version != "v1" {
		t.Fatalf("version = %q, want %q", got.Message.ProviderState.Version, "v1")
	}

	var replayed []responses.ResponseOutputItemUnion
	if err := json.Unmarshal(got.Message.ProviderState.Payload, &replayed); err != nil {
		t.Fatalf("unmarshal provider state payload: %v", err)
	}
	if len(replayed) != 3 {
		t.Fatalf("len(replayed) = %d, want 3", len(replayed))
	}
	if replayed[0].Type != "reasoning" || replayed[1].Type != "message" || replayed[2].Type != "function_call" {
		t.Fatalf("replayed output sequence = %#v", replayed)
	}
}
