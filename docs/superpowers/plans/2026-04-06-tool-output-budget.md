# Tool Output Budget and Context Compression Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make tool outputs, replay payloads, and provider requests stay within bounded budgets by shaping every tool result, verifying and wiring real LLM compression, and adding a request-time send guard with deterministic trimming fallback.

**Architecture:** Keep the work in three focused layers. First, add shared output-budget helpers in `core/tools/builtin` and make each high-risk builtin return bounded, metadata-rich results. Second, extend `core/memory` so the runtime can use a real LLM-backed compressor, expose reserve-aware compression traces, and share token-counting helpers. Third, add a request-budget orchestrator in `core/agent` that estimates full request size, retries with compression, trims old low-priority content deterministically, emits audit/log evidence, and blocks the provider call if the request is still unsafe.

**Tech Stack:** Go, existing `core/tools`, `core/memory`, `core/agent`, `core/audit`, provider token counters in `core/providers/tools`, standard library file/HTTP/exec utilities

---

## File Structure

- Create: `core/tools/builtin/output_budget.go`
  - Shared builtin output-budget config, truncation metadata helpers, binary detection, and shaping helpers for text, command output, search results, and list entries.
- Modify: `core/tools/builtin/options.go`
  - Add normalized builtin output-budget options to `Options` / `runtimeEnv`.
- Modify: `core/tools/builtin/read_file.go`
  - Enforce text-only reads, default-to-300-lines behavior, and continuation metadata.
- Modify: `core/tools/builtin/exec_command.go`
  - Bound stdout/stderr and return truncation metadata.
- Modify: `core/tools/builtin/http_request.go`
  - Bound response body length and surface body truncation metadata.
- Modify: `core/tools/builtin/web_search.go`
  - Bound snippets/result count and mark truncated responses.
- Modify: `core/tools/builtin/search_file.go`
  - Cap returned matches and per-match text length.
- Modify: `core/tools/builtin/grep_file.go`
  - Cap per-file match output.
- Modify: `core/tools/builtin/list_files.go`
  - Cap returned entries and surface remaining counts.
- Modify: `core/tools/builtin/register_test.go`
  - Add regression coverage for read-file defaults, binary rejection, and bounded outputs across builtins.
- Create: `core/memory/token_counter.go`
  - Shared exported helper for creating token counters from `*coretypes.LLMModel` so `core/memory` and `core/agent` use the same counting behavior.
- Create: `core/memory/message_budget.go`
  - Shared exported helpers for estimating message payloads and rendering compact message summaries used by compression/trimming.
- Modify: `core/memory/manager.go`
  - Add reserve-aware runtime-context compression, compression trace output, and stricter validation for over-budget state.
- Modify: `core/memory/llm_compressor.go`
  - Reject empty LLM summaries so compression failures are explicit.
- Modify: `core/memory/manager_test.go`
  - Add reserve-triggered compression and fallback behavior tests.
- Modify: `core/memory/llm_compressor_test.go`
  - Add tests proving the LLM compressor returns non-empty summaries and preserves prompt construction.
- Modify: `core/agent/types.go`
  - Add request-budget dependencies to `Options` (token counter / budget config hook) without breaking existing callers.
- Modify: `core/agent/executor.go`
  - Wire the real LLM short-term compressor into the default memory manager and pass the selected model/client into budget-aware runner setup.
- Modify: `core/agent/memory.go`
  - Use the reserve-aware memory runtime-context method so request budgeting can trigger compression before provider send.
- Create: `core/agent/request_budget.go`
  - Request token estimation, compression retry, deterministic trim order, structured budget decisions, and final guard error.
- Modify: `core/agent/stream.go`
  - Replace direct `buildRequestMessages` usage with the budgeted request builder and emit audit/log data.
- Modify: `core/agent/events.go`
  - Attach budget-decision artifacts and request-budget audit events.
- Modify: `core/audit/types.go`
  - Add an artifact kind for budget-decision payloads.
- Create: `core/agent/request_budget_test.go`
  - Focused tests for direct fit, compression retry, deterministic trim fallback, and final guard failure.
- Modify: `core/agent/memory_test.go`
  - Prove reserve-based compression is triggered from runner codepaths.
- Modify: `core/agent/stream_test.go`
  - Prove provider send is skipped when the final guard fails and audit metadata is recorded when compression/trim succeed.

### Task 1: Add shared builtin output-budget primitives and fix `read_file`

**Files:**
- Create: `core/tools/builtin/output_budget.go`
- Modify: `core/tools/builtin/options.go`
- Modify: `core/tools/builtin/read_file.go`
- Test: `core/tools/builtin/register_test.go`

- [ ] **Step 1: Write the failing tests for default line window and binary rejection**

Add these tests to `core/tools/builtin/register_test.go` near the existing `TestReadFileReturnsLineWindow` test:

```go
func TestReadFileDefaultsToFirst300LinesAndAdvertisesContinuation(t *testing.T) {
	workspace := t.TempDir()
	lines := make([]string, 320)
	for i := range lines {
		lines[i] = fmt.Sprintf("line-%03d", i+1)
	}
	mustWriteFile(t, filepath.Join(workspace, "huge.txt"), strings.Join(lines, "\n")+"\n")

	registry := newBuiltinRegistry(t, Options{WorkspaceRoot: workspace})
	raw, err := registry.Execute(context.Background(), "read_file", map[string]any{"path": "huge.txt"})
	if err != nil {
		t.Fatalf("Execute(read_file) error = %v", err)
	}

	var result struct {
		Path          string `json:"path"`
		StartLine     int    `json:"start_line"`
		EndLine       int    `json:"end_line"`
		TotalLines    int    `json:"total_lines"`
		HasMore       bool   `json:"has_more"`
		NextStartLine int    `json:"next_start_line"`
		Truncated     bool   `json:"truncated"`
		Content       string `json:"content"`
	}
	decodeJSON(t, raw, &result)

	if result.StartLine != 1 || result.EndLine != 300 || result.TotalLines != 320 {
		t.Fatalf("read_file window = %#v, want first 300 of 320 lines", result)
	}
	if !result.HasMore || result.NextStartLine != 301 || !result.Truncated {
		t.Fatalf("continuation metadata = %#v, want has_more=true next_start_line=301 truncated=true", result)
	}
	if strings.Contains(result.Content, "line-320") {
		t.Fatalf("content unexpectedly contains line beyond default window: %q", result.Content)
	}
}

func TestReadFileRejectsBinaryFiles(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "blob.bin"), []byte{0x00, 0x01, 0x02, 0x03}, 0o644); err != nil {
		t.Fatalf("WriteFile(binary) error = %v", err)
	}

	registry := newBuiltinRegistry(t, Options{WorkspaceRoot: workspace})
	_, err := registry.Execute(context.Background(), "read_file", map[string]any{"path": "blob.bin"})
	if err == nil {
		t.Fatal("Execute(read_file binary) error = nil, want non-nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "binary") {
		t.Fatalf("Execute(read_file binary) error = %q, want binary message", err)
	}
}
```

- [ ] **Step 2: Run the read-file tests to verify they fail**

Run: `go test ./core/tools/builtin -run 'TestReadFileReturnsLineWindow|TestReadFileDefaultsToFirst300LinesAndAdvertisesContinuation|TestReadFileRejectsBinaryFiles'`

Expected: FAIL because `read_file` currently returns the whole file when `line_count` is omitted and does not reject binary payloads.

- [ ] **Step 3: Add shared builtin output-budget config and helpers**

Create `core/tools/builtin/output_budget.go` with the shared config and helpers:

```go
package builtin

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	defaultReadDefaultLines    = 300
	defaultReadMaxLines        = 300
	defaultTextResultMaxBytes  = 16 * 1024
	defaultCommandStdoutBytes  = 16 * 1024
	defaultCommandStderrBytes  = 8 * 1024
	defaultSearchMaxMatches    = 100
	defaultListMaxEntries      = 200
	defaultWebSearchMaxResults = 10
	defaultMatchTextMaxBytes   = 256
)

type outputBudgetConfig struct {
	ReadDefaultLines   int
	ReadMaxLines       int
	TextResultMaxBytes int
	CommandStdoutBytes int
	CommandStderrBytes int
	SearchMaxMatches   int
	ListMaxEntries     int
	WebSearchMaxResults int
	MatchTextMaxBytes  int
}

type truncationMeta struct {
	Truncated    bool   `json:"truncated"`
	LimitReason  string `json:"limit_reason,omitempty"`
	OriginalSize int    `json:"original_size,omitempty"`
	ReturnedSize int    `json:"returned_size,omitempty"`
}

func normalizeOutputBudget(config outputBudgetConfig) outputBudgetConfig {
	if config.ReadDefaultLines <= 0 {
		config.ReadDefaultLines = defaultReadDefaultLines
	}
	if config.ReadMaxLines <= 0 {
		config.ReadMaxLines = defaultReadMaxLines
	}
	if config.TextResultMaxBytes <= 0 {
		config.TextResultMaxBytes = defaultTextResultMaxBytes
	}
	if config.CommandStdoutBytes <= 0 {
		config.CommandStdoutBytes = defaultCommandStdoutBytes
	}
	if config.CommandStderrBytes <= 0 {
		config.CommandStderrBytes = defaultCommandStderrBytes
	}
	if config.SearchMaxMatches <= 0 {
		config.SearchMaxMatches = defaultSearchMaxMatches
	}
	if config.ListMaxEntries <= 0 {
		config.ListMaxEntries = defaultListMaxEntries
	}
	if config.WebSearchMaxResults <= 0 {
		config.WebSearchMaxResults = defaultWebSearchMaxResults
	}
	if config.MatchTextMaxBytes <= 0 {
		config.MatchTextMaxBytes = defaultMatchTextMaxBytes
	}
	return config
}

func isProbablyBinary(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return true
	}
	return !utf8.Valid(data)
}

func limitStringBytes(text string, max int) (string, truncationMeta) {
	if max <= 0 || len(text) <= max {
		return text, truncationMeta{OriginalSize: len(text), ReturnedSize: len(text)}
	}
	trimmed := text[:max]
	for !utf8.ValidString(trimmed) && len(trimmed) > 0 {
		trimmed = trimmed[:len(trimmed)-1]
	}
	return trimmed, truncationMeta{
		Truncated:    true,
		LimitReason:  "byte_limit",
		OriginalSize: len(text),
		ReturnedSize: len(trimmed),
	}
}

func limitLineCount(lines []string, maxLines int) ([]string, bool) {
	if maxLines <= 0 || len(lines) <= maxLines {
		return lines, false
	}
	return append([]string(nil), lines[:maxLines]...), true
}

func truncateMatchText(text string, maxBytes int) string {
	trimmed, _ := limitStringBytes(strings.TrimSpace(text), maxBytes)
	return trimmed
}

func binaryReadError(path string) error {
	return fmt.Errorf("binary file is not readable as text: %s", path)
}
```

Then update `core/tools/builtin/options.go` so the environment always carries normalized limits:

```go
type OutputBudgetOptions struct {
	ReadDefaultLines    int
	ReadMaxLines        int
	TextResultMaxBytes  int
	CommandStdoutBytes  int
	CommandStderrBytes  int
	SearchMaxMatches    int
	ListMaxEntries      int
	WebSearchMaxResults int
	MatchTextMaxBytes   int
}

type Options struct {
	WorkspaceRoot  string
	CommandTimeout time.Duration
	HTTPClient     *http.Client
	WebSearch      WebSearchOptions
	OutputBudget   OutputBudgetOptions
}

type runtimeEnv struct {
	workspaceRoot  string
	commandTimeout time.Duration
	httpClient     *http.Client
	webSearch      WebSearchOptions
	outputBudget   outputBudgetConfig
}

return runtimeEnv{
	workspaceRoot:  root,
	commandTimeout: clampDuration(timeout, minCommandTimeout, maxCommandTimeout),
	httpClient:     client,
	webSearch:      options.WebSearch,
	outputBudget: normalizeOutputBudget(outputBudgetConfig{
		ReadDefaultLines:    options.OutputBudget.ReadDefaultLines,
		ReadMaxLines:        options.OutputBudget.ReadMaxLines,
		TextResultMaxBytes:  options.OutputBudget.TextResultMaxBytes,
		CommandStdoutBytes:  options.OutputBudget.CommandStdoutBytes,
		CommandStderrBytes:  options.OutputBudget.CommandStderrBytes,
		SearchMaxMatches:    options.OutputBudget.SearchMaxMatches,
		ListMaxEntries:      options.OutputBudget.ListMaxEntries,
		WebSearchMaxResults: options.OutputBudget.WebSearchMaxResults,
		MatchTextMaxBytes:   options.OutputBudget.MatchTextMaxBytes,
	}),
}, nil
```

- [ ] **Step 4: Update `read_file` to use the shared budget rules**

Replace the handler body in `core/tools/builtin/read_file.go` with the bounded version:

```go
Handler: func(ctx context.Context, arguments map[string]any) (string, error) {
	pathArg, err := requiredStringArg(arguments, "path")
	if err != nil {
		return "", err
	}
	startLine, err := intArg(arguments, "start_line", 1)
	if err != nil {
		return "", err
	}
	lineCount, hasLineCount, err := optionalIntArg(arguments, "line_count")
	if err != nil {
		return "", err
	}
	if startLine < 1 {
		return "", fmt.Errorf("start_line must be >= 1")
	}
	if !hasLineCount {
		lineCount = env.outputBudget.ReadDefaultLines
	}
	if lineCount < 0 {
		return "", fmt.Errorf("line_count must be >= 0")
	}
	if lineCount > env.outputBudget.ReadMaxLines {
		lineCount = env.outputBudget.ReadMaxLines
	}

	startedAt := time.Now()
	logToolStart(ctx, "read_file", corelog.String("path", pathArg), corelog.Int("start_line", startLine), corelog.Int("line_count", lineCount))
	filePath, relPath, err := env.resolveWorkspaceFile(pathArg, true)
	if err != nil {
		logToolFailure(ctx, "read_file", err, corelog.String("path", pathArg))
		return "", err
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		logToolFailure(ctx, "read_file", err, corelog.String("path", relPath))
		return "", err
	}
	if isProbablyBinary(data) {
		err := binaryReadError(relPath)
		logToolFailure(ctx, "read_file", err, corelog.String("path", relPath))
		return "", err
	}

	lines := splitLinesWithEndings(string(data))
	totalLines := len(lines)
	startIndex := startLine - 1
	if startIndex > totalLines {
		startIndex = totalLines
	}
	endIndex := totalLines
	if lineCount > 0 && startIndex+lineCount < endIndex {
		endIndex = startIndex + lineCount
	}
	window := lines[startIndex:endIndex]
	content := joinLines(window)
	truncated := endIndex < totalLines
	meta := truncationMeta{Truncated: truncated, OriginalSize: len(data), ReturnedSize: len(content)}
	if truncated {
		meta.LimitReason = "line_limit"
	}

	logToolFinish(ctx, "read_file", corelog.String("path", relPath), corelog.Int("total_lines", totalLines), corelog.Int("content_length", len(content)), corelog.Bool("truncated", truncated), corelog.Duration("duration", time.Since(startedAt)))
	return jsonResult(struct {
		Path          string `json:"path"`
		StartLine     int    `json:"start_line"`
		EndLine       int    `json:"end_line"`
		TotalLines    int    `json:"total_lines"`
		HasMore       bool   `json:"has_more"`
		NextStartLine int    `json:"next_start_line,omitempty"`
		Truncated     bool   `json:"truncated"`
		LimitReason   string `json:"limit_reason,omitempty"`
		Content       string `json:"content"`
	}{
		Path:          relPath,
		StartLine:     startLine,
		EndLine:       endIndex,
		TotalLines:    totalLines,
		HasMore:       truncated,
		NextStartLine: endIndex + 1,
		Truncated:     meta.Truncated,
		LimitReason:   meta.LimitReason,
		Content:       content,
	})
},
```

Add the missing helper to `core/tools/builtin/arg_helpers.go`:

```go
func optionalIntArg(arguments map[string]any, key string) (int, bool, error) {
	value, ok := arguments[key]
	if !ok || value == nil {
		return 0, false, nil
	}
	parsed, err := intArg(arguments, key, 0)
	if err != nil {
		return 0, false, err
	}
	return parsed, true, nil
}
```

- [ ] **Step 5: Run the read-file tests to verify they pass**

Run: `go test ./core/tools/builtin -run 'TestReadFileReturnsLineWindow|TestReadFileDefaultsToFirst300LinesAndAdvertisesContinuation|TestReadFileRejectsBinaryFiles'`

Expected: PASS

- [ ] **Step 6: Commit the read-file budget slice**

Run:

```bash
git add core/tools/builtin/output_budget.go core/tools/builtin/options.go core/tools/builtin/arg_helpers.go core/tools/builtin/read_file.go core/tools/builtin/register_test.go
git commit -m "feat(builtin): bound read_file output"
```

Expected: commit succeeds with the new read-file defaults and shared budget helpers.

### Task 2: Apply bounded output shaping to the remaining high-risk builtin tools

**Files:**
- Modify: `core/tools/builtin/exec_command.go`
- Modify: `core/tools/builtin/http_request.go`
- Modify: `core/tools/builtin/web_search.go`
- Modify: `core/tools/builtin/search_file.go`
- Modify: `core/tools/builtin/grep_file.go`
- Modify: `core/tools/builtin/list_files.go`
- Modify: `core/tools/builtin/output_budget.go`
- Test: `core/tools/builtin/register_test.go`

- [ ] **Step 1: Write the failing truncation tests for command, HTTP, search, and list outputs**

Add these tests to `core/tools/builtin/register_test.go`:

```go
func TestExecCommandTruncatesLargeStdout(t *testing.T) {
	registry := newBuiltinRegistry(t, Options{WorkspaceRoot: t.TempDir(), OutputBudget: OutputBudgetOptions{CommandStdoutBytes: 32}})
	raw, err := registry.Execute(context.Background(), "exec_command", map[string]any{
		"command": "python",
		"args":    []any{"-c", "print('x' * 200)"},
	})
	if err != nil {
		t.Fatalf("Execute(exec_command) error = %v", err)
	}

	var result struct {
		Stdout           string `json:"stdout"`
		StdoutTruncated  bool   `json:"stdout_truncated"`
		OriginalStdout   int    `json:"original_stdout_bytes"`
		ReturnedStdout   int    `json:"returned_stdout_bytes"`
	}
	decodeJSON(t, raw, &result)

	if !result.StdoutTruncated {
		t.Fatalf("stdout truncation metadata = %#v, want truncated output", result)
	}
	if result.OriginalStdout <= result.ReturnedStdout {
		t.Fatalf("stdout byte counters = %#v, want original > returned", result)
	}
}

func TestHTTPRequestTruncatesLargeBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("abcdef", 100))
	}))
	defer server.Close()

	registry := newBuiltinRegistry(t, Options{
		WorkspaceRoot: t.TempDir(),
		OutputBudget:  OutputBudgetOptions{TextResultMaxBytes: 40},
	})
	raw, err := registry.Execute(context.Background(), "http_request", map[string]any{"url": server.URL})
	if err != nil {
		t.Fatalf("Execute(http_request) error = %v", err)
	}

	var result struct {
		Body          string `json:"body"`
		Truncated     bool   `json:"truncated"`
		LimitReason   string `json:"limit_reason"`
		OriginalSize  int    `json:"original_size"`
		ReturnedSize  int    `json:"returned_size"`
	}
	decodeJSON(t, raw, &result)

	if !result.Truncated || result.LimitReason != "byte_limit" {
		t.Fatalf("http truncation metadata = %#v, want byte_limit truncation", result)
	}
	if result.OriginalSize <= result.ReturnedSize {
		t.Fatalf("http size counters = %#v, want original > returned", result)
	}
}

func TestSearchFileLimitsMatchesAndReportsTotals(t *testing.T) {
	workspace := t.TempDir()
	contents := strings.Repeat("needle\n", 20)
	mustWriteFile(t, filepath.Join(workspace, "matches.txt"), contents)

	registry := newBuiltinRegistry(t, Options{WorkspaceRoot: workspace, OutputBudget: OutputBudgetOptions{SearchMaxMatches: 5, MatchTextMaxBytes: 8}})
	raw, err := registry.Execute(context.Background(), "search_file", map[string]any{"path": ".", "pattern": "needle"})
	if err != nil {
		t.Fatalf("Execute(search_file) error = %v", err)
	}

	var result struct {
		Matches        []struct{ Text string `json:"text"` } `json:"matches"`
		TotalMatches   int  `json:"total_matches"`
		ReturnedMatches int `json:"returned_matches"`
		Truncated      bool `json:"truncated"`
	}
	decodeJSON(t, raw, &result)

	if len(result.Matches) != 5 || result.TotalMatches != 20 || result.ReturnedMatches != 5 || !result.Truncated {
		t.Fatalf("search result = %#v, want 5 returned matches out of 20", result)
	}
}

func TestListFilesLimitsEntriesAndReportsRemainingCount(t *testing.T) {
	workspace := t.TempDir()
	for i := 0; i < 6; i++ {
		mustWriteFile(t, filepath.Join(workspace, fmt.Sprintf("file-%d.txt", i)), "x")
	}

	registry := newBuiltinRegistry(t, Options{WorkspaceRoot: workspace, OutputBudget: OutputBudgetOptions{ListMaxEntries: 3}})
	raw, err := registry.Execute(context.Background(), "list_files", map[string]any{"path": ".", "recursive": false})
	if err != nil {
		t.Fatalf("Execute(list_files) error = %v", err)
	}

	var result struct {
		Entries         []struct{ Path string `json:"path"` } `json:"entries"`
		ReturnedEntries int  `json:"returned_entries"`
		RemainingCount  int  `json:"remaining_count"`
		Truncated       bool `json:"truncated"`
	}
	decodeJSON(t, raw, &result)

	if len(result.Entries) != 3 || result.ReturnedEntries != 3 || result.RemainingCount != 3 || !result.Truncated {
		t.Fatalf("list_files result = %#v, want 3 returned entries and 3 remaining", result)
	}
}
```

- [ ] **Step 2: Run the builtin truncation tests to verify they fail**

Run: `go test ./core/tools/builtin -run 'TestExecCommandTruncatesLargeStdout|TestHTTPRequestTruncatesLargeBody|TestSearchFileLimitsMatchesAndReportsTotals|TestListFilesLimitsEntriesAndReportsRemainingCount'`

Expected: FAIL because the current tool handlers return unbounded stdout/body/matches/entries and do not expose truncation metadata.

- [ ] **Step 3: Add shared shaping helpers and wire them into each builtin**

Extend `core/tools/builtin/output_budget.go` with the reusable result shapers:

```go
type boundedCommandOutput struct {
	Stdout              string `json:"stdout"`
	Stderr              string `json:"stderr"`
	StdoutTruncated     bool   `json:"stdout_truncated"`
	StderrTruncated     bool   `json:"stderr_truncated"`
	OriginalStdoutBytes int    `json:"original_stdout_bytes"`
	OriginalStderrBytes int    `json:"original_stderr_bytes"`
	ReturnedStdoutBytes int    `json:"returned_stdout_bytes"`
	ReturnedStderrBytes int    `json:"returned_stderr_bytes"`
}

func shapeCommandOutput(stdout, stderr string, config outputBudgetConfig) boundedCommandOutput {
	stdoutText, stdoutMeta := limitStringBytes(stdout, config.CommandStdoutBytes)
	stderrText, stderrMeta := limitStringBytes(stderr, config.CommandStderrBytes)
	return boundedCommandOutput{
		Stdout:              stdoutText,
		Stderr:              stderrText,
		StdoutTruncated:     stdoutMeta.Truncated,
		StderrTruncated:     stderrMeta.Truncated,
		OriginalStdoutBytes: stdoutMeta.OriginalSize,
		OriginalStderrBytes: stderrMeta.OriginalSize,
		ReturnedStdoutBytes: stdoutMeta.ReturnedSize,
		ReturnedStderrBytes: stderrMeta.ReturnedSize,
	}
}

func trimMatches[T any](items []T, max int) ([]T, int, bool) {
	if max <= 0 || len(items) <= max {
		return items, len(items), false
	}
	return append([]T(nil), items[:max]...), max, true
}
```

Patch `core/tools/builtin/exec_command.go` so the result body uses the shaped stdout/stderr values:

```go
shaped := shapeCommandOutput(stdout.String(), stderr.String(), env.outputBudget)
result := struct {
	Success             bool   `json:"success"`
	ExitCode            int    `json:"exit_code"`
	Stdout              string `json:"stdout"`
	Stderr              string `json:"stderr"`
	TimedOut            bool   `json:"timed_out"`
	Cwd                 string `json:"cwd"`
	StdoutTruncated     bool   `json:"stdout_truncated"`
	StderrTruncated     bool   `json:"stderr_truncated"`
	OriginalStdoutBytes int    `json:"original_stdout_bytes"`
	OriginalStderrBytes int    `json:"original_stderr_bytes"`
	ReturnedStdoutBytes int    `json:"returned_stdout_bytes"`
	ReturnedStderrBytes int    `json:"returned_stderr_bytes"`
}{
	Success:             runErr == nil,
	ExitCode:            0,
	Stdout:              shaped.Stdout,
	Stderr:              shaped.Stderr,
	TimedOut:            errors.Is(commandCtx.Err(), context.DeadlineExceeded),
	Cwd:                 cwdValue,
	StdoutTruncated:     shaped.StdoutTruncated,
	StderrTruncated:     shaped.StderrTruncated,
	OriginalStdoutBytes: shaped.OriginalStdoutBytes,
	OriginalStderrBytes: shaped.OriginalStderrBytes,
	ReturnedStdoutBytes: shaped.ReturnedStdoutBytes,
	ReturnedStderrBytes: shaped.ReturnedStderrBytes,
}
```

Patch `core/tools/builtin/http_request.go` to shape large bodies instead of returning the full response:

```go
bodyText, meta := limitStringBytes(string(responseBody), env.outputBudget.TextResultMaxBytes)
return jsonResult(struct {
	StatusCode  int    `json:"status_code"`
	Body        string `json:"body"`
	URL         string `json:"url,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	Truncated   bool   `json:"truncated"`
	LimitReason string `json:"limit_reason,omitempty"`
	OriginalSize int   `json:"original_size,omitempty"`
	ReturnedSize int   `json:"returned_size,omitempty"`
}{
	StatusCode:   response.StatusCode,
	Body:         bodyText,
	URL:          response.Request.URL.String(),
	ContentType:  response.Header.Get("Content-Type"),
	Truncated:    meta.Truncated,
	LimitReason:  meta.LimitReason,
	OriginalSize: meta.OriginalSize,
	ReturnedSize: meta.ReturnedSize,
})
```

Patch `core/tools/builtin/search_file.go`, `grep_file.go`, and `list_files.go` with the same pattern: truncate text per item, cap the returned slice with `trimMatches`, and add `total_*`, `returned_*`, `remaining_count`, and `truncated` metadata.

Patch `core/tools/builtin/web_search.go` so it caps both provider-requested and returned result counts:

```go
if maxResults <= 0 || maxResults > env.outputBudget.WebSearchMaxResults {
	maxResults = env.outputBudget.WebSearchMaxResults
}
results, err := provider.Search(ctx, query, maxResults)
if err != nil {
	...
}
trimmed, returned, truncated := trimMatches(results, env.outputBudget.WebSearchMaxResults)
for i := range trimmed {
	trimmed[i].Snippet = truncateMatchText(trimmed[i].Snippet, env.outputBudget.MatchTextMaxBytes)
}
return jsonResult(struct {
	Provider       string            `json:"provider"`
	Results        []webSearchResult `json:"results"`
	ReturnedResults int              `json:"returned_results"`
	Truncated      bool              `json:"truncated"`
}{Provider: resolvedName, Results: trimmed, ReturnedResults: returned, Truncated: truncated})
```

- [ ] **Step 4: Run the focused builtin tests to verify they pass**

Run: `go test ./core/tools/builtin -run 'TestExecCommandTruncatesLargeStdout|TestHTTPRequestTruncatesLargeBody|TestSearchFileLimitsMatchesAndReportsTotals|TestListFilesLimitsEntriesAndReportsRemainingCount'`

Expected: PASS

- [ ] **Step 5: Run the full builtin package tests**

Run: `go test ./core/tools/builtin`

Expected: PASS

- [ ] **Step 6: Commit the bounded builtin output changes**

Run:

```bash
git add core/tools/builtin/output_budget.go core/tools/builtin/exec_command.go core/tools/builtin/http_request.go core/tools/builtin/web_search.go core/tools/builtin/search_file.go core/tools/builtin/grep_file.go core/tools/builtin/list_files.go core/tools/builtin/register_test.go
git commit -m "feat(builtin): bound high-risk tool outputs"
```

Expected: commit succeeds with truncation metadata and bounded outputs across the builtin surface.

### Task 3: Wire real LLM compression and add reserve-aware memory budgeting

**Files:**
- Create: `core/memory/token_counter.go`
- Create: `core/memory/message_budget.go`
- Modify: `core/memory/manager.go`
- Modify: `core/memory/llm_compressor.go`
- Modify: `core/memory/manager_test.go`
- Modify: `core/memory/llm_compressor_test.go`
- Modify: `core/agent/executor.go`

- [ ] **Step 1: Write the failing reserve-aware compression and LLM-compressor tests**

Add these tests to `core/memory/manager_test.go` and `core/memory/llm_compressor_test.go`:

```go
func TestRuntimeContextWithReserveTriggersCompressionWhenPromptOverheadPushesRequestOverBudget(t *testing.T) {
	compressCalls := 0
	mgr, err := NewManager(Options{
		MaxContextTokens: 100,
		Counter:          fakeTokenCounter{},
		Compressor: func(_ context.Context, request CompressionRequest) (string, error) {
			compressCalls++
			return "compressed memory", nil
		},
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	mgr.AddMessages([]model.Message{{Role: model.RoleUser, Content: strings.Repeat("a", 30)}, {Role: model.RoleAssistant, Content: strings.Repeat("b", 30)}})

	state, trace, err := mgr.RuntimeContextWithReserve(context.Background(), 50)
	if err != nil {
		t.Fatalf("RuntimeContextWithReserve() error = %v", err)
	}
	if compressCalls != 1 || !trace.Attempted || !trace.Succeeded {
		t.Fatalf("compression trace = %#v, want one successful reserve-triggered compression", trace)
	}
	if state.Summary == nil || !strings.Contains(state.Summary.Content, "compressed memory") {
		t.Fatalf("summary = %#v, want rendered compressed summary", state.Summary)
	}
}

func TestLLMShortTermCompressorRejectsEmptySummary(t *testing.T) {
	client := &fakeChatClient{response: model.ChatResponse{Message: model.Message{Role: model.RoleAssistant, Content: "   "}}}
	compressor := NewLLMShortTermCompressor(LLMCompressorOptions{Client: client, Model: "gpt-test"})

	_, err := compressor(context.Background(), CompressionRequest{Instruction: "compress", Messages: []model.Message{{Role: model.RoleUser, Content: "hello"}}})
	if err == nil {
		t.Fatal("compressor() error = nil, want empty-summary error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "empty") {
		t.Fatalf("compressor() error = %q, want empty summary message", err)
	}
}
```

Add this executor regression to `core/agent/memory_test.go`:

```go
func TestBuildMemoryManagerUsesLLMShortTermCompressorByDefault(t *testing.T) {
	client := &stubClient{}
	llmModel := &coretypes.LLMModel{BaseModel: coretypes.BaseModel{ID: "gpt-test", Name: "gpt-test"}}

	mgr, err := buildMemoryManager(nil, client, llmModel)
	if err != nil {
		t.Fatalf("buildMemoryManager() error = %v", err)
	}
	if mgr == nil {
		t.Fatal("buildMemoryManager() = nil, want manager")
	}
}
```

- [ ] **Step 2: Run the focused memory tests to verify they fail**

Run: `go test ./core/memory -run 'TestRuntimeContextWithReserveTriggersCompressionWhenPromptOverheadPushesRequestOverBudget|TestLLMShortTermCompressorRejectsEmptySummary' && go test ./core/agent -run 'TestBuildMemoryManagerUsesLLMShortTermCompressorByDefault'`

Expected: FAIL because `RuntimeContextWithReserve` does not exist, blank LLM summaries are currently accepted, and `buildMemoryManager` does not wire an LLM compressor.

- [ ] **Step 3: Extract shared token/message budget helpers and add reserve-aware runtime context**

Create `core/memory/token_counter.go`:

```go
package memory

import (
	"strings"

	providertools "github.com/EquentR/agent_runtime/core/providers/tools"
	coretypes "github.com/EquentR/agent_runtime/core/types"
)

func NewTokenCounterForModel(llmModel *coretypes.LLMModel) TokenCounter {
	if llmModel != nil {
		modelName := strings.TrimSpace(llmModel.ModelName())
		if modelName == "" {
			modelName = strings.TrimSpace(llmModel.ModelID())
		}
		if modelName != "" {
			counter, err := providertools.NewTokenCounter(providertools.CountModeTokenizer, modelName)
			if err == nil {
				return counter
			}
		}
	}
	counter, err := providertools.NewCl100kTokenCounter()
	if err == nil {
		return counter
	}
	counter, err = providertools.NewTokenCounter(providertools.CountModeRune, "")
	if err == nil {
		return counter
	}
	return nil
}
```

Create `core/memory/message_budget.go` and move the message payload helpers there so both `manager.go` and `core/agent/request_budget.go` can reuse them:

```go
package memory

import (
	"strings"

	model "github.com/EquentR/agent_runtime/core/providers/types"
	coretypes "github.com/EquentR/agent_runtime/core/types"
)

func BudgetPayloadForMessage(message model.Message) string {
	return budgetPayloadForMessage(message)
}

func SummarizeMessageForBudget(message model.Message) string {
	return summarizeMessage(message)
}

func CountMessageTokens(counter TokenCounter, messages []model.Message) int64 {
	if counter == nil || len(messages) == 0 {
		return 0
	}
	payloads := make([]string, 0, len(messages))
	for _, message := range messages {
		if payload := BudgetPayloadForMessage(message); payload != "" {
			payloads = append(payloads, payload)
		}
	}
	if len(payloads) == 0 {
		return 0
	}
	return int64(counter.CountMessages(payloads))
}

func CountRuntimeContextTokens(counter TokenCounter, state RuntimeContext) int64 {
	messages := make([]model.Message, 0, len(state.Body)+1)
	if state.Summary != nil {
		messages = append(messages, cloneMessage(*state.Summary))
	}
	messages = append(messages, cloneMessages(state.Body)...)
	return CountMessageTokens(counter, messages)
}
```

Then extend `core/memory/manager.go` with a reserve-aware API:

```go
type CompressionTrace struct {
	Attempted    bool   `json:"attempted"`
	Succeeded    bool   `json:"succeeded"`
	Status       string `json:"status,omitempty"`
	TokensBefore int64  `json:"tokens_before,omitempty"`
	TokensAfter  int64  `json:"tokens_after,omitempty"`
}

func (m *Manager) RuntimeContextWithReserve(ctx context.Context, reserveTokens int64) (RuntimeContext, CompressionTrace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	trace := CompressionTrace{TokensBefore: m.estimateShortTermTokensLocked()}
	if m.requiresCompressionLocked(reserveTokens) {
		trace.Attempted = true
		if err := m.compressLocked(ctx); err != nil {
			trace.Status = "failed"
			return RuntimeContext{}, trace, err
		}
		trace.Succeeded = true
		trace.Status = "success"
	}
	if err := m.validateContextBudgetLocked(reserveTokens); err != nil {
		if trace.Status == "" {
			trace.Status = "over_budget"
		}
		return RuntimeContext{}, trace, err
	}
	trace.TokensAfter = m.estimateShortTermTokensLocked()
	return m.runtimeContextLocked(), trace, nil
}

func (m *Manager) RuntimeContext(ctx context.Context) (RuntimeContext, error) {
	state, _, err := m.RuntimeContextWithReserve(ctx, 0)
	return state, err
}
```

Update the internal checks so reserve tokens are included:

```go
func (m *Manager) requiresCompressionLocked(reserveTokens int64) bool {
	if len(m.shortTerm) == 0 {
		return false
	}
	used := m.estimateShortTermTokensLocked() + reserveTokens
	if used <= m.shortTermLimitTokens && used <= m.maxContextTokens {
		return false
	}
	compressible, _ := splitMessagesForCompression(m.shortTerm)
	return len(compressible) > 0
}

func (m *Manager) validateContextBudgetLocked(reserveTokens int64) error {
	if err := validateShortTermBudget(m.counter, m.shortTerm, m.shortTermLimitTokens); err != nil {
		return err
	}
	if reserveTokens > 0 && m.estimateShortTermTokensLocked()+reserveTokens > m.maxContextTokens {
		return fmt.Errorf("runtime context exceeds request budget: %d > %d", m.estimateShortTermTokensLocked()+reserveTokens, m.maxContextTokens)
	}
	...
}
```

- [ ] **Step 4: Make the LLM compressor explicit and wire it into the default executor memory manager**

Patch `core/memory/llm_compressor.go` so blank summaries fail loudly:

```go
content := strings.TrimSpace(resp.Message.Content)
if content == "" {
	content = strings.TrimSpace(resp.Content)
}
if content == "" {
	return "", fmt.Errorf("memory compressor returned empty summary")
}
return content, nil
```

Patch `core/agent/executor.go` so the default memory manager uses the real LLM compressor instead of the deterministic fallback:

```go
func buildMemoryManager(factory MemoryFactory, client model.LlmClient, llmModel *coretypes.LLMModel) (*memory.Manager, error) {
	if factory != nil {
		return factory(llmModel)
	}
	compressor := memory.NewLLMShortTermCompressor(memory.LLMCompressorOptions{
		Client: client,
		Model:  llmModel.ModelID(),
	})
	return memory.NewManager(memory.Options{
		Model:      llmModel,
		Counter:    memory.NewTokenCounterForModel(llmModel),
		Compressor: compressor,
	})
}
```

Update the call site in `NewTaskExecutor`:

```go
memoryManager, err := buildMemoryManager(deps.MemoryFactory, client, llmModel)
if err != nil {
	return nil, err
}
```

- [ ] **Step 5: Run the focused memory and executor tests to verify they pass**

Run: `go test ./core/memory -run 'TestRuntimeContextWithReserveTriggersCompressionWhenPromptOverheadPushesRequestOverBudget|TestLLMShortTermCompressorRejectsEmptySummary' && go test ./core/agent -run 'TestBuildMemoryManagerUsesLLMShortTermCompressorByDefault'`

Expected: PASS

- [ ] **Step 6: Run the full memory package tests**

Run: `go test ./core/memory`

Expected: PASS

- [ ] **Step 7: Commit the memory/compression slice**

Run:

```bash
git add core/memory/token_counter.go core/memory/message_budget.go core/memory/manager.go core/memory/llm_compressor.go core/memory/manager_test.go core/memory/llm_compressor_test.go core/agent/executor.go core/agent/memory_test.go
git commit -m "feat(memory): add reserve-aware llm compression"
```

Expected: commit succeeds with the real LLM-compression wiring and reserve-aware memory budgeting.

### Task 4: Add request-budget orchestration, deterministic trimming, and final send guard

**Files:**
- Create: `core/agent/request_budget.go`
- Create: `core/agent/request_budget_test.go`
- Modify: `core/agent/types.go`
- Modify: `core/agent/memory.go`
- Modify: `core/agent/stream.go`
- Modify: `core/agent/events.go`
- Modify: `core/audit/types.go`
- Modify: `core/agent/memory_test.go`
- Modify: `core/agent/stream_test.go`

- [ ] **Step 1: Write the failing request-budget tests**

Create `core/agent/request_budget_test.go` with focused orchestration tests:

```go
package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/EquentR/agent_runtime/core/memory"
	model "github.com/EquentR/agent_runtime/core/providers/types"
	coretypes "github.com/EquentR/agent_runtime/core/types"
)

func TestBuildBudgetedRequestRetriesAfterReserveCompression(t *testing.T) {
	mgr, err := memory.NewManager(memory.Options{
		MaxContextTokens: 120,
		Counter:          fakeTokenCounter{},
		Compressor: func(context.Context, memory.CompressionRequest) (string, error) {
			return "compressed memory", nil
		},
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	mgr.AddMessages([]model.Message{{Role: model.RoleUser, Content: strings.Repeat("a", 70)}, {Role: model.RoleAssistant, Content: strings.Repeat("b", 70)}})

	runner, err := NewRunner(&stubClient{}, nil, Options{
		Model:        "test-model",
		LLMModel:     &coretypes.LLMModel{Context: coretypes.LLMContextConfig{Max: 120}},
		Memory:       mgr,
		TokenCounter: fakeTokenCounter{},
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	_, messages, decision, err := runner.buildBudgetedRequest(context.Background(), false)
	if err != nil {
		t.Fatalf("buildBudgetedRequest() error = %v", err)
	}
	if !decision.CompressionAttempted || decision.FinalPath != "compressed" {
		t.Fatalf("budget decision = %#v, want successful compression path", decision)
	}
	if len(messages) == 0 {
		t.Fatal("messages = empty, want rebuilt request")
	}
}

func TestBuildBudgetedRequestTrimsOldToolMessagesWhenCompressionIsInsufficient(t *testing.T) {
	runner, err := NewRunner(&stubClient{}, nil, Options{
		Model:        "test-model",
		LLMModel:     &coretypes.LLMModel{Context: coretypes.LLMContextConfig{Max: 80}},
		TokenCounter: fakeTokenCounter{},
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	messages := []model.Message{
		{Role: model.RoleUser, Content: strings.Repeat("u", 30)},
		{Role: model.RoleTool, ToolCallId: "call_1", Content: strings.Repeat("tool", 40)},
		{Role: model.RoleAssistant, Content: strings.Repeat("assistant", 20)},
	}
	trimmed, changed := trimMessagesForBudget(messages, 60, fakeTokenCounter{})
	if !changed {
		t.Fatal("trimMessagesForBudget() changed = false, want true")
	}
	if trimmed[1].Role != model.RoleTool || !strings.Contains(trimmed[1].Content, "trimmed tool output") {
		t.Fatalf("trimmed tool message = %#v, want summarized tool output", trimmed[1])
	}
}

func TestBuildBudgetedRequestReturnsErrContextBudgetExceededWhenStillUnsafe(t *testing.T) {
	runner, err := NewRunner(&stubClient{}, nil, Options{
		Model:        "test-model",
		LLMModel:     &coretypes.LLMModel{Context: coretypes.LLMContextConfig{Max: 20}},
		TokenCounter: fakeTokenCounter{},
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	runnerOptions := memory.RuntimeContext{Body: []model.Message{{Role: model.RoleUser, Content: strings.Repeat("x", 200)}}}
	_, _, _, err = runner.buildBudgetedRequestFromContext(context.Background(), runnerOptions, false)
	if !errors.Is(err, ErrContextBudgetExceeded) {
		t.Fatalf("buildBudgetedRequestFromContext() error = %v, want ErrContextBudgetExceeded", err)
	}
}
```

Add this stream regression to `core/agent/stream_test.go`:

```go
func TestRunStreamSkipsProviderCallWhenFinalBudgetGuardFails(t *testing.T) {
	client := &stubClient{}
	runner, err := NewRunner(client, nil, Options{
		Model:        "test-model",
		LLMModel:     &coretypes.LLMModel{Context: coretypes.LLMContextConfig{Max: 20}},
		TokenCounter: fakeTokenCounter{},
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	_, err = runner.Run(context.Background(), RunInput{Messages: []model.Message{{Role: model.RoleUser, Content: strings.Repeat("x", 200)}}})
	if !errors.Is(err, ErrContextBudgetExceeded) {
		t.Fatalf("Run() error = %v, want ErrContextBudgetExceeded", err)
	}
	if len(client.streamRequests) != 0 {
		t.Fatalf("streamRequests = %d, want 0 when budget guard blocks send", len(client.streamRequests))
	}
}
```

- [ ] **Step 2: Run the focused request-budget tests to verify they fail**

Run: `go test ./core/agent -run 'TestBuildBudgetedRequestRetriesAfterReserveCompression|TestBuildBudgetedRequestTrimsOldToolMessagesWhenCompressionIsInsufficient|TestBuildBudgetedRequestReturnsErrContextBudgetExceededWhenStillUnsafe|TestRunStreamSkipsProviderCallWhenFinalBudgetGuardFails'`

Expected: FAIL because no request-budget builder, trim helper, or final guard exists yet.

- [ ] **Step 3: Add runner budget dependencies and implement the request-budget builder**

Patch `core/agent/types.go` so tests and runtime can inject a token counter:

```go
type Options struct {
	SystemPrompt         string
	ResolvedPrompt       *coreprompt.ResolvedPrompt
	RuntimePromptBuilder *runtimeprompt.Builder
	Model                string
	LLMModel             *coretypes.LLMModel
	MaxSteps             int
	MaxTokens            int64
	Memory               *memory.Manager
	TokenCounter         memory.TokenCounter
	EventSink            EventSink
	TraceID              string
	ToolChoice           coretypes.ToolChoice
	Metadata             map[string]string
	Actor                string
	TaskID               string
	AuditRecorder        coreaudit.Recorder
	AuditRunID           string
	Now                  func() time.Time
}
```

Update `NewRunner` to default the counter:

```go
if options.TokenCounter == nil {
	options.TokenCounter = memory.NewTokenCounterForModel(options.LLMModel)
}
```

Create `core/agent/request_budget.go`:

```go
package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/EquentR/agent_runtime/core/memory"
	model "github.com/EquentR/agent_runtime/core/providers/types"
	coretypes "github.com/EquentR/agent_runtime/core/types"
)

var ErrContextBudgetExceeded = errors.New("context budget exceeded")

type requestBudgetDecision struct {
	EstimatedInputTokens int64  `json:"estimated_input_tokens"`
	ContextWindow        int64  `json:"context_window"`
	CompressionAttempted bool   `json:"compression_attempted"`
	CompressionStatus    string `json:"compression_status,omitempty"`
	TrimAttempted        bool   `json:"trim_attempted"`
	FinalPath            string `json:"final_path"`
}

func (r *Runner) buildBudgetedRequest(ctx context.Context, afterToolTurn bool) (runtimeprompt.BuildResult, []model.Message, requestBudgetDecision, error) {
	state, err := r.currentRuntimeContext(ctx)
	if err != nil {
		return runtimeprompt.BuildResult{}, nil, requestBudgetDecision{}, err
	}
	return r.buildBudgetedRequestFromContext(ctx, state, afterToolTurn)
}

func (r *Runner) buildBudgetedRequestFromContext(ctx context.Context, state memory.RuntimeContext, afterToolTurn bool) (runtimeprompt.BuildResult, []model.Message, requestBudgetDecision, error) {
	buildResult, messages, err := r.buildRequestMessages(state, afterToolTurn)
	if err != nil {
		return runtimeprompt.BuildResult{}, nil, requestBudgetDecision{}, err
	}
	decision := requestBudgetDecision{EstimatedInputTokens: r.estimateRequestTokens(messages), ContextWindow: r.maxInputTokens()}
	if decision.ContextWindow <= 0 || decision.EstimatedInputTokens <= decision.ContextWindow {
		decision.FinalPath = "direct"
		return buildResult, messages, decision, nil
	}
	if r.options.Memory != nil {
		reserve := decision.EstimatedInputTokens - memory.CountRuntimeContextTokens(r.options.TokenCounter, state)
		compressedState, trace, err := r.options.Memory.RuntimeContextWithReserve(ctx, reserve)
		decision.CompressionAttempted = trace.Attempted
		decision.CompressionStatus = trace.Status
		if err == nil && trace.Succeeded {
			buildResult, messages, err = r.buildRequestMessages(compressedState, afterToolTurn)
			if err != nil {
				return runtimeprompt.BuildResult{}, nil, decision, err
			}
			decision.EstimatedInputTokens = r.estimateRequestTokens(messages)
			if decision.EstimatedInputTokens <= decision.ContextWindow {
				decision.FinalPath = "compressed"
				return buildResult, messages, decision, nil
			}
		}
	}
	trimmed, changed := trimMessagesForBudget(messages, decision.ContextWindow, r.options.TokenCounter)
	decision.TrimAttempted = changed
	if changed {
		decision.EstimatedInputTokens = r.estimateRequestTokens(trimmed)
		if decision.EstimatedInputTokens <= decision.ContextWindow {
			decision.FinalPath = "trimmed"
			return buildResult, trimmed, decision, nil
		}
	}
	decision.FinalPath = "failed"
	return runtimeprompt.BuildResult{}, nil, decision, fmt.Errorf("%w: request still exceeds budget after compression and trimming", ErrContextBudgetExceeded)
}

func (r *Runner) estimateRequestTokens(messages []model.Message) int64 {
	return memory.CountMessageTokens(r.options.TokenCounter, messages)
}

func (r *Runner) maxInputTokens() int64 {
	if r.options.LLMModel == nil {
		return 0
	}
	window := r.options.LLMModel.ContextWindow()
	if window.Input > 0 {
		return window.Input
	}
	return window.Max
}

func trimMessagesForBudget(messages []model.Message, limit int64, counter memory.TokenCounter) ([]model.Message, bool) {
	trimmed := cloneMessages(messages)
	changed := false
	for i := 0; i < len(trimmed) && memory.CountMessageTokens(counter, trimmed) > limit; i++ {
		if trimmed[i].Role == model.RoleTool && trimmed[i].Content != "" {
			trimmed[i].Content = fmt.Sprintf("trimmed tool output for %s (tool_call_id=%s)", trimmed[i].ToolCallId, trimmed[i].ToolCallId)
			trimmed[i].ProviderState = nil
			changed = true
			continue
		}
		if trimmed[i].Role == model.RoleAssistant || trimmed[i].Role == model.RoleUser {
			trimmed[i].Content = memory.SummarizeMessageForBudget(trimmed[i])
			trimmed[i].ProviderState = nil
			changed = true
		}
	}
	return trimmed, changed
}
```

- [ ] **Step 4: Integrate the budget builder into the stream loop and record audit evidence**

In `core/agent/stream.go`, replace the direct request-build call:

```go
requestBuild, requestMessages, budgetDecision, err := r.buildBudgetedRequestFromContext(ctx, conversationState.Memory, afterToolTurn)
if err != nil {
	r.emitStepFinish(ctx, step, title, map[string]any{"error": err.Error()})
	snapshotResult(step - 1)
	runErr = err
	return
}
```

Then attach and record the decision before the model request artifact:

```go
budgetArtifactID := r.attachAuditArtifact(ctx, coreaudit.ArtifactKindBudgetDecision, budgetDecision)
r.appendAuditEvent(ctx, step, coreaudit.PhaseRequest, "request.budget_evaluated", map[string]any{
	"estimated_input_tokens": budgetDecision.EstimatedInputTokens,
	"context_window":        budgetDecision.ContextWindow,
	"compression_attempted": budgetDecision.CompressionAttempted,
	"compression_status":    budgetDecision.CompressionStatus,
	"trim_attempted":        budgetDecision.TrimAttempted,
	"final_path":            budgetDecision.FinalPath,
}, budgetArtifactID)
```

Add the artifact kind in `core/audit/types.go`:

```go
ArtifactKindBudgetDecision ArtifactKind = "budget_decision"
```

- [ ] **Step 5: Run the focused request-budget tests to verify they pass**

Run: `go test ./core/agent -run 'TestBuildBudgetedRequestRetriesAfterReserveCompression|TestBuildBudgetedRequestTrimsOldToolMessagesWhenCompressionIsInsufficient|TestBuildBudgetedRequestReturnsErrContextBudgetExceededWhenStillUnsafe|TestRunStreamSkipsProviderCallWhenFinalBudgetGuardFails'`

Expected: PASS

- [ ] **Step 6: Run the full agent package tests**

Run: `go test ./core/agent`

Expected: PASS

- [ ] **Step 7: Commit the request-budget orchestration slice**

Run:

```bash
git add core/agent/request_budget.go core/agent/request_budget_test.go core/agent/types.go core/agent/memory.go core/agent/stream.go core/agent/events.go core/audit/types.go core/agent/memory_test.go core/agent/stream_test.go
git commit -m "feat(agent): guard provider sends with request budgets"
```

Expected: commit succeeds with compression retry, deterministic trim fallback, and final send blocking.

### Task 5: Run end-to-end verification for bounded tool outputs and request budgeting

**Files:**
- Verify only; no new code expected
- Verify: `core/tools/builtin/register_test.go`
- Verify: `core/memory/manager_test.go`
- Verify: `core/memory/llm_compressor_test.go`
- Verify: `core/agent/request_budget_test.go`
- Verify: `core/agent/memory_test.go`
- Verify: `core/agent/stream_test.go`

- [ ] **Step 1: Run the targeted package tests together**

Run: `go test ./core/tools/builtin ./core/memory ./core/agent`

Expected: PASS

- [ ] **Step 2: Run a broader repo test sweep**

Run: `go test ./...`

Expected: PASS

- [ ] **Step 3: Review the final diff scope**

Run: `git diff -- core/tools/builtin core/memory core/agent core/audit`

Expected: the diff is limited to bounded builtin outputs, LLM-compression wiring, request budgeting, and audit metadata; no unrelated app/UI changes appear.

## Self-Review

- **Spec coverage:**
  - Tool-level hard limits are covered in Tasks 1-2.
  - LLM compression verification and wiring are covered in Task 3.
  - Request-level budgeting, deterministic trimming, and final guard behavior are covered in Task 4.
  - Observability/audit/test verification are covered in Task 4 and Task 5.
- **Placeholder scan:** no `TODO`, `TBD`, or task references that depend on unspecified code.
- **Type consistency:** the plan uses one set of shared names throughout: `OutputBudgetOptions`, `CompressionTrace`, `RuntimeContextWithReserve`, `NewTokenCounterForModel`, `requestBudgetDecision`, `trimMessagesForBudget`, and `ErrContextBudgetExceeded`.
