# Core Logging Abstraction Design

## Summary

Introduce a `core/log` package that becomes the only logging entrypoint inside `core`. The package exposes a small package-level facade (`Debug` / `Info` / `Warn` / `Error`), a replaceable `SetLogger` hook, lightweight structured `Field` helpers, and a stdout fallback implementation used before app bootstrapping.

The app layer remains responsible for concrete logger wiring. During startup, the app will initialize `pkg/log` as it does today, wrap the resulting zap logger with an app-layer adapter, and inject that adapter into `core/log` once so every core package shares the same logger.

## Goals

- Keep `core` independent from `pkg/log` and zap.
- Support `debug` / `info` / `warn` / `error` logging from core packages through one shared global facade.
- Allow tests to replace the active logger with `SetLogger`.
- Provide a safe stdout fallback before app initialization.
- Add first-batch summary-only instrumentation to `core/tasks` and selected `core/tools/builtin` files.

## Non-Goals

- No tracing/metrics work.
- No context-scoped logger redesign.
- No full-repo logging sweep outside the selected task and builtin paths.
- No raw payload logging for prompts, file contents, HTTP bodies, or full command lines.

## Design

### 1. `core/log` facade

Add a new `core/log` package with:

- `Logger` interface with `Debug/Info/Warn/Error`
- package-level facade methods with the same names
- `SetLogger(Logger)` to swap the shared logger
- lightweight `Field` type plus helpers such as `String`, `Int`, `Bool`, `Duration`, `Any`, and `Err`
- stdout fallback logger used when no app adapter has been installed

The fallback must be best-effort and never break control flow. If field formatting fails, it should degrade to readable text output.

### 2. App-layer adapter

Add an app-layer adapter that converts `core/log.Field` values into zap fields and delegates to the zap logger created by `pkg/log`.

Startup flow:

1. `pkg/log.Init(...)`
2. wrap `pkg/log.Log()` with the app adapter
3. `core/log.SetLogger(...)`

This preserves the existing app logging stack while keeping core decoupled from zap.

### 3. Instrument `core/tasks`

Add summary-only structured logs to the main task lifecycle:

- worker startup
- task claim / start / finish
- cancel / retry / suspend / resume
- approval and interaction creation / resolution
- executor missing, store transition failures, and recovery paths

Fields should focus on identifiers and state, for example:

- `component=tasks`
- `module=task_manager`
- `task_id`
- `task_type`
- `runner_id`
- `status`
- `approval_id`
- `interaction_id`
- `suspend_reason`
- `duration_ms`

### 4. Instrument selected builtin tools

Add summary-only logs to these tools first:

- `exec_command`
- `http_request`
- `read_file`
- `write_file`
- `delete_file`
- `move_file`
- `web_search`

Each tool should log start, success, and failure. Logged data must stay at the summary level:

- tool name
- task/runtime identifiers when available
- path or cwd summary
- command name only (not full command line)
- args count
- timeout
- exit code
- response size / content length
- duration
- timeout flag

Do not log full file content, request body, response body, prompt text, or raw command output.

## Testing

Add tests before implementation for:

- `core/log` fallback and `SetLogger` replacement behavior
- app adapter delegation into zap
- representative task lifecycle logging
- representative builtin tool logging for start/success/failure behavior

Verification after implementation:

- focused Go package tests for new and changed packages
- broad `go test ./...`
- `go build ./cmd/...`
- `go list ./...`
