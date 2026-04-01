# Admin Prompt Element Plus Redesign Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the current hand-crafted prompt management screen with an Element Plus-based admin UI that gives prompt editing more space and moves scene binding management into a dedicated dialog.

**Architecture:** Keep the existing data-loading and CRUD logic from `AdminPromptView.vue`, but rebuild the presentation layer with Element Plus components. The main page will focus on prompt document selection and prompt editing, while scene bindings move behind a top-right action that opens a large split dialog with a table on the left and a form on the right.

**Tech Stack:** Vue 3, TypeScript, Element Plus, Vitest, Vue Test Utils

---

### Task 1: Update frontend bootstrap for Element Plus

**Files:**
- Modify: `webapp/src/main.ts`
- Modify: `webapp/package.json`

**Step 1:** Import Element Plus and its stylesheet in the app bootstrap.

**Step 2:** Register Element Plus on the Vue app instance.

### Task 2: Rewrite prompt admin tests around the new dialog-driven UX

**Files:**
- Modify: `webapp/src/views/AdminPromptView.spec.ts`

**Step 1:** Add tests for the prompt editor header action opening a scene binding dialog.

**Step 2:** Update binding CRUD tests so they operate inside the dialog instead of the page body.

**Step 3:** Keep tests for prompt document CRUD, floating toasts, tooltip help, responsive sidebar, and textarea auto-grow, adjusting selectors to the new layout.

### Task 3: Rebuild AdminPromptView with Element Plus components

**Files:**
- Modify: `webapp/src/views/AdminPromptView.vue`

**Step 1:** Keep the current sidebar/document selection behavior, but rename the main card to “提示词编辑” and give the content editor more room.

**Step 2:** Replace custom prompt document form controls with Element Plus form controls (`ElForm`, `ElInput`, `ElSelect`, `ElSwitch` or checkbox-equivalent only if it matches the field intent, `ElButton`, `ElTooltip`).

**Step 3:** Move scene binding management into a large `ElDialog` launched from the prompt editor header.

**Step 4:** Implement the binding list with `ElTable` on the left and a single-column binding form on the right.

**Step 5:** Use `ElMessage` for success/error feedback instead of inline banners or custom toasts.

### Task 4: Polish the remaining UX details

**Files:**
- Modify: `webapp/src/views/AdminPromptView.vue`

**Step 1:** Make the prompt content textarea auto-grow from a default 8-row height and add a half-line buffer so scrollbars do not appear prematurely.

**Step 2:** Make the “默认绑定” field visually consistent with the rest of the form.

**Step 3:** Preserve mobile sidebar behavior and keep delete confirmations aligned with the existing chat confirmation pattern unless Element Plus offers a cleaner confirmed-action flow that matches the admin context.

### Task 5: Verify

**Files:**
- Modify: none

**Step 1:** Run `npm test -- src/views/AdminPromptView.spec.ts` from `webapp`.

**Step 2:** Run `npm run build` from `webapp`.
