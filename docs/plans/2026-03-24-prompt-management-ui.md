# Prompt Management UI Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Improve the prompt management screen so document creation is easier and the page behaves like Chat on medium and narrow viewports.

**Architecture:** Keep the existing `AdminPromptView.vue` page, but align its sidebar behavior with `ChatView.vue` by adding the same viewport state and drawer toggle pattern. Add lightweight draft logic so prompt document IDs auto-generate from the name while still allowing manual overrides.

**Tech Stack:** Vue 3, TypeScript, Vitest, Vue Test Utils, existing shared CSS in `webapp/src/style.css`

---

### Task 1: Add failing tests for new prompt document behavior

**Files:**
- Modify: `webapp/src/views/AdminPromptView.spec.ts`

**Step 1:** Add a test asserting that when creating a document and entering only a name, the ID field auto-populates with a slugified value.

**Step 2:** Verify the created document payload uses the auto-generated ID if the user did not manually override it.

### Task 2: Add failing tests for responsive sidebar behavior

**Files:**
- Modify: `webapp/src/views/AdminPromptView.spec.ts`

**Step 1:** Add a mobile-width helper like the one in `ChatView.spec.ts`.

**Step 2:** Add a test asserting that on narrow viewports the topbar shows a sidebar toggle, the prompt sidebar opens as a drawer, and a backdrop appears.

### Task 3: Implement prompt management view logic changes

**Files:**
- Modify: `webapp/src/views/AdminPromptView.vue`

**Step 1:** Add viewport state (`sidebarCollapsed`, `sidebarMobile`, `sidebarDrawerOpen`) and the same toggle/resize behavior used in Chat.

**Step 2:** Add document draft metadata to detect whether the ID has been manually edited.

**Step 3:** Add slug generation from the document name for new documents only, preserving manual edits.

**Step 4:** Update labels/help text so `Scope` reads as a scope tag instead of a user-only permission concept.

### Task 4: Implement layout and style updates

**Files:**
- Modify: `webapp/src/views/AdminPromptView.vue`

**Step 1:** Make the sidebar header action button match the icon-button style used in Chat.

**Step 2:** Reduce the height footprint of the upper document fields and give the content textarea more space.

**Step 3:** Ensure the stage content and cards scroll correctly vertically.

**Step 4:** Add mobile and medium-width layout rules so the sidebar behaves like the chat drawer and the cards stack cleanly.

### Task 5: Verify

**Files:**
- Modify: none

**Step 1:** Run `npm test -- src/views/AdminPromptView.spec.ts` from `webapp`.

**Step 2:** If needed, run the broader frontend test target already used by the repo.
