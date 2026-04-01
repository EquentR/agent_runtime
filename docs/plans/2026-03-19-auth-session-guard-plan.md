# Auth Session Guard Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ensure protected frontend pages always validate the backend session and make protected backend APIs opt into reusable Gin auth middleware at registration time.

**Architecture:** Keep cookie-based session ownership on the backend and local username caching on the frontend, but stop treating local cache as authoritative for protected pages. Reuse the existing `pkg/rest` wrapper option model so handlers can attach auth middleware at group level or per route without duplicating auth checks in handler bodies.

**Tech Stack:** Go, Gin, GORM, Vue 3, Vue Router, Vitest

---

### Task 1: Frontend protected-route session validation

**Files:**
- Create: `webapp/src/router/index.spec.ts`
- Modify: `webapp/src/router/index.ts`
- Test: `webapp/src/router/index.spec.ts`

**Step 1: Write the failing test**
- Add a router test proving that navigating to `/chat` calls `syncSession(true)` even when `localStorage` already contains a cached username.
- Add a router test proving that a failed forced sync redirects the user to `/login` and clears the stale local cache.

**Step 2: Run test to verify it fails**
- Run: `pnpm --dir webapp test -- src/router/index.spec.ts`
- Expected: FAIL because the current guard skips backend validation when local cache exists.

**Step 3: Write minimal implementation**
- Update `webapp/src/router/index.ts` so routes with `meta.requiresSession` always call `syncSession(true)` before deciding access.
- Keep `/login` redirection behavior aligned with the validated session state.

**Step 4: Run test to verify it passes**
- Run: `pnpm --dir webapp test -- src/router/index.spec.ts src/lib/session.spec.ts`
- Expected: PASS.

### Task 2: Backend reusable auth wrapper option

**Files:**
- Modify: `app/handlers/auth_middleware.go`
- Modify: `app/handlers/auth_handler.go`
- Modify: `app/handlers/auth_handler_test.go`
- Test: `app/handlers/auth_handler_test.go`

**Step 1: Write the failing test**
- Add a handler test proving anonymous `/api/v1/auth/me` requests are rejected by middleware before handler logic.
- Add a handler test proving authenticated `/api/v1/auth/me` still succeeds.

**Step 2: Run test to verify it fails**
- Run: `go test ./app/handlers -run 'TestAuthMiddlewareRejectsAnonymousCurrentUserRequest|TestAuthHandlerRegisterLoginLogoutFlow'`
- Expected: FAIL if the test asserts middleware-based protection that is not yet wired through route options.

**Step 3: Write minimal implementation**
- Add a helper on `AuthMiddleware` that returns a `resp.WrapperOption` using `RequireSession()`.
- Register `/auth/me` with that wrapper option so auth stays declarative at route registration time.

**Step 4: Run test to verify it passes**
- Run: `go test ./app/handlers -run 'TestAuthMiddlewareRejectsAnonymousCurrentUserRequest|TestAuthHandlerRegisterLoginLogoutFlow'`
- Expected: PASS.

### Task 3: Broader verification

**Files:**
- Modify: `app/router/init.go` only if route registration needs cleanup after middleware helper adoption.

**Step 1: Run focused backend tests**
- Run: `go test ./app/handlers`

**Step 2: Run focused frontend tests**
- Run: `pnpm --dir webapp test -- --runInBand src/router/index.spec.ts src/lib/session.spec.ts`

**Step 3: Run build-level verification**
- Run: `pnpm --dir webapp build`
- Run: `go test ./...`

**Step 4: Review for scope control**
- Confirm only protected-page validation and reusable auth middleware registration changed.
- Avoid unrelated auth model, DB, or session lifetime changes.
