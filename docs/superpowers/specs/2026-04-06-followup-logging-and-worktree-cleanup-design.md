# Follow-up Logging and Worktree Cleanup Design

## Summary

This follow-up extends the new `core/log` layer in two directions: prettier human-readable output and broader core observability. It also safely cleans up only those stale Claude-created worktrees whose uncommitted contents are already superseded by current `main`.

The work splits into three parts:

1. remove stale worktrees that contain no unique value relative to `main`
2. add `Debugf` / `Infof` / `Warnf` / `Errorf` support to `core/log`
3. add summary-only logs to `core/agent` and `core/memory`

## Goals

- Safely remove stale `.claude/worktrees/*` entries only when analysis shows their local changes are already absorbed by `main`
- Improve log ergonomics by supporting printf-style logging from core call sites
- Improve fallback console readability without giving up structured field logging
- Add the next batch of core logs in `core/agent` and `core/memory`
- Continue avoiding raw prompt text, full message bodies, raw summary text, or other sensitive/noisy payloads in logs

## Non-Goals

- No cleanup of worktrees that still contain potentially valuable unfinished work
- No change to the product decision for the two `conversation_handler` worktrees in this round
- No full logging sweep across all core packages
- No redesign of `pkg/log` or replacement of zap
- No tracing, metrics, or request-id architecture work in this round

## Worktree Cleanup Design

Current analysis shows three stale worktrees whose uncommitted intent is already present on `main`:

- `.claude/worktrees/agent-a6ee4a24`
- `.claude/worktrees/agent-ad2e2847`
- `.claude/worktrees/agent-afe4f707`

These should be removed only after one more safety pass confirms:

- the worktree branch is not ahead of `main`
- there are no unique commits to preserve
- the uncommitted diff is either already present on `main` or superseded by newer code on `main`

Worktrees that still look potentially useful should remain untouched for now:

- `.claude/worktrees/agent-a201136b`
- `.claude/worktrees/agent-a6b390b5`
- `.claude/worktrees/agent-ad9ad283`
- `.claude/worktrees/agent-a8ab57dd`
- `.claude/worktrees/agent-afdfc81e`

## Formatted Logging Design

### 1. Extend `core/log` with printf-style helpers

Add package-level helpers:

- `Debugf`
- `Infof`
- `Warnf`
- `Errorf`

These should not require expanding the `Logger` interface. Instead, each helper should perform `fmt.Sprintf(...)` inside `core/log`, then delegate to the existing `Debug` / `Info` / `Warn` / `Error` methods.

That keeps the adapter contract stable while giving core call sites a lighter-weight option for human-readable logs.

### 2. Improve fallback stdout formatting

The current fallback format is readable but flat. Replace it with a more human-scannable layout such as:

- no fields: `[2026-04-06 21:30:15.123] INFO task started`
- with fields: `[2026-04-06 21:30:15.123] INFO task started | task_id=123 task_type=agent.run`

This keeps:

- stable timestamp
- visible log level
- message-first reading
- clear separation between message and fields

The fallback logger remains best-effort only.

### 3. Preserve structured field support

Structured `Field` helpers remain valid and should continue working through both:

- stdout fallback
- app zap adapter

Printf-style helpers are additive, not a replacement.

## Additional Logging Hotspots

### `core/agent`

Focus files:

- `core/agent/executor.go`
- `core/agent/stream.go`
- optionally small supporting touch-ups in nearby runner helpers if needed

Recommended events:

- executor start / finish
- conversation loaded
- provider/model resolved
- runtime prompt resolved
- step start / finish
- stream open failure
- model completion received
- tool-call execution start / finish
- waiting-for-interaction / waiting-for-tool-approval transitions
- persistence failures
- stop reason and duration summaries

Allowed fields:

- conversation id
- task id
- provider/model ids
- step number
- tool-call count
- message count
- stop reason
- usage totals
- duration

Not allowed:

- full prompt contents
- full assistant message bodies
- tool argument bodies beyond short summaries

### `core/memory`

Focus files:

- `core/memory/manager.go`
- `core/memory/llm_compressor.go`

Recommended events:

- compression required / skipped
- compression request built
- compression success / failure
- context budget validation failure
- summary truncation applied
- short-term / summary token budget figures
- LLM compressor request failure

Allowed fields:

- short-term message count
- budget/token counts
- summary length
- max/target summary tokens
- duration

Not allowed:

- raw compressed summary text
- full messages being compressed

## Testing

Add tests before implementation for:

- `core/log` formatted helper behavior and improved fallback output
- representative `core/agent` logging for run/step lifecycle
- representative `core/memory` logging for compression-trigger and compression-failure paths

Verification after implementation:

- focused package tests for changed areas
- `go test ./...`
- `go build ./cmd/...`
- `go list ./...`

## Rollout Order

1. Remove the three stale worktrees only
2. Extend `core/log` with `*f` helpers and prettier fallback output
3. Add `core/agent` logs
4. Add `core/memory` logs
5. Run full verification
