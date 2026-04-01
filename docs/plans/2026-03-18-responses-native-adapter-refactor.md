# Responses Native Adapter Refactor Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Refactor `core/providers/client/openai_responses` to treat `/responses` as a native response-state protocol instead of reconstructing continuation from transcript replay.

**Architecture:** Keep `core/agent` and conversation persistence interfaces stable. Move continuation logic fully into the `openai_responses` adapter by introducing explicit provider-state shapes for native response sessions, response-output archives, and follow-up tool submissions. Build requests from provider state first, and only project transcript messages for UI/persistence.

**Tech Stack:** Go, `openai-go` Responses API, existing `core/providers/types`, Go tests, existing webapp regression tests for transcript rendering.

---

### Task 1: Define native provider-state shapes

**Files:**
- Modify: `core/providers/types/types.go`
- Modify: `core/providers/client/openai_responses/provider_state.go`
- Test: `core/providers/client/openai_responses/stream_test.go`

**Step 1: Write the failing test**

Add a test that expects provider state to preserve both the response archive and a native continuation session identity after a completed response with tool calls.

**Step 2: Run test to verify it fails**

Run: `go test ./core/providers/client/openai_responses -run TestResponseStreamFinalMessage_PreservesNativeResponseState`

Expected: FAIL because current provider state only stores replay-oriented output items.

**Step 3: Write minimal implementation**

Add explicit provider-state fields/format handling for:
- archived output items
- native response id / session id for follow-up continuation
- future compatibility flags if needed

**Step 4: Run test to verify it passes**

Run: `go test ./core/providers/client/openai_responses -run TestResponseStreamFinalMessage_PreservesNativeResponseState`

Expected: PASS

### Task 2: Build follow-up requests from provider state, not transcript replay

**Files:**
- Modify: `core/providers/client/openai_responses/utils.go`
- Test: `core/providers/client/openai_responses/utils_test.go`

**Step 1: Write the failing test**

Add tests for two paths:
- native tool follow-up uses `previous_response_id + function_call_output[]`
- archived non-follow-up replay does not require rebuilding reasoning/tool transcript heuristics

**Step 2: Run test to verify it fails**

Run: `go test ./core/providers/client/openai_responses -run "TestBuildResponseRequestParams_(UsesNativeContinuationState|ReplaysArchivedOutputState)"`

Expected: FAIL because current builder infers continuation from message ordering.

**Step 3: Write minimal implementation**

Refactor request building to:
- detect native continuation directly from assistant provider state
- submit only tool outputs for follow-up turns
- reserve archived output replay for non-follow-up history reconstruction
- remove transcript-order heuristics as the primary continuation mechanism

**Step 4: Run test to verify it passes**

Run: `go test ./core/providers/client/openai_responses -run "TestBuildResponseRequestParams_(UsesNativeContinuationState|ReplaysArchivedOutputState)"`

Expected: PASS

### Task 3: Keep stream extraction aligned with native state

**Files:**
- Modify: `core/providers/client/openai_responses/client.go`
- Modify: `core/providers/client/openai_responses/stream.go`
- Test: `core/providers/client/openai_responses/stream_test.go`

**Step 1: Write the failing test**

Add a test proving the stream collector captures the native response id and uses it when building the final assistant message provider state.

**Step 2: Run test to verify it fails**

Run: `go test ./core/providers/client/openai_responses -run TestApplyStreamEvent_CapturesResponseIDOnCompletion`

Expected: FAIL because the stream does not fully model native continuation state.

**Step 3: Write minimal implementation**

Ensure stream collection owns:
- response id capture
- output item archive capture
- final assistant message projection built from native state

**Step 4: Run test to verify it passes**

Run: `go test ./core/providers/client/openai_responses -run TestApplyStreamEvent_CapturesResponseIDOnCompletion`

Expected: PASS

### Task 4: Preserve transcript projection separately from provider continuation

**Files:**
- Modify: `core/agent/executor.go`
- Test: `core/agent/executor_test.go`
- Modify: `webapp/src/lib/transcript.ts`
- Test: `webapp/src/lib/transcript.spec.ts`

**Step 1: Write the failing test**

Add tests that prove:
- assistant reasoning/tool messages remain visible from persisted conversation messages
- failure messages remain visible after a native continuation error

**Step 2: Run test to verify it fails**

Run: `go test ./core/agent -run TestAgentExecutorPersistsTranscriptAroundNativeContinuationFailure`

Run: `npm test -- --run src/lib/transcript.spec.ts`

Expected: FAIL if transcript projection still depends on replay-specific state assumptions.

**Step 3: Write minimal implementation**

Keep persistence/UI behavior stable while decoupling it from how the adapter continues `/responses` requests.

**Step 4: Run test to verify it passes**

Run: `go test ./core/agent -run TestAgentExecutorPersistsTranscriptAroundNativeContinuationFailure`

Run: `npm test -- --run src/lib/transcript.spec.ts`

Expected: PASS

### Task 5: Real-request verification against the live backend

**Files:**
- Modify: `core/providers/client/openai_responses/*` as needed from findings
- Test: live backend on `http://127.0.0.1:18080`

**Step 1: Write the reproduction script**

Use the existing direct backend task creation flow and a raw `/responses` probe for the same tool sequence.

**Step 2: Run live verification**

Run:
- `go test ./core/providers/client/openai_responses ./core/agent ./core/tasks`
- `go test ./...`
- `npm test`
- `npm run build`
- backend reproduction against `18080`

Expected: Tests pass; live request either succeeds or yields a narrower, protocol-accurate failure that is no longer caused by transcript replay design.

### Task 6: Final cleanup

**Files:**
- Modify: only files touched above

**Step 1: Remove temporary compatibility hacks that conflict with native design**

Delete request-builder logic that special-cases continuation based on transcript ordering when native provider state exists.

**Step 2: Run final verification**

Run:
- `go test ./...`
- `npm test && npm run build`

Expected: PASS
