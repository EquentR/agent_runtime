# Chat Responsive Sidebar Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make the chat layout hide the sidebar automatically on narrow screens, reopen it as a drawer from the chat header, remove the redundant CONVERSATION eyebrow, require delete confirmation for conversation items, and keep long conversation lists scrollable without shrinking item width.

**Architecture:** Keep the existing `ChatView` + `ConversationSidebar` split, but move responsive state control into `ChatView` so viewport mode and manual collapse stay coordinated. Treat desktop collapse and mobile drawer as separate UI states, then let the sidebar component render controls and confirmation behavior from props. Cover the changes with focused Vue/Vitest specs before implementation.

**Tech Stack:** Vue 3 script setup, Vue Test Utils, Vitest, Vite CSS

---

### Task 1: Add failing sidebar behavior tests

**Files:**
- Modify: `webapp/src/views/ChatView.spec.ts`
- Modify: `webapp/src/components/ConversationSidebar.spec.ts`

**Step 1: Write the failing test**

Add a `ChatView` spec that simulates a narrow viewport and verifies the sidebar is hidden by default, a header toggle button is shown, and clicking it opens drawer mode instead of stacking the sidebar below the chat area.

Add a `ConversationSidebar` spec that verifies delete clicks first request confirmation and only emit `delete` after confirmation succeeds.

**Step 2: Run test to verify it fails**

Run: `npm test -- src/views/ChatView.spec.ts src/components/ConversationSidebar.spec.ts`
Expected: FAIL because the current UI still stacks the sidebar under the chat section on small screens and delete emits immediately.

**Step 3: Write minimal implementation**

No production changes in this task.

**Step 4: Run test to verify it still fails for the expected reason**

Run: `npm test -- src/views/ChatView.spec.ts src/components/ConversationSidebar.spec.ts`
Expected: FAIL with responsive/delete behavior mismatches, not syntax errors.

### Task 2: Implement coordinated desktop/mobile sidebar states

**Files:**
- Modify: `webapp/src/views/ChatView.vue`
- Modify: `webapp/src/components/ConversationSidebar.vue`
- Modify: `webapp/src/style.css`

**Step 1: Write the failing test**

Extend `ChatView.spec.ts` with an assertion that the redundant topbar eyebrow text is gone and only the current conversation title remains visible.

**Step 2: Run test to verify it fails**

Run: `npm test -- src/views/ChatView.spec.ts`
Expected: FAIL because the topbar still renders `Conversation` and the mobile drawer controls are missing.

**Step 3: Write minimal implementation**

In `ChatView.vue`, track whether the viewport is in mobile mode, whether the drawer is open, and whether desktop collapse is active. Use a left-top toggle in the chat header to open the drawer on mobile and keep the existing collapse toggle only for desktop behavior.

In `ConversationSidebar.vue`, accept drawer/mobile props, render overlay-dismiss affordances, and preserve item rendering without compressing content widths.

In `style.css`, replace the narrow-screen stacked layout with an overlay drawer pattern and ensure the sidebar list scrolls independently.

**Step 4: Run test to verify it passes**

Run: `npm test -- src/views/ChatView.spec.ts src/components/ConversationSidebar.spec.ts`
Expected: PASS.

### Task 3: Refine scroll behavior and final verification

**Files:**
- Modify: `webapp/src/style.css`
- Test: `webapp/src/views/ChatView.spec.ts`
- Test: `webapp/src/components/ConversationSidebar.spec.ts`

**Step 1: Write the failing test**

If needed, add a sidebar spec asserting the list container keeps `.sidebar-list` and the item title remains rendered with truncation instead of collapsing to unusable widths.

**Step 2: Run test to verify it fails**

Run: `npm test -- src/components/ConversationSidebar.spec.ts`
Expected: FAIL if the scrollable list or truncation contract is not preserved.

**Step 3: Write minimal implementation**

Adjust CSS so the sidebar card widths stay stable, the list gains its own scroll area, and mobile layout keeps the chat area full-width when the drawer is closed.

**Step 4: Run test to verify it passes**

Run: `npm test -- src/views/ChatView.spec.ts src/components/ConversationSidebar.spec.ts && npm run build`
Expected: PASS and successful webapp build.
