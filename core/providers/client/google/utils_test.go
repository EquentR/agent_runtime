package google

import (
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
