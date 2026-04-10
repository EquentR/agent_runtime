package runtimeprompt

import (
	"strings"
	"time"

	"github.com/EquentR/agent_runtime/core/prompt"
	model "github.com/EquentR/agent_runtime/core/providers/types"
)

type SessionSegmentProvider interface {
	SessionSegments(now time.Time) ([]Segment, error)
}

type Builder struct {
	provider SessionSegmentProvider
}

type BuildInput struct {
	Time               time.Time
	ConversationBody   []model.Message
	ResolvedPrompt     *prompt.ResolvedPrompt
	AfterToolTurn      bool
	LegacySystemPrompt string
}

type BuildResult struct {
	Envelope      Envelope
	Body          []model.Message
	AfterToolTurn bool
}

func NewBuilder(provider SessionSegmentProvider) *Builder {
	return &Builder{provider: provider}
}

func (b *Builder) Build(input BuildInput) (BuildResult, error) {
	result := BuildResult{
		Body:          cloneMessages(input.ConversationBody),
		AfterToolTurn: input.AfterToolTurn,
	}

	segments := make([]Segment, 0)
	order := 0

	if b != nil && b.provider != nil {
		forcedSegments, err := b.provider.SessionSegments(input.Time)
		if err != nil {
			return BuildResult{}, err
		}
		for _, segment := range forcedSegments {
			if !isSupportedPhase(segment.Phase) || strings.TrimSpace(segment.Content) == "" {
				continue
			}
			order++
			segment.Order = order
			if strings.TrimSpace(segment.Role) == "" {
				segment.Role = RoleSystem
			}
			segments = append(segments, segment)
		}
	}

	if input.ResolvedPrompt != nil {
		resolvedSegments := input.ResolvedPrompt.Segments
		if len(resolvedSegments) == 0 {
			resolvedSegments = synthesizeResolvedPromptSegments(input.ResolvedPrompt)
		}
		for _, resolved := range resolvedSegments {
			if !isSupportedPhase(resolved.Phase) || strings.TrimSpace(resolved.Content) == "" {
				continue
			}
			order++
			segments = append(segments, Segment{
				SourceType:   SourceTypeResolvedPrompt,
				SourceKey:    resolvedSegmentSourceKey(resolved),
				Phase:        resolved.Phase,
				Order:        order,
				Role:         RoleSystem,
				Content:      resolved.Content,
				RuntimeOnly:  resolved.RuntimeOnly,
				AuditVisible: !resolved.RuntimeOnly,
			})
		}
	} else if legacyPrompt := strings.TrimSpace(input.LegacySystemPrompt); legacyPrompt != "" {
		order++
		segments = append(segments, Segment{
			SourceType: SourceTypeResolvedPrompt,
			SourceKey:  "legacy_system_prompt",
			Phase:      PhaseSession,
			Order:      order,
			Role:       RoleSystem,
			Content:    legacyPrompt,
		})
	}

	result.Envelope = Envelope{Segments: segments}
	return result, nil
}

func synthesizeResolvedPromptSegments(resolved *prompt.ResolvedPrompt) []prompt.ResolvedPromptSegment {
	if resolved == nil {
		return nil
	}
	segments := make([]prompt.ResolvedPromptSegment, 0, len(resolved.Session)+len(resolved.StepPreModel)+len(resolved.ToolResult))
	appendPhaseSegments := func(phase string, messages []model.Message) {
		for _, message := range messages {
			content := strings.TrimSpace(message.Content)
			if content == "" {
				continue
			}
			segments = append(segments, prompt.ResolvedPromptSegment{
				Order:   len(segments) + 1,
				Phase:   phase,
				Content: content,
			})
		}
	}
	appendPhaseSegments(PhaseSession, resolved.Session)
	appendPhaseSegments(PhaseStepPreModel, resolved.StepPreModel)
	appendPhaseSegments(PhaseToolResult, resolved.ToolResult)
	return segments
}

func resolvedSegmentSourceKey(segment prompt.ResolvedPromptSegment) string {
	if key := strings.TrimSpace(segment.SourceRef); key != "" {
		return key
	}
	if key := strings.TrimSpace(segment.SourceKind); key != "" {
		return key
	}
	return SourceTypeResolvedPrompt
}

func isSupportedPhase(phase string) bool {
	switch strings.TrimSpace(phase) {
	case PhaseSession, PhaseStepPreModel, PhaseToolResult:
		return true
	default:
		return false
	}
}
