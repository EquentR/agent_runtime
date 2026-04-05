# Configurable LLM Request Timeout Design

## Summary

Add a single global, configurable LLM request timeout for provider clients, with a default of **10 minutes**. Apply this timeout uniformly to both the `openai_completions` and `openai_responses` client paths so long-running model outputs are not cut off by short hardcoded request timeouts.

## Problem

The runtime currently has inconsistent request-timeout behavior across provider clients:

- `openai_responses` is constructed with a hardcoded `30*time.Second` request timeout.
- `openai_completions` does not share the same centralized timeout configuration path.
- Long-running streamed outputs can therefore fail prematurely instead of being governed by a clear application-level timeout policy.

The desired behavior is:

- timeout is configurable from app config
- default timeout is 10 minutes when not configured
- both completions and responses use the same timeout source
- this change only affects provider request timeout, not unrelated runtime timeouts

## Goals

1. Replace hardcoded provider request timeout values with app configuration.
2. Provide a default timeout of `10m` when no explicit value is configured.
3. Use the same timeout value for `openai_completions` and `openai_responses`.
4. Keep the change narrowly scoped to provider client request-timeout behavior.

## Non-Goals

- No per-provider timeout overrides in this change.
- No per-model timeout overrides in this change.
- No changes to task-manager lease/claim timeouts.
- No changes to agent-run overall lifecycle limits.
- No changes to SSE keepalive behavior or frontend subscription logic.

## Proposed Approach

### 1. Add a global LLM request timeout to app configuration

Introduce a single timeout field in application configuration, represented as `time.Duration`, with YAML support through the existing config loading path.

Semantics:

- the value is the maximum duration allowed for a single provider request
- it applies equally to streaming and non-streaming requests created by provider clients
- if omitted or zero-valued, runtime falls back to `10m`

This keeps configuration simple and solves the current issue without expanding scope into a larger provider-specific config matrix.

### 2. Centralize the default at config boundary

The default value of `10m` should be established at the app configuration boundary so all downstream client factories receive a resolved timeout.

This avoids duplicated fallback logic in each provider client implementation and prevents future drift where one client has a different implicit default than another.

### 3. Thread the timeout through the LLM client factory

Update the startup wiring so `buildLLMClientFactory(...)` receives access to the resolved request-timeout value and passes it into provider client constructors.

This replaces the current hardcoded `30*time.Second` in the `openai_responses` branch and extends the same injection path to `openai_completions`.

### 4. Update provider constructors

#### `openai_responses`

Keep constructor-level timeout injection, but source the value from app config instead of a hardcoded literal.

#### `openai_completions`

Extend the constructor to accept a timeout parameter and configure the underlying SDK/client transport to use that timeout for outbound requests.

After this change, both implementations derive request timeout from the same resolved config value.

## Data Flow

1. YAML config is loaded into app config.
2. App config resolves `LLM request timeout`, defaulting to `10m` when unspecified.
3. `Serve(...)` passes the resolved timeout into the LLM client factory.
4. The factory injects the timeout into both OpenAI client constructors.
5. Provider clients use that timeout for outbound model requests.

## Error Handling

- If timeout is not configured, the runtime uses the default `10m` value.
- If a request still exceeds the configured timeout, provider clients may return timeout-related transport errors; these should continue to propagate through the existing stream/error path unchanged.
- No special retry behavior is added in this change.

## Testing Plan

### Config tests

- verify default timeout resolves to `10m` when unset
- verify explicit configured timeout is preserved

### Wiring tests

- verify the client factory passes the configured timeout into `openai_responses`
- verify the client factory passes the configured timeout into `openai_completions`

### Client tests

- verify `openai_responses` continues honoring injected timeout
- verify `openai_completions` accepts and stores/applies injected timeout in the underlying client configuration path

## Success Criteria

The change is complete when:

1. There is no hardcoded `30*time.Second` provider request timeout in startup wiring.
2. Both `openai_completions` and `openai_responses` derive request timeout from the same app configuration field.
3. The default request timeout is `10m`.
4. Existing streaming, SSE, tool-call, and task execution behavior remain unchanged apart from the longer configurable provider timeout window.

## Risks and Mitigations

### Risk: Timeout default is applied inconsistently

Mitigation: resolve the default once at the config boundary and pass the resolved value downward.

### Risk: Change accidentally alters unrelated timeout behavior

Mitigation: keep scope limited to provider client construction and avoid touching task-manager, SSE, or agent-run lifecycle logic.

## Recommended Implementation Scope

Files likely involved:

- `app/config/app.go`
- `app/commands/serve.go`
- `core/providers/client/openai_responses/client.go`
- `core/providers/client/openai_completions/client.go`
- related tests in the same packages

## Out of Scope Follow-ups

Potential future improvements, intentionally excluded from this change:

- provider-specific timeout overrides
- separate connect/header/stream idle timeouts
- distinct timeouts for streaming vs non-streaming requests
- end-to-end tests that simulate very long provider streams
