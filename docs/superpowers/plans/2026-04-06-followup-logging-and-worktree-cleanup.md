# Follow-up Logging and Worktree Cleanup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Safely remove stale worktrees, extend `core/log` with formatted helpers and better fallback output, and add the next batch of summary-only logs in `core/agent` and `core/memory`.

**Architecture:** Keep the current `core/log` interface stable while adding package-level `*f` helpers that format messages before delegating to the existing methods. Improve only the stdout fallback presentation, then instrument `core/agent` and `core/memory` with summary-only logs that expose lifecycle, compression, and failure signals without logging raw prompt or summary payloads.

**Tech Stack:** Go, existing `core/log`, zap via `app/logging`, git worktree CLI, Go tests.

---

## Planned File Structure

### Modified files

- `core/log/log.go`
  - Add `Debugf` / `Infof` / `Warnf` / `Errorf` and improve fallback stdout formatting.
- `core/log/log_test.go`
  - Cover formatted helper routing and improved fallback rendering.
- `core/agent/executor.go`
  - Add summary-only executor lifecycle logs.
- `core/agent/stream.go`
  - Add step/request/stream/tool summary logs.
- `core/agent/executor_test.go`
  - Add representative logging assertions via a spy logger.
- `core/agent/stream_test.go`
  - Add representative logging assertions for stream lifecycle.
- `core/memory/manager.go`
  - Add compression trigger/skip/success/failure logs.
- `core/memory/llm_compressor.go`
  - Add compressor request/failure summary logs.
- `core/memory/manager_test.go`
  - Add representative logging assertions for memory manager behavior.

### Deleted filesystem entries

- `.claude/worktrees/agent-a6ee4a24`
- `.claude/worktrees/agent-ad2e2847`
- `.claude/worktrees/agent-afe4f707`

---

### Task 1: Remove stale worktrees safely

**Files:**
- Remove: `.claude/worktrees/agent-a6ee4a24`
- Remove: `.claude/worktrees/agent-ad2e2847`
- Remove: `.claude/worktrees/agent-afe4f707`

- [ ] Re-check each candidate worktree branch for ahead/behind counts against `main`.
- [ ] Re-check each candidate worktree for uncommitted files that are already superseded by `main`.
- [ ] Run `git worktree remove` for each stale candidate.
- [ ] Remove the corresponding stale branch with `git branch -D` only after the worktree is removed.
- [ ] Run `git worktree list` and confirm only the preserved worktrees remain.

### Task 2: Add formatted logging helpers and prettier fallback output

**Files:**
- Modify: `core/log/log.go`
- Modify: `core/log/log_test.go`

- [ ] Write failing tests for `Debugf` / `Infof` / `Warnf` / `Errorf` routing and improved fallback formatting.
- [ ] Run `go test ./core/log -run 'Format|Fallback' -v` and confirm failure.
- [ ] Implement the package-level formatted helpers by delegating through existing methods after `fmt.Sprintf(...)`.
- [ ] Update fallback output formatting to message-first human-readable rendering.
- [ ] Re-run `go test ./core/log -run 'Format|Fallback' -v` and confirm pass.

### Task 3: Instrument `core/agent`

**Files:**
- Modify: `core/agent/executor.go`
- Modify: `core/agent/stream.go`
- Modify: `core/agent/executor_test.go`
- Modify: `core/agent/stream_test.go`

- [ ] Write failing tests with a spy logger for representative executor and stream lifecycle events.
- [ ] Run `go test ./core/agent -run 'Logging' -v` and confirm failure.
- [ ] Add summary-only logs for executor start/finish, model resolution, conversation loading, stream failures, step lifecycle, tool-call handling, and stop reasons.
- [ ] Re-run `go test ./core/agent -run 'Logging' -v` and confirm pass.

### Task 4: Instrument `core/memory`

**Files:**
- Modify: `core/memory/manager.go`
- Modify: `core/memory/llm_compressor.go`
- Modify: `core/memory/manager_test.go`

- [ ] Write failing tests with a spy logger for compression-trigger and compression-failure behavior.
- [ ] Run `go test ./core/memory -run 'Logging' -v` and confirm failure.
- [ ] Add summary-only logs for compression required/skipped, compression success/failure, summary truncation, and budget validation failures.
- [ ] Re-run `go test ./core/memory -run 'Logging' -v` and confirm pass.

### Task 5: Verify the full follow-up change set

**Files:**
- Modify as needed from earlier tasks only.

- [ ] Run focused verification for `core/log`, `core/agent`, and `core/memory`.
- [ ] Run `go test ./...`.
- [ ] Run `go build ./cmd/...`.
- [ ] Run `go list ./...`.
- [ ] Fix regressions and re-run any failing verification until clean.
