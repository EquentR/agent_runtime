# Task Manager Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a durable serial task manager with cancellation, background execution, and SSE observation, without implementing the agent loop itself.

**Architecture:** Add a new `core/tasks` package for task models, store, manager, executor registry, and SSE event hub. Wire it into the existing SQLite + Gin app through app migrations, a bootstrapped task manager in `app/commands`, and new task HTTP handlers. Keep tool execution backward-compatible by introducing an explicit runtime object in `core/tools` that can be attached to `context.Context`.

**Tech Stack:** Go, GORM + SQLite, Gin, SSE, context cancellation, JSON payload persistence.

---

### Task 1: Define durable task domain and store tests

**Files:**
- Create: `core/tasks/store_test.go`
- Create: `core/tasks/test_helpers_test.go`
- Create: `core/tasks/types.go`
- Create: `core/tasks/model.go`
- Create: `core/tasks/store.go`

**Step 1: Write the failing store tests**

Add tests covering:
- creating a queued task with initial `task.created` event
- claiming the next queued task and appending `task.started`
- requesting cancellation for a running task
- retrying a terminal task by creating a new queued task linked by `retry_of_task_id`
- listing events after a sequence number

**Step 2: Run the targeted store tests and verify RED**

Run: `go test ./core/tasks -run Store -count=1`
Expected: FAIL because the `core/tasks` package and store do not exist yet.

**Step 3: Write minimal task types and store implementation**

Implement:
- task status constants
- GORM models for `tasks` and `task_events`
- JSON helpers for payload fields
- store methods for create/get/list events/claim/cancel/retry/finish/heartbeat

**Step 4: Re-run the targeted store tests and verify GREEN**

Run: `go test ./core/tasks -run Store -count=1`
Expected: PASS.

### Task 2: Add manager, executor runtime, and task execution tests

**Files:**
- Create: `core/tasks/manager.go`
- Create: `core/tasks/runtime.go`
- Create: `core/tasks/event_hub.go`
- Create: `core/tasks/manager_test.go`
- Modify: `core/tools/register.go`
- Create: `core/tools/runtime.go`
- Modify: `core/tools/register_test.go`

**Step 1: Write the failing manager/runtime tests**

Add tests covering:
- background manager claims and executes a queued task through a registered executor
- cancellation of a running task triggers context cancellation and ends in `cancelled`
- live subscription receives published events
- tool registry preserves attached runtime metadata through `context.Context`

**Step 2: Run targeted manager/tool tests and verify RED**

Run: `go test ./core/tasks ./core/tools -run 'Manager|Runtime' -count=1`
Expected: FAIL because manager/runtime behavior is not implemented yet.

**Step 3: Write minimal manager and runtime implementation**

Implement:
- executor registration by task type
- single-runner background loop
- in-memory live event hub backed by DB event persistence
- active task cancel func tracking
- explicit tool runtime attachment/retrieval helpers

**Step 4: Re-run targeted manager/tool tests and verify GREEN**

Run: `go test ./core/tasks ./core/tools -run 'Manager|Runtime' -count=1`
Expected: PASS.

### Task 3: Add database migration for task tables

**Files:**
- Modify: `app/migration/init.go`
- Modify: `app/migration/define.go`
- Create: `app/migration/task_models.go`

**Step 1: Write the failing migration test**

Add a test that boots migrations against a temporary SQLite DB and asserts the `tasks` and `task_events` tables are created.

**Step 2: Run the migration test and verify RED**

Run: `go test ./app/migration -run Task -count=1`
Expected: FAIL because task migration models are not registered yet.

**Step 3: Register the new migration**

Implement:
- a new migration version for `tasks` and `task_events`
- index creation through GORM tags or explicit SQL where needed

**Step 4: Re-run the migration test and verify GREEN**

Run: `go test ./app/migration -run Task -count=1`
Expected: PASS.

### Task 4: Expose REST + SSE task APIs with handler tests

**Files:**
- Create: `app/handlers/task_handler.go`
- Create: `app/handlers/task_handler_test.go`
- Modify: `app/router/init.go`
- Create: `app/router/deps.go`
- Modify: `app/commands/serve.go`

**Step 1: Write the failing handler tests**

Add tests covering:
- `POST /tasks` creates a queued task
- `GET /tasks/:id` returns the task snapshot
- `POST /tasks/:id/cancel` returns accepted cancellation state
- `POST /tasks/:id/retry` creates a new task
- `GET /tasks/:id/events` streams SSE frames containing historical and live events

**Step 2: Run the handler tests and verify RED**

Run: `go test ./app/handlers -run Task -count=1`
Expected: FAIL because task routes and dependencies do not exist yet.

**Step 3: Write minimal handler and wiring code**

Implement:
- task manager bootstrap in `Serve`
- router dependency injection for handlers
- JSON endpoints for create/get/cancel/retry
- SSE endpoint with `after_seq` support and keep-alive comments

**Step 4: Re-run the handler tests and verify GREEN**

Run: `go test ./app/handlers -run Task -count=1`
Expected: PASS.

### Task 5: Verify the full application still builds and tests cleanly

**Files:**
- Verify only: `cmd/example_agent/main.go`
- Verify only: `app/config/app.go`
- Verify only: `conf/app.yaml`

**Step 1: Run focused package tests**

Run: `go test ./core/tasks ./core/tools ./app/handlers ./app/migration -count=1`
Expected: PASS.

**Step 2: Run the full test suite**

Run: `go test ./... -count=1`
Expected: PASS.

**Step 3: Run the example command build**

Run: `go build ./cmd/...`
Expected: PASS.
