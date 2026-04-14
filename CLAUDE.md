# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Key commands

### Backend
- Install Go dependencies: `go mod download`
- Build all backend commands: `go build ./cmd/...`
- Build the example binary: `go build -o bin/example_agent ./cmd/example_agent`
- Run all Go tests: `go test ./...`
- List packages: `go list ./...`
- Run one Go package: `go test ./app/handlers`
- Run one Go test by name: `go test ./app/handlers -run '^TestSwaggerUIRoutesExposeHTMLAndGeneratedDocs$'`
- Run one core test by name: `go test ./core/tasks -run '^TestStoreCreateTaskPersistsQueuedSnapshotAndCreatedEvent$'`

### Frontend
- Install frontend dependencies: `pnpm --dir webapp install`
- Start frontend dev server: `pnpm --dir webapp dev`
- Build frontend: `pnpm --dir webapp build`
- Run frontend tests: `pnpm --dir webapp test`
- Run frontend typecheck: `pnpm --dir webapp exec vue-tsc -b`
- Run one Vitest file: `pnpm --dir webapp exec vitest run src/lib/api.spec.ts`
- Run one Vitest test by title: `pnpm --dir webapp exec vitest run src/lib/api.spec.ts -t "includes role from backend auth payloads"`

### Full app / packaging
- Run backend locally: `./bin/example_agent -config conf/app.yaml`
- Build distributable package: `./build.sh`

## Environment expectations
- Go version: `1.25.0` (`go.mod`)
- Frontend package manager: `pnpm`
- Frontend stack expects Node `20.19+` or `22.12+` because of Vite 8 / Vitest 4
- Main runtime config is `conf/app.yaml`
- Config values may use environment variable placeholders expanded by `cmd/example_agent/main.go`

## High-level architecture

### Startup flow
The main startup path is:

`cmd/example_agent/main.go` → `app/commands/serve.go` → `app/router/init.go`

`main.go` loads YAML config, expands environment variables, optionally opens the browser, and hands off to `commands.Serve`. `Serve` is the composition root: it initializes logging, SQLite, migrations, task manager, conversation store, prompt runtime, auth logic, tool registry, audit runtime, model resolver, and finally registers the `agent.run` executor plus HTTP routes.

### Layering
Keep the existing layering intact:
- `app`: application assembly, HTTP handlers, router wiring, migrations, auth/app logic
- `core`: runtime domain logic (agent execution, tasks, tools, prompts, approvals, interactions, audit, providers, memory)
- `pkg`: shared infrastructure (DB, logging, REST helpers, migrations)

Do not move reusable runtime behavior into `app`; keep it in `core`.

### Agent runtime execution model
`agent.run` is registered as a task executor in `app/commands/serve.go`. The path is:
- task created through `app/handlers/task_handler.go`
- executed by `core/tasks.Manager`
- dispatched to `core/agent.NewTaskExecutor`
- run by the agent runner with model client, prompt resolution, tools, memory, conversation persistence, and audit recording

Important pieces:
- `core/agent/executor.go`: resolves model + prompt, loads conversation history, resumes waiting tasks, persists messages
- `core/tasks/manager.go`: worker pool, leasing, cancellation, retries, suspend/resume, event publication
- `core/agent/conversation_store.go`: conversation metadata + persisted message history

Conversation ID also acts as the task `concurrency_key`, so one conversation’s active task stream is serialized through the task system.

### Human-in-the-loop flow
Human approval and Q&A are first-class runtime features, not ad hoc handler logic.

Key stores and flows:
- `core/approvals`: tool approval persistence
- `core/interactions`: structured user question/response persistence
- `core/tasks.Manager`: creates/resumes waiting tasks and publishes related events
- `core/tools/builtin/register.go`: registers built-in `ask_user` and command/file/web tools

When a task needs approval or user input, it transitions into a waiting state and resumes through the task manager flow rather than bypassing task state.

### Prompt system
Prompt resolution is not hardcoded in handlers.

Key pieces:
- `core/prompt/store.go`: prompt documents and binding persistence
- `core/prompt/resolver.go`: resolves prompt segments by scene / provider / model / phase
- workspace prompt content is also loaded from the workspace root during resolution

Keep prompt routing and phase-specific prompt assembly inside `core/prompt`.

### Tools and providers
- `core/tools/register.go`: in-process tool registry and MCP prompt/tool registration bridge
- `core/tools/builtin/register.go`: built-in local tools (file ops, grep/search, command execution, ask-user, HTTP, web search, process tools)
- `core/providers/client/*`: provider-specific SDK wiring for Google GenAI, OpenAI Completions-compatible, and OpenAI Responses APIs

Provider-specific details should stay inside provider clients / MCP adapters, not leak into handlers.

### HTTP surface
Handlers live in `app/handlers` and are centrally registered in `app/router/init.go`.
Current main resources include:
- auth
- models
- tasks
- conversations
- prompts
- approvals
- interactions
- audit
- swagger

Handler responsibilities:
- request parsing
- auth checks
- response shaping / status mapping
- delegating business logic downward

Use `pkg/rest` wrappers instead of introducing raw Gin boilerplate patterns.

### Frontend structure
The frontend is a Vue 3 + TypeScript + Vite app in `webapp`.

Important boundaries:
- `webapp/src/lib/api.ts`: API client, normalization, request builders
- `webapp/src/lib/transcript.ts`: converts task stream / conversation data into UI transcript entries, including reasoning/tool/approval/question states
- `webapp/src/router/index.ts`: session and admin route guards
- `webapp/src/views/ChatView.vue`: main chat/task interaction screen
- `webapp/src/views/AdminPromptView.vue`: prompt management UI
- `webapp/src/views/AdminAuditView.vue`: audit viewer UI

If backend payload shapes change for tasks, approvals, interactions, or conversations, update both frontend types and the normalization/transcript logic in the same change.

## Working rules for this repo
- Register new runtime dependencies in `app/commands/serve.go`
- Register new HTTP handlers in `app/router/init.go`; do not wire routes ad hoc elsewhere
- Keep task lifecycle logic in `core/tasks`; do not recreate waiting/cancellation state machines in handlers or views
- Keep conversation persistence in `core/agent/conversation_store.go`
- Reuse `core/approvals`, `core/interactions`, and `core/audit` for those domains instead of adding parallel local implementations
- Keep prompt routing in `core/prompt`
- Keep provider-specific SDK details in `core/providers/client/*` or `core/mcp`
- If request/response contracts change, regenerate `docs/swagger/*` instead of hand-editing generated Swagger files
- Prefer narrow, pattern-matching changes over broad refactors

## Testing guidance
- For backend changes, the default verification set is:
  - `go test ./...`
  - `go build ./cmd/...`
  - `go list ./...`
- For frontend-only changes, run at least:
  - `pnpm --dir webapp exec vue-tsc -b`
  - relevant `pnpm --dir webapp exec vitest ...` tests
- Frontend tests live beside source as `*.spec.ts`
- Go tests live beside source as `*_test.go`

## Existing repo guidance worth following
There is no existing `CLAUDE.md`, `.cursorrules`, or `.github/copilot-instructions.md`, but there is a detailed `AGENTS.md` at the repo root. When working in this repo, align with it—especially:
- keep the `app -> core -> pkg` layering
- prefer Chinese when updating repo collaboration docs
- avoid hand-editing generated Swagger artifacts
- update frontend boundary code when backend contract changes
