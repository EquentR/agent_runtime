package agent

import (
	"context"
	"encoding/json"
	"testing"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	coretypes "github.com/EquentR/agent_runtime/core/types"
)

func TestResolveConfiguredModelByProviderAndModelID(t *testing.T) {
	resolver := &ModelResolver{Provider: &coretypes.LLMProvider{
		BaseProvider: coretypes.BaseProvider{Name: "openai"},
		Models: []coretypes.LLMModel{{
			BaseModel: coretypes.BaseModel{ID: "gpt-5.4", Name: "GPT 5.4"},
			Type:      coretypes.LLMTypeOpenAIResponses,
		}},
	}}
	model, err := resolver.Resolve("openai", "gpt-5.4")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if model == nil || model.ModelID() != "gpt-5.4" {
		t.Fatalf("model = %#v, want gpt-5.4", model)
	}
}

func TestResolveConfiguredModelRejectsUnknownProvider(t *testing.T) {
	resolver := &ModelResolver{Provider: &coretypes.LLMProvider{BaseProvider: coretypes.BaseProvider{Name: "openai"}}}
	_, err := resolver.Resolve("google", "gpt-5.4")
	if err == nil {
		t.Fatal("Resolve() error = nil, want unknown provider error")
	}
}

func TestResolveConfiguredModelRejectsUnknownModel(t *testing.T) {
	resolver := &ModelResolver{Provider: &coretypes.LLMProvider{BaseProvider: coretypes.BaseProvider{Name: "openai"}}}
	_, err := resolver.Resolve("openai", "missing-model")
	if err == nil {
		t.Fatal("Resolve() error = nil, want unknown model error")
	}
}

func TestAgentExecutorCreatesConversationWhenMissing(t *testing.T) {
	store := newConversationStoreForTest(t)
	resolver := &ModelResolver{Provider: &coretypes.LLMProvider{
		BaseProvider: coretypes.BaseProvider{Name: "openai"},
		Models: []coretypes.LLMModel{{
			BaseModel: coretypes.BaseModel{ID: "gpt-5.4", Name: "GPT 5.4"},
			Type:      coretypes.LLMTypeOpenAIResponses,
		}},
	}}
	executor := NewTaskExecutor(ExecutorDependencies{
		Resolver:          resolver,
		ConversationStore: store,
		ClientFactory: func(*coretypes.LLMModel) (model.LlmClient, error) {
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
	resolver := &ModelResolver{Provider: &coretypes.LLMProvider{
		BaseProvider: coretypes.BaseProvider{Name: "openai"},
		Models:       []coretypes.LLMModel{{BaseModel: coretypes.BaseModel{ID: "gpt-5.4", Name: "GPT 5.4"}, Type: coretypes.LLMTypeOpenAIResponses}},
	}}
	executor := NewTaskExecutor(ExecutorDependencies{
		Resolver:          resolver,
		ConversationStore: store,
		ClientFactory:     func(*coretypes.LLMModel) (model.LlmClient, error) { return client, nil },
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

func TestAgentExecutorUsesTaskRuntimeSink(t *testing.T) {
	store := newConversationStoreForTest(t)
	resolver := &ModelResolver{Provider: &coretypes.LLMProvider{
		BaseProvider: coretypes.BaseProvider{Name: "openai"},
		Models:       []coretypes.LLMModel{{BaseModel: coretypes.BaseModel{ID: "gpt-5.4", Name: "GPT 5.4"}, Type: coretypes.LLMTypeOpenAIResponses}},
	}}
	recorder := &recordingTaskRuntime{}
	executor := NewTaskExecutor(ExecutorDependencies{
		Resolver:          resolver,
		ConversationStore: store,
		ClientFactory: func(*coretypes.LLMModel) (model.LlmClient, error) {
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
	resolver := &ModelResolver{Provider: &coretypes.LLMProvider{
		BaseProvider: coretypes.BaseProvider{Name: "openai"},
		Models:       []coretypes.LLMModel{{BaseModel: coretypes.BaseModel{ID: "gpt-5.4", Name: "GPT 5.4"}, Type: coretypes.LLMTypeOpenAIResponses}},
	}}
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
	executor := NewTaskExecutor(ExecutorDependencies{
		Resolver:          resolver,
		ConversationStore: store,
		Registry:          registry,
		ClientFactory:     func(*coretypes.LLMModel) (model.LlmClient, error) { return client, nil },
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
