# LLM Request Timeout Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make LLM provider request timeout configurable from app config, default it to 10 minutes, and apply it consistently to both OpenAI completions and OpenAI responses clients.

**Architecture:** Add one app-level timeout field with a resolved default of 10 minutes, then thread that resolved value through the server-side LLM client factory into both provider client constructors. Keep the scope narrow: only provider request timeout wiring changes, with focused tests at config, wiring, and client levels.

**Tech Stack:** Go 1.25, yaml.v3, github.com/sashabaranov/go-openai, github.com/openai/openai-go/v3, httptest

---

### Task 1: Add app-level timeout config with a 10-minute default

**Files:**
- Modify: `app/config/app.go`
- Modify: `app/config/app_test.go`
- Modify: `conf/app.yaml`

- [ ] **Step 1: Write the failing config tests**

Add these tests to `app/config/app_test.go`:

```go
func TestConfigUnmarshalSupportsLLMRequestTimeout(t *testing.T) {
	var cfg Config
	if err := yaml.Unmarshal([]byte(`
llmRequestTimeout: 3m
`), &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal() error = %v", err)
	}

	if got := cfg.LLMRequestTimeout; got != 3*time.Minute {
		t.Fatalf("cfg.LLMRequestTimeout = %v, want %v", got, 3*time.Minute)
	}
}

func TestConfigResolvedLLMRequestTimeoutDefaultsToTenMinutes(t *testing.T) {
	var cfg Config

	if got := cfg.ResolvedLLMRequestTimeout(); got != 10*time.Minute {
		t.Fatalf("ResolvedLLMRequestTimeout() = %v, want %v", got, 10*time.Minute)
	}
}

func TestConfigResolvedLLMRequestTimeoutPreservesConfiguredValue(t *testing.T) {
	cfg := Config{LLMRequestTimeout: 3 * time.Minute}

	if got := cfg.ResolvedLLMRequestTimeout(); got != 3*time.Minute {
		t.Fatalf("ResolvedLLMRequestTimeout() = %v, want %v", got, 3*time.Minute)
	}
}
```

Also update the import block in `app/config/app_test.go` to include:

```go
import (
	"reflect"
	"testing"
	"time"

	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	builtin "github.com/EquentR/agent_runtime/core/tools/builtin"
	"gopkg.in/yaml.v3"
)
```

- [ ] **Step 2: Run config tests to verify they fail**

Run: `go test ./app/config`

Expected: FAIL with missing `LLMRequestTimeout` / missing `ResolvedLLMRequestTimeout` compile errors.

- [ ] **Step 3: Add the config field and resolved-default helper**

Update `app/config/app.go`:

```go
package config

import (
	"time"

	coretasks "github.com/EquentR/agent_runtime/core/tasks"
	builtin "github.com/EquentR/agent_runtime/core/tools/builtin"
	coretypes "github.com/EquentR/agent_runtime/core/types"
	"github.com/EquentR/agent_runtime/pkg/db"
	"github.com/EquentR/agent_runtime/pkg/log"
	"github.com/EquentR/agent_runtime/pkg/rest"
)

const defaultLLMRequestTimeout = 10 * time.Minute

type Config struct {
	WorkspaceDir       string                      `yaml:"workspaceDir"`
	Server             rest.Config                 `yaml:"server"`
	Sqlite             db.Database                 `yaml:"sqlite"`
	Log                log.Config                  `yaml:"log"`
	Tasks              TaskManagerConfig           `yaml:"tasks"`
	Tools              ToolsConfig                 `yaml:"tools"`
	LLMRequestTimeout  time.Duration               `yaml:"llmRequestTimeout"`
	LLM                []coretypes.LLMProvider     `yaml:"llmProviders"`
	Embedding          coretypes.EmbeddingProvider `yaml:"embeddingProvider"`
	Rerank             coretypes.RerankingProvider `yaml:"rerankProvider"`
}

func (c Config) ResolvedLLMRequestTimeout() time.Duration {
	if c.LLMRequestTimeout > 0 {
		return c.LLMRequestTimeout
	}
	return defaultLLMRequestTimeout
}
```

- [ ] **Step 4: Add the sample config entry**

Insert this block into `conf/app.yaml` between `tools:` and `llmProviders:`:

```yaml
llmRequestTimeout: 10m
```

- [ ] **Step 5: Run config tests to verify they pass**

Run: `go test ./app/config`

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add app/config/app.go app/config/app_test.go conf/app.yaml
git commit -m "feat(config): add configurable llm request timeout"
```

### Task 2: Thread the resolved timeout through server wiring

**Files:**
- Modify: `app/commands/serve.go:112-127`
- Modify: `app/commands/serve_test.go`

- [ ] **Step 1: Write the failing wiring test**

Add this test to `app/commands/serve_test.go`:

```go
func TestBuildLLMClientFactoryUsesConfiguredRequestTimeout(t *testing.T) {
	factory := buildLLMClientFactory(7 * time.Minute)
	provider := &coretypes.LLMProvider{BaseProvider: coretypes.BaseProvider{Name: "openai", BaseURL: "https://example.com", APIKey: "test-key"}}

	responsesModel := &coretypes.LLMModel{BaseModel: coretypes.BaseModel{ID: "gpt-5.4", Name: "GPT 5.4"}, Type: coretypes.LLMTypeOpenAIResponses}
	responsesClient, err := factory(provider, responsesModel)
	if err != nil {
		t.Fatalf("factory(responses) error = %v", err)
	}
	if _, ok := responsesClient.(*openairesponses.Client); !ok {
		t.Fatalf("responses client type = %T, want *openairesponses.Client", responsesClient)
	}

	completionsModel := &coretypes.LLMModel{BaseModel: coretypes.BaseModel{ID: "glm-5", Name: "GLM-5"}, Type: coretypes.LLMTypeOpenAICompletions}
	completionsClient, err := factory(provider, completionsModel)
	if err != nil {
		t.Fatalf("factory(completions) error = %v", err)
	}
	if _, ok := completionsClient.(*openaicompletions.Client); !ok {
		t.Fatalf("completions client type = %T, want *openaicompletions.Client", completionsClient)
	}
}
```

Update the import block in `app/commands/serve_test.go` to include:

```go
openaicompletions "github.com/EquentR/agent_runtime/core/providers/client/openai_completions"
openairesponses "github.com/EquentR/agent_runtime/core/providers/client/openai_responses"
```

- [ ] **Step 2: Run the focused server wiring test and confirm it fails**

Run: `go test ./app/commands -run '^TestBuildLLMClientFactoryUsesConfiguredRequestTimeout$'`

Expected: FAIL because `buildLLMClientFactory` does not yet accept a timeout parameter.

- [ ] **Step 3: Pass the resolved timeout through startup wiring**

Update `app/commands/serve.go`:

```go
if err := registerAgentRunExecutor(
	taskManager,
	approvalStore,
	interactionStore,
	resolver,
	conversationStore,
	toolRegistry,
	promptRuntime.Resolver,
	workspaceRoot,
	buildLLMClientFactory(c.ResolvedLLMRequestTimeout()),
	auditRuntime.RunRecorder,
); err != nil {
	log.Panicf("Failed to register agent.run executor: %v", err)
}
```

Replace the factory function with:

```go
func buildLLMClientFactory(requestTimeout time.Duration) coreagent.ClientFactory {
	return func(provider *coretypes.LLMProvider, llmModel *coretypes.LLMModel) (model.LlmClient, error) {
		if provider == nil {
			return nil, fmt.Errorf("llm provider is not configured")
		}
		switch llmModel.ModelType() {
		case coretypes.LLMTypeOpenAIResponses:
			return openairesponses.NewOpenAiResponsesClient(provider.AuthKey(), provider.BaseURL(), requestTimeout), nil
		case coretypes.LLMTypeOpenAICompletions:
			return openaicompletions.NewOpenAiCompletionsClient(provider.BaseURL(), provider.AuthKey(), requestTimeout), nil
		case coretypes.LLMTypeGoogle:
			return googleclient.NewGoogleGenAIClient(provider.BaseURL(), provider.AuthKey())
		default:
			return nil, fmt.Errorf("unsupported llm model type %q", llmModel.ModelType())
		}
	}
}
```

- [ ] **Step 4: Run the focused server wiring test and nearby package tests**

Run: `go test ./app/commands -run 'TestBuildLLMClientFactoryUsesConfiguredRequestTimeout|TestBuildAgentRunExecutorDependenciesThreadPromptRuntimeAndWorkspaceRoot|TestRegisterAgentRunExecutorPromptWiringKeepsAuditRecorder'`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add app/commands/serve.go app/commands/serve_test.go
git commit -m "refactor(serve): thread llm request timeout into client factory"
```

### Task 3: Apply timeout injection to OpenAI completions client

**Files:**
- Modify: `core/providers/client/openai_completions/client.go`
- Modify: `core/providers/client/openai_completions/client_reasoning_test.go`
- Modify: `core/providers/client/provider_replay_integration_test.go`

- [ ] **Step 1: Write the failing completions timeout test**

Add this test near the other client tests in `core/providers/client/openai_completions/client_reasoning_test.go`:

```go
func TestNewOpenAiCompletionsClient_AppliesHTTPTimeout(t *testing.T) {
	client := NewOpenAiCompletionsClient("https://example.com/v1", "test-key", 2*time.Minute)

	if client == nil || client.client == nil {
		t.Fatal("client or underlying openai client is nil")
	}

	httpClient, ok := client.client.GetConfig().HTTPClient.(*http.Client)
	if !ok {
		t.Fatalf("HTTPClient type = %T, want *http.Client", client.client.GetConfig().HTTPClient)
	}
	if httpClient.Timeout != 2*time.Minute {
		t.Fatalf("httpClient.Timeout = %v, want %v", httpClient.Timeout, 2*time.Minute)
	}
}
```

Update imports in `core/providers/client/openai_completions/client_reasoning_test.go` to include:

```go
import (
	"net/http"
	"time"
)
```

- [ ] **Step 2: Run the focused completions test and confirm it fails**

Run: `go test ./core/providers/client/openai_completions -run '^TestNewOpenAiCompletionsClient_AppliesHTTPTimeout$'`

Expected: FAIL because the constructor does not yet accept timeout or set `HTTPClient.Timeout`.

- [ ] **Step 3: Update the completions constructor to accept request timeout**

Change `core/providers/client/openai_completions/client.go`:

```go
func NewOpenAiCompletionsClient(baseUrl, apiKey string, requestTimeout time.Duration) *Client {
	cfg := openai.DefaultConfig(apiKey)
	if baseUrl != "" {
		cfg.BaseURL = baseUrl
	}
	if requestTimeout > 0 {
		cfg.HTTPClient = &http.Client{Timeout: requestTimeout}
	}
	return &Client{
		client: openai.NewClientWithConfig(cfg),
	}
}
```

Update imports in that file to include:

```go
import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/EquentR/agent_runtime/core/providers/tools"
	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/sashabaranov/go-openai"
)
```

- [ ] **Step 4: Update call sites to the new signature**

Replace each existing two-argument constructor call with a timeout-aware call.

In `core/providers/client/provider_replay_integration_test.go`, update all constructor calls like this:

```go
client := openaiclient.NewOpenAiCompletionsClient(server.URL+"/v1", "test-key", time.Minute)
```

In `core/providers/client/openai_completions/client_reasoning_test.go`, update all constructor calls like this:

```go
client := NewOpenAiCompletionsClient(server.URL+"/v1", "test-key", time.Minute)
```

- [ ] **Step 5: Run completions package tests**

Run: `go test ./core/providers/client/openai_completions`

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add core/providers/client/openai_completions/client.go core/providers/client/openai_completions/client_reasoning_test.go core/providers/client/provider_replay_integration_test.go
git commit -m "feat(openai): apply configurable timeout to completions client"
```

### Task 4: Add a timeout regression test for OpenAI responses client

**Files:**
- Create: `core/providers/client/openai_responses/client_timeout_test.go`

- [ ] **Step 1: Write the failing responses timeout regression test**

Create `core/providers/client/openai_responses/client_timeout_test.go`:

```go
package openai_responses

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	model "github.com/EquentR/agent_runtime/core/providers/types"
)

func TestOpenAiResponsesClient_HonorsConfiguredRequestTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			http.NotFound(w, r)
			return
		}
		time.Sleep(150 * time.Millisecond)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: response.completed\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"id\":\"msg_1\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}]}}\n\n"))
	}))
	defer server.Close()

	client := NewOpenAiResponsesClient("test-key", server.URL, 50*time.Millisecond)
	_, err := client.Chat(context.Background(), model.ChatRequest{
		Model:    "gpt-5.4",
		Messages: []model.Message{{Role: model.RoleUser, Content: "hello"}},
	})
	if err == nil {
		t.Fatal("Chat() error = nil, want timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("Chat() error = %v, want deadline exceeded/timeout", err)
	}
}
```

Before running, add the missing `strings` import if needed:

```go
import "strings"
```

- [ ] **Step 2: Run the focused responses timeout test**

Run: `go test ./core/providers/client/openai_responses -run '^TestOpenAiResponsesClient_HonorsConfiguredRequestTimeout$'`

Expected: PASS if the existing constructor wiring already honors injected timeout. If it fails, inspect the actual request path and adjust only the test payload/path until it targets the real responses streaming endpoint.

- [ ] **Step 3: Keep the regression test and run the full responses package tests**

Run: `go test ./core/providers/client/openai_responses`

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add core/providers/client/openai_responses/client_timeout_test.go
git commit -m "test(openai): cover responses request timeout behavior"
```

### Task 5: Run cross-package verification and finish

**Files:**
- Modify: none
- Test: `app/config/app_test.go`
- Test: `app/commands/serve_test.go`
- Test: `core/providers/client/openai_completions/client_reasoning_test.go`
- Test: `core/providers/client/openai_responses/client_timeout_test.go`
- Test: `core/providers/client/provider_replay_integration_test.go`

- [ ] **Step 1: Run the focused verification set**

Run: `go test ./app/config ./app/commands ./core/providers/client/openai_completions ./core/providers/client/openai_responses ./core/providers/client`

Expected: PASS

- [ ] **Step 2: Run the repository-level Go verification commands**

Run: `go test ./... && go build ./cmd/... && go list ./...`

Expected: all commands succeed with exit code 0.

- [ ] **Step 3: Commit the final verification-only checkpoint**

```bash
git add app/config/app.go app/config/app_test.go conf/app.yaml app/commands/serve.go app/commands/serve_test.go core/providers/client/openai_completions/client.go core/providers/client/openai_completions/client_reasoning_test.go core/providers/client/openai_responses/client_timeout_test.go core/providers/client/provider_replay_integration_test.go
git commit -m "fix(llm): make provider request timeout configurable"
```

## Self-Review

- Spec coverage: Task 1 covers config field + default 10m; Task 2 covers wiring from app config into startup factory; Task 3 covers completions constructor timeout injection; Task 4 covers responses timeout regression; Task 5 covers full verification.
- Placeholder scan: removed generic TODO language; every task has exact files, code snippets, commands, and expected outcomes.
- Type consistency: uses `LLMRequestTimeout`, `ResolvedLLMRequestTimeout()`, and `buildLLMClientFactory(requestTimeout time.Duration)` consistently across tasks.
