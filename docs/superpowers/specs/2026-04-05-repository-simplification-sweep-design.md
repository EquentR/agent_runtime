# Repository Simplification Sweep Design

## Goal

Execute a repository-wide cleanup as three independent implementation tracks so we can remove high-value duplication, simplify state and data-flow boundaries, and trim obvious performance waste without changing product scope, API shape, or repository layering.

## Why this is split into tracks

The requested cleanup spans both backend and frontend, with a mix of pure deduplication work, state-flow simplification, and hot-path optimization. Treating it as one undifferentiated rewrite would make verification weak and rollback difficult. Splitting by subsystem keeps each batch understandable and testable on its own.

The approved implementation order is:
1. Backend cleanup
2. Frontend chat-path cleanup
3. Frontend admin cleanup

## Non-goals

- No new product features
- No database schema changes
- No new state-management library
- No route redesign or API resource redesign
- No large-scale package or directory reorganization
- No visual restyling beyond minimal template cleanup required for maintainability

## Constraints

- Preserve the repository layering: `app -> core -> pkg`
- Reuse existing runtime and frontend boundaries instead of inventing parallel abstractions
- Prefer small helper extraction and local replacement over broad architectural rewrites
- Keep behavior stable; simplification is only valid if existing flows still work

## Track 1: Backend cleanup

### Objective

Remove duplicated backend policy and persistence helpers, and simplify the most obvious repeated work in conversation and approval flows.

### Included work

1. Consolidate duplicated task access logic currently repeated across approval and interaction handlers.
2. Remove redundant `ensureApprovalInteraction` calls in approval creation flow.
3. Consolidate duplicated JSON marshaling / normalization helpers used by core stores where the logic is materially the same.
4. Simplify conversation list enrichment to prefer already maintained summary fields and reduce avoidable per-conversation extra work.

### Explicit boundaries

- Keep existing handlers and route registration structure intact.
- Do not introduce new API resources or change response contracts unless the current contract is already internally inconsistent and can be normalized without external behavior change.
- Do not move business logic upward from `core` into `app`.

### Expected result

The backend should have fewer policy copies, less repeated approval-side bookkeeping, and a slimmer conversation listing path, while preserving existing tests and endpoint behavior.

## Track 2: Frontend chat-path cleanup

### Objective

Reduce duplicated normalization and task-stream handling, simplify transcript-related state ownership, and trim obvious expensive work in the chat path.

### Included work

1. Unify message normalization responsibilities across `webapp/src/lib/api.ts` and `webapp/src/lib/transcript.ts` so REST and SSE payloads converge to one client shape.
2. Extract shared task / approval / stream helpers currently duplicated between `ChatView.vue` and `ApprovalView.vue`.
3. Centralize task status, interaction status, and suspend-reason literals into shared frontend constants or typed helpers.
4. Simplify repeated question-entry parsing in `MessageList.vue` by normalizing question data once.
5. Reduce duplicated transcript-state ownership in `ChatView.vue` where feasible without introducing a new state library.
6. Trim obvious hot-path waste such as overly broad persistence or avoidable startup / stream update churn.

### Explicit boundaries

- Keep Vue Router structure unchanged.
- Do not replace existing local state with Pinia, Vuex, or another state solution.
- Do not redesign the chat UI; structural simplification must preserve the current interaction model.

### Expected result

Frontend chat code should have clearer data boundaries: one normalization path, fewer raw status strings in views, less duplicated task-stream logic, and less unnecessary work during transcript-heavy interaction.

## Track 3: Frontend admin cleanup

### Objective

Consolidate repeated helper logic across admin screens and apply low-risk request-flow simplifications that improve maintainability and responsiveness.

### Included work

1. Extract shared provider/model fallback selection logic used by chat/admin prompt configuration flows where the behavior is genuinely the same.
2. Consolidate tiny repeated admin helpers such as time formatting.
3. Flatten obvious request waterfalls in admin audit views where requests are independent.
4. Apply low-risk template cleanup where current markup is unnecessarily fragile.

### Explicit boundaries

- Do not redesign admin workflows.
- Do not introduce generic abstraction layers unless at least two call sites clearly benefit immediately.
- Do not perform speculative component splitting solely for style reasons.

### Expected result

Admin code should retain the current UI and permissions model while using fewer repeated helpers and less avoidable sequential loading.

## Implementation style

Each track should follow the same discipline:

1. Remove exact duplication first.
2. Introduce the smallest shared helper that serves current duplication.
3. Replace call sites locally.
4. Run focused verification before broad verification.
5. Only then apply contained efficiency improvements.

This keeps cleanup changes explainable and prevents "simplification" from becoming an uncontrolled refactor.

## Verification approach

### Backend track

- Run focused Go tests for touched packages first.
- Then run `go test ./...`.
- If conversation handlers or approval flows change materially, also run `go build ./cmd/...`.

### Frontend chat track

- Run targeted Vitest coverage for touched lib/view/component modules where tests exist.
- Run `pnpm --dir webapp exec vue-tsc -b`.
- Run additional chat-related tests for transcript or API normalization when modified.

### Frontend admin track

- Run targeted frontend tests where coverage exists.
- Run `pnpm --dir webapp exec vue-tsc -b`.
- Re-run any admin-screen tests affected by shared helper extraction.

### Final verification

After all three tracks are complete:
- `go test ./...`
- `go build ./cmd/...`
- `pnpm --dir webapp exec vue-tsc -b`
- `pnpm --dir webapp test`

## Risks and controls

### Risk: cleanup changes accidentally alter behavior
Control: keep public contracts stable, make narrow edits, and verify each track independently.

### Risk: helper extraction creates vague abstractions
Control: only extract helpers for active duplication with immediate call sites.

### Risk: performance cleanup mixes with behavior cleanup
Control: do duplication/state-boundary cleanup first, then do targeted efficiency simplification after behavior is already centralized.

## Recommended implementation order inside each track

1. Pure duplication removal
2. Shared helper extraction
3. Local state/data boundary simplification
4. Performance cleanup
5. Full verification

## Plan handoff

The implementation plan should preserve the three-track structure rather than flatten everything into one long task list. Each track should produce a coherent, testable batch of work.
