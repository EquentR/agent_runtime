# Auth UI And Session Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a Chinese login/register experience to the webapp and implement backend username/password authentication with cookie session support behind the existing Vite proxy.

**Architecture:** Keep the change inside the current app/core/pkg boundaries. The backend adds an app-level user model, auth logic, auth/session middleware, migration, and auth handlers under `/api/v1/auth`. The frontend replaces the current localStorage-only fake login with real session-based auth checks and Chinese login/register forms.

**Tech Stack:** Go, Gin, Gorm + SQLite, Vue 3, Vue Router, Vitest, cookie-based session

---

### Task 1: Add failing backend auth handler tests

**Files:**
- Modify: `app/handlers/task_handler_test.go`
- Create: `app/handlers/auth_handler_test.go`

**Step 1: Write the failing test**

Write tests for:
- register success with username + password + confirm password
- register reject duplicated username
- login success sets session cookie
- login reject wrong password
- current session endpoint returns logged-in user
- logout clears session cookie
- protected conversation/task endpoints reject anonymous requests

**Step 2: Run test to verify it fails**

Run: `go test ./app/handlers -run 'TestAuthHandler|TestConversationHandler|TestTaskHandler'`
Expected: FAIL because auth handler, middleware, and user persistence do not exist yet.

**Step 3: Write minimal implementation**

Create the smallest handler/middleware/model surface needed to satisfy the first failing assertions.

**Step 4: Run test to verify it passes**

Run: `go test ./app/handlers -run 'TestAuthHandler|TestConversationHandler|TestTaskHandler'`
Expected: PASS

### Task 2: Add user persistence and migration

**Files:**
- Create: `app/models/user.go`
- Modify: `app/migration/define.go`
- Modify: `app/migration/init.go`

**Step 1: Write the failing test**

Extend handler tests so they require database persistence across register/login lookups and uniqueness checks.

**Step 2: Run test to verify it fails**

Run: `go test ./app/handlers -run TestAuthHandler`
Expected: FAIL with missing user table / lookup behavior.

**Step 3: Write minimal implementation**

Add a `users` table with unique username, password hash, timestamps, and migrate it through app migration registration.

**Step 4: Run test to verify it passes**

Run: `go test ./app/handlers -run TestAuthHandler`
Expected: PASS

### Task 3: Add auth logic, session store, and protected API wiring

**Files:**
- Create: `app/logics/auth_logic.go`
- Create: `app/handlers/auth_handler.go`
- Create: `app/handlers/auth_types.go`
- Create: `app/handlers/auth_middleware.go`
- Modify: `app/router/deps.go`
- Modify: `app/router/init.go`
- Modify: `app/commands/serve.go`

**Step 1: Write the failing test**

Require:
- secure password verification
- cookie session read/write
- anonymous request rejection on chat-related APIs
- successful requests expose current username

**Step 2: Run test to verify it fails**

Run: `go test ./app/handlers -run 'TestAuthHandler|TestConversationHandler|TestTaskHandler'`
Expected: FAIL because middleware and session behavior are missing.

**Step 3: Write minimal implementation**

Implement register/login/logout/me endpoints and cookie session middleware. Ensure the cookie works through Vite reverse proxy by using same-origin API paths, `HttpOnly`, `Path=/`, and frontend requests with credentials.

**Step 4: Run test to verify it passes**

Run: `go test ./app/handlers -run 'TestAuthHandler|TestConversationHandler|TestTaskHandler'`
Expected: PASS

### Task 4: Replace fake frontend session with real auth API

**Files:**
- Modify: `webapp/src/lib/api.ts`
- Modify: `webapp/src/lib/session.ts`
- Modify: `webapp/src/lib/session.spec.ts`
- Modify: `webapp/src/router/index.ts`
- Modify: `webapp/src/router/index.spec.ts`

**Step 1: Write the failing test**

Add tests for:
- auth helpers calling `/auth/register`, `/auth/login`, `/auth/logout`, `/auth/me`
- router guard waiting for real session state instead of localStorage-only state

**Step 2: Run test to verify it fails**

Run: `pnpm --dir webapp test -- --run src/lib/session.spec.ts src/router/index.spec.ts`
Expected: FAIL because helpers still use localStorage-only auth.

**Step 3: Write minimal implementation**

Switch auth helpers to real HTTP calls, preserve a lightweight cached username if needed for UI, and make all API/EventSource requests session-aware.

**Step 4: Run test to verify it passes**

Run: `pnpm --dir webapp test -- --run src/lib/session.spec.ts src/router/index.spec.ts`
Expected: PASS

### Task 5: Rebuild login/register view in Chinese with smaller visual scale

**Files:**
- Modify: `webapp/src/views/LoginView.vue`
- Modify: `webapp/src/views/ChatView.vue`
- Modify: `webapp/src/views/ChatView.spec.ts`

**Step 1: Write the failing test**

Add tests covering:
- Chinese login/register labels
- register form with username, password, confirm password only
- successful login/register route to `/chat`
- logout returns user to `/login`

**Step 2: Run test to verify it fails**

Run: `pnpm --dir webapp test -- --run src/views/ChatView.spec.ts`
Expected: FAIL because current page is English demo access only.

**Step 3: Write minimal implementation**

Build a compact two-panel or tabbed Chinese auth card, reduce oversized spacing, and keep the visual language aligned with the existing app.

**Step 4: Run test to verify it passes**

Run: `pnpm --dir webapp test -- --run src/views/ChatView.spec.ts`
Expected: PASS

### Task 6: Verify end-to-end behavior

**Files:**
- Modify as needed based on verification output

**Step 1: Run targeted verification**

Run:
- `go test ./app/handlers ./app/migration`
- `pnpm --dir webapp test -- --run`

**Step 2: Run full verification**

Run:
- `go test ./...`
- `pnpm --dir webapp build`

**Step 3: Confirm behavior**

Verify:
- anonymous access to chat APIs is rejected
- register/login/logout works with cookie session
- Vite proxied requests keep session
- login page is Chinese and visually more compact
