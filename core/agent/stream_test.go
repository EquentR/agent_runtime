package agent

import (
	"context"
	"errors"
	"sync"
	"testing"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	coretypes "github.com/EquentR/agent_runtime/core/types"
)

func TestRunnerRunStreamEmitsTextAndCompletedEvents(t *testing.T) {
	client := &stubClient{
		streams: []model.Stream{newStubStream(
			[]model.StreamEvent{
				{Type: model.StreamEventTextDelta, Text: "hel"},
				{Type: model.StreamEventTextDelta, Text: "lo"},
				{Type: model.StreamEventUsage, Usage: model.TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}},
				{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "hello"}},
			},
			model.Message{Role: model.RoleAssistant, Content: "hello"},
			nil,
		)},
	}
	runner, err := NewRunner(client, nil, Options{Model: "test-model"})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	streamResult, err := runner.RunStream(context.Background(), RunInput{Messages: []model.Message{{Role: model.RoleUser, Content: "say hi"}}})
	if err != nil {
		t.Fatalf("RunStream() error = %v", err)
	}

	var got []StreamEventKind
	for event := range streamResult.Events {
		got = append(got, event.Kind)
	}
	result, err := streamResult.Wait()
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if len(got) < 4 {
		t.Fatalf("event count = %d, want at least 4", len(got))
	}
	if got[0] != EventTextDelta || got[1] != EventTextDelta || got[2] != EventUsage || got[3] != EventCompleted {
		t.Fatalf("event kinds = %#v, want text/text/usage/completed prefix", got)
	}
	if result.FinalMessage.Content != "hello" {
		t.Fatalf("final content = %q, want hello", result.FinalMessage.Content)
	}
}

func TestRunnerRunUsesRunStreamAggregation(t *testing.T) {
	client := &stubClient{
		streams: []model.Stream{newStubStream(
			[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "hello"}}},
			model.Message{Role: model.RoleAssistant, Content: "hello"},
			nil,
		)},
	}
	runner, err := NewRunner(client, nil, Options{Model: "test-model"})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	result, err := runner.Run(context.Background(), RunInput{Messages: []model.Message{{Role: model.RoleUser, Content: "say hi"}}})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.FinalMessage.Content != "hello" {
		t.Fatalf("final content = %q, want hello", result.FinalMessage.Content)
	}
	if len(client.streamRequests) != 1 {
		t.Fatalf("stream request count = %d, want 1", len(client.streamRequests))
	}
	if len(client.requests) != 0 {
		t.Fatalf("chat request count = %d, want 0", len(client.requests))
	}
}

func TestRunnerRunStreamCollectsToolCallDeltasBeforeExecution(t *testing.T) {
	var executed bool
	registry := newTestRegistry(t, map[string]func(context.Context, map[string]interface{}) (string, error){
		"lookup_weather": func(context.Context, map[string]interface{}) (string, error) {
			executed = true
			return "sunny", nil
		},
	})

	client := &stubClient{
		streams: []model.Stream{
			newStubStream(
				[]model.StreamEvent{
					{Type: model.StreamEventToolCallDelta, ToolCall: coretypes.ToolCall{ID: "call_1", Name: "lookup_weather", Arguments: `{"city":"Shang`}},
					{Type: model.StreamEventToolCallDelta, ToolCall: coretypes.ToolCall{ID: "call_1", Name: "lookup_weather", Arguments: `{"city":"Shanghai"}`}},
					{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: `{"city":"Shanghai"}`}}}},
				},
				model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: `{"city":"Shanghai"}`}}},
				nil,
			),
			newStubStream(
				[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "done"}}},
				model.Message{Role: model.RoleAssistant, Content: "done"},
				nil,
			),
		},
	}
	runner, err := NewRunner(client, registry, Options{Model: "test-model"})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	streamResult, err := runner.RunStream(context.Background(), RunInput{Messages: []model.Message{{Role: model.RoleUser, Content: "weather?"}}, Tools: registry.List()})
	if err != nil {
		t.Fatalf("RunStream() error = %v", err)
	}
	for range streamResult.Events {
	}
	result, err := streamResult.Wait()
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if !executed {
		t.Fatal("tool executed = false, want true after final message")
	}
	if result.FinalMessage.Content != "done" {
		t.Fatalf("final content = %q, want done", result.FinalMessage.Content)
	}
}

func TestRunnerRunStreamRequiresFinalMessageBeforeToolExecution(t *testing.T) {
	var mu sync.Mutex
	var executed bool
	allowFinal := make(chan struct{})
	registry := newTestRegistry(t, map[string]func(context.Context, map[string]interface{}) (string, error){
		"lookup_weather": func(context.Context, map[string]interface{}) (string, error) {
			mu.Lock()
			executed = true
			mu.Unlock()
			return "sunny", nil
		},
	})
	client := &stubClient{streams: []model.Stream{
		newBlockingFinalStream(
			[]model.StreamEvent{
				{Type: model.StreamEventToolCallDelta, ToolCall: coretypes.ToolCall{ID: "call_1", Name: "lookup_weather", Arguments: `{"city":"Shanghai"}`}},
				{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: `{"city":"Shanghai"}`}}}},
			},
			model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: `{"city":"Shanghai"}`}}},
			allowFinal,
			nil,
		),
		newStubStream(
			[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "done"}}},
			model.Message{Role: model.RoleAssistant, Content: "done"},
			nil,
		),
	}}
	runner, err := NewRunner(client, registry, Options{Model: "test-model"})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	streamResult, err := runner.RunStream(context.Background(), RunInput{Messages: []model.Message{{Role: model.RoleUser, Content: "weather?"}}, Tools: registry.List()})
	if err != nil {
		t.Fatalf("RunStream() error = %v", err)
	}
	seenCompleted := false
	for event := range streamResult.Events {
		if event.Kind == EventCompleted && !seenCompleted {
			seenCompleted = true
			mu.Lock()
			alreadyExecuted := executed
			mu.Unlock()
			if alreadyExecuted {
				t.Fatal("tool executed before FinalMessage() was allowed to continue")
			}
			close(allowFinal)
		}
	}
	_, err = streamResult.Wait()
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !executed {
		t.Fatal("tool executed = false, want true")
	}
}

func TestRunnerRunStreamReturnsErrorWhenFinalMessageUnavailable(t *testing.T) {
	registry := newTestRegistry(t, map[string]func(context.Context, map[string]interface{}) (string, error){
		"lookup_weather": func(context.Context, map[string]interface{}) (string, error) {
			return "sunny", nil
		},
	})
	client := &stubClient{streams: []model.Stream{newStubStream(
		[]model.StreamEvent{{Type: model.StreamEventToolCallDelta, ToolCall: coretypes.ToolCall{ID: "call_1", Name: "lookup_weather", Arguments: `{"city":"Shanghai"}`}}},
		model.Message{},
		errors.New("final unavailable"),
	)}}
	runner, err := NewRunner(client, registry, Options{Model: "test-model"})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	streamResult, err := runner.RunStream(context.Background(), RunInput{Messages: []model.Message{{Role: model.RoleUser, Content: "weather?"}}, Tools: registry.List()})
	if err != nil {
		t.Fatalf("RunStream() error = %v", err)
	}
	for range streamResult.Events {
	}
	_, err = streamResult.Wait()
	if err == nil {
		t.Fatal("Wait() error = nil, want final message error")
	}
}

func TestRunnerRunStreamIncludesRegistryToolsWhenInputToolsEmpty(t *testing.T) {
	registry := newTestRegistry(t, map[string]func(context.Context, map[string]interface{}) (string, error){
		"read_file": func(context.Context, map[string]interface{}) (string, error) {
			return "line 3", nil
		},
	})
	client := &stubClient{
		streams: []model.Stream{newStubStream(
			[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "done"}}},
			model.Message{Role: model.RoleAssistant, Content: "done"},
			nil,
		)},
	}
	runner, err := NewRunner(client, registry, Options{Model: "test-model"})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	streamResult, err := runner.RunStream(context.Background(), RunInput{Messages: []model.Message{{Role: model.RoleUser, Content: "read README"}}})
	if err != nil {
		t.Fatalf("RunStream() error = %v", err)
	}
	for range streamResult.Events {
	}
	_, err = streamResult.Wait()
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if len(client.streamRequests) != 1 {
		t.Fatalf("stream request count = %d, want 1", len(client.streamRequests))
	}
	tools := client.streamRequests[0].Tools
	if len(tools) != 1 {
		t.Fatalf("request tools = %#v, want exactly one registry tool", tools)
	}
	if tools[0].Name != "read_file" {
		t.Fatalf("request tool name = %q, want read_file", tools[0].Name)
	}
}

func TestRunnerRunStreamMergesRegistryToolsWithInputTools(t *testing.T) {
	registry := newTestRegistry(t, map[string]func(context.Context, map[string]interface{}) (string, error){
		"read_file": func(context.Context, map[string]interface{}) (string, error) {
			return "line 3", nil
		},
	})
	client := &stubClient{
		streams: []model.Stream{newStubStream(
			[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "done"}}},
			model.Message{Role: model.RoleAssistant, Content: "done"},
			nil,
		)},
	}
	runner, err := NewRunner(client, registry, Options{Model: "test-model"})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	inputTools := []coretypes.Tool{{Name: "lookup_weather"}}
	streamResult, err := runner.RunStream(context.Background(), RunInput{
		Messages: []model.Message{{Role: model.RoleUser, Content: "read README and weather"}},
		Tools:    inputTools,
	})
	if err != nil {
		t.Fatalf("RunStream() error = %v", err)
	}
	for range streamResult.Events {
	}
	_, err = streamResult.Wait()
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if len(client.streamRequests) != 1 {
		t.Fatalf("stream request count = %d, want 1", len(client.streamRequests))
	}
	tools := client.streamRequests[0].Tools
	if len(tools) != 2 {
		t.Fatalf("request tools = %#v, want merged registry and input tools", tools)
	}
	if tools[0].Name != "read_file" && tools[1].Name != "read_file" {
		t.Fatalf("request tools = %#v, want read_file included", tools)
	}
	if tools[0].Name != "lookup_weather" && tools[1].Name != "lookup_weather" {
		t.Fatalf("request tools = %#v, want lookup_weather included", tools)
	}
}
