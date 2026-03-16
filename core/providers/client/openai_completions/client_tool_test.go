package openai

import (
	"encoding/json"
	"testing"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/types"
	goopenai "github.com/sashabaranov/go-openai"
)

func TestBuildChatCompletionRequest_WithTools(t *testing.T) {
	req := model.ChatRequest{
		Model: "gpt-4o-mini",
		Messages: []model.Message{{
			Role:    model.RoleUser,
			Content: "查下北京天气",
		}},
		MaxTokens: 256,
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

	oaiReq, err := buildChatCompletionRequest(req)
	if err != nil {
		t.Fatalf("buildChatCompletionRequest() error = %v", err)
	}

	if len(oaiReq.Tools) != 1 {
		t.Fatalf("len(oaiReq.Tools) = %d, want 1", len(oaiReq.Tools))
	}
	if oaiReq.Tools[0].Type != goopenai.ToolTypeFunction {
		t.Fatalf("oaiReq.Tools[0].Type = %q, want %q", oaiReq.Tools[0].Type, goopenai.ToolTypeFunction)
	}

	choice, ok := oaiReq.ToolChoice.(goopenai.ToolChoice)
	if !ok {
		t.Fatalf("oaiReq.ToolChoice type = %T, want goopenai.ToolChoice", oaiReq.ToolChoice)
	}
	if choice.Function.Name != "lookup_weather" {
		t.Fatalf("choice.Function.Name = %q, want %q", choice.Function.Name, "lookup_weather")
	}
}

func TestExtractChatResponse_WithToolCalls(t *testing.T) {
	oaiResp := goopenai.ChatCompletionResponse{
		Choices: []goopenai.ChatCompletionChoice{{
			Message: goopenai.ChatCompletionMessage{
				Content: "",
				ToolCalls: []goopenai.ToolCall{{
					ID:   "call_1",
					Type: goopenai.ToolTypeFunction,
					Function: goopenai.FunctionCall{
						Name:      "lookup_weather",
						Arguments: `{"city":"Beijing"}`,
					},
				}},
			},
		}},
		Usage: goopenai.Usage{
			PromptTokens:     12,
			CompletionTokens: 8,
			TotalTokens:      20,
			PromptTokensDetails: &goopenai.PromptTokensDetails{
				CachedTokens: 4,
			},
		},
	}

	resp, err := extractChatResponse(oaiResp)
	if err != nil {
		t.Fatalf("extractChatResponse() error = %v", err)
	}

	if len(resp.ToolCalls) != 1 {
		t.Fatalf("len(resp.ToolCalls) = %d, want 1", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "lookup_weather" {
		t.Fatalf("resp.ToolCalls[0].Name = %q, want %q", resp.ToolCalls[0].Name, "lookup_weather")
	}
	if resp.ToolCalls[0].Arguments != `{"city":"Beijing"}` {
		t.Fatalf("resp.ToolCalls[0].Arguments = %q, want %q", resp.ToolCalls[0].Arguments, `{"city":"Beijing"}`)
	}
	if resp.Usage.TotalTokens != 20 {
		t.Fatalf("resp.Usage.TotalTokens = %d, want 20", resp.Usage.TotalTokens)
	}
	if resp.Usage.CachedPromptTokens != 4 {
		t.Fatalf("resp.Usage.CachedPromptTokens = %d, want 4", resp.Usage.CachedPromptTokens)
	}
}

func TestExtractChatResponse_StripsLeadingThinkBlockIntoReasoning(t *testing.T) {
	oaiResp := goopenai.ChatCompletionResponse{
		Choices: []goopenai.ChatCompletionChoice{{
			Message: goopenai.ChatCompletionMessage{
				Content: "<think>plan first</think>Final answer",
			},
		}},
	}

	resp, err := extractChatResponse(oaiResp)
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

func TestExtractChatResponse_PopulatesMessageProviderState(t *testing.T) {
	oaiResp := goopenai.ChatCompletionResponse{
		Choices: []goopenai.ChatCompletionChoice{{
			Message: goopenai.ChatCompletionMessage{
				Role:             model.RoleAssistant,
				Content:          "Final answer",
				ReasoningContent: "plan first",
				ToolCalls: []goopenai.ToolCall{{
					ID:   "call_1",
					Type: goopenai.ToolTypeFunction,
					Function: goopenai.FunctionCall{
						Name:      "lookup_weather",
						Arguments: `{"city":"Beijing"}`,
					},
				}},
			},
		}},
	}

	resp, err := extractChatResponse(oaiResp)
	if err != nil {
		t.Fatalf("extractChatResponse() error = %v", err)
	}
	if resp.Message.Role != model.RoleAssistant {
		t.Fatalf("resp.Message.Role = %q, want %q", resp.Message.Role, model.RoleAssistant)
	}
	if resp.Message.Content != "Final answer" || resp.Content != "Final answer" {
		t.Fatalf("response content/message = %#v", resp)
	}
	if resp.Message.Reasoning != "plan first" || resp.Reasoning != "plan first" {
		t.Fatalf("response reasoning/message = %#v", resp)
	}
	if len(resp.Message.ToolCalls) != 1 || len(resp.ToolCalls) != 1 {
		t.Fatalf("response tool calls = %#v", resp)
	}
	if resp.Message.ProviderState == nil {
		t.Fatal("resp.Message.ProviderState = nil, want provider state")
	}
	if resp.Message.ProviderState.Provider != "openai_completions" {
		t.Fatalf("resp.Message.ProviderState.Provider = %q, want %q", resp.Message.ProviderState.Provider, "openai_completions")
	}
	if resp.Message.ProviderState.Format != "openai_chat_message.v1" {
		t.Fatalf("resp.Message.ProviderState.Format = %q, want %q", resp.Message.ProviderState.Format, "openai_chat_message.v1")
	}
	if resp.Message.ProviderState.Version != "v1" {
		t.Fatalf("resp.Message.ProviderState.Version = %q, want %q", resp.Message.ProviderState.Version, "v1")
	}
	var replayed goopenai.ChatCompletionMessage
	if err := json.Unmarshal(resp.Message.ProviderState.Payload, &replayed); err != nil {
		t.Fatalf("unmarshal provider state payload: %v", err)
	}
	if replayed.Content != "Final answer" || replayed.ReasoningContent != "plan first" {
		t.Fatalf("replayed payload = %#v", replayed)
	}
}

func TestExtractChatResponse_FallsBackToLegacyFunctionCall(t *testing.T) {
	oaiResp := goopenai.ChatCompletionResponse{
		Choices: []goopenai.ChatCompletionChoice{{
			Message: goopenai.ChatCompletionMessage{
				Role:    model.RoleAssistant,
				Content: "",
				FunctionCall: &goopenai.FunctionCall{
					Name:      "lookup_weather",
					Arguments: `{"city":"Beijing"}`,
				},
			},
		}},
	}

	resp, err := extractChatResponse(oaiResp)
	if err != nil {
		t.Fatalf("extractChatResponse() error = %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("len(resp.ToolCalls) = %d, want 1", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "lookup_weather" || resp.ToolCalls[0].Arguments != `{"city":"Beijing"}` {
		t.Fatalf("resp.ToolCalls[0] = %#v", resp.ToolCalls[0])
	}
	if len(resp.Message.ToolCalls) != 1 {
		t.Fatalf("len(resp.Message.ToolCalls) = %d, want 1", len(resp.Message.ToolCalls))
	}
	var replayed goopenai.ChatCompletionMessage
	if err := json.Unmarshal(resp.Message.ProviderState.Payload, &replayed); err != nil {
		t.Fatalf("unmarshal provider state payload: %v", err)
	}
	if replayed.FunctionCall == nil || replayed.FunctionCall.Name != "lookup_weather" {
		t.Fatalf("replayed.FunctionCall = %#v", replayed.FunctionCall)
	}
}

func TestBuildChatCompletionStreamRequest_WithTools(t *testing.T) {
	req := model.ChatRequest{
		Model: "gpt-4o-mini",
		Messages: []model.Message{{
			Role:    model.RoleUser,
			Content: "查下北京天气",
		}},
		MaxTokens: 256,
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

	oaiReq, _, err := buildChatCompletionStreamRequest(req)
	if err != nil {
		t.Fatalf("buildChatCompletionStreamRequest() error = %v", err)
	}

	if !oaiReq.Stream {
		t.Fatalf("oaiReq.Stream = %v, want true", oaiReq.Stream)
	}
	if oaiReq.StreamOptions == nil || !oaiReq.StreamOptions.IncludeUsage {
		t.Fatalf("oaiReq.StreamOptions.IncludeUsage = false, want true")
	}
	if len(oaiReq.Tools) != 1 {
		t.Fatalf("len(oaiReq.Tools) = %d, want 1", len(oaiReq.Tools))
	}
	choice, ok := oaiReq.ToolChoice.(goopenai.ToolChoice)
	if !ok {
		t.Fatalf("oaiReq.ToolChoice type = %T, want goopenai.ToolChoice", oaiReq.ToolChoice)
	}
	if choice.Function.Name != "lookup_weather" {
		t.Fatalf("choice.Function.Name = %q, want %q", choice.Function.Name, "lookup_weather")
	}
}
