package openai_responses

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/types"
)

func TestLiveResponsesClientConsecutiveToolCalls(t *testing.T) {
	if os.Getenv("RESPONSES_LIVE_TEST") != "1" {
		t.Skip("set RESPONSES_LIVE_TEST=1 to run live Responses API integration test")
	}
	apiKey := strings.TrimSpace(os.Getenv("RESPONSES_TEST_API_KEY"))
	baseURL := strings.TrimSpace(os.Getenv("RESPONSES_TEST_BASE_URL"))
	testModel := strings.TrimSpace(os.Getenv("RESPONSES_TEST_MODEL"))
	if testModel == "" {
		testModel = "gpt-5.4"
	}
	if apiKey == "" || baseURL == "" {
		t.Fatal("RESPONSES_TEST_API_KEY and RESPONSES_TEST_BASE_URL are required")
	}

	client := NewOpenAiResponsesClient(apiKey, baseURL, 60*time.Second)
	tools := []types.Tool{
		livePayloadTool("echo_payload", "Echoes the first short JSON payload."),
		livePayloadTool("second_payload", "Accepts the second short JSON payload."),
	}
	messages := []model.Message{{
		Role:    model.RoleUser,
		Content: `Call echo_payload with value "alpha" and step 1. After I give the tool result, call second_payload with value "beta" and step 2. Do not write the final answer until both tools have returned.`,
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	first := liveChat(t, ctx, client, testModel, messages, tools, types.ToolChoice{Type: types.ToolForce, Name: "echo_payload"})
	firstCall := requireSingleToolCall(t, first.Message, "echo_payload")
	requireToolArgs(t, firstCall.Arguments, "alpha", 1)
	messages = append(messages,
		first.Message,
		model.Message{Role: model.RoleTool, ToolCallId: firstCall.ID, Content: `{"ok":true,"value":"alpha","step":1,"next":"call second_payload with value beta and step 2"}`},
	)

	second := liveChat(t, ctx, client, testModel, messages, tools, types.ToolChoice{})
	secondCall := requireSingleToolCall(t, second.Message, "second_payload")
	requireToolArgs(t, secondCall.Arguments, "beta", 2)
	messages = append(messages,
		second.Message,
		model.Message{Role: model.RoleTool, ToolCallId: secondCall.ID, Content: `{"ok":true,"value":"beta","step":2}`},
	)

	final := liveChat(t, ctx, client, testModel, messages, tools, types.ToolChoice{Type: types.ToolNone})
	if strings.TrimSpace(final.Message.Content) == "" {
		t.Fatalf("final content is empty; message = %#v", final.Message)
	}
	if len(final.Message.ToolCalls) != 0 {
		t.Fatalf("final tool calls = %#v, want none", final.Message.ToolCalls)
	}
	t.Logf("live responses consecutive tool calls passed: first=%s second=%s final=%q", firstCall.ID, secondCall.ID, final.Message.Content)
}

func TestLiveResponsesClientFourStepToolChain(t *testing.T) {
	env := liveResponsesEnv(t)
	client := NewOpenAiResponsesClient(env.apiKey, env.baseURL, 60*time.Second)
	tools := []types.Tool{
		livePayloadTool("alpha_tool", "Accepts value alpha with step 1."),
		livePayloadTool("beta_tool", "Accepts value beta with step 2."),
		livePayloadTool("gamma_tool", "Accepts value gamma with step 3."),
		livePayloadTool("delta_tool", "Accepts value delta with step 4."),
	}
	messages := []model.Message{{
		Role:    model.RoleUser,
		Content: `We are testing a four-step tool protocol. For every tool call, use the value and step named in the most recent tool result. Start by calling alpha_tool with value "alpha" and step 1. After delta_tool returns, answer exactly FOUR_CHAIN_DONE.`,
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	sequence := []struct {
		name       string
		value      string
		step       float64
		nextOutput string
		choice     types.ToolChoice
	}{
		{name: "alpha_tool", value: "alpha", step: 1, nextOutput: `{"ok":true,"next":"call beta_tool with value beta and step 2"}`, choice: types.ToolChoice{Type: types.ToolForce, Name: "alpha_tool"}},
		{name: "beta_tool", value: "beta", step: 2, nextOutput: `{"ok":true,"next":"call gamma_tool with value gamma and step 3"}`, choice: types.ToolChoice{Type: types.ToolForce, Name: "beta_tool"}},
		{name: "gamma_tool", value: "gamma", step: 3, nextOutput: `{"ok":true,"next":"call delta_tool with value delta and step 4"}`, choice: types.ToolChoice{Type: types.ToolForce, Name: "gamma_tool"}},
		{name: "delta_tool", value: "delta", step: 4, nextOutput: `{"ok":true,"next":"answer exactly FOUR_CHAIN_DONE"}`, choice: types.ToolChoice{Type: types.ToolForce, Name: "delta_tool"}},
	}
	callIDs := make([]string, 0, len(sequence))
	for _, step := range sequence {
		resp := liveChat(t, ctx, client, env.model, messages, tools, step.choice)
		call := requireSingleToolCall(t, resp.Message, step.name)
		requireToolArgs(t, call.Arguments, step.value, step.step)
		callIDs = append(callIDs, call.ID)
		messages = append(messages,
			resp.Message,
			model.Message{Role: model.RoleTool, ToolCallId: call.ID, Content: step.nextOutput},
		)
	}

	final := liveChat(t, ctx, client, env.model, messages, tools, types.ToolChoice{Type: types.ToolNone})
	if !strings.Contains(final.Message.Content, "FOUR_CHAIN_DONE") {
		t.Fatalf("final content = %q, want FOUR_CHAIN_DONE", final.Message.Content)
	}
	t.Logf("live four-step tool chain passed: calls=%s final=%q", strings.Join(callIDs, ","), final.Message.Content)
}

func TestLiveResponsesClientMultipleToolCallsInOneTurn(t *testing.T) {
	env := liveResponsesEnv(t)
	client := NewOpenAiResponsesClient(env.apiKey, env.baseURL, 60*time.Second)
	tools := []types.Tool{
		livePayloadTool("left_payload", "Accepts the left payload."),
		livePayloadTool("right_payload", "Accepts the right payload."),
	}
	messages := []model.Message{{
		Role:    model.RoleUser,
		Content: `Call both tools before answering: left_payload with value "left" step 1 and right_payload with value "right" step 2. Do not answer in text until both tool results are supplied.`,
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	first := liveChat(t, ctx, client, env.model, messages, tools, types.ToolChoice{Type: types.ToolForce})
	calls := requireToolCalls(t, first.Message, []string{"left_payload", "right_payload"})
	requireToolArgs(t, calls["left_payload"].Arguments, "left", 1)
	requireToolArgs(t, calls["right_payload"].Arguments, "right", 2)
	messages = append(messages, first.Message)
	for _, name := range []string{"left_payload", "right_payload"} {
		call := calls[name]
		messages = append(messages, model.Message{Role: model.RoleTool, ToolCallId: call.ID, Content: `{"ok":true}`})
	}

	final := liveChat(t, ctx, client, env.model, messages, tools, types.ToolChoice{Type: types.ToolNone})
	if strings.TrimSpace(final.Message.Content) == "" || len(final.Message.ToolCalls) != 0 {
		t.Fatalf("final message = %#v, want text-only final response", final.Message)
	}
	t.Logf("live multiple tools in one turn passed: calls=%s,%s final=%q", calls["left_payload"].ID, calls["right_payload"].ID, final.Message.Content)
}

func TestLiveResponsesClientSystemSamplingAndNewUserToolAfterHistory(t *testing.T) {
	env := liveResponsesEnv(t)
	client := NewOpenAiResponsesClient(env.apiKey, env.baseURL, 60*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	temp := float32(0.2)
	topP := float32(0.9)
	systemResp, err := client.Chat(ctx, model.ChatRequest{
		Model: env.model,
		Messages: []model.Message{
			{Role: model.RoleSystem, Content: "You must answer with exactly SYSTEM_SAMPLING_OK."},
			{Role: model.RoleUser, Content: "Say the required token."},
		},
		Sampling:  model.SamplingParams{Temperature: &temp, TopP: &topP},
		MaxTokens: 64,
	})
	if err != nil {
		t.Fatalf("system/sampling Chat() error = %v", err)
	}
	if !strings.Contains(systemResp.Message.Content, "SYSTEM_SAMPLING_OK") {
		t.Fatalf("system/sampling content = %q, want SYSTEM_SAMPLING_OK", systemResp.Message.Content)
	}

	tools := []types.Tool{livePayloadTool("history_payload", "Accepts a payload after prior assistant history.")}
	messages := []model.Message{
		{Role: model.RoleUser, Content: "Record this prior turn."},
		systemResp.Message,
		{Role: model.RoleUser, Content: `Now call history_payload with value "history" and step 3.`},
	}
	toolResp := liveChat(t, ctx, client, env.model, messages, tools, types.ToolChoice{Type: types.ToolForce, Name: "history_payload"})
	call := requireSingleToolCall(t, toolResp.Message, "history_payload")
	requireToolArgs(t, call.Arguments, "history", 3)
	t.Logf("live system/sampling plus new user tool passed: call=%s", call.ID)
}

func TestLiveResponsesClientFullProviderStateReplayAndRefsOnlyFallback(t *testing.T) {
	env := liveResponsesEnv(t)
	client := NewOpenAiResponsesClient(env.apiKey, env.baseURL, 60*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	tools := []types.Tool{livePayloadTool("echo_payload", "Echoes a payload.")}
	fullReplayMessages := []model.Message{
		{Role: model.RoleUser, Content: `Call echo_payload with value "alpha" step 1. After the tool result, reply exactly FULL_REPLAY_OK.`},
		{
			Role: model.RoleAssistant,
			ProviderState: &model.ProviderState{
				Provider:   providerName,
				Format:     responseStateFormat,
				Version:    messageVersion,
				ResponseID: "live_full_replay_fixture",
				Payload:    json.RawMessage(`{"response_id":"live_full_replay_fixture","output":[{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"I will call the tool now."}]},{"type":"function_call","call_id":"call_live_full_replay","name":"echo_payload","arguments":"{\"value\":\"alpha\",\"step\":1}"}]}`),
			},
		},
		{Role: model.RoleTool, ToolCallId: "call_live_full_replay", Content: `{"ok":true,"value":"alpha","step":1}`},
	}
	fullReplay := liveChat(t, ctx, client, env.model, fullReplayMessages, tools, types.ToolChoice{Type: types.ToolNone})
	if !strings.Contains(fullReplay.Message.Content, "FULL_REPLAY_OK") {
		t.Fatalf("full replay content = %q, want FULL_REPLAY_OK", fullReplay.Message.Content)
	}

	refsOnlyMessages := []model.Message{
		{
			Role:    model.RoleAssistant,
			Content: "This assistant message should replay as normalized content, not as item_reference.",
			ProviderState: &model.ProviderState{
				Provider:   providerName,
				Format:     responseStateFormat,
				Version:    messageVersion,
				ResponseID: "live_refs_only_fixture",
				Payload:    json.RawMessage(`{"response_id":"live_refs_only_fixture","items":[{"id":"msg_ref_only","type":"message"}]}`),
			},
		},
		{Role: model.RoleUser, Content: "Reply exactly REFS_FALLBACK_OK."},
	}
	refsFallback := liveChat(t, ctx, client, env.model, refsOnlyMessages, nil, types.ToolChoice{})
	if !strings.Contains(refsFallback.Message.Content, "REFS_FALLBACK_OK") {
		t.Fatalf("refs fallback content = %q, want REFS_FALLBACK_OK", refsFallback.Message.Content)
	}
	t.Logf("live provider state full replay and refs-only fallback passed")
}

type liveEnv struct {
	apiKey  string
	baseURL string
	model   string
}

func liveResponsesEnv(t *testing.T) liveEnv {
	t.Helper()
	if os.Getenv("RESPONSES_LIVE_TEST") != "1" {
		t.Skip("set RESPONSES_LIVE_TEST=1 to run live Responses API integration test")
	}
	env := liveEnv{
		apiKey:  strings.TrimSpace(os.Getenv("RESPONSES_TEST_API_KEY")),
		baseURL: strings.TrimSpace(os.Getenv("RESPONSES_TEST_BASE_URL")),
		model:   strings.TrimSpace(os.Getenv("RESPONSES_TEST_MODEL")),
	}
	if env.model == "" {
		env.model = "gpt-5.4"
	}
	if env.apiKey == "" || env.baseURL == "" {
		t.Fatal("RESPONSES_TEST_API_KEY and RESPONSES_TEST_BASE_URL are required")
	}
	return env
}

func livePayloadTool(name, description string) types.Tool {
	return types.Tool{
		Name:        name,
		Description: description,
		Parameters: types.JSONSchema{
			Type: "object",
			Properties: map[string]types.SchemaProperty{
				"value": {Type: "string", Description: "A short value."},
				"step":  {Type: "integer", Description: "The protocol step number."},
			},
			Required: []string{"value", "step"},
		},
	}
}

func liveChat(t *testing.T, ctx context.Context, client *Client, testModel string, messages []model.Message, tools []types.Tool, choice types.ToolChoice) model.ChatResponse {
	t.Helper()
	resp, err := client.Chat(ctx, model.ChatRequest{
		Model:      testModel,
		Messages:   messages,
		Tools:      tools,
		ToolChoice: choice,
		MaxTokens:  128,
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	return resp
}

func requireSingleToolCall(t *testing.T, message model.Message, wantName string) types.ToolCall {
	t.Helper()
	if len(message.ToolCalls) != 1 {
		t.Fatalf("tool calls = %#v, want exactly one %s call", message.ToolCalls, wantName)
	}
	call := message.ToolCalls[0]
	if strings.TrimSpace(call.ID) == "" {
		t.Fatalf("tool call id is empty: %#v", call)
	}
	if call.Name != wantName {
		t.Fatalf("tool call name = %q, want %q; call = %#v", call.Name, wantName, call)
	}
	return call
}

func requireToolCalls(t *testing.T, message model.Message, wantNames []string) map[string]types.ToolCall {
	t.Helper()
	got := make(map[string]types.ToolCall, len(message.ToolCalls))
	for _, call := range message.ToolCalls {
		got[call.Name] = call
	}
	for _, name := range wantNames {
		call, ok := got[name]
		if !ok {
			names := make([]string, 0, len(got))
			for gotName := range got {
				names = append(names, gotName)
			}
			sort.Strings(names)
			t.Fatalf("tool calls = %v, want %s; raw=%#v", names, name, message.ToolCalls)
		}
		if strings.TrimSpace(call.ID) == "" {
			t.Fatalf("tool call id for %s is empty: %#v", name, call)
		}
	}
	return got
}

func requireToolArgs(t *testing.T, raw string, wantValue string, wantStep float64) {
	t.Helper()
	var args struct {
		Value string  `json:"value"`
		Step  float64 `json:"step"`
	}
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		t.Fatalf("tool args %q are not valid JSON: %v", raw, err)
	}
	if args.Value != wantValue || args.Step != wantStep {
		t.Fatalf("tool args = %#v, want value=%q step=%v", args, wantValue, wantStep)
	}
}
