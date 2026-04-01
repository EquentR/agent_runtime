# Builtin Tools Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement the first batch of runtime builtin tools in `core/tools/builtin`, with one file per tool and a single registration entrypoint for `core/tools.Registry`.

**Architecture:** Add a `builtin` package that binds runtime options (workspace root, HTTP client, command timeout, web search providers) into concrete `tools.Tool` definitions. Keep shared argument parsing, path safety, JSON formatting, and command/search helpers in internal package-private helpers, while each builtin tool lives in its own Go file and the package exposes one aggregator for registration.

**Tech Stack:** Go 1.25, standard library, existing `core/tools` registry, existing `core/types` JSON schema types

---

### Task 1: Add builtin package scaffolding and registration tests

**Files:**
- Create: `core/tools/builtin/register.go`
- Create: `core/tools/builtin/options.go`
- Create: `core/tools/builtin/helpers.go`
- Create: `core/tools/builtin/register_test.go`

**Step 1: Write the failing test**

Add tests that assert:
- `Register(...)` installs the planned builtin tool names into `core/tools.Registry`
- registration rejects invalid builtin options such as a missing workspace root when normalization fails

**Step 2: Run test to verify it fails**

Run: `go test ./core/tools/builtin -run TestRegister`
Expected: FAIL because the builtin package and registration entrypoint do not exist yet.

**Step 3: Write minimal implementation**

Create the builtin package scaffolding, options normalization, and aggregated registration entrypoint.

**Step 4: Run test to verify it passes**

Run: `go test ./core/tools/builtin -run TestRegister`
Expected: PASS.

### Task 2: Add file-operation builtins with workspace safety

**Files:**
- Create: `core/tools/builtin/list_files.go`
- Create: `core/tools/builtin/read_file.go`
- Create: `core/tools/builtin/write_file.go`
- Create: `core/tools/builtin/search_file.go`
- Create: `core/tools/builtin/grep_file.go`
- Create: `core/tools/builtin/delete_file.go`
- Create: `core/tools/builtin/move_file.go`
- Create: `core/tools/builtin/copy_file.go`
- Modify: `core/tools/builtin/register_test.go`

**Step 1: Write the failing test**

Add tests that assert:
- file paths are resolved relative to the configured workspace root
- symlink targets are rejected
- `list_files` supports recursion and depth control
- `read_file` supports line windows
- `write_file` supports overwrite, insert, and replace-line modes
- `search_file` and `grep_file` support literal and regex matching
- `delete_file` requires explicit confirmation
- `move_file` and `copy_file` preserve file contents at the destination

**Step 2: Run test to verify it fails**

Run: `go test ./core/tools/builtin -run "Test(ListFiles|ReadFile|WriteFile|SearchFile|GrepFile|DeleteFile|MoveFile|CopyFile)"`
Expected: FAIL because the file-operation tools are not implemented yet.

**Step 3: Write minimal implementation**

Implement the file-operation tools and shared safe-path helpers.

**Step 4: Run test to verify it passes**

Run: `go test ./core/tools/builtin -run "Test(ListFiles|ReadFile|WriteFile|SearchFile|GrepFile|DeleteFile|MoveFile|CopyFile)"`
Expected: PASS.

### Task 3: Add command, process, and system builtins

**Files:**
- Create: `core/tools/builtin/exec_command.go`
- Create: `core/tools/builtin/check_command.go`
- Create: `core/tools/builtin/list_processes.go`
- Create: `core/tools/builtin/kill_process.go`
- Create: `core/tools/builtin/get_system_info.go`
- Modify: `core/tools/builtin/register_test.go`

**Step 1: Write the failing test**

Add tests that assert:
- `exec_command` can execute a command in the configured workspace and returns stdout/stderr plus exit metadata
- `check_command` reports whether a command exists and can capture version output
- `list_processes` can filter by PID or name and returns the current test process
- `kill_process` requires explicit confirmation and can terminate a spawned helper process
- `get_system_info` returns stable runtime/system keys

**Step 2: Run test to verify it fails**

Run: `go test ./core/tools/builtin -run "Test(ExecCommand|CheckCommand|ListProcesses|KillProcess|GetSystemInfo)"`
Expected: FAIL because the command and process builtins do not exist yet.

**Step 3: Write minimal implementation**

Implement the command execution, command detection, process inspection, process termination, and system info tools.

**Step 4: Run test to verify it passes**

Run: `go test ./core/tools/builtin -run "Test(ExecCommand|CheckCommand|ListProcesses|KillProcess|GetSystemInfo)"`
Expected: PASS.

### Task 4: Add HTTP and web-search builtins

**Files:**
- Create: `core/tools/builtin/http_request.go`
- Create: `core/tools/builtin/web_search.go`
- Modify: `core/tools/builtin/register_test.go`

**Step 1: Write the failing test**

Add tests that assert:
- `http_request` can send requests to an `httptest` server with headers and body and returns response status/body
- `web_search` can use a configured provider and normalize provider-specific responses into a common result payload
- `web_search` returns a clear error when the requested provider is not configured

**Step 2: Run test to verify it fails**

Run: `go test ./core/tools/builtin -run "Test(HTTPRequest|WebSearch)"`
Expected: FAIL because the network builtins are not implemented yet.

**Step 3: Write minimal implementation**

Implement the HTTP client builtin plus Tavily, SerpAPI, and Bing-backed web-search provider adapters with injectable HTTP base URLs for tests.

**Step 4: Run test to verify it passes**

Run: `go test ./core/tools/builtin -run "Test(HTTPRequest|WebSearch)"`
Expected: PASS.

### Task 5: Run focused and broad verification

**Files:**
- Modify: `core/tools/builtin/*.go`

**Step 1: Run focused builtin verification**

Run: `go test ./core/tools/builtin`
Expected: PASS.

**Step 2: Run broader tools verification**

Run: `go test ./core/tools ./core/tools/builtin`
Expected: PASS.

**Step 3: Run full repository verification**

Run: `go test ./...`
Expected: PASS.
