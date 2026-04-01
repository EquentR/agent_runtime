# Core Agent MVP Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a minimal but durable `core/agent` execution loop that can drive one LLM conversation, execute tool calls, integrate short-term memory, and optionally emit task runtime events.

**Architecture:** Add a small `core/agent` package centered on a synchronous runner that owns the agent loop: assemble messages, call the provider, execute returned tool calls through `core/tools`, append tool results, and stop on final assistant text or max-steps exhaustion. Keep task integration optional through a thin event sink abstraction so `core/agent` stays reusable outside HTTP/background tasks. Memory is integrated as a lightweight context manager, not as a session persistence layer.

**Tech Stack:** Go, existing `core/providers`, `core/tools`, `core/memory`, `core/tasks`, `gorm`-backed task runtime only through optional adapter

---

## Scope And Non-Goals

- In scope:
  - single-threaded agent loop
  - non-streaming provider path first
  - tool execution with JSON argument decoding
  - short-term memory integration
  - optional task event emission bridge
  - deterministic unit tests for normal answer, tool loop, tool failure, max-steps, cancellation
- Explicitly out of scope for MVP:
  - `core/rag`
  - skills system
  - multi-agent / child-task orchestration
  - approval workflow
  - distributed runners / resumable checkpoints
  - provider streaming orchestration in the first cut

## Proposed Package Shape

Create a new `core/agent` package with these initial files:

- Create: `core/agent/types.go`
- Create: `core/agent/runner.go`
- Create: `core/agent/events.go`
- Create: `core/agent/memory.go`
- Create: `core/agent/errors.go`
- Create: `core/agent/runner_test.go`
- Create: `core/agent/memory_test.go`

Optional follow-up, only if the package starts feeling too crowded during implementation:

- Create later: `core/agent/tool_executor.go`
- Create later: `core/agent/task_events.go`

Do not add `core/agent/session_store.go`, `core/agent/skills.go`, or `core/agent/streaming.go` in MVP.

## Public API Sketch

Use a narrow API so upper layers can compose around it:

```go
package agent

import (
	"context"

	"github.com/EquentR/agent_runtime/core/memory"
	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/tools"
)

type Client interface {
	Chat(ctx context.Context, req model.ChatRequest) (model.ChatResponse, error)
}

type EventSink interface {
	OnStepStart(ctx context.Context, event StepEvent) error
	OnStepFinish(ctx context.Context, event StepEvent) error
	OnToolStart(ctx context.Context, event ToolEvent) error
	OnToolFinish(ctx context.Context, event ToolEvent) error
	OnLog(ctx context.Context, event LogEvent) error
}

type Runner struct {
	client   Client
	registry *tools.Registry
	options  Options
}

type Options struct {
	SystemPrompt string
	Model        string
	MaxSteps     int
	MaxTokens    int64
	Memory       *memory.Manager
	EventSink    EventSink
	TraceID      string
	ToolChoice   types.ToolChoice
	Metadata     map[string]string
	Actor        string
}

type RunInput struct {
	Messages []model.Message
	Tools    []types.Tool
}

type RunResult struct {
	Messages      []model.Message
	FinalMessage  model.Message
	StepsExecuted int
	ToolCalls     int
	StopReason    string
	Usage         model.TokenUsage
}
```

Notes:

- Reuse `model.LlmClient` if it fits cleanly; do not invent another richer provider abstraction.
- `RunInput.Messages` should be normalized conversation input for this invocation only.
- `RunResult.Messages` should contain all newly produced assistant/tool messages for the run, not the caller's historical input duplicated again.
- Default `MaxSteps` should be small and safe, such as `8`.

### Task 1: Create package skeleton and public contracts

**Files:**
- Create: `core/agent/types.go`
- Create: `core/agent/errors.go`
- Test: `core/agent/runner_test.go`

**Step 1: Write the failing compile-oriented test scaffold**

Add a test that references the intended API surface:

```go
func TestRunnerRequiresClient(t *testing.T) {
	_, err := NewRunner(nil, nil, Options{})
	if err == nil {
		t.Fatal("NewRunner() error = nil, want non-nil")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./core/agent -run TestRunnerRequiresClient`
Expected: FAIL because package or symbols do not exist.

**Step 3: Write minimal contracts**

Define:

- `Options`
- `RunInput`
- `RunResult`
- `StepEvent`
- `ToolEvent`
- `LogEvent`
- `EventSink`
- `NewRunner(client model.LlmClient, registry *tools.Registry, options Options) (*Runner, error)`
- `ErrNilClient`
- `ErrMaxStepsExceeded`

Validation rules:

- `client` cannot be nil
- `MaxSteps <= 0` becomes default value
- nil `registry` is allowed only when no tools are provided at runtime

**Step 4: Run test to verify it passes**

Run: `go test ./core/agent -run TestRunnerRequiresClient`
Expected: PASS

### Task 2: Implement the simplest non-tool successful run

**Files:**
- Modify: `core/agent/runner.go`
- Modify: `core/agent/runner_test.go`

**Step 1: Write the failing test**

```go
func TestRunnerReturnsDirectAssistantMessage(t *testing.T) {
	client := &stubClient{
		responses: []model.ChatResponse{{
			Message: model.Message{Role: model.RoleAssistant, Content: "hello"},
		}},
	}

	runner, err := NewRunner(client, nil, Options{Model: "test-model", MaxSteps: 4})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	result, err := runner.Run(context.Background(), RunInput{
		Messages: []model.Message{{Role: model.RoleUser, Content: "say hi"}},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.FinalMessage.Content != "hello" {
		t.Fatalf("final content = %q, want %q", result.FinalMessage.Content, "hello")
	}
	if result.StepsExecuted != 1 {
		t.Fatalf("steps = %d, want 1", result.StepsExecuted)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./core/agent -run TestRunnerReturnsDirectAssistantMessage`
Expected: FAIL because `Run` is not implemented.

**Step 3: Write minimal implementation**

In `runner.go`, implement:

- prepend optional system prompt to the request messages
- call `client.Chat`
- read `resp.Message` as the authoritative assistant message
- if `resp.Message` is empty but legacy fields are populated, call `resp.SyncMessageFromFields()`
- append the final assistant message to `RunResult.Messages`
- return immediately when there are no tool calls

Pseudo-shape:

```go
func (r *Runner) Run(ctx context.Context, input RunInput) (RunResult, error) {
	conversation := buildConversation(r.options.SystemPrompt, input.Messages)
	resp, err := r.client.Chat(ctx, model.ChatRequest{
		Model:      r.options.Model,
		Messages:   conversation,
		MaxTokens:  r.options.MaxTokens,
		Tools:      input.Tools,
		ToolChoice: r.options.ToolChoice,
		TraceID:    r.options.TraceID,
	})
	if err != nil {
		return RunResult{}, err
	}
	msg := normalizeAssistantMessage(resp)
	return RunResult{
		Messages:      []model.Message{msg},
		FinalMessage:  msg,
		StepsExecuted: 1,
		StopReason:    "assistant_message",
		Usage:         resp.Usage,
	}, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./core/agent -run TestRunnerReturnsDirectAssistantMessage`
Expected: PASS

### Task 3: Add short-term memory integration without creating session persistence

**Files:**
- Create: `core/agent/memory.go`
- Create: `core/agent/memory_test.go`
- Modify: `core/agent/runner.go`

**Step 1: Write the failing tests**

Cover two behaviors:

```go
func TestRunnerUsesMemoryContextMessages(t *testing.T) {}
func TestRunnerWritesUserAndAssistantMessagesBackToMemory(t *testing.T) {}
```

Use a real `memory.Manager` with a simple fake token counter so the test checks integration through public APIs, not internals.

**Step 2: Run tests to verify they fail**

Run: `go test ./core/agent -run TestRunnerUsesMemoryContextMessages|TestRunnerWritesUserAndAssistantMessagesBackToMemory`
Expected: FAIL

**Step 3: Write minimal implementation**

Rules:

- Before calling the provider, append the new input messages into memory if `options.Memory != nil`
- Build the provider request from `memory.ContextMessages(ctx)` rather than raw input messages
- After each assistant message and each tool result, write them back via `Memory.AddMessage`
- Do not try to persist full sessions; keep memory usage limited to `memory.Manager`

Important helper:

```go
func buildRequestMessages(ctx context.Context, mem *memory.Manager, systemPrompt string, input []model.Message) ([]model.Message, error)
```

It should:

- clone inputs
- optionally add user input to memory
- obtain compacted context from memory
- prepend one system prompt only once per provider request

**Step 4: Run tests to verify they pass**

Run: `go test ./core/agent -run TestRunnerUsesMemoryContextMessages|TestRunnerWritesUserAndAssistantMessagesBackToMemory`
Expected: PASS

### Task 4: Implement tool-call loop and JSON argument decoding

**Files:**
- Modify: `core/agent/runner.go`
- Modify: `core/agent/runner_test.go`

**Step 1: Write the failing test for one tool round-trip**

```go
func TestRunnerExecutesToolCallsAndContinuesConversation(t *testing.T) {
	registry := tools.NewRegistry()
	_ = registry.Register(tools.Tool{
		Name: "lookup_weather",
		Handler: func(ctx context.Context, arguments map[string]interface{}) (string, error) {
			if arguments["city"] != "Shanghai" {
				t.Fatalf("city = %#v, want Shanghai", arguments["city"])
			}
			return "sunny", nil
		},
	})

	client := &stubClient{responses: []model.ChatResponse{
		{Message: model.Message{Role: model.RoleAssistant, ToolCalls: []types.ToolCall{{ID: "call_1", Name: "lookup_weather", Arguments: `{"city":"Shanghai"}`}}}},
		{Message: model.Message{Role: model.RoleAssistant, Content: "The weather is sunny."}},
	}}

	runner, _ := NewRunner(client, registry, Options{Model: "test-model", MaxSteps: 4})
	result, err := runner.Run(context.Background(), RunInput{Messages: []model.Message{{Role: model.RoleUser, Content: "weather?"}}, Tools: registry.List()})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.FinalMessage.Content != "The weather is sunny." {
		t.Fatalf("final content = %q", result.FinalMessage.Content)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./core/agent -run TestRunnerExecutesToolCallsAndContinuesConversation`
Expected: FAIL

**Step 3: Write minimal implementation**

Add a loop:

- call provider
- normalize returned assistant message
- if no tool calls, finish
- otherwise execute each tool serially
- decode `ToolCall.Arguments` JSON into `map[string]interface{}`
- append one assistant tool-call message to conversation exactly as returned
- append one `role=tool` message per tool result with `ToolCallId` populated
- continue until a final assistant text arrives or max steps reached

Pseudo-shape:

```go
for step := 1; step <= maxSteps; step++ {
	resp := callProvider(...)
	assistant := normalizeAssistantMessage(resp)
	produced = append(produced, assistant)
	conversation = append(conversation, assistant)
	if len(assistant.ToolCalls) == 0 {
		return success
	}
	for _, tc := range assistant.ToolCalls {
		args, err := decodeArguments(tc.Arguments)
		...
		toolOutput, err := r.registry.Execute(toolCtx, tc.Name, args)
		toolMsg := model.Message{Role: model.RoleTool, ToolCallId: tc.ID, Content: toolOutput}
		conversation = append(conversation, toolMsg)
		produced = append(produced, toolMsg)
	}
}
return ErrMaxStepsExceeded
```

Follow provider compatibility rules:

- preserve assistant `ProviderState`
- preserve `Reasoning` and `ReasoningItems`
- preserve `ToolCall.ThoughtSignature`
- do not rewrite tool call IDs

**Step 4: Run test to verify it passes**

Run: `go test ./core/agent -run TestRunnerExecutesToolCallsAndContinuesConversation`
Expected: PASS

### Task 5: Add event sink support and task runtime bridge points

**Files:**
- Create: `core/agent/events.go`
- Modify: `core/agent/runner.go`
- Modify: `core/agent/runner_test.go`

**Step 1: Write the failing test**

```go
func TestRunnerEmitsStepAndToolEvents(t *testing.T) {}
```

Use a recording sink and assert event order:

- `step.start`
- `tool.start`
- `tool.finish`
- `step.finish`

**Step 2: Run test to verify it fails**

Run: `go test ./core/agent -run TestRunnerEmitsStepAndToolEvents`
Expected: FAIL

**Step 3: Write minimal implementation**

Implement helpers:

```go
func (r *Runner) emitStepStart(ctx context.Context, step int, title string)
func (r *Runner) emitStepFinish(ctx context.Context, step int, title string, payload any)
func (r *Runner) emitToolStart(ctx context.Context, step int, call types.ToolCall)
func (r *Runner) emitToolFinish(ctx context.Context, step int, call types.ToolCall, output string, err error)
func (r *Runner) emitLog(ctx context.Context, level, message string, fields map[string]any)
```

Event schema should stay plain and task-compatible:

- `StepEvent`: `Step`, `Title`, `Metadata`
- `ToolEvent`: `Step`, `ToolCallID`, `ToolName`, `Arguments`, `Output`, `Err`
- `LogEvent`: `Level`, `Message`, `Metadata`

Do not import `core/tasks` into `core/agent` yet. Keep the sink generic so a later adapter can map sink methods to `tasks.Runtime`.

**Step 4: Run test to verify it passes**

Run: `go test ./core/agent -run TestRunnerEmitsStepAndToolEvents`
Expected: PASS

### Task 6: Handle tool errors, invalid JSON arguments, and max-steps exhaustion

**Files:**
- Modify: `core/agent/errors.go`
- Modify: `core/agent/runner.go`
- Modify: `core/agent/runner_test.go`

**Step 1: Write the failing tests**

Add:

```go
func TestRunnerReturnsErrorWhenToolArgumentsAreInvalidJSON(t *testing.T) {}
func TestRunnerReturnsErrorWhenToolExecutionFails(t *testing.T) {}
func TestRunnerReturnsErrorWhenMaxStepsExceeded(t *testing.T) {}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./core/agent -run TestRunnerReturnsErrorWhenToolArgumentsAreInvalidJSON|TestRunnerReturnsErrorWhenToolExecutionFails|TestRunnerReturnsErrorWhenMaxStepsExceeded`
Expected: FAIL

**Step 3: Write minimal implementation**

Rules:

- invalid tool JSON returns wrapped error including tool name
- tool execution error returns wrapped error including tool name
- if loop reaches `MaxSteps` while still getting tool calls, return `ErrMaxStepsExceeded`
- partial produced messages may be returned alongside error only if that makes tests and callers simpler; document whichever behavior you choose and keep it consistent

Suggested error text:

- `decode tool arguments for "lookup_weather": ...`
- `execute tool "lookup_weather": ...`
- `agent max steps exceeded`

**Step 4: Run tests to verify they pass**

Run: `go test ./core/agent -run TestRunnerReturnsErrorWhenToolArgumentsAreInvalidJSON|TestRunnerReturnsErrorWhenToolExecutionFails|TestRunnerReturnsErrorWhenMaxStepsExceeded`
Expected: PASS

### Task 7: Respect cancellation and pass tool runtime metadata through context

**Files:**
- Modify: `core/agent/runner.go`
- Modify: `core/agent/runner_test.go`

**Step 1: Write the failing tests**

Add:

```go
func TestRunnerStopsWhenContextCancelled(t *testing.T) {}
func TestRunnerPassesRuntimeMetadataToToolContext(t *testing.T) {}
```

The second test should read tool runtime from `core/tools.RuntimeFromContext(ctx)` and validate:

- `StepID` is stable for the tool invocation, for example `step-1`
- `Actor` comes from `Options.Actor`
- `Metadata` includes runner-level metadata if supplied

**Step 2: Run tests to verify they fail**

Run: `go test ./core/agent -run TestRunnerStopsWhenContextCancelled|TestRunnerPassesRuntimeMetadataToToolContext`
Expected: FAIL

**Step 3: Write minimal implementation**

Before each provider call and each tool execution:

- check `ctx.Err()` and return early if canceled
- wrap tool execution context with `core/tools.WithRuntime`

Suggested helper:

```go
func (r *Runner) toolContext(ctx context.Context, step int, call types.ToolCall) context.Context {
	return tools.WithRuntime(ctx, &tools.Runtime{
		StepID:   fmt.Sprintf("step-%d", step),
		Actor:    r.options.Actor,
		Metadata: cloneStringMap(r.options.Metadata),
	})
}
```

If a task adapter later wants task IDs, it can enrich the incoming context before `Run` is called.

**Step 4: Run tests to verify they pass**

Run: `go test ./core/agent -run TestRunnerStopsWhenContextCancelled|TestRunnerPassesRuntimeMetadataToToolContext`
Expected: PASS

### Task 8: Add a thin adapter plan for `core/tasks` integration without coupling packages too early

**Files:**
- Modify later only if needed: `core/agent/events.go`
- Reference only: `core/tasks/runtime.go`

**Step 1: Do not write production adapter code yet**

Instead, document the intended adapter shape in package comments or a short comment block:

```go
// A future tasks adapter can implement EventSink by mapping:
// - OnStepStart  -> runtime.StartStep
// - OnStepFinish -> runtime.FinishStep
// - OnToolStart  -> runtime.Emit(EventToolStarted, ...)
// - OnToolFinish -> runtime.Emit(EventToolFinished, ...)
// - OnLog        -> runtime.Emit(EventLogMessage, ...)
```

**Step 2: Keep MVP package dependency one-way**

`core/agent` may depend on:

- `core/providers/types`
- `core/tools`
- `core/memory`
- `core/types`

`core/agent` should not yet import `core/tasks` directly.

**Step 3: Verify compile boundaries**

Run: `go test ./core/agent`
Expected: PASS

### Task 9: Run full verification and polish package docs

**Files:**
- Modify if useful: `README.md`
- Create if useful: `core/agent/README.md`

**Step 1: Add only minimal docs**

If package is stable enough, add a short `core/agent/README.md` describing:

- package purpose
- MVP capabilities
- explicit non-goals
- example construction flow

Do not expand root `README.md` until implementation and tests are green.

**Step 2: Run focused tests**

Run: `go test ./core/agent ./core/memory ./core/tools ./core/tasks`
Expected: PASS

**Step 3: Run full repository verification**

Run: `go test ./...`
Expected: PASS

Run: `go build ./cmd/...`
Expected: PASS

Run: `go list ./...`
Expected: PASS

**Step 4: Update root docs if implementation is complete**

If `core/agent` exists and tests are green, update `README.md` to move `core/agent` from “not landed” to “MVP landed”, but only if the wording is accurate.

## Implementation Notes And Guardrails

- Prefer `resp.Message` over legacy flattened response fields.
- Never drop assistant `ProviderState`, `Reasoning`, `ReasoningItems`, or `ToolCall.ThoughtSignature`.
- Always include `ToolCallId` on tool result messages.
- Keep tool execution serial in MVP.
- Keep event sink failures non-fatal only if explicitly documented; otherwise surface them. Recommended default: event sink failures are non-fatal and logged through `OnLog` only when possible.
- Clone message slices before mutation so caller-owned input is not modified.
- Do not add streaming support in the same patch as non-streaming loop.
- Do not invent session persistence abstractions yet.
- Do not introduce `skills` hooks beyond maybe one placeholder interface type; avoid dead abstractions.

## Recommended Test Matrix

- direct assistant response
- one tool call then final answer
- multiple tool calls in one assistant turn
- invalid tool JSON
- missing registry when tool is requested
- tool handler returns error
- max steps exceeded
- context cancellation
- memory read/write integration
- tool runtime metadata propagation
- assistant message with provider state survives loop append

## Suggested Commit Breakdown

Only create commits if the user explicitly asks.

- Commit 1: `feat: add core agent package skeleton`
- Commit 2: `feat: implement core agent tool loop`
- Commit 3: `test: cover core agent memory and runtime integration`
- Commit 4: `docs: document core agent mvp boundaries`
