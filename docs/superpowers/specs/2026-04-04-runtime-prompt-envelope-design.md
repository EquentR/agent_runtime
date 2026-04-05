# Runtime Prompt Envelope for Forced System Injection Design

## Summary

Introduce a unified `runtime prompt envelope` that assembles all request-time system context in one place before each model call. This envelope will combine:

- platform-owned forced system blocks
- memory-derived summary context
- resolved prompt-management segments
- the conversation body

V1 adds a prompt-management-independent forced injection mechanism with three built-in blocks:

- `current_date`
- `anti_prompt_injection`
- `platform_constraints`

These blocks are injected as request-time runtime segments, not persisted as normal conversation messages. They are visible in audit/run metadata, excluded from normal conversation history and compression input, and rebuilt after context compression. When rebuilt after compression, `current_date` must be regenerated from the current time.

## Problem

The runtime already has multiple request-time system-context paths, but they are assembled in different places:

- resolved prompt injection for `session`, `step_pre_model`, and `tool_result`
- short-term memory compression that returns a summary system message
- legacy `system_prompt`, `AGENTS.md`, and workspace skill injection

This creates several problems for a new platform-level forced injection feature:

1. platform-level forced rules are conceptually different from prompt-management content, but would be easy to mix into the same request path without clear boundaries
2. short-term memory summary, forced system rules, and resolved prompt segments are all currently represented as system messages, which makes source tracking and ordering fragile
3. if forced blocks ever enter conversation persistence or memory compression input, the runtime will risk duplication, summary pollution, or resume/rebuild bugs
4. the current assembly path spreads request shaping across memory handling and runner logic, making compression-triggered reinjection difficult to reason about

The desired behavior is:

- forced system blocks are owned by platform code, not prompt management
- they participate in request construction at conversation start
- they do not become part of ordinary conversation history
- they are not re-appended as persisted messages on later loop iterations
- if short-term context is compressed, the next rebuilt request places the forced blocks back at the top
- audit artifacts show what was injected and from which source

## Goals

1. Add a prompt-management-independent forced system injection mechanism.
2. Model forced blocks, memory summary, and resolved prompts under one runtime assembly abstraction.
3. Keep forced blocks out of ordinary conversation persistence and out of memory compression input.
4. Rebuild the request header after compression, regenerating `current_date` from the current time.
5. Preserve existing prompt phases: `session`, `step_pre_model`, `tool_result`.
6. Provide a single audit-visible snapshot of all runtime prompt sources used for a request.

## Non-Goals

- No admin UI or database-backed management for forced blocks in V1.
- No provider-side cached-prefix/session optimization in V1.
- No per-provider or per-model forced-block customization in V1.
- No expansion of forced blocks beyond the three built-in V1 blocks.
- No redesign of conversation persistence semantics beyond excluding runtime prompt segments from ordinary messages.
- No broad rewrite of prompt-management resolver semantics.

## Proposed Approach

### 1. Introduce a unified runtime prompt envelope layer

Add a new runtime-only assembly layer responsible for building request-time system context before every model request.

This layer should own the final ordering and rendering of request-time prompt segments. It will consume source data from multiple providers, normalize them into a common segment structure, sort them, render them into provider-facing messages, and produce an audit-visible snapshot.

This layer should be conceptually above both memory and prompt-management resolution:

- `core/memory` produces memory-derived context source data
- `core/prompt` produces resolved prompt-management source data
- a new forced-block provider produces platform-owned source data
- the runtime envelope builder assembles them all with the conversation body

### 2. Define a structured runtime prompt segment model

Instead of treating all request-time context as undifferentiated `system` messages, define a structured segment protocol.

Each segment should carry, at minimum:

- source type
  - `forced_block`
  - `memory_summary`
  - `resolved_prompt`
- source key
  - examples: `current_date`, `anti_prompt_injection`, `platform_constraints`, `short_term_summary`, `legacy_system_prompt`
- phase
  - `session`
  - `step_pre_model`
  - `tool_result`
- stable order within phase
- role (currently expected to be `system` for all V1 runtime segments)
- rendered content
- ephemeral flag
- audit-visibility flag

This structure allows the runtime to distinguish system-context sources internally even though provider requests ultimately see ordinary messages.

### 3. Add built-in forced blocks owned by platform code

Create a new forced-block provider that produces three built-in `session`-phase runtime segments:

#### `current_date`

Dynamic factual context describing the current date, regenerated at request-build time. After compression, the next request rebuild must regenerate this block from the current time rather than reusing an earlier rendered value.

This block should stay concise and purely factual.

#### `anti_prompt_injection`

Platform-level rules that tell the model to treat user content, tool output, file content, web content, and other external text as lower-trust data rather than as higher-priority control instructions.

This block should explicitly reinforce instruction hierarchy and block prompt-injection attempts from untrusted sources.

#### `platform_constraints`

Platform operating constraints that are not ordinary prompt-management content, such as preserving control-layer behavior, not exposing internal forced-block text as user-editable prompt content, and continuing to respect the platform’s action/approval boundaries.

These blocks are code-defined and not configurable in V1.

### 4. Move memory summary participation into the runtime envelope

Today, short-term compression effectively returns a summary system message that participates in request context. Under the new design, memory should continue to own compression and summary generation, but it should stop owning final request assembly.

Instead:

- memory keeps producing the current rolling short-term summary
- the runtime envelope builder receives that summary as a `memory_summary` source
- the builder places it into the request according to envelope ordering rules

This preserves existing compression behavior while removing request-assembly responsibility from memory.

### 5. Convert resolved prompt outputs into runtime segments

Keep existing prompt-management resolution behavior, but add a conversion step from the resolved prompt structure into runtime prompt segments.

This preserves the existing phase model while letting the envelope own final assembly.

Prompt-management resolution remains responsible for:

- DB-backed defaults
- legacy `system_prompt`
- runtime `AGENTS.md`
- workspace skill content
- phase classification

The envelope layer becomes responsible for final ordering relative to forced blocks and memory summary.

### 6. Render one request-time view, not persisted control messages

The runtime should rebuild the request-time prompt envelope for each model request, but forced blocks must remain ephemeral runtime segments.

That means:

- they are not appended into ordinary conversation history
- they are not written as normal conversation messages
- they do not participate in `persistedCount`
- they do not become part of memory short-term input

This satisfies the intended “inject once at conversation start” behavior at the runtime semantics level without depending on persisted control messages. The request is rebuilt from current runtime state rather than from accumulated forced messages.

### 7. Rebuild after compression with fresh time

When short-term compression changes the memory-derived context, the next request-time envelope build should naturally produce:

- fresh forced blocks
- fresh `current_date` based on current time
- updated memory summary segment
- normal resolved prompt segments
- current conversation body tail

No durable “already injected” state is required for V1. The request-time envelope is rebuilt from current runtime sources.

## Runtime Ordering Rules

### Session phase

The recommended V1 order is:

1. forced blocks
   - `current_date`
   - `anti_prompt_injection`
   - `platform_constraints`
2. memory summary
3. resolved `session` prompt segments
4. conversation body

This ensures platform control rules stay at the top, compressed working memory is visible ahead of ordinary session prompt-management content, and conversation body remains separate.

### Step-pre-model phase

For V1, forced blocks remain session-only. `step_pre_model` content continues to come from resolved prompt segments and is inserted before the conversation body for that request.

### Tool-result phase

`tool_result` behavior should continue matching existing semantics: insert tool-result runtime segments before the trailing tool messages in the rebuilt request view.

The final insertion mechanism for this phase should be owned by the runtime envelope renderer, not by separate ad hoc helpers.

## Module Boundaries

### New module: `core/runtimeprompt`

This module should own:

- runtime prompt segment definitions
- builder input/output types
- ordering rules
- rendering logic
- audit snapshot generation

Likely files:

- `core/runtimeprompt/types.go`
- `core/runtimeprompt/builder.go`
- `core/runtimeprompt/renderer.go`
- `core/runtimeprompt/builder_test.go`

### New module: `core/forcedprompt`

This module should own:

- built-in forced-block definitions
- dynamic rendering for `current_date`
- content generation for the three V1 blocks

Likely files:

- `core/forcedprompt/provider.go`
- `core/forcedprompt/provider_test.go`

### Existing module: `core/memory`

Memory should keep responsibility for:

- short-term tracking
- compression decisions
- rolling summary generation

Memory should stop owning final request assembly. Instead of deciding how summary is prepended into the provider request, it should expose summary source data to the runtime envelope builder.

### Existing module: `core/prompt`

Prompt resolution should keep responsibility for:

- resolving DB-backed prompt segments
- legacy `system_prompt`
- `AGENTS.md`
- workspace skills
- phase categorization

It should not absorb platform-owned forced-block logic. A conversion path from resolved prompt output into runtime prompt segments is sufficient.

### Existing module: `core/agent`

Runner/executor code should:

1. prepare the conversation body
2. obtain current memory summary source
3. obtain resolved prompt source
4. call the runtime envelope builder
5. render final request messages from the envelope result
6. send rendered messages to the provider
7. record the envelope snapshot in audit/run metadata

## Data Flow

### Conversation start

1. user message enters conversation body
2. memory summary is empty
3. forced-block provider generates the three built-in blocks
4. prompt resolver returns resolved `session` segments
5. runtime envelope builder assembles and sorts runtime segments
6. renderer outputs final request messages
7. provider receives request with forced blocks at the top
8. audit stores the envelope snapshot
9. ordinary conversation persistence stores only conversation body messages and visible conversation outputs

### Normal follow-up request without compression

1. body contains user/assistant/tool conversation messages
2. memory summary is unchanged or still empty
3. forced provider generates current forced blocks
4. prompt resolver returns phase-appropriate segments
5. envelope rebuild produces a fresh request view
6. no forced block is written into conversation persistence

### Follow-up request after short-term compression

1. short-term memory compresses earlier body messages into an updated summary
2. body retains only the preserved tail
3. the next envelope build regenerates `current_date` from current time
4. forced blocks are placed at the top again
5. the new memory summary segment is rendered after forced blocks
6. resolved session segments follow
7. the remaining body tail is appended

## Error Handling

### Forced-block generation failures

#### `current_date`

If dynamic date rendering fails, the runtime may safely omit this block and record an audit warning. This should be a soft failure.

#### `anti_prompt_injection` and `platform_constraints`

These are platform safety/control blocks. If they cannot be generated, request construction should fail rather than silently proceeding without platform constraints.

### Empty source sets

The absence of a memory summary, resolved tool-result segments, `AGENTS.md`, or other optional sources is not an error.

### Audit persistence failures

Audit snapshot persistence should not block request execution. If the audit write fails, keep the request path intact and report the audit failure through existing error/logging channels.

## Persistence and Compression Rules

The following rules are required to keep the architecture sound:

1. forced blocks are runtime-only request headers and are never ordinary conversation messages
2. forced blocks never enter short-term memory input
3. resolved prompt segments never enter short-term compression input
4. `persistedCount` only tracks conversation body messages, not runtime prompt segments
5. conversation list/title/last-message summaries must ignore runtime prompt envelope content
6. audit should expose a single envelope snapshot as the authoritative record of request-time injected context

## Risks and Mitigations

### Risk: Runtime system sources remain indistinguishable after rendering

Mitigation: keep structured source metadata in runtime prompt segments and in audit snapshots. Do not treat role alone as identity.

### Risk: Old helper paths continue inserting prompt content separately

Mitigation: move final phase insertion responsibility into the runtime envelope renderer and retire overlapping ad hoc request-assembly helpers once the new path is in place.

### Risk: Compression accidentally consumes runtime prompt content

Mitigation: keep compression input limited to conversation body messages only, and add explicit tests proving forced/resolved prompt content never appears in compression requests.

### Risk: Conversation summaries become polluted by hidden runtime system text

Mitigation: preserve the separation between runtime prompt envelope content and visible conversation persistence, and add regression tests covering title/last-message computation.

### Risk: Audit shows two conflicting truths

Mitigation: make the runtime envelope snapshot the authoritative audit representation of request-time prompt assembly. Existing resolved-prompt audit structures, if retained, should either become subordinate fields or be clearly deprecated.

## Testing Plan

### Forced provider tests

- verify the three V1 forced blocks are generated in stable order
- verify `current_date` is rendered from injected `time.Time` input rather than ambient wall-clock state
- verify anti-injection and platform-constraints blocks are non-empty and stable

### Runtime envelope builder tests

- conversation-start build: forced blocks appear before resolved `session` segments and body
- memory-summary build: forced blocks appear before memory summary, which appears before resolved `session` segments
- tool-result build: tool-result phase insertion occurs before trailing tool messages
- time-variation build: rebuilding with a different `Now` updates only the date block

### Runner/executor integration tests

- provider-facing request messages contain forced blocks at the top
- ordinary conversation persistence does not contain forced block text
- audit/run metadata includes the envelope snapshot and source counts

### Compression regression tests

- after short-term compression, the rebuilt request header contains fresh forced blocks and fresh date content
- compressor input does not contain forced-block text
- compressor input does not contain resolved session prompt text
- rebuilt request places memory summary after forced blocks and before resolved session prompts

### Conversation-summary regression tests

- conversation title, last-message, and summary calculations ignore runtime prompt envelope content

## Recommended Implementation Scope

Files likely involved:

- New:
  - `core/runtimeprompt/types.go`
  - `core/runtimeprompt/builder.go`
  - `core/runtimeprompt/renderer.go`
  - `core/runtimeprompt/builder_test.go`
  - `core/forcedprompt/provider.go`
  - `core/forcedprompt/provider_test.go`
- Modify:
  - `core/memory/manager.go`
  - `core/agent/memory.go`
  - `core/agent/runner`-related request assembly files
  - `core/agent/executor`-related audit wiring files
  - `core/prompt` conversion path from resolved prompt outputs into runtime segments
  - existing tests for runner, executor, stream, and conversation summary behavior

## Recommended Implementation Sequence

1. define runtime prompt segment and envelope types
2. implement forced-block provider with tests
3. implement envelope builder/renderer with ordering tests
4. adapt resolved prompt output into runtime segments
5. move memory summary participation into the envelope path
6. switch runner/executor request assembly to the envelope
7. add audit snapshot wiring for the envelope
8. remove or retire overlapping legacy request-assembly helpers
9. run focused regression tests for compression, tool-result insertion, resume, and conversation summaries

## Success Criteria

The change is complete when:

1. forced system blocks are assembled through the runtime envelope, not prompt management
2. `current_date`, `anti_prompt_injection`, and `platform_constraints` appear in provider-facing requests in the intended order
3. these forced blocks do not appear in ordinary conversation persistence
4. forced/resolved prompt text does not enter short-term compression input
5. after compression, the next rebuilt request reintroduces forced blocks at the top and regenerates `current_date` from current time
6. tool-result prompt insertion still works correctly under the new renderer
7. audit/run metadata exposes a single structured runtime envelope snapshot for each request build
