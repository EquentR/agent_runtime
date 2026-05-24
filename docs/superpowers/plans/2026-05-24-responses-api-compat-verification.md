# Responses API Compatibility Verification Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Verify each special OpenAI Responses API compatibility rule documented in `ResponsesAPI踩坑记录.md` against `https://c2.ice-starter.cn/v1`, then remove only the compatibility rules that the live API and repository tests prove are unnecessary or harmful.

**Architecture:** Use a three-layer verification pass: raw HTTP probes isolate the upstream API behavior, local request-builder snapshots prove what this adapter sends, and focused Go tests protect the final code change. Do not remove compatibility code until the live probe result and local regression test agree.

**Tech Stack:** Go 1.25, `github.com/openai/openai-go/v3`, raw `/v1/responses` HTTP probes, `go test`, models `gpt-5.4` and `gpt-5.5`.

---

## Scope And Safety

- Do not store the API key in the repository, shell history scripts, test fixtures, or logs.
- Use environment variables only:

```powershell
$env:RESPONSES_TEST_BASE_URL = 'https://c2.ice-starter.cn/v1'
$env:RESPONSES_TEST_API_KEY = '<provided key>'
$env:RESPONSES_TEST_MODEL = 'gpt-5.4'
```

- Default to `gpt-5.4` for cost. Use `gpt-5.5` only for a final smoke pass and any `gpt-5.4`/`gpt-5.5` behavior difference that affects removal decisions.
- Keep prompts tiny and cap output:
  - normal text probes: `max_output_tokens: 32`
  - tool-call probes: `max_output_tokens: 96`
  - reasoning-specific probes: use `reasoning.effort: none` except when explicitly validating current adapter shape `reasoning: {effort: "medium", summary: "auto"}`
- Capture only redacted request/response summaries under `tmp/responses-api-probes/`:
  - HTTP status
  - request id header if present
  - response id
  - output item types
  - function `call_id`, tool name, and whether arguments parse as JSON
  - usage tokens
  - error type/message
- Never commit `tmp/responses-api-probes/`.

## Source Baseline

Official docs used for the test oracle:

- Responses supports manual conversation state by including prior output as new input, and also supports `previous_response_id` when stored state is available.
- Function calls in Responses are output items with `type: "function_call"`, `call_id`, `name`, and JSON-encoded `arguments`.
- Function results are returned as input items with `type: "function_call_output"`, `call_id`, and string `output`.
- The official function-calling continuation example sends the continuation with `tools` still present.
- For reasoning models with function calls, OpenAI recommends passing relevant reasoning items, function call items, and function call output items forward; stateless mode needs `include: ["reasoning.encrypted_content"]` if encrypted reasoning must be replayed.
- `gpt-5.4` and `gpt-5.5` both support Responses and function calling. Their reasoning-effort defaults differ: `gpt-5.4` documents `none` as default; `gpt-5.5` documents `medium` as default.

Current local compatibility points to test:

- `core/providers/client/openai_responses/utils.go`
  - `getResponsesModelConfig`
  - `buildResponseRequestParams`
  - `requestEndsWithToolContinuation`
  - `buildResponseInput`
  - `filterReplayOutputItems`
  - `filterReplayInputItems`
  - `responseInputHasToolOutput` (currently dead code)
- `core/providers/client/openai_responses/provider_state.go`
  - `providerStateFromOutputItems`
  - `outputItemsFromProviderState`
  - `providerStateItemReferences`
  - `rawOutputItemsFromProviderData`
- `core/providers/client/openai_responses/client.go` and `stream.go`
  - stream output-item archive and final assistant message assembly

## Decision Rules

For each compatibility rule:

- **Remove** only if the standard/no-compat request shape succeeds on the live API, the current compat shape is rejected or observably degrades tool behavior, and local tests can lock the safer standard shape.
- **Keep** if the standard shape is rejected, returns malformed tool arguments, loses required replay state, or works only by relying on `store: true` while the adapter still sends `store: false`.
- **Narrow** if a rule is needed only for a model family not covered by this API. Example: do not remove `o1-mini` system-message handling based only on `gpt-5.4` and `gpt-5.5` probes.
- **Defer** if the live API accepts both shapes and neither local behavior nor official docs proves one is safer.

## Compatibility Matrix

| ID | Record section | Current behavior | Suspected issue | Live probes | Removal candidate |
| --- | --- | --- | --- | --- | --- |
| C1 | Multi-turn replay | Adapter replays full prior assistant output in stateless `input` | Single-turn success can hide replay bugs | P0, P4, P5, P6 | No direct code removal; only test coverage |
| C2 | Tool output does not always mean continuation | `requestEndsWithToolContinuation` removes `tools` only when tail is assistant tool call + trailing tool result | Current code still removes `tools` during real continuation; official examples keep `tools` | P4-A/B/C, P6 | Likely remove `params.Tools = nil` for continuation if live API accepts tools |
| C3 | Prefer full output over `item_reference` | ProviderData/ProviderState output archive is replayed before item references | Full replay may be unnecessary if upstream supports stored references, but `store=false` may require it | P5-A/B/C | Keep full replay unless `store=false` references work |
| C4 | Drop non-function items during continuation | Continuation filters replay to `function_call` only, then appends `function_call_output` | Official reasoning docs recommend passing reasoning items for reasoning+function calls | P4-B/C/D, P6 | Remove or narrow filtering if full relevant replay succeeds |
| C5.1 | System role mapping | `gpt-5*` system messages become `developer` | May be unnecessary and can change instruction semantics | P2-A/B | Remove mapping for `gpt-5.4/5.5` only if `system` works and behavior remains correct |
| C5.2 | Reasoning param gating | Reasoning is sent for model IDs detected as reasoning models | `summary:auto` or forced `effort:medium` may cause proxy parameter errors/cost | P3-A/B/C/D | Change default reasoning shape if live API rejects or defaults are safer |
| C5.3 | Sampling omission | Temperature/top_p omitted for reasoning models | User sampling is silently dropped; if API accepts these params, omission causes feature loss | P3-E/F | Remove omission if live API accepts and behavior is valid |
| C6 | ProviderState for replay | Full output and item ids are stored; encrypted reasoning is preserved only if returned | Adapter never requests `include:["reasoning.encrypted_content"]` | P6-A/B | Add include if needed; do not remove archive based on non-reasoning probes |

## Probe Payload Conventions

Common tool schema:

```json
{
  "type": "function",
  "name": "echo_payload",
  "description": "Echoes a short JSON payload for protocol testing.",
  "parameters": {
    "type": "object",
    "properties": {
      "value": { "type": "string", "description": "A short value." },
      "step": { "type": "integer", "description": "The protocol step number." }
    },
    "required": ["value", "step"],
    "additionalProperties": false
  },
  "strict": true
}
```

Common first-turn prompt:

```text
Call echo_payload exactly once with value "alpha" and step 1.
```

Common synthetic tool result:

```json
{"ok":true,"value":"alpha","step":1}
```

Tool arguments pass criteria:

- `arguments` must be valid JSON.
- `arguments.value == "alpha"`.
- `arguments.step == 1`.
- `call_id` must be non-empty and reused by the matching `function_call_output`.

## Task 1: Build Raw Probe Harness

**Files:**
- Create after approval: `scripts/responses_probe/README.md`
- Create after approval: `scripts/responses_probe/probe.ps1` or `scripts/responses_probe/main.go`
- Output after approval: `tmp/responses-api-probes/*.json`

- [ ] **Step 1: Create a redacted probe runner**

Implement a tiny runner that sends JSON to `$env:RESPONSES_TEST_BASE_URL/responses`, redacts authorization, writes request/response summaries, and exits non-zero on transport errors.

- [ ] **Step 2: Add a response summarizer**

Summarize `id`, `status`, `output[].type`, function call fields, usage, and errors. Preserve enough raw response in `tmp/` to debug malformed JSON, but never write the API key.

- [ ] **Step 3: Dry-run without network**

Run the harness with a fixture response and confirm redaction works before using the live key.

Expected: no API key in files or terminal output.

## Task 2: Basic Responses And Streaming Smoke

**Files:**
- Output after approval: `tmp/responses-api-probes/P1-*.json`

- [ ] **P1-A: Basic non-stream response, `gpt-5.4`**

Payload:

```json
{
  "model": "gpt-5.4",
  "input": "Reply with exactly: OK",
  "store": false,
  "max_output_tokens": 32
}
```

Pass: HTTP 200, response has parseable JSON, `output_text` or message text contains `OK`.

- [ ] **P1-B: Basic non-stream response, `gpt-5.5`**

Same as P1-A with `model: "gpt-5.5"`.

Pass: same as P1-A.

- [ ] **P1-C: Streaming response, `gpt-5.4`**

Payload adds `"stream": true`.

Pass: SSE contains response events and completes without malformed JSON/event parsing errors.

## Task 3: System/Developer Role And Top-Level Parameter Probes

**Files:**
- Output after approval: `tmp/responses-api-probes/P2-*.json`
- Output after approval: `tmp/responses-api-probes/P3-*.json`

- [ ] **P2-A: `system` role accepted for `gpt-5.4`**

Payload input:

```json
[
  { "role": "system", "content": "You must answer with exactly SYS_OK." },
  { "role": "user", "content": "Say the required token." }
]
```

Pass: HTTP 200 and answer contains `SYS_OK`.

- [ ] **P2-B: `developer` role accepted for `gpt-5.4`**

Same as P2-A with role `developer` and expected token `DEV_OK`.

Pass: HTTP 200 and answer contains `DEV_OK`.

- [ ] **P2-C: Repeat P2-A/P2-B for `gpt-5.5` only if `gpt-5.4` differs or if removing gpt-5 role mapping**

Pass: same as P2-A/P2-B.

- [ ] **P3-A: Current adapter reasoning shape, `gpt-5.4`**

Payload includes:

```json
"reasoning": { "effort": "medium", "summary": "auto" }
```

Pass: HTTP 200. Fail if API returns unknown/unsupported parameter.

- [ ] **P3-B: No reasoning object, `gpt-5.4`**

Pass: HTTP 200. Compare usage and output shape with P3-A.

- [ ] **P3-C: `reasoning.effort: none`, `gpt-5.4`**

Pass: HTTP 200. This is the documented default-equivalent shape for `gpt-5.4`.

- [ ] **P3-D: Current adapter reasoning shape, `gpt-5.5`**

Pass: HTTP 200. Fail if API rejects `summary:auto` or `effort:medium`.

- [ ] **P3-E: Sampling params with reasoning, `gpt-5.4`**

Payload includes:

```json
"reasoning": { "effort": "none" },
"temperature": 0.2,
"top_p": 0.9
```

Pass: HTTP 200. If accepted, current omission of sampling for reasoning models is a removal candidate.

- [ ] **P3-F: Sampling params with no reasoning object, `gpt-5.4`**

Payload includes temperature/top_p but no reasoning.

Pass: HTTP 200. Compare with P3-E to distinguish reasoning-param conflict from model-level conflict.

## Task 4: Function Calling And Tool Continuation Matrix

**Files:**
- Output after approval: `tmp/responses-api-probes/P4-*.json`

- [ ] **P4-A: First turn forced tool call**

Payload:

```json
{
  "model": "gpt-5.4",
  "input": [
    { "role": "user", "content": "Call echo_payload exactly once with value \"alpha\" and step 1." }
  ],
  "tools": [ "<common tool schema>" ],
  "tool_choice": { "type": "function", "name": "echo_payload" },
  "store": false,
  "max_output_tokens": 96
}
```

Pass: output has a `function_call` item with valid JSON arguments and non-empty `call_id`.

- [ ] **P4-B: Continuation with current adapter shape**

Input contains the previous `function_call` item and one `function_call_output`; `tools` omitted.

Pass today means the old compat works. Fail or malformed final response means this compat is actively harmful.

- [ ] **P4-C: Continuation with official shape**

Same input as P4-B, but keep `tools` present.

Pass: HTTP 200 and final response incorporates the tool result. If P4-C passes and P4-B fails or prevents consecutive tool calls, remove `params.Tools = nil` for continuation.

- [ ] **P4-D: Continuation with reasoning/message/function_call replay plus `function_call_output` and `tools`**

Use the first response's full output items plus the tool output, without filtering non-function items.

Pass: HTTP 200. If this passes and P4-B/P4-C filtered forms are worse, remove or narrow `filterReplayOutputItems` / `filterReplayInputItems`.

- [ ] **P4-E: Consecutive tool-call ability after tool output**

Use two tools: `echo_payload` and `second_payload`. The tool result says: `{"next":"call second_payload with value beta and step 2"}`.

Run continuation with tools present and omitted.

Pass: with tools present, the model can emit a second `function_call` when instructed. If tools omitted prevents this, remove continuation tool stripping.

## Task 5: ProviderState Replay, Full Output, And Item References

**Files:**
- Output after approval: `tmp/responses-api-probes/P5-*.json`

- [ ] **P5-A: Stateless full-output replay**

Use P4-A response output items in a second request as input, then append a new user message asking for a brief summary.

Pass: HTTP 200. This proves full archive replay remains valid with `store:false`.

- [ ] **P5-B: `item_reference` with `store:false`**

Attempt to replay P4-A output items by `item_reference` IDs instead of full output items.

Pass: HTTP 200. Fail is expected if the API cannot resolve non-stored response items. If it fails, keep full output archive and remove/narrow item-reference fallback if it is causing errors.

- [ ] **P5-C: `item_reference` with `store:true`**

Repeat first turn with `store:true`, then replay by item references.

Pass: HTTP 200. This does not justify removing full output replay while production still uses `store:false`; it only documents whether a future stored-state mode can use references.

- [ ] **P5-D: `previous_response_id` with `store:false` and `store:true`**

Verify whether the custom API accepts `previous_response_id` for both storage modes.

Pass with `store:true` only is expected. Do not switch production to `previous_response_id` unless storage policy changes.

## Task 6: Reasoning Replay And Encrypted Content

**Files:**
- Output after approval: `tmp/responses-api-probes/P6-*.json`

- [ ] **P6-A: Reasoning + tool call with `include: ["reasoning.encrypted_content"]`**

Payload uses `gpt-5.4`, `reasoning: { "effort": "low", "summary": "auto" }`, `include: ["reasoning.encrypted_content"]`, forced `echo_payload` tool.

Pass: HTTP 200. Record whether reasoning output items include `encrypted_content`.

- [ ] **P6-B: Continuation replaying reasoning + function_call + function_call_output**

Use P6-A output items and append the tool output. Keep `tools` present.

Pass: HTTP 200. If it succeeds, the adapter should preserve and replay reasoning items instead of filtering them out during continuation.

- [ ] **P6-C: Same continuation without reasoning items**

Use only `function_call` + `function_call_output`.

Pass: HTTP 200. Compare response quality/status with P6-B. If both pass, keep a cost/risk note; official docs still prefer carrying reasoning items for reasoning+tools.

## Task 7: Local Adapter Snapshot Tests Before Code Removal

**Files:**
- Modify after approval: `core/providers/client/openai_responses/utils_test.go`
- Modify after approval: `core/providers/client/openai_responses/provider_state.go`
- Modify after approval only as needed: `core/providers/client/openai_responses/utils.go`

- [ ] **Step 1: Add tests that describe the chosen post-probe request shape**

Candidate tests, depending on live results:

- `TestBuildResponseRequestParams_KeepsToolsForToolContinuation`
- `TestBuildResponseRequestParams_ReplaysReasoningItemsForToolContinuation`
- `TestBuildResponseRequestParams_PreservesSamplingForGPT54WhenAccepted`
- `TestBuildResponseRequestParams_UsesSystemRoleForGPT54WhenAccepted`
- `TestBuildResponseRequestParams_RequestsEncryptedReasoningWhenNeeded`
- `TestProviderStateItemReferenceFallbackRemovedOrScoped`

- [ ] **Step 2: Run focused tests and confirm they fail before removal**

Run:

```powershell
go test ./core/providers/client/openai_responses -run "TestBuildResponseRequestParams|TestProviderState" -count=1
```

Expected: at least the tests for the chosen removals fail against current compatibility behavior.

## Task 8: Remove Or Narrow Only Proven Compatibility Rules

**Files:**
- Modify after approval: `core/providers/client/openai_responses/utils.go`
- Modify after approval: `core/providers/client/openai_responses/provider_state.go`
- Modify after approval: `core/providers/client/openai_responses/utils_test.go`
- Modify after approval as needed: `core/providers/client/openai_responses/stream_test.go`

- [ ] **Step 1: Remove continuation tool stripping if P4-C/P4-E prove tools are valid and necessary**

Expected code direction:

- Delete or narrow this block:

```go
if requestEndsWithToolContinuation(req.Messages) {
	params.Tools = nil
}
```

- Keep `requestEndsWithToolContinuation` only if still needed for replay filtering, or delete it if P4-D/P6-B remove that need too.

- [ ] **Step 2: Remove or narrow continuation replay filtering if P4-D/P6-B prove full relevant output replay is valid**

Expected code direction:

- Keep output item order.
- Do not drop reasoning items needed for reasoning+function calling.
- Avoid replaying unrelated old assistant messages only if live probes show that specific shape is rejected.

- [ ] **Step 3: Adjust GPT-5.4/5.5 top-level parameter handling if P2/P3 prove current mapping is unnecessary**

Possible outcomes:

- Use `system` role for `gpt-5.4/5.5` if accepted and behavior matches.
- Stop forcing `reasoning.effort=medium` for `gpt-5.4` if the no-reasoning or `none` shape is accepted and preferred.
- Preserve `temperature/top_p` for `gpt-5.4/5.5` if accepted.
- Keep `o1-mini` / `o1-preview` handling unless separately tested.

- [ ] **Step 4: ProviderState cleanup**

Possible outcomes:

- Keep full output archive for stateless production.
- Remove dead `responseInputHasToolOutput`.
- Remove or scope `providerStateItemReferences` if item-reference replay is rejected or fragile under `store:false`.
- Add `include: ["reasoning.encrypted_content"]` if P6 shows it is required for correct stateless reasoning replay.

## Task 9: Verification

**Files:**
- Verify only

- [ ] **Step 1: Focused package tests**

Run:

```powershell
go test ./core/providers/client/openai_responses -count=1
```

Expected: PASS.

- [ ] **Step 2: Provider client tests**

Run:

```powershell
go test ./core/providers/client/... -count=1
```

Expected: PASS.

- [ ] **Step 3: Full backend tests**

Run:

```powershell
go test ./...
```

Expected: PASS.

- [ ] **Step 4: Build**

Run:

```powershell
go build ./cmd/...
```

Expected: PASS.

- [ ] **Step 5: Optional live adapter smoke**

Run only after unit tests pass and only with the API key in env:

- One `gpt-5.4` adapter ChatStream forced-tool turn.
- One `gpt-5.4` adapter continuation with tool result.
- One `gpt-5.5` basic response smoke.

Expected: no JSON parse failures, no parameter errors, tool arguments parse as JSON, final message has replayable `ProviderState`.

## Exit Criteria

The change is ready only when:

- Each record section in `ResponsesAPI踩坑记录.md` has a probe result and a keep/remove/narrow decision.
- Every removed compatibility rule has a failing-then-passing local test.
- No API key or live raw response with secrets is committed.
- `go test ./core/providers/client/openai_responses -count=1`, `go test ./...`, and `go build ./cmd/...` pass.
- The final report lists:
  - live probe matrix results
  - exact compatibility code removed or kept
  - any API behavior differences between `gpt-5.4` and `gpt-5.5`
  - remaining known risks

## Live Probe Results

Executed against `https://c2.ice-starter.cn/v1` on 2026-05-24. The API key was passed only in the shell process and was not written to repository files.

| Probe | Result | Decision |
| --- | --- | --- |
| P1-A `gpt-5.4` basic response | HTTP 200, `message`, text `OK`, 27 total tokens | API baseline works |
| P1-B `gpt-5.5` basic response | HTTP 200, `message`, text `OK`, 39 total tokens, 10 reasoning tokens | Keep `gpt-5.4` as default probe model for cost |
| P1-C `gpt-5.4` stream | HTTP 200, SSE events through `response.completed`, text `OK` | Streaming baseline works |
| P2-A/P2-B `gpt-5.4` system/developer | Both HTTP 200; required tokens returned | Do not force `developer` for `gpt-5.4` |
| P2-C/P2-D `gpt-5.5` system/developer | Both HTTP 200; required tokens returned | Do not force `developer` for `gpt-5.5` |
| P3-A current reasoning shape | HTTP 200 with `reasoning.effort=medium, summary=auto` | No parameter rejection |
| P3-B no reasoning object | HTTP 200 | Reasoning object is not required for basic `gpt-5.4` |
| P3-C `reasoning.effort=none` | HTTP 200 | Low-cost/no-reasoning shape accepted |
| P3-D `gpt-5.5` current reasoning shape | HTTP 200 | No parameter rejection |
| P3-E/P3-F sampling with and without reasoning | Both HTTP 200 | Do not strip `temperature/top_p` for `gpt-5.4` |
| P4-A forced tool call | HTTP 200, `function_call`, valid JSON args, non-empty `call_id` | Tool baseline works |
| P4-B continuation without tools | HTTP 200 but only generic text; no second tool possible | Current tool-stripping behavior is harmful |
| P4-C continuation with tools | HTTP 200, closed first tool result | Official shape accepted |
| P4-D full output + tool output + tools | HTTP 200 | Full replay shape accepted |
| P4-E second tool with tools | HTTP 200, emitted `second_payload` with valid JSON args | Keep tools during continuation |
| P4-E second tool without tools | HTTP 200, text only, no `function_call` | Confirms tool stripping causes functional loss |
| P5-A2 closed full replay + new user | HTTP 200, returned requested token | Full output archive replay works |
| P5-A3 simple output replay + new user | HTTP 200, returned requested token | Non-tool full replay works |
| P5-B item_reference with `store:false` | HTTP 502 upstream error | Remove normal `item_reference` fallback |
| P5-C item_reference after requested `store:true` | HTTP 502 upstream error; proxy response still reported `store:false` | Do not rely on item references in this API |
| P5-D/P5-E `previous_response_id` | HTTP 400: only supported on Responses WebSocket v2 | Do not switch to `previous_response_id` |
| P6-A reasoning+tool+include encrypted | HTTP 200, `function_call`, no encrypted reasoning returned | No `include` implementation needed from this API behavior alone |
| P6-B full reasoning continuation | HTTP 200, output included `reasoning,message` | Do not drop reasoning items in continuation |
| P6-C function-only reasoning continuation | HTTP 200, `message`, no reasoning | Function-only replay works but loses reasoning continuity |

## Decisions Implemented

- Removed continuation-time `tools` stripping from `buildResponseRequestParams`.
- Removed continuation replay filtering that dropped `reasoning` and assistant `message` output items.
- Removed `item_reference` fallback from ProviderState replay; when full output archive is absent, the adapter now falls back to normalized assistant content/tool calls.
- Removed dead helpers for old continuation detection/filtering.
- Stopped classifying generic `gpt-5*` model IDs as special reasoning models for request shaping. This preserves `system`, `temperature`, and `top_p` for `gpt-5.4`/`gpt-5.5` while keeping existing o-series/codex/computer-use rules.

## Verification Run

- `go test ./scripts/responses_probe -count=1`: PASS
- `go test ./core/providers/client/openai_responses -count=1`: PASS
- `go test ./core/providers/client/... -count=1`: PASS

## Live Integration Test Run

Added gated live tests in `core/providers/client/openai_responses/live_integration_test.go`. They run only when `RESPONSES_LIVE_TEST=1` and require `RESPONSES_TEST_BASE_URL`, `RESPONSES_TEST_API_KEY`, and `RESPONSES_TEST_MODEL`.

Executed against `https://c2.ice-starter.cn/v1` with `gpt-5.4` on 2026-05-24:

```powershell
$env:RESPONSES_LIVE_TEST='1'
$env:RESPONSES_TEST_BASE_URL='https://c2.ice-starter.cn/v1'
$env:RESPONSES_TEST_MODEL='gpt-5.4'
go test ./core/providers/client/openai_responses -run '^TestLiveResponsesClient' -count=1 -v
```

Result: PASS in 40.893s.

Covered live cases:

- `TestLiveResponsesClientConsecutiveToolCalls`: two sequential tool calls, then final answer.
- `TestLiveResponsesClientFourStepToolChain`: four sequential tool-call turns across growing replay history, then final answer.
- `TestLiveResponsesClientMultipleToolCallsInOneTurn`: two tool calls emitted in one model turn, both tool outputs replayed, then final answer.
- `TestLiveResponsesClientSystemSamplingAndNewUserToolAfterHistory`: `system` role plus `temperature/top_p`, then a later user turn with tool calling after prior assistant history.
- `TestLiveResponsesClientFullProviderStateReplayAndRefsOnlyFallback`: full ProviderState output replay succeeds; refs-only ProviderState falls back to normalized assistant replay instead of `item_reference`.

Observed successful live call IDs from the final full-suite run:

- Consecutive tools: `call_kZQl5aqC7b9ZiRWmgpKUubHQ`, `call_q2qcuXXzGrPg2NePM2D6OlsA`.
- Four-step chain: `call_8Cyx5fIztOTk6f0cLFQZHN4k`, `call_eQC2Hp7c4TvyvDmXQljCtU1z`, `call_9nLO9FXxBJKVRMCzKczfx2Bx`, `call_VVzfmThEl9oxHrM9AHnK2wMX`.
- Multiple tools in one turn: `call_PFd7f5V6KtaEJcp1RMN4Qzed`, `call_cFeTpxDtDwGrt40SGF1HYetz`.
- System/sampling follow-up tool: `call_ejmONYHNvWFg6aUY8BRr6Tnv`.

Default non-live verification after adding the gated tests:

- `go test ./core/providers/client/openai_responses -count=1`: PASS.
