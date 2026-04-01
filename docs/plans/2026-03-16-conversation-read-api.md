# Conversation Read API Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add read-only conversation APIs so the UI can load conversation metadata and replay message history, while keeping conversation creation implicit in `agent.run` task execution.

**Architecture:** Reuse the existing `core/agent.ConversationStore` as the single source of truth for conversation metadata and persisted normalized messages. Add a lightweight `ConversationHandler` in the app layer, inject the store through router dependencies, and expose only read routes: get conversation detail and get conversation messages. Do not add a dedicated create conversation route.

**Tech Stack:** Go, Gin, `core/agent`, existing REST wrapper utilities, existing app router dependency injection

---

### Task 1: Add store helpers and failing handler tests

**Files:**
- Modify: `core/agent/conversation_store.go`
- Create: `app/handlers/conversation_handler_test.go`
- Modify: `app/router/deps.go`

**Step 1: Write the failing tests**

Add tests:

- `TestConversationHandlerGetConversation`
- `TestConversationHandlerGetConversationMessages`
- `TestConversationHandlerReturnsNotFoundForMissingConversation`

**Step 2: Run tests to verify they fail**

Run: `go test ./app/handlers -run "TestConversationHandler"`
Expected: FAIL

**Step 3: Add minimal store helpers if needed**

If handler ergonomics need it, add:

- `ListConversationMessages(...)` alias or summary mapping helper

Prefer not to grow the store unless the handler truly needs it.

**Step 4: Run tests to verify they still fail for missing handler**

Run: `go test ./app/handlers -run "TestConversationHandler"`
Expected: FAIL because handler/routes not implemented yet

### Task 2: Implement conversation read handler

**Files:**
- Create: `app/handlers/conversation_handler.go`
- Modify: `app/router/deps.go`
- Modify: `app/router/init.go`

**Step 1: Write minimal implementation**

Expose two routes:

- `GET /conversations/:id`
- `GET /conversations/:id/messages`

Handler dependencies:

```go
type ConversationHandler struct {
	store *agent.ConversationStore
}
```

Response behavior:

- missing conversation -> HTTP 404-ish REST error path
- found conversation -> return `core/agent.Conversation`
- found messages -> return `[]model.Message`

**Step 2: Register routes**

Inject `ConversationStore` through `router.Dependencies`, then add `NewConversationHandler(...)` into router registration.

**Step 3: Run tests to verify they pass**

Run: `go test ./app/handlers -run "TestConversationHandler"`
Expected: PASS

### Task 3: Update docs and run full verification

**Files:**
- Modify: `core/agent/README.md`
- Modify: `README.md`

**Step 1: Update docs**

Document that:

- conversation creation is implicit in `agent.run`
- read APIs are available for UI hydration

**Step 2: Run focused verification**

Run: `go test ./app/handlers ./core/agent`
Expected: PASS

**Step 3: Run full verification**

Run: `go test ./...`
Expected: PASS

Run: `go build ./cmd/...`
Expected: PASS

Run: `go list ./...`
Expected: PASS
