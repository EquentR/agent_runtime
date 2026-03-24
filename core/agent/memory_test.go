package agent

import (
	"context"
	"testing"

	"github.com/EquentR/agent_runtime/core/memory"
	coreprompt "github.com/EquentR/agent_runtime/core/prompt"
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

func TestRunnerUsesResolvedSessionPromptsBeforeHistoryAndIgnoresLegacySystemPrompt(t *testing.T) {
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
	runner, err := NewRunner(client, nil, Options{
		Model:        "test-model",
		Memory:       mgr,
		SystemPrompt: "legacy prompt should be ignored",
		ResolvedPrompt: &coreprompt.ResolvedPrompt{
			Session: []model.Message{
				{Role: model.RoleSystem, Content: "Session one"},
				{Role: model.RoleSystem, Content: "Session two"},
			},
		},
	})
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
	if len(got) != 3 {
		t.Fatalf("len(Messages) = %d, want 3", len(got))
	}
	if got[0].Role != model.RoleSystem || got[0].Content != "Session one\n\nSession two" {
		t.Fatalf("first message = %#v, want combined resolved session prompt", got[0])
	}
	if got[1].Content != "remembered" {
		t.Fatalf("second message = %q, want remembered", got[1].Content)
	}
	if got[2].Content != "new request" {
		t.Fatalf("third message = %q, want new request", got[2].Content)
	}
}

func TestRunnerDoesNotWriteResolvedPromptsIntoMemoryShortTermMessages(t *testing.T) {
	mgr, err := memory.NewManager(memory.Options{Counter: fakeTokenCounter{}})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	registry := newTestRegistry(t, map[string]func(context.Context, map[string]interface{}) (string, error){
		"lookup_weather": func(context.Context, map[string]interface{}) (string, error) {
			return "sunny", nil
		},
	})
	client := &stubClient{streams: []model.Stream{
		newStubStream(
			[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: `{"city":"Shanghai"}`}}}}},
			model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: `{"city":"Shanghai"}`}}},
			nil,
		),
		newStubStream(
			[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "done"}}},
			model.Message{Role: model.RoleAssistant, Content: "done"},
			nil,
		),
	}}
	runner, err := NewRunner(client, registry, Options{
		Model:  "test-model",
		Memory: mgr,
		ResolvedPrompt: &coreprompt.ResolvedPrompt{
			Session:      []model.Message{{Role: model.RoleSystem, Content: "Session prompt"}},
			StepPreModel: []model.Message{{Role: model.RoleSystem, Content: "Step prompt"}},
			ToolResult:   []model.Message{{Role: model.RoleSystem, Content: "Tool prompt"}},
		},
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	_, err = runner.Run(context.Background(), RunInput{
		Messages: []model.Message{{Role: model.RoleUser, Content: "weather?"}},
		Tools:    registry.List(),
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(client.streamRequests) != 2 {
		t.Fatalf("request count = %d, want 2", len(client.streamRequests))
	}
	assertRequestContainsPrompt(t, client.streamRequests[0].Messages, "Session prompt")
	assertRequestContainsPrompt(t, client.streamRequests[0].Messages, "Step prompt")
	assertRequestContainsPrompt(t, client.streamRequests[1].Messages, "Session prompt")
	assertRequestContainsPrompt(t, client.streamRequests[1].Messages, "Step prompt")
	assertRequestContainsPrompt(t, client.streamRequests[1].Messages, "Tool prompt")

	got := mgr.ShortTermMessages()
	if len(got) != 4 {
		t.Fatalf("len(ShortTermMessages()) = %d, want 4", len(got))
	}
	for _, message := range got {
		if message.Content == "Session prompt" || message.Content == "Step prompt" || message.Content == "Tool prompt" {
			t.Fatalf("memory contains injected prompt message = %#v", message)
		}
	}
	if got[0].Role != model.RoleUser || got[0].Content != "weather?" {
		t.Fatalf("first memory message = %#v, want user request", got[0])
	}
	if got[1].Role != model.RoleAssistant || len(got[1].ToolCalls) != 1 {
		t.Fatalf("second memory message = %#v, want assistant tool call", got[1])
	}
	if got[2].Role != model.RoleTool || got[2].Content != "sunny" {
		t.Fatalf("third memory message = %#v, want tool result", got[2])
	}
	if got[3].Role != model.RoleAssistant || got[3].Content != "done" {
		t.Fatalf("fourth memory message = %#v, want final assistant reply", got[3])
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

func TestCloneMessagePreservesProviderData(t *testing.T) {
	message := model.Message{
		Role: model.RoleAssistant,
		ProviderData: map[string]any{
			"type":        "openai_responses_new.output.v1",
			"response_id": "resp_1",
			"output_json": "[]",
		},
	}

	cloned := cloneMessage(message)
	if cloned.ProviderData == nil {
		t.Fatal("cloneMessage().ProviderData = nil, want cloned provider data")
	}
	clonedMap := cloned.ProviderData.(map[string]any)
	clonedMap["type"] = "changed"
	originalMap := message.ProviderData.(map[string]any)
	if originalMap["type"] != "openai_responses_new.output.v1" {
		t.Fatalf("original provider data mutated = %#v", originalMap)
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

func assertRequestContainsPrompt(t *testing.T, messages []model.Message, want string) {
	t.Helper()

	for _, message := range messages {
		if message.Content == want {
			return
		}
	}
	t.Fatalf("request messages = %#v, want prompt %q", messages, want)
}
