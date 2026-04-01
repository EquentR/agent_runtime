# AI SDK Style Responses Rework Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Rework `openai_responses` to follow the Vercel AI SDK pattern: persist OpenAI-native response identity and item metadata, continue same-model `/responses` turns via native ids, and use transcript replay only as a fallback for model/provider/API switches.

**Architecture:** Split `responses` handling into two layers. The transcript layer keeps `ConversationMessage` persistence and UI projection unchanged. The provider layer stores native continuation state (`response_id`, item ids, reasoning encrypted content, optional conversation id) and builds follow-up requests from that state, preferring `previous_response_id`, `conversation`, and `item_reference` over synthetic message replay.

**Tech Stack:** Go, `openai-go` Responses API, existing `core/providers/types`, existing `core/agent` runner/executor, SQLite-backed conversation store, webapp transcript projection tests.

---

### Task 1: Define AI-SDK-style provider metadata/state model

**Files:**
- Modify: `core/providers/types/types.go`
- Modify: `core/providers/client/openai_responses/provider_state.go`
- Test: `core/providers/client/openai_responses/utils_test.go`
- Test: `core/providers/client/openai_responses/stream_test.go`

**Step 1: Write the failing test**

Add tests that expect assistant provider state to preserve:
- `response_id`
- archived output items
- per-item identity metadata needed for later `item_reference`
- reasoning encrypted content when present

**Step 2: Run test to verify it fails**

Run: `go test ./core/providers/client/openai_responses -run "Test(ExtractChatResponse|ResponseStreamFinalMessage)_PreservesNativeResponsesMetadata"`

Expected: FAIL because current state stores only a coarse archive.

**Step 3: Write minimal implementation**

Introduce an explicit persisted state envelope, e.g.:

```go
type PersistedResponsesState struct {
  ResponseID   string
  Conversation string
  Output       []responses.ResponseOutputItemUnion
  ItemMeta     []PersistedResponsesItemMeta
}
```

Where `ItemMeta` captures provider item ids and replay mode decisions.

**Step 4: Run test to verify it passes**

Run: `go test ./core/providers/client/openai_responses -run "Test(ExtractChatResponse|ResponseStreamFinalMessage)_PreservesNativeResponsesMetadata"`

Expected: PASS

### Task 2: Add native continuation detection at the conversation boundary

**Files:**
- Modify: `core/agent/types.go`
- Modify: `core/agent/executor.go`
- Modify: `core/agent/stream.go`
- Test: `core/agent/executor_test.go`
- Test: `core/agent/runner_test.go`

**Step 1: Write the failing test**

Add tests proving that for same provider/model `/responses` continuation:
- the runner receives only the new user message after the last native assistant turn
- the old transcript is not forwarded as `ChatRequest.Messages`
- fallback still replays full transcript when provider/model changes

**Step 2: Run test to verify it fails**

Run: `go test ./core/agent -run "Test(TaskExecutor|Runner)_(UsesNativeResponsesContinuation|FallsBackToTranscriptReplayOnModelSwitch)"`

Expected: FAIL because current executor still feeds full message history into the runner.

**Step 3: Write minimal implementation**

Add a continuation preparation step in `executor` that builds a `RunInput` containing:
- `MessagesForPersistence` / transcript view
- `ProviderMessages` / native continuation input slice for provider requests
- model/provider match guard

Do not change public HTTP APIs.

**Step 4: Run test to verify it passes**

Run: `go test ./core/agent -run "Test(TaskExecutor|Runner)_(UsesNativeResponsesContinuation|FallsBackToTranscriptReplayOnModelSwitch)"`

Expected: PASS

### Task 3: Rebuild Responses input conversion around item identity

**Files:**
- Modify: `core/providers/client/openai_responses/utils.go`
- Modify: `core/providers/client/openai_responses/provider_state.go`
- Test: `core/providers/client/openai_responses/utils_test.go`

**Step 1: Write the failing test**

Add tests for three cases:
- user follow-up after assistant turn -> `previous_response_id + new user input`
- tool follow-up after function call -> `previous_response_id + function_call_output[]`
- replay fallback with provider item ids -> `item_reference` instead of full assistant/tool reconstruction

**Step 2: Run test to verify it fails**

Run: `go test ./core/providers/client/openai_responses -run "TestBuildResponseRequestParams_(UsesPreviousResponseIDForUserFollowup|UsesPreviousResponseIDForToolContinuation|UsesItemReferenceReplay)"`

Expected: FAIL because current builder still reconstructs assistant history from transcript/items.

**Step 3: Write minimal implementation**

Refactor request building into explicit branches:

```go
buildResponsesRequest(messages []model.Message) {
  if nativeTurn := lastNativeTurn(messages); nativeTurn != nil {
    return buildNativeFollowup(nativeTurn, messagesAfter(nativeTurn))
  }
  return buildFallbackReplay(messages)
}
```

For fallback replay:
- use item ids as references when possible
- only emit full items if no id/reference exists

**Step 4: Run test to verify it passes**

Run: `go test ./core/providers/client/openai_responses -run "TestBuildResponseRequestParams_(UsesPreviousResponseIDForUserFollowup|UsesPreviousResponseIDForToolContinuation|UsesItemReferenceReplay)"`

Expected: PASS

### Task 4: Separate transcript projection from native continuation state

**Files:**
- Modify: `core/agent/executor.go`
- Modify: `webapp/src/lib/transcript.ts`
- Test: `core/agent/executor_test.go`
- Test: `webapp/src/lib/transcript.spec.ts`

**Step 1: Write the failing test**

Add tests verifying:
- transcript persistence still stores assistant reasoning/tool calls/failure entries for UI
- native provider metadata is not required by the web transcript renderer
- switching models still reuses persisted transcript correctly

**Step 2: Run test to verify it fails**

Run: `go test ./core/agent -run TestAgentExecutorPersistsTranscriptIndependentlyOfNativeResponsesState`

Run: `npm test -- --run src/lib/transcript.spec.ts`

Expected: FAIL if transcript projection is still coupled to provider continuation assumptions.

**Step 3: Write minimal implementation**

Keep current persistence/UI behavior, but make it explicit that transcript is a projection of completed turns, not the source of truth for `/responses` continuation.

**Step 4: Run test to verify it passes**

Run: `go test ./core/agent -run TestAgentExecutorPersistsTranscriptIndependentlyOfNativeResponsesState`

Run: `npm test -- --run src/lib/transcript.spec.ts`

Expected: PASS

### Task 5: Add cache and transport passthrough surface

**Files:**
- Modify: `core/providers/types/types.go`
- Modify: `core/providers/client/openai_responses/client.go`
- Modify: `core/providers/client/openai_responses/utils.go`
- Test: `core/providers/client/openai_responses/utils_test.go`

**Step 1: Write the failing test**

Add tests that expect request builder/client to forward optional OpenAI-native fields without mutating continuation semantics:
- `prompt_cache_key`
- `prompt_cache_retention`
- optional `conversation`

**Step 2: Run test to verify it fails**

Run: `go test ./core/providers/client/openai_responses -run "TestBuildResponseRequestParams_(ForwardsPromptCacheFields|ForwardsConversationField)"`

Expected: FAIL because current request model does not surface these fields.

**Step 3: Write minimal implementation**

Add optional request metadata fields to provider-specific state/options and pass them through exactly.

**Step 4: Run test to verify it passes**

Run: `go test ./core/providers/client/openai_responses -run "TestBuildResponseRequestParams_(ForwardsPromptCacheFields|ForwardsConversationField)"`

Expected: PASS

### Task 6: Verify against live backend and raw gateway calls

**Files:**
- Modify: only files above as needed
- Test: live backend on `http://127.0.0.1:18080`

**Step 1: Create verification matrix**

Verify these cases separately:
- same-model user follow-up
- same-model tool follow-up
- model switch fallback replay
- transcript persistence after failure

**Step 2: Run verification**

Run:
- `go test ./...`
- `npm test && npm run build`
- raw `/responses` probes against current gateway
- backend task probes against `18080`

Expected: either successful continuation or a gateway-only incompatibility that occurs with the exact same raw body your backend now emits.

### Task 7: Final cleanup and documentation

**Files:**
- Modify: `docs/plans/2026-03-18-ai-sdk-style-responses-rework.md` if findings require notes
- Modify: touched code files only

**Step 1: Remove obsolete transcript-driven continuation helpers**

Delete or downgrade helpers that infer provider continuation from generic assistant/tool transcript ordering once native state exists.

**Step 2: Run final verification**

Run:
- `go test ./...`
- `npm test && npm run build`

Expected: PASS
