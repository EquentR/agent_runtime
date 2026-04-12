package agent

import (
	"context"
	"strings"
	"sync"
	"time"

	coreaudit "github.com/EquentR/agent_runtime/core/audit"
	"github.com/EquentR/agent_runtime/core/forcedprompt"
	"github.com/EquentR/agent_runtime/core/memory"
	coreprompt "github.com/EquentR/agent_runtime/core/prompt"
	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/runtimeprompt"
	"github.com/EquentR/agent_runtime/core/tools"
	coretypes "github.com/EquentR/agent_runtime/core/types"
)

const DefaultMaxSteps = 128

type Runner struct {
	client   model.LlmClient
	registry *tools.Registry
	options  Options

	snapshotMu            sync.RWMutex
	lastMemoryCompression *MemoryCompressionSnapshot
}

type Options struct {
	SystemPrompt         string
	ResolvedPrompt       *coreprompt.ResolvedPrompt
	ConversationPrelude  []model.Message
	RuntimePromptBuilder *runtimeprompt.Builder
	Model                string
	LLMModel             *coretypes.LLMModel
	MaxSteps             int
	MaxTokens            int64
	Memory               *memory.Manager
	EventSink            EventSink
	TraceID              string
	ToolChoice           coretypes.ToolChoice
	Metadata             map[string]string
	Actor                string
	TaskID               string
	AuditRecorder        coreaudit.Recorder
	AuditRunID           string
	Now                  func() time.Time
}

type RunInput struct {
	Messages          []model.Message
	Tools             []coretypes.Tool
	InteractionResume *interactionResume
}

type interactionCheckpoint struct {
	InteractionID                    string          `json:"interaction_id"`
	Step                             int             `json:"step"`
	AssistantMessage                 model.Message   `json:"assistant_message"`
	ToolCallIndex                    int             `json:"tool_call_index"`
	ProducedMessagesBeforeCheckpoint []model.Message `json:"produced_messages_before_checkpoint"`
}

type interactionResume struct {
	Checkpoint      interactionCheckpoint `json:"checkpoint"`
	SyntheticOutput string                `json:"synthetic_output,omitempty"`
}

type toolApprovalCheckpoint struct {
	ApprovalID                       string          `json:"approval_id"`
	Step                             int             `json:"step"`
	AssistantMessage                 model.Message   `json:"assistant_message"`
	ToolCallIndex                    int             `json:"tool_call_index"`
	ProducedMessagesBeforeCheckpoint []model.Message `json:"produced_messages_before_checkpoint"`
}

type RunResult struct {
	Messages          []model.Message
	FinalMessage      model.Message
	StepsExecuted     int
	ToolCalls         int
	StopReason        string
	Usage             model.TokenUsage
	Cost              *coretypes.CostBreakdown
	MemoryCompression *MemoryCompressionSnapshot
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
	if options.RuntimePromptBuilder == nil {
		options.RuntimePromptBuilder = runtimeprompt.NewBuilder(forcedprompt.NewProvider())
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
