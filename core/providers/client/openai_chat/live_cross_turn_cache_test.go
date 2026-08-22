package openai_chat

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	model "github.com/EquentR/agent_runtime/core/providers/types"
)

func TestLiveCrossTurnPromptCacheReplay(t *testing.T) {
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
		testModel = "gpt-5.4"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	client := NewOpenAIChatClient(apiKey, baseURL, 90*time.Second)

	prefix := strings.Repeat("Cross turn cache stable prefix alpha beta gamma delta epsilon zeta.\n", 80)
	baseMessages := []model.Message{{
		Role:    model.RoleUser,
		Content: prefix + "\nReply with exactly CACHE_OK.",
	}}

	first, err := client.Chat(ctx, model.ChatRequest{
		Model:     testModel,
		Messages:  baseMessages,
		MaxTokens: 32,
	})
	if err != nil {
		t.Fatalf("first Chat() error = %v", err)
	}
	if first.Message.ProviderState == nil {
		t.Fatal("first Message.ProviderState = nil, want provider state for replay")
	}
	t.Logf("first usage=%#v content=%q", first.Usage, first.Message.Content)

	time.Sleep(2 * time.Second)

	sameRunMessages := []model.Message{
		{Role: model.RoleUser, Content: prefix + "\nReply with exactly CACHE_OK."},
		first.Message,
	}
	sameRun, err := client.Chat(ctx, model.ChatRequest{
		Model:     testModel,
		Messages:  sameRunMessages,
		MaxTokens: 32,
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
		Model: testModel,
		Messages: []model.Message{
			{Role: model.RoleUser, Content: prefix + "\nReply with exactly CACHE_OK."},
			replayedAssistant,
		},
		MaxTokens: 32,
	})
	if err != nil {
		t.Fatalf("cross-turn replay Chat() error = %v", err)
	}
	t.Logf("cross-turn replay usage=%#v content=%q", crossTurn.Usage, crossTurn.Message.Content)

	if crossTurn.Usage.CachedPromptTokens != sameRun.Usage.CachedPromptTokens {
		t.Fatalf("cross-turn replay cache differs from same-run: same-run=%d cross-turn=%d", sameRun.Usage.CachedPromptTokens, crossTurn.Usage.CachedPromptTokens)
	}
	if crossTurn.Usage.PromptTokens != sameRun.Usage.PromptTokens {
		t.Fatalf("cross-turn replay prompt differs from same-run: same-run=%d cross-turn=%d", sameRun.Usage.PromptTokens, crossTurn.Usage.PromptTokens)
	}

	// Sanity check that the upstream reports prompt caching for an exact repeat.
	repeat, err := client.Chat(ctx, model.ChatRequest{
		Model:     testModel,
		Messages:  baseMessages,
		MaxTokens: 32,
	})
	if err != nil {
		t.Fatalf("repeat Chat() error = %v", err)
	}
	t.Logf("repeat usage=%#v content=%q", repeat.Usage, repeat.Message.Content)
	if repeat.Usage.CachedPromptTokens > first.Usage.CachedPromptTokens {
		t.Logf("upstream reported additional cached tokens for exact repeat: first=%d repeat=%d", first.Usage.CachedPromptTokens, repeat.Usage.CachedPromptTokens)
	}
}
