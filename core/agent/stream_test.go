package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	coreaudit "github.com/EquentR/agent_runtime/core/audit"
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

func TestRunnerRunStreamRecordsPromptAndModelAuditArtifacts(t *testing.T) {
	recorder := newRecordingRunnerAuditRecorder()
	client := &stubClient{
		streams: []model.Stream{newStubStream(
			[]model.StreamEvent{
				{Type: model.StreamEventUsage, Usage: model.TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}},
				{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "hello"}},
			},
			model.Message{Role: model.RoleAssistant, Content: "hello"},
			nil,
		)},
	}
	runner, err := NewRunner(client, nil, Options{
		Model:         "test-model",
		SystemPrompt:  "You are helpful.",
		AuditRecorder: recorder,
		AuditRunID:    "run_stream_1",
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	streamResult, err := runner.RunStream(context.Background(), RunInput{Messages: []model.Message{{Role: model.RoleUser, Content: "say hi"}}})
	if err != nil {
		t.Fatalf("RunStream() error = %v", err)
	}
	for range streamResult.Events {
	}
	result, err := streamResult.Wait()
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.FinalMessage.Content != "hello" {
		t.Fatalf("FinalMessage.Content = %q, want hello", result.FinalMessage.Content)
	}

	assertRunnerAuditEventTypes(t, recorder, "run_stream_1",
		"step.started",
		"prompt.resolved",
		"request.built",
		"model.completed",
		"step.finished",
	)

	promptEvent := recorder.requireEventForStep(t, "run_stream_1", "prompt.resolved", 1)
	promptPayload := decodeAuditPayload(t, promptEvent)
	if _, ok := promptPayload["messages"]; ok {
		t.Fatalf("prompt payload = %#v, want compact payload without messages", promptPayload)
	}
	if promptPayload["message_count"] != float64(2) {
		t.Fatalf("prompt payload = %#v, want message_count=2", promptPayload)
	}

	promptArtifact := recorder.requireArtifactByKind(t, "run_stream_1", coreaudit.ArtifactKindResolvedPrompt)
	prompt := decodeResolvedPromptArtifact(t, promptArtifact)
	if len(prompt.Messages) != 2 {
		t.Fatalf("resolved prompt message count = %d, want 2", len(prompt.Messages))
	}
	if prompt.Messages[0].Role != model.RoleSystem || prompt.Messages[0].Content != "You are helpful." {
		t.Fatalf("resolved prompt first message = %#v, want system prompt", prompt.Messages[0])
	}

	requestArtifact := recorder.requireArtifactByKind(t, "run_stream_1", coreaudit.ArtifactKindModelRequest)
	request := decodeModelRequestArtifact(t, requestArtifact)
	if request.Model != "test-model" {
		t.Fatalf("request.Model = %q, want test-model", request.Model)
	}
	if len(request.Messages) != 2 {
		t.Fatalf("request message count = %d, want 2", len(request.Messages))
	}

	modelCompleted := recorder.requireEventForStep(t, "run_stream_1", "model.completed", 1)
	modelPayload := decodeAuditPayload(t, modelCompleted)
	if _, ok := modelPayload["message"]; ok {
		t.Fatalf("model.completed payload = %#v, want compact payload without assistant body", modelPayload)
	}
	if modelPayload["usage_total_tokens"] != float64(15) {
		t.Fatalf("model.completed payload = %#v, want usage_total_tokens=15", modelPayload)
	}

	responseArtifact := recorder.requireArtifactByKind(t, "run_stream_1", coreaudit.ArtifactKindModelResponse)
	response := decodeModelResponseArtifact(t, responseArtifact)
	if response.Message.Content != "hello" {
		t.Fatalf("response message = %#v, want content hello", response.Message)
	}
	if response.Usage.TotalTokens != 15 {
		t.Fatalf("response usage = %#v, want total tokens 15", response.Usage)
	}
}

func TestRunnerRunStreamDoesNotReusePreviousStepUsageForModelCompletedAudit(t *testing.T) {
	registry := newTestRegistry(t, map[string]func(context.Context, map[string]interface{}) (string, error){
		"lookup_weather": func(context.Context, map[string]interface{}) (string, error) {
			return "sunny", nil
		},
	})
	recorder := newRecordingRunnerAuditRecorder()
	client := &stubClient{streams: []model.Stream{
		newStubStream(
			[]model.StreamEvent{
				{Type: model.StreamEventUsage, Usage: model.TokenUsage{PromptTokens: 9, CompletionTokens: 6, TotalTokens: 15}},
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
	}}
	runner, err := NewRunner(client, registry, Options{
		Model:         "test-model",
		AuditRecorder: recorder,
		AuditRunID:    "run_usage_reset_1",
	})
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
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}

	stepOne := recorder.requireEventForStep(t, "run_usage_reset_1", "model.completed", 1)
	stepOnePayload := decodeAuditPayload(t, stepOne)
	if got := int(stepOnePayload["usage_total_tokens"].(float64)); got != 15 {
		t.Fatalf("step 1 usage_total_tokens = %d, want 15", got)
	}

	stepTwo := recorder.requireEventForStep(t, "run_usage_reset_1", "model.completed", 2)
	stepTwoPayload := decodeAuditPayload(t, stepTwo)
	if got := int(stepTwoPayload["usage_total_tokens"].(float64)); got != 0 {
		t.Fatalf("step 2 usage_total_tokens = %d, want 0 when step emits no usage event", got)
	}

	responseArtifacts := recorder.requireArtifactsByKind(t, "run_usage_reset_1", coreaudit.ArtifactKindModelResponse)
	if len(responseArtifacts) != 2 {
		t.Fatalf("model response artifact count = %d, want 2", len(responseArtifacts))
	}
	stepTwoResponse := decodeModelResponseArtifact(t, responseArtifacts[1])
	if stepTwoResponse.Usage.TotalTokens != 0 {
		t.Fatalf("step 2 response usage = %#v, want zero usage", stepTwoResponse.Usage)
	}
}

func TestRunnerRunStreamReturnsErrorWhenModelEmitsToolCallsWithoutRegistry(t *testing.T) {
	client := &stubClient{streams: []model.Stream{newStubStream(
		[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: `{"city":"Shanghai"}`}}}}},
		model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: `{"city":"Shanghai"}`}}},
		nil,
	)}}
	runner, err := NewRunner(client, nil, Options{Model: "test-model"})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	streamResult, err := runner.RunStream(context.Background(), RunInput{Messages: []model.Message{{Role: model.RoleUser, Content: "weather?"}}})
	if err != nil {
		t.Fatalf("RunStream() error = %v", err)
	}
	for range streamResult.Events {
	}
	_, err = streamResult.Wait()
	if err == nil {
		t.Fatal("Wait() error = nil, want missing registry error")
	}
	if err.Error() != "tool registry is required when model emits tool calls" {
		t.Fatalf("Wait() error = %v, want missing registry error", err)
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

type resolvedPromptAuditArtifact struct {
	Messages []model.Message `json:"messages"`
}

type modelResponseAuditArtifact struct {
	Message model.Message    `json:"message"`
	Usage   model.TokenUsage `json:"usage"`
}

func decodeResolvedPromptArtifact(t *testing.T, artifact *coreaudit.Artifact) resolvedPromptAuditArtifact {
	t.Helper()

	var snapshot resolvedPromptAuditArtifact
	if err := json.Unmarshal(artifact.BodyJSON, &snapshot); err != nil {
		t.Fatalf("decode resolved_prompt artifact error = %v", err)
	}
	return snapshot
}

func decodeModelResponseArtifact(t *testing.T, artifact *coreaudit.Artifact) modelResponseAuditArtifact {
	t.Helper()

	var snapshot modelResponseAuditArtifact
	if err := json.Unmarshal(artifact.BodyJSON, &snapshot); err != nil {
		t.Fatalf("decode model_response artifact error = %v", err)
	}
	return snapshot
}
