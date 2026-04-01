# Webapp Initial Scaffold Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Create a `webapp` Vue app with a no-auth login screen and a simple chat UI wired to the running Go backend via reverse proxy.

**Architecture:** Use Vite + Vue 3 + Vue Router to deliver a lightweight SPA. The app stores a temporary local username, routes into a chat page, loads conversation history from `/api/v1/conversations`, sends messages through `POST /api/v1/tasks` with `task_type=agent.run`, and refreshes messages by reading conversation/task state from the existing backend APIs.

**Tech Stack:** Node 20, pnpm, Vite, Vue 3, TypeScript, Vue Router, Vitest.

---

### Task 1: Scaffold project

**Files:**
- Create: `webapp/*`
- Modify: `webapp/package.json`

**Step 1: Verify repo root and `webapp` parent location**

Run: `dir`
Expected: repository root entries are listed and `webapp` can be created here.

**Step 2: Initialize Vue project with pnpm on Node 20**

Run: `pnpm create vite webapp --template vue-ts`
Expected: Vite scaffold files are created in `webapp`.

**Step 3: Install frontend dependencies**

Run: `pnpm install`
Expected: lockfile and dependencies are installed successfully.

### Task 2: Define app shell and routes

**Files:**
- Create: `webapp/src/router/index.ts`
- Modify: `webapp/src/main.ts`
- Modify: `webapp/src/App.vue`

**Step 1: Write failing route/bootstrap tests**

Test behaviors:
- unauthenticated access to chat redirects to login
- login route remains publicly accessible

**Step 2: Run targeted tests and confirm failure**

Run: `pnpm test -- --run`
Expected: tests fail because router/auth code is not implemented yet.

**Step 3: Implement minimal router and shell**

Use local storage backed session state and two routes: `/login` and `/chat`.

**Step 4: Run targeted tests again**

Run: `pnpm test -- --run`
Expected: route/bootstrap tests pass.

### Task 3: Add backend API helpers and chat state

**Files:**
- Create: `webapp/src/lib/api.ts`
- Create: `webapp/src/lib/chat.ts`
- Create: `webapp/src/types/api.ts`

**Step 1: Write failing tests for envelope parsing and task payload creation**

Test behaviors:
- API envelope unwrap returns `data` when `ok=true`
- API envelope unwrap throws when `ok=false`
- task request builder sends `agent.run` with provider/model/message fields

**Step 2: Run targeted tests and confirm failure**

Run: `pnpm test -- --run`
Expected: tests fail because helper functions do not exist yet.

**Step 3: Implement minimal API helpers**

Use `/api/v1` relative URLs and backend-compatible request/response typing.

**Step 4: Run targeted tests again**

Run: `pnpm test -- --run`
Expected: helper tests pass.

### Task 4: Build login and chat pages

**Files:**
- Create: `webapp/src/views/LoginView.vue`
- Create: `webapp/src/views/ChatView.vue`
- Create: `webapp/src/components/ConversationSidebar.vue`
- Create: `webapp/src/components/MessageComposer.vue`
- Create: `webapp/src/components/MessageList.vue`
- Modify: `webapp/src/style.css`

**Step 1: Implement login page**

Provide username input and a button that stores session locally and routes to chat.

**Step 2: Implement chat page**

Load conversation list, conversation messages, and send messages to backend.

**Step 3: Add reverse proxy dev config**

Modify `webapp/vite.config.ts` to proxy `/api` to `http://127.0.0.1:18080`.

**Step 4: Run tests and build**

Run: `pnpm test -- --run && pnpm build`
Expected: tests pass and production build succeeds.
