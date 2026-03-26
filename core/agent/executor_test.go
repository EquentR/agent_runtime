package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	coreaudit "github.com/EquentR/agent_runtime/core/audit"
	"github.com/EquentR/agent_runtime/core/memory"
	coreprompt "github.com/EquentR/agent_runtime/core/prompt"
	model "github.com/EquentR/agent_runtime/core/providers/types"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	coretools "github.com/EquentR/agent_runtime/core/tools"
	coretypes "github.com/EquentR/agent_runtime/core/types"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestResolveConfiguredModelByProviderAndModelID(t *testing.T) {
	resolver := &ModelResolver{Providers: []coretypes.LLMProvider{{
		BaseProvider: coretypes.BaseProvider{Name: "openai"},
		Models: []coretypes.LLMModel{{
			BaseModel: coretypes.BaseModel{ID: "gpt-5.4", Name: "GPT 5.4"},
			Type:      coretypes.LLMTypeOpenAIResponses,
		}},
	}}}
	resolved, err := resolver.Resolve("openai", "gpt-5.4")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved == nil || resolved.Model == nil || resolved.Model.ModelID() != "gpt-5.4" {
		t.Fatalf("resolved = %#v, want gpt-5.4", resolved)
	}
	if resolved.Provider == nil || resolved.Provider.ProviderName() != "openai" {
		t.Fatalf("resolved provider = %#v, want openai", resolved.Provider)
	}
}

func TestResolveConfiguredModelRejectsUnknownProvider(t *testing.T) {
	resolver := &ModelResolver{Providers: []coretypes.LLMProvider{{BaseProvider: coretypes.BaseProvider{Name: "openai"}}}}
	_, err := resolver.Resolve("google", "gpt-5.4")
	if err == nil {
		t.Fatal("Resolve() error = nil, want unknown provider error")
	}
}

func TestResolveConfiguredModelRejectsUnknownModel(t *testing.T) {
	resolver := &ModelResolver{Providers: []coretypes.LLMProvider{{BaseProvider: coretypes.BaseProvider{Name: "openai"}}}}
	_, err := resolver.Resolve("openai", "missing-model")
	if err == nil {
		t.Fatal("Resolve() error = nil, want unknown model error")
	}
}

func TestResolveConfiguredModelSupportsMultipleProviders(t *testing.T) {
	resolver := &ModelResolver{Providers: []coretypes.LLMProvider{
		{
			BaseProvider: coretypes.BaseProvider{Name: "openai"},
			Models: []coretypes.LLMModel{{
				BaseModel: coretypes.BaseModel{ID: "gpt-5.4", Name: "GPT 5.4"},
				Type:      coretypes.LLMTypeOpenAIResponses,
			}},
		},
		{
			BaseProvider: coretypes.BaseProvider{Name: "google"},
			Models: []coretypes.LLMModel{{
				BaseModel: coretypes.BaseModel{ID: "gemini-2.5-flash", Name: "Gemini 2.5 Flash"},
				Type:      coretypes.LLMTypeGoogle,
			}},
		},
	}}

	resolved, err := resolver.Resolve("google", "gemini-2.5-flash")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved == nil || resolved.Provider == nil || resolved.Provider.ProviderName() != "google" {
		t.Fatalf("resolved provider = %#v, want google", resolved)
	}
	if resolved.Model == nil || resolved.Model.ModelID() != "gemini-2.5-flash" {
		t.Fatalf("resolved model = %#v, want gemini-2.5-flash", resolved.Model)
	}
}

func TestAgentExecutorCreatesConversationWhenMissing(t *testing.T) {
	store := newConversationStoreForTest(t)
	resolver := &ModelResolver{Providers: []coretypes.LLMProvider{{
		BaseProvider: coretypes.BaseProvider{Name: "openai"},
		Models: []coretypes.LLMModel{{
			BaseModel: coretypes.BaseModel{ID: "gpt-5.4", Name: "GPT 5.4"},
			Type:      coretypes.LLMTypeOpenAIResponses,
		}},
	}}}
	executor := newTaskExecutorForTest(t, ExecutorDependencies{
		Resolver:          resolver,
		ConversationStore: store,
		ClientFactory: func(*coretypes.LLMProvider, *coretypes.LLMModel) (model.LlmClient, error) {
			return &stubClient{streams: []model.Stream{newStubStream(
				[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "hello"}}},
				model.Message{Role: model.RoleAssistant, Content: "hello"},
				nil,
			)}}, nil
		},
	})

	payload, _ := json.Marshal(RunTaskInput{ProviderID: "openai", ModelID: "gpt-5.4", Message: "hi", CreatedBy: "tester"})
	task := &coretasks.Task{ID: "task_1", TaskType: "agent.run", InputJSON: payload}
	result, err := executor(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("executor() error = %v", err)
	}
	runResult := result.(RunTaskResult)
	if runResult.ConversationID == "" {
		t.Fatal("ConversationID = empty, want created conversation id")
	}
	messages, err := store.ListMessages(context.Background(), runResult.ConversationID)
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("len(messages) = %d, want 2", len(messages))
	}
}

func TestAgentExecutorLoadsConversationHistoryAndAppendsNewTurn(t *testing.T) {
	store := newConversationStoreForTest(t)
	_, err := store.CreateConversation(context.Background(), CreateConversationInput{ID: "conv_1", ProviderID: "openai", ModelID: "gpt-5.4"})
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	if err := store.AppendMessages(context.Background(), "conv_1", "task_0", []model.Message{{Role: model.RoleUser, Content: "first"}, {Role: model.RoleAssistant, Content: "answer"}}); err != nil {
		t.Fatalf("AppendMessages() error = %v", err)
	}

	client := &stubClient{streams: []model.Stream{newStubStream(
		[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "second answer"}}},
		model.Message{Role: model.RoleAssistant, Content: "second answer"},
		nil,
	)}}
	resolver := &ModelResolver{Providers: []coretypes.LLMProvider{{
		BaseProvider: coretypes.BaseProvider{Name: "openai"},
		Models:       []coretypes.LLMModel{{BaseModel: coretypes.BaseModel{ID: "gpt-5.4", Name: "GPT 5.4"}, Type: coretypes.LLMTypeOpenAIResponses}},
	}}}
	executor := newTaskExecutorForTest(t, ExecutorDependencies{
		Resolver:          resolver,
		ConversationStore: store,
		ClientFactory:     func(*coretypes.LLMProvider, *coretypes.LLMModel) (model.LlmClient, error) { return client, nil },
	})
	payload, _ := json.Marshal(RunTaskInput{ConversationID: "conv_1", ProviderID: "openai", ModelID: "gpt-5.4", Message: "second"})
	task := &coretasks.Task{ID: "task_1", TaskType: "agent.run", InputJSON: payload}
	result, err := executor(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("executor() error = %v", err)
	}
	runResult := result.(RunTaskResult)
	if runResult.MessagesAppended != 2 {
		t.Fatalf("MessagesAppended = %d, want 2", runResult.MessagesAppended)
	}
	if len(client.streamRequests) != 1 || len(client.streamRequests[0].Messages) != 3 {
		t.Fatalf("stream request messages = %#v, want prior history plus new user message", client.streamRequests)
	}
	got, err := store.ListMessages(context.Background(), "conv_1")
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("len(messages) = %d, want 4", len(got))
	}
}

func TestAgentExecutorDoesNotReplayVisibleSystemFailureMessagesIntoNextModelRequest(t *testing.T) {
	store := newConversationStoreForTest(t)
	_, err := store.CreateConversation(context.Background(), CreateConversationInput{ID: "conv_1", ProviderID: "openai", ModelID: "gpt-5.4"})
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	if err := store.AppendMessages(context.Background(), "conv_1", "task_0", []model.Message{
		{Role: model.RoleUser, Content: "first"},
		{Role: model.RoleAssistant, Content: "answer"},
		newVisibleFailureSystemMessage("Run failed: upstream 502"),
	}); err != nil {
		t.Fatalf("AppendMessages() error = %v", err)
	}

	client := &stubClient{streams: []model.Stream{newStubStream(
		[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "second answer"}}},
		model.Message{Role: model.RoleAssistant, Content: "second answer"},
		nil,
	)}}
	executor := newTaskExecutorForTest(t, ExecutorDependencies{
		Resolver:          newExecutorResolverForTest(),
		ConversationStore: store,
		ClientFactory:     func(*coretypes.LLMProvider, *coretypes.LLMModel) (model.LlmClient, error) { return client, nil },
	})
	payload := marshalExecutorTaskInput(t, map[string]any{
		"conversation_id": "conv_1",
		"provider_id":     "openai",
		"model_id":        "gpt-5.4",
		"message":         "second",
	})
	task := &coretasks.Task{ID: "task_replay_visibility", TaskType: "agent.run", InputJSON: payload}

	_, err = executor(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("executor() error = %v", err)
	}
	if len(client.streamRequests) != 1 {
		t.Fatalf("stream request count = %d, want 1", len(client.streamRequests))
	}
	requestMessages := client.streamRequests[0].Messages
	if len(requestMessages) != 3 {
		t.Fatalf("request messages = %#v, want prior user/assistant plus new user only", requestMessages)
	}
	if requestMessages[0].Role != model.RoleUser || requestMessages[1].Role != model.RoleAssistant || requestMessages[2].Role != model.RoleUser {
		t.Fatalf("request messages = %#v, want no replayed visible system failure notice", requestMessages)
	}
	for _, message := range requestMessages {
		if message.Role == model.RoleSystem {
			t.Fatalf("request message = %#v, want UI-visible system failure excluded from replay history", message)
		}
	}

	uiMessages, err := store.ListMessages(context.Background(), "conv_1")
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(uiMessages) < 3 || uiMessages[2].Role != model.RoleSystem {
		t.Fatalf("ui messages = %#v, want visible system failure retained for UI history", uiMessages)
	}
}

func TestAgentExecutorPromptSceneDefaultsToAgentRunDefault(t *testing.T) {
	store := newConversationStoreForTest(t)
	workspaceRoot := t.TempDir()
	promptResolver := newExecutorPromptResolverForTest(t, func(store *coreprompt.Store) {
		mustCreateExecutorPromptDocument(t, store, coreprompt.CreateDocumentInput{ID: "doc-default", Name: "Default", Content: "Default scene prompt", Scope: "admin", Status: "active"})
		mustCreateExecutorPromptBinding(t, store, coreprompt.CreateBindingInput{PromptID: "doc-default", Scene: "agent.run.default", Phase: "session", IsDefault: true, Status: "active"})
	})
	client := &stubClient{streams: []model.Stream{newStubStream(
		[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "hello"}}},
		model.Message{Role: model.RoleAssistant, Content: "hello"},
		nil,
	)}}
	executor := newTaskExecutorForTest(t, ExecutorDependencies{
		Resolver:          newExecutorResolverForTest(),
		ConversationStore: store,
		PromptResolver:    promptResolver,
		WorkspaceRoot:     workspaceRoot,
		ClientFactory:     func(*coretypes.LLMProvider, *coretypes.LLMModel) (model.LlmClient, error) { return client, nil },
	})

	payload := marshalExecutorTaskInput(t, map[string]any{
		"provider_id": "openai",
		"model_id":    "gpt-5.4",
		"message":     "hi",
	})
	task := &coretasks.Task{ID: "task_prompt_default_scene", TaskType: "agent.run", InputJSON: payload}

	_, err := executor(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("executor() error = %v", err)
	}
	assertExecutorRequestSystemPrompt(t, client, "Default scene prompt")
}

func TestAgentExecutorPromptUsesExplicitScene(t *testing.T) {
	store := newConversationStoreForTest(t)
	workspaceRoot := t.TempDir()
	promptResolver := newExecutorPromptResolverForTest(t, func(store *coreprompt.Store) {
		mustCreateExecutorPromptDocument(t, store, coreprompt.CreateDocumentInput{ID: "doc-default", Name: "Default", Content: "Default scene prompt", Scope: "admin", Status: "active"})
		mustCreateExecutorPromptDocument(t, store, coreprompt.CreateDocumentInput{ID: "doc-review", Name: "Review", Content: "Review scene prompt", Scope: "admin", Status: "active"})
		mustCreateExecutorPromptBinding(t, store, coreprompt.CreateBindingInput{PromptID: "doc-default", Scene: "agent.run.default", Phase: "session", IsDefault: true, Status: "active"})
		mustCreateExecutorPromptBinding(t, store, coreprompt.CreateBindingInput{PromptID: "doc-review", Scene: "agent.run.review", Phase: "session", IsDefault: true, Status: "active"})
	})
	client := &stubClient{streams: []model.Stream{newStubStream(
		[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "hello"}}},
		model.Message{Role: model.RoleAssistant, Content: "hello"},
		nil,
	)}}
	executor := newTaskExecutorForTest(t, ExecutorDependencies{
		Resolver:          newExecutorResolverForTest(),
		ConversationStore: store,
		PromptResolver:    promptResolver,
		WorkspaceRoot:     workspaceRoot,
		ClientFactory:     func(*coretypes.LLMProvider, *coretypes.LLMModel) (model.LlmClient, error) { return client, nil },
	})

	payload := marshalExecutorTaskInput(t, map[string]any{
		"provider_id": "openai",
		"model_id":    "gpt-5.4",
		"message":     "hi",
		"scene":       "agent.run.review",
	})
	task := &coretasks.Task{ID: "task_prompt_explicit_scene", TaskType: "agent.run", InputJSON: payload}

	_, err := executor(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("executor() error = %v", err)
	}
	assertExecutorRequestSystemPrompt(t, client, "Review scene prompt")
}

func TestAgentExecutorPromptRoutesLegacySystemPromptThroughResolver(t *testing.T) {
	store := newConversationStoreForTest(t)
	workspaceRoot := t.TempDir()
	writeExecutorWorkspacePrompt(t, workspaceRoot, "Workspace prompt")
	promptResolver := newExecutorPromptResolverForTest(t, func(store *coreprompt.Store) {
		mustCreateExecutorPromptDocument(t, store, coreprompt.CreateDocumentInput{ID: "doc-default", Name: "Default", Content: "DB prompt", Scope: "admin", Status: "active"})
		mustCreateExecutorPromptBinding(t, store, coreprompt.CreateBindingInput{PromptID: "doc-default", Scene: "agent.run.default", Phase: "session", IsDefault: true, Status: "active"})
	})
	client := &stubClient{streams: []model.Stream{newStubStream(
		[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "hello"}}},
		model.Message{Role: model.RoleAssistant, Content: "hello"},
		nil,
	)}}
	executor := newTaskExecutorForTest(t, ExecutorDependencies{
		Resolver:          newExecutorResolverForTest(),
		ConversationStore: store,
		PromptResolver:    promptResolver,
		WorkspaceRoot:     workspaceRoot,
		ClientFactory:     func(*coretypes.LLMProvider, *coretypes.LLMModel) (model.LlmClient, error) { return client, nil },
	})

	payload := marshalExecutorTaskInput(t, map[string]any{
		"provider_id":   "openai",
		"model_id":      "gpt-5.4",
		"message":       "hi",
		"system_prompt": "Legacy prompt",
	})
	task := &coretasks.Task{ID: "task_prompt_legacy_system_prompt", TaskType: "agent.run", InputJSON: payload}

	_, err := executor(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("executor() error = %v", err)
	}
	assertExecutorRequestMessages(t, client, []model.Message{
		{Role: model.RoleSystem, Content: "DB prompt"},
		{Role: model.RoleSystem, Content: "Legacy prompt"},
		{Role: model.RoleSystem, Content: "The following AGENTS.md file was injected from the user's working directory. Treat it as guidance and operating rules for the current workspace.\n---\nWorkspace prompt"},
		{Role: model.RoleUser, Content: "hi"},
	})
}

func TestAgentExecutorPromptUsesCanonicalResolvedProviderAndModelForPromptSelection(t *testing.T) {
	store := newConversationStoreForTest(t)
	workspaceRoot := t.TempDir()
	promptResolver := newExecutorPromptResolverForTest(t, func(store *coreprompt.Store) {
		mustCreateExecutorPromptDocument(t, store, coreprompt.CreateDocumentInput{ID: "doc-canonical", Name: "Canonical", Content: "Canonical prompt", Scope: "admin", Status: "active"})
		mustCreateExecutorPromptBinding(t, store, coreprompt.CreateBindingInput{
			PromptID:   "doc-canonical",
			Scene:      "agent.run.default",
			Phase:      "session",
			IsDefault:  true,
			ProviderID: "openai",
			ModelID:    "gpt-5.4",
			Status:     "active",
		})
	})
	client := &stubClient{streams: []model.Stream{newStubStream(
		[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "hello"}}},
		model.Message{Role: model.RoleAssistant, Content: "hello"},
		nil,
	)}}
	executor := newTaskExecutorForTest(t, ExecutorDependencies{
		Resolver: &ModelResolver{Providers: []coretypes.LLMProvider{{
			BaseProvider: coretypes.BaseProvider{Name: "openai"},
			Models: []coretypes.LLMModel{{
				BaseModel: coretypes.BaseModel{ID: "gpt-5.4", Name: "GPT 5.4"},
				Type:      coretypes.LLMTypeOpenAIResponses,
			}},
		}}},
		ConversationStore: store,
		PromptResolver:    promptResolver,
		WorkspaceRoot:     workspaceRoot,
		ClientFactory:     func(*coretypes.LLMProvider, *coretypes.LLMModel) (model.LlmClient, error) { return client, nil },
	})

	payload := marshalExecutorTaskInput(t, map[string]any{
		"provider_id": " OPENAI ",
		"model_id":    " GPT 5.4 ",
		"message":     "hi",
	})
	task := &coretasks.Task{ID: "task_prompt_canonical_provider_model", TaskType: "agent.run", InputJSON: payload}

	_, err := executor(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("executor() error = %v", err)
	}
	assertExecutorRequestSystemPrompt(t, client, "Canonical prompt")
}

func TestTemporaryLegacySystemPromptBridgeUsesResolvedSessionOnly(t *testing.T) {
	resolved := &coreprompt.ResolvedPrompt{
		Scene: "agent.run.default",
		Session: []model.Message{
			{Role: model.RoleSystem, Content: "Session one"},
			{Role: model.RoleSystem, Content: "Session two"},
		},
		StepPreModel: []model.Message{{Role: model.RoleSystem, Content: "Step only prompt"}},
		ToolResult:   []model.Message{{Role: model.RoleSystem, Content: "Tool only prompt"}},
		Segments: []coreprompt.ResolvedPromptSegment{
			{Phase: "step_pre_model", Content: "Step only prompt"},
			{Phase: "tool_result", Content: "Tool only prompt"},
		},
	}

	got := bridgeLegacySystemPromptFromResolvedPromptSession(resolved)
	if got != "Session one\n\nSession two" {
		t.Fatalf("bridgeLegacySystemPromptFromResolvedPromptSession() = %q, want %q", got, "Session one\n\nSession two")
	}
}

func TestAgentExecutorPromptRequiresPromptResolver(t *testing.T) {
	store := newConversationStoreForTest(t)
	workspaceRoot := t.TempDir()
	clientFactoryCalls := 0
	executor := NewTaskExecutor(ExecutorDependencies{
		Resolver:          newExecutorResolverForTest(),
		ConversationStore: store,
		WorkspaceRoot:     workspaceRoot,
		ClientFactory: func(*coretypes.LLMProvider, *coretypes.LLMModel) (model.LlmClient, error) {
			clientFactoryCalls++
			return &stubClient{streams: []model.Stream{newStubStream(
				[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "hello"}}},
				model.Message{Role: model.RoleAssistant, Content: "hello"},
				nil,
			)}}, nil
		},
	})

	payload := marshalExecutorTaskInput(t, map[string]any{
		"provider_id": "openai",
		"model_id":    "gpt-5.4",
		"message":     "hi",
	})
	task := &coretasks.Task{ID: "task_prompt_missing_resolver", TaskType: "agent.run", InputJSON: payload}

	_, err := executor(context.Background(), task, nil)
	if err == nil || !strings.Contains(err.Error(), "prompt resolver is required") {
		t.Fatalf("executor() error = %v, want missing prompt resolver error", err)
	}
	if clientFactoryCalls != 0 {
		t.Fatalf("clientFactoryCalls = %d, want 0", clientFactoryCalls)
	}
}

func TestAgentExecutorPromptResolutionFailureAbortsBeforeModelCall(t *testing.T) {
	store := newConversationStoreForTest(t)
	workspaceRoot := t.TempDir()
	clientFactoryCalls := 0
	executor := newTaskExecutorForTest(t, ExecutorDependencies{
		Resolver:          newExecutorResolverForTest(),
		ConversationStore: store,
		PromptResolver:    coreprompt.NewResolver(nil),
		WorkspaceRoot:     workspaceRoot,
		ClientFactory: func(*coretypes.LLMProvider, *coretypes.LLMModel) (model.LlmClient, error) {
			clientFactoryCalls++
			return &stubClient{streams: []model.Stream{newStubStream(
				[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "hello"}}},
				model.Message{Role: model.RoleAssistant, Content: "hello"},
				nil,
			)}}, nil
		},
	})

	payload := marshalExecutorTaskInput(t, map[string]any{
		"provider_id": "openai",
		"model_id":    "gpt-5.4",
		"message":     "hi",
	})
	task := &coretasks.Task{ID: "task_prompt_resolution_failure", TaskType: "agent.run", InputJSON: payload}

	_, err := executor(context.Background(), task, nil)
	if !errors.Is(err, coreprompt.ErrResolverStoreRequired) {
		t.Fatalf("executor() error = %v, want ErrResolverStoreRequired", err)
	}
	if clientFactoryCalls != 0 {
		t.Fatalf("clientFactoryCalls = %d, want 0", clientFactoryCalls)
	}
}

func TestAgentExecutorPersistsFinalAssistantUsageInConversationHistory(t *testing.T) {
	store := newConversationStoreForTest(t)
	resolver := &ModelResolver{Providers: []coretypes.LLMProvider{{
		BaseProvider: coretypes.BaseProvider{Name: "openai"},
		Models: []coretypes.LLMModel{{
			BaseModel: coretypes.BaseModel{ID: "gpt-5.4", Name: "GPT 5.4"},
			Type:      coretypes.LLMTypeOpenAIResponses,
		}},
	}}}
	executor := newTaskExecutorForTest(t, ExecutorDependencies{
		Resolver:          resolver,
		ConversationStore: store,
		ClientFactory: func(*coretypes.LLMProvider, *coretypes.LLMModel) (model.LlmClient, error) {
			return &stubClient{streams: []model.Stream{newStubStream(
				[]model.StreamEvent{
					{Type: model.StreamEventUsage, Usage: model.TokenUsage{PromptTokens: 21, CompletionTokens: 13, TotalTokens: 34}},
					{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "hello"}},
				},
				model.Message{Role: model.RoleAssistant, Content: "hello"},
				nil,
			)}}, nil
		},
	})

	payload, _ := json.Marshal(RunTaskInput{ProviderID: "openai", ModelID: "gpt-5.4", Message: "hi", CreatedBy: "tester"})
	task := &coretasks.Task{ID: "task_1", TaskType: "agent.run", InputJSON: payload}
	result, err := executor(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("executor() error = %v", err)
	}
	runResult := result.(RunTaskResult)

	var stored []ConversationMessage
	if err := store.db.Where("conversation_id = ?", runResult.ConversationID).Order("seq asc").Find(&stored).Error; err != nil {
		t.Fatalf("query stored messages error = %v", err)
	}
	if len(stored) != 2 {
		t.Fatalf("len(stored) = %d, want 2", len(stored))
	}
	if string(stored[1].MessageJSON) == "" || !strings.Contains(string(stored[1].MessageJSON), `"Usage":{"PromptTokens":21,"CachedPromptTokens":0,"CompletionTokens":13,"TotalTokens":34}`) {
		t.Fatalf("assistant message json = %s, want persisted usage payload", string(stored[1].MessageJSON))
	}
	if string(stored[1].MessageJSON) != "" && strings.Contains(string(stored[0].MessageJSON), `"Usage":`) {
		t.Fatalf("user message json = %s, want no usage on user turn", string(stored[0].MessageJSON))
	}
}

func TestAgentExecutorAllowsConversationModelSwitchAndUsesSelectedModelMemory(t *testing.T) {
	store := newConversationStoreForTest(t)
	_, err := store.CreateConversation(context.Background(), CreateConversationInput{ID: "conv_1", ProviderID: "openai", ModelID: "gpt-5.4"})
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	if err := store.AppendMessages(context.Background(), "conv_1", "task_0", []model.Message{{Role: model.RoleUser, Content: "first"}, {Role: model.RoleAssistant, Content: "answer", ProviderID: "openai", ModelID: "gpt-5.4"}}); err != nil {
		t.Fatalf("AppendMessages() error = %v", err)
	}

	client := &stubClient{streams: []model.Stream{newStubStream(
		[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "switched"}}},
		model.Message{Role: model.RoleAssistant, Content: "switched"},
		nil,
	)}}
	resolver := &ModelResolver{Providers: []coretypes.LLMProvider{{
		BaseProvider: coretypes.BaseProvider{Name: "openai"},
		Models: []coretypes.LLMModel{
			{BaseModel: coretypes.BaseModel{ID: "gpt-5.4", Name: "GPT 5.4"}, Type: coretypes.LLMTypeOpenAIResponses, Context: coretypes.LLMContextConfig{Max: 128000, Output: 8000}},
			{BaseModel: coretypes.BaseModel{ID: "gpt-4.1-mini", Name: "GPT 4.1 Mini"}, Type: coretypes.LLMTypeOpenAIResponses, Context: coretypes.LLMContextConfig{Max: 64000, Output: 2000}},
		},
	}}}
	memoryModels := make([]string, 0, 1)
	executor := newTaskExecutorForTest(t, ExecutorDependencies{
		Resolver:          resolver,
		ConversationStore: store,
		ClientFactory:     func(*coretypes.LLMProvider, *coretypes.LLMModel) (model.LlmClient, error) { return client, nil },
		MemoryFactory: func(llmModel *coretypes.LLMModel) (*memory.Manager, error) {
			memoryModels = append(memoryModels, llmModel.ModelID())
			return memory.NewManager(memory.Options{Model: llmModel, Counter: fakeTokenCounter{}})
		},
	})

	payload, _ := json.Marshal(RunTaskInput{ConversationID: "conv_1", ProviderID: "openai", ModelID: "gpt-4.1-mini", Message: "second"})
	task := &coretasks.Task{ID: "task_1", TaskType: "agent.run", InputJSON: payload}
	result, err := executor(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("executor() error = %v", err)
	}
	runResult := result.(RunTaskResult)
	if runResult.ModelID != "gpt-4.1-mini" {
		t.Fatalf("runResult.ModelID = %q, want gpt-4.1-mini", runResult.ModelID)
	}
	if len(memoryModels) != 1 || memoryModels[0] != "gpt-4.1-mini" {
		t.Fatalf("memoryModels = %#v, want gpt-4.1-mini", memoryModels)
	}
	if len(client.streamRequests) != 1 || client.streamRequests[0].MaxTokens != 2000 {
		t.Fatalf("stream request = %#v, want MaxTokens=2000 from switched model", client.streamRequests)
	}
	conversation, err := store.GetConversation(context.Background(), "conv_1")
	if err != nil {
		t.Fatalf("GetConversation() error = %v", err)
	}
	if conversation.ModelID != "gpt-4.1-mini" {
		t.Fatalf("conversation.ModelID = %q, want gpt-4.1-mini", conversation.ModelID)
	}
	messages, err := store.ListMessages(context.Background(), "conv_1")
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	last := messages[len(messages)-1]
	if last.ProviderID != "openai" || last.ModelID != "gpt-4.1-mini" {
		t.Fatalf("last message = %#v, want persisted switched provider/model", last)
	}
}

func TestAgentExecutorUsesTaskRuntimeSink(t *testing.T) {
	store := newConversationStoreForTest(t)
	resolver := &ModelResolver{Providers: []coretypes.LLMProvider{{
		BaseProvider: coretypes.BaseProvider{Name: "openai"},
		Models:       []coretypes.LLMModel{{BaseModel: coretypes.BaseModel{ID: "gpt-5.4", Name: "GPT 5.4"}, Type: coretypes.LLMTypeOpenAIResponses}},
	}}}
	recorder := &recordingTaskRuntime{}
	executor := newTaskExecutorForTest(t, ExecutorDependencies{
		Resolver:          resolver,
		ConversationStore: store,
		ClientFactory: func(*coretypes.LLMProvider, *coretypes.LLMModel) (model.LlmClient, error) {
			return &stubClient{streams: []model.Stream{newStubStream(
				[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "hello"}}},
				model.Message{Role: model.RoleAssistant, Content: "hello"},
				nil,
			)}}, nil
		},
		NewEventSink: func(*coretasks.Runtime) EventSink { return &taskRuntimeSink{runtime: recorder} },
	})
	payload, _ := json.Marshal(RunTaskInput{ProviderID: "openai", ModelID: "gpt-5.4", Message: "hi"})
	task := &coretasks.Task{ID: "task_1", TaskType: "agent.run", InputJSON: payload}
	_, err := executor(context.Background(), task, &coretasks.Runtime{})
	if err != nil {
		t.Fatalf("executor() error = %v", err)
	}
	if len(recorder.started) == 0 {
		t.Fatal("started events = empty, want task runtime sink activity")
	}
}

func TestAgentExecutorPersistsPartialMessagesWhenLaterStepFails(t *testing.T) {
	store := newConversationStoreForTest(t)
	resolver := &ModelResolver{Providers: []coretypes.LLMProvider{{
		BaseProvider: coretypes.BaseProvider{Name: "openai"},
		Models:       []coretypes.LLMModel{{BaseModel: coretypes.BaseModel{ID: "gpt-5.4", Name: "GPT 5.4"}, Type: coretypes.LLMTypeOpenAIResponses}},
	}}}
	client := &stubClient{streams: []model.Stream{
		newStubStream(
			[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: `{"city":"Shanghai"}`}}}}},
			model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: `{"city":"Shanghai"}`}}},
			nil,
		),
	}, streamErrs: []error{nil, context.DeadlineExceeded}}
	registry := newTestRegistry(t, map[string]func(context.Context, map[string]interface{}) (string, error){
		"lookup_weather": func(context.Context, map[string]interface{}) (string, error) {
			return "sunny", nil
		},
	})
	executor := newTaskExecutorForTest(t, ExecutorDependencies{
		Resolver:          resolver,
		ConversationStore: store,
		Registry:          registry,
		ClientFactory:     func(*coretypes.LLMProvider, *coretypes.LLMModel) (model.LlmClient, error) { return client, nil },
	})
	payload, _ := json.Marshal(RunTaskInput{ConversationID: "conv_1", ProviderID: "openai", ModelID: "gpt-5.4", Message: "weather?"})
	task := &coretasks.Task{ID: "task_1", TaskType: "agent.run", InputJSON: payload}

	_, err := executor(context.Background(), task, nil)
	if err == nil {
		t.Fatal("executor() error = nil, want step-two failure")
	}

	got, listErr := store.ListMessages(context.Background(), "conv_1")
	if listErr != nil {
		t.Fatalf("ListMessages() error = %v", listErr)
	}
	if len(got) != 4 {
		t.Fatalf("len(messages) = %d, want 4", len(got))
	}
	if got[0].Role != model.RoleUser || got[1].Role != model.RoleAssistant || got[2].Role != model.RoleTool || got[3].Role != model.RoleSystem {
		t.Fatalf("messages = %#v, want persisted user/assistant/tool/error partial turn", got)
	}
	if got[3].Content == "" {
		t.Fatalf("failure message = %#v, want non-empty error content", got[3])
	}
}

func TestAgentExecutorRecordsConversationAuditEvents(t *testing.T) {
	store := newConversationStoreForTest(t)
	_, err := store.CreateConversation(context.Background(), CreateConversationInput{ID: "conv_1", ProviderID: "openai", ModelID: "gpt-5.4"})
	if err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	if err := store.AppendMessages(context.Background(), "conv_1", "task_0", []model.Message{{Role: model.RoleUser, Content: "first"}, {Role: model.RoleAssistant, Content: "answer"}}); err != nil {
		t.Fatalf("AppendMessages() error = %v", err)
	}

	recorder := newRecordingExecutorAuditRecorder()
	client := &verifyingStreamClient{
		base: &stubClient{streams: []model.Stream{newStubStream(
			[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "second answer"}}},
			model.Message{Role: model.RoleAssistant, Content: "second answer"},
			nil,
		)}},
		beforeStream: func(req model.ChatRequest) {
			runID := recorder.requireRunIDForTask(t, "task_1")
			artifact := recorder.requireArtifactByKind(t, runID, coreaudit.ArtifactKindRequestMessages)
			snapshot := decodeRequestMessagesArtifact(t, artifact)
			if snapshot.ConversationID != "conv_1" {
				t.Fatalf("request artifact conversation id = %q, want conv_1", snapshot.ConversationID)
			}
			if !reflect.DeepEqual(snapshot.Messages, req.Messages) {
				t.Fatalf("request artifact messages = %#v, want %#v", snapshot.Messages, req.Messages)
			}
		},
	}
	executor := newTaskExecutorForTest(t, ExecutorDependencies{
		Resolver:          newExecutorResolverForTest(),
		ConversationStore: store,
		ClientFactory:     func(*coretypes.LLMProvider, *coretypes.LLMModel) (model.LlmClient, error) { return client, nil },
		AuditRecorder:     recorder,
	})
	payload, _ := json.Marshal(RunTaskInput{ConversationID: "conv_1", ProviderID: "openai", ModelID: "gpt-5.4", Message: "second", CreatedBy: "tester"})
	task := &coretasks.Task{ID: "task_1", TaskType: "agent.run", CreatedBy: "tester", InputJSON: payload}

	_, err = executor(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("executor() error = %v", err)
	}

	runID := recorder.requireRunIDForTask(t, task.ID)
	assertExecutorAuditEventTypes(t, recorder, runID,
		"conversation.loaded",
		"user_message.appended",
		"step.started",
		"prompt.resolved",
		"request.built",
		"model.completed",
		"step.finished",
		"messages.persisted",
	)

	loadedEvent := recorder.requireEvent(t, runID, "conversation.loaded")
	loadedPayload := decodeAuditPayload(t, loadedEvent)
	if got := int(loadedPayload["message_count"].(float64)); got != 2 {
		t.Fatalf("conversation.loaded message_count = %d, want 2", got)
	}
	if _, ok := loadedPayload["messages"]; ok {
		t.Fatalf("conversation.loaded payload = %#v, want compact payload without full messages", loadedPayload)
	}

	userEvent := recorder.requireEvent(t, runID, "user_message.appended")
	if userEvent.RefArtifactID == "" {
		t.Fatal("user_message.appended ref artifact id = empty, want request_messages artifact reference")
	}
	userPayload := decodeAuditPayload(t, userEvent)
	if got := int(userPayload["request_message_count"].(float64)); got != 3 {
		t.Fatalf("user_message.appended request_message_count = %d, want 3", got)
	}
	if _, ok := userPayload["messages"]; ok {
		t.Fatalf("user_message.appended payload = %#v, want compact payload without full messages", userPayload)
	}

	requestArtifact := recorder.requireArtifactByKind(t, runID, coreaudit.ArtifactKindRequestMessages)
	if requestArtifact.ID != userEvent.RefArtifactID {
		t.Fatalf("request artifact id = %q, want user event ref %q", requestArtifact.ID, userEvent.RefArtifactID)
	}
	requestSnapshot := decodeRequestMessagesArtifact(t, requestArtifact)
	if len(requestSnapshot.Messages) != 3 {
		t.Fatalf("request artifact message count = %d, want 3", len(requestSnapshot.Messages))
	}
	if requestSnapshot.Messages[2].Role != model.RoleUser || requestSnapshot.Messages[2].Content != "second" {
		t.Fatalf("request artifact last message = %#v, want appended user message", requestSnapshot.Messages[2])
	}
}

func TestAgentExecutorWiresRunnerAuditEvidence(t *testing.T) {
	store := newConversationStoreForTest(t)
	recorder := newRecordingExecutorAuditRecorder()
	client := &stubClient{streams: []model.Stream{newStubStream(
		[]model.StreamEvent{
			{Type: model.StreamEventUsage, Usage: model.TokenUsage{PromptTokens: 11, CompletionTokens: 7, TotalTokens: 18}},
			{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "hello from runner"}},
		},
		model.Message{Role: model.RoleAssistant, Content: "hello from runner"},
		nil,
	)}}
	executor := newTaskExecutorForTest(t, ExecutorDependencies{
		Resolver:          newExecutorResolverForTest(),
		ConversationStore: store,
		ClientFactory:     func(*coretypes.LLMProvider, *coretypes.LLMModel) (model.LlmClient, error) { return client, nil },
		AuditRecorder:     recorder,
	})
	payload, _ := json.Marshal(RunTaskInput{ConversationID: "conv_1", ProviderID: "openai", ModelID: "gpt-5.4", Message: "hi", CreatedBy: "tester"})
	task := &coretasks.Task{ID: "task_1", TaskType: "agent.run", CreatedBy: "tester", InputJSON: payload}

	_, err := executor(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("executor() error = %v", err)
	}

	runID := recorder.requireRunIDForTask(t, task.ID)
	assertExecutorAuditEventTypes(t, recorder, runID,
		"conversation.loaded",
		"user_message.appended",
		"step.started",
		"prompt.resolved",
		"request.built",
		"model.completed",
		"step.finished",
		"messages.persisted",
	)

	requestEvent := recorder.requireEvent(t, runID, "request.built")
	if requestEvent.RefArtifactID == "" {
		t.Fatal("request.built ref artifact id = empty, want model_request artifact reference")
	}
	requestArtifact := recorder.requireArtifactByKind(t, runID, coreaudit.ArtifactKindModelRequest)
	if requestArtifact.ID != requestEvent.RefArtifactID {
		t.Fatalf("model request artifact id = %q, want request event ref %q", requestArtifact.ID, requestEvent.RefArtifactID)
	}

	modelEvent := recorder.requireEvent(t, runID, "model.completed")
	modelPayload := decodeAuditPayload(t, modelEvent)
	if got := int(modelPayload["usage_total_tokens"].(float64)); got != 18 {
		t.Fatalf("model.completed usage_total_tokens = %d, want 18", got)
	}
	responseArtifact := recorder.requireArtifactByKind(t, runID, coreaudit.ArtifactKindModelResponse)
	response := decodeModelResponseArtifact(t, responseArtifact)
	if response.Message.Content != "hello from runner" {
		t.Fatalf("model response artifact message = %#v, want final assistant reply", response.Message)
	}
}

func TestAgentExecutorPassesTaskIDToToolRuntimeContext(t *testing.T) {
	store := newConversationStoreForTest(t)
	var gotTaskID string
	registry := newTestRegistry(t, map[string]func(context.Context, map[string]interface{}) (string, error){
		"capture_runtime": func(ctx context.Context, arguments map[string]interface{}) (string, error) {
			runtime, ok := coretools.RuntimeFromContext(ctx)
			if !ok {
				t.Fatal("RuntimeFromContext() ok = false, want true")
			}
			gotTaskID = runtime.TaskID
			return "ok", nil
		},
	})
	client := &stubClient{streams: []model.Stream{
		newStubStream(
			[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "capture_runtime", Arguments: `{}`}}}}},
			model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "capture_runtime", Arguments: `{}`}}},
			nil,
		),
		newStubStream(
			[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "done"}}},
			model.Message{Role: model.RoleAssistant, Content: "done"},
			nil,
		),
	}}
	executor := newTaskExecutorForTest(t, ExecutorDependencies{
		Resolver:          newExecutorResolverForTest(),
		ConversationStore: store,
		Registry:          registry,
		ClientFactory:     func(*coretypes.LLMProvider, *coretypes.LLMModel) (model.LlmClient, error) { return client, nil },
	})
	payload, _ := json.Marshal(RunTaskInput{ConversationID: "conv_1", ProviderID: "openai", ModelID: "gpt-5.4", Message: "hi"})
	task := &coretasks.Task{ID: "task_1", TaskType: "agent.run", InputJSON: payload}

	_, err := executor(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("executor() error = %v", err)
	}
	if gotTaskID != task.ID {
		t.Fatalf("tool runtime TaskID = %q, want %q", gotTaskID, task.ID)
	}
}

func TestAgentExecutorAttachesErrorSnapshotArtifactOnFailure(t *testing.T) {
	store := newConversationStoreForTest(t)
	recorder := newRecordingExecutorAuditRecorder()
	client := &stubClient{streams: []model.Stream{
		newStubStream(
			[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: `{"city":"Shanghai"}`}}}}},
			model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: `{"city":"Shanghai"}`}}},
			nil,
		),
	}, streamErrs: []error{nil, context.DeadlineExceeded}}
	registry := newTestRegistry(t, map[string]func(context.Context, map[string]interface{}) (string, error){
		"lookup_weather": func(context.Context, map[string]interface{}) (string, error) {
			return "sunny", nil
		},
	})
	executor := newTaskExecutorForTest(t, ExecutorDependencies{
		Resolver:          newExecutorResolverForTest(),
		ConversationStore: store,
		Registry:          registry,
		ClientFactory:     func(*coretypes.LLMProvider, *coretypes.LLMModel) (model.LlmClient, error) { return client, nil },
		AuditRecorder:     recorder,
	})
	payload, _ := json.Marshal(RunTaskInput{ConversationID: "conv_1", ProviderID: "openai", ModelID: "gpt-5.4", Message: "weather?", CreatedBy: "tester"})
	task := &coretasks.Task{ID: "task_1", TaskType: "agent.run", CreatedBy: "tester", InputJSON: payload}

	_, err := executor(context.Background(), task, nil)
	if err == nil {
		t.Fatal("executor() error = nil, want deadline exceeded")
	}

	runID := recorder.requireRunIDForTask(t, task.ID)
	errorArtifact := recorder.requireArtifactByKind(t, runID, coreaudit.ArtifactKindErrorSnapshot)
	snapshot := decodeErrorSnapshotArtifact(t, errorArtifact)
	if snapshot.TaskID != task.ID {
		t.Fatalf("error snapshot task_id = %q, want %q", snapshot.TaskID, task.ID)
	}
	if snapshot.ConversationID != "conv_1" {
		t.Fatalf("error snapshot conversation_id = %q, want conv_1", snapshot.ConversationID)
	}
	if !strings.Contains(snapshot.Error, context.DeadlineExceeded.Error()) {
		t.Fatalf("error snapshot error = %q, want substring %q", snapshot.Error, context.DeadlineExceeded.Error())
	}
	if len(snapshot.PartialMessages) != 2 {
		t.Fatalf("error snapshot partial messages = %d, want 2", len(snapshot.PartialMessages))
	}
	if snapshot.PartialMessages[0].Role != model.RoleAssistant || snapshot.PartialMessages[1].Role != model.RoleTool {
		t.Fatalf("error snapshot partial messages = %#v, want assistant/tool messages", snapshot.PartialMessages)
	}
}

func TestAgentExecutorAttachesErrorSnapshotArtifactWhenPersistingAssistantMessagesFails(t *testing.T) {
	store := newConversationStoreForTest(t)
	recorder := newRecordingExecutorAuditRecorder()
	persistErr := errors.New("persist assistant messages failed")
	callbackName := "test:executor:assistant_persist_failure"
	conversationMessageCreates := 0
	if err := store.db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement == nil || tx.Statement.Schema == nil || tx.Statement.Schema.Table != "conversation_messages" {
			return
		}
		conversationMessageCreates++
		if conversationMessageCreates == 2 {
			tx.AddError(persistErr)
		}
	}); err != nil {
		t.Fatalf("register callback error = %v", err)
	}
	defer func() {
		if err := store.db.Callback().Create().Remove(callbackName); err != nil {
			t.Fatalf("remove callback error = %v", err)
		}
	}()

	executor := newTaskExecutorForTest(t, ExecutorDependencies{
		Resolver:          newExecutorResolverForTest(),
		ConversationStore: store,
		ClientFactory: func(*coretypes.LLMProvider, *coretypes.LLMModel) (model.LlmClient, error) {
			return &stubClient{streams: []model.Stream{newStubStream(
				[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "hello"}}},
				model.Message{Role: model.RoleAssistant, Content: "hello"},
				nil,
			)}}, nil
		},
		AuditRecorder: recorder,
	})
	payload, _ := json.Marshal(RunTaskInput{ConversationID: "conv_1", ProviderID: "openai", ModelID: "gpt-5.4", Message: "hi", CreatedBy: "tester"})
	task := &coretasks.Task{ID: "task_1", TaskType: "agent.run", CreatedBy: "tester", InputJSON: payload}

	_, err := executor(context.Background(), task, nil)
	if err == nil || !strings.Contains(err.Error(), persistErr.Error()) {
		t.Fatalf("executor() error = %v, want persist failure", err)
	}
	if conversationMessageCreates < 2 {
		t.Fatalf("conversation message create count = %d, want injected assistant persist failure", conversationMessageCreates)
	}

	runID := recorder.requireRunIDForTask(t, task.ID)
	errorArtifact := recorder.requireArtifactByKind(t, runID, coreaudit.ArtifactKindErrorSnapshot)
	snapshot := decodeErrorSnapshotArtifact(t, errorArtifact)
	if !strings.Contains(snapshot.Error, persistErr.Error()) {
		t.Fatalf("error snapshot error = %q, want substring %q", snapshot.Error, persistErr.Error())
	}
	if len(snapshot.PartialMessages) != 1 {
		t.Fatalf("error snapshot partial messages = %d, want 1", len(snapshot.PartialMessages))
	}
	if snapshot.PartialMessages[0].Role != model.RoleAssistant || snapshot.PartialMessages[0].Content != "hello" {
		t.Fatalf("error snapshot partial messages = %#v, want assistant reply", snapshot.PartialMessages)
	}
}

type verifyingStreamClient struct {
	base         *stubClient
	beforeStream func(model.ChatRequest)
}

func (c *verifyingStreamClient) Chat(ctx context.Context, req model.ChatRequest) (model.ChatResponse, error) {
	return c.base.Chat(ctx, req)
}

func (c *verifyingStreamClient) ChatStream(ctx context.Context, req model.ChatRequest) (model.Stream, error) {
	if c.beforeStream != nil {
		c.beforeStream(req)
	}
	return c.base.ChatStream(ctx, req)
}

type recordingExecutorAuditRecorder struct {
	mu               sync.Mutex
	runsByTaskID     map[string]*coreaudit.Run
	eventsByRunID    map[string][]*coreaudit.Event
	artifactsByRunID map[string][]*coreaudit.Artifact
}

func newRecordingExecutorAuditRecorder() *recordingExecutorAuditRecorder {
	return &recordingExecutorAuditRecorder{
		runsByTaskID:     make(map[string]*coreaudit.Run),
		eventsByRunID:    make(map[string][]*coreaudit.Event),
		artifactsByRunID: make(map[string][]*coreaudit.Artifact),
	}
}

func (r *recordingExecutorAuditRecorder) StartRun(_ context.Context, input coreaudit.StartRunInput) (*coreaudit.Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	run := r.runsByTaskID[input.TaskID]
	if run == nil {
		run = &coreaudit.Run{ID: "run_for_" + input.TaskID, TaskID: input.TaskID, TaskType: input.TaskType}
		r.runsByTaskID[input.TaskID] = run
	}
	if input.ConversationID != "" {
		run.ConversationID = input.ConversationID
	}
	if input.ProviderID != "" {
		run.ProviderID = input.ProviderID
	}
	if input.ModelID != "" {
		run.ModelID = input.ModelID
	}
	if input.CreatedBy != "" {
		run.CreatedBy = input.CreatedBy
	}
	if input.Status != "" {
		run.Status = input.Status
	}
	return cloneAuditRun(run), nil
}

func (r *recordingExecutorAuditRecorder) AppendEvent(_ context.Context, runID string, input coreaudit.AppendEventInput) (*coreaudit.Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	payloadJSON, err := json.Marshal(input.Payload)
	if err != nil {
		return nil, err
	}
	event := &coreaudit.Event{
		RunID:         runID,
		Seq:           int64(len(r.eventsByRunID[runID]) + 1),
		Phase:         input.Phase,
		EventType:     input.EventType,
		Level:         input.Level,
		StepIndex:     input.StepIndex,
		ParentSeq:     input.ParentSeq,
		RefArtifactID: input.RefArtifactID,
		PayloadJSON:   payloadJSON,
	}
	r.eventsByRunID[runID] = append(r.eventsByRunID[runID], event)
	return cloneAuditEvent(event), nil
}

func (r *recordingExecutorAuditRecorder) AttachArtifact(_ context.Context, runID string, input coreaudit.CreateArtifactInput) (*coreaudit.Artifact, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	bodyJSON, err := json.Marshal(input.Body)
	if err != nil {
		return nil, err
	}
	artifact := &coreaudit.Artifact{
		ID:       firstNonEmpty(input.ArtifactID, fmt.Sprintf("art_%d", len(r.artifactsByRunID[runID])+1)),
		RunID:    runID,
		Kind:     input.Kind,
		MimeType: input.MimeType,
		Encoding: input.Encoding,
		BodyJSON: bodyJSON,
	}
	r.artifactsByRunID[runID] = append(r.artifactsByRunID[runID], artifact)
	return cloneAuditArtifact(artifact), nil
}

func (r *recordingExecutorAuditRecorder) FinishRun(_ context.Context, runID string, input coreaudit.FinishRunInput) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, run := range r.runsByTaskID {
		if run.ID == runID {
			run.Status = input.Status
			break
		}
	}
	return nil
}

func (r *recordingExecutorAuditRecorder) requireRunIDForTask(t *testing.T, taskID string) string {
	t.Helper()

	r.mu.Lock()
	defer r.mu.Unlock()
	run := r.runsByTaskID[taskID]
	if run == nil {
		t.Fatalf("task %q has no audit run", taskID)
	}
	return run.ID
}

func (r *recordingExecutorAuditRecorder) requireEvent(t *testing.T, runID string, eventType string) *coreaudit.Event {
	t.Helper()

	r.mu.Lock()
	defer r.mu.Unlock()
	for _, event := range r.eventsByRunID[runID] {
		if event.EventType == eventType {
			return cloneAuditEvent(event)
		}
	}
	t.Fatalf("run %q did not record audit event %q", runID, eventType)
	return nil
}

func (r *recordingExecutorAuditRecorder) requireArtifactByKind(t *testing.T, runID string, kind coreaudit.ArtifactKind) *coreaudit.Artifact {
	t.Helper()

	r.mu.Lock()
	defer r.mu.Unlock()
	for _, artifact := range r.artifactsByRunID[runID] {
		if artifact.Kind == kind {
			return cloneAuditArtifact(artifact)
		}
	}
	t.Fatalf("run %q did not record artifact kind %q", runID, kind)
	return nil
}

func (r *recordingExecutorAuditRecorder) eventTypes(runID string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	events := r.eventsByRunID[runID]
	result := make([]string, 0, len(events))
	for _, event := range events {
		result = append(result, event.EventType)
	}
	return result
}

func newExecutorResolverForTest() *ModelResolver {
	return &ModelResolver{Providers: []coretypes.LLMProvider{{
		BaseProvider: coretypes.BaseProvider{Name: "openai"},
		Models:       []coretypes.LLMModel{{BaseModel: coretypes.BaseModel{ID: "gpt-5.4", Name: "GPT 5.4"}, Type: coretypes.LLMTypeOpenAIResponses}},
	}}}
}

func assertExecutorAuditEventTypes(t *testing.T, recorder *recordingExecutorAuditRecorder, runID string, want ...string) {
	t.Helper()

	got := recorder.eventTypes(runID)
	if len(got) != len(want) {
		t.Fatalf("audit event count = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("audit events = %v, want %v", got, want)
		}
	}
}

func decodeAuditPayload(t *testing.T, event *coreaudit.Event) map[string]any {
	t.Helper()

	var payload map[string]any
	if err := json.Unmarshal(event.PayloadJSON, &payload); err != nil {
		t.Fatalf("decode audit payload error = %v", err)
	}
	return payload
}

func decodeRequestMessagesArtifact(t *testing.T, artifact *coreaudit.Artifact) requestMessagesArtifact {
	t.Helper()

	var snapshot requestMessagesArtifact
	if err := json.Unmarshal(artifact.BodyJSON, &snapshot); err != nil {
		t.Fatalf("decode request_messages artifact error = %v", err)
	}
	return snapshot
}

func decodeErrorSnapshotArtifact(t *testing.T, artifact *coreaudit.Artifact) errorSnapshotArtifact {
	t.Helper()

	var snapshot errorSnapshotArtifact
	if err := json.Unmarshal(artifact.BodyJSON, &snapshot); err != nil {
		t.Fatalf("decode error_snapshot artifact error = %v", err)
	}
	return snapshot
}

func cloneAuditRun(run *coreaudit.Run) *coreaudit.Run {
	if run == nil {
		return nil
	}
	copy := *run
	return &copy
}

func cloneAuditEvent(event *coreaudit.Event) *coreaudit.Event {
	if event == nil {
		return nil
	}
	copy := *event
	if event.PayloadJSON != nil {
		copy.PayloadJSON = append([]byte(nil), event.PayloadJSON...)
	}
	return &copy
}

func cloneAuditArtifact(artifact *coreaudit.Artifact) *coreaudit.Artifact {
	if artifact == nil {
		return nil
	}
	copy := *artifact
	if artifact.BodyJSON != nil {
		copy.BodyJSON = append([]byte(nil), artifact.BodyJSON...)
	}
	return &copy
}

func newTaskExecutorForTest(t *testing.T, deps ExecutorDependencies) coretasks.Executor {
	t.Helper()

	if deps.PromptResolver == nil {
		deps.PromptResolver = newExecutorPromptResolverForTest(t, nil)
	}
	if strings.TrimSpace(deps.WorkspaceRoot) == "" {
		deps.WorkspaceRoot = t.TempDir()
	}
	return NewTaskExecutor(deps)
}

func marshalExecutorTaskInput(t *testing.T, fields map[string]any) []byte {
	t.Helper()

	payload, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return payload
}

func assertExecutorRequestSystemPrompt(t *testing.T, client *stubClient, want string) {
	t.Helper()

	if len(client.streamRequests) != 1 {
		t.Fatalf("stream request count = %d, want 1", len(client.streamRequests))
	}
	request := client.streamRequests[0]
	if len(request.Messages) < 2 {
		t.Fatalf("request messages = %#v, want system and user messages", request.Messages)
	}
	if request.Messages[0].Role != model.RoleSystem {
		t.Fatalf("request first role = %q, want %q", request.Messages[0].Role, model.RoleSystem)
	}
	if request.Messages[0].Content != want {
		t.Fatalf("request system prompt = %q, want %q", request.Messages[0].Content, want)
	}
}

func assertExecutorRequestMessages(t *testing.T, client *stubClient, want []model.Message) {
	t.Helper()

	if len(client.streamRequests) != 1 {
		t.Fatalf("stream request count = %d, want 1", len(client.streamRequests))
	}
	request := client.streamRequests[0]
	if len(request.Messages) != len(want) {
		t.Fatalf("request messages = %#v, want %#v", request.Messages, want)
	}
	for i := range want {
		if request.Messages[i].Role != want[i].Role || request.Messages[i].Content != want[i].Content {
			t.Fatalf("request.Messages[%d] = %#v, want %#v", i, request.Messages[i], want[i])
		}
	}
}

func newExecutorPromptResolverForTest(t *testing.T, seed func(store *coreprompt.Store)) *coreprompt.Resolver {
	t.Helper()

	store := newExecutorPromptStoreForTest(t)
	if seed != nil {
		seed(store)
	}
	return coreprompt.NewResolver(store)
}

func newExecutorPromptStoreForTest(t *testing.T) *coreprompt.Store {
	t.Helper()

	dsn := fmt.Sprintf("file:%s_prompt?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	store := coreprompt.NewStore(db)
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate() error = %v", err)
	}
	return store
}

func mustCreateExecutorPromptDocument(t *testing.T, store *coreprompt.Store, input coreprompt.CreateDocumentInput) *coreprompt.PromptDocument {
	t.Helper()

	document, err := store.CreateDocument(context.Background(), input)
	if err != nil {
		t.Fatalf("CreateDocument(%+v) error = %v", input, err)
	}
	return document
}

func mustCreateExecutorPromptBinding(t *testing.T, store *coreprompt.Store, input coreprompt.CreateBindingInput) *coreprompt.PromptBinding {
	t.Helper()

	binding, err := store.CreateBinding(context.Background(), input)
	if err != nil {
		t.Fatalf("CreateBinding(%+v) error = %v", input, err)
	}
	return binding
}

func writeExecutorWorkspacePrompt(t *testing.T, workspaceRoot string, content string) {
	t.Helper()

	path := filepath.Join(workspaceRoot, "AGENTS.md")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}
}
