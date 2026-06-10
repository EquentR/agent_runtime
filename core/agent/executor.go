package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/EquentR/agent_runtime/core/approvals"
	"github.com/EquentR/agent_runtime/core/attachments"
	coreaudit "github.com/EquentR/agent_runtime/core/audit"
	"github.com/EquentR/agent_runtime/core/forcedprompt"
	"github.com/EquentR/agent_runtime/core/interactions"
	corelog "github.com/EquentR/agent_runtime/core/log"
	"github.com/EquentR/agent_runtime/core/memory"
	coreprompt "github.com/EquentR/agent_runtime/core/prompt"
	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/runtimeprompt"
	coreskills "github.com/EquentR/agent_runtime/core/skills"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	"github.com/EquentR/agent_runtime/core/tools"
	coretypes "github.com/EquentR/agent_runtime/core/types"
	"github.com/EquentR/agent_runtime/core/workspaces"
)

type ModelResolver struct {
	Providers       []coretypes.LLMProvider
	ResolveFunc     func(ctx context.Context, providerID string, modelID string) (*ResolvedModel, error)
	ResolveTaskFunc func(ctx context.Context, input RunTaskInput) (*ResolvedModel, error)
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

// ModelContext captures the pre-existing model catalog context shape.
type ModelContext struct {
	Max    int64 `json:"max"`
	Input  int64 `json:"input"`
	Output int64 `json:"output"`
}

type ModelOptionEntry struct {
	ID           string                      `json:"id"`
	Name         string                      `json:"name"`
	Type         string                      `json:"type"`
	Context      ModelContext                `json:"context"`
	Cost         *coretypes.ModelPricing     `json:"cost,omitempty"`
	Capabilities coretypes.ModelCapabilities `json:"capabilities"`
}

type RunTaskInput struct {
	ConversationID  string          `json:"conversation_id"`
	ProviderID      string          `json:"provider_id"`
	ModelID         string          `json:"model_id"`
	UserID          string          `json:"user_id,omitempty"`
	Message         string          `json:"message"`
	AttachmentIDs   []string        `json:"attachment_ids,omitempty"`
	Scene           string          `json:"scene,omitempty"`
	SystemPrompt    string          `json:"system_prompt,omitempty"`
	CreatedBy       string          `json:"created_by,omitempty"`
	Skills          []string        `json:"skills,omitempty"`
	WorkspaceUserID string          `json:"workspace_user_id,omitempty"`
	WorkspaceMode   workspaces.Mode `json:"workspace_mode,omitempty"`
}

const defaultRunTaskScene = "agent.run.default"

type RunTaskResult struct {
	ConversationID    string                     `json:"conversation_id"`
	ProviderID        string                     `json:"provider_id"`
	ModelID           string                     `json:"model_id"`
	FinalMessage      model.Message              `json:"final_message"`
	Usage             model.TokenUsage           `json:"usage"`
	Cost              *coretypes.CostBreakdown   `json:"cost,omitempty"`
	MessagesAppended  int                        `json:"messages_appended"`
	MemoryContext     *MemoryContextSnapshot     `json:"memory_context,omitempty"`
	MemoryCompression *MemoryCompressionSnapshot `json:"memory_compression,omitempty"`
	WorkspaceMode     workspaces.Mode            `json:"workspace_mode,omitempty"`
	WorkspaceState    workspaces.State           `json:"workspace_state,omitempty"`
}

type ClientFactory func(provider *coretypes.LLMProvider, llmModel *coretypes.LLMModel) (model.LlmClient, error)

type MemoryFactory func(model *coretypes.LLMModel) (*memory.Manager, error)

type EventSinkFactory func(runtime *coretasks.Runtime) EventSink

type ToolRegistryFactory func(workspaceRoot string) (*tools.Registry, error)

type ExecutorDependencies struct {
	Resolver            *ModelResolver
	ConversationStore   *ConversationStore
	AttachmentStore     *attachments.Store
	AttachmentStorage   attachments.Storage
	Registry            *tools.Registry
	ApprovalStore       *approvals.Store
	InteractionStore    *interactions.Store
	PromptResolver      *coreprompt.Resolver
	SkillsResolver      *coreskills.Resolver
	WorkspaceRoot       string
	WorkspaceManager    *workspaces.Manager
	ToolRegistryFactory ToolRegistryFactory
	ClientFactory       ClientFactory
	MemoryFactory       MemoryFactory
	NewEventSink        EventSinkFactory
	AuditRecorder       coreaudit.Recorder
}

const (
	interactionCheckpointMetadataKey        = coretypes.TaskMetadataKeyInteractionCheckpoint
	legacyToolApprovalCheckpointMetadataKey = coretypes.TaskMetadataKeyToolApprovalCheckpoint
)

func (r *ModelResolver) Resolve(providerID, modelID string) (*ResolvedModel, error) {
	return r.ResolveContext(context.Background(), providerID, modelID)
}

func (r *ModelResolver) ResolveContext(ctx context.Context, providerID, modelID string) (*ResolvedModel, error) {
	if r != nil && r.ResolveFunc != nil {
		return r.ResolveFunc(ctx, providerID, modelID)
	}
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

func (r *ModelResolver) ResolveTask(ctx context.Context, input RunTaskInput) (*ResolvedModel, error) {
	if r != nil && r.ResolveTaskFunc != nil {
		return r.ResolveTaskFunc(ctx, input)
	}
	return r.ResolveContext(ctx, input.ProviderID, input.ModelID)
}

func (r *ModelResolver) Catalog() ModelCatalog {
	providers := make([]ModelProviderOption, 0, len(r.Providers))
	defaultProviderID, defaultModelID := r.DefaultSelection()
	for i := range r.Providers {
		provider := &r.Providers[i]
		models := make([]ModelOptionEntry, 0, len(provider.Models))
		for j := range provider.Models {
			llmModel := &provider.Models[j]
			ctx := llmModel.ContextWindow()
			models = append(models, ModelOptionEntry{
				ID:   llmModel.ModelID(),
				Name: firstNonEmpty(llmModel.ModelName(), llmModel.ModelID()),
				Type: llmModel.ModelType(),
				Context: ModelContext{
					Max:    ctx.Max,
					Input:  ctx.Input,
					Output: ctx.Output,
				},
				Cost:         llmModel.Pricing(),
				Capabilities: llmModel.Capabilities,
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
	return func(ctx context.Context, task *coretasks.Task, runtime *coretasks.Runtime) (output any, execErr error) {
		startedAt := time.Now()
		conversationID := ""
		if task == nil {
			return nil, fmt.Errorf("task is required")
		}
		corelog.Info("agent executor started", corelog.String("component", "agent"), corelog.String("module", "executor"), corelog.String("task_id", task.ID), corelog.String("task_type", task.TaskType))
		defer func() {
			fields := []corelog.Field{
				corelog.String("component", "agent"),
				corelog.String("module", "executor"),
				corelog.String("task_id", task.ID),
				corelog.Duration("duration", time.Since(startedAt)),
			}
			if conversationID != "" {
				fields = append(fields, corelog.String("conversation_id", conversationID))
			}
			if execErr != nil {
				corelog.Error("agent executor failed", append(fields, corelog.Err(execErr))...)
				return
			}
			corelog.Info("agent executor finished", fields...)
		}()
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
			partialMessages     []model.Message
			snapshotErr         error
			workspaceInfo       executorWorkspaceInfo
			workspaceFinalized  bool
			workspaceFailureErr error
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
		defer func() {
			if execErr == nil || workspaceFinalized || errors.Is(execErr, coretasks.ErrTaskSuspended) {
				return
			}
			cause := execErr
			if workspaceFailureErr != nil {
				cause = workspaceFailureErr
			}
			_, _ = completeExecutorWorkspace(ctx, deps, workspaceInfo, cause)
		}()

		var input RunTaskInput
		if err := json.Unmarshal(task.InputJSON, &input); err != nil {
			return nil, err
		}
		if task.CreatedBy != "" {
			input.CreatedBy = task.CreatedBy
			input.UserID = ""
		}
		input.Skills = coreskills.NormalizeNames(input.Skills)
		auditor.setInput(input)

		workspaceRoot, registry, skillsResolver, workspaceInfo, err := resolveExecutorWorkspace(ctx, deps, task, input)
		if err != nil {
			return nil, err
		}
		resolved, err := deps.Resolver.ResolveTask(ctx, input)
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
			WorkspaceRoot:      workspaceRoot,
		})
		if err != nil {
			return nil, err
		}
		var conversationPrelude []model.Message
		if len(input.Skills) > 0 {
			if skillsResolver == nil {
				return nil, fmt.Errorf("skills resolver is required when skills are selected")
			}
			resolvedSkills, err := skillsResolver.Resolve(ctx, coreskills.ResolveInput{Names: input.Skills})
			if err != nil {
				return nil, err
			}
			if summary := coreskills.BuildSelectedSkillSummaryMessage(resolvedSkills); summary != nil {
				conversationPrelude = append(conversationPrelude, *summary)
			}
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
		conversationID = conversation.ID
		auditor.setConversation(conversation)
		savedSummary := ""
		if deps.ConversationStore != nil {
			if loadedSummary, loadErr := deps.ConversationStore.GetMemorySummary(ctx, conversationID); loadErr != nil {
				corelog.Warn("failed to restore memory summary", corelog.String("component", "agent"), corelog.String("module", "executor"), corelog.String("conversation_id", conversationID), corelog.Err(loadErr))
			} else {
				savedSummary = loadedSummary
			}
		}
		history, err := deps.ConversationStore.ListReplayMessages(ctx, conversation.ID)
		if err != nil {
			return nil, err
		}
		history, err = hydrateReplayMessages(ctx, deps.AttachmentStore, deps.AttachmentStorage, history, conversation.ID, workspaceRoot, llmModel.SupportsAttachments(), true)
		if err != nil {
			return nil, err
		}
		history = sanitizeReplayMessages(history)
		corelog.Info("conversation history loaded", corelog.String("component", "agent"), corelog.String("module", "executor"), corelog.String("task_id", task.ID), corelog.String("conversation_id", conversation.ID), corelog.Int("message_count", len(history)))
		auditor.recordConversationLoaded(ctx, history)
		client, err := deps.ClientFactory(provider, llmModel)
		if err != nil {
			return nil, err
		}
		memoryManager, err := buildMemoryManager(deps.MemoryFactory, client, llmModel)
		if err != nil {
			return nil, err
		}
		if memoryManager != nil && savedSummary != "" {
			memoryManager.LoadSummary(savedSummary)
		}
		var sink EventSink
		if deps.NewEventSink != nil {
			sink = deps.NewEventSink(runtime)
		} else if runtime != nil {
			sink = NewTaskRuntimeSink(runtime)
		}
		runID := auditor.ensureRun(ctx)
		runner, err := NewRunner(client, registry, Options{
			LLMModel:             llmModel,
			Memory:               memoryManager,
			ResolvedPrompt:       resolvedPrompt,
			ConversationPrelude:  conversationPrelude,
			RuntimePromptBuilder: runtimeprompt.NewBuilder(forcedprompt.NewProvider()),
			EventSink:            sink,
			TaskID:               task.ID,
			AuditRecorder:        deps.AuditRecorder,
			AuditRunID:           runID,
			Actor:                "agent.run",
			RecoveryDelay:        streamRecoveryDelay,
			Metadata: map[string]string{
				"conversation_id": conversation.ID,
				"provider_id":     conversation.ProviderID,
				"model_id":        conversation.ModelID,
				"created_by":      firstNonEmpty(input.CreatedBy, task.CreatedBy),
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
			plannedAttachments, err := planCurrentAttachments(ctx, deps.AttachmentStore, deps.AttachmentStorage, input.AttachmentIDs, firstNonEmpty(strings.TrimSpace(input.CreatedBy), strings.TrimSpace(task.CreatedBy)), conversation.ID, workspaceRoot, llmModel.SupportsAttachments())
			if err != nil {
				return nil, err
			}
			persistedUserMessage := model.Message{Role: model.RoleUser, Content: appendAttachmentManifest(input.Message, plannedAttachments.manifestText), ProviderID: input.ProviderID, ModelID: input.ModelID, Attachments: plannedAttachments.display}
			userMessage := persistedUserMessage
			userMessage.Attachments = plannedAttachments.direct
			if err := deps.ConversationStore.AppendMessages(ctx, conversation.ID, task.ID, []model.Message{persistedUserMessage}); err != nil {
				return nil, err
			}
			messages = append(messages, userMessage)
			runInput.Messages = messages
			auditor.recordUserMessageAppended(ctx, persistedUserMessage, messages)
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
		latestCompression := cloneMemoryCompressionSnapshot(conversation.MemoryCompression)
		if result.MemoryCompression != nil {
			latestCompression = cloneMemoryCompressionSnapshot(result.MemoryCompression)
		}
		if err != nil {
			if errors.Is(err, ErrInteractionPending) {
				return nil, coretasks.ErrTaskSuspended
			}
			workspaceFailureErr = err
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
			if _, persistErr := persistConversationMemoryState(ctx, deps.ConversationStore, conversation.ID, memoryManager, latestCompression); persistErr != nil {
				return nil, persistErr
			}
			return nil, err
		}
		result = attachUsageToPersistedAssistantReply(result, input.ProviderID, input.ModelID)
		partialMessages = cloneMessages(result.Messages)
		if err := deps.ConversationStore.AppendMessages(ctx, conversation.ID, task.ID, result.Messages); err != nil {
			return nil, err
		}
		auditor.recordMessagesPersisted(ctx, result.Messages)
		memoryContext, err := persistConversationMemoryState(ctx, deps.ConversationStore, conversation.ID, memoryManager, latestCompression)
		if err != nil {
			return nil, err
		}
		workspaceState, err := finishSuccessfulExecutorWorkspace(ctx, deps, workspaceInfo)
		if err != nil {
			return nil, err
		}
		workspaceFinalized = true
		return RunTaskResult{
			ConversationID:    conversation.ID,
			ProviderID:        conversation.ProviderID,
			ModelID:           conversation.ModelID,
			FinalMessage:      result.FinalMessage,
			Usage:             result.Usage,
			Cost:              result.Cost,
			MessagesAppended:  len(result.Messages) + boolToInt(checkpoint == nil),
			MemoryContext:     memoryContext,
			MemoryCompression: latestCompression,
			WorkspaceMode:     workspaceInfo.Mode,
			WorkspaceState:    workspaceState,
		}, nil
	}
}

type executorWorkspaceInfo struct {
	UserID      string
	WorkspaceID string
	TaskID      string
	Mode        workspaces.Mode
}

func resolveExecutorWorkspace(ctx context.Context, deps ExecutorDependencies, task *coretasks.Task, input RunTaskInput) (string, *tools.Registry, *coreskills.Resolver, executorWorkspaceInfo, error) {
	workspaceRoot := deps.WorkspaceRoot
	registry := deps.Registry
	skillsResolver := deps.SkillsResolver
	info := executorWorkspaceInfo{Mode: normalizeWorkspaceMode(input.WorkspaceMode)}
	if deps.WorkspaceManager == nil {
		return workspaceRoot, registry, skillsResolver, info, nil
	}

	workspaceUserID := firstNonEmpty(input.WorkspaceUserID, task.CreatedBy, input.CreatedBy)
	if workspaceUserID == "" {
		return workspaceRoot, registry, skillsResolver, info, nil
	}
	info.UserID = workspaceUserID
	info.WorkspaceID = executorWorkspaceID(task, input)
	info.TaskID = task.ID
	taskWorkspace, err := deps.WorkspaceManager.CreateTaskWorkspace(ctx, workspaceUserID, info.WorkspaceID, info.Mode)
	if err != nil {
		return "", nil, nil, info, err
	}
	workspaceRoot = taskWorkspace.Root
	skillsResolver = coreskills.NewResolver(coreskills.NewLoader(workspaceRoot))
	if deps.ToolRegistryFactory != nil {
		registry, err = deps.ToolRegistryFactory(workspaceRoot)
		if err != nil {
			return "", nil, nil, info, err
		}
	}
	return workspaceRoot, registry, skillsResolver, info, nil
}

func finishSuccessfulExecutorWorkspace(ctx context.Context, deps ExecutorDependencies, info executorWorkspaceInfo) (workspaces.State, error) {
	if deps.WorkspaceManager == nil || info.UserID == "" || info.WorkspaceID == "" {
		return "", nil
	}
	if info.Mode == workspaces.ModeMutable {
		state, err := deps.WorkspaceManager.FinishMutableWorkspace(ctx, info.UserID, info.WorkspaceID)
		if err != nil {
			return "", err
		}
		return state.State, nil
	}
	return "", nil
}

func completeExecutorWorkspace(ctx context.Context, deps ExecutorDependencies, info executorWorkspaceInfo, cause error) (*workspaces.WorkspaceStateFile, error) {
	if deps.WorkspaceManager == nil || info.UserID == "" || info.WorkspaceID == "" {
		return nil, nil
	}
	if info.Mode == workspaces.ModeReadonly {
		return nil, nil
	}
	message := ""
	if cause != nil {
		message = cause.Error()
	}
	return deps.WorkspaceManager.CompleteTaskWorkspace(ctx, info.UserID, info.WorkspaceID, message)
}

func executorWorkspaceID(task *coretasks.Task, input RunTaskInput) string {
	if normalizeWorkspaceMode(input.WorkspaceMode) == workspaces.ModeReadonly {
		return task.ID
	}
	return firstNonEmpty(input.ConversationID, task.ID)
}

func hydrateReplayMessages(ctx context.Context, store *attachments.Store, storage attachments.Storage, messages []model.Message, conversationID string, workspaceRoot string, directImageInput bool, allowExpiredHistoryContinuation bool) ([]model.Message, error) {
	if len(messages) == 0 {
		return nil, nil
	}
	hydratedMessages := cloneMessages(messages)
	for messageIndex := range hydratedMessages {
		if len(hydratedMessages[messageIndex].Attachments) == 0 {
			continue
		}
		hydrated, err := planReplayMessageAttachments(ctx, store, storage, hydratedMessages[messageIndex], workspaceRoot, conversationID, directImageInput)
		if err != nil {
			if allowExpiredHistoryContinuation && errors.Is(err, attachments.ErrAttachmentExpired) {
				// Expired attachments in history: drop the attachment references but keep
				// the message content (which already contains the manifest text describing
				// what was uploaded). This allows conversation continuation even when local
				// attachment files have expired after long idle periods.
				hydratedMessages[messageIndex].Attachments = nil
				continue
			}
			return nil, err
		}
		hydratedMessages[messageIndex] = hydrated
	}
	return hydratedMessages, nil
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

func persistConversationMemoryState(ctx context.Context, store *ConversationStore, conversationID string, manager *memory.Manager, compression *MemoryCompressionSnapshot) (*MemoryContextSnapshot, error) {
	if manager == nil || store == nil {
		return nil, nil
	}
	if err := store.SetMemorySummary(ctx, conversationID, manager.Summary()); err != nil {
		corelog.Warn("failed to persist memory summary", corelog.String("component", "agent"), corelog.String("module", "executor"), corelog.String("conversation_id", conversationID), corelog.Err(err))
	}
	memoryContext := newMemoryContextSnapshot(manager.ContextState())
	if err := store.SetMemorySnapshots(ctx, conversationID, memoryContext, compression); err != nil {
		return nil, err
	}
	return memoryContext, nil
}

func buildMemoryManager(factory MemoryFactory, client model.LlmClient, llmModel *coretypes.LLMModel) (*memory.Manager, error) {
	if factory != nil {
		return factory(llmModel)
	}
	return memory.NewManager(memory.Options{
		Model:      llmModel,
		Counter:    memory.NewTokenCounterForModel(llmModel),
		Compressor: memory.NewLLMShortTermCompressor(memory.LLMCompressorOptions{Client: client, Model: llmModel.ModelID()}),
	})
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

func normalizeWorkspaceMode(mode workspaces.Mode) workspaces.Mode {
	switch mode {
	case workspaces.ModeReadonly:
		return workspaces.ModeReadonly
	default:
		return workspaces.ModeMutable
	}
}

// trimIncompleteToolCallMessages keeps only the longest prefix whose tool-call
// turns are replayable. Partial multi-tool turns must be dropped as a unit; an
// orphan tool message is just as invalid for the next provider request as a
// dangling assistant tool_call.
func trimIncompleteToolCallMessages(messages []model.Message) []model.Message {
	if len(messages) == 0 {
		return messages
	}

	out := make([]model.Message, 0, len(messages))
	for index := 0; index < len(messages); index++ {
		message := messages[index]
		if message.Role == model.RoleTool {
			break
		}
		if message.Role != model.RoleAssistant || len(message.ToolCalls) == 0 {
			out = append(out, message)
			continue
		}

		toolResultEnd, ok := completedToolTurnEnd(messages[index:])
		if !ok {
			break
		}
		out = append(out, messages[index:index+toolResultEnd]...)
		index += toolResultEnd - 1
	}
	return out
}

func sanitizeReplayMessages(messages []model.Message) []model.Message {
	if len(messages) == 0 {
		return messages
	}
	out := make([]model.Message, 0, len(messages))
	for index := 0; index < len(messages); index++ {
		message := messages[index]
		if message.Role == model.RoleTool {
			continue
		}
		if message.Role != model.RoleAssistant || len(message.ToolCalls) == 0 {
			out = append(out, message)
			continue
		}
		toolResultEnd, ok := completedToolTurnEnd(messages[index:])
		if !ok {
			index = skipIncompleteToolTurn(messages, index)
			continue
		}
		out = append(out, messages[index:index+toolResultEnd]...)
		index += toolResultEnd - 1
	}
	return out
}

func skipIncompleteToolTurn(messages []model.Message, assistantIndex int) int {
	if assistantIndex < 0 || assistantIndex >= len(messages) {
		return assistantIndex
	}
	callIDs := make(map[string]struct{}, len(messages[assistantIndex].ToolCalls))
	for _, call := range messages[assistantIndex].ToolCalls {
		if strings.TrimSpace(call.ID) != "" {
			callIDs[call.ID] = struct{}{}
		}
	}
	index := assistantIndex + 1
	for index < len(messages) {
		message := messages[index]
		if message.Role != model.RoleTool {
			break
		}
		if len(callIDs) > 0 {
			if _, ok := callIDs[message.ToolCallId]; !ok {
				break
			}
		}
		index++
	}
	return index - 1
}

func completedToolTurnEnd(messages []model.Message) (int, bool) {
	if len(messages) == 0 || messages[0].Role != model.RoleAssistant || len(messages[0].ToolCalls) == 0 {
		return 0, false
	}

	required := make(map[string]struct{}, len(messages[0].ToolCalls))
	for _, call := range messages[0].ToolCalls {
		if strings.TrimSpace(call.ID) == "" {
			return 0, false
		}
		required[call.ID] = struct{}{}
	}

	end := 1
	for ; end < len(messages) && len(required) > 0; end++ {
		message := messages[end]
		if message.Role != model.RoleTool {
			return 0, false
		}
		if _, ok := required[message.ToolCallId]; !ok {
			return 0, false
		}
		delete(required, message.ToolCallId)
	}
	if len(required) != 0 {
		return 0, false
	}
	return end, true
}
