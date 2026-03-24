package agent

import (
	"context"
	"strings"

	coreaudit "github.com/EquentR/agent_runtime/core/audit"
	"github.com/EquentR/agent_runtime/core/memory"
	coreprompt "github.com/EquentR/agent_runtime/core/prompt"
	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/tools"
	coretypes "github.com/EquentR/agent_runtime/core/types"
)

const DefaultMaxSteps = 8

type Runner struct {
	client   model.LlmClient
	registry *tools.Registry
	options  Options
}

type Options struct {
	SystemPrompt   string
	ResolvedPrompt *coreprompt.ResolvedPrompt
	Model          string
	LLMModel       *coretypes.LLMModel
	MaxSteps       int
	MaxTokens      int64
	Memory         *memory.Manager
	EventSink      EventSink
	TraceID        string
	ToolChoice     coretypes.ToolChoice
	Metadata       map[string]string
	Actor          string
	TaskID         string
	AuditRecorder  coreaudit.Recorder
	AuditRunID     string
}

type RunInput struct {
	Messages []model.Message
	Tools    []coretypes.Tool
}

type RunResult struct {
	Messages      []model.Message
	FinalMessage  model.Message
	StepsExecuted int
	ToolCalls     int
	StopReason    string
	Usage         model.TokenUsage
	Cost          *coretypes.CostBreakdown
}

func NewRunner(client model.LlmClient, registry *tools.Registry, options Options) (*Runner, error) {
	if client == nil {
		return nil, ErrNilClient
	}
	if options.MaxSteps <= 0 {
		options.MaxSteps = DefaultMaxSteps
	}
	if strings.TrimSpace(options.Model) == "" && options.LLMModel != nil {
		options.Model = options.LLMModel.ModelID()
	}
	if options.MaxTokens <= 0 && options.LLMModel != nil {
		if output := options.LLMModel.ContextWindow().Output; output > 0 {
			options.MaxTokens = output
		}
	}
	options.Metadata = cloneStringMap(options.Metadata)
	return &Runner{
		client:   client,
		registry: registry,
		options:  options,
	}, nil
}

func (r *Runner) Run(ctx context.Context, input RunInput) (RunResult, error) {
	streamResult, err := r.RunStream(ctx, input)
	if err != nil {
		return RunResult{}, err
	}
	for range streamResult.Events {
	}
	return streamResult.Wait()
}
