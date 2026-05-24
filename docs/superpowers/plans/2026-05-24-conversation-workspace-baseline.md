# Conversation Workspace Baseline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make mutable workspaces conversation-scoped, keep changes cumulative across turns, and only prompt merge when the conversation workspace differs from its baseline manifest.

**Architecture:** Keep `core/tasks` as the per-run execution system, but make `core/workspaces` accept a stable workspace id that is normally the `conversation_id`. Store a `.workspace-baseline.json` sidecar beside `.workspace-state.json`, compare current workspace content against that baseline when mutable runs finish, enforce one pending merge per user home, and expose conversation-level workspace state APIs so the frontend does not depend on localStorage for merge prompts.

**Tech Stack:** Go 1.25, Gin, filesystem IO, SHA-256 manifests, Vue 3, TypeScript, Vitest.

---

### Task 1: Baseline Manifest Support in `core/workspaces`

**Files:**
- Modify: `core/workspaces/types.go`
- Modify: `core/workspaces/manager.go`
- Modify: `core/workspaces/copy.go`
- Modify: `core/workspaces/manager_test.go`

- [ ] **Step 1: Write failing manifest tests**

Add these tests to `core/workspaces/manager_test.go`.

```go
func TestManagerDoesNotEnterPendingMergeWhenWorkspaceMatchesBaseline(t *testing.T) {
	templateRoot := t.TempDir()
	dataRoot := t.TempDir()
	writeFile(t, templateRoot, "AGENTS.md", "template rules")

	mgr, err := NewManager(Config{TemplateRoot: templateRoot, Root: dataRoot})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	workspace, err := mgr.CreateTaskWorkspace(context.Background(), "42", "conv_1", ModeMutable)
	if err != nil {
		t.Fatalf("CreateTaskWorkspace() error = %v", err)
	}

	state, err := mgr.FinishMutableWorkspace(context.Background(), "42", "conv_1")
	if err != nil {
		t.Fatalf("FinishMutableWorkspace() error = %v", err)
	}
	if state.State == StatePendingMerge {
		t.Fatalf("state = %q, want non-pending for unchanged workspace rooted at %s", state.State, workspace.Root)
	}
}

func TestManagerEntersPendingMergeWhenWorkspaceDiffersFromBaseline(t *testing.T) {
	templateRoot := t.TempDir()
	dataRoot := t.TempDir()
	writeFile(t, templateRoot, "AGENTS.md", "template rules")

	mgr, err := NewManager(Config{TemplateRoot: templateRoot, Root: dataRoot})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	workspace, err := mgr.CreateTaskWorkspace(context.Background(), "42", "conv_1", ModeMutable)
	if err != nil {
		t.Fatalf("CreateTaskWorkspace() error = %v", err)
	}
	writeFile(t, workspace.Root, "notes.txt", "changed")

	state, err := mgr.FinishMutableWorkspace(context.Background(), "42", "conv_1")
	if err != nil {
		t.Fatalf("FinishMutableWorkspace() error = %v", err)
	}
	if state.State != StatePendingMerge {
		t.Fatalf("state = %q, want %q", state.State, StatePendingMerge)
	}
}
```

Run:

```powershell
go test ./core/workspaces -run 'TestManager(DoesNotEnterPendingMergeWhenWorkspaceMatchesBaseline|EntersPendingMergeWhenWorkspaceDiffersFromBaseline)' -v
```

Expected: fail because `FinishMutableWorkspace` and baseline tracking do not exist.

- [ ] **Step 2: Write failing confirm/discard baseline tests**

Add these tests to `core/workspaces/manager_test.go`.

```go
func TestManagerConfirmUpdatesBaselineAfterMerge(t *testing.T) {
	templateRoot := t.TempDir()
	dataRoot := t.TempDir()
	writeFile(t, templateRoot, "AGENTS.md", "template rules")

	mgr, err := NewManager(Config{TemplateRoot: templateRoot, Root: dataRoot})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	workspace, err := mgr.CreateTaskWorkspace(context.Background(), "42", "conv_1", ModeMutable)
	if err != nil {
		t.Fatalf("CreateTaskWorkspace() error = %v", err)
	}
	writeFile(t, workspace.Root, "notes.txt", "changed")
	if _, err := mgr.FinishMutableWorkspace(context.Background(), "42", "conv_1"); err != nil {
		t.Fatalf("FinishMutableWorkspace() error = %v", err)
	}
	if _, err := mgr.ConfirmTaskWorkspace(context.Background(), "42", "conv_1"); err != nil {
		t.Fatalf("ConfirmTaskWorkspace() error = %v", err)
	}

	state, err := mgr.FinishMutableWorkspace(context.Background(), "42", "conv_1")
	if err != nil {
		t.Fatalf("FinishMutableWorkspace(after confirm) error = %v", err)
	}
	if state.State == StatePendingMerge {
		t.Fatalf("state after confirm and no further edits = %q, want non-pending", state.State)
	}
}

func TestManagerDiscardRestoresHomeAndUpdatesBaseline(t *testing.T) {
	templateRoot := t.TempDir()
	dataRoot := t.TempDir()
	writeFile(t, templateRoot, "AGENTS.md", "template rules")

	mgr, err := NewManager(Config{TemplateRoot: templateRoot, Root: dataRoot})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	home, err := mgr.EnsureHomeWorkspace(context.Background(), "42")
	if err != nil {
		t.Fatalf("EnsureHomeWorkspace() error = %v", err)
	}
	writeFile(t, home.Root, "notes.txt", "home")
	workspace, err := mgr.CreateTaskWorkspace(context.Background(), "42", "conv_1", ModeMutable)
	if err != nil {
		t.Fatalf("CreateTaskWorkspace() error = %v", err)
	}
	writeFile(t, workspace.Root, "notes.txt", "changed")
	if _, err := mgr.FinishMutableWorkspace(context.Background(), "42", "conv_1"); err != nil {
		t.Fatalf("FinishMutableWorkspace() error = %v", err)
	}
	if _, err := mgr.DiscardTaskWorkspace(context.Background(), "42", "conv_1"); err != nil {
		t.Fatalf("DiscardTaskWorkspace() error = %v", err)
	}
	assertFileContent(t, workspace.Root, "notes.txt", "home")

	state, err := mgr.FinishMutableWorkspace(context.Background(), "42", "conv_1")
	if err != nil {
		t.Fatalf("FinishMutableWorkspace(after discard) error = %v", err)
	}
	if state.State == StatePendingMerge {
		t.Fatalf("state after discard and no further edits = %q, want non-pending", state.State)
	}
}
```

Run:

```powershell
go test ./core/workspaces -run 'TestManager(ConfirmUpdatesBaselineAfterMerge|DiscardRestoresHomeAndUpdatesBaseline)' -v
```

Expected: fail until confirm/discard refresh baseline and discard restores workspace from home.

- [ ] **Step 3: Write failing pending mutex and confirm conflict tests**

Add these tests to `core/workspaces/manager_test.go`.

```go
func TestManagerListsPendingMergeWorkspacesForUser(t *testing.T) {
	templateRoot := t.TempDir()
	dataRoot := t.TempDir()
	writeFile(t, templateRoot, "AGENTS.md", "template rules")

	mgr, err := NewManager(Config{TemplateRoot: templateRoot, Root: dataRoot})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	workspace, err := mgr.CreateTaskWorkspace(context.Background(), "42", "conv_1", ModeMutable)
	if err != nil {
		t.Fatalf("CreateTaskWorkspace(conv_1) error = %v", err)
	}
	writeFile(t, workspace.Root, "notes.txt", "changed")
	if _, err := mgr.FinishMutableWorkspace(context.Background(), "42", "conv_1"); err != nil {
		t.Fatalf("FinishMutableWorkspace() error = %v", err)
	}
	if _, err := mgr.CreateTaskWorkspace(context.Background(), "42", "conv_2", ModeReadonly); err != nil {
		t.Fatalf("CreateTaskWorkspace(readonly) error = %v", err)
	}

	pending, err := mgr.ListPendingMergeWorkspaces(context.Background(), "42")
	if err != nil {
		t.Fatalf("ListPendingMergeWorkspaces() error = %v", err)
	}
	if len(pending) != 1 || pending[0].TaskID != "conv_1" {
		t.Fatalf("pending = %#v, want only conv_1", pending)
	}
}

func TestManagerConfirmRejectsWhenHomeChangedSinceBaseline(t *testing.T) {
	templateRoot := t.TempDir()
	dataRoot := t.TempDir()
	writeFile(t, templateRoot, "AGENTS.md", "template rules")

	mgr, err := NewManager(Config{TemplateRoot: templateRoot, Root: dataRoot})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	home, err := mgr.EnsureHomeWorkspace(context.Background(), "42")
	if err != nil {
		t.Fatalf("EnsureHomeWorkspace() error = %v", err)
	}
	workspace, err := mgr.CreateTaskWorkspace(context.Background(), "42", "conv_1", ModeMutable)
	if err != nil {
		t.Fatalf("CreateTaskWorkspace() error = %v", err)
	}
	writeFile(t, workspace.Root, "notes.txt", "workspace change")
	if _, err := mgr.FinishMutableWorkspace(context.Background(), "42", "conv_1"); err != nil {
		t.Fatalf("FinishMutableWorkspace() error = %v", err)
	}
	writeFile(t, home.Root, "other.txt", "other conversation merged first")

	_, err = mgr.ConfirmTaskWorkspace(context.Background(), "42", "conv_1")
	if !errors.Is(err, ErrWorkspaceHomeChanged) {
		t.Fatalf("ConfirmTaskWorkspace() error = %v, want ErrWorkspaceHomeChanged", err)
	}
	assertPathMissing(t, home.Root, "notes.txt")
}
```

Run:

```powershell
go test ./core/workspaces -run 'TestManager(ListsPendingMergeWorkspacesForUser|ConfirmRejectsWhenHomeChangedSinceBaseline)' -v
```

Expected: fail because pending listing and home manifest conflict detection do not exist.

- [ ] **Step 4: Implement baseline manifest types, scanner, and conflict error**

In `core/workspaces/types.go`, add:

```go
const (
	BaselineFileName = ".workspace-baseline.json"
)

type WorkspaceManifest struct {
	Version int                      `json:"version"`
	Entries []WorkspaceManifestEntry `json:"entries"`
}

type WorkspaceManifestEntry struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Size   int64  `json:"size,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
}

var ErrWorkspaceHomeChanged = errors.New("workspace home changed since baseline")
```

In `core/workspaces/manager.go`, implement helpers with these exact behaviors:

```go
func (m *Manager) buildManifest(root string) (WorkspaceManifest, error)
func (m *Manager) loadBaseline(root string) (WorkspaceManifest, bool, error)
func (m *Manager) saveBaseline(root string, manifest WorkspaceManifest) error
func manifestsEqual(left WorkspaceManifest, right WorkspaceManifest) bool
```

Scanner rules:

- Walk `root` with `filepath.WalkDir`.
- Skip `StateFileName` and `BaselineFileName`.
- Reject symlinks with the existing symlink error style.
- Record directories with `Kind: "dir"`.
- Record files with `Kind: "file"`, `Size`, and SHA-256 hex digest.
- Sort entries by `Path`.

- [ ] **Step 5: Wire baseline into create, finish, confirm, discard, and pending listing**

Modify `CreateTaskWorkspace`:

- Existing task/conversation workspace with state should be reused, not removed.
- If baseline is missing, generate it from the existing workspace and save it.
- Newly copied workspace should save baseline immediately after copy and before state save.

Add this method to `Manager`:

```go
func (m *Manager) FinishMutableWorkspace(ctx context.Context, userID string, workspaceID string) (*WorkspaceStateFile, error)
func (m *Manager) ListPendingMergeWorkspaces(ctx context.Context, userID string) ([]TaskWorkspaceSummary, error)
```

Behavior:

- Load and normalize state for `workspaceID`.
- Reject non-mutable mode.
- Load baseline; if missing, generate and save current manifest, then mark `completed`.
- Compare current manifest to baseline.
- If equal, mark `completed`.
- If different, mark `pending_merge`.

Modify `ConfirmTaskWorkspace`:

- Before creating a backup or replacing home, build current home manifest and compare it to the loaded workspace baseline.
- If home manifest differs from baseline, return `ErrWorkspaceHomeChanged`.
- After replacing home from workspace, generate current workspace manifest and save it as baseline.
- Keep state `merged`.

Modify `DiscardTaskWorkspace`:

- Replace workspace contents from home while excluding internal sidecars from copied content.
- Generate current workspace manifest and save it as baseline.
- Mark state `discarded`.

- [ ] **Step 6: Run focused tests**

Run:

```powershell
go test ./core/workspaces -v
```

Expected: all workspace tests pass, including existing safety and merge tests.

- [ ] **Step 7: Commit**

```powershell
git add core/workspaces/types.go core/workspaces/manager.go core/workspaces/copy.go core/workspaces/manager_test.go
git commit -m "feat: track workspace baseline manifests"
```

### Task 2: Conversation-Scoped Workspace Runtime

**Files:**
- Modify: `core/agent/executor.go`
- Modify: `core/agent/executor_test.go`
- Modify: `app/handlers/task_handler.go`
- Modify: `app/handlers/task_handler_test.go`
- Modify: `app/handlers/swagger_types.go`

- [ ] **Step 1: Write failing executor tests**

Add tests to `core/agent/executor_test.go` that prove workspace identity is conversation-scoped.

```go
func TestAgentExecutorUsesConversationIDForMutableWorkspace(t *testing.T) {
	store := newConversationStoreForTest(t)
	templateRoot := t.TempDir()
	writeExecutorWorkspacePrompt(t, templateRoot, "Template prompt")
	workspacesRoot := t.TempDir()
	workspaceManager := newExecutorWorkspaceManager(t, templateRoot, workspacesRoot)
	home, err := workspaceManager.EnsureHomeWorkspace(context.Background(), "alice")
	if err != nil {
		t.Fatalf("EnsureHomeWorkspace() error = %v", err)
	}
	writeExecutorWorkspacePrompt(t, home.Root, "Alice home prompt")
	client := &stubClient{streams: []model.Stream{newStubStream(
		[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "hello"}}},
		model.Message{Role: model.RoleAssistant, Content: "hello"},
		nil,
	)}}
	executor := newTaskExecutorForTest(t, ExecutorDependencies{
		Resolver:          newExecutorResolverForTest(),
		ConversationStore: store,
		PromptResolver:    newExecutorPromptResolverForTest(t, nil),
		WorkspaceRoot:     templateRoot,
		WorkspaceManager:  workspaceManager,
		ClientFactory:     func(*coretypes.LLMProvider, *coretypes.LLMModel) (model.LlmClient, error) { return client, nil },
	})

	payload := marshalExecutorTaskInput(t, map[string]any{
		"conversation_id":   "conv_1",
		"provider_id":       "openai",
		"model_id":          "gpt-5.4",
		"message":           "write a file",
		"workspace_user_id": "alice",
		"workspace_mode":    "mutable",
	})
	task := &coretasks.Task{ID: "task_1", TaskType: "agent.run", CreatedBy: "alice", InputJSON: payload}
	if _, err := executor(context.Background(), task, nil); err != nil {
		t.Fatalf("executor() error = %v", err)
	}

	wantRoot := filepath.Join(workspacesRoot, "users", "alice", "tasks", "conv_1")
	assertExecutorFileContent(t, filepath.Join(wantRoot, "AGENTS.md"), "Alice home prompt")
	if _, err := os.Stat(filepath.Join(workspacesRoot, "users", "alice", "tasks", "task_1")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("task id workspace exists or stat error = %v, want no task-scoped workspace", err)
	}
}
```

Add a second executor test with this shape:

```go
func TestAgentExecutorReusesConversationWorkspaceAcrossTurns(t *testing.T) {
	store := newConversationStoreForTest(t)
	templateRoot := t.TempDir()
	writeExecutorWorkspacePrompt(t, templateRoot, "Template prompt")
	workspacesRoot := t.TempDir()
	workspaceManager := newExecutorWorkspaceManager(t, templateRoot, workspacesRoot)
	home, err := workspaceManager.EnsureHomeWorkspace(context.Background(), "alice")
	if err != nil {
		t.Fatalf("EnsureHomeWorkspace() error = %v", err)
	}
	writeExecutorWorkspacePrompt(t, home.Root, "Alice home prompt")

	firstClient := &stubClient{streams: []model.Stream{newStubStream(
		[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "first"}}},
		model.Message{Role: model.RoleAssistant, Content: "first"},
		nil,
	)}}
	executor := newTaskExecutorForTest(t, ExecutorDependencies{
		Resolver:          newExecutorResolverForTest(),
		ConversationStore: store,
		PromptResolver:    newExecutorPromptResolverForTest(t, nil),
		WorkspaceRoot:     templateRoot,
		WorkspaceManager:  workspaceManager,
		ClientFactory:     func(*coretypes.LLMProvider, *coretypes.LLMModel) (model.LlmClient, error) { return firstClient, nil },
	})

	payload := marshalExecutorTaskInput(t, map[string]any{
		"conversation_id":   "conv_1",
		"provider_id":       "openai",
		"model_id":          "gpt-5.4",
		"message":           "first",
		"workspace_user_id": "alice",
		"workspace_mode":    "mutable",
	})
	if _, err := executor(context.Background(), &coretasks.Task{ID: "task_1", TaskType: "agent.run", CreatedBy: "alice", InputJSON: payload}, nil); err != nil {
		t.Fatalf("executor(first) error = %v", err)
	}

	conversationRoot := filepath.Join(workspacesRoot, "users", "alice", "tasks", "conv_1")
	if err := os.WriteFile(filepath.Join(conversationRoot, "notes.txt"), []byte("from first turn"), 0o644); err != nil {
		t.Fatalf("write conversation workspace file error = %v", err)
	}

	secondClient := &stubClient{streams: []model.Stream{newStubStream(
		[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "second"}}},
		model.Message{Role: model.RoleAssistant, Content: "second"},
		nil,
	)}}
	executor = newTaskExecutorForTest(t, ExecutorDependencies{
		Resolver:          newExecutorResolverForTest(),
		ConversationStore: store,
		PromptResolver:    newExecutorPromptResolverForTest(t, nil),
		WorkspaceRoot:     templateRoot,
		WorkspaceManager:  workspaceManager,
		ClientFactory:     func(*coretypes.LLMProvider, *coretypes.LLMModel) (model.LlmClient, error) { return secondClient, nil },
	})
	secondPayload := marshalExecutorTaskInput(t, map[string]any{
		"conversation_id":   "conv_1",
		"provider_id":       "openai",
		"model_id":          "gpt-5.4",
		"message":           "second",
		"workspace_user_id": "alice",
		"workspace_mode":    "mutable",
	})
	if _, err := executor(context.Background(), &coretasks.Task{ID: "task_2", TaskType: "agent.run", CreatedBy: "alice", InputJSON: secondPayload}, nil); err != nil {
		t.Fatalf("executor(second) error = %v", err)
	}
	assertExecutorFileContent(t, filepath.Join(conversationRoot, "notes.txt"), "from first turn")
}
```

Run:

```powershell
go test ./core/agent -run 'TestAgentExecutorUsesConversationIDForMutableWorkspace|TestAgentExecutorReusesConversationWorkspaceAcrossTurns' -v
```

Expected: fail because executor currently passes `task.ID` to `CreateTaskWorkspace`.

- [ ] **Step 2: Update executor workspace info**

Modify `executorWorkspaceInfo` in `core/agent/executor.go`:

```go
type executorWorkspaceInfo struct {
	UserID      string
	WorkspaceID string
	TaskID      string
	Mode        workspaces.Mode
}
```

In `resolveExecutorWorkspace`:

- Compute `workspaceID := firstNonEmpty(input.ConversationID, task.ID)`.
- Set `info.WorkspaceID = workspaceID` and `info.TaskID = task.ID`.
- Call `CreateTaskWorkspace(ctx, workspaceUserID, workspaceID, info.Mode)`.

In `finishSuccessfulExecutorWorkspace`:

- Call `FinishMutableWorkspace(ctx, info.UserID, info.WorkspaceID)` for mutable.
- Keep readonly discard behavior, but pass `info.WorkspaceID`.

In `completeExecutorWorkspace`:

- Pass `info.WorkspaceID` to complete/discard.

- [ ] **Step 3: Write failing task handler compatibility tests**

In `app/handlers/task_handler_test.go`, add tests for confirm/discard mapping.

```go
func TestTaskWorkspaceConfirmUsesConversationIDWhenPresent(t *testing.T) {
	manager, server := newWorkspaceActionTaskHandlerTestServer(t, map[string]any{"conversation_id": "conv_1"})
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/v1/tasks/task_1/workspace/confirm", "application/json", nil)
	if err != nil {
		t.Fatalf("POST confirm error = %v", err)
	}
	defer resp.Body.Close()
	if manager.confirmTaskID != "conv_1" {
		t.Fatalf("confirm workspace id = %q, want conv_1", manager.confirmTaskID)
	}
}

func TestTaskWorkspaceConfirmFallsBackToTaskIDWithoutConversationID(t *testing.T) {
	manager, server := newWorkspaceActionTaskHandlerTestServer(t, map[string]any{})
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/v1/tasks/task_1/workspace/confirm", "application/json", nil)
	if err != nil {
		t.Fatalf("POST confirm error = %v", err)
	}
	defer resp.Body.Close()
	if manager.confirmTaskID != "task_1" {
		t.Fatalf("confirm workspace id = %q, want task_1", manager.confirmTaskID)
	}
}
```

Add this helper near `recordingTaskWorkspaceManager`:

```go
func newWorkspaceActionTaskHandlerTestServer(t *testing.T, input map[string]any) (*recordingTaskWorkspaceManager, *httptest.Server) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	store := coretasks.NewStore(db)
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("task AutoMigrate() error = %v", err)
	}
	manager := coretasks.NewManager(store, coretasks.ManagerOptions{RunnerID: "workspace-action-test"})
	inputJSON, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("json.Marshal(input) error = %v", err)
	}
	if _, err := store.CreateTask(context.Background(), coretasks.CreateTaskInput{
		ID:        "task_1",
		TaskType:  "agent.run",
		Input:     json.RawMessage(inputJSON),
		CreatedBy: "tester",
	}); err != nil {
		t.Fatalf("CreateTask() error = %v", err)
	}
	workspaces := &recordingTaskWorkspaceManager{}
	engine := rest.Init()
	NewTaskHandler(manager, nil).WithWorkspaceManager(workspaces).Register(engine.Group("/api/v1"))
	return workspaces, httptest.NewServer(engine)
}
```

Run:

```powershell
go test ./app/handlers -run 'TestTaskWorkspaceConfirmUsesConversationIDWhenPresent|TestTaskWorkspaceConfirmFallsBackToTaskIDWithoutConversationID' -v
```

Expected: fail because `taskWorkspaceInput` currently always returns `task.ID`.

- [ ] **Step 4: Write failing pending mutex executor tests**

Add this test to `core/agent/executor_test.go`.

```go
func TestAgentExecutorRejectsMutableRunWhenAnotherConversationHasPendingMerge(t *testing.T) {
	store := newConversationStoreForTest(t)
	templateRoot := t.TempDir()
	writeExecutorWorkspacePrompt(t, templateRoot, "Template prompt")
	workspacesRoot := t.TempDir()
	workspaceManager := newExecutorWorkspaceManager(t, templateRoot, workspacesRoot)
	home, err := workspaceManager.EnsureHomeWorkspace(context.Background(), "alice")
	if err != nil {
		t.Fatalf("EnsureHomeWorkspace() error = %v", err)
	}
	writeExecutorWorkspacePrompt(t, home.Root, "Alice home prompt")
	pending, err := workspaceManager.CreateTaskWorkspace(context.Background(), "alice", "conv_pending", workspaces.ModeMutable)
	if err != nil {
		t.Fatalf("CreateTaskWorkspace(pending) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(pending.Root, "pending.txt"), []byte("pending change"), 0o600); err != nil {
		t.Fatalf("write pending file error = %v", err)
	}
	if _, err := workspaceManager.FinishMutableWorkspace(context.Background(), "alice", "conv_pending"); err != nil {
		t.Fatalf("FinishMutableWorkspace(pending) error = %v", err)
	}
	client := &stubClient{streams: []model.Stream{newStubStream(
		[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "hello"}}},
		model.Message{Role: model.RoleAssistant, Content: "hello"},
		nil,
	)}}
	executor := newTaskExecutorForTest(t, ExecutorDependencies{
		Resolver:          newExecutorResolverForTest(),
		ConversationStore: store,
		PromptResolver:    newExecutorPromptResolverForTest(t, nil),
		WorkspaceRoot:     templateRoot,
		WorkspaceManager:  workspaceManager,
		ClientFactory:     func(*coretypes.LLMProvider, *coretypes.LLMModel) (model.LlmClient, error) { return client, nil },
	})
	payload := marshalExecutorTaskInput(t, map[string]any{
		"conversation_id":   "conv_new",
		"provider_id":       "openai",
		"model_id":          "gpt-5.4",
		"message":           "new mutable work",
		"workspace_user_id": "alice",
		"workspace_mode":    "mutable",
	})

	_, err = executor(context.Background(), &coretasks.Task{ID: "task_new", TaskType: "agent.run", CreatedBy: "alice", InputJSON: payload}, nil)
	if err == nil || !strings.Contains(err.Error(), "pending workspace merge") {
		t.Fatalf("executor() error = %v, want pending workspace merge rejection", err)
	}
}
```

Add this companion test:

```go
func TestAgentExecutorAllowsReadonlyRunWhenAnotherConversationHasPendingMerge(t *testing.T) {
	store := newConversationStoreForTest(t)
	templateRoot := t.TempDir()
	writeExecutorWorkspacePrompt(t, templateRoot, "Template prompt")
	workspacesRoot := t.TempDir()
	workspaceManager := newExecutorWorkspaceManager(t, templateRoot, workspacesRoot)
	home, err := workspaceManager.EnsureHomeWorkspace(context.Background(), "alice")
	if err != nil {
		t.Fatalf("EnsureHomeWorkspace() error = %v", err)
	}
	writeExecutorWorkspacePrompt(t, home.Root, "Alice home prompt")
	pending, err := workspaceManager.CreateTaskWorkspace(context.Background(), "alice", "conv_pending", workspaces.ModeMutable)
	if err != nil {
		t.Fatalf("CreateTaskWorkspace(pending) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(pending.Root, "pending.txt"), []byte("pending change"), 0o600); err != nil {
		t.Fatalf("write pending file error = %v", err)
	}
	if _, err := workspaceManager.FinishMutableWorkspace(context.Background(), "alice", "conv_pending"); err != nil {
		t.Fatalf("FinishMutableWorkspace(pending) error = %v", err)
	}
	client := &stubClient{streams: []model.Stream{newStubStream(
		[]model.StreamEvent{{Type: model.StreamEventCompleted, Message: model.Message{Role: model.RoleAssistant, Content: "hello"}}},
		model.Message{Role: model.RoleAssistant, Content: "hello"},
		nil,
	)}}
	executor := newTaskExecutorForTest(t, ExecutorDependencies{
		Resolver:          newExecutorResolverForTest(),
		ConversationStore: store,
		PromptResolver:    newExecutorPromptResolverForTest(t, nil),
		WorkspaceRoot:     templateRoot,
		WorkspaceManager:  workspaceManager,
		ClientFactory:     func(*coretypes.LLMProvider, *coretypes.LLMModel) (model.LlmClient, error) { return client, nil },
	})
	payload := marshalExecutorTaskInput(t, map[string]any{
		"conversation_id":   "conv_readonly",
		"provider_id":       "openai",
		"model_id":          "gpt-5.4",
		"message":           "readonly work",
		"workspace_user_id": "alice",
		"workspace_mode":    "readonly",
	})

	if _, err := executor(context.Background(), &coretasks.Task{ID: "task_readonly", TaskType: "agent.run", CreatedBy: "alice", InputJSON: payload}, nil); err != nil {
		t.Fatalf("executor(readonly) error = %v", err)
	}
}
```

Run:

```powershell
go test ./core/agent -run 'TestAgentExecutor(RejectsMutableRunWhenAnotherConversationHasPendingMerge|AllowsReadonlyRunWhenAnotherConversationHasPendingMerge)' -v
```

Expected: fail because executor does not check other pending conversation workspaces yet.

- [ ] **Step 5: Update task workspace input mapping and executor mutex**

Modify `taskWorkspaceInput` in `app/handlers/task_handler.go`:

- Decode task input once.
- Resolve `workspaceID` from `input["conversation_id"]`.
- Fallback to `task.ID`.
- Return it in the existing `TaskID` field or rename the struct field to `WorkspaceID` if the change stays local.

Keep route shape unchanged for compatibility.

Keep `WorkspaceStateSwaggerDoc` and `TaskWorkspaceSummarySwaggerDoc` fields unchanged in this task. The first implementation continues to return `task_id` as the persisted workspace id for compatibility, even when that value is a conversation id.

In `resolveExecutorWorkspace`, before creating a mutable workspace:

- Call `ListPendingMergeWorkspaces(ctx, workspaceUserID)`.
- If a pending workspace exists with a different workspace id, return an error containing `pending workspace merge`.
- If the pending workspace id equals the current workspace id, allow the run so the same conversation can keep accumulating.
- Skip this check for readonly.

- [ ] **Step 6: Run backend runtime tests**

Run:

```powershell
go test ./core/agent ./app/handlers -run 'Workspace|workspace' -v
```

Expected: executor and handler workspace tests pass.

- [ ] **Step 7: Commit**

```powershell
git add core/agent/executor.go core/agent/executor_test.go app/handlers/task_handler.go app/handlers/task_handler_test.go app/handlers/swagger_types.go
git commit -m "feat: reuse mutable workspaces per conversation"
```

### Task 3: Conversation Workspace Status API

**Files:**
- Modify: `core/workspaces/types.go`
- Modify: `core/workspaces/manager.go`
- Modify: `core/workspaces/manager_test.go`
- Modify: `app/handlers/conversation_handler.go`
- Modify: `app/handlers/conversation_handler_test.go`
- Modify: `app/router/init.go`
- Modify: `app/handlers/swagger_types.go`

- [ ] **Step 1: Write failing manager status tests**

Add this test to `core/workspaces/manager_test.go`.

```go
func TestManagerGetWorkspaceStateReturnsConversationWorkspaceState(t *testing.T) {
	templateRoot := t.TempDir()
	dataRoot := t.TempDir()
	writeFile(t, templateRoot, "AGENTS.md", "template rules")

	mgr, err := NewManager(Config{TemplateRoot: templateRoot, Root: dataRoot})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	workspace, err := mgr.CreateTaskWorkspace(context.Background(), "42", "conv_1", ModeMutable)
	if err != nil {
		t.Fatalf("CreateTaskWorkspace() error = %v", err)
	}
	writeFile(t, workspace.Root, "notes.txt", "changed")
	if _, err := mgr.FinishMutableWorkspace(context.Background(), "42", "conv_1"); err != nil {
		t.Fatalf("FinishMutableWorkspace() error = %v", err)
	}

	state, ok, err := mgr.GetWorkspaceState(context.Background(), "42", "conv_1")
	if err != nil {
		t.Fatalf("GetWorkspaceState() error = %v", err)
	}
	if !ok {
		t.Fatal("GetWorkspaceState() ok = false, want true")
	}
	if state.State != StatePendingMerge {
		t.Fatalf("state = %q, want %q", state.State, StatePendingMerge)
	}
}
```

Run:

```powershell
go test ./core/workspaces -run '^TestManagerGetWorkspaceStateReturnsConversationWorkspaceState$' -v
```

Expected: fail because `GetWorkspaceState` does not exist.

- [ ] **Step 2: Implement state lookup**

Add to `Manager`:

```go
func (m *Manager) GetWorkspaceState(ctx context.Context, userID string, workspaceID string) (*WorkspaceStateFile, bool, error)
```

Behavior:

- Lock user workspace.
- Resolve workspace root with existing path validation.
- Load state.
- Return `ok=false` for missing workspace or missing state.
- Normalize loaded state before returning.

- [ ] **Step 3: Write failing conversation handler tests**

In `app/handlers/conversation_handler_test.go`, add coverage:

```go
func TestConversationWorkspaceStatusReturnsPendingStateForOwner(t *testing.T) {
	store, manager, server := newConversationHandlerWorkspaceTestServer(t)

	if _, err := store.CreateConversation(context.Background(), coreagent.CreateConversationInput{ID: "conv_1", ProviderID: "openai", ModelID: "gpt-5.4", CreatedBy: "tester"}); err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	workspace, err := manager.CreateTaskWorkspace(context.Background(), "tester", "conv_1", coreworkspaces.ModeMutable)
	if err != nil {
		t.Fatalf("CreateTaskWorkspace() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace.Root, "notes.txt"), []byte("changed"), 0o600); err != nil {
		t.Fatalf("write workspace notes error = %v", err)
	}
	if _, err := manager.FinishMutableWorkspace(context.Background(), "tester", "conv_1"); err != nil {
		t.Fatalf("FinishMutableWorkspace() error = %v", err)
	}

	resp, err := http.Get(server.URL + "/api/v1/conversations/conv_1/workspace")
	if err != nil {
		t.Fatalf("http.Get() error = %v", err)
	}
	defer resp.Body.Close()
	got := decodeConversationWorkspaceResponse(t, resp.Body)
	if got == nil || got.State != coreworkspaces.StatePendingMerge {
		t.Fatalf("workspace state = %#v, want pending_merge", got)
	}
}

func TestConversationWorkspaceStatusRejectsOtherUsersConversation(t *testing.T) {
	deps, _, server := newAuthenticatedConversationHandlerWorkspaceTestServer(t)
	owner := registerActiveAuthUserForTest(t, deps.authLogic, "owner", "secret-123")
	registerActiveAuthUserForTest(t, deps.authLogic, "guest", "secret-123")
	if _, err := deps.store.CreateConversation(context.Background(), coreagent.CreateConversationInput{ID: "conv_other", ProviderID: "openai", ModelID: "gpt-5.4", CreatedBy: owner.Username}); err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/conversations/conv_other/workspace", nil)
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	req.AddCookie(newConversationHandlerSessionCookie(t, deps.authLogic, "guest"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer resp.Body.Close()
	envelope := decodeEnvelope(t, resp.Body)
	if envelope.Code != http.StatusUnauthorized && envelope.Code != http.StatusForbidden {
		t.Fatalf("envelope.Code = %d, want unauthorized or forbidden", envelope.Code)
	}
}
```

Add this confirm conflict test:

```go
func TestConversationWorkspaceConfirmMapsHomeChangedConflict(t *testing.T) {
	store, manager, server := newConversationHandlerWorkspaceTestServer(t)
	if _, err := store.CreateConversation(context.Background(), coreagent.CreateConversationInput{ID: "conv_1", ProviderID: "openai", ModelID: "gpt-5.4", CreatedBy: "tester"}); err != nil {
		t.Fatalf("CreateConversation() error = %v", err)
	}
	home, err := manager.EnsureHomeWorkspace(context.Background(), "tester")
	if err != nil {
		t.Fatalf("EnsureHomeWorkspace() error = %v", err)
	}
	workspace, err := manager.CreateTaskWorkspace(context.Background(), "tester", "conv_1", coreworkspaces.ModeMutable)
	if err != nil {
		t.Fatalf("CreateTaskWorkspace() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace.Root, "notes.txt"), []byte("workspace change"), 0o600); err != nil {
		t.Fatalf("write workspace file error = %v", err)
	}
	if _, err := manager.FinishMutableWorkspace(context.Background(), "tester", "conv_1"); err != nil {
		t.Fatalf("FinishMutableWorkspace() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(home.Root, "other.txt"), []byte("home changed"), 0o600); err != nil {
		t.Fatalf("write home file error = %v", err)
	}

	resp, err := http.Post(server.URL+"/api/v1/conversations/conv_1/workspace/confirm", "application/json", nil)
	if err != nil {
		t.Fatalf("POST confirm error = %v", err)
	}
	defer resp.Body.Close()
	envelope := decodeEnvelope(t, resp.Body)
	if envelope.Code != http.StatusConflict {
		t.Fatalf("envelope.Code = %d, want %d", envelope.Code, http.StatusConflict)
	}
}
```

Run:

```powershell
go test ./app/handlers -run 'TestConversationWorkspace(Status|Confirm)' -v
```

Expected: fail because no route exists.

Add these helpers near the existing conversation handler test server helpers:

```go
func newConversationHandlerWorkspaceTestServer(t *testing.T) (*coreagent.ConversationStore, *coreworkspaces.Manager, *httptest.Server) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	store := coreagent.NewConversationStore(db)
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("conversation AutoMigrate() error = %v", err)
	}
	auditStore := coreaudit.NewStore(db)
	if err := auditStore.AutoMigrate(); err != nil {
		t.Fatalf("audit AutoMigrate() error = %v", err)
	}
	templateRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(templateRoot, "AGENTS.md"), []byte("rules"), 0o600); err != nil {
		t.Fatalf("write template AGENTS.md error = %v", err)
	}
	manager, err := coreworkspaces.NewManager(coreworkspaces.Config{TemplateRoot: templateRoot, Root: t.TempDir()})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	engine := rest.Init()
	NewConversationHandler(store, auditStore).WithWorkspaceManager(manager).Register(engine.Group("/api/v1"))
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	return store, manager, server
}

func newAuthenticatedConversationHandlerWorkspaceTestServer(t *testing.T) (*authenticatedConversationHandlerTestDeps, *coreworkspaces.Manager, *httptest.Server) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s-auth?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	store := coreagent.NewConversationStore(db)
	if err := store.AutoMigrate(); err != nil {
		t.Fatalf("conversation AutoMigrate() error = %v", err)
	}
	auditStore := coreaudit.NewStore(db)
	if err := auditStore.AutoMigrate(); err != nil {
		t.Fatalf("audit AutoMigrate() error = %v", err)
	}
	authLogic := newAuthLogicForTest(t, db)
	authMiddleware := NewAuthMiddleware(authLogic)
	templateRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(templateRoot, "AGENTS.md"), []byte("rules"), 0o600); err != nil {
		t.Fatalf("write template AGENTS.md error = %v", err)
	}
	manager, err := coreworkspaces.NewManager(coreworkspaces.Config{TemplateRoot: templateRoot, Root: t.TempDir()})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	engine := rest.Init()
	NewConversationHandler(store, auditStore, authMiddleware.RequireSession()).WithWorkspaceManager(manager).Register(engine.Group("/api/v1"))
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	return &authenticatedConversationHandlerTestDeps{store: store, authLogic: authLogic}, manager, server
}
```

- [ ] **Step 4: Add conversation workspace routes**

Extend `ConversationHandler` with an optional workspace manager dependency:

```go
type conversationWorkspaceManager interface {
	GetWorkspaceState(ctx context.Context, userID string, workspaceID string) (*coreworkspaces.WorkspaceStateFile, bool, error)
	ConfirmTaskWorkspace(ctx context.Context, userID string, workspaceID string) (*coreworkspaces.WorkspaceStateFile, error)
	DiscardTaskWorkspace(ctx context.Context, userID string, workspaceID string) (*coreworkspaces.WorkspaceStateFile, error)
}

func (h *ConversationHandler) WithWorkspaceManager(workspaces conversationWorkspaceManager) *ConversationHandler
```

Register:

```http
GET /conversations/:id/workspace
POST /conversations/:id/workspace/confirm
POST /conversations/:id/workspace/discard
```

Handler rules:

- Load conversation and apply existing ownership checks.
- Resolve workspace user id from current auth user id, fallback to conversation `CreatedBy` only when auth is not required.
- GET returns `nil` data when no state exists.
- POST confirm/discard returns workspace state.
- Map `coreworkspaces.ErrWorkspaceHomeChanged` to HTTP 409.
- Map invalid state errors to HTTP 400.

Update `app/router/init.go` to pass `deps.WorkspaceManager` into `ConversationHandler`.

- [ ] **Step 5: Update swagger types and annotations**

In `app/handlers/swagger_types.go`, add this nullable response doc:

```go
type ConversationWorkspaceStateSwaggerResponse struct {
	Code    int                        `json:"code"`
	Message string                     `json:"message"`
	Data    *WorkspaceStateSwaggerDoc  `json:"data"`
	OK      bool                       `json:"ok"`
	Time    string                     `json:"time"`
}
```

Add Swagger comments beside the new handler methods. Do not hand-edit `docs/swagger/*`; regenerate in Task 5.

- [ ] **Step 6: Run handler and workspace tests**

Run:

```powershell
go test ./core/workspaces ./app/handlers -run 'Workspace|workspace|ConversationWorkspace' -v
```

Expected: conversation workspace status/action routes pass, including 409 mapping for stale baseline confirms.

- [ ] **Step 7: Commit**

```powershell
git add core/workspaces/types.go core/workspaces/manager.go core/workspaces/manager_test.go app/handlers/conversation_handler.go app/handlers/conversation_handler_test.go app/router/init.go app/handlers/swagger_types.go
git commit -m "feat: expose conversation workspace state"
```

### Task 4: Frontend State Sync and Compact Merge Control

**Files:**
- Modify: `webapp/src/types/api.ts`
- Modify: `webapp/src/lib/api.ts`
- Modify: `webapp/src/lib/api.spec.ts`
- Modify: `webapp/src/lib/chat-state.ts`
- Modify: `webapp/src/lib/chat-state.spec.ts`
- Modify: `webapp/src/lib/task-runtime.ts`
- Modify: `webapp/src/views/ChatView.vue`
- Modify: `webapp/src/views/ChatView.spec.ts`

- [ ] **Step 1: Write failing API tests**

In `webapp/src/lib/api.spec.ts`, add tests for new helpers:

```ts
it('fetches conversation workspace state', async () => {
  vi.stubGlobal('fetch', vi.fn())
  vi.mocked(fetch).mockResolvedValueOnce({
    ok: true,
    json: async () => ({
      ok: true,
      code: 200,
      message: 'OK',
      time: '',
      data: {
      task_id: 'conv_1',
      user_id: '42',
      mode: 'mutable',
      state: 'pending_merge',
      home_root: '/home',
      task_root: '/workspace',
      created_at: '2026-05-24T00:00:00Z',
      updated_at: '2026-05-24T00:00:00Z',
      },
    }),
  } as Response)

  const state = await fetchConversationWorkspaceState('conv_1')

  expect(fetch).toHaveBeenCalledWith('/api/v1/conversations/conv_1/workspace', expect.any(Object))
  expect(state?.state).toBe('pending_merge')
})

it('confirms conversation workspace changes', async () => {
  vi.stubGlobal('fetch', vi.fn())
  vi.mocked(fetch).mockResolvedValueOnce({
    ok: true,
    json: async () => ({
      ok: true,
      code: 200,
      message: 'OK',
      time: '',
      data: { task_id: 'conv_1', user_id: '42', mode: 'mutable', state: 'merged', home_root: '/home', task_root: '/workspace', created_at: '', updated_at: '' },
    }),
  } as Response)

  const state = await confirmConversationWorkspaceMerge('conv_1')

  expect(fetch).toHaveBeenCalledWith('/api/v1/conversations/conv_1/workspace/confirm', expect.objectContaining({ method: 'POST' }))
  expect(state.state).toBe('merged')
})
```

Use `vi.stubGlobal('fetch', vi.fn())` in the test `beforeEach`, matching the existing `webapp/src/lib/api.spec.ts` tests.

Run:

```powershell
pnpm --dir webapp exec vitest run src/lib/api.spec.ts -t "conversation workspace"
```

Expected: fail until helpers exist.

- [ ] **Step 2: Add API helpers and types**

In `webapp/src/types/api.ts`:

- Keep `TaskWorkspaceState` shape unless backend adds `workspace_id` or `conversation_id`; add optional fields if present.
- Allow `fetchConversationWorkspaceState` to return `TaskWorkspaceState | null`.

In `webapp/src/lib/api.ts`, add:

```ts
export async function fetchConversationWorkspaceState(conversationId: string) {
  const state = await request<Partial<TaskWorkspaceState> & Record<string, unknown> | null>(
    `/conversations/${encodeURIComponent(conversationId)}/workspace`,
  )
  return state ? normalizeTaskWorkspaceState(state) : null
}

export async function confirmConversationWorkspaceMerge(conversationId: string) {
  const state = await request<Partial<TaskWorkspaceState> & Record<string, unknown>>(
    `/conversations/${encodeURIComponent(conversationId)}/workspace/confirm`,
    { method: 'POST' },
  )
  return normalizeTaskWorkspaceState(state)
}

export async function discardConversationWorkspaceChanges(conversationId: string) {
  const state = await request<Partial<TaskWorkspaceState> & Record<string, unknown>>(
    `/conversations/${encodeURIComponent(conversationId)}/workspace/discard`,
    { method: 'POST' },
  )
  return normalizeTaskWorkspaceState(state)
}
```

- [ ] **Step 3: Write failing ChatView tests**

In `webapp/src/views/ChatView.spec.ts`, add or update tests:

```ts
it('restores pending workspace merge from backend when switching conversations', async () => {
  api.fetchConversations.mockResolvedValue([{ id: 'conv_1', title: 'Work', provider_id: 'openai', model_id: 'gpt-5.4', created_by: 'demo-user', created_at: '', updated_at: '', last_message: '', message_count: 1 }])
  api.fetchConversationMessages.mockResolvedValue([])
  api.fetchConversationWorkspaceState.mockResolvedValue({
    task_id: 'conv_1',
    user_id: '42',
    mode: 'mutable',
    state: 'pending_merge',
    home_root: '/home',
    task_root: '/workspace',
    created_at: '',
    updated_at: '',
  })

  const router = makeRouter()
  await router.push('/chat/conv_1')
  await router.isReady()
  const wrapper = mount(ChatView, { global: { plugins: [router] } })
  await flushPromises()

  expect(api.fetchConversationWorkspaceState).toHaveBeenCalledWith('conv_1')
  expect(wrapper.find('[data-workspace-merge-inline]').exists()).toBe(true)
  expect(wrapper.find('[data-workspace-merge-banner]').exists()).toBe(false)
})

it('does not show merge control when backend workspace has no pending changes', async () => {
  api.fetchConversations.mockResolvedValue([
    {
      id: 'conv_1',
      title: 'Work',
      provider_id: 'openai',
      model_id: 'gpt-5.4',
      created_by: 'demo-user',
      created_at: '',
      updated_at: '',
      last_message: '',
      message_count: 1,
    },
  ])
  api.fetchConversationMessages.mockResolvedValue([])
  api.fetchConversationWorkspaceState.mockResolvedValue(null)

  const router = makeRouter()
  await router.push('/chat/conv_1')
  await router.isReady()
  const wrapper = mount(ChatView, { global: { plugins: [router] } })
  await flushPromises()

  expect(wrapper.find('[data-workspace-merge-inline]').exists()).toBe(false)
})
```

Run:

```powershell
pnpm --dir webapp exec vitest run src/views/ChatView.spec.ts -t "workspace merge"
```

Expected: fail until ChatView sync and compact control are implemented.

- [ ] **Step 4: Replace local-only pending logic with backend sync**

In `webapp/src/views/ChatView.vue`:

- Import the three conversation workspace API helpers.
- Add `syncConversationWorkspaceState(conversationId: string)`:

```ts
async function syncConversationWorkspaceState(conversationId: string) {
  if (!conversationId) {
    return
  }
  const state = await fetchConversationWorkspaceState(conversationId)
  syncWorkspaceMergeStateFromWorkspaceState(state, conversationId)
}
```

- Call it during `loadConversationForRoute(conversationId)` after loading messages and before/after `resumeStreamForConversation`.
- Keep `pendingWorkspaceMergeTaskIdByConversation` as local cache, but let backend sync overwrite it.
- In `handleWorkspaceMergeAction`, prefer conversation helper when `activeConversationId` or route conversation id exists:

```ts
if (conversationId) {
  workspaceState = action === 'confirm'
    ? await confirmConversationWorkspaceMerge(conversationId)
    : await discardConversationWorkspaceChanges(conversationId)
} else if (action === 'confirm') {
  workspaceState = await confirmTaskWorkspaceMerge(taskId)
} else {
  workspaceState = await discardTaskWorkspaceChanges(taskId)
}
```

- [ ] **Step 5: Move merge UI next to mode switch**

Replace the large `workspace-merge-banner` block with an inline control inside `.workspace-mode-row`:

```vue
<div v-if="currentPendingWorkspaceMergeTaskId" class="workspace-merge-inline" data-workspace-merge-inline>
  <span class="workspace-merge-inline-label">待合并</span>
  <button class="ghost-button workspace-merge-inline-button" type="button" data-workspace-merge-discard @click="handleWorkspaceMergeAction('discard')">丢弃</button>
  <button class="primary-button workspace-merge-inline-button" type="button" data-workspace-merge-confirm @click="handleWorkspaceMergeAction('confirm')">合并</button>
</div>
```

Adjust CSS:

- Keep row height compact.
- Buttons use smaller padding than composer primary buttons.
- On mobile, allow wrapping inside the same row instead of a full-width banner.

- [ ] **Step 6: Run frontend checks**

Run:

```powershell
pnpm --dir webapp exec vitest run src/lib/api.spec.ts src/lib/chat-state.spec.ts src/views/ChatView.spec.ts
pnpm --dir webapp exec vue-tsc -b
```

Expected: tests and typecheck pass.

- [ ] **Step 7: Commit**

```powershell
git add webapp/src/types/api.ts webapp/src/lib/api.ts webapp/src/lib/api.spec.ts webapp/src/lib/chat-state.ts webapp/src/lib/chat-state.spec.ts webapp/src/lib/task-runtime.ts webapp/src/views/ChatView.vue webapp/src/views/ChatView.spec.ts
git commit -m "feat: sync conversation workspace merge prompts"
```

### Task 5: Docs, Swagger, and Full Verification

**Files:**
- Modify: `docs/swagger/docs.go`
- Modify: `docs/swagger/swagger.json`
- Modify: `docs/swagger/swagger.yaml`
- Modify: `docs/superpowers/specs/2026-05-24-conversation-workspace-baseline-design.md` if implementation reveals a naming adjustment
- Modify: `docs/superpowers/plans/2026-05-24-conversation-workspace-baseline.md` if implementation steps need correction

- [ ] **Step 1: Regenerate Swagger docs**

Run the Swagger generation command:

```powershell
go run github.com/swaggo/swag/cmd/swag init -g cmd/example_agent/main.go -o docs/swagger
```

Expected: `docs/swagger/docs.go`, `docs/swagger/swagger.json`, and `docs/swagger/swagger.yaml` reflect the new conversation workspace endpoints.

- [ ] **Step 2: Run focused backend verification**

Run:

```powershell
go test ./core/workspaces ./core/agent ./app/handlers -v
```

Expected: all focused backend tests pass.

- [ ] **Step 3: Run full backend verification**

Run:

```powershell
go test ./...
go build ./cmd/...
go list ./...
```

Expected: all commands pass.

- [ ] **Step 4: Run frontend verification**

Run:

```powershell
pnpm --dir webapp exec vitest run src/lib/api.spec.ts src/lib/chat-state.spec.ts src/views/ChatView.spec.ts
pnpm --dir webapp exec vue-tsc -b
```

Expected: all targeted frontend tests pass and typecheck is clean.

- [ ] **Step 5: Commit**

```powershell
git add docs/swagger/docs.go docs/swagger/swagger.json docs/swagger/swagger.yaml docs/superpowers/specs/2026-05-24-conversation-workspace-baseline-design.md docs/superpowers/plans/2026-05-24-conversation-workspace-baseline.md
git commit -m "docs: update workspace baseline api docs"
```
