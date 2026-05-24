# User Workspace Isolation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 给每个用户提供永久 home workspace 和每次运行的 task workspace，让 `agent.run` 在各自工作区内读写、确认后再整目录回写，同时保留未来 sandbox 后端的抽象边界。

**Architecture:** `workspace/` 继续作为系统模板源，但只复制 `AGENTS.md` 和 `skills/`；真正的用户数据落在 `data/workspaces/users/{user_id}/home` 与 `.../tasks/{task_id}`。`core/workspaces` 负责模板初始化、快照复制、确认合并、放弃保留和路径安全，`TaskHandler` / `SkillHandler` / `agent executor` 只消费解析后的 workspace context，不再依赖全局共享根目录。

**Tech Stack:** Go 1.25, Gin, GORM, filesystem IO, Vue 3, TypeScript, Vite.

---

### Task 1: Workspace Core and Template Seed

**Files:**
- Create: `workspace/AGENTS.md`
- Create: `core/workspaces/types.go`
- Create: `core/workspaces/manager.go`
- Create: `core/workspaces/copy.go`
- Create: `core/workspaces/manager_test.go`
- Modify: `app/config/app.go`
- Modify: `conf/app.yaml`

- [ ] **Step 1: Write the failing tests**

在 `manager_test.go` 里定义几个很小的本地测试助手即可：`mustWriteFile`、`assertFileExists`、`assertFileNotExists`、`assertDirExists`、`assertFileContent`。

```go
func TestManagerSeedsHomeWorkspaceFromTemplateOnlyAGENTSandSkills(t *testing.T) {
	templateRoot := t.TempDir()
	dataRoot := t.TempDir()

	mustWriteFile(t, filepath.Join(templateRoot, "AGENTS.md"), "seed rules")
	mustWriteFile(t, filepath.Join(templateRoot, "skills", "debugging", "SKILL.md"), "---\ndescription: Debug skill\n---\n")
	mustWriteFile(t, filepath.Join(templateRoot, "notes.txt"), "ignore me")

	mgr := NewManager(Config{TemplateRoot: templateRoot, Root: dataRoot})
	homeRoot, err := mgr.EnsureHomeWorkspace(context.Background(), "42")
	if err != nil {
		t.Fatalf("EnsureHomeWorkspace() error = %v", err)
	}

	assertFileExists(t, filepath.Join(homeRoot, "AGENTS.md"))
	assertFileExists(t, filepath.Join(homeRoot, "skills", "debugging", "SKILL.md"))
	assertFileNotExists(t, filepath.Join(homeRoot, "notes.txt"))
}
```

Run: `go test ./core/workspaces -run '^TestManagerSeedsHomeWorkspaceFromTemplateOnlyAGENTSandSkills$' -v`
Expected: fail until `NewManager`, `EnsureHomeWorkspace`, and the seed copy rules exist.

- [ ] **Step 2: Add the snapshot and merge tests**

```go
func TestManagerCreatesTaskSnapshotAndConfirmMergeBacksUpHome(t *testing.T) {
	templateRoot := t.TempDir()
	dataRoot := t.TempDir()
	mustWriteFile(t, filepath.Join(templateRoot, "AGENTS.md"), "template rules")
	mustWriteFile(t, filepath.Join(templateRoot, "skills", "review", "SKILL.md"), "---\ndescription: Review skill\n---\n")

	mgr := NewManager(Config{TemplateRoot: templateRoot, Root: dataRoot})
	homeRoot, _ := mgr.EnsureHomeWorkspace(context.Background(), "42")
	mustWriteFile(t, filepath.Join(homeRoot, "notes.txt"), "home draft")

	taskRoot, err := mgr.CreateTaskWorkspace(context.Background(), "42", "tsk_1", ModeMutable)
	if err != nil {
		t.Fatalf("CreateTaskWorkspace() error = %v", err)
	}
	mustWriteFile(t, filepath.Join(taskRoot, "notes.txt"), "task changes")

	if err := mgr.ConfirmTaskWorkspace(context.Background(), "42", "tsk_1"); err != nil {
		t.Fatalf("ConfirmTaskWorkspace() error = %v", err)
	}

	assertFileContent(t, filepath.Join(homeRoot, "notes.txt"), "task changes")
	assertDirExists(t, filepath.Join(dataRoot, "users", "42", "backups"))
}
```

Run: `go test ./core/workspaces -run '^TestManagerCreatesTaskSnapshotAndConfirmMergeBacksUpHome$' -v`
Expected: fail until task snapshots, backup, and whole-directory replacement exist.

- [ ] **Step 3: Implement the minimal workspace manager**

Implement a small `core/workspaces` package with:
- `Config` for `TemplateRoot` and `Root`
- `Mode` values for `mutable` and `readonly`
- `State` values for `pending_merge`, `merged`, and `discarded`
- `Manager` methods for home seeding, task snapshot creation, confirmation merge, discard, and safe path resolution
- a hidden `.workspace-state.json` sidecar inside each task workspace so confirm/discard stays idempotent across restarts
- copy rules that only seed `AGENTS.md` and `skills/` from the template root and reject symlinks / absolute escapes

Update `app/config/app.go` and `conf/app.yaml` so `workspaceDir` is explicitly the template root and `workspaces.root` defaults to `data/workspaces`.

- [ ] **Step 4: Run the focused tests and clean up**

Run:
```bash
go test ./core/workspaces ./app/config -v
```

Expected: all workspace core and config tests pass, and the new template path resolves cleanly.

- [ ] **Step 5: Commit**

```bash
git add workspace/AGENTS.md core/workspaces app/config/app.go conf/app.yaml
git commit -m "feat: add per-user workspace core"
```

### Task 2: Backend Workspace Wiring and Access Control

**Files:**
- Modify: `app/commands/serve.go`
- Modify: `app/router/deps.go`
- Modify: `app/router/init.go`
- Modify: `app/handlers/skill_handler.go`
- Modify: `app/handlers/task_handler.go`
- Modify: `core/agent/executor.go`
- Create: `app/handlers/admin_workspace_handler.go`
- Create: `app/handlers/admin_workspace_handler_test.go`
- Modify: `app/handlers/skill_handler_test.go`
- Modify: `core/agent/executor_test.go`
- Modify: `app/handlers/task_handler_test.go`

- [ ] **Step 1: Write the failing backend tests**

Add tests that prove:
- `SkillHandler` lists skills from the current authenticated user’s home workspace, not the global template root
- `TaskHandler` canonicalizes `workspace_user_id` from auth and ignores spoofed input
- `agent executor` creates a per-task snapshot root from the workspace manager, keeps `UserID` spoofing out of model resolution, and uses the task workspace for prompt/skills/tool execution
- `admin workspace` summary routes reject non-admins and return the target user’s home/task state for admins

Run:
```bash
go test ./app/handlers ./core/agent -run 'TestSkillHandler|TestTaskHandler|TestAgentExecutor|TestAdminWorkspace' -v
```
Expected: initial failures until the workspace manager is threaded through the runtime and handlers.

- [ ] **Step 2: Thread workspace resolution through the server**

Implement `WorkspaceManager` construction in `app/commands/serve.go`, pass it through `router.Dependencies`, and teach `router.Init` to instantiate:
- `SkillHandler` from the current user’s home workspace
- `TaskHandler` with workspace-aware task creation and merge/discard actions
- `AdminWorkspaceHandler` for read-only inspection

Keep `core/agent/executor.go` workspace-aware by resolving the current user’s home workspace, creating a per-task snapshot root, and building the task-specific tool registry / skills loader from that root. Preserve the existing spoofed `UserID` test behavior, but add a separate internal `workspace_user_id` field for filesystem isolation.

- [ ] **Step 3: Add the workspace-aware handler endpoints**

`TaskHandler` should expose:
- `POST /tasks` with `workspace_mode` in the request body
- `POST /tasks/:id/workspace/confirm`
- `POST /tasks/:id/workspace/discard`

`AdminWorkspaceHandler` should expose a read-only summary endpoint for:
- home workspace root
- task workspace roots
- merge / discard state

The handler layer should treat `workspace_user_id` and `workspace_mode` as server-derived or server-validated values only.

- [ ] **Step 4: Run the backend test slice**

Run:
```bash
go test ./app/handlers ./core/agent ./core/skills -v
```

Expected: workspace-aware skill listing, task snapshot creation, merge/discard routes, and admin summary tests all pass.

- [ ] **Step 5: Commit**

```bash
git add app/commands/serve.go app/router/deps.go app/router/init.go app/handlers/skill_handler.go app/handlers/task_handler.go app/handlers/admin_workspace_handler.go core/agent/executor.go app/handlers/skill_handler_test.go app/handlers/task_handler_test.go app/handlers/admin_workspace_handler_test.go core/agent/executor_test.go
git commit -m "feat: route runtime through per-user workspaces"
```

### Task 3: Frontend Workspace Mode and Merge Confirmation

**Files:**
- Modify: `webapp/src/types/api.ts`
- Modify: `webapp/src/lib/api.ts`
- Modify: `webapp/src/lib/api.spec.ts`
- Modify: `webapp/src/lib/chat-state.ts`
- Modify: `webapp/src/lib/chat-state.spec.ts`
- Modify: `webapp/src/lib/task-runtime.ts`
- Modify: `webapp/src/views/ChatView.vue`
- Modify: `webapp/src/views/ChatView.spec.ts`

- [ ] **Step 1: Write the failing frontend tests**

Cover these behaviors:
- `buildRunTaskRequest()` includes `workspace_mode`
- task normalization preserves `workspace_mode` and `workspace_state`
- chat state remembers the selected workspace mode per conversation
- `ChatView` shows a merge confirmation banner when the current task result is `pending_merge`
- the banner’s confirm/discard buttons call the new API helpers

Run:
```bash
pnpm --dir webapp exec vitest run src/lib/api.spec.ts src/lib/chat-state.spec.ts src/views/ChatView.spec.ts
```
Expected: fail until the new request/result fields and merge banner flow exist.

- [ ] **Step 2: Add the request/result fields and API helpers**

Extend the shared API types with:
- `WorkspaceMode` = `mutable | readonly`
- `WorkspaceState` = `pending_merge | merged | discarded`
- `workspace_mode` on `RunTaskRequest.input` and `TaskInput`
- `workspace_state` on `RunTaskResult`

Add `confirmTaskWorkspaceMerge(taskId)` and `discardTaskWorkspaceChanges(taskId)` helpers in `webapp/src/lib/api.ts`, plus normalization for the new fields in `normalizeTaskInput()` and `normalizeRunTaskResult()`.

- [ ] **Step 3: Add the chat-state and task-runtime glue**

Persist the selected workspace mode per conversation in `webapp/src/lib/chat-state.ts`, and add a tiny helper in `webapp/src/lib/task-runtime.ts` that detects `pending_merge` tasks from normalized task details.

`ChatView.vue` should:
- default new conversations to `mutable`
- keep the user’s mode selection per conversation
- show a compact merge banner above the composer when a task is waiting for confirmation
- reload task details after confirm/discard and clear the pending banner state

- [ ] **Step 4: Run the frontend verification slice**

Run:
```bash
pnpm --dir webapp exec vue-tsc -b
pnpm --dir webapp exec vitest run src/lib/api.spec.ts src/lib/chat-state.spec.ts src/views/ChatView.spec.ts
```

Expected: typecheck passes and the new merge/mode tests pass.

- [ ] **Step 5: Commit**

```bash
git add webapp/src/types/api.ts webapp/src/lib/api.ts webapp/src/lib/api.spec.ts webapp/src/lib/chat-state.ts webapp/src/lib/chat-state.spec.ts webapp/src/lib/task-runtime.ts webapp/src/views/ChatView.vue webapp/src/views/ChatView.spec.ts
git commit -m "feat: add workspace mode and merge confirmation ui"
```
