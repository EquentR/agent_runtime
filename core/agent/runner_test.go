package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/EquentR/agent_runtime/core/approvals"
	coreaudit "github.com/EquentR/agent_runtime/core/audit"
	"github.com/EquentR/agent_runtime/core/memory"
	coreprompt "github.com/EquentR/agent_runtime/core/prompt"
	model "github.com/EquentR/agent_runtime/core/providers/types"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	coretools "github.com/EquentR/agent_runtime/core/tools"
	coretypes "github.com/EquentR/agent_runtime/core/types"
)

func TestRunnerRequiresClient(t *testing.T) {
	_, err := NewRunner(nil, nil, Options{})
	if err == nil {
		t.Fatal("NewRunner() error = nil, want non-nil")
	}
}

func TestRunnerReturnsDirectAssistantMessage(t *testing.T) {
	client := &stubClient{
		streams: []model.Stream{newStubStream(
			[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "hello"}}},
			model.Message{Role: model.RoleAssistant, Content: "hello"},
			nil,
		)},
	}

	runner, err := NewRunner(client, nil, Options{Model: "test-model", MaxSteps: 4})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	result, err := runner.Run(context.Background(), RunInput{
		Messages: []model.Message{{Role: model.RoleUser, Content: "say hi"}},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.FinalMessage.Content != "hello" {
		t.Fatalf("final content = %q, want %q", result.FinalMessage.Content, "hello")
	}
	if result.StepsExecuted != 1 {
		t.Fatalf("steps = %d, want 1", result.StepsExecuted)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("len(Messages) = %d, want 1", len(result.Messages))
	}
}

func TestRunnerExecutesToolCallsAndContinuesConversation(t *testing.T) {
	registry := coretools.NewRegistry()
	if err := registry.Register(coretools.Tool{
		Name: "lookup_weather",
		Handler: func(ctx context.Context, arguments map[string]interface{}) (string, error) {
			if arguments["city"] != "Shanghai" {
				t.Fatalf("city = %#v, want Shanghai", arguments["city"])
			}
			return "sunny", nil
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	client := &stubClient{responses: []model.ChatResponse{}}
	client.streams = []model.Stream{
		newStubStream(
			[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: `{"city":"Shanghai"}`}}}}},
			model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: `{"city":"Shanghai"}`}}},
			nil,
		),
		newStubStream(
			[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "The weather is sunny."}}},
			model.Message{Role: model.RoleAssistant, Content: "The weather is sunny."},
			nil,
		),
	}

	runner, err := NewRunner(client, registry, Options{Model: "test-model", MaxSteps: 4})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	result, err := runner.Run(context.Background(), RunInput{Messages: []model.Message{{Role: model.RoleUser, Content: "weather?"}}, Tools: registry.List()})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.FinalMessage.Content != "The weather is sunny." {
		t.Fatalf("final content = %q", result.FinalMessage.Content)
	}
	if result.ToolCalls != 1 {
		t.Fatalf("tool calls = %d, want 1", result.ToolCalls)
	}
	if len(result.Messages) != 3 {
		t.Fatalf("len(Messages) = %d, want 3", len(result.Messages))
	}
	if result.Messages[1].Role != model.RoleTool || result.Messages[1].ToolCallId != "call_1" {
		t.Fatalf("tool message = %#v, want role=tool call_1", result.Messages[1])
	}
	if len(client.streamRequests) != 2 {
		t.Fatalf("request count = %d, want 2", len(client.streamRequests))
	}
	secondReq := client.streamRequests[1].Messages
	if len(secondReq) != 3 {
		t.Fatalf("len(second request messages) = %d, want 3", len(secondReq))
	}
	if secondReq[1].Role != model.RoleAssistant || len(secondReq[1].ToolCalls) != 1 {
		t.Fatalf("assistant replay message = %#v, want tool call replay", secondReq[1])
	}
}

func TestRunnerInjectsStepPreModelOnEveryTurnAndToolResultOnlyAfterToolContinuation(t *testing.T) {
	registry := coretools.NewRegistry()
	if err := registry.Register(coretools.Tool{
		Name: "lookup_weather",
		Handler: func(context.Context, map[string]interface{}) (string, error) {
			return "sunny", nil
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	client := &stubClient{streams: []model.Stream{
		newStubStream(
			[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: `{"city":"Shanghai"}`}}}}},
			model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: `{"city":"Shanghai"}`}}},
			nil,
		),
		newStubStream(
			[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "The weather is sunny."}}},
			model.Message{Role: model.RoleAssistant, Content: "The weather is sunny."},
			nil,
		),
	}}

	runner, err := NewRunner(client, registry, Options{
		Model: "test-model",
		ResolvedPrompt: &coreprompt.ResolvedPrompt{
			Session:      []model.Message{{Role: model.RoleSystem, Content: "Session prompt"}},
			StepPreModel: []model.Message{{Role: model.RoleSystem, Content: "Step prompt"}},
			ToolResult:   []model.Message{{Role: model.RoleSystem, Content: "Tool-result prompt"}},
		},
		MaxSteps: 4,
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	result, err := runner.Run(context.Background(), RunInput{Messages: []model.Message{{Role: model.RoleUser, Content: "weather?"}}, Tools: registry.List()})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.FinalMessage.Content != "The weather is sunny." {
		t.Fatalf("final content = %q", result.FinalMessage.Content)
	}
	if len(client.streamRequests) != 2 {
		t.Fatalf("request count = %d, want 2", len(client.streamRequests))
	}

	firstReq := client.streamRequests[0].Messages
	if len(firstReq) != 3 {
		t.Fatalf("len(first request messages) = %d, want 3", len(firstReq))
	}
	if firstReq[0].Content != "Session prompt" || firstReq[1].Content != "Step prompt" || firstReq[2].Content != "weather?" {
		t.Fatalf("first request messages = %#v, want session+step+user", firstReq)
	}
	assertMessagesDoNotContainContent(t, firstReq, "Tool-result prompt")

	secondReq := client.streamRequests[1].Messages
	if len(secondReq) != 6 {
		t.Fatalf("len(second request messages) = %d, want 6", len(secondReq))
	}
	if secondReq[0].Content != "Session prompt" || secondReq[1].Content != "Step prompt" {
		t.Fatalf("second request prompt prefix = %#v, want session+step", secondReq[:2])
	}
	if secondReq[2].Role != model.RoleUser || secondReq[2].Content != "weather?" {
		t.Fatalf("second request user replay = %#v, want original user message", secondReq[2])
	}
	if secondReq[3].Role != model.RoleAssistant || len(secondReq[3].ToolCalls) != 1 {
		t.Fatalf("second request assistant replay = %#v, want assistant tool call replay", secondReq[3])
	}
	if secondReq[4].Role != model.RoleSystem || secondReq[4].Content != "Tool-result prompt" {
		t.Fatalf("second request tool-result prompt = %#v, want prompt between assistant and tool", secondReq[4])
	}
	if secondReq[5].Role != model.RoleTool || secondReq[5].Content != "sunny" {
		t.Fatalf("second request tool replay = %#v, want tool output replay", secondReq[5])
	}
}

func TestRunnerInjectsToolResultPromptBetweenAssistantAndMultipleToolMessagesOncePerTurn(t *testing.T) {
	registry := coretools.NewRegistry()
	if err := registry.Register(coretools.Tool{
		Name: "lookup_weather",
		Handler: func(context.Context, map[string]interface{}) (string, error) {
			return "sunny", nil
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := registry.Register(coretools.Tool{
		Name: "lookup_time",
		Handler: func(context.Context, map[string]interface{}) (string, error) {
			return "08:00", nil
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	client := &stubClient{streams: []model.Stream{
		newStubStream(
			[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: `{"city":"Shanghai"}`}, {ID: "call_2", Name: "lookup_time", Arguments: `{"city":"Shanghai"}`}}}}},
			model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: `{"city":"Shanghai"}`}, {ID: "call_2", Name: "lookup_time", Arguments: `{"city":"Shanghai"}`}}},
			nil,
		),
		newStubStream(
			[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "done"}}},
			model.Message{Role: model.RoleAssistant, Content: "done"},
			nil,
		),
	}}

	runner, err := NewRunner(client, registry, Options{
		Model: "test-model",
		ResolvedPrompt: &coreprompt.ResolvedPrompt{
			Session:      []model.Message{{Role: model.RoleSystem, Content: "Session prompt"}},
			StepPreModel: []model.Message{{Role: model.RoleSystem, Content: "Step prompt"}},
			ToolResult: []model.Message{
				{Role: model.RoleSystem, Content: "Tool-result prompt one"},
				{Role: model.RoleSystem, Content: "Tool-result prompt two"},
			},
		},
		MaxSteps: 4,
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	result, err := runner.Run(context.Background(), RunInput{Messages: []model.Message{{Role: model.RoleUser, Content: "weather and time?"}}, Tools: registry.List()})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.FinalMessage.Content != "done" {
		t.Fatalf("final content = %q, want done", result.FinalMessage.Content)
	}
	if len(client.streamRequests) != 2 {
		t.Fatalf("request count = %d, want 2", len(client.streamRequests))
	}

	secondReq := client.streamRequests[1].Messages
	if len(secondReq) != 8 {
		t.Fatalf("len(second request messages) = %d, want 8", len(secondReq))
	}
	if secondReq[0].Content != "Session prompt" || secondReq[1].Content != "Step prompt" {
		t.Fatalf("second request prefix = %#v, want session+step prompts", secondReq[:2])
	}
	if secondReq[2].Role != model.RoleUser || secondReq[2].Content != "weather and time?" {
		t.Fatalf("second request user replay = %#v, want original user message", secondReq[2])
	}
	if secondReq[3].Role != model.RoleAssistant || len(secondReq[3].ToolCalls) != 2 {
		t.Fatalf("second request assistant replay = %#v, want assistant tool-call replay", secondReq[3])
	}
	if secondReq[4].Role != model.RoleSystem || secondReq[4].Content != "Tool-result prompt one" {
		t.Fatalf("second request first tool-result prompt = %#v, want first tool-result prompt after assistant", secondReq[4])
	}
	if secondReq[5].Role != model.RoleSystem || secondReq[5].Content != "Tool-result prompt two" {
		t.Fatalf("second request second tool-result prompt = %#v, want second tool-result prompt after assistant", secondReq[5])
	}
	if secondReq[6].Role != model.RoleTool || secondReq[6].ToolCallId != "call_1" || secondReq[6].Content != "sunny" {
		t.Fatalf("second request first tool replay = %#v, want weather tool result", secondReq[6])
	}
	if secondReq[7].Role != model.RoleTool || secondReq[7].ToolCallId != "call_2" || secondReq[7].Content != "08:00" {
		t.Fatalf("second request second tool replay = %#v, want time tool result", secondReq[7])
	}

	assertMessagesContainContentOnce(t, secondReq, "Tool-result prompt one")
	assertMessagesContainContentOnce(t, secondReq, "Tool-result prompt two")
}

func TestRunnerFallsBackToLegacySystemPromptWhenResolvedPromptAbsent(t *testing.T) {
	client := &stubClient{
		streams: []model.Stream{newStubStream(
			[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "hello"}}},
			model.Message{Role: model.RoleAssistant, Content: "hello"},
			nil,
		)},
	}
	runner, err := NewRunner(client, nil, Options{Model: "test-model", SystemPrompt: "Legacy prompt"})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	_, err = runner.Run(context.Background(), RunInput{Messages: []model.Message{{Role: model.RoleUser, Content: "say hi"}}})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(client.streamRequests) != 1 {
		t.Fatalf("request count = %d, want 1", len(client.streamRequests))
	}
	got := client.streamRequests[0].Messages
	if len(got) != 2 {
		t.Fatalf("len(request messages) = %d, want 2", len(got))
	}
	if got[0].Role != model.RoleSystem || got[0].Content != "Legacy prompt" {
		t.Fatalf("first request message = %#v, want legacy system prompt", got[0])
	}
	if got[1].Role != model.RoleUser || got[1].Content != "say hi" {
		t.Fatalf("second request message = %#v, want user message", got[1])
	}
}

func TestRunnerUsesMemoryManagedContextOnLaterSteps(t *testing.T) {
	mgr, err := memory.NewManager(memory.Options{
		MaxContextTokens: 60,
		Counter:          fakeTokenCounter{},
		Compressor: func(context.Context, memory.CompressionRequest) (string, error) {
			return "compressed memory", nil
		},
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	registry := coretools.NewRegistry()
	if err := registry.Register(coretools.Tool{
		Name: "lookup_weather",
		Handler: func(context.Context, map[string]interface{}) (string, error) {
			return strings.Repeat("sunny", 12), nil
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	client := &stubClient{streams: []model.Stream{
		newStubStream(
			[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: `{"city":"` + strings.Repeat("x", 40) + `"}`}}}}},
			model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: `{"city":"` + strings.Repeat("x", 40) + `"}`}}},
			nil,
		),
		newStubStream(
			[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "done"}}},
			model.Message{Role: model.RoleAssistant, Content: "done"},
			nil,
		),
	}}

	runner, err := NewRunner(client, registry, Options{Model: "test-model", Memory: mgr, MaxSteps: 4})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	result, err := runner.Run(context.Background(), RunInput{Messages: []model.Message{{Role: model.RoleUser, Content: "weather?"}}, Tools: registry.List()})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.FinalMessage.Content != "done" {
		t.Fatalf("final content = %q, want done", result.FinalMessage.Content)
	}
	if len(client.streamRequests) != 2 {
		t.Fatalf("request count = %d, want 2", len(client.streamRequests))
	}

	secondReq := client.streamRequests[1].Messages
	if len(secondReq) != 1 {
		t.Fatalf("len(second request messages) = %d, want 1 compressed summary message", len(secondReq))
	}
	if secondReq[0].Role != model.RoleSystem || !strings.Contains(secondReq[0].Content, "compressed memory") {
		t.Fatalf("second request messages = %#v, want compressed summary from memory manager", secondReq)
	}
	assertMessagesDoNotContainContent(t, secondReq, "weather?")
	assertMessagesDoNotContainContent(t, secondReq, strings.Repeat("sunny", 12))
}

func TestRunnerDoesNotReAddProducedMessagesToMemoryWhenInitialInputEmpty(t *testing.T) {
	mgr, err := memory.NewManager(memory.Options{Counter: fakeTokenCounter{}})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	registry := coretools.NewRegistry()
	if err := registry.Register(coretools.Tool{
		Name: "lookup_weather",
		Handler: func(context.Context, map[string]interface{}) (string, error) {
			return "sunny", nil
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

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

	runner, err := NewRunner(client, registry, Options{Model: "test-model", Memory: mgr, MaxSteps: 4})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	result, err := runner.Run(context.Background(), RunInput{Tools: registry.List()})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.FinalMessage.Content != "done" {
		t.Fatalf("final content = %q, want done", result.FinalMessage.Content)
	}
	if len(client.streamRequests) != 2 {
		t.Fatalf("request count = %d, want 2", len(client.streamRequests))
	}

	secondReq := client.streamRequests[1].Messages
	if len(secondReq) != 2 {
		t.Fatalf("len(second request messages) = %d, want 2 without duplicated replay", len(secondReq))
	}
	if secondReq[0].Role != model.RoleAssistant || len(secondReq[0].ToolCalls) != 1 {
		t.Fatalf("second request first message = %#v, want assistant tool call replay", secondReq[0])
	}
	if secondReq[1].Role != model.RoleTool || secondReq[1].Content != "sunny" {
		t.Fatalf("second request second message = %#v, want tool result replay", secondReq[1])
	}

	got := mgr.ShortTermMessages()
	if len(got) != 3 {
		t.Fatalf("len(ShortTermMessages()) = %d, want 3 without duplicates", len(got))
	}
	if got[0].Role != model.RoleAssistant || len(got[0].ToolCalls) != 1 {
		t.Fatalf("first memory message = %#v, want assistant tool call", got[0])
	}
	if got[1].Role != model.RoleTool || got[1].Content != "sunny" {
		t.Fatalf("second memory message = %#v, want tool output", got[1])
	}
	if got[2].Role != model.RoleAssistant || got[2].Content != "done" {
		t.Fatalf("third memory message = %#v, want final assistant reply", got[2])
	}
}

func TestRunnerInjectsToolResultPromptAfterToolTurnEvenWhenMemoryCompressesSuffixAway(t *testing.T) {
	mgr, err := memory.NewManager(memory.Options{
		MaxContextTokens: 60,
		Counter:          fakeTokenCounter{},
		Compressor: func(context.Context, memory.CompressionRequest) (string, error) {
			return "compressed memory", nil
		},
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	registry := coretools.NewRegistry()
	if err := registry.Register(coretools.Tool{
		Name: "lookup_weather",
		Handler: func(context.Context, map[string]interface{}) (string, error) {
			return strings.Repeat("sunny", 12), nil
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	client := &stubClient{streams: []model.Stream{
		newStubStream(
			[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: `{"city":"` + strings.Repeat("x", 40) + `"}`}}}}},
			model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: `{"city":"` + strings.Repeat("x", 40) + `"}`}}},
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
			StepPreModel: []model.Message{{Role: model.RoleSystem, Content: "Step prompt"}},
			ToolResult:   []model.Message{{Role: model.RoleSystem, Content: "Tool-result prompt"}},
		},
		MaxSteps: 4,
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	result, err := runner.Run(context.Background(), RunInput{Messages: []model.Message{{Role: model.RoleUser, Content: "weather?"}}, Tools: registry.List()})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.FinalMessage.Content != "done" {
		t.Fatalf("final content = %q, want done", result.FinalMessage.Content)
	}
	if len(client.streamRequests) != 2 {
		t.Fatalf("request count = %d, want 2", len(client.streamRequests))
	}

	secondReq := client.streamRequests[1].Messages
	if len(secondReq) != 3 {
		t.Fatalf("len(second request messages) = %d, want 3", len(secondReq))
	}
	if secondReq[0].Role != model.RoleSystem || secondReq[0].Content != "Step prompt" {
		t.Fatalf("second request first message = %#v, want step prompt", secondReq[0])
	}
	if secondReq[1].Role != model.RoleSystem || !strings.Contains(secondReq[1].Content, "compressed memory") {
		t.Fatalf("second request second message = %#v, want compressed memory context", secondReq[1])
	}
	if secondReq[2].Role != model.RoleSystem || secondReq[2].Content != "Tool-result prompt" {
		t.Fatalf("second request third message = %#v, want tool-result prompt appended when assistant/tool boundary was compressed", secondReq[2])
	}
}

func TestRunnerEmitsStepAndToolEvents(t *testing.T) {
	registry := coretools.NewRegistry()
	if err := registry.Register(coretools.Tool{
		Name: "lookup_weather",
		Handler: func(context.Context, map[string]interface{}) (string, error) {
			return "sunny", nil
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	sink := &recordingEventSink{}
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
	runner, err := NewRunner(client, registry, Options{Model: "test-model", EventSink: sink})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	_, err = runner.Run(context.Background(), RunInput{Messages: []model.Message{{Role: model.RoleUser, Content: "weather?"}}, Tools: registry.List()})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(sink.events) < 4 {
		t.Fatalf("event count = %d, want at least 4", len(sink.events))
	}
	if sink.events[0] != "step.start" || sink.events[1] != "tool.start" || sink.events[2] != "tool.finish" || sink.events[3] != "step.finish" {
		t.Fatalf("events = %#v, want step/tool ordering", sink.events)
	}
}

func TestRunnerEmitsReplayableAuditArtifacts(t *testing.T) {
	registry := newTestRegistry(t, map[string]func(context.Context, map[string]interface{}) (string, error){
		"lookup_weather": func(context.Context, map[string]interface{}) (string, error) {
			return "sunny", nil
		},
	})
	recorder := newRecordingRunnerAuditRecorder()
	client := &stubClient{streams: []model.Stream{
		newStubStream(
			[]model.StreamEvent{
				{Type: model.StreamEventUsage, Usage: model.TokenUsage{PromptTokens: 12, CompletionTokens: 8, TotalTokens: 20}},
				{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: `{"city":"Shanghai"}`}}}},
			},
			model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: `{"city":"Shanghai"}`}}},
			nil,
		),
		newStubStream(
			[]model.StreamEvent{
				{Type: model.StreamEventUsage, Usage: model.TokenUsage{PromptTokens: 16, CompletionTokens: 4, TotalTokens: 20}},
				{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "The weather is sunny."}},
			},
			model.Message{Role: model.RoleAssistant, Content: "The weather is sunny."},
			nil,
		),
	}}

	runner, err := NewRunner(client, registry, Options{
		Model:         "test-model",
		AuditRecorder: recorder,
		AuditRunID:    "run_1",
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	result, err := runner.Run(context.Background(), RunInput{
		Messages: []model.Message{{Role: model.RoleUser, Content: "weather?"}},
		Tools:    registry.List(),
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.FinalMessage.Content != "The weather is sunny." {
		t.Fatalf("FinalMessage.Content = %q, want %q", result.FinalMessage.Content, "The weather is sunny.")
	}

	assertRunnerAuditEventTypes(t, recorder, "run_1",
		"step.started",
		"prompt.resolved",
		"request.built",
		"model.completed",
		"tool.started",
		"tool.finished",
		"step.finished",
		"step.started",
		"prompt.resolved",
		"request.built",
		"model.completed",
		"step.finished",
	)

	requestEvent := recorder.requireEventForStep(t, "run_1", "request.built", 1)
	requestPayload := decodeAuditPayload(t, requestEvent)
	if _, ok := requestPayload["messages"]; ok {
		t.Fatalf("request payload = %#v, want compact payload without full messages", requestPayload)
	}
	if requestPayload["message_count"] != float64(1) {
		t.Fatalf("request payload = %#v, want message_count=1", requestPayload)
	}

	toolStarted := recorder.requireEventForStep(t, "run_1", "tool.started", 1)
	toolStartedPayload := decodeAuditPayload(t, toolStarted)
	if _, ok := toolStartedPayload["arguments"]; ok {
		t.Fatalf("tool.started payload = %#v, want compact payload without arguments", toolStartedPayload)
	}
	if toolStartedPayload["tool_name"] != "lookup_weather" {
		t.Fatalf("tool.started payload = %#v, want tool_name lookup_weather", toolStartedPayload)
	}

	toolFinished := recorder.requireEventForStep(t, "run_1", "tool.finished", 1)
	toolFinishedPayload := decodeAuditPayload(t, toolFinished)
	if _, ok := toolFinishedPayload["output"]; ok {
		t.Fatalf("tool.finished payload = %#v, want compact payload without output", toolFinishedPayload)
	}
	if toolFinishedPayload["tool_name"] != "lookup_weather" {
		t.Fatalf("tool.finished payload = %#v, want tool_name lookup_weather", toolFinishedPayload)
	}

	requestArtifacts := recorder.requireArtifactsByKind(t, "run_1", coreaudit.ArtifactKindModelRequest)
	if len(requestArtifacts) != 2 {
		t.Fatalf("model request artifact count = %d, want 2", len(requestArtifacts))
	}
	firstRequest := decodeModelRequestArtifact(t, requestArtifacts[0])
	if len(firstRequest.Messages) != 1 || firstRequest.Messages[0].Content != "weather?" {
		t.Fatalf("first request messages = %#v, want single user message", firstRequest.Messages)
	}
	if len(firstRequest.Tools) != 1 || firstRequest.Tools[0].Name != "lookup_weather" {
		t.Fatalf("first request tools = %#v, want lookup_weather", firstRequest.Tools)
	}
	secondRequest := decodeModelRequestArtifact(t, requestArtifacts[1])
	if len(secondRequest.Messages) != 3 {
		t.Fatalf("second request message count = %d, want 3", len(secondRequest.Messages))
	}

	toolArgsArtifact := recorder.requireArtifactByKind(t, "run_1", coreaudit.ArtifactKindToolArguments)
	toolArgs := decodeToolArgumentsArtifact(t, toolArgsArtifact)
	if toolArgs.ToolName != "lookup_weather" || toolArgs.Arguments != `{"city":"Shanghai"}` {
		t.Fatalf("tool arguments artifact = %#v, want lookup_weather with original arguments", toolArgs)
	}

	toolOutputArtifact := recorder.requireArtifactByKind(t, "run_1", coreaudit.ArtifactKindToolOutput)
	toolOutput := decodeToolOutputArtifact(t, toolOutputArtifact)
	if toolOutput.ToolName != "lookup_weather" || toolOutput.Output != "sunny" {
		t.Fatalf("tool output artifact = %#v, want lookup_weather sunny", toolOutput)
	}
}

func TestRunnerReturnsErrorWhenToolArgumentsAreInvalidJSON(t *testing.T) {
	registry := coretools.NewRegistry()
	if err := registry.Register(coretools.Tool{Name: "lookup_weather", Handler: func(context.Context, map[string]interface{}) (string, error) { return "", nil }}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	client := &stubClient{streams: []model.Stream{newStubStream(
		[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: `{invalid`}}}}},
		model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: `{invalid`}}},
		nil,
	)}}
	runner, err := NewRunner(client, registry, Options{Model: "test-model"})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	_, err = runner.Run(context.Background(), RunInput{Messages: []model.Message{{Role: model.RoleUser, Content: "weather?"}}, Tools: registry.List()})
	if err == nil {
		t.Fatal("Run() error = nil, want invalid JSON error")
	}
}

func TestRunnerReturnsErrorWhenToolExecutionFails(t *testing.T) {
	registry := coretools.NewRegistry()
	if err := registry.Register(coretools.Tool{
		Name: "lookup_weather",
		Handler: func(context.Context, map[string]interface{}) (string, error) {
			return "", errors.New("boom")
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	client := &stubClient{streams: []model.Stream{newStubStream(
		[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: `{"city":"Shanghai"}`}}}}},
		model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: `{"city":"Shanghai"}`}}},
		nil,
	)}}
	runner, err := NewRunner(client, registry, Options{Model: "test-model"})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	_, err = runner.Run(context.Background(), RunInput{Messages: []model.Message{{Role: model.RoleUser, Content: "weather?"}}, Tools: registry.List()})
	if err == nil {
		t.Fatal("Run() error = nil, want tool execution error")
	}
}

func assertMessagesContainContentOnce(t *testing.T, messages []model.Message, want string) {
	t.Helper()

	count := 0
	for _, message := range messages {
		if message.Content == want {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("messages = %#v, want content %q exactly once, got %d", messages, want, count)
	}
}

func TestRunnerReturnsErrorWhenMaxStepsExceeded(t *testing.T) {
	registry := coretools.NewRegistry()
	if err := registry.Register(coretools.Tool{Name: "lookup_weather", Handler: func(context.Context, map[string]interface{}) (string, error) { return "sunny", nil }}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	client := &stubClient{streams: []model.Stream{newStubStream(
		[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: `{"city":"Shanghai"}`}}}}},
		model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: `{"city":"Shanghai"}`}}},
		nil,
	)}}
	runner, err := NewRunner(client, registry, Options{Model: "test-model", MaxSteps: 1})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	_, err = runner.Run(context.Background(), RunInput{Messages: []model.Message{{Role: model.RoleUser, Content: "weather?"}}, Tools: registry.List()})
	if !errors.Is(err, ErrMaxStepsExceeded) {
		t.Fatalf("Run() error = %v, want ErrMaxStepsExceeded", err)
	}
}

func TestRunnerStopsWhenContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := &stubClient{}
	runner, err := NewRunner(client, nil, Options{Model: "test-model"})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	_, err = runner.Run(ctx, RunInput{Messages: []model.Message{{Role: model.RoleUser, Content: "hello"}}})
	if err == nil {
		t.Fatal("Run() error = nil, want context cancellation")
	}
}

func TestRunnerPassesRuntimeMetadataToToolContext(t *testing.T) {
	registry := coretools.NewRegistry()
	if err := registry.Register(coretools.Tool{
		Name: "capture_runtime",
		Handler: func(ctx context.Context, arguments map[string]interface{}) (string, error) {
			runtime, ok := coretools.RuntimeFromContext(ctx)
			if !ok {
				t.Fatal("RuntimeFromContext() ok = false, want true")
			}
			if runtime.StepID != "step-1" {
				t.Fatalf("StepID = %q, want step-1", runtime.StepID)
			}
			if runtime.Actor != "agent-runner" {
				t.Fatalf("Actor = %q, want agent-runner", runtime.Actor)
			}
			if runtime.Metadata["request_id"] != "req-1" {
				t.Fatalf("Metadata = %#v, want request_id", runtime.Metadata)
			}
			return "ok", nil
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	client := &stubClient{streams: []model.Stream{
		newStubStream(
			[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "capture_runtime", Arguments: `{}`}}}}},
			model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "capture_runtime", Arguments: `{}`}}},
			nil,
		),
		newStubStream(
			[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "done"}}},
			model.Message{Role: model.RoleAssistant, Content: "done"},
			nil,
		),
	}}
	runner, err := NewRunner(client, registry, Options{Model: "test-model", Actor: "agent-runner", Metadata: map[string]string{"request_id": "req-1"}})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	_, err = runner.Run(context.Background(), RunInput{Messages: []model.Message{{Role: model.RoleUser, Content: "go"}}, Tools: registry.List()})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRunnerSuspendsGuardedToolCallForApprovalBeforeToolStart(t *testing.T) {
	var executed bool
	registry := coretools.NewRegistry()
	if err := registry.Register(coretools.Tool{
		Name:         "delete_file",
		ApprovalMode: coretypes.ToolApprovalModeAlways,
		ApprovalEvaluator: func(arguments map[string]any) coretools.ApprovalRequirement {
			return coretools.ApprovalRequirement{
				Required:         true,
				ArgumentsSummary: "danger.txt",
				RiskLevel:        coretools.RiskLevelHigh,
				Reason:           "dangerous delete",
			}
		},
		Handler: func(context.Context, map[string]interface{}) (string, error) {
			executed = true
			return "deleted", nil
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	client := &stubClient{streams: []model.Stream{newStubStream(
		[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "delete_file", Arguments: `{"path":"danger.txt"}`}}}}},
		model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "delete_file", Arguments: `{"path":"danger.txt"}`}}},
		nil,
	)}}
	runtime := &recordingTaskRuntime{taskID: "task-approval", metadata: map[string]any{"existing": "value"}}
	runner, err := NewRunner(client, registry, Options{
		Model:     "test-model",
		TaskID:    "task-approval",
		Metadata:  map[string]string{"conversation_id": "conv-approval"},
		EventSink: &taskRuntimeSink{runtime: runtime},
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	result, err := runner.Run(context.Background(), RunInput{Messages: []model.Message{{Role: model.RoleUser, Content: "delete it"}}, Tools: registry.List()})
	if !errors.Is(err, ErrToolApprovalPending) {
		t.Fatalf("Run() error = %v, want ErrToolApprovalPending", err)
	}
	if executed {
		t.Fatal("guarded tool executed before approval")
	}
	if runtime.suspendReason != "waiting_for_tool_approval" {
		t.Fatalf("suspend reason = %q, want waiting_for_tool_approval", runtime.suspendReason)
	}
	if len(runtime.approvals) != 1 {
		t.Fatalf("approval count = %d, want 1", len(runtime.approvals))
	}
	if result.StopReason != "waiting_for_tool_approval" {
		t.Fatalf("StopReason = %q, want waiting_for_tool_approval", result.StopReason)
	}
	if len(result.Messages) != 1 || result.Messages[0].Role != model.RoleAssistant {
		t.Fatalf("result.Messages = %#v, want assistant checkpoint message only", result.Messages)
	}
	approvalRequested := 0
	for _, emit := range runtime.emits {
		if emit.eventType == coretasks.EventApprovalRequested {
			approvalRequested++
		}
		if emit.eventType == coretasks.EventToolStarted {
			t.Fatalf("runtime emits = %#v, want no tool.started before approval", runtime.emits)
		}
	}
	if approvalRequested != 1 {
		t.Fatalf("approval.requested count = %d, want 1; emits = %#v", approvalRequested, runtime.emits)
	}
	checkpointValue, ok := runtime.metadata[toolApprovalCheckpointMetadataKey]
	if !ok {
		t.Fatalf("runtime metadata = %#v, want %q checkpoint key", runtime.metadata, toolApprovalCheckpointMetadataKey)
	}
	checkpointJSON, err := json.Marshal(checkpointValue)
	if err != nil {
		t.Fatalf("json.Marshal(checkpoint) error = %v", err)
	}
	var checkpoint toolApprovalCheckpoint
	if err := json.Unmarshal(checkpointJSON, &checkpoint); err != nil {
		t.Fatalf("json.Unmarshal(checkpoint) error = %v", err)
	}
	if checkpoint.ApprovalID != runtime.approvals[0].ID {
		t.Fatalf("checkpoint approval id = %q, want %q", checkpoint.ApprovalID, runtime.approvals[0].ID)
	}
	if checkpoint.Step != 1 {
		t.Fatalf("checkpoint step = %d, want 1", checkpoint.Step)
	}
	if checkpoint.ToolCallIndex != 0 {
		t.Fatalf("checkpoint tool_call_index = %d, want 0", checkpoint.ToolCallIndex)
	}
	if checkpoint.AssistantMessage.Role != model.RoleAssistant || len(checkpoint.AssistantMessage.ToolCalls) != 1 {
		t.Fatalf("checkpoint assistant_message = %#v, want assistant tool-call message", checkpoint.AssistantMessage)
	}
	if len(checkpoint.ProducedMessagesBeforeCheckpoint) != 1 || checkpoint.ProducedMessagesBeforeCheckpoint[0].Role != model.RoleAssistant {
		t.Fatalf("checkpoint produced_messages_before_checkpoint = %#v, want assistant message", checkpoint.ProducedMessagesBeforeCheckpoint)
	}
	if runtime.metadata["existing"] != "value" {
		t.Fatalf("runtime metadata = %#v, want existing metadata preserved", runtime.metadata)
	}
	if len(client.streamRequests) != 1 {
		t.Fatalf("stream request count = %d, want 1", len(client.streamRequests))
	}
}

func TestRunnerSuspendsGuardedToolCallWhenTaskMetadataIsJSONNull(t *testing.T) {
	registry := coretools.NewRegistry()
	if err := registry.Register(coretools.Tool{
		Name:         "delete_file",
		ApprovalMode: coretypes.ToolApprovalModeAlways,
		Handler: func(context.Context, map[string]interface{}) (string, error) {
			t.Fatal("guarded tool should not execute before approval")
			return "", nil
		},
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	client := &stubClient{streams: []model.Stream{newStubStream(
		[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "delete_file", Arguments: `{"path":"danger.txt"}`}}}}},
		model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "delete_file", Arguments: `{"path":"danger.txt"}`}}},
		nil,
	)}}
	runtime := &recordingTaskRuntime{taskID: "task_approval_null_metadata"}
	runner, err := NewRunner(client, registry, Options{
		Model:     "test-model",
		EventSink: &taskRuntimeSink{runtime: runtime},
		Metadata:  map[string]string{"conversation_id": "conv_1"},
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	result, err := runner.Run(context.Background(), RunInput{Messages: []model.Message{{Role: model.RoleUser, Content: "delete it"}}, Tools: registry.List()})
	if err == nil {
		t.Fatal("Run() error = nil, want task suspended")
	}
	if !errors.Is(err, ErrToolApprovalPending) {
		t.Fatalf("Run() error = %v, want ErrToolApprovalPending", err)
	}
	if runtime.suspendReason != "waiting_for_tool_approval" {
		t.Fatalf("suspend reason = %q, want waiting_for_tool_approval", runtime.suspendReason)
	}
	if result.StopReason != "waiting_for_tool_approval" {
		t.Fatalf("StopReason = %q, want waiting_for_tool_approval", result.StopReason)
	}
	if _, ok := runtime.metadata[toolApprovalCheckpointMetadataKey]; !ok {
		t.Fatalf("runtime metadata = %#v, want checkpoint key", runtime.metadata)
	}
	if len(runtime.approvals) != 1 {
		t.Fatalf("approval count = %d, want 1", len(runtime.approvals))
	}
}

func TestRunnerAggregatesUsageAcrossMultipleSteps(t *testing.T) {
	registry := newTestRegistry(t, map[string]func(context.Context, map[string]interface{}) (string, error){
		"lookup_weather": func(context.Context, map[string]interface{}) (string, error) {
			return "sunny", nil
		},
	})
	client := &stubClient{streams: []model.Stream{
		newStubStream(
			[]model.StreamEvent{
				{Type: model.StreamEventUsage, Usage: model.TokenUsage{PromptTokens: 100, CachedPromptTokens: 20, CompletionTokens: 30, TotalTokens: 130}},
				{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: `{"city":"Shanghai"}`}}}},
			},
			model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: `{"city":"Shanghai"}`}}},
			nil,
		),
		newStubStream(
			[]model.StreamEvent{
				{Type: model.StreamEventUsage, Usage: model.TokenUsage{PromptTokens: 50, CachedPromptTokens: 10, CompletionTokens: 40, TotalTokens: 90}},
				{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "done"}},
			},
			model.Message{Role: model.RoleAssistant, Content: "done"},
			nil,
		),
	}}
	runner, err := NewRunner(client, registry, Options{Model: "test-model"})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	result, err := runner.Run(context.Background(), RunInput{Messages: []model.Message{{Role: model.RoleUser, Content: "weather?"}}, Tools: registry.List()})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Usage.PromptTokens != 150 || result.Usage.CachedPromptTokens != 30 || result.Usage.CompletionTokens != 70 || result.Usage.TotalTokens != 220 {
		t.Fatalf("Usage = %#v, want aggregated tokens", result.Usage)
	}
}

func TestRunnerCalculatesRunCostFromLLMModelPricing(t *testing.T) {
	inputCost := 1.25
	cachedInputCost := 0.125
	outputCost := 10.0
	runner, err := NewRunner(&stubClient{streams: []model.Stream{newStubStream(
		[]model.StreamEvent{
			{Type: model.StreamEventUsage, Usage: model.TokenUsage{PromptTokens: 2000000, CachedPromptTokens: 500000, CompletionTokens: 3000000, TotalTokens: 5000000}},
			{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "done"}},
		},
		model.Message{Role: model.RoleAssistant, Content: "done"},
		nil,
	)}}, nil, Options{LLMModel: &coretypes.LLMModel{
		BaseModel: coretypes.BaseModel{ID: "gpt-5.4", Name: "GPT 5.4"},
		Cost:      coretypes.LLMCostConfig{Input: &inputCost, CachedInput: &cachedInputCost, Output: &outputCost},
	}})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	result, err := runner.Run(context.Background(), RunInput{Messages: []model.Message{{Role: model.RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Cost == nil {
		t.Fatal("Cost = nil, want non-nil")
	}
	if result.Cost.UncachedPromptTokens != 1500000 || result.Cost.CachedPromptTokens != 500000 || result.Cost.CompletionTokens != 3000000 {
		t.Fatalf("Cost = %#v, want aggregated breakdown tokens", result.Cost)
	}
}

func TestTaskRuntimeSinkMapsStepEvents(t *testing.T) {
	sink := &taskRuntimeSink{runtime: &recordingTaskRuntime{}}
	if err := sink.OnStepStart(context.Background(), StepEvent{Step: 2, Title: "Agent step 2"}); err != nil {
		t.Fatalf("OnStepStart() error = %v", err)
	}
	if err := sink.OnStepFinish(context.Background(), StepEvent{Step: 2, Title: "Agent step 2", Metadata: map[string]any{"ok": true}}); err != nil {
		t.Fatalf("OnStepFinish() error = %v", err)
	}
	recorder := sink.runtime.(*recordingTaskRuntime)
	if len(recorder.started) != 1 || recorder.started[0] != "agent.step.2|Agent step 2" {
		t.Fatalf("started = %#v, want mapped step start", recorder.started)
	}
	if len(recorder.finished) != 1 {
		t.Fatalf("finished = %#v, want one finish payload", recorder.finished)
	}
}

func TestTaskRuntimeSinkMapsToolEvents(t *testing.T) {
	recorder := &recordingTaskRuntime{}
	sink := &taskRuntimeSink{runtime: recorder}
	if err := sink.OnToolStart(context.Background(), ToolEvent{Step: 1, ToolCallID: "call_1", ToolName: "lookup_weather"}); err != nil {
		t.Fatalf("OnToolStart() error = %v", err)
	}
	if err := sink.OnToolFinish(context.Background(), ToolEvent{Step: 1, ToolCallID: "call_1", ToolName: "lookup_weather", Output: "sunny"}); err != nil {
		t.Fatalf("OnToolFinish() error = %v", err)
	}
	if len(recorder.emits) != 2 {
		t.Fatalf("emit count = %d, want 2", len(recorder.emits))
	}
	if recorder.emits[0].eventType != coretasks.EventToolStarted || recorder.emits[1].eventType != coretasks.EventToolFinished {
		t.Fatalf("emits = %#v, want tool started/finished", recorder.emits)
	}
}

func TestTaskRuntimeSinkMapsLogEvents(t *testing.T) {
	recorder := &recordingTaskRuntime{}
	sink := &taskRuntimeSink{runtime: recorder}
	if err := sink.OnLog(context.Background(), LogEvent{Level: "info", Message: "hello"}); err != nil {
		t.Fatalf("OnLog() error = %v", err)
	}
	if len(recorder.emits) != 1 {
		t.Fatalf("emit count = %d, want 1", len(recorder.emits))
	}
	if recorder.emits[0].eventType != coretasks.EventLogMessage || recorder.emits[0].level != "info" {
		t.Fatalf("emits = %#v, want log.message/info", recorder.emits)
	}
}

func TestTaskRuntimeSinkMapsRunStreamEventsToLogMessages(t *testing.T) {
	recorder := &recordingTaskRuntime{}
	sink := &taskRuntimeSink{runtime: recorder}
	if err := sink.OnStreamEvent(context.Background(), RunStreamEvent{Kind: EventTextDelta, Step: 1, Text: "he"}); err != nil {
		t.Fatalf("OnStreamEvent() error = %v", err)
	}
	if len(recorder.emits) != 1 {
		t.Fatalf("emit count = %d, want 1", len(recorder.emits))
	}
	if recorder.emits[0].eventType != coretasks.EventLogMessage {
		t.Fatalf("event type = %q, want log.message", recorder.emits[0].eventType)
	}
}

type stubClient struct {
	responses      []model.ChatResponse
	streams        []model.Stream
	streamErrs     []error
	err            error
	requests       []model.ChatRequest
	streamRequests []model.ChatRequest
}

func (s *stubClient) Chat(_ context.Context, req model.ChatRequest) (model.ChatResponse, error) {
	s.requests = append(s.requests, req)
	if s.err != nil {
		return model.ChatResponse{}, s.err
	}
	if len(s.responses) == 0 {
		return model.ChatResponse{}, nil
	}
	response := s.responses[0]
	s.responses = s.responses[1:]
	return response, nil
}

func (s *stubClient) ChatStream(_ context.Context, req model.ChatRequest) (model.Stream, error) {
	s.streamRequests = append(s.streamRequests, req)
	return s.nextStream()
}

func (s *stubClient) nextStream() (model.Stream, error) {
	if len(s.streamErrs) > 0 {
		err := s.streamErrs[0]
		s.streamErrs = s.streamErrs[1:]
		if err != nil {
			return nil, err
		}
	}
	if s.err != nil {
		return nil, s.err
	}
	if len(s.streams) == 0 {
		return nil, errors.New("no stream prepared")
	}
	stream := s.streams[0]
	s.streams = s.streams[1:]
	return stream, nil
}

type recordingEventSink struct {
	events []string
}

func (r *recordingEventSink) OnStepStart(context.Context, StepEvent) error {
	r.events = append(r.events, "step.start")
	return nil
}

func (r *recordingEventSink) OnStepFinish(context.Context, StepEvent) error {
	r.events = append(r.events, "step.finish")
	return nil
}

func (r *recordingEventSink) OnToolStart(context.Context, ToolEvent) error {
	r.events = append(r.events, "tool.start")
	return nil
}

func (r *recordingEventSink) OnToolFinish(context.Context, ToolEvent) error {
	r.events = append(r.events, "tool.finish")
	return nil
}

func (r *recordingEventSink) OnLog(context.Context, LogEvent) error {
	r.events = append(r.events, "log")
	return nil
}

type recordingTaskRuntime struct {
	taskID        string
	started       []string
	finished      []any
	emits         []recordedEmit
	metadata      map[string]any
	suspendReason string
	approvals     []*approvals.ToolApproval
}

type toolArgumentsAuditArtifact struct {
	ToolCallID string `json:"tool_call_id,omitempty"`
	ToolName   string `json:"tool_name"`
	Arguments  string `json:"arguments,omitempty"`
}

type toolOutputAuditArtifact struct {
	ToolCallID string `json:"tool_call_id,omitempty"`
	ToolName   string `json:"tool_name"`
	Output     string `json:"output,omitempty"`
	Error      string `json:"error,omitempty"`
}

type recordedEmit struct {
	eventType string
	level     string
	payload   any
}

type recordingRunnerAuditRecorder struct {
	mu               sync.Mutex
	eventsByRunID    map[string][]*coreaudit.Event
	artifactsByRunID map[string][]*coreaudit.Artifact
	seqByRunID       map[string]int64
}

func newRecordingRunnerAuditRecorder() *recordingRunnerAuditRecorder {
	return &recordingRunnerAuditRecorder{
		eventsByRunID:    make(map[string][]*coreaudit.Event),
		artifactsByRunID: make(map[string][]*coreaudit.Artifact),
		seqByRunID:       make(map[string]int64),
	}
}

func (r *recordingRunnerAuditRecorder) StartRun(_ context.Context, input coreaudit.StartRunInput) (*coreaudit.Run, error) {
	return &coreaudit.Run{ID: input.RunID, TaskID: input.TaskID, TaskType: input.TaskType}, nil
}

func (r *recordingRunnerAuditRecorder) AppendEvent(_ context.Context, runID string, input coreaudit.AppendEventInput) (*coreaudit.Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	payloadJSON, err := json.Marshal(input.Payload)
	if err != nil {
		return nil, err
	}
	r.seqByRunID[runID]++
	event := &coreaudit.Event{
		RunID:         runID,
		Seq:           r.seqByRunID[runID],
		Phase:         input.Phase,
		EventType:     input.EventType,
		Level:         input.Level,
		StepIndex:     input.StepIndex,
		ParentSeq:     input.ParentSeq,
		RefArtifactID: input.RefArtifactID,
		PayloadJSON:   payloadJSON,
	}
	r.eventsByRunID[runID] = append(r.eventsByRunID[runID], event)
	return cloneAuditEvent(event), nil
}

func (r *recordingRunnerAuditRecorder) AttachArtifact(_ context.Context, runID string, input coreaudit.CreateArtifactInput) (*coreaudit.Artifact, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	bodyJSON, err := json.Marshal(input.Body)
	if err != nil {
		return nil, err
	}
	artifact := &coreaudit.Artifact{
		ID:       firstNonEmpty(input.ArtifactID, fmt.Sprintf("art_%d", len(r.artifactsByRunID[runID])+1)),
		RunID:    runID,
		Kind:     input.Kind,
		MimeType: input.MimeType,
		Encoding: input.Encoding,
		BodyJSON: bodyJSON,
	}
	r.artifactsByRunID[runID] = append(r.artifactsByRunID[runID], artifact)
	return cloneAuditArtifact(artifact), nil
}

func (r *recordingRunnerAuditRecorder) FinishRun(context.Context, string, coreaudit.FinishRunInput) error {
	return nil
}

func (r *recordingRunnerAuditRecorder) requireEventForStep(t *testing.T, runID string, eventType string, step int) *coreaudit.Event {
	t.Helper()

	r.mu.Lock()
	defer r.mu.Unlock()
	for _, event := range r.eventsByRunID[runID] {
		if event.EventType == eventType && event.StepIndex == step {
			return cloneAuditEvent(event)
		}
	}
	t.Fatalf("run %q did not record audit event %q for step %d", runID, eventType, step)
	return nil
}

func (r *recordingRunnerAuditRecorder) requireArtifactByKind(t *testing.T, runID string, kind coreaudit.ArtifactKind) *coreaudit.Artifact {
	t.Helper()

	artifacts := r.requireArtifactsByKind(t, runID, kind)
	return artifacts[0]
}

func (r *recordingRunnerAuditRecorder) requireArtifactsByKind(t *testing.T, runID string, kind coreaudit.ArtifactKind) []*coreaudit.Artifact {
	t.Helper()

	r.mu.Lock()
	defer r.mu.Unlock()
	var artifacts []*coreaudit.Artifact
	for _, artifact := range r.artifactsByRunID[runID] {
		if artifact.Kind == kind {
			artifacts = append(artifacts, cloneAuditArtifact(artifact))
		}
	}
	if len(artifacts) == 0 {
		t.Fatalf("run %q did not record artifact kind %q", runID, kind)
	}
	return artifacts
}

func (r *recordingRunnerAuditRecorder) eventTypes(runID string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	events := r.eventsByRunID[runID]
	result := make([]string, 0, len(events))
	for _, event := range events {
		result = append(result, event.EventType)
	}
	return result
}

func assertRunnerAuditEventTypes(t *testing.T, recorder *recordingRunnerAuditRecorder, runID string, want ...string) {
	t.Helper()

	got := recorder.eventTypes(runID)
	if len(got) != len(want) {
		t.Fatalf("audit event count = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("audit events = %v, want %v", got, want)
		}
	}
}

func assertMessagesDoNotContainContent(t *testing.T, messages []model.Message, forbidden string) {
	t.Helper()

	for _, message := range messages {
		if message.Content == forbidden {
			t.Fatalf("messages = %#v, want no message with content %q", messages, forbidden)
		}
	}
}

func decodeModelRequestArtifact(t *testing.T, artifact *coreaudit.Artifact) model.ChatRequest {
	t.Helper()

	var request model.ChatRequest
	if err := json.Unmarshal(artifact.BodyJSON, &request); err != nil {
		t.Fatalf("decode model_request artifact error = %v", err)
	}
	return request
}

func decodeToolArgumentsArtifact(t *testing.T, artifact *coreaudit.Artifact) toolArgumentsAuditArtifact {
	t.Helper()

	var snapshot toolArgumentsAuditArtifact
	if err := json.Unmarshal(artifact.BodyJSON, &snapshot); err != nil {
		t.Fatalf("decode tool_arguments artifact error = %v", err)
	}
	return snapshot
}

func decodeToolOutputArtifact(t *testing.T, artifact *coreaudit.Artifact) toolOutputAuditArtifact {
	t.Helper()

	var snapshot toolOutputAuditArtifact
	if err := json.Unmarshal(artifact.BodyJSON, &snapshot); err != nil {
		t.Fatalf("decode tool_output artifact error = %v", err)
	}
	return snapshot
}

func (r *recordingTaskRuntime) StartStep(_ context.Context, key string, title string) error {
	r.started = append(r.started, key+"|"+title)
	return nil
}

func (r *recordingTaskRuntime) FinishStep(_ context.Context, payload any) error {
	r.finished = append(r.finished, payload)
	return nil
}

func (r *recordingTaskRuntime) Emit(_ context.Context, eventType string, level string, payload any) error {
	r.emits = append(r.emits, recordedEmit{eventType: eventType, level: level, payload: payload})
	return nil
}

func (r *recordingTaskRuntime) TaskID() string {
	return r.taskID
}

func (r *recordingTaskRuntime) GetTask(context.Context) (*coretasks.Task, error) {
	metadataJSON, err := json.Marshal(r.metadata)
	if err != nil {
		return nil, err
	}
	return &coretasks.Task{ID: r.taskID, MetadataJSON: metadataJSON}, nil
}

func (r *recordingTaskRuntime) UpdateMetadata(_ context.Context, metadata any) error {
	raw, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, &r.metadata)
}

func (r *recordingTaskRuntime) Suspend(_ context.Context, reason string) error {
	r.suspendReason = reason
	return nil
}

func (r *recordingTaskRuntime) CreateApproval(_ context.Context, input approvals.CreateApprovalInput) (*approvals.ToolApproval, error) {
	approval := &approvals.ToolApproval{
		ID:               fmt.Sprintf("approval_%d", len(r.approvals)+1),
		TaskID:           input.TaskID,
		ConversationID:   input.ConversationID,
		StepIndex:        input.StepIndex,
		ToolCallID:       input.ToolCallID,
		ToolName:         input.ToolName,
		ArgumentsSummary: input.ArgumentsSummary,
		RiskLevel:        input.RiskLevel,
		Reason:           input.Reason,
		Status:           approvals.StatusPending,
	}
	r.approvals = append(r.approvals, approval)
	r.emits = append(r.emits, recordedEmit{eventType: coretasks.EventApprovalRequested, level: "info", payload: approval})
	return approval, nil
}

func (r *recordingTaskRuntime) GetApproval(_ context.Context, approvalID string) (*approvals.ToolApproval, error) {
	for _, approval := range r.approvals {
		if approval.ID == approvalID {
			copy := *approval
			return &copy, nil
		}
	}
	return nil, fmt.Errorf("approval %q not found", approvalID)
}

func (r *recordingTaskRuntime) ExpireApproval(_ context.Context, approvalID string, reason string) (*approvals.ToolApproval, error) {
	for _, approval := range r.approvals {
		if approval.ID != approvalID {
			continue
		}
		approval.Status = approvals.StatusExpired
		approval.DecisionReason = reason
		copy := *approval
		return &copy, nil
	}
	return nil, fmt.Errorf("approval %q not found", approvalID)
}

func (r *recordingTaskRuntime) ToolContext(ctx context.Context, stepID string) context.Context {
	return coretools.WithRuntime(ctx, &coretools.Runtime{TaskID: r.taskID, StepID: stepID, Actor: "runner-test"})
}

func newTestRegistry(t *testing.T, handlers map[string]func(context.Context, map[string]interface{}) (string, error)) *coretools.Registry {
	t.Helper()
	registry := coretools.NewRegistry()
	for name, handler := range handlers {
		if err := registry.Register(coretools.Tool{Name: name, Handler: handler}); err != nil {
			t.Fatalf("Register(%q) error = %v", name, err)
		}
	}
	return registry
}

type stubStream struct {
	events     []model.StreamEvent
	final      model.Message
	finalErr   error
	closed     bool
	allowFinal <-chan struct{}
}

func newStubStream(events []model.StreamEvent, final model.Message, finalErr error) model.Stream {
	cloned := make([]model.StreamEvent, len(events))
	copy(cloned, events)
	return &stubStream{events: cloned, final: final, finalErr: finalErr}
}

func newBlockingFinalStream(events []model.StreamEvent, final model.Message, allow <-chan struct{}, finalErr error) model.Stream {
	cloned := make([]model.StreamEvent, len(events))
	copy(cloned, events)
	return &stubStream{events: cloned, final: final, finalErr: finalErr, allowFinal: allow}
}

func (s *stubStream) Recv() (string, error) {
	if len(s.events) == 0 {
		return "", nil
	}
	event := s.events[0]
	s.events = s.events[1:]
	return event.Text, nil
}

func (s *stubStream) RecvEvent() (model.StreamEvent, error) {
	if len(s.events) == 0 {
		return model.StreamEvent{}, nil
	}
	event := s.events[0]
	s.events = s.events[1:]
	return event, nil
}

func (s *stubStream) FinalMessage() (model.Message, error) {
	if s.allowFinal != nil {
		<-s.allowFinal
	}
	if s.finalErr != nil {
		return model.Message{}, s.finalErr
	}
	return s.final, nil
}

func (s *stubStream) Close() error {
	s.closed = true
	return nil
}

func (s *stubStream) Context() context.Context {
	return context.Background()
}

func (s *stubStream) Stats() *model.StreamStats {
	return &model.StreamStats{}
}

func (s *stubStream) ToolCalls() []coretypes.ToolCall {
	return nil
}

func (s *stubStream) ResponseType() model.StreamResponseType {
	return model.StreamResponseUnknown
}

func (s *stubStream) FinishReason() string {
	return "stop"
}

func (s *stubStream) Reasoning() string {
	return ""
}
