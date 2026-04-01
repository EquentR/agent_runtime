# MCP Client Adapter Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Remove the cross-project `agent_study` MCP dependency and add a client-first `core/mcp` abstraction backed by `mcp-go`.

**Architecture:** Add a small internal `core/mcp` port with tool listing and tool calling primitives, then implement a `mark3labs/mcp-go` adapter behind that port. Keep `core/tools` depending only on the internal port so the runtime owns its MCP boundary and can swap implementations later.

**Tech Stack:** Go 1.25, `github.com/mark3labs/mcp-go`, standard library testing

---

### Task 1: Replace external-test coupling with local MCP-facing tests

**Files:**
- Modify: `core/tools/register_test.go`
- Create: `core/mcp/types_test.go`

**Step 1: Write the failing test**

Add tests that assert:
- `RegisterMCPClient` accepts a local `core/mcp.Client`
- listed tool schemas are converted into `core/types.JSONSchema`
- tool execution calls the remote tool name, not the prefixed local alias

**Step 2: Run test to verify it fails**

Run: `go test ./core/tools ./core/mcp/...`
Expected: FAIL because `core/mcp` does not exist yet and `RegisterMCPClient` is still missing.

**Step 3: Write minimal implementation**

Create the new `core/mcp` types and restore `RegisterMCPClient` against the local interface.

**Step 4: Run test to verify it passes**

Run: `go test ./core/tools ./core/mcp/...`
Expected: PASS.

### Task 2: Add the `mcp-go` adapter

**Files:**
- Create: `core/mcp/client.go`
- Create: `core/mcp/types.go`
- Create: `core/mcp/mark3labs/client.go`
- Create: `core/mcp/mark3labs/client_test.go`
- Modify: `go.mod`

**Step 1: Write the failing test**

Add adapter tests that use an in-process `mcp-go` server and verify:
- `ListTools` maps tool descriptors into internal types
- `CallTool` returns text content
- non-text or error cases return useful errors

**Step 2: Run test to verify it fails**

Run: `go test ./core/mcp/...`
Expected: FAIL because the adapter and dependency are not implemented yet.

**Step 3: Write minimal implementation**

Implement the adapter with initialization, schema conversion, and text result extraction.

**Step 4: Run test to verify it passes**

Run: `go test ./core/mcp/...`
Expected: PASS.

### Task 3: Verify module cleanup and project health

**Files:**
- Modify: `core/tools/register.go`
- Modify: `go.sum`

**Step 1: Run targeted verification**

Run: `go test ./core/tools ./core/mcp/...`
Expected: PASS.

**Step 2: Run broad verification**

Run: `go test ./...`
Expected: PASS with no `agent_study` imports remaining.

**Step 3: Confirm dependency cleanup**

Run: `go list -deps ./...`
Expected: no local sibling-module MCP dependency in the graph.

### Task 4: Add transport constructors for real MCP connections

**Files:**
- Modify: `core/mcp/mark3labs/client.go`
- Modify: `core/mcp/mark3labs/client_test.go`
- Modify: `core/mcp/README.md`

**Step 1: Write the failing test**

Add tests that assert:
- `NewStreamableHTTPClient(...)` returns an already initialized adapter that can call `ListTools`
- `NewStdioClient(...)` can spawn a subprocess MCP server and immediately call `ListTools`

**Step 2: Run test to verify it fails**

Run: `go test ./core/mcp/mark3labs -run "TestNew(StreamableHTTP|Stdio)Client"`
Expected: FAIL because the constructor helpers do not exist yet.

**Step 3: Write minimal implementation**

Implement the constructor helpers so they create the underlying `mcp-go` client, start it if needed, run `initialize`, then wrap it in the local adapter.

**Step 4: Run test to verify it passes**

Run: `go test ./core/mcp/mark3labs -run "TestNew(StreamableHTTP|Stdio)Client"`
Expected: PASS.

### Task 5: Add legacy SSE constructor for older MCP servers

**Files:**
- Modify: `core/mcp/mark3labs/client.go`
- Modify: `core/mcp/mark3labs/constructors_test.go`
- Modify: `core/mcp/README.md`

**Step 1: Write the failing test**

Add a test that asserts `NewSSEClient(...)` can connect to a legacy SSE server and immediately call `ListTools`.

**Step 2: Run test to verify it fails**

Run: `go test ./core/mcp/mark3labs -run TestNewSSEClient`
Expected: FAIL because the constructor helper does not exist yet.

**Step 3: Write minimal implementation**

Implement the helper around `mcp-go`'s SSE client, then reuse the shared initialization flow.

**Step 4: Run test to verify it passes**

Run: `go test ./core/mcp/mark3labs -run TestNewSSEClient`
Expected: PASS.

### Task 6: Add MCP prompt wrapping support

**Files:**
- Modify: `core/mcp/client.go`
- Modify: `core/mcp/types.go`
- Modify: `core/mcp/mark3labs/client.go`
- Modify: `core/mcp/mark3labs/client_test.go`
- Modify: `core/tools/register.go`
- Modify: `core/tools/register_test.go`

**Step 1: Write the failing test**

Add tests that assert:
- the adapter can `ListPrompts` and `GetPrompt`
- MCP prompts can be wrapped as local tools with generated string schemas
- wrapped prompt execution calls the remote prompt and returns a readable prompt transcript

**Step 2: Run test to verify it fails**

Run: `go test ./core/mcp/mark3labs ./core/tools`
Expected: FAIL because prompt descriptors and registration helpers do not exist yet.

**Step 3: Write minimal implementation**

Extend the internal MCP client contract to cover prompts, implement prompt mapping in the `mark3labs` adapter, and add a prompt-to-tool registration helper in `core/tools`.

**Step 4: Run test to verify it passes**

Run: `go test ./core/mcp/mark3labs ./core/tools`
Expected: PASS.

### Task 7: Wire constructors into YAML-backed app config

**Files:**
- Create: `app/config/mcp.go`
- Create: `app/config/mcp_test.go`

**Step 1: Write the failing test**

Add tests that assert:
- YAML can unmarshal into the MCP config structs using `yaml` tags
- config can build a streamable HTTP client
- config can build a legacy SSE client
- config can expose MCP registration options for tools/prompts

**Step 2: Run test to verify it fails**

Run: `go test ./app/config`
Expected: FAIL because the config package and constructor wiring do not exist yet.

**Step 3: Write minimal implementation**

Create the config structs with explicit `yaml` tags, validate required fields per transport, and dispatch to the correct `mark3labs` constructor.

**Step 4: Run test to verify it passes**

Run: `go test ./app/config`
Expected: PASS.
