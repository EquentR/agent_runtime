package prompt

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	model "github.com/EquentR/agent_runtime/core/providers/types"
)

const (
	promptPhaseSession      = "session"
	promptPhaseStepPreModel = "step_pre_model"
	promptPhaseToolResult   = "tool_result"

	promptSourceKindDBDefaultBinding   = "db_default_binding"
	promptSourceKindLegacySystemPrompt = "legacy_system_prompt"

	promptSourceRefLegacySystemPrompt = "system_prompt"
)

var ErrResolverStoreRequired = errors.New("prompt resolver store is required")

type Resolver struct {
	store *Store
}

type ResolveInput struct {
	Scene              string
	ProviderID         string
	ModelID            string
	LegacySystemPrompt string
	WorkspaceRoot      string
}

type ResolvedPrompt struct {
	Scene        string                  `json:"scene"`
	Segments     []ResolvedPromptSegment `json:"segments,omitempty"`
	Session      []model.Message         `json:"session,omitempty"`
	StepPreModel []model.Message         `json:"step_pre_model,omitempty"`
	ToolResult   []model.Message         `json:"tool_result,omitempty"`
}

type ResolvedPromptSegment struct {
	Order      int    `json:"order"`
	Phase      string `json:"phase"`
	Content    string `json:"content"`
	SourceKind string `json:"source_kind"`
	SourceRef  string `json:"source_ref"`

	RuntimeOnly bool   `json:"runtime_only,omitempty"`
	BindingID   uint64 `json:"binding_id,omitempty"`
	PromptID    string `json:"prompt_id,omitempty"`
	PromptName  string `json:"prompt_name,omitempty"`
	PromptScope string `json:"prompt_scope,omitempty"`
	ProviderID  string `json:"provider_id,omitempty"`
	ModelID     string `json:"model_id,omitempty"`
	Priority    int    `json:"priority,omitempty"`
}

func NewResolver(store *Store) *Resolver {
	return &Resolver{store: store}
}

func (r *Resolver) Resolve(ctx context.Context, input ResolveInput) (*ResolvedPrompt, error) {
	if r == nil || r.store == nil {
		return nil, ErrResolverStoreRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	scene := strings.TrimSpace(input.Scene)
	if scene == "" {
		return nil, fmt.Errorf("scene cannot be empty")
	}

	providerID := strings.TrimSpace(input.ProviderID)
	modelID := strings.TrimSpace(input.ModelID)
	resolved := &ResolvedPrompt{Scene: scene}
	order := 0

	if err := r.appendDefaultBindings(ctx, resolved, DefaultBindingFilter{
		Scene:      scene,
		Phase:      promptPhaseSession,
		ProviderID: providerID,
		ModelID:    modelID,
	}, &order); err != nil {
		return nil, err
	}

	if legacyPrompt := input.LegacySystemPrompt; strings.TrimSpace(legacyPrompt) != "" {
		resolved.appendSegment(&order, ResolvedPromptSegment{
			Phase:      promptPhaseSession,
			Content:    legacyPrompt,
			SourceKind: promptSourceKindLegacySystemPrompt,
			SourceRef:  promptSourceRefLegacySystemPrompt,
		})
	}

	workspaceSegments, err := LoadWorkspacePrompts(ctx, input.WorkspaceRoot)
	if err != nil {
		return nil, err
	}
	for _, workspaceSegment := range workspaceSegments {
		phase := strings.TrimSpace(workspaceSegment.Phase)
		if !isSupportedPromptPhase(phase) || strings.TrimSpace(workspaceSegment.Content) == "" {
			continue
		}
		resolved.appendSegment(&order, ResolvedPromptSegment{
			Phase:       phase,
			Content:     workspaceSegment.Content,
			SourceKind:  workspaceSegment.SourceKind,
			SourceRef:   workspaceSegment.SourceRef,
			RuntimeOnly: workspaceSegment.RuntimeOnly,
		})
	}

	for _, phase := range []string{promptPhaseStepPreModel, promptPhaseToolResult} {
		if err := r.appendDefaultBindings(ctx, resolved, DefaultBindingFilter{
			Scene:      scene,
			Phase:      phase,
			ProviderID: providerID,
			ModelID:    modelID,
		}, &order); err != nil {
			return nil, err
		}
	}

	return resolved, nil
}

func (r *Resolver) appendDefaultBindings(ctx context.Context, resolved *ResolvedPrompt, filter DefaultBindingFilter, order *int) error {
	if resolved == nil {
		return nil
	}

	bindings, err := r.store.ListDefaultBindings(ctx, filter)
	if err != nil {
		return err
	}

	for _, binding := range bindings {
		content := ""
		promptName := ""
		promptScope := ""
		if binding.Prompt != nil {
			content = binding.Prompt.Content
			promptName = binding.Prompt.Name
			promptScope = binding.Prompt.Scope
		}
		if strings.TrimSpace(content) == "" {
			continue
		}

		resolved.appendSegment(order, ResolvedPromptSegment{
			Phase:       filter.Phase,
			Content:     content,
			SourceKind:  promptSourceKindDBDefaultBinding,
			SourceRef:   formatBindingSourceRef(binding.ID),
			BindingID:   binding.ID,
			PromptID:    binding.PromptID,
			PromptName:  promptName,
			PromptScope: promptScope,
			ProviderID:  binding.ProviderID,
			ModelID:     binding.ModelID,
			Priority:    binding.Priority,
		})
	}

	return nil
}

func (r *ResolvedPrompt) appendSegment(order *int, segment ResolvedPromptSegment) {
	if r == nil || order == nil {
		return
	}
	if !isSupportedPromptPhase(segment.Phase) || strings.TrimSpace(segment.Content) == "" {
		return
	}

	*order = *order + 1
	segment.Order = *order
	r.Segments = append(r.Segments, segment)

	message := model.Message{Role: model.RoleSystem, Content: segment.Content}
	switch segment.Phase {
	case promptPhaseSession:
		r.Session = append(r.Session, message)
	case promptPhaseStepPreModel:
		r.StepPreModel = append(r.StepPreModel, message)
	case promptPhaseToolResult:
		r.ToolResult = append(r.ToolResult, message)
	}
}

func isSupportedPromptPhase(phase string) bool {
	switch strings.TrimSpace(phase) {
	case promptPhaseSession, promptPhaseStepPreModel, promptPhaseToolResult:
		return true
	default:
		return false
	}
}

func formatBindingSourceRef(bindingID uint64) string {
	return "binding:" + strconv.FormatUint(bindingID, 10)
}
