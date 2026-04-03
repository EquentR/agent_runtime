package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/EquentR/agent_runtime/core/approvals"
	coreaudit "github.com/EquentR/agent_runtime/core/audit"
	"github.com/EquentR/agent_runtime/core/interactions"
	coreprompt "github.com/EquentR/agent_runtime/core/prompt"
	model "github.com/EquentR/agent_runtime/core/providers/types"
	coretypes "github.com/EquentR/agent_runtime/core/types"
)

var ErrInteractionPending = errors.New("interaction pending")

var ErrToolApprovalPending = ErrInteractionPending

type StreamEventKind string

const (
	EventTextDelta      StreamEventKind = "text_delta"
	EventReasoningDelta StreamEventKind = "reasoning_delta"
	EventToolCallDelta  StreamEventKind = "tool_call_delta"
	EventUsage          StreamEventKind = "usage"
	EventCompleted      StreamEventKind = "completed"
)

type RunStreamEvent struct {
	Kind      StreamEventKind
	Step      int
	Text      string
	Reasoning string
	ToolCall  *coretypes.ToolCall
	Usage     *model.TokenUsage
	Message   *model.Message
	Err       error
	Metadata  map[string]any
}

type RunStreamResult struct {
	Events <-chan RunStreamEvent
	Wait   func() (RunResult, error)
	Close  func() error
}

const (
	runnerPromptPhaseSession      = "session"
	runnerPromptPhaseStepPreModel = "step_pre_model"
	runnerPromptPhaseToolResult   = "tool_result"

	runnerPromptSourceKindLegacySystemPrompt = "legacy_system_prompt"
	runnerPromptSourceRefLegacySystemPrompt  = "system_prompt"
)

type runnerResolvedPromptArtifact struct {
	Scene              string                             `json:"scene,omitempty"`
	Session            []model.Message                    `json:"session,omitempty"`
	StepPreModel       []model.Message                    `json:"step_pre_model,omitempty"`
	ToolResult         []model.Message                    `json:"tool_result,omitempty"`
	Segments           []coreprompt.ResolvedPromptSegment `json:"segments,omitempty"`
	Messages           []model.Message                    `json:"messages"`
	PhaseSegmentCounts map[string]int                     `json:"phase_segment_counts,omitempty"`
	SourceCounts       map[string]int                     `json:"source_counts,omitempty"`
}

type runnerModelResponseArtifact struct {
	Message model.Message    `json:"message"`
	Usage   model.TokenUsage `json:"usage"`
}

func (r *Runner) RunStream(ctx context.Context, input RunInput) (*RunStreamResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(input.Tools) > 0 && r.registry == nil {
		return nil, fmt.Errorf("tool registry is required when tools are provided")
	}

	memoryInsertedCount := len(input.Messages)
	baseConversation := cloneMessages(input.Messages)
	conversation, err := r.prepareConversationMessagesWithPersistedCount(ctx, baseConversation, 0)
	if err != nil {
		return nil, err
	}
	requestTools := cloneTools(input.Tools)
	if r.registry != nil {
		requestTools = mergeTools(r.registry.List(), requestTools)
	}

	events := make(chan RunStreamEvent, 32)
	var (
		mu       sync.Mutex
		result   RunResult
		runErr   error
		closeErr error
		closer   streamCloser
		done     = make(chan struct{})
	)

	go func() {
		defer close(done)
		defer close(events)
		produced := make([]model.Message, 0, r.options.MaxSteps*2)
		toolCalls := 0
		afterToolTurn := false
		var usage model.TokenUsage
		var totalUsage model.TokenUsage
		var totalCost coretypes.CostBreakdown
		pricing := pricingFromOptions(r.options)
		snapshotResult := func(stepsExecuted int) {
			result = RunResult{
				Messages:      append([]model.Message(nil), produced...),
				StepsExecuted: stepsExecuted,
				ToolCalls:     toolCalls,
				Usage:         totalUsage,
			}
			if pricing != nil {
				cost := totalCost
				result.Cost = &cost
			}
		}

		for step := 1; step <= r.options.MaxSteps; step++ {
			if step == 1 && input.InteractionResume != nil {
				resume := cloneInteractionResume(input.InteractionResume)
				checkpointMessages := cloneMessages(resume.Checkpoint.ProducedMessagesBeforeCheckpoint)
				baseConversation = append(baseConversation, checkpointMessages...)
				produced = append(produced, checkpointMessages...)
				if r.options.Memory != nil && len(checkpointMessages) > 0 {
					r.options.Memory.AddMessages(checkpointMessages)
					memoryInsertedCount += len(checkpointMessages)
				}
				title := fmt.Sprintf("Agent step %d", resume.Checkpoint.Step)
				suspended, execErr := r.executeAssistantToolCalls(ctx, resume.Checkpoint.Step, title, resume.Checkpoint.AssistantMessage, resume.Checkpoint.ToolCallIndex, resume, &baseConversation, &produced, &memoryInsertedCount, &toolCalls)
				if execErr != nil {
					r.emitStepFinish(ctx, resume.Checkpoint.Step, title, map[string]any{"error": execErr.Error()})
					snapshotResult(resume.Checkpoint.Step)
					runErr = execErr
					return
				}
				if suspended {
					snapshotResult(resume.Checkpoint.Step)
					result.StopReason = "waiting_for_interaction"
					runErr = ErrInteractionPending
					return
				}
				afterToolTurn = len(resume.Checkpoint.AssistantMessage.ToolCalls) > 0
				r.emitStepFinish(ctx, resume.Checkpoint.Step, title, map[string]any{"tool_calls": len(resume.Checkpoint.AssistantMessage.ToolCalls)})
				step = resume.Checkpoint.Step
				continue
			}
			if err := ctx.Err(); err != nil {
				snapshotResult(step - 1)
				runErr = err
				return
			}
			if step > 1 {
				if r.options.Memory != nil {
					conversation, err = r.prepareConversationMessagesWithPersistedCount(ctx, baseConversation, memoryInsertedCount)
					if err != nil {
						snapshotResult(step - 1)
						runErr = err
						return
					}
				} else {
					conversation = cloneMessages(baseConversation)
				}
			}
			requestMessages := r.buildRequestMessages(conversation, afterToolTurn)
			usage = model.TokenUsage{}
			title := fmt.Sprintf("Agent step %d", step)
			r.emitStepStart(ctx, step, title)
			promptArtifact := buildRunnerResolvedPromptArtifact(r.options, requestMessages, afterToolTurn)
			promptArtifactID := r.attachAuditArtifact(ctx, coreaudit.ArtifactKindResolvedPrompt, promptArtifact)
			r.appendAuditEvent(ctx, step, coreaudit.PhasePrompt, "prompt.resolved", buildRunnerResolvedPromptPayload(promptArtifact, len(conversation)), promptArtifactID)

			request := model.ChatRequest{
				Model:      r.options.Model,
				Messages:   cloneMessages(requestMessages),
				MaxTokens:  r.options.MaxTokens,
				Tools:      cloneTools(requestTools),
				ToolChoice: r.options.ToolChoice,
				TraceID:    r.options.TraceID,
			}
			requestArtifactID := r.attachAuditArtifact(ctx, coreaudit.ArtifactKindModelRequest, request)
			r.appendAuditEvent(ctx, step, coreaudit.PhaseRequest, "request.built", map[string]any{
				"message_count": len(request.Messages),
				"tool_count":    len(request.Tools),
				"max_tokens":    request.MaxTokens,
			}, requestArtifactID)

			stream, err := r.client.ChatStream(ctx, request)
			if err != nil {
				r.emitStepFinish(ctx, step, title, map[string]any{"error": err.Error()})
				snapshotResult(step - 1)
				runErr = err
				return
			}
			mu.Lock()
			closer = stream
			mu.Unlock()

			for {
				event, err := stream.RecvEvent()
				if err != nil {
					r.emitStepFinish(ctx, step, title, map[string]any{"error": err.Error()})
					snapshotResult(step - 1)
					runErr = err
					return
				}
				if event.Type == "" && event.Text == "" && event.Reasoning == "" && event.Message.Role == "" && event.Usage == (model.TokenUsage{}) && event.ToolCall.Name == "" && event.ToolCall.Arguments == "" && event.ToolCall.ID == "" {
					break
				}

				switch event.Type {
				case model.StreamEventTextDelta:
					runEvent := RunStreamEvent{Kind: EventTextDelta, Step: step, Text: event.Text}
					events <- runEvent
					r.emitStreamEvent(ctx, runEvent)
				case model.StreamEventReasoningDelta:
					runEvent := RunStreamEvent{Kind: EventReasoningDelta, Step: step, Reasoning: event.Reasoning}
					events <- runEvent
					r.emitStreamEvent(ctx, runEvent)
				case model.StreamEventToolCallDelta:
					toolCall := event.ToolCall
					runEvent := RunStreamEvent{Kind: EventToolCallDelta, Step: step, ToolCall: &toolCall}
					events <- runEvent
					r.emitStreamEvent(ctx, runEvent)
				case model.StreamEventUsage:
					usage = event.Usage
					totalUsage = addUsage(totalUsage, event.Usage)
					if pricing != nil {
						totalCost = totalCost.Add(breakdownFromUsage(pricing, event.Usage))
					}
					usageCopy := usage
					runEvent := RunStreamEvent{Kind: EventUsage, Step: step, Usage: &usageCopy}
					events <- runEvent
					r.emitStreamEvent(ctx, runEvent)
				case model.StreamEventCompleted:
					message := cloneMessage(event.Message)
					runEvent := RunStreamEvent{Kind: EventCompleted, Step: step, Message: &message}
					events <- runEvent
					r.emitStreamEvent(ctx, runEvent)
				}
			}

			assistant, err := stream.FinalMessage()
			if err != nil {
				r.emitStepFinish(ctx, step, title, map[string]any{"error": err.Error()})
				snapshotResult(step - 1)
				runErr = err
				return
			}
			assistant = normalizeAssistantMessage(model.ChatResponse{Message: assistant})
			responseArtifactID := r.attachAuditArtifact(ctx, coreaudit.ArtifactKindModelResponse, runnerModelResponseArtifact{
				Message: cloneMessage(assistant),
				Usage:   usage,
			})
			r.appendAuditEvent(ctx, step, coreaudit.PhaseModel, "model.completed", map[string]any{
				"message_role":            assistant.Role,
				"content_length":          len(assistant.Content),
				"tool_call_count":         len(assistant.ToolCalls),
				"usage_prompt_tokens":     usage.PromptTokens,
				"usage_completion_tokens": usage.CompletionTokens,
				"usage_total_tokens":      usage.TotalTokens,
			}, responseArtifactID)
			baseConversation = append(baseConversation, assistant)
			produced = append(produced, assistant)
			if r.options.Memory != nil {
				r.options.Memory.AddMessage(assistant)
				memoryInsertedCount++
			}

			if len(assistant.ToolCalls) == 0 {
				afterToolTurn = false
				r.emitStepFinish(ctx, step, title, map[string]any{"stop_reason": "assistant_message"})
				result = RunResult{
					Messages:      produced,
					FinalMessage:  assistant,
					StepsExecuted: step,
					ToolCalls:     toolCalls,
					StopReason:    "assistant_message",
					Usage:         totalUsage,
				}
				if pricing != nil {
					cost := totalCost
					result.Cost = &cost
				}
				return
			}
			if r.registry == nil {
				wrapped := fmt.Errorf("tool registry is required when model emits tool calls")
				r.emitStepFinish(ctx, step, title, map[string]any{"error": wrapped.Error()})
				snapshotResult(step)
				runErr = wrapped
				return
			}

			suspended, execErr := r.executeAssistantToolCalls(ctx, step, title, assistant, 0, nil, &baseConversation, &produced, &memoryInsertedCount, &toolCalls)
			if execErr != nil {
				r.emitStepFinish(ctx, step, title, map[string]any{"error": execErr.Error()})
				snapshotResult(step)
				runErr = execErr
				return
			}
			if suspended {
				snapshotResult(step)
				result.StopReason = "waiting_for_interaction"
				runErr = ErrInteractionPending
				return
			}
			afterToolTurn = len(assistant.ToolCalls) > 0

			r.emitStepFinish(ctx, step, title, map[string]any{"tool_calls": len(assistant.ToolCalls)})
		}

		result = RunResult{
			Messages:      produced,
			StepsExecuted: r.options.MaxSteps,
			ToolCalls:     toolCalls,
			StopReason:    "max_steps_exceeded",
			Usage:         totalUsage,
		}
		if pricing != nil {
			cost := totalCost
			result.Cost = &cost
		}
		runErr = ErrMaxStepsExceeded
	}()

	return &RunStreamResult{
		Events: events,
		Wait: func() (RunResult, error) {
			<-done
			return result, runErr
		},
		Close: func() error {
			mu.Lock()
			defer mu.Unlock()
			if closer != nil {
				closeErr = closer.Close()
			}
			return closeErr
		},
	}, nil
}

func (r *Runner) executeAssistantToolCalls(ctx context.Context, step int, title string, assistant model.Message, startIndex int, resume *interactionResume, baseConversation *[]model.Message, produced *[]model.Message, memoryInsertedCount *int, toolCalls *int) (bool, error) {
	if startIndex < 0 || startIndex > len(assistant.ToolCalls) {
		return false, fmt.Errorf("resume tool call index %d out of range", startIndex)
	}
	for callIndex := startIndex; callIndex < len(assistant.ToolCalls); callIndex++ {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		call := assistant.ToolCalls[callIndex]
		*toolCalls = *toolCalls + 1
		arguments, err := decodeToolArguments(call)
		if err != nil {
			decodeError := fmt.Sprintf("tool %q argument decode error: %s", call.Name, err.Error())
			r.emitToolFinish(ctx, step, call, "", err)
			toolMessage := model.Message{Role: model.RoleTool, ToolCallId: call.ID, Content: decodeError}
			*baseConversation = append(*baseConversation, toolMessage)
			*produced = append(*produced, toolMessage)
			if r.options.Memory != nil {
				r.options.Memory.AddMessage(toolMessage)
				*memoryInsertedCount = *memoryInsertedCount + 1
			}
			continue
		}

		syntheticOutput := ""
		resumeCurrentCall := resume != nil && callIndex == startIndex
		if resumeCurrentCall {
			syntheticOutput = strings.TrimSpace(resume.SyntheticOutput)
			r.emitInteractionResumed(ctx, step, resume.Checkpoint.InteractionID, inferInteractionKindFromToolCall(call.Name))
			if syntheticOutput != "" {
				var response map[string]any
				if err := json.Unmarshal([]byte(syntheticOutput), &response); err == nil {
					r.emitInteractionResponded(ctx, step, resume.Checkpoint.InteractionID, inferInteractionKindFromToolCall(call.Name), response)
				}
			}
		} else {
			if call.Name == "ask_user" {
				required, suspendErr := r.maybeSuspendForQuestion(ctx, step, callIndex, assistant, call, arguments, *produced)
				if suspendErr != nil {
					return false, suspendErr
				}
				if required {
					return true, nil
				}
				continue
			}
			required, suspendErr := r.maybeSuspendForInteraction(ctx, step, callIndex, assistant, call, arguments, *produced)
			if suspendErr != nil {
				return false, suspendErr
			}
			if required {
				return true, nil
			}
		}

		if syntheticOutput != "" {
			toolMessage := model.Message{Role: model.RoleTool, ToolCallId: call.ID, Content: syntheticOutput}
			*baseConversation = append(*baseConversation, toolMessage)
			*produced = append(*produced, toolMessage)
			if r.options.Memory != nil {
				r.options.Memory.AddMessage(toolMessage)
				*memoryInsertedCount = *memoryInsertedCount + 1
			}
			continue
		}

		r.emitToolStart(ctx, step, call)
		output, err := r.registry.Execute(r.executionToolContext(ctx, step), call.Name, arguments)
		if err != nil {
			toolError := fmt.Sprintf("tool %q execution error: %s", call.Name, err.Error())
			r.emitToolFinish(ctx, step, call, "", err)
			toolMessage := model.Message{Role: model.RoleTool, ToolCallId: call.ID, Content: toolError}
			*baseConversation = append(*baseConversation, toolMessage)
			*produced = append(*produced, toolMessage)
			if r.options.Memory != nil {
				r.options.Memory.AddMessage(toolMessage)
				*memoryInsertedCount = *memoryInsertedCount + 1
			}
			continue
		}
		toolMessage := model.Message{Role: model.RoleTool, ToolCallId: call.ID, Content: output}
		*baseConversation = append(*baseConversation, toolMessage)
		*produced = append(*produced, toolMessage)
		if r.options.Memory != nil {
			r.options.Memory.AddMessage(toolMessage)
			*memoryInsertedCount = *memoryInsertedCount + 1
		}
		r.emitToolFinish(ctx, step, call, output, nil)
	}
	return false, nil
}

func (r *Runner) maybeSuspendForInteraction(ctx context.Context, step int, toolCallIndex int, assistant model.Message, call coretypes.ToolCall, arguments map[string]interface{}, produced []model.Message) (bool, error) {
	if r == nil || r.registry == nil {
		return false, nil
	}
	policy, ok := r.registry.ApprovalPolicy(call.Name)
	if !ok {
		return false, nil
	}
	requirement := policy.Evaluate(arguments)
	if !requirement.Required {
		return false, nil
	}
	runtime := r.taskRuntime()
	if runtime == nil {
		return false, fmt.Errorf("tool %q requires approval but task runtime is not available", call.Name)
	}
	approval, err := runtime.CreateApproval(ctx, approvals.CreateApprovalInput{
		TaskID:           runtime.TaskID(),
		ConversationID:   firstNonEmpty(r.options.Metadata["conversation_id"]),
		StepIndex:        step,
		ToolCallID:       call.ID,
		ToolName:         call.Name,
		ArgumentsSummary: requirement.ArgumentsSummary,
		RiskLevel:        string(requirement.RiskLevel),
		Reason:           requirement.Reason,
	})
	if err != nil {
		return false, err
	}
	r.emitInteractionRequested(ctx, step, approval.ID, string(interactions.KindApproval), call, map[string]any{
		"arguments_summary": requirement.ArgumentsSummary,
		"risk_level":        requirement.RiskLevel,
		"reason":            requirement.Reason,
	})
	task, err := runtime.GetTask(ctx)
	if err != nil {
		return false, err
	}
	metadata, err := metadataWithInteractionCheckpoint(task.MetadataJSON, interactionCheckpoint{
		InteractionID:                    approval.ID,
		Step:                             step,
		AssistantMessage:                 cloneMessage(assistant),
		ToolCallIndex:                    toolCallIndex,
		ProducedMessagesBeforeCheckpoint: cloneMessages(produced),
	})
	if err != nil {
		return false, err
	}
	if err := runtime.UpdateMetadata(ctx, metadata); err != nil {
		return false, err
	}
	if err := runtime.Suspend(ctx, "waiting_for_interaction"); err != nil {
		return false, err
	}
	return true, nil
}

func (r *Runner) maybeSuspendForQuestion(ctx context.Context, step int, toolCallIndex int, assistant model.Message, call coretypes.ToolCall, arguments map[string]interface{}, produced []model.Message) (bool, error) {
	runtime := r.taskRuntime()
	if runtime == nil {
		return false, fmt.Errorf("tool %q requires task runtime to request human input", call.Name)
	}
	question := strings.TrimSpace(fmt.Sprint(arguments["question"]))
	if question == "" {
		return false, fmt.Errorf("ask_user requires question")
	}
	options := make([]string, 0)
	if rawOptions, ok := arguments["options"].([]any); ok {
		for _, option := range rawOptions {
			label := strings.TrimSpace(fmt.Sprint(option))
			if label != "" {
				options = append(options, label)
			}
		}
	}
	request := map[string]any{
		"question":     question,
		"options":      options,
		"allow_custom": arguments["allow_custom"],
		"placeholder":  arguments["placeholder"],
		"multiple":     arguments["multiple"],
	}
	interaction, err := runtime.CreateInteraction(ctx, interactions.CreateInteractionInput{
		ID:             questionInteractionID(runtime.TaskID(), call.ID),
		TaskID:         runtime.TaskID(),
		ConversationID: firstNonEmpty(r.options.Metadata["conversation_id"]),
		StepIndex:      step,
		ToolCallID:     call.ID,
		Kind:           interactions.KindQuestion,
		Request:        request,
	})
	if err != nil {
		return false, err
	}
	interactionID := interaction.ID
	r.emitInteractionRequested(ctx, step, interactionID, string(interactions.KindQuestion), call, request)
	task, err := runtime.GetTask(ctx)
	if err != nil {
		return false, err
	}
	metadata, err := metadataWithInteractionCheckpoint(task.MetadataJSON, interactionCheckpoint{
		InteractionID:                    interactionID,
		Step:                             step,
		AssistantMessage:                 cloneMessage(assistant),
		ToolCallIndex:                    toolCallIndex,
		ProducedMessagesBeforeCheckpoint: cloneMessages(produced),
	})
	if err != nil {
		return false, err
	}
	if err := runtime.UpdateMetadata(ctx, metadata); err != nil {
		return false, err
	}
	if err := runtime.Suspend(ctx, "waiting_for_interaction"); err != nil {
		return false, err
	}
	return true, nil
}

func inferInteractionKindFromToolCall(toolName string) string {
	if toolName == "ask_user" {
		return string(interactions.KindQuestion)
	}
	return string(interactions.KindApproval)
}

func questionInteractionID(taskID string, toolCallID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(taskID) + "\n" + strings.TrimSpace(toolCallID)))
	return fmt.Sprintf("interaction_question_%x", sum[:16])
}

func (r *Runner) taskRuntime() taskRuntime {
	if r == nil || r.options.EventSink == nil {
		return nil
	}
	bridge, ok := r.options.EventSink.(taskRuntimeBridge)
	if !ok {
		return nil
	}
	return bridge.TaskRuntime()
}

func (r *Runner) executionToolContext(ctx context.Context, step int) context.Context {
	runtime := r.taskRuntime()
	if runtime != nil {
		return runtime.ToolContext(ctx, fmt.Sprintf("step-%d", step))
	}
	return r.toolContext(ctx, step)
}

func decodeToolArguments(call coretypes.ToolCall) (map[string]interface{}, error) {
	arguments := make(map[string]interface{})
	if call.Arguments == "" {
		return arguments, nil
	}
	if err := json.Unmarshal([]byte(call.Arguments), &arguments); err != nil {
		return nil, err
	}
	return arguments, nil
}

func metadataWithInteractionCheckpoint(metadataJSON []byte, checkpoint interactionCheckpoint) (map[string]any, error) {
	metadata := map[string]any{}
	if len(metadataJSON) > 0 {
		if err := json.Unmarshal(metadataJSON, &metadata); err != nil {
			return nil, fmt.Errorf("decode task metadata: %w", err)
		}
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata[interactionCheckpointMetadataKey] = checkpoint
	return metadata, nil
}

func cloneInteractionResume(resume *interactionResume) *interactionResume {
	if resume == nil {
		return nil
	}
	cloned := *resume
	cloned.Checkpoint.AssistantMessage = cloneMessage(resume.Checkpoint.AssistantMessage)
	cloned.Checkpoint.ProducedMessagesBeforeCheckpoint = cloneMessages(resume.Checkpoint.ProducedMessagesBeforeCheckpoint)
	return &cloned
}

type streamCloser interface {
	Close() error
}

func buildRunnerResolvedPromptArtifact(options Options, requestMessages []model.Message, afterToolTurn bool) runnerResolvedPromptArtifact {
	artifact := runnerResolvedPromptArtifact{Messages: cloneMessages(requestMessages)}
	resolved := options.ResolvedPrompt
	if resolved == nil {
		if options.SystemPrompt != "" {
			systemPrompt := options.SystemPrompt
			artifact.Session = []model.Message{{Role: model.RoleSystem, Content: systemPrompt}}
			artifact.Segments = []coreprompt.ResolvedPromptSegment{{
				Order:      1,
				Phase:      runnerPromptPhaseSession,
				Content:    systemPrompt,
				SourceKind: runnerPromptSourceKindLegacySystemPrompt,
				SourceRef:  runnerPromptSourceRefLegacySystemPrompt,
			}}
		}
		artifact.PhaseSegmentCounts = countPromptSegmentsByPhase(artifact.Segments)
		artifact.SourceCounts = countPromptSegmentsBySource(artifact.Segments)
		return artifact
	}

	artifact.Scene = strings.TrimSpace(resolved.Scene)
	artifact.Session = cloneMessages(resolved.Session)
	artifact.StepPreModel = cloneMessages(resolved.StepPreModel)
	if afterToolTurn {
		artifact.ToolResult = cloneMessages(resolved.ToolResult)
	}
	artifact.Segments = filterResolvedPromptSegmentsForStep(resolved, afterToolTurn)
	artifact.PhaseSegmentCounts = countPromptSegmentsByPhase(artifact.Segments)
	artifact.SourceCounts = countPromptSegmentsBySource(artifact.Segments)
	return artifact
}

func buildRunnerResolvedPromptPayload(artifact runnerResolvedPromptArtifact, conversationMessageCount int) map[string]any {
	promptMessageCount := len(artifact.Messages) - conversationMessageCount
	if promptMessageCount < 0 {
		promptMessageCount = 0
	}
	payload := map[string]any{
		"message_count":        len(artifact.Messages),
		"prompt_message_count": promptMessageCount,
		"segment_count":        len(artifact.Segments),
	}
	if artifact.Scene != "" {
		payload["scene"] = artifact.Scene
	}
	if len(artifact.PhaseSegmentCounts) > 0 {
		payload["phase_segment_counts"] = cloneIntMap(artifact.PhaseSegmentCounts)
	}
	if len(artifact.SourceCounts) > 0 {
		payload["source_counts"] = cloneIntMap(artifact.SourceCounts)
	}
	return payload
}

func synthesizeResolvedPromptSegments(resolved *coreprompt.ResolvedPrompt) []coreprompt.ResolvedPromptSegment {
	if resolved == nil {
		return nil
	}
	segments := make([]coreprompt.ResolvedPromptSegment, 0, len(resolved.Session)+len(resolved.StepPreModel)+len(resolved.ToolResult))
	appendPhaseSegments := func(phase string, messages []model.Message) {
		for _, message := range messages {
			content := strings.TrimSpace(message.Content)
			if content == "" {
				continue
			}
			segments = append(segments, coreprompt.ResolvedPromptSegment{
				Order:   len(segments) + 1,
				Phase:   phase,
				Content: content,
			})
		}
	}
	appendPhaseSegments(runnerPromptPhaseSession, resolved.Session)
	appendPhaseSegments(runnerPromptPhaseStepPreModel, resolved.StepPreModel)
	appendPhaseSegments(runnerPromptPhaseToolResult, resolved.ToolResult)
	return segments
}

func filterResolvedPromptSegmentsForStep(resolved *coreprompt.ResolvedPrompt, afterToolTurn bool) []coreprompt.ResolvedPromptSegment {
	if resolved == nil {
		return nil
	}
	segments := cloneResolvedPromptSegments(resolved.Segments)
	if len(segments) == 0 {
		segments = synthesizeResolvedPromptSegments(resolved)
	}
	if len(segments) == 0 {
		return nil
	}
	filtered := make([]coreprompt.ResolvedPromptSegment, 0, len(segments))
	for _, segment := range segments {
		phase := strings.TrimSpace(segment.Phase)
		if phase == runnerPromptPhaseToolResult && !afterToolTurn {
			continue
		}
		segment.Order = len(filtered) + 1
		filtered = append(filtered, segment)
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

func cloneResolvedPromptSegments(segments []coreprompt.ResolvedPromptSegment) []coreprompt.ResolvedPromptSegment {
	if len(segments) == 0 {
		return nil
	}
	cloned := make([]coreprompt.ResolvedPromptSegment, len(segments))
	copy(cloned, segments)
	return cloned
}

func countPromptSegmentsByPhase(segments []coreprompt.ResolvedPromptSegment) map[string]int {
	if len(segments) == 0 {
		return nil
	}
	counts := make(map[string]int)
	for _, segment := range segments {
		phase := strings.TrimSpace(segment.Phase)
		if phase == "" {
			continue
		}
		counts[phase]++
	}
	if len(counts) == 0 {
		return nil
	}
	return counts
}

func countPromptSegmentsBySource(segments []coreprompt.ResolvedPromptSegment) map[string]int {
	if len(segments) == 0 {
		return nil
	}
	counts := make(map[string]int)
	for _, segment := range segments {
		sourceKind := strings.TrimSpace(segment.SourceKind)
		if sourceKind == "" {
			continue
		}
		counts[sourceKind]++
	}
	if len(counts) == 0 {
		return nil
	}
	return counts
}

func cloneIntMap(input map[string]int) map[string]int {
	if len(input) == 0 {
		return nil
	}
	cloned := make(map[string]int, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}
