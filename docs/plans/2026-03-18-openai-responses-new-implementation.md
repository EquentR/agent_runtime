# OpenAI Responses New Wrapper Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a fresh `core/providers/client/openai_responses_new` adapter that uses the OpenAI Responses API in stateless `store=false` mode while supporting prompt-cache hints, function calling, streaming, non-streaming, and multi-turn replay through the existing outer provider interfaces.

**Architecture:** Build a clean wrapper around `github.com/openai/openai-go/v3` that converts `core/providers/types.ChatRequest` into stateless Responses input items, always includes replay-safe reasoning metadata, and normalizes full responses plus streaming events back into `core/providers/types`. Preserve exact assistant output items in provider state so later agent-loop turns can resend prior assistant reasoning/tool-call state without relying on `previous_response_id`.

**Tech Stack:** Go, `openai-go/v3`, `httptest`, existing provider/model interfaces.

---

### Task 1: Lock the contract with failing builder tests

**Files:**
- Create: `core/providers/client/openai_responses_new/request_test.go`
- Create: `core/providers/client/openai_responses_new/testdata/` (only if needed)

**Step 1: Write the failing tests**

- Verify requests always send `store=false` and include `reasoning.encrypted_content`.
- Verify `PromptCacheKey` and `PromptCacheRetention` are forwarded.
- Verify same-provider assistant `ProviderState` is replayed as exact Responses output items.
- Verify normalized assistant fallback replays message + reasoning items + function calls when provider state is missing.
- Verify tool outputs are sent as `function_call_output` items.

**Step 2: Run the focused test file to verify it fails**

Run: `go test ./core/providers/client/openai_responses_new -run TestBuild`

**Step 3: Implement the minimal request-building helpers**

- Add request builder helpers that translate outer `Message` values into Responses input items.
- Add JSON-schema-to-function-tool conversion.
- Add stateless provider-state archive helpers.

**Step 4: Run the focused test file to verify it passes**

Run: `go test ./core/providers/client/openai_responses_new -run TestBuild`

### Task 2: Lock full-response normalization with failing client tests

**Files:**
- Create: `core/providers/client/openai_responses_new/client_test.go`
- Create: `core/providers/client/openai_responses_new/client.go`
- Create: `core/providers/client/openai_responses_new/state.go`

**Step 1: Write the failing tests**

- Verify `Chat` converts a JSON Responses payload into normalized assistant content, reasoning, reasoning items, tool calls, usage, and replayable provider state.
- Verify a second turn resends the first turn's provider state even if normalized fields were mutated.

**Step 2: Run the focused test file to verify it fails**

Run: `go test ./core/providers/client/openai_responses_new -run TestChat`

**Step 3: Implement the minimal production code**

- Add client constructor and `Chat`.
- Add response parsing helpers and usage extraction.
- Archive raw output items into `Message.ProviderState` and `Message.ProviderData`.

**Step 4: Run the focused test file to verify it passes**

Run: `go test ./core/providers/client/openai_responses_new -run TestChat`

### Task 3: Lock streaming behavior with failing stream tests

**Files:**
- Create: `core/providers/client/openai_responses_new/stream_test.go`
- Create: `core/providers/client/openai_responses_new/stream.go`

**Step 1: Write the failing tests**

- Verify `ChatStream` emits text, reasoning, tool-call, usage, and completed events.
- Verify `FinalMessage()` returns the replayable assistant message with provider state.
- Verify canceled/error streams do not return partial final messages.

**Step 2: Run the focused test file to verify it fails**

Run: `go test ./core/providers/client/openai_responses_new -run TestChatStream`

**Step 3: Implement the minimal streaming code**

- Wrap `Responses.NewStreaming`.
- Accumulate deltas, tool calls, and final response payload.
- Emit normalized `core/providers/types.StreamEvent` values.

**Step 4: Run the focused test file to verify it passes**

Run: `go test ./core/providers/client/openai_responses_new -run TestChatStream`

### Task 4: Verify the new adapter end-to-end

**Files:**
- Modify: `core/providers/client/openai_responses_new/*.go`

**Step 1: Run package tests**

Run: `go test ./core/providers/client/openai_responses_new`

**Step 2: Run full repository tests**

Run: `go test ./...`

**Step 3: Fix any regressions and re-run verification**

- Keep changes scoped to the new package unless a real outer-interface bug is uncovered.
