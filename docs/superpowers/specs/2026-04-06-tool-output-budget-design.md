# Tool Output Budget and Context Compression Design

## Summary

Add a bounded-output pipeline so no tool result, replay payload, or provider request can grow without control. Every tool output must be shaped into a replay-safe form before it enters conversation history. Every provider request must pass a context-budget check before send. When the assembled context still exceeds the model window, the runtime should first attempt LLM-based compression and then fall back to deterministic trimming before the final provider-send guard either allows the request or returns a structured budget error.

## Goals

- Prevent oversized tool results from breaking provider replay before context compression can run.
- Make tool outputs replay-safe by default rather than relying on later best-effort compression.
- Add request-level context budgeting before every provider call.
- Prefer automatic LLM compression when the assembled request exceeds the model window.
- Fall back to deterministic trimming when compression is unavailable, fails, or does not reduce enough.
- Make every budget decision observable in logs and audit/replay data.
- Verify that the existing LLM-based context compression path is actually triggerable and effective.

## Non-Goals

- No attempt to preserve arbitrary raw tool output in full once it exceeds configured budgets.
- No silent provider-side truncation or blind retry against oversize requests.
- No dependence on compression as the only safety mechanism.
- No UI redesign in this spec beyond clearer surfaced error messages and truncation metadata.

## Problems This Design Solves

1. A single tool call such as file read or command execution can return a body large enough to make the next provider replay fail before compression runs.
2. Even if each individual tool result is bounded, the accumulated conversation can still exceed the model context window.
3. The current LLM compression path is not yet proven to trigger reliably, reduce tokens enough, or preserve the right information.
4. When the system fails today, it can fail as an opaque provider error rather than a controlled, explainable budget decision.

## Design Overview

Add five layers in the request pipeline:

1. **Tool Output Shaper**
   - Shapes each tool result into a bounded, replay-safe payload before it becomes a tool message.
2. **Context Budget Estimator**
   - Estimates token usage for the full provider request, including history and reserved output headroom.
3. **Compression Orchestrator**
   - When over budget, first attempts LLM-based context compression on compressible older content.
4. **Deterministic Trimmer**
   - If compression is unavailable, fails, or is insufficient, replaces low-priority large segments with compact summaries/excerpts.
5. **Provider Send Guard**
   - Final hard gate that blocks any request still exceeding the budget after all recovery steps.

This makes replay safety a runtime invariant instead of a best-effort property.

## Architectural Invariant

Any content that can enter conversation history, participate in replay, or be sent to a provider must pass budget control first.

There should be no bypass path for "display-only" or "raw tool output" messages if those messages can later participate in replay.

## Component Design

### 1. Tool Output Shaper

The shaper runs immediately after a tool finishes and before its result is persisted or attached to the conversation.

Responsibilities:

- classify text vs binary content where applicable
- enforce per-tool limits for lines, bytes, items, and field lengths
- produce pagination metadata for continuable reads
- emit explicit truncation metadata
- normalize tool results into a bounded structured envelope

Common metadata fields:

- `truncated`
- `limit_reason`
- `original_size`
- `returned_size`
- tool-specific continuation pointers such as `next_start_line`, `next_offset`, or `remaining_count`

The output envelope should favor navigable excerpts plus continuation metadata over dumping raw full bodies.

### 2. Context Budget Estimator

Before each provider call, estimate total input size and compare it against the selected model context window.

Budget inputs should include:

- system/developer prompt tokens
- replay/history tokens
- tool message tokens
- current user input tokens
- reserved output tokens
- reasoning reserve if the selected model requires extra headroom

The estimator decides whether the request:

- fits directly
- exceeds budget and needs compression
- still exceeds budget after compression and needs deterministic trimming
- remains unsafe and must be blocked

### 3. Compression Orchestrator

When the full request is over budget, attempt LLM-based compression first.

Compression target priority:

1. old large tool outputs
2. old long assistant/user text blocks
3. older finished turns
4. content already superseded by later discussion

Compression should avoid modifying:

- the newest user request
- required continuation fragments for current tool/reasoning replay
- system/developer instructions
- the most recent high-fidelity interaction needed to continue correctly

Compression output should be structured, not a raw untyped string. Example shape:

```json
{
  "type": "compressed_context",
  "source_message_ids": ["m18", "m19", "m20"],
  "reason": "context_budget",
  "summary": "...",
  "facts_to_preserve": ["...", "..."],
  "open_threads": ["..."]
}
```

Success criteria:

- result parses and matches expected shape
- result is non-empty
- estimated tokens decrease
- preserved facts/open threads remain available
- the recomputed request now fits, or at least meaningfully shrinks toward the target

### 4. Deterministic Trimmer

If compression is unavailable, fails, or remains insufficient, apply deterministic trimming.

Trim priority order:

1. oldest large tool outputs
2. old outputs already summarized once
3. large command stdout/stderr blocks
4. long file body excerpts
5. older low-priority dialogue text

Trimming replaces large raw blocks with compact summaries or excerpts rather than deleting silently. Example:

```json
{
  "type": "tool_result_excerpt",
  "tool": "read_file",
  "summary": "Read lines 1-300 of foo.go; file has 2480 total lines.",
  "truncated": true,
  "next_start_line": 301
}
```

The trimming order must be stable so the runtime behaves predictably and tests can assert exact fallback behavior.

### 5. Provider Send Guard

The final provider send step must re-check the request after shaping/compression/trimming.

If the request still exceeds the budget:

- do not send it to the provider
- return a structured `context_budget_exceeded` error
- include whether compression and trimming were attempted
- include a user-actionable next step

## Tool Rule Matrix

### `read_file`

Rules:

- text files only
- binary files are rejected with a structured tool error
- default behavior without explicit window: return first 300 lines
- explicit `line_count` still has a hard maximum, such as 300
- always return line-window metadata

Recommended metadata:

- `path`
- `start_line`
- `end_line`
- `total_lines`
- `has_more`
- `next_start_line`
- `truncated`

Example:

```json
{
  "meta": {
    "tool": "read_file",
    "truncated": true,
    "limit_reason": "default_window",
    "start_line": 1,
    "end_line": 300,
    "total_lines": 2480,
    "has_more": true,
    "next_start_line": 301
  },
  "content": "..."
}
```

### Command execution tools

Rules:

- bound stdout and stderr independently
- bound total lines as well as total bytes
- preserve exit code and truncation status
- prefer prefix retention and optional suffix retention with middle elision

Recommended metadata:

- `exit_code`
- `stdout_truncated`
- `stderr_truncated`
- `original_stdout_bytes`
- `original_stderr_bytes`
- `returned_stdout_bytes`
- `returned_stderr_bytes`

### Search tools

Rules:

- cap matched files count
- cap returned match count
- cap per-match excerpt length
- return totals plus leading results rather than dumping all matches

Recommended metadata:

- `total_matches`
- `returned_matches`
- `truncated`
- `suggestion` for narrowing scope

### Directory/listing tools

Rules:

- cap returned item count
- cap item label/path length
- return `remaining_count` when truncated

### JSON and structured outputs

Rules:

- cap serialized byte size
- cap individual string field length
- cap array item counts
- collapse deep/large nested values

### Network/web fetch tools

Rules:

- do not return full large response bodies by default
- return summary plus bounded excerpts and source metadata
- raw bodies must also use excerpt/pagination semantics

## End-to-End Budget Flow

1. Tool executes.
2. Tool Output Shaper bounds and annotates the tool result.
3. The bounded result is persisted and attached as the tool message.
4. Before the next provider call, Context Budget Estimator computes total request size.
5. If the request fits, send directly.
6. If over budget, Compression Orchestrator attempts LLM compression.
7. Re-estimate the request.
8. If still over budget, Deterministic Trimmer reduces low-priority large segments.
9. Re-estimate again.
10. Provider Send Guard either sends or returns a structured budget error.

## Compression Verification Requirements

The existing LLM-based compression flow must not be assumed to work until it passes explicit validation.

Required validation:

1. **Triggerability**
   - A provably oversize request enters the compression path rather than failing earlier in replay assembly.
2. **Reduction**
   - Compression reduces estimated tokens measurably and can bring a request below budget when appropriate.
3. **Semantic preservation**
   - Latest user goal, open threads, important tool conclusions, and required replay structure remain usable.
4. **Fallback safety**
   - Provider unavailable, invalid output, empty output, oversized summary, and insufficient reduction all fall through to deterministic trimming.

## Observability

### Budget evaluation logs

Log at every provider send attempt:

- `model`
- `context_window`
- `estimated_input_tokens`
- `reserved_output_tokens`
- `budget_status`

### Compression logs

When compression is attempted, log:

- `compression_attempted`
- `compression_target_message_count`
- `compression_input_tokens`
- `compression_output_tokens`
- `compression_ratio`
- `compression_status`

Suggested status values:

- `success`
- `provider_unavailable`
- `llm_error`
- `invalid_result`
- `insufficient_reduction`

### Trimming logs

When deterministic trimming runs, log:

- `trim_attempted`
- `trimmed_segments`
- `trimmed_bytes_or_tokens`
- `trim_strategy`
- `post_trim_budget_status`

### Final decision logs

- `send_guard_passed`
- `final_estimated_input_tokens`
- `final_path` with values such as `direct`, `compressed`, `trimmed`, or `failed`

## Audit / Replay Integration

Add or extend audit events so replay can explain why a request was shortened or blocked.

Suggested events:

- `context_budget_evaluated`
- `context_compression_started`
- `context_compression_succeeded`
- `context_compression_failed`
- `context_trim_applied`
- `provider_send_blocked`

These events should make it possible to answer, from replay data alone, whether a turn used direct send, compression, trimming, or final blocking.

## Failure Semantics

### Tool-level rejection

Used for local tool safety failures such as binary file reads or requests that violate allowed windows.

Example:

```json
{
  "error": "tool_output_rejected",
  "tool": "read_file",
  "reason": "binary_file",
  "message": "Binary files cannot be read as text."
}
```

### Auto-recoverable over-budget state

If compression or trimming succeeds, continue normally without surfacing a hard error to the user. Internal audit/logging should still record what happened.

### Compression failure with successful fallback

If compression fails but deterministic trimming succeeds, continue normally while preserving internal evidence of the failed compression attempt and applied trim.

### Final budget guard failure

If the request remains too large after all recovery steps, return a structured error and do not call the provider.

Example:

```json
{
  "error": "context_budget_exceeded",
  "message": "Request still exceeds context budget after compression and trimming.",
  "compression_attempted": true,
  "trim_attempted": true,
  "suggested_action": "Read a smaller file range or narrow command output."
}
```

### Capability gaps

If model-window metadata, token estimation, or compression infrastructure is unavailable, the runtime must fail closed rather than bypassing budget control. Prefer conservative trimming and then a structured hard failure over sending an unsafe request.

## User-Visible Behavior

### File reads

Users should see a bounded window, explicit truncation metadata, and a continuation pointer instead of a full huge file dump.

### Command output

Users should see bounded stdout/stderr excerpts, truncation metadata, and the original size summary instead of unbounded logs.

### Over-budget conversations

- If automatic recovery succeeds, the conversation continues normally.
- If automatic recovery fails, the user receives a specific actionable budget error instead of a vague provider failure.

## Testing Plan

### Unit tests

Add tests for:

- `read_file` default windowing
- binary-file rejection
- command/search/list/json shaping behavior
- context budget estimator edge cases
- deterministic trimming order and stability

### Integration tests

Add end-to-end coverage for:

1. large single tool output shaped into replay-safe form
2. accumulated history triggers compression
3. compression failure falls back to deterministic trimming
4. trimming success allows provider send
5. trimming still insufficient causes final guard failure

### Regression tests

Cover the failure modes most likely to recur:

- large file read followed by continued conversation
- long command output followed by another tool step
- multi-turn replay with tool continuation
- compression path present in code but not actually reached at runtime

## Success Criteria

This design is successful when all of the following are true:

- No single tool result can enter history in unbounded form.
- Every provider request is budget-checked before send.
- Over-budget requests first attempt LLM compression.
- Compression failure or insufficiency triggers deterministic trimming.
- No request exceeds the provider window without the final guard blocking it.
- Logs and audit/replay data clearly show which path each turn took.
- The runtime no longer fails with opaque oversize provider errors before local recovery paths run.
