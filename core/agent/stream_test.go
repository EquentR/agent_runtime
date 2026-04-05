package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	coreaudit "github.com/EquentR/agent_runtime/core/audit"
	coreprompt "github.com/EquentR/agent_runtime/core/prompt"
	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/runtimeprompt"
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

func TestRunnerRunStreamRecordsRuntimePromptEnvelopeArtifact(t *testing.T) {
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
		Model:                "test-model",
		SystemPrompt:         "You are helpful.",
		RuntimePromptBuilder: runtimeprompt.NewBuilder(nil),
		AuditRecorder:        recorder,
		AuditRunID:           "run_stream_1",
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
	if _, ok := promptPayload["segments"]; ok {
		t.Fatalf("prompt payload = %#v, want compact payload without segments", promptPayload)
	}
	if promptPayload["message_count"] != float64(2) {
		t.Fatalf("prompt payload = %#v, want message_count=2", promptPayload)
	}
	if promptPayload["prompt_message_count"] != float64(1) {
		t.Fatalf("prompt payload = %#v, want prompt_message_count=1", promptPayload)
	}
	if promptPayload["segment_count"] != float64(1) {
		t.Fatalf("prompt payload = %#v, want segment_count=1", promptPayload)
	}
	phaseCounts := requireAuditCountMap(t, promptPayload, "phase_segment_counts")
	if phaseCounts["session"] != 1 {
		t.Fatalf("phase_segment_counts = %#v, want session=1", phaseCounts)
	}
	sourceCounts := requireAuditCountMap(t, promptPayload, "source_counts")
	if sourceCounts["resolved_prompt"] != 1 {
		t.Fatalf("source_counts = %#v, want resolved_prompt=1", sourceCounts)
	}
	if strings.Contains(string(promptEvent.PayloadJSON), "You are helpful.") {
		t.Fatalf("prompt payload json = %s, want compact payload without prompt body", string(promptEvent.PayloadJSON))
	}

	promptArtifact := recorder.requireArtifactByKind(t, "run_stream_1", coreaudit.ArtifactKindRuntimePromptEnvelope)
	envelope := decodeRuntimePromptEnvelopeArtifact(t, promptArtifact)
	if envelope.PromptMessageCount != 1 {
		t.Fatalf("runtime prompt prompt_message_count = %d, want 1", envelope.PromptMessageCount)
	}
	if len(envelope.Messages) != 2 {
		t.Fatalf("runtime prompt message count = %d, want 2", len(envelope.Messages))
	}
	if len(envelope.Segments) != 1 {
		t.Fatalf("runtime prompt segment count = %d, want 1", len(envelope.Segments))
	}
	if envelope.PhaseSegmentCounts["session"] != 1 {
		t.Fatalf("runtime prompt phase counts = %#v, want session=1", envelope.PhaseSegmentCounts)
	}
	if envelope.SourceCounts["resolved_prompt"] != 1 {
		t.Fatalf("runtime prompt source counts = %#v, want resolved_prompt=1", envelope.SourceCounts)
	}
	if envelope.Messages[0].Role != model.RoleSystem || envelope.Messages[0].Content != "You are helpful." {
		t.Fatalf("runtime prompt first message = %#v, want system prompt", envelope.Messages[0])
	}
	if envelope.Segments[0].Phase != runtimeprompt.PhaseSession || envelope.Segments[0].SourceType != runtimeprompt.SourceTypeResolvedPrompt || envelope.Segments[0].SourceKey != "legacy_system_prompt" {
		t.Fatalf("runtime prompt legacy segment = %#v, want session resolved prompt metadata", envelope.Segments[0])
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

func TestRunnerRunStreamLegacyPromptArtifactPreservesExactInjectedString(t *testing.T) {
	recorder := newRecordingRunnerAuditRecorder()
	client := &stubClient{
		streams: []model.Stream{newStubStream(
			[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "hello"}}},
			model.Message{Role: model.RoleAssistant, Content: "hello"},
			nil,
		)},
	}
	legacyPrompt := "  Keep exact spacing\n\n"
	runner, err := NewRunner(client, nil, Options{
		Model:                "test-model",
		SystemPrompt:         legacyPrompt,
		RuntimePromptBuilder: runtimeprompt.NewBuilder(nil),
		AuditRecorder:        recorder,
		AuditRunID:           "run_stream_legacy_exact",
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
	_, err = streamResult.Wait()
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}

	promptArtifact := recorder.requireArtifactByKind(t, "run_stream_legacy_exact", coreaudit.ArtifactKindRuntimePromptEnvelope)
	envelope := decodeRuntimePromptEnvelopeArtifact(t, promptArtifact)
	if len(envelope.Segments) != 1 || envelope.Segments[0].Content != legacyPrompt {
		t.Fatalf("runtime prompt segments = %#v, want exact legacy segment content %q", envelope.Segments, legacyPrompt)
	}
	if len(envelope.Messages) < 1 || envelope.Messages[0].Content != "Keep exact spacing" {
		t.Fatalf("runtime prompt messages = %#v, want rendered prompt message content normalized to %q while exact legacy text remains in segments", envelope.Messages, "Keep exact spacing")
	}

	requestArtifact := recorder.requireArtifactByKind(t, "run_stream_legacy_exact", coreaudit.ArtifactKindModelRequest)
	request := decodeModelRequestArtifact(t, requestArtifact)
	if len(request.Messages) < 1 || request.Messages[0].Content != "Keep exact spacing" {
		t.Fatalf("model request messages = %#v, want rendered request message content normalized to %q while exact legacy text remains in segments", request.Messages, "Keep exact spacing")
	}
}

func TestRunnerRunStreamRecordsPromptArtifactsPerStepWithPhaseAwareInjection(t *testing.T) {
	registry := newTestRegistry(t, map[string]func(context.Context, map[string]interface{}) (string, error){
		"lookup_weather": func(context.Context, map[string]interface{}) (string, error) {
			return "sunny", nil
		},
	})
	recorder := newRecordingRunnerAuditRecorder()
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
		Model:                "test-model",
		RuntimePromptBuilder: runtimeprompt.NewBuilder(nil),
		ResolvedPrompt: &coreprompt.ResolvedPrompt{
			Scene: "agent.run.review",
			Segments: []coreprompt.ResolvedPromptSegment{
				{Order: 1, Phase: "session", Content: "Session prompt", SourceKind: "db_default_binding", SourceRef: "binding:101", BindingID: 101, PromptID: "doc-session", PromptName: "Session prompt", PromptScope: "admin", Priority: 10},
				{Order: 2, Phase: "step_pre_model", Content: "Step prompt", SourceKind: "legacy_system_prompt", SourceRef: "system_prompt"},
				{Order: 3, Phase: "tool_result", Content: "Tool-result prompt", SourceKind: "workspace_file", SourceRef: "AGENTS.md", RuntimeOnly: true},
			},
			Session:      []model.Message{{Role: model.RoleSystem, Content: "Session prompt"}},
			StepPreModel: []model.Message{{Role: model.RoleSystem, Content: "Step prompt"}},
			ToolResult:   []model.Message{{Role: model.RoleSystem, Content: "Tool-result prompt"}},
		},
		AuditRecorder: recorder,
		AuditRunID:    "run_stream_prompt_steps",
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

	promptArtifacts := recorder.requireArtifactsByKind(t, "run_stream_prompt_steps", coreaudit.ArtifactKindRuntimePromptEnvelope)
	if len(promptArtifacts) != 2 {
		t.Fatalf("runtime prompt artifact count = %d, want 2", len(promptArtifacts))
	}
	stepOnePromptEvent := recorder.requireEventForStep(t, "run_stream_prompt_steps", "prompt.resolved", 1)
	stepOnePromptPayload := decodeAuditPayload(t, stepOnePromptEvent)
	if stepOnePromptPayload["scene"] != "agent.run.review" {
		t.Fatalf("step 1 prompt payload = %#v, want scene=agent.run.review", stepOnePromptPayload)
	}
	if stepOnePromptPayload["message_count"] != float64(3) {
		t.Fatalf("step 1 prompt payload = %#v, want message_count=3", stepOnePromptPayload)
	}
	if stepOnePromptPayload["prompt_message_count"] != float64(2) {
		t.Fatalf("step 1 prompt payload = %#v, want prompt_message_count=2", stepOnePromptPayload)
	}
	if stepOnePromptPayload["segment_count"] != float64(2) {
		t.Fatalf("step 1 prompt payload = %#v, want segment_count=2", stepOnePromptPayload)
	}
	stepOnePhaseCounts := requireAuditCountMap(t, stepOnePromptPayload, "phase_segment_counts")
	if len(stepOnePhaseCounts) != 2 || stepOnePhaseCounts["session"] != 1 || stepOnePhaseCounts["step_pre_model"] != 1 {
		t.Fatalf("step 1 phase_segment_counts = %#v, want only session=1 and step_pre_model=1", stepOnePhaseCounts)
	}
	stepOneSourceCounts := requireAuditCountMap(t, stepOnePromptPayload, "source_counts")
	if len(stepOneSourceCounts) != 1 || stepOneSourceCounts["resolved_prompt"] != 2 {
		t.Fatalf("step 1 source_counts = %#v, want only resolved_prompt=2", stepOneSourceCounts)
	}
	if strings.Contains(string(stepOnePromptEvent.PayloadJSON), "Session prompt") || strings.Contains(string(stepOnePromptEvent.PayloadJSON), "Tool-result prompt") {
		t.Fatalf("step 1 prompt payload json = %s, want compact payload without prompt bodies", string(stepOnePromptEvent.PayloadJSON))
	}
	stepTwoPromptEvent := recorder.requireEventForStep(t, "run_stream_prompt_steps", "prompt.resolved", 2)
	stepTwoPromptPayload := decodeAuditPayload(t, stepTwoPromptEvent)
	if stepTwoPromptPayload["message_count"] != float64(6) {
		t.Fatalf("step 2 prompt payload = %#v, want message_count=6", stepTwoPromptPayload)
	}
	if stepTwoPromptPayload["prompt_message_count"] != float64(3) {
		t.Fatalf("step 2 prompt payload = %#v, want prompt_message_count=3", stepTwoPromptPayload)
	}
	if stepTwoPromptPayload["segment_count"] != float64(3) {
		t.Fatalf("step 2 prompt payload = %#v, want segment_count=3", stepTwoPromptPayload)
	}
	stepTwoPhaseCounts := requireAuditCountMap(t, stepTwoPromptPayload, "phase_segment_counts")
	if len(stepTwoPhaseCounts) != 3 || stepTwoPhaseCounts["session"] != 1 || stepTwoPhaseCounts["step_pre_model"] != 1 || stepTwoPhaseCounts["tool_result"] != 1 {
		t.Fatalf("step 2 phase_segment_counts = %#v, want all three injected phases", stepTwoPhaseCounts)
	}
	stepTwoSourceCounts := requireAuditCountMap(t, stepTwoPromptPayload, "source_counts")
	if len(stepTwoSourceCounts) != 1 || stepTwoSourceCounts["resolved_prompt"] != 3 {
		t.Fatalf("step 2 source_counts = %#v, want only resolved_prompt=3", stepTwoSourceCounts)
	}

	firstPrompt := decodeRuntimePromptEnvelopeArtifact(t, promptArtifacts[0])
	if len(firstPrompt.Segments) != 2 {
		t.Fatalf("step 1 runtime prompt segment count = %d, want 2", len(firstPrompt.Segments))
	}
	if firstPrompt.Segments[0].SourceType != runtimeprompt.SourceTypeResolvedPrompt || firstPrompt.Segments[0].SourceKey != "binding:101" {
		t.Fatalf("step 1 first segment = %#v, want db binding metadata in runtime segment", firstPrompt.Segments[0])
	}
	if firstPrompt.Segments[1].SourceType != runtimeprompt.SourceTypeResolvedPrompt || firstPrompt.Segments[1].SourceKey != "system_prompt" {
		t.Fatalf("step 1 second segment = %#v, want legacy metadata in runtime segment", firstPrompt.Segments[1])
	}
	if len(firstPrompt.PhaseSegmentCounts) != 2 || firstPrompt.PhaseSegmentCounts["session"] != 1 || firstPrompt.PhaseSegmentCounts["step_pre_model"] != 1 {
		t.Fatalf("step 1 phase counts = %#v, want only session=1 and step_pre_model=1", firstPrompt.PhaseSegmentCounts)
	}
	if len(firstPrompt.SourceCounts) != 1 || firstPrompt.SourceCounts["resolved_prompt"] != 2 {
		t.Fatalf("step 1 source counts = %#v, want only resolved_prompt=2", firstPrompt.SourceCounts)
	}
	if len(firstPrompt.Messages) != 3 {
		t.Fatalf("step 1 runtime prompt message count = %d, want 3", len(firstPrompt.Messages))
	}
	if firstPrompt.Messages[0].Content != "Session prompt" || firstPrompt.Messages[1].Content != "Step prompt" || firstPrompt.Messages[2].Content != "weather?" {
		t.Fatalf("step 1 runtime prompt messages = %#v, want session+step+user", firstPrompt.Messages)
	}

	secondPrompt := decodeRuntimePromptEnvelopeArtifact(t, promptArtifacts[1])
	if len(secondPrompt.Messages) != 6 {
		t.Fatalf("step 2 runtime prompt message count = %d, want 6", len(secondPrompt.Messages))
	}
	if secondPrompt.Messages[0].Content != "Session prompt" || secondPrompt.Messages[1].Content != "Step prompt" {
		t.Fatalf("step 2 runtime prompt prefix = %#v, want session+step", secondPrompt.Messages[:2])
	}
	if secondPrompt.Messages[2].Role != model.RoleUser || secondPrompt.Messages[2].Content != "weather?" {
		t.Fatalf("step 2 runtime prompt user replay = %#v, want original user message", secondPrompt.Messages[2])
	}
	if secondPrompt.Messages[3].Role != model.RoleAssistant || len(secondPrompt.Messages[3].ToolCalls) != 1 {
		t.Fatalf("step 2 runtime prompt assistant replay = %#v, want assistant tool call", secondPrompt.Messages[3])
	}
	if secondPrompt.Messages[4].Content != "Tool-result prompt" {
		t.Fatalf("step 2 runtime prompt insertion = %#v, want tool-result after assistant", secondPrompt.Messages)
	}
	if len(secondPrompt.Segments) != 3 {
		t.Fatalf("step 2 runtime prompt segment count = %d, want 3", len(secondPrompt.Segments))
	}
	if secondPrompt.Segments[2].SourceType != runtimeprompt.SourceTypeResolvedPrompt || secondPrompt.Segments[2].SourceKey != "AGENTS.md" {
		t.Fatalf("step 2 third segment = %#v, want workspace metadata in runtime segment", secondPrompt.Segments[2])
	}
	if len(secondPrompt.PhaseSegmentCounts) != 3 || secondPrompt.PhaseSegmentCounts["tool_result"] != 1 {
		t.Fatalf("step 2 phase counts = %#v, want tool_result phase present only on step 2", secondPrompt.PhaseSegmentCounts)
	}
	if len(secondPrompt.SourceCounts) != 1 || secondPrompt.SourceCounts["resolved_prompt"] != 3 {
		t.Fatalf("step 2 source counts = %#v, want resolved_prompt source present only with total 3", secondPrompt.SourceCounts)
	}

	requestArtifacts := recorder.requireArtifactsByKind(t, "run_stream_prompt_steps", coreaudit.ArtifactKindModelRequest)
	if len(requestArtifacts) != 2 {
		t.Fatalf("model request artifact count = %d, want 2", len(requestArtifacts))
	}
	firstRequest := decodeModelRequestArtifact(t, requestArtifacts[0])
	assertMessagesDoNotContainContent(t, firstRequest.Messages, "Tool-result prompt")
	secondRequest := decodeModelRequestArtifact(t, requestArtifacts[1])
	if secondRequest.Messages[4].Content != "Tool-result prompt" {
		t.Fatalf("step 2 request messages = %#v, want tool-result prompt between assistant and tool", secondRequest.Messages)
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
	Scene              string                             `json:"scene,omitempty"`
	Session            []model.Message                    `json:"session,omitempty"`
	StepPreModel       []model.Message                    `json:"step_pre_model,omitempty"`
	ToolResult         []model.Message                    `json:"tool_result,omitempty"`
	Messages           []model.Message                    `json:"messages"`
	Segments           []coreprompt.ResolvedPromptSegment `json:"segments,omitempty"`
	PhaseSegmentCounts map[string]int                     `json:"phase_segment_counts,omitempty"`
	SourceCounts       map[string]int                     `json:"source_counts,omitempty"`
}

type runtimePromptEnvelopeAuditArtifact struct {
	Segments           []runtimeprompt.Segment `json:"segments,omitempty"`
	Messages           []model.Message         `json:"messages"`
	PromptMessageCount int                     `json:"prompt_message_count"`
	PhaseSegmentCounts map[string]int          `json:"phase_segment_counts,omitempty"`
	SourceCounts       map[string]int          `json:"source_counts,omitempty"`
}

type modelResponseAuditArtifact struct {
	Message model.Message    `json:"message"`
	Usage   model.TokenUsage `json:"usage"`
}

func decodeRuntimePromptEnvelopeArtifact(t *testing.T, artifact *coreaudit.Artifact) runtimePromptEnvelopeAuditArtifact {
	t.Helper()

	var snapshot runtimePromptEnvelopeAuditArtifact
	if err := json.Unmarshal(artifact.BodyJSON, &snapshot); err != nil {
		t.Fatalf("decode runtime_prompt_envelope artifact error = %v", err)
	}
	return snapshot
}

func decodeResolvedPromptArtifact(t *testing.T, artifact *coreaudit.Artifact) resolvedPromptAuditArtifact {
	t.Helper()

	var snapshot resolvedPromptAuditArtifact
	if err := json.Unmarshal(artifact.BodyJSON, &snapshot); err != nil {
		t.Fatalf("decode resolved_prompt artifact error = %v", err)
	}
	return snapshot
}

func requireAuditCountMap(t *testing.T, payload map[string]any, key string) map[string]int {
	t.Helper()

	raw, ok := payload[key]
	if !ok {
		t.Fatalf("payload = %#v, want key %q", payload, key)
	}
	countMap, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("payload[%q] = %#v, want map[string]any", key, raw)
	}
	counts := make(map[string]int, len(countMap))
	for name, value := range countMap {
		count, ok := value.(float64)
		if !ok {
			t.Fatalf("payload[%q][%q] = %#v, want float64", key, name, value)
		}
		counts[name] = int(count)
	}
	return counts
}

func decodeModelResponseArtifact(t *testing.T, artifact *coreaudit.Artifact) modelResponseAuditArtifact {
	t.Helper()

	var snapshot modelResponseAuditArtifact
	if err := json.Unmarshal(artifact.BodyJSON, &snapshot); err != nil {
		t.Fatalf("decode model_response artifact error = %v", err)
	}
	return snapshot
}
