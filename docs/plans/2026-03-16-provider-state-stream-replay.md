# Provider State Stream Replay Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add provider-specific replay state and structured stream finalization so all `core/providers/client` implementations can replay assistant messages losslessly and convert stream results into reusable messages for the next turn.

**Architecture:** Extend the shared provider model with `ProviderState`, a first-class final `Message`, and structured stream events. Then update each provider client to build and preserve provider-native replay payloads, expose final messages from streams, and prefer provider-state fast paths during request reconstruction while keeping the existing text-only `Recv()` path compatible.

**Tech Stack:** Go, `google.golang.org/genai`, `github.com/openai/openai-go`, `github.com/sashabaranov/go-openai`, Go test

---

### Task 1: Extend shared provider model types

**Files:**
- Modify: `core/providers/types/types.go`
- Modify: `core/providers/types/stream.go`
- Create: `core/providers/types/message_state_test.go`
- Test: `core/providers/types/message_state_test.go`

**Step 1: Write the failing tests**

```go
package model

import (
	"encoding/json"
	"testing"
)

func TestMessageClonePreservesProviderState(t *testing.T) {
	raw := json.RawMessage(`{"role":"assistant","content":"hello"}`)
	msg := Message{
		Role:    RoleAssistant,
		Content: "hello",
		ProviderState: &ProviderState{
			Provider: "openai_completions",
			Format:   "openai_chat_message.v1",
			Version:  "v1",
			Payload:  raw,
		},
	}

	cloned := cloneMessage(msg)
	cloned.ProviderState.Payload[0] = 'x'

	if string(msg.ProviderState.Payload) != string(raw) {
		t.Fatalf("provider payload mutated = %s", string(msg.ProviderState.Payload))
	}
}

func TestChatResponseMessageProjection(t *testing.T) {
	resp := ChatResponse{Message: Message{Role: RoleAssistant, Content: "hi", Reasoning: "plan"}}
	resp.SyncFieldsFromMessage()

	if resp.Content != "hi" || resp.Reasoning != "plan" {
		t.Fatalf("response projection = %#v", resp)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./core/providers/types -run "TestMessageClonePreservesProviderState|TestChatResponseMessageProjection"`
Expected: FAIL because `ProviderState`, `Message`, or sync helpers do not exist yet.

**Step 3: Write minimal implementation**

Add shared types and helpers in `core/providers/types/types.go` and `core/providers/types/stream.go`.

```go
type ProviderState struct {
	Provider string
	Format   string
	Version  string
	Payload  json.RawMessage
}

type Message struct {
	Role          string
	Content       string
	Reasoning     string
	ReasoningItems []ReasoningItem
	Attachments   []Attachment
	ToolCalls     []types.ToolCall
	ToolCallId    string
	ProviderState *ProviderState
}

type ChatResponse struct {
	Message        Message
	Content        string
	Reasoning      string
	ReasoningItems []ReasoningItem
	ToolCalls      []types.ToolCall
	Usage          TokenUsage
	Latency        time.Duration
}

func (r *ChatResponse) SyncFieldsFromMessage() {
	r.Content = r.Message.Content
	r.Reasoning = r.Message.Reasoning
	r.ReasoningItems = cloneReasoningItems(r.Message.ReasoningItems)
	r.ToolCalls = cloneToolCalls(r.Message.ToolCalls)
}
```

Also add stream-level shared result/event types in `core/providers/types/stream.go`.

```go
type StreamEventType string

const (
	StreamEventTextDelta      StreamEventType = "text_delta"
	StreamEventReasoningDelta StreamEventType = "reasoning_delta"
	StreamEventToolCallDelta  StreamEventType = "tool_call_delta"
	StreamEventUsage          StreamEventType = "usage"
	StreamEventCompleted      StreamEventType = "completed"
)

type StreamEvent struct {
	Type     StreamEventType
	Text     string
	Reasoning string
	ToolCall *types.ToolCall
	Usage    *TokenUsage
	Message  *Message
}

type StreamResult struct {
	Message Message
	Stats   StreamStats
}
```

Update cloning helpers so `ProviderState.Payload` is deep-copied anywhere messages are cloned.

**Step 4: Run tests to verify they pass**

Run: `go test ./core/providers/types`
Expected: PASS

**Step 5: Commit**

```bash
git add core/providers/types/types.go core/providers/types/stream.go core/providers/types/message_state_test.go
git commit -m "feat: add shared provider replay state model"
```

### Task 2: Extend the stream interface with events and final message access

**Files:**
- Modify: `core/providers/types/stream.go`
- Modify: `core/providers/client/openai_completions/stream.go`
- Modify: `core/providers/client/openai_responses/client.go`
- Modify: `core/providers/client/google/stream.go`
- Create: `core/providers/types/stream_result_test.go`
- Test: `core/providers/types/stream_result_test.go`

**Step 1: Write the failing test**

```go
package model

import "testing"

type fakeStream struct {
	final Message
	err   error
}

func (f *fakeStream) FinalMessage() (Message, error) { return f.final, f.err }

func TestStreamFinalMessageContract(t *testing.T) {
	msg, err := (&fakeStream{final: Message{Role: RoleAssistant, Content: "done"}}).FinalMessage()
	if err != nil || msg.Content != "done" {
		t.Fatalf("FinalMessage() = %#v, %v", msg, err)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./core/providers/types -run TestStreamFinalMessageContract`
Expected: FAIL because the current `Stream` interface has no `FinalMessage()` contract.

**Step 3: Write minimal implementation**

Extend `Stream` in `core/providers/types/stream.go`.

```go
type Stream interface {
	Recv() (content string, err error)
	RecvEvent() (StreamEvent, error)
	FinalMessage() (Message, error)
	Close() error
	Context() context.Context
	Stats() *StreamStats
	ToolCalls() []types.ToolCall
	ResponseType() StreamResponseType
	FinishReason() string
	Reasoning() string
}
```

Implementation rules for every concrete stream:

- Maintain one internal event channel.
- `Recv()` filters that channel and returns only `text_delta` text.
- `FinalMessage()` succeeds only after the stream completes normally.
- If the stream is canceled or closed early, `FinalMessage()` returns an error and no partial replayable message.

**Step 4: Run tests to verify they pass**

Run: `go test ./core/providers/types`
Expected: PASS

**Step 5: Commit**

```bash
git add core/providers/types/stream.go core/providers/types/stream_result_test.go core/providers/client/openai_completions/stream.go core/providers/client/openai_responses/client.go core/providers/client/google/stream.go
git commit -m "feat: add structured stream finalization contract"
```

### Task 3: Add provider-state helpers and request replay fast path for OpenAI Chat Completions

**Files:**
- Modify: `core/providers/client/openai_completions/message_builder.go`
- Modify: `core/providers/client/openai_completions/utils.go`
- Modify: `core/providers/client/openai_completions/stream.go`
- Modify: `core/providers/client/openai_completions/client.go`
- Create: `core/providers/client/openai_completions/provider_state.go`
- Modify: `core/providers/client/openai_completions/message_builder_test.go`
- Modify: `core/providers/client/openai_completions/client_reasoning_test.go`
- Test: `core/providers/client/openai_completions/message_builder_test.go`
- Test: `core/providers/client/openai_completions/client_reasoning_test.go`
- Test: `core/providers/client/openai_completions/stream_tool_test.go`

**Step 1: Write the failing tests**

Add one test for request replay fast path and one for final stream message.

```go
func TestBuildOpenAIMessages_UsesProviderStateReplay(t *testing.T) {
	msg := model.Message{
		Role: model.RoleAssistant,
		ProviderState: &model.ProviderState{
			Provider: "openai_completions",
			Format:   "openai_chat_message.v1",
			Version:  "v1",
			Payload:  json.RawMessage(`{"role":"assistant","content":"raw text","reasoning_content":"raw reasoning"}`),
		},
		Content:   "normalized text",
		Reasoning: "normalized reasoning",
	}

	msgs, _, err := buildOpenAIMessages([]model.Message{msg})
	if err != nil {
		t.Fatalf("buildOpenAIMessages() error = %v", err)
	}
	if msgs[0].Content != "raw text" || msgs[0].ReasoningContent != "raw reasoning" {
		t.Fatalf("replayed message = %#v", msgs[0])
	}
}

func TestClientChat_UsesFinalMessageProjection(t *testing.T) {
	// stream completion should yield response.Message with provider state and mirrored fields.
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./core/providers/client/openai_completions -run "TestBuildOpenAIMessages_UsesProviderStateReplay|TestClientChat_UsesFinalMessageProjection|TestStreamToolCallAccumulator_Append"`
Expected: FAIL because replay helper and final message support do not exist yet.

**Step 3: Write minimal implementation**

Create helper code in `core/providers/client/openai_completions/provider_state.go`.

```go
const (
	providerName = "openai_completions"
	messageFormat = "openai_chat_message.v1"
)

func messageFromProviderState(state *model.ProviderState) (openai.ChatCompletionMessage, bool, error)
func providerStateFromMessage(msg openai.ChatCompletionMessage) (*model.ProviderState, error)
func finalAssistantMessage(content string, reasoning string, toolCalls []types.ToolCall, state *model.ProviderState) model.Message
```

Implementation details:

- In `buildOpenAIMessages`, use provider-state replay when `ProviderState.Provider == "openai_completions"` and `Format == "openai_chat_message.v1"`.
- In sync extraction and stream finalization, create a `model.Message` with mirrored `Content`, `Reasoning`, `ToolCalls`, and `ProviderState` payload built from the OpenAI assistant message JSON.
- Update the stream implementation to emit `reasoning_delta`, `tool_call_delta`, and `text_delta` events while preserving current `Recv()` behavior.
- Change `Client.Chat()` to consume the stream to completion, then call `FinalMessage()` and `SyncFieldsFromMessage()` instead of rebuilding text manually.

**Step 4: Run tests to verify they pass**

Run: `go test ./core/providers/client/openai_completions`
Expected: PASS

**Step 5: Commit**

```bash
git add core/providers/client/openai_completions/message_builder.go core/providers/client/openai_completions/utils.go core/providers/client/openai_completions/stream.go core/providers/client/openai_completions/client.go core/providers/client/openai_completions/provider_state.go core/providers/client/openai_completions/message_builder_test.go core/providers/client/openai_completions/client_reasoning_test.go core/providers/client/openai_completions/stream_tool_test.go
git commit -m "feat: preserve replay state for openai chat completions"
```

### Task 4: Add provider-state helpers and stream result assembly for OpenAI Responses

**Files:**
- Modify: `core/providers/client/openai_responses/utils.go`
- Modify: `core/providers/client/openai_responses/stream.go`
- Modify: `core/providers/client/openai_responses/client.go`
- Create: `core/providers/client/openai_responses/provider_state.go`
- Modify: `core/providers/client/openai_responses/utils_test.go`
- Modify: `core/providers/client/openai_responses/stream_test.go`
- Test: `core/providers/client/openai_responses/utils_test.go`
- Test: `core/providers/client/openai_responses/stream_test.go`

**Step 1: Write the failing tests**

Add tests that assert complete output-item replay and final message assembly.

```go
func TestBuildResponseRequestParams_ReplaysProviderStateOutputItems(t *testing.T) {
	msg := model.Message{
		Role: model.RoleAssistant,
		ProviderState: &model.ProviderState{
			Provider: "openai_responses",
			Format:   "openai_response_output_items.v1",
			Version:  "v1",
			Payload:  json.RawMessage(`[ {"type":"reasoning","id":"rs_1"}, {"type":"function_call","call_id":"call_1","name":"lookup_weather","arguments":"{}"} ]`),
		},
	}
	params, err := buildResponseRequestParams(model.ChatRequest{Model: "gpt-5.4", Messages: []model.Message{msg}})
	if err != nil {
		t.Fatalf("buildResponseRequestParams() error = %v", err)
	}
	_ = params
}

func TestExtractChatResponse_PopulatesFinalMessageProviderState(t *testing.T) {
	// expect response.Message.ProviderState payload to contain output item sequence.
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./core/providers/client/openai_responses -run "TestBuildResponseRequestParams_ReplaysProviderStateOutputItems|TestExtractChatResponse_PopulatesFinalMessageProviderState|TestApplyStreamEvent_DeltaToolAndCompletion"`
Expected: FAIL because full output-item replay payload and final message state are not implemented.

**Step 3: Write minimal implementation**

Create provider-state helpers in `core/providers/client/openai_responses/provider_state.go`.

```go
const (
	providerName = "openai_responses"
	outputItemsFormat = "openai_response_output_items.v1"
)

func outputItemsFromProviderState(state *model.ProviderState) ([]responses.ResponseInputItemUnionParam, bool, error)
func providerStateFromOutputItems(items []responses.ResponseOutputItemUnion) (*model.ProviderState, error)
func finalAssistantMessageFromResponse(content string, reasoning string, reasoningItems []model.ReasoningItem, toolCalls []types.ToolCall, state *model.ProviderState) model.Message
```

Implementation details:

- In `buildResponseInput`, when an assistant message carries matching provider state, decode and append those stored output items directly instead of reconstructing from `Content` and `ReasoningItems`.
- In `extractChatResponse`, build `ChatResponse.Message` from the full output sequence and sync convenience fields from that message.
- In stream handling, accumulate output items, reasoning text, tool calls, and text deltas into one final assistant message with provider-state payload equal to the final output-item list.
- Emit `tool_call_delta` and `reasoning_delta` events as the stream progresses.

**Step 4: Run tests to verify they pass**

Run: `go test ./core/providers/client/openai_responses`
Expected: PASS

**Step 5: Commit**

```bash
git add core/providers/client/openai_responses/utils.go core/providers/client/openai_responses/stream.go core/providers/client/openai_responses/client.go core/providers/client/openai_responses/provider_state.go core/providers/client/openai_responses/utils_test.go core/providers/client/openai_responses/stream_test.go
git commit -m "feat: preserve output item replay for openai responses"
```

### Task 5: Add provider-state helpers and part replay for Google GenAI

**Files:**
- Modify: `core/providers/client/google/utils.go`
- Modify: `core/providers/client/google/stream.go`
- Modify: `core/providers/client/google/client.go`
- Create: `core/providers/client/google/provider_state.go`
- Modify: `core/providers/client/google/utils_test.go`
- Modify: `core/providers/client/google/stream_test.go`
- Test: `core/providers/client/google/utils_test.go`
- Test: `core/providers/client/google/stream_test.go`

**Step 1: Write the failing tests**

```go
func TestBuildGenAIMessages_ReplaysProviderStateParts(t *testing.T) {
	msg := model.Message{
		Role: model.RoleAssistant,
		ProviderState: &model.ProviderState{
			Provider: "google_genai",
			Format:   "google_genai_content.v1",
			Version:  "v1",
			Payload:  json.RawMessage(`{"role":"model","parts":[{"text":"hello"},{"text":"plan","thought":true}]}`),
		},
	}
	contents, _, _, err := buildGenAIMessages([]model.Message{msg})
	if err != nil {
		t.Fatalf("buildGenAIMessages() error = %v", err)
	}
	if len(contents) != 1 || contents[0].Role != genai.RoleModel {
		t.Fatalf("contents = %#v", contents)
	}
}

func TestExtractChatResponse_PopulatesProviderState(t *testing.T) {
	// expect final message provider state to contain thought/text/functionCall parts.
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./core/providers/client/google -run "TestBuildGenAIMessages_ReplaysProviderStateParts|TestExtractChatResponse_PopulatesProviderState|TestGenAIStreamRecv_ReturnsStreamErrorWhenChannelClosed"`
Expected: FAIL because provider-state replay and final message assembly are missing.

**Step 3: Write minimal implementation**

Create `core/providers/client/google/provider_state.go`.

```go
const (
	providerName = "google_genai"
	contentFormat = "google_genai_content.v1"
)

func contentFromProviderState(state *model.ProviderState) (*genai.Content, bool, error)
func providerStateFromContent(content *genai.Content) (*model.ProviderState, error)
func finalAssistantMessageFromContent(content string, reasoning string, toolCalls []types.ToolCall, state *model.ProviderState) model.Message
```

Implementation details:

- In `buildGenAIMessages`, use stored provider-state content when available and matching.
- Preserve full part sequences for assistant messages, including `thought`, `thoughtSignature`, and `functionCall` details.
- Update stream assembly so the final message is based on the provider-native content payload, not only flattened `Content` and `Reasoning` fields.
- Emit structured stream events while keeping current plain-text `Recv()` output unchanged.

**Step 4: Run tests to verify they pass**

Run: `go test ./core/providers/client/google`
Expected: PASS

**Step 5: Commit**

```bash
git add core/providers/client/google/utils.go core/providers/client/google/stream.go core/providers/client/google/client.go core/providers/client/google/provider_state.go core/providers/client/google/utils_test.go core/providers/client/google/stream_test.go
git commit -m "feat: preserve replay state for google genai messages"
```

### Task 6: Wire cross-provider fallback, memory cloning, and final regression coverage

**Files:**
- Modify: `core/memory/manager.go`
- Modify: `core/memory/manager_test.go`
- Modify: `core/providers/client/openai_completions/client.go`
- Modify: `core/providers/client/openai_responses/client.go`
- Modify: `core/providers/client/google/client.go`
- Create: `core/providers/client/provider_replay_integration_test.go`
- Test: `core/memory/manager_test.go`
- Test: `core/providers/client/provider_replay_integration_test.go`

**Step 1: Write the failing tests**

```go
func TestCloneMessagePreservesProviderStateDeepCopy(t *testing.T) {
	// extend existing memory clone coverage with ProviderState payload deep copy.
}

func TestCrossProviderFallsBackToNormalizedMessage(t *testing.T) {
	// assistant message created by one provider should still be rebuildable by another provider using public fields.
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./core/memory ./core/providers/client/...`
Expected: FAIL because replay-state cloning and fallback integration coverage are missing.

**Step 3: Write minimal implementation**

Implementation checklist:

- Update `core/memory/manager.go` cloning logic so `ProviderState.Payload` is deep-copied with the rest of the message.
- Update any provider `Chat()` implementation still manually assembling `ChatResponse` fields from `Recv()` to rely on `FinalMessage()` and `SyncFieldsFromMessage()`.
- Add integration tests proving:
  - same-provider replay prefers provider state,
  - missing provider state falls back to normalized fields,
  - cross-provider replay ignores foreign provider state and still works.

**Step 4: Run tests to verify they pass**

Run: `go test ./core/memory ./core/providers/client/...`
Expected: PASS

**Step 5: Run full regression suite**

Run: `go test ./...`
Expected: PASS

**Step 6: Commit**

```bash
git add core/memory/manager.go core/memory/manager_test.go core/providers/client/provider_replay_integration_test.go core/providers/client/openai_completions/client.go core/providers/client/openai_responses/client.go core/providers/client/google/client.go
git commit -m "test: cover provider replay fallback paths"
```
