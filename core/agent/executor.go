package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/EquentR/agent_runtime/core/memory"
	model "github.com/EquentR/agent_runtime/core/providers/types"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	"github.com/EquentR/agent_runtime/core/tools"
	coretypes "github.com/EquentR/agent_runtime/core/types"
)

type ModelResolver struct {
	Providers []coretypes.LLMProvider
}

type ResolvedModel struct {
	Provider *coretypes.LLMProvider
	Model    *coretypes.LLMModel
}

type ModelCatalog struct {
	DefaultProviderID string                `json:"default_provider_id"`
	DefaultModelID    string                `json:"default_model_id"`
	Providers         []ModelProviderOption `json:"providers"`
}

type ModelProviderOption struct {
	ID     string             `json:"id"`
	Name   string             `json:"name"`
	Models []ModelOptionEntry `json:"models"`
}

type ModelOptionEntry struct {
	ID      string                     `json:"id"`
	Name    string                     `json:"name"`
	Type    string                     `json:"type"`
	Context coretypes.LLMContextConfig `json:"context"`
	Cost    *coretypes.ModelPricing    `json:"cost,omitempty"`
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

type ClientFactory func(provider *coretypes.LLMProvider, llmModel *coretypes.LLMModel) (model.LlmClient, error)

type MemoryFactory func(model *coretypes.LLMModel) (*memory.Manager, error)

type EventSinkFactory func(runtime *coretasks.Runtime) EventSink

type ExecutorDependencies struct {
	Resolver          *ModelResolver
	ConversationStore *ConversationStore
	Registry          *tools.Registry
	ClientFactory     ClientFactory
	MemoryFactory     MemoryFactory
	NewEventSink      EventSinkFactory
}

func (r *ModelResolver) Resolve(providerID, modelID string) (*ResolvedModel, error) {
	if r == nil || len(r.Providers) == 0 {
		return nil, fmt.Errorf("llm provider is not configured")
	}
	provider := r.findProvider(providerID)
	if provider == nil {
		return nil, fmt.Errorf("llm provider %q is not configured", providerID)
	}
	llmModel := provider.FindModel(modelID)
	if llmModel == nil {
		return nil, fmt.Errorf("llm model %q is not configured under provider %q", modelID, providerID)
	}
	return &ResolvedModel{Provider: provider, Model: llmModel}, nil
}

func (r *ModelResolver) Catalog() ModelCatalog {
	providers := make([]ModelProviderOption, 0, len(r.Providers))
	defaultProviderID, defaultModelID := r.DefaultSelection()
	for i := range r.Providers {
		provider := &r.Providers[i]
		models := make([]ModelOptionEntry, 0, len(provider.Models))
		for j := range provider.Models {
			llmModel := &provider.Models[j]
			models = append(models, ModelOptionEntry{
				ID:      llmModel.ModelID(),
				Name:    firstNonEmpty(llmModel.ModelName(), llmModel.ModelID()),
				Type:    llmModel.ModelType(),
				Context: llmModel.ContextWindow(),
				Cost:    llmModel.Pricing(),
			})
		}
		providers = append(providers, ModelProviderOption{
			ID:     provider.ProviderName(),
			Name:   provider.ProviderName(),
			Models: models,
		})
	}
	return ModelCatalog{
		DefaultProviderID: defaultProviderID,
		DefaultModelID:    defaultModelID,
		Providers:         providers,
	}
}

func (r *ModelResolver) DefaultSelection() (string, string) {
	if r == nil {
		return "", ""
	}
	for i := range r.Providers {
		provider := &r.Providers[i]
		for j := range provider.Models {
			llmModel := &provider.Models[j]
			if provider.ProviderName() != "" && llmModel.ModelID() != "" {
				return provider.ProviderName(), llmModel.ModelID()
			}
		}
	}
	return "", ""
}

func (r *ModelResolver) findProvider(providerID string) *coretypes.LLMProvider {
	target := strings.TrimSpace(providerID)
	if r == nil || target == "" {
		return nil
	}
	for i := range r.Providers {
		if strings.EqualFold(r.Providers[i].ProviderName(), target) {
			return &r.Providers[i]
		}
	}
	return nil
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

		resolved, err := deps.Resolver.Resolve(input.ProviderID, input.ModelID)
		if err != nil {
			return nil, err
		}
		provider := resolved.Provider
		llmModel := resolved.Model
		conversation, err := deps.ConversationStore.EnsureConversation(ctx, EnsureConversationInput{
			ID:         input.ConversationID,
			ProviderID: input.ProviderID,
			ModelID:    input.ModelID,
			CreatedBy:  firstNonEmpty(input.CreatedBy, task.CreatedBy),
		})
		if err != nil {
			return nil, err
		}
		history, err := deps.ConversationStore.ListMessages(ctx, conversation.ID)
		if err != nil {
			return nil, err
		}
		client, err := deps.ClientFactory(provider, llmModel)
		if err != nil {
			return nil, err
		}
		memoryManager, err := buildMemoryManager(deps.MemoryFactory, llmModel)
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
			Memory:       memoryManager,
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
		userMessage := model.Message{Role: model.RoleUser, Content: input.Message, ProviderID: input.ProviderID, ModelID: input.ModelID}
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
		result = attachUsageToPersistedAssistantReply(result, input.ProviderID, input.ModelID)
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

func buildMemoryManager(factory MemoryFactory, llmModel *coretypes.LLMModel) (*memory.Manager, error) {
	if factory != nil {
		return factory(llmModel)
	}
	return memory.NewManager(memory.Options{Model: llmModel})
}

func attachUsageToPersistedAssistantReply(result RunResult, providerID string, modelID string) RunResult {
	result.FinalMessage.ProviderID = providerID
	result.FinalMessage.ModelID = modelID
	for index := range result.Messages {
		if result.Messages[index].Role == model.RoleAssistant {
			result.Messages[index].ProviderID = providerID
			result.Messages[index].ModelID = modelID
		}
	}
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
