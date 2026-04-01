# Chat UI Optimization Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Improve the web chat workspace and transcript UX with collapsible workspace navigation, deletable conversations, denser non-reply cards, animated loading states, and Enter-to-send composer behavior.

**Architecture:** Extend the existing Vue chat view with a collapsible sidebar state and refresh-aware selection flow, then tighten transcript rendering by introducing collapsible detail cards and a merged tool result presentation. Add the smallest backend delete capability needed to keep the sidebar state authoritative instead of relying on local-only hiding.

**Tech Stack:** Vue 3, Vue Test Utils, Vitest, TypeScript, Go, Gin, Gorm, SQLite

---

### Task 1: Conversation delete backend

**Files:**
- Modify: `core/agent/conversation_store.go`
- Modify: `core/agent/conversation_store_test.go`
- Modify: `app/handlers/conversation_handler.go`
- Modify: `app/handlers/conversation_handler_test.go`

**Step 1: Write the failing test**

Add a store test that creates a conversation with persisted messages, deletes it, then verifies `GetConversation` returns `ErrConversationNotFound` and `ListMessages` returns an empty slice. Add a handler test that sends `DELETE /api/v1/conversations/:id` and verifies the conversation no longer appears in `GET /api/v1/conversations`.

**Step 2: Run test to verify it fails**

Run: `go test ./core/agent ./app/handlers`
Expected: FAIL because delete behavior and route do not exist yet.

**Step 3: Write minimal implementation**

Add `DeleteConversation(ctx, id)` to the store and delete both `conversation_messages` and `conversations` in one transaction. Register a `DELETE /conversations/:id` handler that returns 404 for missing conversations.

**Step 4: Run test to verify it passes**

Run: `go test ./core/agent ./app/handlers`
Expected: PASS.

### Task 2: Sidebar behavior and refresh selection flow

**Files:**
- Modify: `webapp/src/views/ChatView.vue`
- Modify: `webapp/src/views/ChatView.spec.ts`
- Modify: `webapp/src/components/ConversationSidebar.vue`
- Modify: `webapp/src/components/ConversationSidebar.spec.ts`
- Modify: `webapp/src/lib/api.ts`

**Step 1: Write the failing test**

Add component tests that verify the sidebar can emit collapse and delete actions, and a view test that after creating a new task the conversations list reloads and the new conversation becomes active. Add a delete flow test that removing the active conversation clears the transcript or falls back to the newest remaining conversation.

**Step 2: Run test to verify it fails**

Run: `npm test -- ChatView.spec.ts ConversationSidebar.spec.ts`
Expected: FAIL because the new UI controls and refresh flow do not exist.

**Step 3: Write minimal implementation**

Add a collapsible workspace panel with a compact rail state, expose per-item delete controls, call the delete API, and extend `loadConversations` so post-send refresh can explicitly select the returned conversation ID.

**Step 4: Run test to verify it passes**

Run: `npm test -- ChatView.spec.ts ConversationSidebar.spec.ts`
Expected: PASS.

### Task 3: Transcript card density and collapsible detail states

**Files:**
- Modify: `webapp/src/components/MessageList.vue`
- Modify: `webapp/src/components/MessageList.spec.ts`
- Modify: `webapp/src/lib/transcript.ts`
- Modify: `webapp/src/types/api.ts`

**Step 1: Write the failing test**

Add tests that verify `Thinking`, tool params, and tool result render collapsed by default, tool result is merged into the same tool card with a visible title, and non-reply cards use the compact card classes/structure instead of the previous full-width content style.

**Step 2: Run test to verify it fails**

Run: `npm test -- MessageList.spec.ts`
Expected: FAIL because the transcript model and card layout do not support collapsed detail sections.

**Step 3: Write minimal implementation**

Enrich transcript entries with optional detail blocks for params/result text, keep tool start and finish events mapped to the same entry by `tool_call_id`, and render compact collapsible sections with status-aware loading labels.

**Step 4: Run test to verify it passes**

Run: `npm test -- MessageList.spec.ts`
Expected: PASS.

### Task 4: Composer keyboard behavior and loading animation styling

**Files:**
- Modify: `webapp/src/components/MessageComposer.vue`
- Modify: `webapp/src/views/ChatView.vue`
- Modify: `webapp/src/style.css`

**Step 1: Write the failing test**

Add a composer test that pressing `Enter` sends and pressing `Shift+Enter` inserts a newline. Add message list tests or DOM assertions covering animated loading affordances for thinking/tool cards while reply remains visually larger.

**Step 2: Run test to verify it fails**

Run: `npm test -- MessageComposer.spec.ts MessageList.spec.ts`
Expected: FAIL because keyboard handling and animated compact card styling are missing.

**Step 3: Write minimal implementation**

Handle textarea keydown to submit on bare Enter, preserve newline on `Shift+Enter`, and update CSS for sidebar collapse, compact cards, merged tool card details, and lightweight animated loading placeholders/spinners for non-reply entries.

**Step 4: Run test to verify it passes**

Run: `npm test -- MessageComposer.spec.ts MessageList.spec.ts`
Expected: PASS.

### Task 5: Final verification

**Files:**
- Modify: `docs/swagger/*` only if route annotations require regenerated output

**Step 1: Run focused frontend and backend tests**

Run: `go test ./core/agent ./app/handlers && npm test -- ChatView.spec.ts ConversationSidebar.spec.ts MessageList.spec.ts MessageComposer.spec.ts`
Expected: PASS.

**Step 2: Run build verification**

Run: `npm run build`
Expected: PASS.

**Step 3: Run broader safety net if needed**

Run: `go test ./...`
Expected: PASS.
