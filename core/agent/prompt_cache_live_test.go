package agent

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	openaichat "github.com/EquentR/agent_runtime/core/providers/client/openai_chat"
	openaicompletions "github.com/EquentR/agent_runtime/core/providers/client/openai_completions"
	openairesponses "github.com/EquentR/agent_runtime/core/providers/client/openai_responses"
	model "github.com/EquentR/agent_runtime/core/providers/types"
)

type liveRunnerPromptCacheAdapter struct {
	name   string
	client model.LlmClient
}

func TestLiveRunnerPromptCacheAcrossOpenAIAdapters(t *testing.T) {
	if os.Getenv("PROMPT_CACHE_LIVE_TEST") != "1" {
		t.Skip("set PROMPT_CACHE_LIVE_TEST=1 to run live runner prompt cache comparison")
	}
	apiKey := strings.TrimSpace(os.Getenv("PROMPT_CACHE_TEST_API_KEY"))
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("PROMPT_CACHE_TEST_BASE_URL")), "/")
	testModel := strings.TrimSpace(os.Getenv("PROMPT_CACHE_TEST_MODEL"))
	if testModel == "" {
		testModel = "oai/gpt-5.5"
	}
	if apiKey == "" || baseURL == "" {
		t.Fatal("PROMPT_CACHE_TEST_API_KEY and PROMPT_CACHE_TEST_BASE_URL are required")
	}

	adapters := []liveRunnerPromptCacheAdapter{
		{
			name:   "openai_chat",
			client: openaichat.NewOpenAIChatClient(apiKey, baseURL, 90*time.Second),
		},
		{
			name:   "openai_completions",
			client: openaicompletions.NewOpenAiCompletionsClient(baseURL, apiKey, 90*time.Second),
		},
		{
			name:   "openai_responses",
			client: openairesponses.NewOpenAiResponsesClient(apiKey, baseURL, 90*time.Second),
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	delay := liveRunnerPromptCacheDelay()
	// Keep the forced current-date prompt stable so this test isolates provider and replay behavior.
	fixedNow := time.Date(2026, time.May, 31, 12, 0, 0, 0, time.UTC)
	for _, adapter := range adapters {
		t.Run(adapter.name, func(t *testing.T) {
			conversationID := fmt.Sprintf("agent-runtime-live-runner-cache-%s-%d", adapter.name, time.Now().UTC().UnixNano())
			usages := make([]model.TokenUsage, 0, 4)
			for attempt := 1; attempt <= 4; attempt++ {
				runner, err := NewRunner(adapter.client, nil, Options{
					Model:     testModel,
					MaxTokens: 16,
					Metadata: map[string]string{
						"conversation_id": conversationID,
						"provider_id":     "live",
						"model_id":        testModel,
					},
					Now: func() time.Time { return fixedNow },
				})
				if err != nil {
					t.Fatalf("attempt %d NewRunner() error = %v", attempt, err)
				}
				result, err := runner.Run(ctx, RunInput{Messages: liveRunnerPromptCacheMessages()})
				if err != nil {
					t.Fatalf("attempt %d Run() error = %v", attempt, err)
				}
				usages = append(usages, result.Usage)
				t.Logf("attempt=%d prompt=%d cached=%d completion=%d total=%d content=%q",
					attempt,
					result.Usage.PromptTokens,
					result.Usage.CachedPromptTokens,
					result.Usage.CompletionTokens,
					result.Usage.TotalTokens,
					strings.TrimSpace(result.FinalMessage.Content),
				)
				if attempt < 4 {
					select {
					case <-time.After(delay):
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
				}
			}
			if !liveRunnerPromptCacheObserved(usages) {
				t.Fatalf("no runner prompt cache behavior observed for %s; usages=%#v", adapter.name, usages)
			}
		})
	}
}

func liveRunnerPromptCacheDelay() time.Duration {
	raw := strings.TrimSpace(os.Getenv("PROMPT_CACHE_TEST_DELAY_MS"))
	if raw == "" {
		return 1200 * time.Millisecond
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms < 0 {
		return 1200 * time.Millisecond
	}
	return time.Duration(ms) * time.Millisecond
}

func liveRunnerPromptCacheMessages() []model.Message {
	prefix := strings.Repeat("Runner cache stable prefix alpha beta gamma delta epsilon zeta eta theta.\n", 180)
	return []model.Message{
		{Role: model.RoleSystem, Content: "You are verifying runner prompt-cache behavior. Reply with exactly CACHE_OK."},
		{Role: model.RoleUser, Content: prefix + "\nReply with exactly CACHE_OK."},
	}
}

func liveRunnerPromptCacheObserved(usages []model.TokenUsage) bool {
	if len(usages) < 2 {
		return false
	}
	firstPrompt := usages[0].PromptTokens
	for _, usage := range usages[1:] {
		if usage.CachedPromptTokens > 0 {
			return true
		}
		if firstPrompt > 0 && usage.PromptTokens > 0 && usage.PromptTokens*100 <= firstPrompt*80 {
			return true
		}
	}
	return false
}
