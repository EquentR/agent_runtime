package google

import (
	"encoding/json"
	"testing"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/types"
	genai "google.golang.org/genai"
)

func TestBuildGenerateContentRequest_WithToolsAndToolChoice(t *testing.T) {
	req := model.ChatRequest{
		Model: "gemini-2.5-flash",
		Messages: []model.Message{{
			Role:    model.RoleUser,
			Content: "查一下北京天气",
		}},
		MaxTokens: 256,
		Tools: []types.Tool{{
			Name:        "lookup_weather",
			Description: "查询天气",
			Parameters: types.JSONSchema{
				Type: "object",
				Properties: map[string]types.SchemaProperty{
					"city": {Type: "string", Description: "城市"},
				},
				Required: []string{"city"},
			},
		}},
		ToolChoice: types.ToolChoice{Type: types.ToolForce, Name: "lookup_weather"},
	}

	_, cfg, promptMessages, err := buildGenerateContentRequest(req)
	if err != nil {
		t.Fatalf("buildGenerateContentRequest() error = %v", err)
	}

	if cfg.MaxOutputTokens != 256 {
		t.Fatalf("cfg.MaxOutputTokens = %d, want 256", cfg.MaxOutputTokens)
	}
	if len(cfg.Tools) != 1 {
		t.Fatalf("len(cfg.Tools) = %d, want 1", len(cfg.Tools))
	}
	if len(cfg.Tools[0].FunctionDeclarations) != 1 {
		t.Fatalf("len(cfg.Tools[0].FunctionDeclarations) = %d, want 1", len(cfg.Tools[0].FunctionDeclarations))
	}
	if cfg.Tools[0].FunctionDeclarations[0].Name != "lookup_weather" {
		t.Fatalf("function name = %q, want %q", cfg.Tools[0].FunctionDeclarations[0].Name, "lookup_weather")
	}
	if cfg.ToolConfig == nil || cfg.ToolConfig.FunctionCallingConfig == nil {
		t.Fatalf("cfg.ToolConfig.FunctionCallingConfig should not be nil")
	}
	if cfg.ToolConfig.FunctionCallingConfig.Mode != genai.FunctionCallingConfigModeAny {
		t.Fatalf("function calling mode = %q, want %q", cfg.ToolConfig.FunctionCallingConfig.Mode, genai.FunctionCallingConfigModeAny)
	}
	if len(cfg.ToolConfig.FunctionCallingConfig.AllowedFunctionNames) != 1 || cfg.ToolConfig.FunctionCallingConfig.AllowedFunctionNames[0] != "lookup_weather" {
		t.Fatalf("allowed function names = %#v, want [lookup_weather]", cfg.ToolConfig.FunctionCallingConfig.AllowedFunctionNames)
	}
	if len(promptMessages) != 1 || promptMessages[0] != "查一下北京天气" {
		t.Fatalf("promptMessages = %#v, want [查一下北京天气]", promptMessages)
	}
}

func TestBuildGenAIMessages_WithAssistantToolCallsAndToolResponse(t *testing.T) {
	msgs, _, promptMessages, err := buildGenAIMessages([]model.Message{
		{Role: model.RoleUser, Content: "上海天气怎么样"},
		{
			Role: model.RoleAssistant,
			ToolCalls: []types.ToolCall{{
				ID:               "call_1",
				Name:             "lookup_weather",
				Arguments:        `{"city":"Shanghai"}`,
				ThoughtSignature: []byte{1, 2, 3},
			}},
		},
		{Role: model.RoleTool, ToolCallId: "call_1", Content: `{"temp":23}`},
	})
	if err != nil {
		t.Fatalf("buildGenAIMessages() error = %v", err)
	}

	if len(msgs) != 3 {
		t.Fatalf("len(msgs) = %d, want 3", len(msgs))
	}
	if msgs[1].Role != genai.RoleModel {
		t.Fatalf("assistant role mapped to %q, want %q", msgs[1].Role, genai.RoleModel)
	}
	if len(msgs[1].Parts) != 1 || msgs[1].Parts[0].FunctionCall == nil {
		t.Fatalf("assistant message should contain function call part")
	}
	if string(msgs[1].Parts[0].ThoughtSignature) != string([]byte{1, 2, 3}) {
		t.Fatalf("thought signature not preserved")
	}
	if msgs[1].Parts[0].FunctionCall.ID != "call_1" {
		t.Fatalf("function call id = %q, want %q", msgs[1].Parts[0].FunctionCall.ID, "call_1")
	}
	if msgs[2].Role != genai.RoleUser {
		t.Fatalf("tool role mapped to %q, want %q", msgs[2].Role, genai.RoleUser)
	}
	if len(msgs[2].Parts) != 1 || msgs[2].Parts[0].FunctionResponse == nil {
		t.Fatalf("tool message should contain function response part")
	}
	if msgs[2].Parts[0].FunctionResponse.Name != "lookup_weather" {
		t.Fatalf("function response name = %q, want %q", msgs[2].Parts[0].FunctionResponse.Name, "lookup_weather")
	}
	if len(promptMessages) != 3 {
		t.Fatalf("len(promptMessages) = %d, want 3", len(promptMessages))
	}
}

func TestBuildGenAIMessages_ReplaysProviderStateParts(t *testing.T) {
	msg := model.Message{
		Role: model.RoleAssistant,
		ProviderState: &model.ProviderState{
			Provider: "google_genai",
			Format:   "google_genai_content.v1",
			Version:  "v1",
			Payload:  json.RawMessage(`{"role":"model","parts":[{"text":"hello"},{"text":"plan","thought":true,"thoughtSignature":"AQID"},{"functionCall":{"id":"call_1","name":"lookup_weather","args":{"city":"Beijing"}},"thoughtSignature":"BAUG"}]}`),
		},
		Content:   "normalized text should be ignored",
		Reasoning: "normalized reasoning should be ignored",
		ToolCalls: []types.ToolCall{{ID: "ignored", Name: "ignored", Arguments: `{}`}},
	}

	contents, _, promptMessages, err := buildGenAIMessages([]model.Message{msg})
	if err != nil {
		t.Fatalf("buildGenAIMessages() error = %v", err)
	}
	if len(contents) != 1 {
		t.Fatalf("len(contents) = %d, want 1", len(contents))
	}
	if contents[0].Role != genai.RoleModel {
		t.Fatalf("contents[0].Role = %q, want %q", contents[0].Role, genai.RoleModel)
	}
	if len(contents[0].Parts) != 3 {
		t.Fatalf("len(contents[0].Parts) = %d, want 3", len(contents[0].Parts))
	}
	if contents[0].Parts[0].Text != "hello" {
		t.Fatalf("contents[0].Parts[0].Text = %q, want hello", contents[0].Parts[0].Text)
	}
	if !contents[0].Parts[1].Thought || contents[0].Parts[1].Text != "plan" {
		t.Fatalf("thought part = %#v", contents[0].Parts[1])
	}
	if string(contents[0].Parts[1].ThoughtSignature) != string([]byte{1, 2, 3}) {
		t.Fatalf("thought signature = %v, want %v", contents[0].Parts[1].ThoughtSignature, []byte{1, 2, 3})
	}
	if contents[0].Parts[2].FunctionCall == nil {
		t.Fatalf("function call part = %#v, want non-nil FunctionCall", contents[0].Parts[2])
	}
	if contents[0].Parts[2].FunctionCall.Name != "lookup_weather" {
		t.Fatalf("function name = %q, want lookup_weather", contents[0].Parts[2].FunctionCall.Name)
	}
	if string(contents[0].Parts[2].ThoughtSignature) != string([]byte{4, 5, 6}) {
		t.Fatalf("function thought signature = %v, want %v", contents[0].Parts[2].ThoughtSignature, []byte{4, 5, 6})
	}
	if len(promptMessages) != 1 || promptMessages[0] != "hello\nlookup_weather({\"city\":\"Beijing\"})" {
		t.Fatalf("promptMessages = %#v", promptMessages)
	}
	if contents[0].Parts[2].FunctionCall.ID != "call_1" {
		t.Fatalf("function call id = %q, want call_1", contents[0].Parts[2].FunctionCall.ID)
	}
	if got := contents[0].Parts[2].FunctionCall.Args["city"]; got != "Beijing" {
		t.Fatalf("function args city = %#v, want Beijing", got)
	}
	if contents[0].Parts[0].Text == msg.Content {
		t.Fatal("provider replay did not override normalized assistant content")
	}
}

func TestExtractContentAndToolCalls_PreservesThoughtSignature(t *testing.T) {
	_, _, toolCalls, err := extractContentAndToolCalls(&genai.Content{
		Role: genai.RoleModel,
		Parts: []*genai.Part{{
			FunctionCall:     &genai.FunctionCall{ID: "call_1", Name: "lookup_weather", Args: map[string]any{"city": "Beijing"}},
			ThoughtSignature: []byte{9, 8, 7},
		}},
	})
	if err != nil {
		t.Fatalf("extractContentAndToolCalls() error = %v", err)
	}
	if len(toolCalls) != 1 {
		t.Fatalf("len(toolCalls) = %d, want 1", len(toolCalls))
	}
	if string(toolCalls[0].ThoughtSignature) != string([]byte{9, 8, 7}) {
		t.Fatalf("thought signature not preserved in tool call")
	}
}

func TestExtractChatResponse_WithToolCalls(t *testing.T) {
	resp, err := extractChatResponse(&genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{{
			Content: &genai.Content{
				Role: genai.RoleModel,
				Parts: []*genai.Part{
					{FunctionCall: &genai.FunctionCall{ID: "call_1", Name: "lookup_weather", Args: map[string]any{"city": "Beijing"}}},
					{Text: ""},
				},
			},
		}},
		UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:        10,
			CachedContentTokenCount: 3,
			CandidatesTokenCount:    6,
			ToolUsePromptTokenCount: 2,
			ThoughtsTokenCount:      4,
			TotalTokenCount:         22,
		},
	})
	if err != nil {
		t.Fatalf("extractChatResponse() error = %v", err)
	}

	if len(resp.ToolCalls) != 1 {
		t.Fatalf("len(resp.ToolCalls) = %d, want 1", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "lookup_weather" {
		t.Fatalf("tool call name = %q, want %q", resp.ToolCalls[0].Name, "lookup_weather")
	}
	if resp.ToolCalls[0].Arguments != `{"city":"Beijing"}` {
		t.Fatalf("tool call arguments = %q, want %q", resp.ToolCalls[0].Arguments, `{"city":"Beijing"}`)
	}
	if resp.Usage.PromptTokens != 12 {
		t.Fatalf("resp.Usage.PromptTokens = %d, want 12", resp.Usage.PromptTokens)
	}
	if resp.Usage.CompletionTokens != 10 {
		t.Fatalf("resp.Usage.CompletionTokens = %d, want 10", resp.Usage.CompletionTokens)
	}
	if resp.Usage.TotalTokens != 22 {
		t.Fatalf("resp.Usage.TotalTokens = %d, want 22", resp.Usage.TotalTokens)
	}
	if resp.Usage.CachedPromptTokens != 3 {
		t.Fatalf("resp.Usage.CachedPromptTokens = %d, want 3", resp.Usage.CachedPromptTokens)
	}
}

func TestExtractChatResponse_CollectsThoughtPartsAsReasoning(t *testing.T) {
	resp, err := extractChatResponse(&genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{{
			Content: &genai.Content{
				Role: genai.RoleModel,
				Parts: []*genai.Part{
					{Text: "plan first", Thought: true},
					{Text: "Final answer"},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("extractChatResponse() error = %v", err)
	}
	if resp.Reasoning != "plan first" {
		t.Fatalf("reasoning = %q, want %q", resp.Reasoning, "plan first")
	}
	if resp.Content != "Final answer" {
		t.Fatalf("content = %q, want %q", resp.Content, "Final answer")
	}
}

func TestExtractChatResponse_StripsLeadingThinkBlockWhenThoughtMissing(t *testing.T) {
	resp, err := extractChatResponse(&genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{{
			Content: &genai.Content{
				Role:  genai.RoleModel,
				Parts: []*genai.Part{{Text: "<think>plan first</think>Final answer"}},
			},
		}},
	})
	if err != nil {
		t.Fatalf("extractChatResponse() error = %v", err)
	}
	if resp.Reasoning != "plan first" {
		t.Fatalf("reasoning = %q, want %q", resp.Reasoning, "plan first")
	}
	if resp.Content != "Final answer" {
		t.Fatalf("content = %q, want %q", resp.Content, "Final answer")
	}
}

func TestExtractChatResponse_PopulatesProviderState(t *testing.T) {
	resp, err := extractChatResponse(&genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{{
			Content: &genai.Content{
				Role: genai.RoleModel,
				Parts: []*genai.Part{
					{Text: "plan", Thought: true, ThoughtSignature: []byte{1, 2, 3}},
					{FunctionCall: &genai.FunctionCall{ID: "call_1", Name: "lookup_weather", Args: map[string]any{"city": "Beijing"}}, ThoughtSignature: []byte{4, 5, 6}},
					{Text: "Final answer"},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("extractChatResponse() error = %v", err)
	}

	if resp.Message.Role != model.RoleAssistant {
		t.Fatalf("resp.Message.Role = %q, want %q", resp.Message.Role, model.RoleAssistant)
	}
	if resp.Message.Content != "Final answer" || resp.Content != "Final answer" {
		t.Fatalf("content/message = %#v", resp)
	}
	if resp.Message.Reasoning != "plan" || resp.Reasoning != "plan" {
		t.Fatalf("reasoning/message = %#v", resp)
	}
	if len(resp.Message.ToolCalls) != 1 || len(resp.ToolCalls) != 1 {
		t.Fatalf("tool calls = %#v", resp)
	}
	if resp.Message.ProviderState == nil {
		t.Fatal("resp.Message.ProviderState = nil, want provider state")
	}
	if resp.Message.ProviderState.Provider != "google_genai" {
		t.Fatalf("provider = %q, want google_genai", resp.Message.ProviderState.Provider)
	}
	if resp.Message.ProviderState.Format != "google_genai_content.v1" {
		t.Fatalf("format = %q, want google_genai_content.v1", resp.Message.ProviderState.Format)
	}
	if resp.Message.ProviderState.Version != "v1" {
		t.Fatalf("version = %q, want v1", resp.Message.ProviderState.Version)
	}

	replayed, ok, err := contentFromProviderState(resp.Message.ProviderState)
	if err != nil {
		t.Fatalf("contentFromProviderState() error = %v", err)
	}
	if !ok {
		t.Fatal("contentFromProviderState() ok = false, want true")
	}
	if replayed.Role != genai.RoleModel {
		t.Fatalf("replayed.Role = %q, want %q", replayed.Role, genai.RoleModel)
	}
	if len(replayed.Parts) != 3 {
		t.Fatalf("len(replayed.Parts) = %d, want 3", len(replayed.Parts))
	}
	if !replayed.Parts[0].Thought || replayed.Parts[0].Text != "plan" {
		t.Fatalf("replayed thought part = %#v", replayed.Parts[0])
	}
	if replayed.Parts[1].FunctionCall == nil || replayed.Parts[1].FunctionCall.Name != "lookup_weather" {
		t.Fatalf("replayed function call part = %#v", replayed.Parts[1])
	}
	if string(replayed.Parts[1].ThoughtSignature) != string([]byte{4, 5, 6}) {
		t.Fatalf("replayed function signature = %v, want %v", replayed.Parts[1].ThoughtSignature, []byte{4, 5, 6})
	}
	if replayed.Parts[2].Text != "Final answer" {
		t.Fatalf("replayed final text = %q, want Final answer", replayed.Parts[2].Text)
	}
}
