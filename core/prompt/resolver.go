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

func (r *ResolvedPrompt) AppendSegment(segment ResolvedPromptSegment) {
	if r == nil {
		return
	}
	order := 0
	if len(r.Segments) > 0 {
		order = r.Segments[len(r.Segments)-1].Order
		if order <= 0 {
			order = len(r.Segments)
		}
	}
	r.appendSegment(&order, segment)
}

func (r *ResolvedPrompt) InsertSessionSegmentsBeforeLaterPhases(segments []ResolvedPromptSegment) {
	if r == nil || len(segments) == 0 {
		return
	}
	insertAt := len(r.Segments)
	for i, existing := range r.Segments {
		if strings.TrimSpace(existing.Phase) != promptPhaseSession {
			insertAt = i
			break
		}
	}

	prepared := make([]ResolvedPromptSegment, 0, len(segments))
	for _, segment := range segments {
		if strings.TrimSpace(segment.Phase) != promptPhaseSession || strings.TrimSpace(segment.Content) == "" {
			continue
		}
		segment.Order = 0
		prepared = append(prepared, segment)
	}
	if len(prepared) == 0 {
		return
	}

	merged := make([]ResolvedPromptSegment, 0, len(r.Segments)+len(prepared))
	merged = append(merged, r.Segments[:insertAt]...)
	merged = append(merged, prepared...)
	merged = append(merged, r.Segments[insertAt:]...)

	r.Segments = nil
	r.Session = nil
	r.StepPreModel = nil
	r.ToolResult = nil
	order := 0
	for _, segment := range merged {
		r.appendSegment(&order, segment)
	}
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
