# Admin Role And Audit UI Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make the first registered user a system administrator who can read all conversations and audit records, while ordinary users remain limited to their own data, and add a minimal web UI to inspect the result.

**Architecture:** Extend the existing auth model with a persisted role on `users`, assign `admin` atomically when creating the first account, and centralize admin-aware access checks in handlers so existing store APIs can remain simple. Reuse the existing Vue `webapp` and session flow by adding a lightweight admin audit view and role-aware navigation instead of building a separate frontend.

**Tech Stack:** Go, GORM, Gin, SQLite, Vue 3, TypeScript, Vite, Vitest.

---

### Task 1: Add Persisted User Roles And First-User Admin Assignment

**Files:**
- Modify: `app/models/user.go`
- Modify: `app/logics/auth_logic.go`
- Modify: `app/migration/define.go`
- Test: `app/logics/auth_logic_test.go`

**Step 1: Write the failing test**

Add tests covering:
- the first registered user gets role `admin`
- the second registered user gets role `user`
- login/current user returns the persisted role

**Step 2: Run test to verify it fails**

Run: `go test ./app/logics -run 'TestAuthLogic(RegisterAssignsAdminToFirstUser|RegisterAssignsUserRoleToLaterUsers|CurrentUserIncludesRole)' -v`
Expected: FAIL because `User` has no role field and registration does not assign roles.

**Step 3: Write minimal implementation**

Add a `Role` field to `models.User`, define role constants in the auth layer or models layer, and assign the role inside a transaction that counts existing users before insert so only the first successful registration becomes admin.

**Step 4: Run test to verify it passes**

Run: `go test ./app/logics -run 'TestAuthLogic(RegisterAssignsAdminToFirstUser|RegisterAssignsUserRoleToLaterUsers|CurrentUserIncludesRole)' -v`
Expected: PASS.

### Task 2: Make Conversation And Audit Access Admin-Aware

**Files:**
- Modify: `app/handlers/conversation_handler.go`
- Modify: `app/handlers/audit_handler.go`
- Modify: `app/handlers/auth_handler.go`
- Test: `app/handlers/conversation_handler_test.go`
- Test: `app/handlers/audit_handler_test.go`

**Step 1: Write the failing test**

Add tests covering:
- admin can list conversations from other users
- admin can view/delete another user's conversation
- admin can read another user's audit run and replay
- normal users remain denied cross-user access
- `/auth/me` exposes the role to the webapp

**Step 2: Run test to verify it fails**

Run: `go test ./app/handlers -run 'Test(ConversationHandler|AuditHandler|AuthHandler)' -v`
Expected: FAIL on the new admin expectations because handlers only compare `CreatedBy` with the current username and auth responses do not include role.

**Step 3: Write minimal implementation**

Introduce a small helper that treats `admin` as full read access for conversations and audits, while preserving owner checks for ordinary users. Extend auth responses to include the current user's role.

**Step 4: Run test to verify it passes**

Run: `go test ./app/handlers -run 'Test(ConversationHandler|AuditHandler|AuthHandler)' -v`
Expected: PASS.

### Task 3: Extend Web API Types And Session State For Roles

**Files:**
- Modify: `webapp/src/types/api.ts`
- Modify: `webapp/src/lib/session.ts`
- Modify: `webapp/src/lib/session.spec.ts`
- Modify: `webapp/src/lib/api.ts`
- Test: `webapp/src/lib/api.spec.ts`

**Step 1: Write the failing test**

Add tests covering:
- session sync preserves `username` and `role`
- auth payload normalization includes role
- audit API helpers correctly fetch run, events, and replay payloads

**Step 2: Run test to verify it fails**

Run: `pnpm vitest run src/lib/session.spec.ts src/lib/api.spec.ts`
Expected: FAIL because the current session helpers only track username and the API layer has no audit helper functions or audit types.

**Step 3: Write minimal implementation**

Update web types so auth/session state includes `role`, then add simple audit fetch helpers for the existing backend endpoints.

**Step 4: Run test to verify it passes**

Run: `pnpm vitest run src/lib/session.spec.ts src/lib/api.spec.ts`
Expected: PASS.

### Task 4: Add A Minimal Admin Audit View To The Existing Webapp

**Files:**
- Modify: `webapp/src/router/index.ts`
- Modify: `webapp/src/views/ChatView.vue`
- Create: `webapp/src/views/AdminAuditView.vue`
- Create: `webapp/src/views/AdminAuditView.spec.ts`
- Modify: `webapp/src/style.css`

**Step 1: Write the failing test**

Add view/router tests covering:
- admin users see an audit/admin entry point
- non-admin users are redirected away from the admin view
- admin audit page loads conversations and selected conversation audit details

**Step 2: Run test to verify it fails**

Run: `pnpm vitest run src/views/AdminAuditView.spec.ts src/router/index.spec.ts src/views/ChatView.spec.ts`
Expected: FAIL because no admin route or audit view exists.

**Step 3: Write minimal implementation**

Add a single admin-only route that shows:
- conversation list
- selected conversation metadata
- linked audit run details and replay timeline

Keep the UI intentionally small and read-only. Reuse the existing layout language from the chat app so the new page feels native.

**Step 4: Run test to verify it passes**

Run: `pnpm vitest run src/views/AdminAuditView.spec.ts src/router/index.spec.ts src/views/ChatView.spec.ts`
Expected: PASS.

### Task 5: Verify End-To-End Behavior

**Files:**
- Modify: `docs/swagger/docs.go` (only if regenerated)
- Modify: `docs/swagger/swagger.json` (only if regenerated)
- Modify: `docs/swagger/swagger.yaml` (only if regenerated)

**Step 1: Run focused backend and frontend tests**

Run:
- `go test ./app/logics ./app/handlers`
- `pnpm vitest run`

Expected: PASS.

**Step 2: Run broader verification**

Run:
- `go test ./...`
- `go build ./cmd/...`
- `go list ./...`
- `pnpm build`

Expected: PASS.

**Step 3: Manual verification**

Check in browser:
- first registered account shows admin capabilities
- ordinary account sees only its own chat data
- admin page can inspect another user's conversation and audit replay
