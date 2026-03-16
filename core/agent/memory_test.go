package agent

import (
	"context"
	"testing"

	"github.com/EquentR/agent_runtime/core/memory"
	model "github.com/EquentR/agent_runtime/core/providers/types"
	coretypes "github.com/EquentR/agent_runtime/core/types"
)

func TestRunnerUsesMemoryContextMessages(t *testing.T) {
	mgr, err := memory.NewManager(memory.Options{Counter: fakeTokenCounter{}})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	mgr.AddMessage(model.Message{Role: model.RoleAssistant, Content: "remembered"})

	client := &stubClient{
		streams: []model.Stream{newStubStream(
			[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "hello"}}},
			model.Message{Role: model.RoleAssistant, Content: "hello"},
			nil,
		)},
	}
	runner, err := NewRunner(client, nil, Options{Model: "test-model", Memory: mgr})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	_, err = runner.Run(context.Background(), RunInput{
		Messages: []model.Message{{Role: model.RoleUser, Content: "new request"}},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(client.streamRequests) != 1 {
		t.Fatalf("request count = %d, want 1", len(client.streamRequests))
	}
	got := client.streamRequests[0].Messages
	if len(got) != 2 {
		t.Fatalf("len(Messages) = %d, want 2", len(got))
	}
	if got[0].Content != "remembered" {
		t.Fatalf("first message = %q, want remembered", got[0].Content)
	}
	if got[1].Content != "new request" {
		t.Fatalf("second message = %q, want new request", got[1].Content)
	}
}

func TestRunnerWritesUserAndAssistantMessagesBackToMemory(t *testing.T) {
	mgr, err := memory.NewManager(memory.Options{Counter: fakeTokenCounter{}})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	client := &stubClient{
		streams: []model.Stream{newStubStream(
			[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "hello"}}},
			model.Message{Role: model.RoleAssistant, Content: "hello"},
			nil,
		)},
	}
	runner, err := NewRunner(client, nil, Options{Model: "test-model", Memory: mgr})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	_, err = runner.Run(context.Background(), RunInput{
		Messages: []model.Message{{Role: model.RoleUser, Content: "new request"}},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	got := mgr.ShortTermMessages()
	if len(got) != 2 {
		t.Fatalf("len(ShortTermMessages()) = %d, want 2", len(got))
	}
	if got[0].Role != model.RoleUser || got[0].Content != "new request" {
		t.Fatalf("first memory message = %#v, want user request", got[0])
	}
	if got[1].Role != model.RoleAssistant || got[1].Content != "hello" {
		t.Fatalf("second memory message = %#v, want assistant reply", got[1])
	}
}

func TestNewRunnerUsesLLMModelIDWhenModelStringEmpty(t *testing.T) {
	runner, err := NewRunner(&stubClient{}, nil, Options{
		LLMModel: &coretypes.LLMModel{BaseModel: coretypes.BaseModel{ID: "gpt-5.4", Name: "GPT 5.4"}},
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if runner.options.Model != "gpt-5.4" {
		t.Fatalf("resolved model = %q, want gpt-5.4", runner.options.Model)
	}
}

func TestRunnerUsesResolvedOutputBudgetForMaxTokensDefault(t *testing.T) {
	runner, err := NewRunner(&stubClient{}, nil, Options{
		LLMModel: &coretypes.LLMModel{
			BaseModel: coretypes.BaseModel{ID: "gpt-5.4", Name: "GPT 5.4"},
			Context:   coretypes.LLMContextConfig{Max: 128000, Output: 8000},
		},
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if runner.options.MaxTokens != 8000 {
		t.Fatalf("MaxTokens = %d, want 8000", runner.options.MaxTokens)
	}
}

func TestNewRunnerPassesLLMModelToMemoryBudgeting(t *testing.T) {
	mgr, err := memory.NewManager(memory.Options{
		Model: &coretypes.LLMModel{
			BaseModel: coretypes.BaseModel{ID: "gpt-5.4", Name: "GPT 5.4"},
			Context:   coretypes.LLMContextConfig{Max: 128000, Output: 8000},
		},
		Counter: fakeTokenCounter{},
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	if mgr.MaxContextTokens() != 120000 {
		t.Fatalf("MaxContextTokens = %d, want 120000", mgr.MaxContextTokens())
	}
	runner, err := NewRunner(&stubClient{}, nil, Options{
		LLMModel: &coretypes.LLMModel{
			BaseModel: coretypes.BaseModel{ID: "gpt-5.4", Name: "GPT 5.4"},
			Context:   coretypes.LLMContextConfig{Max: 128000, Output: 8000},
		},
		Memory: mgr,
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if runner.options.Memory.MaxContextTokens() != 120000 {
		t.Fatalf("runner memory max context = %d, want 120000", runner.options.Memory.MaxContextTokens())
	}
}

type fakeTokenCounter struct{}

func (fakeTokenCounter) Count(text string) int {
	return len([]rune(text))
}

func (fakeTokenCounter) CountMessages(messages []string) int {
	total := 0
	for _, message := range messages {
		total += len([]rune(message))
	}
	return total
}
