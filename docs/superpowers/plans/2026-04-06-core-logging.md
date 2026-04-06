# Core Logging Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a shared core logging facade with app-layer zap injection, then instrument core task and builtin-tool paths with summary-only structured logs.

**Architecture:** Create `core/log` as the only logging entrypoint used by `core`, backed by a stdout fallback until the app installs a zap adapter. Wire the adapter during startup, then add focused logs to `core/tasks` and selected `core/tools/builtin` handlers without changing business semantics.

**Tech Stack:** Go, zap via existing `pkg/log`, Go tests, existing task manager and builtin tool registry.

---

## Planned File Structure

### New files

- `core/log/log.go` — logger interface, field helpers, package-level facade, stdout fallback.
- `core/log/log_test.go` — tests for fallback formatting and `SetLogger` replacement.
- `app/logging/core_adapter.go` — app-layer adapter from zap to `core/log.Logger`.
- `app/logging/core_adapter_test.go` — adapter tests using zap observer.

### Modified files

- `app/commands/serve.go` — install the app adapter after `pkg/log.Init`.
- `core/tasks/manager.go` — add lifecycle/error/recovery logs.
- `core/tasks/runtime.go` — add step/suspend/recovery logs.
- `core/tasks/manager_test.go` — add representative task logging assertions.
- `core/tools/builtin/exec_command.go` — add summary-only start/success/failure logs.
- `core/tools/builtin/http_request.go` — add summary-only request logs.
- `core/tools/builtin/read_file.go` — add summary-only read logs.
- `core/tools/builtin/write_file.go` — add summary-only write logs.
- `core/tools/builtin/delete_file.go` — add summary-only delete logs.
- `core/tools/builtin/move_file.go` — add summary-only move logs.
- `core/tools/builtin/web_search.go` — add summary-only search logs.
- `core/tools/builtin/register_test.go` — add representative builtin logging assertions.

---

### Task 1: Add the core logging facade

**Files:**
- Create: `core/log/log.go`
- Test: `core/log/log_test.go`

- [ ] Write failing tests for fallback output and `SetLogger` replacement.
- [ ] Run `go test ./core/log -v` and confirm failure.
- [ ] Implement the minimal `Logger`, `Field`, facade helpers, `SetLogger`, and stdout fallback.
- [ ] Re-run `go test ./core/log -v` and confirm pass.

### Task 2: Add the app zap adapter

**Files:**
- Create: `app/logging/core_adapter.go`
- Test: `app/logging/core_adapter_test.go`
- Modify: `app/commands/serve.go`

- [ ] Write failing adapter tests that verify `core/log` fields map into zap records.
- [ ] Run `go test ./app/logging -v` and confirm failure.
- [ ] Implement the adapter and startup installation wiring.
- [ ] Re-run `go test ./app/logging ./app/commands -run 'CoreLogger|Logging' -v` and confirm pass.

### Task 3: Instrument task lifecycle logging

**Files:**
- Modify: `core/tasks/manager.go`
- Modify: `core/tasks/runtime.go`
- Modify: `core/tasks/manager_test.go`

- [ ] Write failing task logging tests with a spy logger for representative success/failure/suspend flows.
- [ ] Run `go test ./core/tasks -run 'Logging' -v` and confirm failure.
- [ ] Add summary-only lifecycle, recovery, and error logs without changing control flow.
- [ ] Re-run `go test ./core/tasks -run 'Logging' -v` and confirm pass.

### Task 4: Instrument selected builtin tools

**Files:**
- Modify: `core/tools/builtin/exec_command.go`
- Modify: `core/tools/builtin/http_request.go`
- Modify: `core/tools/builtin/read_file.go`
- Modify: `core/tools/builtin/write_file.go`
- Modify: `core/tools/builtin/delete_file.go`
- Modify: `core/tools/builtin/move_file.go`
- Modify: `core/tools/builtin/web_search.go`
- Modify: `core/tools/builtin/register_test.go`

- [ ] Write failing builtin logging tests with a spy logger for representative success/failure cases.
- [ ] Run `go test ./core/tools/builtin -run 'Logging' -v` and confirm failure.
- [ ] Add summary-only start/success/failure logs to the selected tools.
- [ ] Re-run `go test ./core/tools/builtin -run 'Logging' -v` and confirm pass.

### Task 5: Verify the full change set

**Files:**
- Modify as needed from earlier tasks only.

- [ ] Run focused verification for changed packages.
- [ ] Run `go test ./...`.
- [ ] Run `go build ./cmd/...`.
- [ ] Run `go list ./...`.
- [ ] Fix any regressions and re-run failing verification until clean.
