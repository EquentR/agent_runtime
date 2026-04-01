# Conversation List And Title Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a conversation list API for sidebar usage and automatically maintain lightweight conversation display metadata such as title, last message, and message count when new turns are appended.

**Architecture:** Extend the persisted `Conversation` snapshot with denormalized UI-facing fields that are cheap to query and update. Maintain these fields inside `ConversationStore.AppendMessages(...)` so reads stay simple. Expose a new `GET /conversations` route that returns conversations ordered by recent activity.

**Tech Stack:** Go, Gin, GORM/SQLite, existing `core/agent.ConversationStore`, existing conversation handler

---

### Task 1: Add failing store tests for title/summary maintenance

**Files:**
- Modify: `core/agent/conversation_store_test.go`

**Step 1: Write the failing tests**

Add:

- `TestConversationStoreAppendMessagesUpdatesTitleSummaryAndCount`
- `TestConversationStoreListConversationsReturnsMostRecentFirst`

**Step 2: Run tests to verify they fail**

Run: `go test ./core/agent -run "TestConversationStoreAppendMessagesUpdatesTitleSummaryAndCount|TestConversationStoreListConversationsReturnsMostRecentFirst"`
Expected: FAIL

### Task 2: Extend conversation snapshot and store methods

**Files:**
- Modify: `core/agent/conversation_store.go`

**Step 1: Write minimal implementation**

Add fields to `Conversation`:

- `LastMessage string`
- `MessageCount int`

Add store method:

- `ListConversations(ctx context.Context) ([]Conversation, error)`

Update `AppendMessages(...)` to:

- set `title` only if currently empty, using first user message summary
- set `last_message` from last appended message content
- increment `message_count`
- update `updated_at`

Title generation rule:

- first non-empty user message only
- trim whitespace
- collapse line breaks to spaces
- cut to around 40 runes

**Step 2: Run tests to verify they pass**

Run: `go test ./core/agent -run "TestConversationStoreAppendMessagesUpdatesTitleSummaryAndCount|TestConversationStoreListConversationsReturnsMostRecentFirst"`
Expected: PASS

### Task 3: Add failing handler test for conversation list API

**Files:**
- Modify: `app/handlers/conversation_handler_test.go`

**Step 1: Write the failing test**

Add:

- `TestConversationHandlerListConversations`

Assert:

- response returns latest-updated conversation first
- includes `title`, `last_message`, `message_count`

**Step 2: Run tests to verify they fail**

Run: `go test ./app/handlers -run "TestConversationHandlerListConversations"`
Expected: FAIL

### Task 4: Implement `GET /conversations`

**Files:**
- Modify: `app/handlers/conversation_handler.go`

**Step 1: Write minimal implementation**

Expose:

- `GET /conversations`

Use `ConversationStore.ListConversations(...)` and return the stored snapshot list.

**Step 2: Run tests to verify they pass**

Run: `go test ./app/handlers -run "TestConversationHandler"`
Expected: PASS

### Task 5: Update docs and run full verification

**Files:**
- Modify: `core/agent/README.md`
- Modify: `README.md`

**Step 1: Update docs**

Document:

- `GET /conversations`
- auto-generated title behavior
- `last_message` and `message_count`

**Step 2: Run focused verification**

Run: `go test ./core/agent ./app/handlers`
Expected: PASS

**Step 3: Run full verification**

Run: `go test ./...`
Expected: PASS

Run: `go build ./cmd/...`
Expected: PASS

Run: `go list ./...`
Expected: PASS
