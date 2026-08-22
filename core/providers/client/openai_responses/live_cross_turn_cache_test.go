package openai_responses

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	coretypes "github.com/EquentR/agent_runtime/core/types"
)

func TestLiveCrossTurnPromptCacheToolReplay(t *testing.T) {
	if os.Getenv("LIVE_CACHE_PROBE") != "1" {
		t.Skip("set LIVE_CACHE_PROBE=1 to run live cross-turn cache probe")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("OPENAI_BASE_URL")), "/")
	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if baseURL == "" || apiKey == "" {
		t.Fatal("OPENAI_BASE_URL and OPENAI_API_KEY are required")
	}
	testModel := strings.TrimSpace(os.Getenv("LIVE_CACHE_MODEL"))
	if testModel == "" {
		testModel = "gpt-5.6-terra"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	client := NewOpenAiResponsesClient(apiKey, baseURL, 90*time.Second)

	tools := []coretypes.Tool{{
		Name:        "echo_probe",
		Description: "Echo the input back",
		Parameters: coretypes.JSONSchema{
			Type:       "object",
			Properties: map[string]coretypes.SchemaProperty{"value": {Type: "string"}},
			Required:   []string{"value"},
		},
	}}

	prefix := strings.Repeat("Cross turn responses cache stable prefix alpha beta gamma delta.\n", 80)
	userMessage := model.Message{Role: model.RoleUser, Content: prefix + "\nCall echo_probe with value probe and then reply CACHE_OK."}

	first, err := client.Chat(ctx, model.ChatRequest{
		Model:     testModel,
		Messages:  []model.Message{userMessage},
		MaxTokens: 256,
		Tools:     tools,
	})
	if err != nil {
		t.Fatalf("first Chat() error = %v", err)
	}
	if first.Message.ProviderState == nil {
		t.Fatal("first Message.ProviderState = nil, want provider state for replay")
	}
	t.Logf("first tool_calls=%d usage=%#v content=%q", len(first.Message.ToolCalls), first.Usage, first.Message.Content)

	time.Sleep(2 * time.Second)

	toolResult := model.Message{Role: model.RoleTool, ToolCallId: first.Message.ToolCalls[0].ID, Content: `{"value":"probe"}`}
	sameRunMessages := []model.Message{userMessage, first.Message, toolResult}
	sameRun, err := client.Chat(ctx, model.ChatRequest{
		Model:     testModel,
		Messages:  sameRunMessages,
		MaxTokens: 128,
		Tools:     tools,
	})
	if err != nil {
		t.Fatalf("same-run continuation Chat() error = %v", err)
	}
	t.Logf("same-run continuation usage=%#v content=%q", sameRun.Usage, sameRun.Message.Content)

	time.Sleep(2 * time.Second)

	// Simulate persistence: provider state round-trips through JSON storage.
	rawState, err := json.Marshal(first.Message.ProviderState)
	if err != nil {
		t.Fatalf("json.Marshal(provider state) error = %v", err)
	}
	var restoredState model.ProviderState
	if err := json.Unmarshal(rawState, &restoredState); err != nil {
		t.Fatalf("json.Unmarshal(provider state) error = %v", err)
	}
	replayedAssistant := first.Message
	replayedAssistant.ProviderState = &restoredState

	crossTurn, err := client.Chat(ctx, model.ChatRequest{
		Model:     testModel,
		Messages:  []model.Message{userMessage, replayedAssistant, toolResult},
		MaxTokens: 128,
		Tools:     tools,
	})
	if err != nil {
		t.Fatalf("cross-turn replay Chat() error = %v", err)
	}
	t.Logf("cross-turn replay usage=%#v content=%q", crossTurn.Usage, crossTurn.Message.Content)

	if crossTurn.Usage.PromptTokens != sameRun.Usage.PromptTokens {
		t.Fatalf("cross-turn replay prompt differs from same-run: same-run=%d cross-turn=%d", sameRun.Usage.PromptTokens, crossTurn.Usage.PromptTokens)
	}
	if crossTurn.Usage.CachedPromptTokens != sameRun.Usage.CachedPromptTokens {
		t.Fatalf("cross-turn replay cache differs from same-run: same-run=%d cross-turn=%d", sameRun.Usage.CachedPromptTokens, crossTurn.Usage.CachedPromptTokens)
	}
}
