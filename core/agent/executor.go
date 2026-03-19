package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	"github.com/EquentR/agent_runtime/core/tools"
	coretypes "github.com/EquentR/agent_runtime/core/types"
)

type ModelResolver struct {
	Provider *coretypes.LLMProvider
}

type RunTaskInput struct {
	ConversationID string `json:"conversation_id"`
	ProviderID     string `json:"provider_id"`
	ModelID        string `json:"model_id"`
	UserID         string `json:"user_id,omitempty"`
	Message        string `json:"message"`
	SystemPrompt   string `json:"system_prompt,omitempty"`
	CreatedBy      string `json:"created_by,omitempty"`
}

type RunTaskResult struct {
	ConversationID   string                   `json:"conversation_id"`
	ProviderID       string                   `json:"provider_id"`
	ModelID          string                   `json:"model_id"`
	FinalMessage     model.Message            `json:"final_message"`
	Usage            model.TokenUsage         `json:"usage"`
	Cost             *coretypes.CostBreakdown `json:"cost,omitempty"`
	MessagesAppended int                      `json:"messages_appended"`
}

type ClientFactory func(model *coretypes.LLMModel) (model.LlmClient, error)

type EventSinkFactory func(runtime *coretasks.Runtime) EventSink

type ExecutorDependencies struct {
	Resolver          *ModelResolver
	ConversationStore *ConversationStore
	Registry          *tools.Registry
	ClientFactory     ClientFactory
	NewEventSink      EventSinkFactory
}

func (r *ModelResolver) Resolve(providerID, modelID string) (*coretypes.LLMModel, error) {
	if r == nil || r.Provider == nil {
		return nil, fmt.Errorf("llm provider is not configured")
	}
	if !strings.EqualFold(strings.TrimSpace(providerID), r.Provider.ProviderName()) {
		return nil, fmt.Errorf("llm provider %q is not configured", providerID)
	}
	model := r.Provider.FindModel(modelID)
	if model == nil {
		return nil, fmt.Errorf("llm model %q is not configured under provider %q", modelID, providerID)
	}
	return model, nil
}

func NewTaskExecutor(deps ExecutorDependencies) coretasks.Executor {
	return func(ctx context.Context, task *coretasks.Task, runtime *coretasks.Runtime) (any, error) {
		if task == nil {
			return nil, fmt.Errorf("task is required")
		}
		if deps.Resolver == nil {
			return nil, fmt.Errorf("model resolver is required")
		}
		if deps.ConversationStore == nil {
			return nil, fmt.Errorf("conversation store is required")
		}
		if deps.ClientFactory == nil {
			return nil, fmt.Errorf("client factory is required")
		}

		var input RunTaskInput
		if err := json.Unmarshal(task.InputJSON, &input); err != nil {
			return nil, err
		}

		llmModel, err := deps.Resolver.Resolve(input.ProviderID, input.ModelID)
		if err != nil {
			return nil, err
		}
		conversation, err := deps.ConversationStore.EnsureConversation(ctx, EnsureConversationInput{
			ID:         input.ConversationID,
			ProviderID: input.ProviderID,
			ModelID:    input.ModelID,
			CreatedBy:  firstNonEmpty(input.CreatedBy, task.CreatedBy),
		})
		if err != nil {
			return nil, err
		}
		if !strings.EqualFold(conversation.ProviderID, input.ProviderID) || !strings.EqualFold(conversation.ModelID, input.ModelID) {
			return nil, fmt.Errorf("conversation %q provider/model mismatch", conversation.ID)
		}
		history, err := deps.ConversationStore.ListMessages(ctx, conversation.ID)
		if err != nil {
			return nil, err
		}
		client, err := deps.ClientFactory(llmModel)
		if err != nil {
			return nil, err
		}
		var sink EventSink
		if deps.NewEventSink != nil {
			sink = deps.NewEventSink(runtime)
		} else if runtime != nil {
			sink = NewTaskRuntimeSink(runtime)
		}
		runner, err := NewRunner(client, deps.Registry, Options{
			LLMModel:     llmModel,
			SystemPrompt: input.SystemPrompt,
			EventSink:    sink,
			Actor:        "agent.run",
			Metadata: map[string]string{
				"conversation_id": conversation.ID,
				"provider_id":     conversation.ProviderID,
				"model_id":        conversation.ModelID,
			},
		})
		if err != nil {
			return nil, err
		}
		userMessage := model.Message{Role: model.RoleUser, Content: input.Message}
		if err := deps.ConversationStore.AppendMessages(ctx, conversation.ID, task.ID, []model.Message{userMessage}); err != nil {
			return nil, err
		}
		messages := append(cloneMessages(history), userMessage)
		result, err := runner.Run(ctx, RunInput{Messages: messages})
		if err != nil {
			if len(result.Messages) > 0 {
				if appendErr := deps.ConversationStore.AppendMessages(ctx, conversation.ID, task.ID, result.Messages); appendErr != nil {
					return nil, appendErr
				}
			}
			failureMessage := model.Message{Role: model.RoleSystem, Content: fmt.Sprintf("Run failed: %s", err.Error())}
			if appendErr := deps.ConversationStore.AppendMessages(ctx, conversation.ID, task.ID, []model.Message{failureMessage}); appendErr != nil {
				return nil, appendErr
			}
			return nil, err
		}
		result = attachUsageToPersistedAssistantReply(result)
		if err := deps.ConversationStore.AppendMessages(ctx, conversation.ID, task.ID, result.Messages); err != nil {
			return nil, err
		}
		return RunTaskResult{
			ConversationID:   conversation.ID,
			ProviderID:       conversation.ProviderID,
			ModelID:          conversation.ModelID,
			FinalMessage:     result.FinalMessage,
			Usage:            result.Usage,
			Cost:             result.Cost,
			MessagesAppended: 1 + len(result.Messages),
		}, nil
	}
}

func attachUsageToPersistedAssistantReply(result RunResult) RunResult {
	if !hasTokenUsage(result.Usage) {
		return result
	}

	usage := result.Usage
	result.FinalMessage.Usage = &usage
	for index := len(result.Messages) - 1; index >= 0; index -= 1 {
		if result.Messages[index].Role != model.RoleAssistant {
			continue
		}
		usageCopy := result.Usage
		result.Messages[index].Usage = &usageCopy
		break
	}
	return result
}

func hasTokenUsage(usage model.TokenUsage) bool {
	return usage.PromptTokens > 0 || usage.CachedPromptTokens > 0 || usage.CompletionTokens > 0 || usage.TotalTokens > 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
