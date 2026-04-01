# Swagger UI And Docs Update Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Expose a browser-friendly Swagger UI route for the example app and update repository docs to reflect the current task manager and API documentation workflow.

**Architecture:** Add a lightweight Swagger handler that serves an embedded HTML shell plus generated Swagger JSON/YAML assets under the existing API base path. Keep the implementation dependency-light by using Go `embed` and CDN-hosted Swagger UI assets instead of adding a new HTTP middleware package. Update `README.md` and `AGENTS.md` so the runtime snapshot matches the new task manager and Swagger entrypoints.

**Tech Stack:** Go, Gin, `embed`, generated Swagger JSON/YAML from `swag.exe`, repository docs.

---

### Task 1: Add failing tests for Swagger UI routes

**Files:**
- Create: `app/handlers/swagger_handler_test.go`
- Modify: `app/handlers/task_handler_test.go`

**Step 1: Write the failing test**

Cover:
- `GET /api/v1/swagger` redirects to `index.html`
- `GET /api/v1/swagger/index.html` returns HTML containing Swagger UI bootstrapping
- `GET /api/v1/swagger/swagger.json` returns the generated API document

**Step 2: Run test to verify it fails**

Run: `go test ./app/handlers -run SwaggerUI -count=1`
Expected: FAIL because the Swagger UI handler and routes do not exist yet.

**Step 3: Write minimal implementation**

Add a dedicated Swagger handler using embedded files and register it through the router.

**Step 4: Run test to verify it passes**

Run: `go test ./app/handlers -run SwaggerUI -count=1`
Expected: PASS.

### Task 2: Update docs and generation workflow

**Files:**
- Modify: `README.md`
- Modify: `AGENTS.md`
- Verify: `docs/swagger/swagger.json`
- Verify: `docs/swagger/swagger.yaml`

**Step 1: Update repository docs**

Document:
- current task manager layer under `core/tasks`
- task APIs and Swagger UI route
- explicit `swag.exe` generation command and output location

**Step 2: Regenerate Swagger docs**

Run: `C:\Users\Equent\go\bin\swag.exe init -g "cmd/example_agent/main.go" -o "docs/swagger" --outputTypes json,yaml --parseDependency --parseInternal`
Expected: `swagger.json` and `swagger.yaml` regenerated successfully.

### Task 3: Verify formatting, tests, and build

**Files:**
- Verify only: touched Go files

**Step 1: Run gofmt**

Run: `gofmt -w <touched-go-files>`
Expected: formatting applied cleanly.

**Step 2: Run focused tests**

Run: `go test ./app/handlers -run 'Swagger|SwaggerUI|Task' -count=1`
Expected: PASS.

**Step 3: Run full verification**

Run: `go test ./... -count=1 && go build ./cmd/...`
Expected: PASS.
