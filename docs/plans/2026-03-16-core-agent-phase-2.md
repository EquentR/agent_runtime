# Core Agent Phase 2 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Evolve `core/agent` from a synchronous MVP into a stream-first agent runtime that unifies streaming and non-streaming execution, consumes `LLMModel` context/pricing metadata, aggregates run-level usage/cost, and bridges agent events into `core/tasks` runtime.

**Architecture:** Introduce `RunStream` as the authoritative agent execution path built on provider `ChatStream` and `StreamEvent`. Keep `Run` as a thin aggregator over `RunStream`. Extend `Options` and `RunResult` so the runner can consume `*coretypes.LLMModel`, derive context/token defaults, accumulate per-step usage across the full run, compute run-level cost from model pricing, and optionally emit step/tool/log/stream events into a task-backed adapter.

**Tech Stack:** Go, existing `core/providers/types.Stream`, `core/types.LLMModel`, `core/types.ModelPricing`, `core/tools`, `core/memory`, `core/tasks`

---

## Scope And Guardrails

- In scope:
  - stream-first unified agent loop
  - `Run` implemented on top of `RunStream`
  - stream event model for text/reasoning/tool/usage/completed
  - `LLMModel` integration for request model id, context budgeting, and pricing
  - run-level usage aggregation and cost calculation
  - task runtime adapter implementing `agent.EventSink`
- Out of scope:
  - approval workflow
  - child task orchestration
  - RAG/skills
  - distributed checkpoint resume
  - provider-side cost reconciliation beyond token-based estimation

## Proposed File Changes

- Modify: `core/agent/types.go`
- Modify: `core/agent/events.go`
- Modify: `core/agent/memory.go`
- Modify: `core/agent/errors.go`
- Modify: `core/agent/runner_test.go`
- Modify: `core/agent/memory_test.go`
- Modify: `core/agent/README.md`
- Modify: `README.md`
- Create: `core/agent/stream.go`
- Create: `core/agent/stream_test.go`
- Create: `core/agent/cost.go`
- Create: `core/agent/task_adapter.go`
- Create: `core/agent/task_adapter_test.go`
- Modify: `core/types/cost.go`

Do not move code into `core/tasks` just to support agent integration.

### Task 1: Add run-level stream contracts and tests

**Files:**
- Create: `core/agent/stream.go`
- Create: `core/agent/stream_test.go`
- Modify: `core/agent/types.go`

**Step 1: Write the failing tests**

Add tests covering:

- `TestRunnerRunStreamEmitsTextAndCompletedEvents`
- `TestRunnerRunUsesRunStreamAggregation`
- `TestRunnerRunStreamCollectsToolCallDeltasBeforeExecution`

Test shape:

- Use a fake provider `ChatStream`
- Emit `text_delta`, optional `reasoning_delta`, `tool_call_delta`, `usage`, `completed`
- Assert the runner does not execute a tool until the stream completes and `FinalMessage()` is replayable

**Step 2: Run tests to verify they fail**

Run: `go test ./core/agent -run "TestRunnerRunStream|TestRunnerRunUsesRunStreamAggregation"`
Expected: FAIL

**Step 3: Write minimal stream contracts**

Add public types:

```go
type StreamEventKind string

const (
	EventTextDelta StreamEventKind = "text_delta"
	EventReasoningDelta StreamEventKind = "reasoning_delta"
	EventToolCallDelta StreamEventKind = "tool_call_delta"
	EventUsage StreamEventKind = "usage"
	EventCompleted StreamEventKind = "completed"
)

type RunStreamEvent struct {
	Kind       StreamEventKind
	Step       int
	Text       string
	Reasoning  string
	ToolCall   *types.ToolCall
	Usage      *model.TokenUsage
	Message    *model.Message
	Err        error
	Metadata   map[string]any
}

type RunStreamResult struct {
	Events <-chan RunStreamEvent
	Wait   func() (RunResult, error)
	Close  func() error
}
```

Also add:

- `func (r *Runner) RunStream(ctx context.Context, input RunInput) (*RunStreamResult, error)`

**Step 4: Implement `RunStream` skeleton**

Rules:

- `RunStream` is the authoritative execution path
- `Run` consumes `RunStream`, drains all events, then returns final `RunResult`
- Use provider `ChatStream`, not `Chat`, for each model turn
- Drive the state machine with `RecvEvent()`, not `Recv()`

**Step 5: Run tests to verify they pass**

Run: `go test ./core/agent -run "TestRunnerRunStream|TestRunnerRunUsesRunStreamAggregation"`
Expected: PASS

### Task 2: Implement stream-first step loop with final-message gating

**Files:**
- Modify: `core/agent/stream.go`
- Modify: `core/agent/stream_test.go`

**Step 1: Write failing tests for step gating**

Add tests:

- `TestRunnerRunStreamRequiresFinalMessageBeforeToolExecution`
- `TestRunnerRunStreamReturnsErrorWhenFinalMessageUnavailable`

The first test should prove:

- tool deltas may arrive during stream
- tool execution waits until the stream finishes normally and `FinalMessage()` succeeds

**Step 2: Run tests to verify they fail**

Run: `go test ./core/agent -run "TestRunnerRunStreamRequiresFinalMessageBeforeToolExecution|TestRunnerRunStreamReturnsErrorWhenFinalMessageUnavailable"`
Expected: FAIL

**Step 3: Write minimal implementation**

For each step:

- start provider stream
- forward `text_delta`, `reasoning_delta`, `tool_call_delta`, and `usage` as run stream events
- when stream ends normally, call `FinalMessage()`
- normalize and append the final assistant message
- only then decide whether to finish or execute tools

Important constraints from provider contracts:

- never execute tools directly from raw tool deltas
- never treat partial/aborted stream output as replayable final assistant state
- preserve `ProviderState`, `ReasoningItems`, and `ThoughtSignature`

**Step 4: Run tests to verify they pass**

Run: `go test ./core/agent -run "TestRunnerRunStreamRequiresFinalMessageBeforeToolExecution|TestRunnerRunStreamReturnsErrorWhenFinalMessageUnavailable"`
Expected: PASS

### Task 3: Move synchronous `Run` to aggregate `RunStream`

**Files:**
- Modify: `core/agent/types.go`
- Modify: `core/agent/runner_test.go`
- Modify: `core/agent/stream_test.go`

**Step 1: Write failing regression tests**

Add assertions that existing synchronous tests still pass through the new stream-first path:

- direct assistant response
- tool round-trip
- invalid JSON
- max-steps

**Step 2: Run tests to verify they fail if `Run` still bypasses stream**

Run: `go test ./core/agent`
Expected: FAIL until `Run` uses `RunStream`

**Step 3: Implement aggregation**

`Run` should:

- call `RunStream`
- drain its event channel until closed
- call `Wait()`
- return the terminal `RunResult`

Keep external `Run` semantics stable where reasonable.

**Step 4: Run tests to verify they pass**

Run: `go test ./core/agent`
Expected: PASS

### Task 4: Add `LLMModel` support for context and model resolution

**Files:**
- Modify: `core/agent/types.go`
- Modify: `core/agent/memory.go`
- Modify: `core/agent/memory_test.go`

**Step 1: Write the failing tests**

Add:

- `TestNewRunnerUsesLLMModelIDWhenModelStringEmpty`
- `TestNewRunnerPassesLLMModelToMemoryBudgeting`
- `TestRunnerUsesResolvedOutputBudgetForMaxTokensDefault`

**Step 2: Run tests to verify they fail**

Run: `go test ./core/agent -run "TestNewRunnerUsesLLMModelIDWhenModelStringEmpty|TestNewRunnerPassesLLMModelToMemoryBudgeting|TestRunnerUsesResolvedOutputBudgetForMaxTokensDefault"`
Expected: FAIL

**Step 3: Write minimal implementation**

Extend `Options`:

```go
type Options struct {
	Model        string
	LLMModel     *coretypes.LLMModel
	...
}
```

Rules:

- provider request `Model` resolves in this order:
  1. explicit `Options.Model`
  2. `Options.LLMModel.ModelID()`
- memory manager creation/use should prefer `LLMModel` context window when available
- default `MaxTokens` should prefer `LLMModel.ContextWindow().Output` when positive

Do not make `core/agent` responsible for provider lookup from config collections.

**Step 4: Run tests to verify they pass**

Run: `go test ./core/agent -run "TestNewRunnerUsesLLMModelIDWhenModelStringEmpty|TestNewRunnerPassesLLMModelToMemoryBudgeting|TestRunnerUsesResolvedOutputBudgetForMaxTokensDefault"`
Expected: PASS

### Task 5: Add cost calculation helpers and run-level cost aggregation

**Files:**
- Modify: `core/types/cost.go`
- Create: `core/agent/cost.go`
- Modify: `core/agent/types.go`
- Modify: `core/agent/runner_test.go`

**Step 1: Write the failing tests**

Add tests for:

- `core/types`: `TestModelPricingBreakdownFromUsage`
- `core/types`: `TestModelPricingFallsBackCachedCostToInputPrice`
- `core/agent`: `TestRunnerAggregatesUsageAcrossMultipleSteps`
- `core/agent`: `TestRunnerCalculatesRunCostFromLLMModelPricing`

**Step 2: Run tests to verify they fail**

Run: `go test ./core/types ./core/agent -run "TestModelPricing|TestRunnerAggregatesUsageAcrossMultipleSteps|TestRunnerCalculatesRunCostFromLLMModelPricing"`
Expected: FAIL

**Step 3: Write minimal implementation**

Add helpers in `core/types/cost.go`:

- `func (p TokenPrice) CostForTokens(tokens int64) float64`
- `func (p *ModelPricing) Breakdown(usage model.TokenUsage) CostBreakdown`
- `func (b CostBreakdown) Add(other CostBreakdown) CostBreakdown`

Then add run-scoped aggregation in `core/agent`:

- sum `PromptTokens`, `CachedPromptTokens`, `CompletionTokens`, `TotalTokens` across all model turns
- compute `RunResult.Cost *coretypes.CostBreakdown` when pricing is available

Extend `RunResult`:

```go
type RunResult struct {
	...
	Usage model.TokenUsage
	Cost  *coretypes.CostBreakdown
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./core/types ./core/agent -run "TestModelPricing|TestRunnerAggregatesUsageAcrossMultipleSteps|TestRunnerCalculatesRunCostFromLLMModelPricing"`
Expected: PASS

### Task 6: Add task runtime adapter for step/tool/log emission

**Files:**
- Create: `core/agent/task_adapter.go`
- Create: `core/agent/task_adapter_test.go`

**Step 1: Write the failing tests**

Add tests proving a task-backed sink maps events correctly:

- `TestTaskRuntimeSinkMapsStepEvents`
- `TestTaskRuntimeSinkMapsToolEvents`
- `TestTaskRuntimeSinkMapsLogEvents`

Use a real `tasks.Manager`/`tasks.Runtime` when practical, or a narrow fake runtime interface mirroring the needed methods.

**Step 2: Run tests to verify they fail**

Run: `go test ./core/agent -run "TestTaskRuntimeSink"`
Expected: FAIL

**Step 3: Write minimal implementation**

Create an adapter in `core/agent` that implements `EventSink` by delegating to `tasks.Runtime`:

- `OnStepStart` -> `runtime.StartStep(ctx, fmt.Sprintf("agent.step.%d", event.Step), event.Title)`
- `OnStepFinish` -> `runtime.FinishStep(ctx, event.Metadata)`
- `OnToolStart` -> `runtime.Emit(ctx, tasks.EventToolStarted, "info", payload)`
- `OnToolFinish` -> `runtime.Emit(ctx, tasks.EventToolFinished, level, payload)`
- `OnLog` -> `runtime.Emit(ctx, tasks.EventLogMessage, event.Level, payload)`

Keep this as an adapter inside `core/agent`; do not invert dependency by importing `agent` from `core/tasks`.

**Step 4: Run tests to verify they pass**

Run: `go test ./core/agent -run "TestTaskRuntimeSink"`
Expected: PASS

### Task 7: Decide stream event exposure policy for tasks and implement only the minimum needed now

**Files:**
- Modify: `core/agent/events.go`
- Modify: `core/agent/task_adapter.go`
- Modify: `core/agent/stream_test.go`

**Step 1: Write one failing test for stream-to-task event forwarding**

Add:

- `TestTaskRuntimeSinkMapsRunStreamEventsToLogMessages`

Recommended MVP policy:

- map text/reasoning/tool delta/usage stream events to `tasks.EventLogMessage`
- do not add new task event constants yet unless the mapping becomes unreadable

**Step 2: Run test to verify it fails**

Run: `go test ./core/agent -run "TestTaskRuntimeSinkMapsRunStreamEventsToLogMessages"`
Expected: FAIL

**Step 3: Write minimal implementation**

Extend `EventSink` with one optional method or a small companion interface:

```go
type StreamEventSink interface {
	OnStreamEvent(ctx context.Context, event RunStreamEvent) error
}
```

Then:

- `RunStream` calls it for forwarded stream events
- task adapter maps those events into `runtime.Emit(..., tasks.EventLogMessage, ...)`

**Step 4: Run test to verify it passes**

Run: `go test ./core/agent -run "TestTaskRuntimeSinkMapsRunStreamEventsToLogMessages"`
Expected: PASS

### Task 8: Run full verification and update docs

**Files:**
- Modify: `core/agent/README.md`
- Modify: `README.md`

**Step 1: Update docs only after code is green**

Document:

- stream-first architecture
- `Run` vs `RunStream`
- `LLMModel` integration boundary
- run-level usage/cost
- task adapter availability

**Step 2: Run focused verification**

Run: `go test ./core/agent ./core/types ./core/tasks ./core/memory`
Expected: PASS

**Step 3: Run full verification**

Run: `go test ./...`
Expected: PASS

Run: `go build ./cmd/...`
Expected: PASS

Run: `go list ./...`
Expected: PASS

## Implementation Notes

- Drive the authoritative agent state machine with provider `RecvEvent()`, not `Recv()`.
- Tool execution must wait until the provider stream completes and yields a replayable `FinalMessage()`.
- `Run` should be a wrapper over `RunStream`, not a second implementation.
- `core/agent` should depend only on one selected `*coretypes.LLMModel`, not on provider collections/config loading.
- Cost aggregation is best-effort token-based estimation from normalized usage, not billing reconciliation.
- Use `StreamEventSink` as an optional extension so existing sinks remain source-compatible.
- Keep agent-to-tasks coupling in adapter code only.

## Suggested Commit Breakdown

Only create commits if the user explicitly asks.

- Commit 1: `feat: add stream-first core agent loop`
- Commit 2: `feat: integrate llm model context and cost aggregation`
- Commit 3: `feat: bridge core agent events to task runtime`
- Commit 4: `docs: document stream-first agent runtime`
