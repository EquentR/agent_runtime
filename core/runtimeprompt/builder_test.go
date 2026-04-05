package runtimeprompt

import (
	"reflect"
	"testing"
	"time"

	"github.com/EquentR/agent_runtime/core/prompt"
	model "github.com/EquentR/agent_runtime/core/providers/types"
)

func TestBuilderBuildCollectsForcedMemoryAndResolvedSegmentsInEnvelopeOrder(t *testing.T) {
	builder := NewBuilder(fakeSessionSegmentProvider{segments: []Segment{
		{
			SourceType:   SourceTypeForcedBlock,
			SourceKey:    "forced_1",
			Phase:        PhaseSession,
			Order:        1,
			Role:         RoleSystem,
			Content:      "Forced prompt 1",
			RuntimeOnly:  true,
			AuditVisible: true,
		},
		{
			SourceType:   SourceTypeForcedBlock,
			SourceKey:    "forced_2",
			Phase:        PhaseSession,
			Order:        2,
			Role:         RoleSystem,
			Content:      "Forced prompt 2",
			RuntimeOnly:  true,
			AuditVisible: true,
		},
	}})

	resolved := &prompt.ResolvedPrompt{
		Segments: []prompt.ResolvedPromptSegment{
			{Order: 1, Phase: PhaseSession, Content: "Resolved session", SourceKind: "db_default_binding", SourceRef: "binding:1", RuntimeOnly: true},
			{Order: 2, Phase: PhaseStepPreModel, Content: "Resolved step", SourceKind: "workspace_file", SourceRef: "AGENTS.md", RuntimeOnly: true},
			{Order: 3, Phase: PhaseToolResult, Content: "Resolved tool", SourceKind: "db_default_binding", SourceRef: "binding:2"},
		},
	}
	conversation := []model.Message{{Role: model.RoleUser, Content: "hello"}}

	got, err := builder.Build(BuildInput{
		Time:             time.Date(2026, time.April, 4, 9, 30, 0, 0, time.UTC),
		ConversationBody: conversation,
		MemorySummary:    "Compressed memory summary",
		ResolvedPrompt:   resolved,
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	if got.AfterToolTurn {
		t.Fatal("AfterToolTurn = true, want false")
	}
	if !reflect.DeepEqual(got.Body, conversation) {
		t.Fatalf("Body = %#v, want %#v", got.Body, conversation)
	}
	if len(got.Envelope.Segments) != 6 {
		t.Fatalf("len(Envelope.Segments) = %d, want 6", len(got.Envelope.Segments))
	}

	wantContents := []string{
		"Forced prompt 1",
		"Forced prompt 2",
		"Compressed memory summary",
		"Resolved session",
		"Resolved step",
		"Resolved tool",
	}
	if gotContents := segmentContents(got.Envelope.Segments); !reflect.DeepEqual(gotContents, wantContents) {
		t.Fatalf("segment contents = %#v, want %#v", gotContents, wantContents)
	}

	memory := got.Envelope.Segments[2]
	if memory.SourceType != SourceTypeMemorySummary {
		t.Fatalf("memory.SourceType = %q, want %q", memory.SourceType, SourceTypeMemorySummary)
	}
	if memory.SourceKey != "memory_summary" {
		t.Fatalf("memory.SourceKey = %q, want %q", memory.SourceKey, "memory_summary")
	}
	if memory.Phase != PhaseSession {
		t.Fatalf("memory.Phase = %q, want %q", memory.Phase, PhaseSession)
	}
	if memory.Role != RoleSystem {
		t.Fatalf("memory.Role = %q, want %q", memory.Role, RoleSystem)
	}

	resolvedSession := got.Envelope.Segments[3]
	if resolvedSession.SourceType != SourceTypeResolvedPrompt {
		t.Fatalf("resolved session SourceType = %q, want %q", resolvedSession.SourceType, SourceTypeResolvedPrompt)
	}
	if resolvedSession.SourceKey != "binding:1" {
		t.Fatalf("resolved session SourceKey = %q, want %q", resolvedSession.SourceKey, "binding:1")
	}
	if !resolvedSession.RuntimeOnly {
		t.Fatal("resolved session RuntimeOnly = false, want true")
	}

	resolvedStep := got.Envelope.Segments[4]
	if resolvedStep.Phase != PhaseStepPreModel {
		t.Fatalf("resolved step Phase = %q, want %q", resolvedStep.Phase, PhaseStepPreModel)
	}
	if resolvedStep.SourceKey != "AGENTS.md" {
		t.Fatalf("resolved step SourceKey = %q, want %q", resolvedStep.SourceKey, "AGENTS.md")
	}

	for i, segment := range got.Envelope.Segments {
		wantOrder := i + 1
		if segment.Order != wantOrder {
			t.Fatalf("segment[%d].Order = %d, want %d", i, segment.Order, wantOrder)
		}
	}
}

func TestBuilderBuildSynthesizesResolvedPromptSegmentsWhenSegmentsAbsent(t *testing.T) {
	builder := NewBuilder(nil)

	got, err := builder.Build(BuildInput{
		ResolvedPrompt: &prompt.ResolvedPrompt{
			Session:      []model.Message{{Role: model.RoleSystem, Content: "Resolved session"}},
			StepPreModel: []model.Message{{Role: model.RoleSystem, Content: "Resolved step"}},
			ToolResult:   []model.Message{{Role: model.RoleSystem, Content: "Resolved tool"}},
		},
		LegacySystemPrompt: "Legacy prompt should not be used",
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	want := []Segment{
		{
			SourceType:   SourceTypeResolvedPrompt,
			SourceKey:    SourceTypeResolvedPrompt,
			Phase:        PhaseSession,
			Order:        1,
			Role:         RoleSystem,
			Content:      "Resolved session",
			AuditVisible: true,
		},
		{
			SourceType:   SourceTypeResolvedPrompt,
			SourceKey:    SourceTypeResolvedPrompt,
			Phase:        PhaseStepPreModel,
			Order:        2,
			Role:         RoleSystem,
			Content:      "Resolved step",
			AuditVisible: true,
		},
		{
			SourceType:   SourceTypeResolvedPrompt,
			SourceKey:    SourceTypeResolvedPrompt,
			Phase:        PhaseToolResult,
			Order:        3,
			Role:         RoleSystem,
			Content:      "Resolved tool",
			AuditVisible: true,
		},
	}
	if !reflect.DeepEqual(got.Envelope.Segments, want) {
		t.Fatalf("Envelope.Segments = %#v, want %#v", got.Envelope.Segments, want)
	}
}

func TestBuilderBuildUsesLegacySystemPromptFallbackWhenResolvedPromptAbsent(t *testing.T) {
	builder := NewBuilder(nil)

	got, err := builder.Build(BuildInput{
		LegacySystemPrompt: "Legacy prompt",
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(got.Envelope.Segments) != 1 {
		t.Fatalf("len(Envelope.Segments) = %d, want 1", len(got.Envelope.Segments))
	}

	segment := got.Envelope.Segments[0]
	if segment.SourceType != SourceTypeResolvedPrompt {
		t.Fatalf("segment.SourceType = %q, want %q", segment.SourceType, SourceTypeResolvedPrompt)
	}
	if segment.SourceKey != "legacy_system_prompt" {
		t.Fatalf("segment.SourceKey = %q, want %q", segment.SourceKey, "legacy_system_prompt")
	}
	if segment.Phase != PhaseSession {
		t.Fatalf("segment.Phase = %q, want %q", segment.Phase, PhaseSession)
	}
	if segment.Role != RoleSystem {
		t.Fatalf("segment.Role = %q, want %q", segment.Role, RoleSystem)
	}
	if segment.Content != "Legacy prompt" {
		t.Fatalf("segment.Content = %q, want %q", segment.Content, "Legacy prompt")
	}
}

func TestRendererRenderPlacesSessionBeforeBodyAndToolResultBeforeTrailingToolMessages(t *testing.T) {
	renderer := NewRenderer()

	got := renderer.Render(BuildResult{
		Envelope: Envelope{Segments: []Segment{
			{Order: 1, Phase: PhaseSession, Role: RoleSystem, Content: "Forced prompt"},
			{Order: 2, Phase: PhaseSession, Role: RoleSystem, Content: "Memory summary"},
			{Order: 3, Phase: PhaseSession, Role: RoleSystem, Content: "Resolved session"},
			{Order: 4, Phase: PhaseStepPreModel, Role: RoleSystem, Content: "Resolved step"},
			{Order: 5, Phase: PhaseToolResult, Role: RoleSystem, Content: "Resolved tool-result"},
		}},
		Body: []model.Message{
			{Role: model.RoleUser, Content: "weather?"},
			{Role: model.RoleAssistant, Content: ""},
			{Role: model.RoleTool, ToolCallId: "call_1", Content: "sunny"},
			{Role: model.RoleTool, ToolCallId: "call_2", Content: "08:00"},
		},
		AfterToolTurn: true,
	})

	want := []model.Message{
		{Role: model.RoleSystem, Content: "Forced prompt"},
		{Role: model.RoleSystem, Content: "Memory summary"},
		{Role: model.RoleSystem, Content: "Resolved session"},
		{Role: model.RoleSystem, Content: "Resolved step"},
		{Role: model.RoleUser, Content: "weather?"},
		{Role: model.RoleAssistant, Content: ""},
		{Role: model.RoleSystem, Content: "Resolved tool-result"},
		{Role: model.RoleTool, ToolCallId: "call_1", Content: "sunny"},
		{Role: model.RoleTool, ToolCallId: "call_2", Content: "08:00"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Render() = %#v, want %#v", got, want)
	}
}

func TestRendererRenderAppendsToolResultPromptWhenAssistantToolBoundaryMissing(t *testing.T) {
	renderer := NewRenderer()

	got := renderer.Render(BuildResult{
		Envelope: Envelope{Segments: []Segment{
			{Order: 1, Phase: PhaseStepPreModel, Role: RoleSystem, Content: "Resolved step"},
			{Order: 2, Phase: PhaseToolResult, Role: RoleSystem, Content: "Resolved tool-result"},
		}},
		Body:          []model.Message{{Role: model.RoleSystem, Content: "compressed memory"}},
		AfterToolTurn: true,
	})

	want := []model.Message{
		{Role: model.RoleSystem, Content: "Resolved step"},
		{Role: model.RoleSystem, Content: "compressed memory"},
		{Role: model.RoleSystem, Content: "Resolved tool-result"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Render() = %#v, want %#v", got, want)
	}
}

func TestRendererRenderSkipsToolResultPromptsBeforeToolTurn(t *testing.T) {
	renderer := NewRenderer()

	got := renderer.Render(BuildResult{
		Envelope: Envelope{Segments: []Segment{
			{Order: 1, Phase: PhaseSession, Role: RoleSystem, Content: "Forced prompt"},
			{Order: 2, Phase: PhaseToolResult, Role: RoleSystem, Content: "Resolved tool-result"},
		}},
		Body:          []model.Message{{Role: model.RoleUser, Content: "hello"}},
		AfterToolTurn: false,
	})

	want := []model.Message{
		{Role: model.RoleSystem, Content: "Forced prompt"},
		{Role: model.RoleUser, Content: "hello"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Render() = %#v, want %#v", got, want)
	}
}

type fakeSessionSegmentProvider struct {
	segments []Segment
	err      error
}

func (p fakeSessionSegmentProvider) SessionSegments(time.Time) ([]Segment, error) {
	return p.segments, p.err
}

func segmentContents(segments []Segment) []string {
	out := make([]string, 0, len(segments))
	for _, segment := range segments {
		out = append(out, segment.Content)
	}
	return out
}
