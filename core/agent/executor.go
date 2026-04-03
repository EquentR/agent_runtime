package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/EquentR/agent_runtime/core/approvals"
	coreaudit "github.com/EquentR/agent_runtime/core/audit"
	"github.com/EquentR/agent_runtime/core/interactions"
	"github.com/EquentR/agent_runtime/core/memory"
	coreprompt "github.com/EquentR/agent_runtime/core/prompt"
	model "github.com/EquentR/agent_runtime/core/providers/types"
	coreskills "github.com/EquentR/agent_runtime/core/skills"
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
	ConversationID string   `json:"conversation_id"`
	ProviderID     string   `json:"provider_id"`
	ModelID        string   `json:"model_id"`
	UserID         string   `json:"user_id,omitempty"`
	Message        string   `json:"message"`
	Scene          string   `json:"scene,omitempty"`
	SystemPrompt   string   `json:"system_prompt,omitempty"`
	CreatedBy      string   `json:"created_by,omitempty"`
	Skills         []string `json:"skills,omitempty"`
}

const defaultRunTaskScene = "agent.run.default"
const promptSourceKindWorkspaceSkill = "workspace_skill"

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
	ApprovalStore     *approvals.Store
	InteractionStore  *interactions.Store
	PromptResolver    *coreprompt.Resolver
	SkillsResolver    *coreskills.Resolver
	WorkspaceRoot     string
	ClientFactory     ClientFactory
	MemoryFactory     MemoryFactory
	NewEventSink      EventSinkFactory
	AuditRecorder     coreaudit.Recorder
}

const (
	interactionCheckpointMetadataKey        = coretypes.TaskMetadataKeyInteractionCheckpoint
	legacyToolApprovalCheckpointMetadataKey = coretypes.TaskMetadataKeyToolApprovalCheckpoint
)

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

func normalizeSkillNames(names []string) []string {
	if len(names) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(names))
	result := make([]string, 0, len(names))
	for _, name := range names {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func appendResolvedSkillsToPrompt(resolvedPrompt *coreprompt.ResolvedPrompt, resolvedSkills []coreskills.ResolvedSkill) {
	if resolvedPrompt == nil {
		return
	}
	segments := make([]coreprompt.ResolvedPromptSegment, 0, len(resolvedSkills))
	for _, skill := range resolvedSkills {
		segments = append(segments, coreprompt.ResolvedPromptSegment{
			Phase:       "session",
			Content:     skill.Content,
			SourceKind:  promptSourceKindWorkspaceSkill,
			SourceRef:   skill.SourceRef,
			RuntimeOnly: skill.RuntimeOnly,
		})
	}
	resolvedPrompt.InsertSessionSegmentsBeforeLaterPhases(segments)
}

func NewTaskExecutor(deps ExecutorDependencies) coretasks.Executor {
	return func(ctx context.Context, task *coretasks.Task, runtime *coretasks.Runtime) (output any, execErr error) {
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
		if deps.PromptResolver == nil {
			return nil, fmt.Errorf("prompt resolver is required")
		}

		auditor := newExecutorAuditor(deps.AuditRecorder, task, RunTaskInput{CreatedBy: task.CreatedBy})
		var (
			partialMessages []model.Message
			snapshotErr     error
		)
		defer func() {
			if execErr == nil {
				return
			}
			auditErr := execErr
			if snapshotErr != nil {
				auditErr = snapshotErr
			}
			auditor.recordErrorSnapshot(ctx, auditErr, partialMessages)
		}()

		var input RunTaskInput
		if err := json.Unmarshal(task.InputJSON, &input); err != nil {
			return nil, err
		}
		input.Skills = normalizeSkillNames(input.Skills)
		auditor.setInput(input)

		resolved, err := deps.Resolver.Resolve(input.ProviderID, input.ModelID)
		if err != nil {
			return nil, err
		}
		provider := resolved.Provider
		llmModel := resolved.Model
		resolvedPrompt, err := deps.PromptResolver.Resolve(ctx, coreprompt.ResolveInput{
			Scene:              resolveRunTaskScene(input.Scene),
			ProviderID:         provider.ProviderName(),
			ModelID:            llmModel.ModelID(),
			LegacySystemPrompt: input.SystemPrompt,
			WorkspaceRoot:      deps.WorkspaceRoot,
		})
		if err != nil {
			return nil, err
		}
		if len(input.Skills) > 0 {
			if deps.SkillsResolver == nil {
				return nil, fmt.Errorf("skills resolver is required when skills are selected")
			}
			resolvedSkills, err := deps.SkillsResolver.Resolve(ctx, coreskills.ResolveInput{Names: input.Skills})
			if err != nil {
				return nil, err
			}
			appendResolvedSkillsToPrompt(resolvedPrompt, resolvedSkills)
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
		auditor.setConversation(conversation)
		history, err := deps.ConversationStore.ListReplayMessages(ctx, conversation.ID)
		if err != nil {
			return nil, err
		}
		auditor.recordConversationLoaded(ctx, history)
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
		runID := auditor.ensureRun(ctx)
		runner, err := NewRunner(client, deps.Registry, Options{
			LLMModel:       llmModel,
			Memory:         memoryManager,
			SystemPrompt:   bridgeLegacySystemPromptFromResolvedPromptSession(resolvedPrompt),
			ResolvedPrompt: resolvedPrompt,
			EventSink:      sink,
			TaskID:         task.ID,
			AuditRecorder:  deps.AuditRecorder,
			AuditRunID:     runID,
			Actor:          "agent.run",
			Metadata: map[string]string{
				"conversation_id": conversation.ID,
				"provider_id":     conversation.ProviderID,
				"model_id":        conversation.ModelID,
			},
		})
		if err != nil {
			return nil, err
		}
		checkpoint, err := taskInteractionCheckpointFromMetadata(task.MetadataJSON)
		if err != nil {
			return nil, err
		}
		messages := cloneMessages(history)
		runInput := RunInput{Messages: messages}
		if checkpoint == nil {
			userMessage := model.Message{Role: model.RoleUser, Content: input.Message, ProviderID: input.ProviderID, ModelID: input.ModelID}
			if err := deps.ConversationStore.AppendMessages(ctx, conversation.ID, task.ID, []model.Message{userMessage}); err != nil {
				return nil, err
			}
			messages = append(messages, userMessage)
			runInput.Messages = messages
			auditor.recordUserMessageAppended(ctx, userMessage, messages)
		} else {
			resume, err := buildInteractionResume(ctx, deps.InteractionStore, deps.ApprovalStore, task, checkpoint)
			if err != nil {
				return nil, err
			}
			recordInteractionResumeAudit(ctx, deps.AuditRecorder, runID, checkpoint, resume)
			cleanedMetadata, cleanupErr := clearInteractionCheckpointMetadata(task.MetadataJSON)
			if cleanupErr != nil {
				return nil, cleanupErr
			}
			if runtime != nil {
				if err := runtime.UpdateMetadata(ctx, cleanedMetadata); err != nil {
					return nil, err
				}
			}
			task.MetadataJSON = marshalTaskMetadataForExecutor(cleanedMetadata, task.MetadataJSON)
			runInput.InteractionResume = resume
		}
		result, err := runner.Run(ctx, runInput)
		if err != nil {
			if errors.Is(err, ErrInteractionPending) {
				return nil, coretasks.ErrTaskSuspended
			}
			snapshotErr = err
			// Trim trailing incomplete tool-call assistant messages before persisting,
			// so that the conversation history remains valid for the next turn.
			safeMessages := trimIncompleteToolCallMessages(result.Messages)
			partialMessages = cloneMessages(safeMessages)
			if len(safeMessages) > 0 {
				if appendErr := deps.ConversationStore.AppendMessages(ctx, conversation.ID, task.ID, safeMessages); appendErr != nil {
					return nil, appendErr
				}
				auditor.recordMessagesPersisted(ctx, safeMessages)
			}
			failureMessage := newVisibleFailureSystemMessage(fmt.Sprintf("Run failed: %s", err.Error()))
			if appendErr := deps.ConversationStore.AppendMessages(ctx, conversation.ID, task.ID, []model.Message{failureMessage}); appendErr != nil {
				return nil, appendErr
			}
			auditor.recordMessagesPersisted(ctx, []model.Message{failureMessage})
			return nil, err
		}
		result = attachUsageToPersistedAssistantReply(result, input.ProviderID, input.ModelID)
		partialMessages = cloneMessages(result.Messages)
		if err := deps.ConversationStore.AppendMessages(ctx, conversation.ID, task.ID, result.Messages); err != nil {
			return nil, err
		}
		auditor.recordMessagesPersisted(ctx, result.Messages)
		return RunTaskResult{
			ConversationID:   conversation.ID,
			ProviderID:       conversation.ProviderID,
			ModelID:          conversation.ModelID,
			FinalMessage:     result.FinalMessage,
			Usage:            result.Usage,
			Cost:             result.Cost,
			MessagesAppended: len(result.Messages) + boolToInt(checkpoint == nil),
		}, nil
	}
}

func taskInteractionCheckpointFromMetadata(metadataJSON []byte) (*interactionCheckpoint, error) {
	if len(metadataJSON) == 0 {
		return nil, nil
	}
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(metadataJSON, &metadata); err != nil {
		return nil, fmt.Errorf("decode task metadata: %w", err)
	}
	if raw, ok := metadata[interactionCheckpointMetadataKey]; ok && len(raw) > 0 && string(raw) != "null" {
		var checkpoint interactionCheckpoint
		if err := json.Unmarshal(raw, &checkpoint); err != nil {
			return nil, fmt.Errorf("decode interaction checkpoint: %w", err)
		}
		return &checkpoint, nil
	}
	raw, ok := metadata[legacyToolApprovalCheckpointMetadataKey]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var checkpoint toolApprovalCheckpoint
	if err := json.Unmarshal(raw, &checkpoint); err != nil {
		return nil, fmt.Errorf("decode tool approval checkpoint: %w", err)
	}
	return &interactionCheckpoint{
		InteractionID:                    checkpoint.ApprovalID,
		Step:                             checkpoint.Step,
		AssistantMessage:                 checkpoint.AssistantMessage,
		ToolCallIndex:                    checkpoint.ToolCallIndex,
		ProducedMessagesBeforeCheckpoint: checkpoint.ProducedMessagesBeforeCheckpoint,
	}, nil
}

func buildInteractionResume(ctx context.Context, interactionStore *interactions.Store, approvalStore *approvals.Store, task *coretasks.Task, checkpoint *interactionCheckpoint) (*interactionResume, error) {
	if checkpoint == nil {
		return nil, nil
	}
	if task == nil {
		return nil, fmt.Errorf("task is required to resume interaction checkpoint")
	}
	resume := &interactionResume{Checkpoint: *checkpoint}
	if interactionStore != nil {
		interaction, err := interactionStore.GetInteraction(ctx, task.ID, checkpoint.InteractionID)
		if err == nil {
			switch interaction.Kind {
			case interactions.KindApproval:
				return buildApprovalInteractionResume(ctx, approvalStore, task, resume)
			case interactions.KindQuestion:
				if len(interaction.ResponseJSON) == 0 {
					return nil, fmt.Errorf("interaction %q is still pending", interaction.ID)
				}
				resume.SyntheticOutput = string(interaction.ResponseJSON)
				return resume, nil
			default:
				return nil, fmt.Errorf("interaction %q kind %q is not resumable", interaction.ID, interaction.Kind)
			}
		}
		if !errors.Is(err, interactions.ErrInteractionNotFound) {
			return nil, err
		}
	}
	return buildApprovalInteractionResume(ctx, approvalStore, task, resume)
}

func buildApprovalInteractionResume(ctx context.Context, approvalStore *approvals.Store, task *coretasks.Task, resume *interactionResume) (*interactionResume, error) {
	if approvalStore == nil {
		return nil, fmt.Errorf("approval store is required to resume checkpointed tool approval")
	}
	approval, err := approvalStore.GetApproval(ctx, task.ID, resume.Checkpoint.InteractionID)
	if err != nil {
		return nil, err
	}
	switch approval.Status {
	case approvals.StatusApproved:
		return resume, nil
	case approvals.StatusRejected, approvals.StatusExpired:
		resume.SyntheticOutput = syntheticToolOutputForApproval(approval)
		return resume, nil
	case approvals.StatusPending:
		return nil, fmt.Errorf("approval %q is still pending", approval.ID)
	default:
		return nil, fmt.Errorf("approval %q is not resumable with status %q", approval.ID, approval.Status)
	}
}

func syntheticToolOutputForApproval(approval *approvals.ToolApproval) string {
	if approval == nil {
		return "Tool execution was skipped because approval was not granted."
	}
	status := strings.TrimSpace(string(approval.Status))
	if status == "" {
		status = "not granted"
	}
	message := fmt.Sprintf("Tool execution was skipped because approval was %s.", status)
	if reason := strings.TrimSpace(approval.DecisionReason); reason != "" {
		message += " Reason: " + reason
	}
	return message
}

func recordInteractionResumeAudit(ctx context.Context, recorder coreaudit.Recorder, runID string, checkpoint *interactionCheckpoint, resume *interactionResume) {
	if recorder == nil || strings.TrimSpace(runID) == "" || checkpoint == nil || resume == nil {
		return
	}
	kind := string(interactions.KindApproval)
	if len(resume.SyntheticOutput) > 0 {
		if _, err := recorder.AppendEvent(ctx, runID, coreaudit.AppendEventInput{
			Phase:     coreaudit.PhaseInteraction,
			EventType: "interaction.responded",
			StepIndex: checkpoint.Step,
			Payload: map[string]any{
				"interaction_id": checkpoint.InteractionID,
				"kind":           kind,
			},
		}); err != nil {
			return
		}
	}
	_, _ = recorder.AppendEvent(ctx, runID, coreaudit.AppendEventInput{
		Phase:     coreaudit.PhaseInteraction,
		EventType: "interaction.resumed",
		StepIndex: checkpoint.Step,
		Payload: map[string]any{
			"interaction_id": checkpoint.InteractionID,
			"kind":           kind,
		},
	})
}

func clearInteractionCheckpointMetadata(metadataJSON []byte) (map[string]any, error) {
	metadata := map[string]any{}
	if len(metadataJSON) > 0 {
		if err := json.Unmarshal(metadataJSON, &metadata); err != nil {
			return nil, fmt.Errorf("decode task metadata: %w", err)
		}
	}
	delete(metadata, interactionCheckpointMetadataKey)
	delete(metadata, legacyToolApprovalCheckpointMetadataKey)
	return metadata, nil
}

func marshalTaskMetadataForExecutor(metadata map[string]any, fallback []byte) []byte {
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return append([]byte(nil), fallback...)
	}
	return encoded
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
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

func resolveRunTaskScene(scene string) string {
	if trimmed := strings.TrimSpace(scene); trimmed != "" {
		return trimmed
	}
	return defaultRunTaskScene
}

func bridgeLegacySystemPromptFromResolvedPromptSession(resolved *coreprompt.ResolvedPrompt) string {
	if resolved == nil || len(resolved.Session) == 0 {
		return ""
	}

	parts := make([]string, 0, len(resolved.Session))
	for _, message := range resolved.Session {
		if content := strings.TrimSpace(message.Content); content != "" {
			parts = append(parts, content)
		}
	}
	return strings.Join(parts, "\n\n")
}

// trimIncompleteToolCallMessages removes trailing assistant messages that have
// unresolved tool calls (i.e. tool calls without matching tool result messages).
// Such dangling tool_calls make the conversation history invalid for subsequent
// turns and must be stripped before persisting a partial run on error.
func trimIncompleteToolCallMessages(messages []model.Message) []model.Message {
	if len(messages) == 0 {
		return messages
	}
	// Collect tool result IDs present in the message list.
	toolResultIDs := make(map[string]struct{})
	for _, msg := range messages {
		if msg.Role == model.RoleTool && msg.ToolCallId != "" {
			toolResultIDs[msg.ToolCallId] = struct{}{}
		}
	}
	// Find the last index that is safe to include: drop trailing assistant
	// messages whose tool calls have no corresponding tool result.
	end := len(messages)
	for end > 0 {
		msg := messages[end-1]
		if msg.Role != model.RoleAssistant || len(msg.ToolCalls) == 0 {
			break
		}
		allResolved := true
		for _, tc := range msg.ToolCalls {
			if _, ok := toolResultIDs[tc.ID]; !ok {
				allResolved = false
				break
			}
		}
		if allResolved {
			break
		}
		end--
	}
	return messages[:end]
}
