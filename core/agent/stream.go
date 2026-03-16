package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	coretypes "github.com/EquentR/agent_runtime/core/types"
)

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

	conversation, err := r.buildRequestMessages(ctx, input.Messages)
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
		var usage model.TokenUsage
		var totalUsage model.TokenUsage
		var totalCost coretypes.CostBreakdown
		pricing := pricingFromOptions(r.options)

		for step := 1; step <= r.options.MaxSteps; step++ {
			if err := ctx.Err(); err != nil {
				runErr = err
				return
			}
			title := fmt.Sprintf("Agent step %d", step)
			r.emitStepStart(ctx, step, title)

			stream, err := r.client.ChatStream(ctx, model.ChatRequest{
				Model:      r.options.Model,
				Messages:   cloneMessages(conversation),
				MaxTokens:  r.options.MaxTokens,
				Tools:      cloneTools(requestTools),
				ToolChoice: r.options.ToolChoice,
				TraceID:    r.options.TraceID,
			})
			if err != nil {
				r.emitStepFinish(ctx, step, title, map[string]any{"error": err.Error()})
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
				runErr = err
				return
			}
			assistant = normalizeAssistantMessage(model.ChatResponse{Message: assistant})
			conversation = append(conversation, assistant)
			produced = append(produced, assistant)
			if r.options.Memory != nil {
				r.options.Memory.AddMessage(assistant)
			}

			if len(assistant.ToolCalls) == 0 {
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

			for _, call := range assistant.ToolCalls {
				if err := ctx.Err(); err != nil {
					runErr = err
					return
				}
				toolCalls++
				r.emitToolStart(ctx, step, call)
				arguments := make(map[string]interface{})
				if call.Arguments != "" {
					if err := json.Unmarshal([]byte(call.Arguments), &arguments); err != nil {
						wrapped := fmt.Errorf("decode tool arguments for %q: %w", call.Name, err)
						r.emitToolFinish(ctx, step, call, "", wrapped)
						r.emitStepFinish(ctx, step, title, map[string]any{"error": wrapped.Error()})
						runErr = wrapped
						return
					}
				}
				output, err := r.registry.Execute(r.toolContext(ctx, step), call.Name, arguments)
				if err != nil {
					wrapped := fmt.Errorf("execute tool %q: %w", call.Name, err)
					r.emitToolFinish(ctx, step, call, "", wrapped)
					r.emitStepFinish(ctx, step, title, map[string]any{"error": wrapped.Error()})
					runErr = wrapped
					return
				}
				toolMessage := model.Message{Role: model.RoleTool, ToolCallId: call.ID, Content: output}
				conversation = append(conversation, toolMessage)
				produced = append(produced, toolMessage)
				if r.options.Memory != nil {
					r.options.Memory.AddMessage(toolMessage)
				}
				r.emitToolFinish(ctx, step, call, output, nil)
			}

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

type streamCloser interface {
	Close() error
}
