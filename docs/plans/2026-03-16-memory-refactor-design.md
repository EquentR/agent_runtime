# Memory Refactor Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add user-scoped long-term memory in `core/memory` and upgrade short-term memory compression to use LLM-driven rolling summaries.

**Architecture:** Keep `core/memory` split into an in-memory short-term manager and a persistent long-term manager. Both managers depend on narrow compressor abstractions so provider-specific client/model wiring stays outside the memory package, while `app/migration` owns the long-term memory table migration.

**Tech Stack:** Go, GORM, SQLite, existing provider `model.ChatRequest` / `ChatResponse` types, package-level migration bootstrap.

---

### Task 1: Define failing tests for short-term LLM compression

**Files:**
- Modify: `core/memory/manager_test.go`
- Modify: `core/memory/manager.go`

**Step 1: Write the failing tests**

Add tests covering:
- rolling compression request includes previous summary and default instruction
- summary target budget is about 8k while max budget allows slight overflow
- failed compression keeps original short-term state untouched

**Step 2: Run test to verify it fails**

Run: `go test ./core/memory -run "TestContextMessages"`
Expected: FAIL because the new request fields / behavior do not exist yet.

**Step 3: Write minimal implementation**

Update `core/memory/manager.go` to:
- extend compression request/options with instruction + target/max summary budgets
- keep rolling-summary semantics
- preserve state on compression failure

**Step 4: Run test to verify it passes**

Run: `go test ./core/memory -run "TestContextMessages"`
Expected: PASS.

### Task 2: Define failing tests for long-term memory store and flush

**Files:**
- Create: `core/memory/long_term_test.go`
- Create: `core/memory/long_term.go`

**Step 1: Write the failing tests**

Add tests covering:
- empty `user_id` returns an error
- `GetSummary` creates or reads the single row for a user
- `Flush` with no messages skips compression
- `Flush` persists compressed summary and overwrites old value only after success

**Step 2: Run test to verify it fails**

Run: `go test ./core/memory -run "TestLongTerm"`
Expected: FAIL because long-term manager/store do not exist yet.

**Step 3: Write minimal implementation**

Implement a user-scoped long-term manager backed by GORM with a single `user_id` row per user and a rolling LLM compressor.

**Step 4: Run test to verify it passes**

Run: `go test ./core/memory -run "TestLongTerm"`
Expected: PASS.

### Task 3: Add migration coverage for long-term memory table

**Files:**
- Modify: `app/migration/define.go`
- Modify: `app/migration/init.go`
- Modify: `app/migration/task_migration_test.go`

**Step 1: Write the failing test**

Extend migration tests to assert the long-term memory table is created.

**Step 2: Run test to verify it fails**

Run: `go test ./app/migration -run TestBootstrapMigratesTaskTables`
Expected: FAIL because the new table is not migrated yet.

**Step 3: Write minimal implementation**

Register a new migration version that auto-migrates the long-term memory model.

**Step 4: Run test to verify it passes**

Run: `go test ./app/migration -run TestBootstrapMigratesTaskTables`
Expected: PASS.

### Task 4: Verify the full memory package behavior

**Files:**
- Modify as needed: `core/memory/*.go`
- Modify as needed: `app/migration/*.go`

**Step 1: Run focused package tests**

Run: `go test ./core/memory ./app/migration`
Expected: PASS.

**Step 2: Run full validation**

Run: `go test ./...`
Expected: PASS.
