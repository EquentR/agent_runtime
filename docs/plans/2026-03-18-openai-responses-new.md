# OpenAI Responses New Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a new OpenAI Responses API wrapper under `core/providers/client/openai_responses_new` that follows this repository's provider interfaces while borrowing the proven request/response mapping ideas from `E:\Develop\OpenSource\fantasy\providers\openai`.

**Architecture:** Reuse this repository's normalized `model.ChatRequest`, `model.ChatResponse`, `model.Stream`, provider-state replay model, and token accounting. Port the fantasy Responses-specific prompt/tool/reasoning mapping patterns into a fresh package instead of trying to salvage `core/providers/client/openai_responses`.

**Tech Stack:** Go, `github.com/openai/openai-go/v3`, existing provider/model abstractions in `core/providers/types` and `core/types`.

---

### Task 1: Lock request-shaping behavior with tests

**Files:**
- Create: `core/providers/client/openai_responses_new/utils_test.go`
- Reference: `core/providers/client/openai_responses/utils_test.go`
- Reference: `E:\Develop\OpenSource\fantasy\providers\openai\responses_language_model.go`

1. Add tests for prompt/message conversion, tool conversion, reasoning replay, and provider-state replay.
2. Run the package tests and confirm the new tests fail because the package does not exist yet.

### Task 2: Implement request/response mapping helpers

**Files:**
- Create: `core/providers/client/openai_responses_new/utils.go`
- Create: `core/providers/client/openai_responses_new/provider_state.go`

1. Implement request param building for user/system/assistant/tool messages, tool schema conversion, and tool choice conversion.
2. Implement response extraction, reasoning item conversion, and provider-state serialization/replay helpers.

### Task 3: Lock stream assembly behavior with tests

**Files:**
- Create: `core/providers/client/openai_responses_new/stream_test.go`
- Reference: `core/providers/client/openai_responses/stream_test.go`

1. Add tests for stream tool-call assembly, reasoning deltas, output-item ordering, finish reason mapping, and final message replay state.
2. Run the new package tests and confirm they fail for the missing stream implementation.

### Task 4: Implement client and streaming wrapper

**Files:**
- Create: `core/providers/client/openai_responses_new/client.go`
- Create: `core/providers/client/openai_responses_new/stream.go`

1. Implement `Client`, `Chat`, `ChatStream`, stream event handling, token counting, and final message assembly.
2. Keep the implementation compatible with this repository's `model.Stream` contract and existing normalized message model.

### Task 5: Verify

**Files:**
- Verify only

1. Run `go test ./core/providers/client/openai_responses_new`.
2. Run `go test ./...`.
3. Fix any regressions before reporting completion.
