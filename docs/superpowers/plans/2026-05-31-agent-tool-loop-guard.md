# Agent Tool Loop Guard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a runtime loop guard that detects repeated tool-call patterns, warns once, then safely completes or fails without changing the default agent step budget.

**Architecture:** Add a focused `LoopGuard` in `core/agent` that profiles tool calls and records repeat counters. `Runner` owns the guard for one run, injects warning system messages before the next model request, emits audit events, and converts stop decisions into deterministic run results or `ErrToolLoopDetected`.

**Tech Stack:** Go 1.25, existing `core/agent` Runner, `core/tools.Registry`, `core/audit` recorder, existing `core/agent` test stubs.

---

## Source Material

- Spec: `docs/superpowers/specs/2026-05-31-agent-tool-loop-guard-design.md`
- Main integration point: `core/agent/stream.go`
- Runner events and audit helpers: `core/agent/events.go`
- Runner options and result shape: `core/agent/types.go`
- Error sentinels: `core/agent/errors.go`
- Existing runner test helpers: `core/agent/runner_test.go`
- Existing executor persistence tests: `core/agent/executor_test.go`

## File Structure

- Create `core/agent/loop_guard.go`: loop guard types, defaults, tool profile extraction, warning text, stop message, counters.
- Create `core/agent/loop_guard_test.go`: pure unit tests for signatures, thresholds, ignored tools, and option defaults.
- Modify `core/agent/types.go`: add `LoopGuard LoopGuardOptions` to `Options`.
- Modify `core/agent/errors.go`: add `ErrToolLoopDetected`.
- Modify `core/agent/stream.go`: instantiate guard per run, inject pending warnings into request context, handle stop decisions, and preserve result shape.
- Modify `core/agent/events.go`: add small loop guard audit emitter if useful, or call existing `appendAuditEvent` directly from `stream.go`.
- Modify `core/agent/runner_test.go`: runner-level tests for warning injection, safe completion stop, failed-loop stop, audit payloads, and unchanged `DefaultMaxSteps`.
- Modify `core/agent/executor_test.go`: executor-level safe completion persistence test.

## Task 1: LoopGuard Domain Model

**Files:**
- Create: `core/agent/loop_guard.go`
- Create: `core/agent/loop_guard_test.go`
- Modify: `core/agent/types.go`
- Modify: `core/agent/errors.go`

- [ ] **Step 1: Write failing pure LoopGuard tests**

Add `core/agent/loop_guard_test.go` with tests covering default options, write overwrite thresholds, different write targets, replace line ranges, list error loops, read windows, search query identity, and ignored interaction tools.

```go
package agent

import (
	"errors"
	"strings"
	"testing"

	coretypes "github.com/EquentR/agent_runtime/core/types"
)

func TestLoopGuardWarnsAndStopsRepeatedWriteOverwriteSuccess(t *testing.T) {
	guard := NewLoopGuard(LoopGuardOptions{})
	first := guard.AfterToolResult(coretypes.ToolCall{ID: "call_1", Name: "write_file", Arguments: `{"path":"skills/foo.md","mode":"overwrite","content":"one"}`}, mustDecodeLoopGuardArgs(t, `{"path":"skills/foo.md","mode":"overwrite","content":"one"}`), "", nil)
	if first.Decision != LoopGuardAllow {
		t.Fatalf("first decision = %q, want allow", first.Decision)
	}
	second := guard.AfterToolResult(coretypes.ToolCall{ID: "call_2", Name: "write_file", Arguments: `{"path":"skills/foo.md","mode":"overwrite","content":"two"}`}, mustDecodeLoopGuardArgs(t, `{"path":"skills/foo.md","mode":"overwrite","content":"two"}`), "", nil)
	if second.Decision != LoopGuardWarn || second.RepeatCount != 2 || second.StopStrategy != "" {
		t.Fatalf("second result = %#v, want warn repeat_count=2", second)
	}
	if !strings.Contains(second.WarningText, "write_file") || !strings.Contains(second.WarningText, "skills/foo.md") {
		t.Fatalf("warning text = %q, want tool and path", second.WarningText)
	}
	third := guard.AfterToolResult(coretypes.ToolCall{ID: "call_3", Name: "write_file", Arguments: `{"path":"skills/foo.md","mode":"overwrite","content":"three"}`}, mustDecodeLoopGuardArgs(t, `{"path":"skills/foo.md","mode":"overwrite","content":"three"}`), "", nil)
	if third.Decision != LoopGuardStop || third.StopStrategy != LoopGuardStopStrategySafeCompletion || third.RepeatCount != 3 {
		t.Fatalf("third result = %#v, want safe completion stop", third)
	}
}

func TestLoopGuardAllowsWriteOverwriteDifferentTargets(t *testing.T) {
	guard := NewLoopGuard(LoopGuardOptions{})
	for _, call := range []coretypes.ToolCall{
		{ID: "call_1", Name: "write_file", Arguments: `{"path":"skills/a.md","mode":"overwrite","content":"one"}`},
		{ID: "call_2", Name: "write_file", Arguments: `{"path":"skills/b.md","mode":"overwrite","content":"two"}`},
		{ID: "call_3", Name: "write_file", Arguments: `{"path":"skills/c.md","mode":"overwrite","content":"three"}`},
	} {
		result := guard.AfterToolResult(call, mustDecodeLoopGuardArgs(t, call.Arguments), "", nil)
		if result.Decision != LoopGuardAllow {
			t.Fatalf("result for %s = %#v, want allow", call.ID, result)
		}
	}
}

func TestLoopGuardAllowsWriteReplaceLinesDifferentRanges(t *testing.T) {
	guard := NewLoopGuard(LoopGuardOptions{})
	calls := []coretypes.ToolCall{
		{ID: "call_1", Name: "write_file", Arguments: `{"path":"skills/foo.md","mode":"replace_lines","start_line":1,"end_line":3,"content":"one"}`},
		{ID: "call_2", Name: "write_file", Arguments: `{"path":"skills/foo.md","mode":"replace_lines","start_line":4,"end_line":6,"content":"two"}`},
		{ID: "call_3", Name: "write_file", Arguments: `{"path":"skills/foo.md","mode":"replace_lines","start_line":7,"end_line":9,"content":"three"}`},
	}
	for _, call := range calls {
		result := guard.AfterToolResult(call, mustDecodeLoopGuardArgs(t, call.Arguments), "", nil)
		if result.Decision != LoopGuardAllow {
			t.Fatalf("result for %s = %#v, want allow", call.ID, result)
		}
	}
}

func TestLoopGuardStopsRepeatedListFilesError(t *testing.T) {
	guard := NewLoopGuard(LoopGuardOptions{})
	errPathRequired := errors.New("path is required")
	call := coretypes.ToolCall{ID: "call_1", Name: "list_files", Arguments: `{}`}
	if result := guard.AfterToolResult(call, mustDecodeLoopGuardArgs(t, call.Arguments), "", errPathRequired); result.Decision != LoopGuardAllow {
		t.Fatalf("first result = %#v, want allow", result)
	}
	call.ID = "call_2"
	if result := guard.AfterToolResult(call, mustDecodeLoopGuardArgs(t, call.Arguments), "", errPathRequired); result.Decision != LoopGuardWarn || result.Reason != LoopGuardReasonSameErrorRepeated {
		t.Fatalf("second result = %#v, want same-error warning", result)
	}
	call.ID = "call_3"
	result := guard.AfterToolResult(call, mustDecodeLoopGuardArgs(t, call.Arguments), "", errPathRequired)
	if result.Decision != LoopGuardStop || result.StopStrategy != LoopGuardStopStrategyFailedLoop {
		t.Fatalf("third result = %#v, want failed-loop stop", result)
	}
	if !errors.Is(result.Err, ErrToolLoopDetected) {
		t.Fatalf("third error = %v, want ErrToolLoopDetected", result.Err)
	}
}

func TestLoopGuardReadFileWindowRules(t *testing.T) {
	guard := NewLoopGuard(LoopGuardOptions{})
	differentWindows := []coretypes.ToolCall{
		{ID: "call_1", Name: "read_file", Arguments: `{"path":"skills/foo.md","start_line":1,"line_count":20}`},
		{ID: "call_2", Name: "read_file", Arguments: `{"path":"skills/foo.md","start_line":21,"line_count":20}`},
	}
	for _, call := range differentWindows {
		if result := guard.AfterToolResult(call, mustDecodeLoopGuardArgs(t, call.Arguments), "ok", nil); result.Decision != LoopGuardAllow {
			t.Fatalf("different-window result = %#v, want allow", result)
		}
	}
	repeated := coretypes.ToolCall{ID: "call_3", Name: "read_file", Arguments: `{"path":"skills/foo.md","start_line":21,"line_count":20}`}
	if result := guard.AfterToolResult(repeated, mustDecodeLoopGuardArgs(t, repeated.Arguments), "ok", nil); result.Decision != LoopGuardWarn {
		t.Fatalf("repeated-window second result = %#v, want warn", result)
	}
	repeated.ID = "call_4"
	if result := guard.AfterToolResult(repeated, mustDecodeLoopGuardArgs(t, repeated.Arguments), "ok", nil); result.Decision != LoopGuardStop {
		t.Fatalf("repeated-window third result = %#v, want stop", result)
	}
}

func TestLoopGuardSearchIdentityAndInteractionExclusion(t *testing.T) {
	guard := NewLoopGuard(LoopGuardOptions{})
	searchA := coretypes.ToolCall{ID: "call_1", Name: "search_file", Arguments: `{"path":"core","query":"LoopGuard"}`}
	searchB := coretypes.ToolCall{ID: "call_2", Name: "search_file", Arguments: `{"path":"core","query":"LoopGuardOptions"}`}
	if result := guard.AfterToolResult(searchA, mustDecodeLoopGuardArgs(t, searchA.Arguments), "ok", nil); result.Decision != LoopGuardAllow {
		t.Fatalf("searchA result = %#v, want allow", result)
	}
	if result := guard.AfterToolResult(searchB, mustDecodeLoopGuardArgs(t, searchB.Arguments), "ok", nil); result.Decision != LoopGuardAllow {
		t.Fatalf("searchB result = %#v, want allow", result)
	}
	for index := 0; index < 4; index++ {
		call := coretypes.ToolCall{ID: "ask", Name: "ask_user", Arguments: `{"question":"continue?"}`}
		if result := guard.AfterToolResult(call, mustDecodeLoopGuardArgs(t, call.Arguments), "", nil); result.Decision != LoopGuardAllow {
			t.Fatalf("ask_user result = %#v, want allow", result)
		}
	}
}

func mustDecodeLoopGuardArgs(t *testing.T, raw string) map[string]interface{} {
	t.Helper()
	args, err := decodeToolArguments(coretypes.ToolCall{Name: "test_tool", Arguments: raw})
	if err != nil {
		t.Fatalf("decodeToolArguments(%s) error = %v", raw, err)
	}
	return args
}
```

- [ ] **Step 2: Run the pure tests and verify RED**

Run:

```powershell
go test ./core/agent -run '^TestLoopGuard' -count=1
```

Expected: FAIL because `NewLoopGuard`, `LoopGuardOptions`, `LoopGuardAllow`, `ErrToolLoopDetected`, and helper types do not exist yet.

- [ ] **Step 3: Add LoopGuard implementation and public options**

In `core/agent/errors.go`, add:

```go
ErrToolLoopDetected = errors.New("tool loop detected")
```

In `core/agent/types.go`, add this field to `Options`:

```go
LoopGuard LoopGuardOptions
```

Create `core/agent/loop_guard.go` implementing:

```go
package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	coretypes "github.com/EquentR/agent_runtime/core/types"
)

type LoopGuardDecision string

const (
	LoopGuardAllow LoopGuardDecision = "allow"
	LoopGuardWarn  LoopGuardDecision = "warn"
	LoopGuardStop  LoopGuardDecision = "stop"
)

const (
	LoopGuardReasonSameTargetWriteRepeated = "same_target_write_repeated"
	LoopGuardReasonSameTargetReadRepeated  = "same_target_read_repeated"
	LoopGuardReasonSameSearchRepeated      = "same_search_repeated"
	LoopGuardReasonSameErrorRepeated       = "same_error_repeated"

	LoopGuardStopStrategySafeCompletion = "safe_completion"
	LoopGuardStopStrategyFailedLoop     = "failed_loop"
)

type LoopGuardOptions struct {
	Enabled                    *bool
	SameTargetSuccessSoftLimit int
	SameTargetSuccessHardLimit int
	SameErrorSoftLimit         int
	SameErrorHardLimit         int
}

type LoopGuardResult struct {
	Decision     LoopGuardDecision
	Reason       string
	ToolName     string
	Operation    string
	Target       string
	RepeatCount  int
	SoftLimit    int
	HardLimit    int
	StopStrategy string
	WarningText  string
	FinalMessage string
	Err          error
}

type LoopGuard struct {
	options       LoopGuardOptions
	successCounts map[string]int
	errorCounts   map[string]int
	lastSuccessKey string
	lastErrorKey   string
	lastSuccess    loopGuardProfile
	lastError      loopGuardProfile
}

type loopGuardProfile struct {
	ToolName            string
	OperationKind       string
	TargetKey           string
	ArgumentFingerprint string
	OutcomeKind         string
	ErrorFingerprint    string
	Reason              string
	StopStrategy        string
	Countable           bool
}
```

The implementation must:

- Default `LoopGuardOptions{}` to enabled with all four limits set to 2/3.
- Treat `Enabled: nil` as enabled and `Enabled: pointer-to-false` as disabled, so the guard can be default-on while tests can disable it.
- Treat `ask_user` as excluded and always `allow`.
- Profile `write_file` using `path`, `mode`, and for non-overwrite modes the line range keys `start_line`, `end_line`, `line_start`, `line_end`, `line`, `line_count`; an omitted `mode` is `overwrite`.
- Profile `list_files` by full argument fingerprint for errors.
- Profile `read_file` by `path + start_line + line_count`.
- Profile `search_file` and `grep_file` by full argument fingerprint.
- Count success only when `err == nil`.
- Count failure by `tool + operation + target + argument_fingerprint + normalized_error`.
- Reset consecutive counters when a different key appears.
- Return `warn` at soft limit and `stop` at hard limit.
- Build safe completion final text as: `文件已写入 `%s`。运行时检测到模型正在重复覆盖同一文件，因此已停止继续调用工具。`
- Build failed-loop error as: `fmt.Errorf("%w: %s repeatedly failed with %q", ErrToolLoopDetected, profile.ToolName, normalizedError)`.
- Every hard stop must include either `StopStrategy: safe_completion` plus `FinalMessage`, or `StopStrategy: failed_loop` plus an `ErrToolLoopDetected` wrapped error.

- [ ] **Step 4: Run the pure tests and verify GREEN**

Run:

```powershell
go test ./core/agent -run '^TestLoopGuard' -count=1
```

Expected: PASS.

## Task 2: Runner Warning Injection and Stop Handling

**Files:**
- Modify: `core/agent/stream.go`
- Modify: `core/agent/events.go`
- Modify: `core/agent/runner_test.go`

- [ ] **Step 1: Write failing runner integration tests**

Add tests to `core/agent/runner_test.go`:

```go
func TestRunnerLoopGuardInjectsWarningAfterRepeatedWriteSuccess(t *testing.T) {
	registry := newTestRegistry(t, map[string]func(context.Context, map[string]interface{}) (string, error){
		"write_file": func(context.Context, map[string]interface{}) (string, error) {
			return `{"path":"skills/foo.md","written":true}`, nil
		},
	})
	client := &stubClient{streams: []model.Stream{
		newStubStream([]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "write_file", Arguments: `{"path":"skills/foo.md","mode":"overwrite","content":"one"}`}}}}}, model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "write_file", Arguments: `{"path":"skills/foo.md","mode":"overwrite","content":"one"}`}}}, nil),
		newStubStream([]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_2", Name: "write_file", Arguments: `{"path":"skills/foo.md","mode":"overwrite","content":"two"}`}}}}}, model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_2", Name: "write_file", Arguments: `{"path":"skills/foo.md","mode":"overwrite","content":"two"}`}}}, nil),
		newStubStream([]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "done"}}}, model.Message{Role: model.RoleAssistant, Content: "done"}, nil),
	}}
	runner, err := NewRunner(client, registry, Options{Model: "test-model", RuntimePromptBuilder: runtimeprompt.NewBuilder(nil), MaxSteps: 5})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	result, err := runner.Run(context.Background(), RunInput{Messages: []model.Message{{Role: model.RoleUser, Content: "write it"}}, Tools: registry.List()})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.FinalMessage.Content != "done" {
		t.Fatalf("final content = %q, want done", result.FinalMessage.Content)
	}
	if len(client.streamRequests) != 3 {
		t.Fatalf("stream request count = %d, want 3", len(client.streamRequests))
	}
	lastRequest := client.streamRequests[2].Messages
	found := false
	for _, message := range lastRequest {
		if message.Role == model.RoleSystem && strings.Contains(message.Content, "previous write_file call already succeeded") && strings.Contains(message.Content, "skills/foo.md") {
			found = true
		}
	}
	if !found {
		t.Fatalf("third request messages = %#v, want loop guard warning system message", lastRequest)
	}
}

func TestRunnerLoopGuardStopsRepeatedWriteOverwriteAsSafeCompletion(t *testing.T) {
	calls := 0
	registry := newTestRegistry(t, map[string]func(context.Context, map[string]interface{}) (string, error){
		"write_file": func(context.Context, map[string]interface{}) (string, error) {
			calls++
			return `{"path":"skills/foo.md","written":true}`, nil
		},
	})
	client := &stubClient{streams: []model.Stream{
		newStubStream([]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "write_file", Arguments: `{"path":"skills/foo.md","mode":"overwrite","content":"one"}`}}}}}, model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "write_file", Arguments: `{"path":"skills/foo.md","mode":"overwrite","content":"one"}`}}}, nil),
		newStubStream([]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_2", Name: "write_file", Arguments: `{"path":"skills/foo.md","mode":"overwrite","content":"two"}`}}}}}, model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_2", Name: "write_file", Arguments: `{"path":"skills/foo.md","mode":"overwrite","content":"two"}`}}}, nil),
		newStubStream([]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_3", Name: "write_file", Arguments: `{"path":"skills/foo.md","mode":"overwrite","content":"three"}`}}}}}, model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_3", Name: "write_file", Arguments: `{"path":"skills/foo.md","mode":"overwrite","content":"three"}`}}}, nil),
	}}
	runner, err := NewRunner(client, registry, Options{Model: "test-model", RuntimePromptBuilder: runtimeprompt.NewBuilder(nil), MaxSteps: 9})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	result, err := runner.Run(context.Background(), RunInput{Messages: []model.Message{{Role: model.RoleUser, Content: "write it"}}, Tools: registry.List()})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if calls != 3 {
		t.Fatalf("write_file calls = %d, want third call executed then safe completion", calls)
	}
	if result.StopReason != "loop_guard_safe_completion" {
		t.Fatalf("StopReason = %q, want loop_guard_safe_completion", result.StopReason)
	}
	if result.FinalMessage.Role != model.RoleAssistant || !strings.Contains(result.FinalMessage.Content, "重复覆盖同一文件") {
		t.Fatalf("FinalMessage = %#v, want deterministic assistant safe-completion message", result.FinalMessage)
	}
	if result.StepsExecuted != 3 {
		t.Fatalf("StepsExecuted = %d, want 3", result.StepsExecuted)
	}
	if result.ToolCalls != 3 {
		t.Fatalf("ToolCalls = %d, want 3", result.ToolCalls)
	}
}

func TestRunnerLoopGuardStopsRepeatedToolErrorAsFailure(t *testing.T) {
	registry := newTestRegistry(t, map[string]func(context.Context, map[string]interface{}) (string, error){
		"list_files": func(context.Context, map[string]interface{}) (string, error) {
			return "", errors.New("path is required")
		},
	})
	client := &stubClient{streams: []model.Stream{
		newStubStream([]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "list_files", Arguments: `{}`}}}}}, model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "list_files", Arguments: `{}`}}}, nil),
		newStubStream([]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_2", Name: "list_files", Arguments: `{}`}}}}}, model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_2", Name: "list_files", Arguments: `{}`}}}, nil),
		newStubStream([]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_3", Name: "list_files", Arguments: `{}`}}}}}, model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_3", Name: "list_files", Arguments: `{}`}}}, nil),
	}}
	runner, err := NewRunner(client, registry, Options{Model: "test-model", RuntimePromptBuilder: runtimeprompt.NewBuilder(nil), MaxSteps: 9})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	result, err := runner.Run(context.Background(), RunInput{Messages: []model.Message{{Role: model.RoleUser, Content: "list"}}, Tools: registry.List()})
	if !errors.Is(err, ErrToolLoopDetected) {
		t.Fatalf("Run() error = %v, want ErrToolLoopDetected", err)
	}
	if result.StopReason != "loop_guard_failed_loop" {
		t.Fatalf("StopReason = %q, want loop_guard_failed_loop", result.StopReason)
	}
	if result.FinalMessage.Role != "" {
		t.Fatalf("FinalMessage = %#v, want empty on failed loop", result.FinalMessage)
	}
}
```

- [ ] **Step 2: Run the runner tests and verify RED**

Run:

```powershell
go test ./core/agent -run '^TestRunnerLoopGuard' -count=1
```

Expected: FAIL because Runner has not yet wired `LoopGuard`.

- [ ] **Step 3: Wire LoopGuard into `RunStream` and tool execution**

Implement these concrete changes:

- Instantiate `loopGuard := NewLoopGuard(r.options.LoopGuard)` near the top of the `RunStream` goroutine.
- Add `pendingLoopGuardWarnings []model.Message` in the goroutine.
- Before building a model request, append pending loop guard warnings to the body passed into `buildBudgetedRequestFromContext` or to `ephemeralConversationTail` for the memory path. The warning message must have `Role: model.RoleSystem` and must not be appended to `produced`.
- Change `executeAssistantToolCalls` to accept `loopGuard *LoopGuard` and `pendingWarnings *[]model.Message`.
- After each tool result, call `loopGuard.AfterToolResult(call, arguments, output.Content, err)`.
- If the decision is `warn`, append `model.Message{Role: model.RoleSystem, Content: result.WarningText}` to pending warnings and emit `loop.guard.warning`.
- If the decision is `stop` with `safe_completion`, return a typed internal result that lets `RunStream` set:

```go
result = RunResult{
	Messages: append(produced, model.Message{Role: model.RoleAssistant, Content: guardResult.FinalMessage}),
	FinalMessage: model.Message{Role: model.RoleAssistant, Content: guardResult.FinalMessage},
	StepsExecuted: step,
	ToolCalls: toolCalls,
	StopReason: "loop_guard_safe_completion",
	Usage: totalUsage,
	MemoryCompression: r.lastMemoryCompressionSnapshot(),
}
runErr = nil
```

- If the decision is `stop` with `failed_loop`, return `ErrToolLoopDetected` and set the snapshot `StopReason` to `loop_guard_failed_loop`.
- Do not modify `DefaultMaxSteps`, `Options.MaxSteps` defaulting, or the `for step := 1; step <= r.options.MaxSteps; step++` bound.

- [ ] **Step 4: Run runner loop guard tests and verify GREEN**

Run:

```powershell
go test ./core/agent -run '^TestRunnerLoopGuard' -count=1
```

Expected: PASS.

## Task 3: Audit Evidence

**Files:**
- Modify: `core/agent/runner_test.go`
- Modify: `core/agent/stream.go` or `core/agent/events.go`

- [ ] **Step 1: Write failing audit tests**

Add tests to `core/agent/runner_test.go`:

```go
func TestRunnerLoopGuardRecordsWarningAndStoppedAuditEvents(t *testing.T) {
	recorder := newRecordingRunnerAuditRecorder()
	registry := newTestRegistry(t, map[string]func(context.Context, map[string]interface{}) (string, error){
		"write_file": func(context.Context, map[string]interface{}) (string, error) {
			return `{"path":"skills/foo.md","written":true}`, nil
		},
	})
	client := &stubClient{streams: []model.Stream{
		newStubStream([]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "write_file", Arguments: `{"path":"skills/foo.md","mode":"overwrite","content":"one"}`}}}}}, model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "write_file", Arguments: `{"path":"skills/foo.md","mode":"overwrite","content":"one"}`}}}, nil),
		newStubStream([]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_2", Name: "write_file", Arguments: `{"path":"skills/foo.md","mode":"overwrite","content":"two"}`}}}}}, model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_2", Name: "write_file", Arguments: `{"path":"skills/foo.md","mode":"overwrite","content":"two"}`}}}, nil),
		newStubStream([]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_3", Name: "write_file", Arguments: `{"path":"skills/foo.md","mode":"overwrite","content":"three"}`}}}}}, model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_3", Name: "write_file", Arguments: `{"path":"skills/foo.md","mode":"overwrite","content":"three"}`}}}, nil),
	}}
	runner, err := NewRunner(client, registry, Options{
		Model:                "test-model",
		RuntimePromptBuilder: runtimeprompt.NewBuilder(nil),
		MaxSteps:             9,
		AuditRecorder:        recorder,
		AuditRunID:           "run_loop_guard_audit",
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if _, err := runner.Run(context.Background(), RunInput{Messages: []model.Message{{Role: model.RoleUser, Content: "write"}}, Tools: registry.List()}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	warning := recorder.requireEventForStep(t, "run_loop_guard_audit", "loop.guard.warning", 2)
	warningPayload := decodeAuditPayload(t, warning)
	if warningPayload["reason"] != LoopGuardReasonSameTargetWriteRepeated || warningPayload["target"] != "skills/foo.md" {
		t.Fatalf("warning payload = %#v, want reason and target", warningPayload)
	}
	if int(warningPayload["repeat_count"].(float64)) != 2 {
		t.Fatalf("warning repeat_count = %#v, want 2", warningPayload["repeat_count"])
	}
	stopped := recorder.requireEventForStep(t, "run_loop_guard_audit", "loop.guard.stopped", 3)
	stoppedPayload := decodeAuditPayload(t, stopped)
	if stoppedPayload["stop_strategy"] != LoopGuardStopStrategySafeCompletion {
		t.Fatalf("stopped payload = %#v, want safe completion", stoppedPayload)
	}
}
```

- [ ] **Step 2: Run the audit test and verify RED**

Run:

```powershell
go test ./core/agent -run '^TestRunnerLoopGuardRecordsWarningAndStoppedAuditEvents$' -count=1
```

Expected: FAIL until audit events are emitted with the required payload.

- [ ] **Step 3: Emit loop guard audit events**

Add an audit payload helper with this shape:

```go
func loopGuardAuditPayload(result LoopGuardResult) map[string]any {
	payload := map[string]any{
		"reason":       result.Reason,
		"tool_name":    result.ToolName,
		"target":       result.Target,
		"operation":    result.Operation,
		"repeat_count": result.RepeatCount,
		"soft_limit":   result.SoftLimit,
		"hard_limit":   result.HardLimit,
	}
	if result.StopStrategy != "" {
		payload["stop_strategy"] = result.StopStrategy
	}
	return payload
}
```

Emit:

```go
r.appendAuditEvent(ctx, step, coreaudit.PhaseTool, "loop.guard.warning", loopGuardAuditPayload(result), "")
r.appendAuditEvent(ctx, step, coreaudit.PhaseTool, "loop.guard.stopped", loopGuardAuditPayload(result), "")
```

- [ ] **Step 4: Run loop guard runner tests and verify GREEN**

Run:

```powershell
go test ./core/agent -run '^TestRunnerLoopGuard' -count=1
```

Expected: PASS.

## Task 4: Executor Persistence and Failure Semantics

**Files:**
- Modify: `core/agent/executor_test.go`
- Modify: `core/agent/stream.go` if needed
- Modify: `core/agent/executor.go` only if persisted safe completion or failed-loop behavior is wrong

- [ ] **Step 1: Write failing executor persistence tests**

Add tests to `core/agent/executor_test.go`:

```go
func TestAgentExecutorPersistsLoopGuardSafeCompletionMessage(t *testing.T) {
	store := newConversationStoreForTest(t)
	client := &stubClient{streams: []model.Stream{
		newStubStream([]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "write_file", Arguments: `{"path":"skills/foo.md","mode":"overwrite","content":"one"}`}}}}}, model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "write_file", Arguments: `{"path":"skills/foo.md","mode":"overwrite","content":"one"}`}}}, nil),
		newStubStream([]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_2", Name: "write_file", Arguments: `{"path":"skills/foo.md","mode":"overwrite","content":"two"}`}}}}}, model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_2", Name: "write_file", Arguments: `{"path":"skills/foo.md","mode":"overwrite","content":"two"}`}}}, nil),
		newStubStream([]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_3", Name: "write_file", Arguments: `{"path":"skills/foo.md","mode":"overwrite","content":"three"}`}}}}}, model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_3", Name: "write_file", Arguments: `{"path":"skills/foo.md","mode":"overwrite","content":"three"}`}}}, nil),
	}}
	registry := newTestRegistry(t, map[string]func(context.Context, map[string]interface{}) (string, error){
		"write_file": func(context.Context, map[string]interface{}) (string, error) {
			return `{"path":"skills/foo.md","written":true}`, nil
		},
	})
	executor := newTaskExecutorForTest(t, ExecutorDependencies{
		Resolver:          newExecutorResolverForTest(),
		ConversationStore: store,
		Registry:          registry,
		ClientFactory:     func(*coretypes.LLMProvider, *coretypes.LLMModel) (model.LlmClient, error) { return client, nil },
	})
	payload, _ := json.Marshal(RunTaskInput{ConversationID: "conv_loop_safe", ProviderID: "openai", ModelID: "gpt-5.4", Message: "write"})
	task := &coretasks.Task{ID: "task_loop_safe", TaskType: "agent.run", InputJSON: payload}
	output, err := executor(context.Background(), task, nil)
	if err != nil {
		t.Fatalf("executor() error = %v", err)
	}
	runResult := output.(RunTaskResult)
	if !strings.Contains(runResult.FinalMessage.Content, "重复覆盖同一文件") {
		t.Fatalf("FinalMessage = %#v, want loop guard safe completion", runResult.FinalMessage)
	}
	messages, err := store.ListMessages(context.Background(), "conv_loop_safe")
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(messages) != 8 {
		t.Fatalf("len(messages) = %d, want user + 3 assistant tool calls + 3 tool results + final assistant", len(messages))
	}
	last := messages[len(messages)-1]
	if last.Role != model.RoleAssistant || !strings.Contains(last.Content, "重复覆盖同一文件") {
		t.Fatalf("last message = %#v, want persisted loop guard assistant completion", last)
	}
}

func TestAgentExecutorTreatsLoopGuardFailedLoopAsFailure(t *testing.T) {
	store := newConversationStoreForTest(t)
	client := &stubClient{streams: []model.Stream{
		newStubStream([]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "list_files", Arguments: `{}`}}}}}, model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_1", Name: "list_files", Arguments: `{}`}}}, nil),
		newStubStream([]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_2", Name: "list_files", Arguments: `{}`}}}}}, model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_2", Name: "list_files", Arguments: `{}`}}}, nil),
		newStubStream([]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_3", Name: "list_files", Arguments: `{}`}}}}}, model.Message{Role: model.RoleAssistant, ToolCalls: []coretypes.ToolCall{{ID: "call_3", Name: "list_files", Arguments: `{}`}}}, nil),
	}}
	registry := newTestRegistry(t, map[string]func(context.Context, map[string]interface{}) (string, error){
		"list_files": func(context.Context, map[string]interface{}) (string, error) {
			return "", errors.New("path is required")
		},
	})
	executor := newTaskExecutorForTest(t, ExecutorDependencies{
		Resolver:          newExecutorResolverForTest(),
		ConversationStore: store,
		Registry:          registry,
		ClientFactory:     func(*coretypes.LLMProvider, *coretypes.LLMModel) (model.LlmClient, error) { return client, nil },
	})
	payload, _ := json.Marshal(RunTaskInput{ConversationID: "conv_loop_fail", ProviderID: "openai", ModelID: "gpt-5.4", Message: "list"})
	task := &coretasks.Task{ID: "task_loop_fail", TaskType: "agent.run", InputJSON: payload}
	_, err := executor(context.Background(), task, nil)
	if !errors.Is(err, ErrToolLoopDetected) {
		t.Fatalf("executor() error = %v, want ErrToolLoopDetected", err)
	}
	messages, err := store.ListMessages(context.Background(), "conv_loop_fail")
	if err != nil {
		t.Fatalf("ListMessages() error = %v", err)
	}
	if len(messages) == 0 || messages[len(messages)-1].Role != model.RoleSystem {
		t.Fatalf("messages = %#v, want visible failure system message at end", messages)
	}
}
```

- [ ] **Step 2: Run executor loop guard tests and verify RED**

Run:

```powershell
go test ./core/agent -run '^TestAgentExecutor.*LoopGuard' -count=1
```

Expected: FAIL until Runner result and executor persistence semantics are correct.

- [ ] **Step 3: Fix persistence semantics**

Expected likely implementation:

- Safe completion should already persist through normal success path if `RunResult.Messages` contains the deterministic assistant message.
- Failed loop should remain an error path so `executor.go` appends the visible failure system message and does not return `RunTaskResult`.
- If `trimIncompleteToolCallMessages` drops the third assistant message on failed-loop, keep that behavior; do not persist unresolved assistant tool call messages.

- [ ] **Step 4: Run executor loop guard tests and verify GREEN**

Run:

```powershell
go test ./core/agent -run '^TestAgentExecutor.*LoopGuard' -count=1
```

Expected: PASS.

## Task 5: Final Verification and Regression Guard

**Files:**
- Modify as needed: `core/agent/*`
- No generated Swagger or frontend files are expected.

- [ ] **Step 1: Run focused loop guard tests**

Run:

```powershell
go test ./core/agent -run 'LoopGuard|TestRunnerLoopGuard|TestAgentExecutor.*LoopGuard' -count=1
```

Expected: PASS.

- [ ] **Step 2: Run full core agent package**

Run:

```powershell
go test ./core/agent -count=1
```

Expected: PASS.

- [ ] **Step 3: Run required backend verification**

Run:

```powershell
go test ./...
go build ./cmd/...
go list ./...
```

Expected: all commands exit 0.

- [ ] **Step 4: Confirm MaxSteps was not changed**

Run:

```powershell
rg -n "DefaultMaxSteps|MaxSteps|loop_guard" core/agent
```

Expected:

- `DefaultMaxSteps = 128` remains unchanged.
- `NewRunner` still defaults `MaxSteps` only when `options.MaxSteps <= 0`.
- The main run loop still uses the configured `r.options.MaxSteps`.
- New loop guard stop reasons are separate from `max_steps_exceeded`.

## Self-Review Checklist

- Spec coverage:
  - Tool-call profiles: Task 1.
  - Soft warning: Task 2 and Task 3.
  - Hard stop: Task 2.
  - Per-tool rules: Task 1.
  - Audit events: Task 3.
  - Safe completion vs failed loop: Task 2 and Task 4.
  - No default step count change: Task 5.
- Placeholder scan: no unresolved placeholders or unspecified test-writing steps.
- Type consistency: use `LoopGuardOptions`, `LoopGuardResult`, `LoopGuardDecision`, `ErrToolLoopDetected`, and stop reason strings exactly as defined above.
