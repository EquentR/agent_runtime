package memory

import (
	"context"
	"strings"
	"testing"

	model "github.com/EquentR/agent_runtime/core/providers/types"
)

func TestLLMShortTermCompressorBuildsSystemAndUserMessages(t *testing.T) {
	client := &fakeChatClient{
		response: model.ChatResponse{Message: model.Message{Role: model.RoleAssistant, Content: "compressed summary"}},
	}
	compressor := NewLLMShortTermCompressor(LLMCompressorOptions{
		Client: client,
		Model:  "gpt-test",
	})

	got, err := compressor(context.Background(), CompressionRequest{
		PreviousSummary:     "old summary",
		Instruction:         "compress carefully",
		TargetSummaryTokens: 8000,
		MaxSummaryTokens:    9000,
		Messages:            []model.Message{{Role: model.RoleUser, Content: "用户想要简洁回答"}},
	})
	if err != nil {
		t.Fatalf("compressor() error = %v", err)
	}
	if got != "compressed summary" {
		t.Fatalf("compressor() = %q, want compressed summary", got)
	}
	if client.lastReq.Model != "gpt-test" {
		t.Fatalf("ChatRequest.Model = %q, want gpt-test", client.lastReq.Model)
	}
	if client.lastReq.MaxTokens != 9000 {
		t.Fatalf("ChatRequest.MaxTokens = %d, want 9000", client.lastReq.MaxTokens)
	}
	if len(client.lastReq.Messages) != 2 {
		t.Fatalf("len(ChatRequest.Messages) = %d, want 2", len(client.lastReq.Messages))
	}
	if client.lastReq.Messages[0].Role != model.RoleSystem || client.lastReq.Messages[0].Content != "compress carefully" {
		t.Fatalf("system message = %#v, want compression instruction", client.lastReq.Messages[0])
	}
	if client.lastReq.Messages[1].Role != model.RoleUser {
		t.Fatalf("user prompt role = %q, want user", client.lastReq.Messages[1].Role)
	}
	if client.lastReq.Messages[1].Content == "" {
		t.Fatal("user prompt content = empty, want rendered memory payload")
	}
	if !containsAll(client.lastReq.Messages[1].Content, []string{"old summary", "用户想要简洁回答", "8000"}) {
		t.Fatalf("user prompt content = %q, want previous summary, message content, and target budget", client.lastReq.Messages[1].Content)
	}
}

func TestLLMLongTermCompressorBuildsSystemAndUserMessages(t *testing.T) {
	client := &fakeChatClient{
		response: model.ChatResponse{Message: model.Message{Role: model.RoleAssistant, Content: "Persistent Facts\n- user likes Go"}},
	}
	compressor := NewLLMLongTermCompressor(LLMCompressorOptions{
		Client: client,
		Model:  "gpt-memory",
	})

	got, err := compressor(context.Background(), LongTermCompressionRequest{
		UserID:          "user-1",
		PreviousSummary: "User Preferences\n- concise",
		Instruction:     "keep only durable memory",
		Messages:        []model.Message{{Role: model.RoleUser, Content: "记住我喜欢 Go 和简洁回复"}},
	})
	if err != nil {
		t.Fatalf("compressor() error = %v", err)
	}
	if got != "Persistent Facts\n- user likes Go" {
		t.Fatalf("compressor() = %q, want returned summary", got)
	}
	if client.lastReq.Model != "gpt-memory" {
		t.Fatalf("ChatRequest.Model = %q, want gpt-memory", client.lastReq.Model)
	}
	if len(client.lastReq.Messages) != 2 {
		t.Fatalf("len(ChatRequest.Messages) = %d, want 2", len(client.lastReq.Messages))
	}
	if client.lastReq.Messages[0].Role != model.RoleSystem || client.lastReq.Messages[0].Content != "keep only durable memory" {
		t.Fatalf("system message = %#v, want long-term instruction", client.lastReq.Messages[0])
	}
	if !containsAll(client.lastReq.Messages[1].Content, []string{"user-1", "User Preferences", "记住我喜欢 Go 和简洁回复"}) {
		t.Fatalf("user prompt content = %q, want user id, previous summary and messages", client.lastReq.Messages[1].Content)
	}
}

func TestLLMShortTermCompressorRejectsEmptySummary(t *testing.T) {
	client := &fakeChatClient{
		response: model.ChatResponse{Message: model.Message{Role: model.RoleAssistant, Content: "   "}},
	}
	compressor := NewLLMShortTermCompressor(LLMCompressorOptions{
		Client: client,
		Model:  "gpt-test",
	})

	_, err := compressor(context.Background(), CompressionRequest{
		Instruction: "compress carefully",
		Messages:    []model.Message{{Role: model.RoleUser, Content: "hello"}},
	})
	if err == nil {
		t.Fatal("compressor() error = nil, want empty summary error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "empty") {
		t.Fatalf("compressor() error = %q, want empty summary message", err)
	}
}

type fakeChatClient struct {
	lastReq  model.ChatRequest
	response model.ChatResponse
	err      error
}

func (f *fakeChatClient) Chat(_ context.Context, req model.ChatRequest) (model.ChatResponse, error) {
	f.lastReq = req
	if f.err != nil {
		return model.ChatResponse{}, f.err
	}
	return f.response, nil
}

func containsAll(text string, parts []string) bool {
	for _, part := range parts {
		if !strings.Contains(text, part) {
			return false
		}
	}
	return true
}
