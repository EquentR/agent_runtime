package agent

import (
	"context"
	"errors"
	"testing"

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
	started  []string
	finished []any
	emits    []recordedEmit
}

type recordedEmit struct {
	eventType string
	level     string
	payload   any
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
