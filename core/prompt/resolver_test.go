package prompt

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	model "github.com/EquentR/agent_runtime/core/providers/types"
)

func TestResolverResolveOrdersMultipleDefaultsLegacyAndWorkspaceInSession(t *testing.T) {
	store := newTestStore(t)
	workspaceRoot := t.TempDir()
	writeWorkspaceFile(t, workspaceRoot, workspacePromptFileName, "# Workspace\nUse repo-specific conventions.\n")

	mustCreateDocument(t, store, CreateDocumentInput{ID: "doc-session-created-first", Name: "Session created first", Content: "DB session created first", Scope: "admin", Status: "active"})
	mustCreateDocument(t, store, CreateDocumentInput{ID: "doc-session-created-second", Name: "Session created second", Content: "DB session created second", Scope: "admin", Status: "active"})
	mustCreateDocument(t, store, CreateDocumentInput{ID: "doc-session-lower-priority", Name: "Session lower priority", Content: "DB session lower priority", Scope: "admin", Status: "active"})
	mustCreateDocument(t, store, CreateDocumentInput{ID: "doc-step", Name: "Step", Content: "DB step prompt", Scope: "admin", Status: "active"})
	mustCreateDocument(t, store, CreateDocumentInput{ID: "doc-tool", Name: "Tool", Content: "DB tool prompt", Scope: "admin", Status: "active"})

	base := time.Date(2026, time.March, 24, 9, 0, 0, 0, time.UTC)
	sessionSecond := mustCreateBinding(t, store, CreateBindingInput{PromptID: "doc-session-created-second", Scene: "agent.run.default", Phase: promptPhaseSession, IsDefault: true, Priority: 1, Status: "active"})
	sessionFirst := mustCreateBinding(t, store, CreateBindingInput{PromptID: "doc-session-created-first", Scene: "agent.run.default", Phase: promptPhaseSession, IsDefault: true, Priority: 1, Status: "active"})
	sessionLowerPriority := mustCreateBinding(t, store, CreateBindingInput{PromptID: "doc-session-lower-priority", Scene: "agent.run.default", Phase: promptPhaseSession, IsDefault: true, Priority: 5, Status: "active"})
	stepBinding := mustCreateBinding(t, store, CreateBindingInput{PromptID: "doc-step", Scene: "agent.run.default", Phase: promptPhaseStepPreModel, IsDefault: true, Priority: 1, Status: "active"})
	toolBinding := mustCreateBinding(t, store, CreateBindingInput{PromptID: "doc-tool", Scene: "agent.run.default", Phase: promptPhaseToolResult, IsDefault: true, Priority: 1, Status: "active"})

	setBindingCreatedAt(t, store, sessionSecond.ID, base.Add(2*time.Minute))
	setBindingCreatedAt(t, store, sessionFirst.ID, base)
	setBindingCreatedAt(t, store, sessionLowerPriority.ID, base.Add(-5*time.Minute))
	setBindingCreatedAt(t, store, stepBinding.ID, base)
	setBindingCreatedAt(t, store, toolBinding.ID, base)

	resolver := NewResolver(store)
	resolved, err := resolver.Resolve(context.Background(), ResolveInput{
		Scene:              " agent.run.default ",
		ProviderID:         " ",
		ModelID:            " ",
		LegacySystemPrompt: "Legacy session prompt",
		WorkspaceRoot:      workspaceRoot,
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if got := messageContents(resolved.Session); !reflect.DeepEqual(got, []string{
		"DB session created first",
		"DB session created second",
		"DB session lower priority",
		"Legacy session prompt",
		"The following AGENTS.md file was injected from the user's working directory. Treat it as guidance and operating rules for the current workspace.\n---\n# Workspace\nUse repo-specific conventions.\n",
	}) {
		t.Fatalf("session contents = %#v, want %#v", got, []string{
			"DB session created first",
			"DB session created second",
			"DB session lower priority",
			"Legacy session prompt",
			"The following AGENTS.md file was injected from the user's working directory. Treat it as guidance and operating rules for the current workspace.\n---\n# Workspace\nUse repo-specific conventions.\n",
		})
	}
	if got := messageContents(resolved.StepPreModel); !reflect.DeepEqual(got, []string{"DB step prompt"}) {
		t.Fatalf("step_pre_model contents = %#v, want %#v", got, []string{"DB step prompt"})
	}
	if got := messageContents(resolved.ToolResult); !reflect.DeepEqual(got, []string{"DB tool prompt"}) {
		t.Fatalf("tool_result contents = %#v, want %#v", got, []string{"DB tool prompt"})
	}

	assertAllSystemMessages(t, resolved.Session)
	assertAllSystemMessages(t, resolved.StepPreModel)
	assertAllSystemMessages(t, resolved.ToolResult)

	if got := segmentContents(resolved.Segments); !reflect.DeepEqual(got, []string{
		"DB session created first",
		"DB session created second",
		"DB session lower priority",
		"Legacy session prompt",
		"The following AGENTS.md file was injected from the user's working directory. Treat it as guidance and operating rules for the current workspace.\n---\n# Workspace\nUse repo-specific conventions.\n",
		"DB step prompt",
		"DB tool prompt",
	}) {
		t.Fatalf("segment contents = %#v, want %#v", got, []string{
			"DB session created first",
			"DB session created second",
			"DB session lower priority",
			"Legacy session prompt",
			"The following AGENTS.md file was injected from the user's working directory. Treat it as guidance and operating rules for the current workspace.\n---\n# Workspace\nUse repo-specific conventions.\n",
			"DB step prompt",
			"DB tool prompt",
		})
	}

	if len(resolved.Segments) != 7 {
		t.Fatalf("len(segments) = %d, want 7", len(resolved.Segments))
	}
	if resolved.Segments[0].Order != 1 || resolved.Segments[6].Order != 7 {
		t.Fatalf("segment orders = [%d ... %d], want [1 ... 7]", resolved.Segments[0].Order, resolved.Segments[6].Order)
	}

	assertDBSegmentMetadata(t, resolved.Segments[0], promptPhaseSession, sessionFirst.ID, "doc-session-created-first", "Session created first", "admin")
	assertDBSegmentMetadata(t, resolved.Segments[1], promptPhaseSession, sessionSecond.ID, "doc-session-created-second", "Session created second", "admin")
	assertDBSegmentMetadata(t, resolved.Segments[2], promptPhaseSession, sessionLowerPriority.ID, "doc-session-lower-priority", "Session lower priority", "admin")

	if resolved.Segments[3].Phase != promptPhaseSession {
		t.Fatalf("legacy segment phase = %q, want %q", resolved.Segments[3].Phase, promptPhaseSession)
	}
	if resolved.Segments[3].SourceKind != promptSourceKindLegacySystemPrompt {
		t.Fatalf("legacy segment source kind = %q, want %q", resolved.Segments[3].SourceKind, promptSourceKindLegacySystemPrompt)
	}
	if resolved.Segments[3].SourceRef != promptSourceRefLegacySystemPrompt {
		t.Fatalf("legacy segment source ref = %q, want %q", resolved.Segments[3].SourceRef, promptSourceRefLegacySystemPrompt)
	}

	if resolved.Segments[4].Phase != promptPhaseSession {
		t.Fatalf("workspace segment phase = %q, want %q", resolved.Segments[4].Phase, promptPhaseSession)
	}
	if resolved.Segments[4].SourceKind != workspacePromptSourceKindWorkspaceFile {
		t.Fatalf("workspace segment source kind = %q, want %q", resolved.Segments[4].SourceKind, workspacePromptSourceKindWorkspaceFile)
	}
	if resolved.Segments[4].SourceRef != workspacePromptFileName {
		t.Fatalf("workspace segment source ref = %q, want %q", resolved.Segments[4].SourceRef, workspacePromptFileName)
	}
	if !resolved.Segments[4].RuntimeOnly {
		t.Fatal("workspace segment RuntimeOnly = false, want true")
	}

	assertDBSegmentMetadata(t, resolved.Segments[5], promptPhaseStepPreModel, stepBinding.ID, "doc-step", "Step", "admin")
	assertDBSegmentMetadata(t, resolved.Segments[6], promptPhaseToolResult, toolBinding.ID, "doc-tool", "Tool", "admin")
}

func TestResolverResolvePassesProviderAndModelFiltersToStoreDefaults(t *testing.T) {
	store := newTestStore(t)

	mustCreateDocument(t, store, CreateDocumentInput{ID: "doc-generic", Name: "Generic", Content: "Generic prompt", Scope: "admin", Status: "active"})
	mustCreateDocument(t, store, CreateDocumentInput{ID: "doc-provider", Name: "Provider", Content: "Provider prompt", Scope: "admin", Status: "active"})
	mustCreateDocument(t, store, CreateDocumentInput{ID: "doc-model", Name: "Model", Content: "Model prompt", Scope: "admin", Status: "active"})
	mustCreateDocument(t, store, CreateDocumentInput{ID: "doc-other-provider", Name: "Other provider", Content: "Other provider prompt", Scope: "admin", Status: "active"})
	mustCreateDocument(t, store, CreateDocumentInput{ID: "doc-other-model", Name: "Other model", Content: "Other model prompt", Scope: "admin", Status: "active"})

	generic := mustCreateBinding(t, store, CreateBindingInput{PromptID: "doc-generic", Scene: "agent.run.default", Phase: promptPhaseSession, IsDefault: true, Status: "active"})
	providerOnly := mustCreateBinding(t, store, CreateBindingInput{PromptID: "doc-provider", Scene: "agent.run.default", Phase: promptPhaseSession, IsDefault: true, ProviderID: "openai", Status: "active"})
	modelSpecific := mustCreateBinding(t, store, CreateBindingInput{PromptID: "doc-model", Scene: "agent.run.default", Phase: promptPhaseSession, IsDefault: true, ProviderID: "openai", ModelID: "gpt-4o", Status: "active"})
	mustCreateBinding(t, store, CreateBindingInput{PromptID: "doc-other-provider", Scene: "agent.run.default", Phase: promptPhaseSession, IsDefault: true, ProviderID: "anthropic", Status: "active"})
	mustCreateBinding(t, store, CreateBindingInput{PromptID: "doc-other-model", Scene: "agent.run.default", Phase: promptPhaseSession, IsDefault: true, ProviderID: "openai", ModelID: "gpt-4.1", Status: "active"})

	resolver := NewResolver(store)
	resolved, err := resolver.Resolve(context.Background(), ResolveInput{
		Scene:      "agent.run.default",
		ProviderID: " openai ",
		ModelID:    " gpt-4o ",
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if got := messageContents(resolved.Session); !reflect.DeepEqual(got, []string{"Generic prompt", "Provider prompt", "Model prompt"}) {
		t.Fatalf("session contents = %#v, want %#v", got, []string{"Generic prompt", "Provider prompt", "Model prompt"})
	}
	if len(resolved.StepPreModel) != 0 {
		t.Fatalf("len(step_pre_model) = %d, want 0", len(resolved.StepPreModel))
	}
	if len(resolved.ToolResult) != 0 {
		t.Fatalf("len(tool_result) = %d, want 0", len(resolved.ToolResult))
	}
	if got := bindingIDsFromSegments(resolved.Segments); !reflect.DeepEqual(got, []uint64{generic.ID, providerOnly.ID, modelSpecific.ID}) {
		t.Fatalf("segment binding ids = %#v, want %#v", got, []uint64{generic.ID, providerOnly.ID, modelSpecific.ID})
	}
}

func TestResolverResolveIgnoresEmptyPromptContentAcrossSources(t *testing.T) {
	store := newTestStore(t)
	workspaceRoot := t.TempDir()
	writeWorkspaceFile(t, workspaceRoot, workspacePromptFileName, "  \n\t  \n")

	mustCreateDocument(t, store, CreateDocumentInput{ID: "doc-empty-session", Name: "Empty session", Content: " \n\t ", Scope: "admin", Status: "active"})
	mustCreateDocument(t, store, CreateDocumentInput{ID: "doc-step", Name: "Step", Content: "Keep tool usage concise.", Scope: "admin", Status: "active"})
	mustCreateDocument(t, store, CreateDocumentInput{ID: "doc-empty-tool", Name: "Empty tool", Content: " \t  ", Scope: "admin", Status: "active"})

	mustCreateBinding(t, store, CreateBindingInput{PromptID: "doc-empty-session", Scene: "agent.run.default", Phase: promptPhaseSession, IsDefault: true, Status: "active"})
	stepBinding := mustCreateBinding(t, store, CreateBindingInput{PromptID: "doc-step", Scene: "agent.run.default", Phase: promptPhaseStepPreModel, IsDefault: true, Status: "active"})
	mustCreateBinding(t, store, CreateBindingInput{PromptID: "doc-empty-tool", Scene: "agent.run.default", Phase: promptPhaseToolResult, IsDefault: true, Status: "active"})

	resolver := NewResolver(store)
	resolved, err := resolver.Resolve(context.Background(), ResolveInput{
		Scene:              "agent.run.default",
		LegacySystemPrompt: " \n\t ",
		WorkspaceRoot:      workspaceRoot,
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if len(resolved.Session) != 0 {
		t.Fatalf("len(session) = %d, want 0", len(resolved.Session))
	}
	if got := messageContents(resolved.StepPreModel); !reflect.DeepEqual(got, []string{"Keep tool usage concise."}) {
		t.Fatalf("step_pre_model contents = %#v, want %#v", got, []string{"Keep tool usage concise."})
	}
	if len(resolved.ToolResult) != 0 {
		t.Fatalf("len(tool_result) = %d, want 0", len(resolved.ToolResult))
	}
	if len(resolved.Segments) != 1 {
		t.Fatalf("len(segments) = %d, want 1", len(resolved.Segments))
	}
	assertDBSegmentMetadata(t, resolved.Segments[0], promptPhaseStepPreModel, stepBinding.ID, "doc-step", "Step", "admin")
	if resolved.Segments[0].Content != "Keep tool usage concise." {
		t.Fatalf("segment content = %q, want %q", resolved.Segments[0].Content, "Keep tool usage concise.")
	}
}

func TestResolverResolveFailsFastWhenStoreMissing(t *testing.T) {
	workspaceRoot := t.TempDir()
	writeWorkspaceFile(t, workspaceRoot, workspacePromptFileName, "# Workspace\nUse repo conventions.\n")

	resolver := NewResolver(nil)
	resolved, err := resolver.Resolve(context.Background(), ResolveInput{
		Scene:              "agent.run.default",
		LegacySystemPrompt: "Legacy session prompt",
		WorkspaceRoot:      workspaceRoot,
	})
	if !errors.Is(err, ErrResolverStoreRequired) {
		t.Fatalf("Resolve() error = %v, want ErrResolverStoreRequired", err)
	}
	if resolved != nil {
		t.Fatalf("Resolve() result = %#v, want nil", resolved)
	}
}

func TestResolverResolveFailsFastWhenResolverMissing(t *testing.T) {
	workspaceRoot := t.TempDir()
	writeWorkspaceFile(t, workspaceRoot, workspacePromptFileName, "# Workspace\nUse repo conventions.\n")

	var resolver *Resolver
	resolved, err := resolver.Resolve(context.Background(), ResolveInput{
		Scene:              "agent.run.default",
		LegacySystemPrompt: "Legacy session prompt",
		WorkspaceRoot:      workspaceRoot,
	})
	if !errors.Is(err, ErrResolverStoreRequired) {
		t.Fatalf("Resolve() error = %v, want ErrResolverStoreRequired", err)
	}
	if resolved != nil {
		t.Fatalf("Resolve() result = %#v, want nil", resolved)
	}
}

func TestResolverResolveMissingStoreWinsOverCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	resolver := NewResolver(nil)
	resolved, err := resolver.Resolve(ctx, ResolveInput{Scene: "agent.run.default"})
	if !errors.Is(err, ErrResolverStoreRequired) {
		t.Fatalf("Resolve() error = %v, want ErrResolverStoreRequired", err)
	}
	if resolved != nil {
		t.Fatalf("Resolve() result = %#v, want nil", resolved)
	}
}

func TestResolverResolveIncludesPromptScopeInDBSegments(t *testing.T) {
	store := newTestStore(t)

	mustCreateDocument(t, store, CreateDocumentInput{ID: "doc-workspace-scope", Name: "Workspace scoped", Content: "Workspace scoped prompt", Scope: "workspace", Status: "active"})
	binding := mustCreateBinding(t, store, CreateBindingInput{PromptID: "doc-workspace-scope", Scene: "agent.run.default", Phase: promptPhaseSession, IsDefault: true, Status: "active"})

	resolver := NewResolver(store)
	resolved, err := resolver.Resolve(context.Background(), ResolveInput{Scene: "agent.run.default"})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if len(resolved.Segments) != 1 {
		t.Fatalf("len(segments) = %d, want 1", len(resolved.Segments))
	}

	assertDBSegmentMetadata(t, resolved.Segments[0], promptPhaseSession, binding.ID, "doc-workspace-scope", "Workspace scoped", "workspace")
}

func assertAllSystemMessages(t *testing.T, messages []model.Message) {
	t.Helper()

	for i, message := range messages {
		if message.Role != model.RoleSystem {
			t.Fatalf("message[%d].Role = %q, want %q", i, message.Role, model.RoleSystem)
		}
	}
}

func assertDBSegmentMetadata(t *testing.T, segment ResolvedPromptSegment, phase string, bindingID uint64, promptID string, promptName string, promptScope string) {
	t.Helper()

	if segment.Phase != phase {
		t.Fatalf("segment phase = %q, want %q", segment.Phase, phase)
	}
	if segment.SourceKind != promptSourceKindDBDefaultBinding {
		t.Fatalf("segment source kind = %q, want %q", segment.SourceKind, promptSourceKindDBDefaultBinding)
	}
	if segment.BindingID != bindingID {
		t.Fatalf("segment binding id = %d, want %d", segment.BindingID, bindingID)
	}
	if segment.PromptID != promptID {
		t.Fatalf("segment prompt id = %q, want %q", segment.PromptID, promptID)
	}
	if segment.PromptName != promptName {
		t.Fatalf("segment prompt name = %q, want %q", segment.PromptName, promptName)
	}
	if segment.PromptScope != promptScope {
		t.Fatalf("segment prompt scope = %q, want %q", segment.PromptScope, promptScope)
	}
}

func messageContents(messages []model.Message) []string {
	contents := make([]string, 0, len(messages))
	for _, message := range messages {
		contents = append(contents, message.Content)
	}
	return contents
}

func segmentContents(segments []ResolvedPromptSegment) []string {
	contents := make([]string, 0, len(segments))
	for _, segment := range segments {
		contents = append(contents, segment.Content)
	}
	return contents
}

func bindingIDsFromSegments(segments []ResolvedPromptSegment) []uint64 {
	ids := make([]uint64, 0, len(segments))
	for _, segment := range segments {
		ids = append(ids, segment.BindingID)
	}
	return ids
}
