# Attachments Storage Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 为 chat 增加可上传、可展示、可随 `agent.run` 发送并可按会话生命周期治理的附件能力，同时避免把原始文件字节直接持久化到 conversation message JSON。

**Architecture:** 新增独立附件领域与 storage 抽象，上传先落 `draft` 附件，再通过 `attachment_ids` 绑定到 `agent.run`。executor 在 provider 调用前按引用加载原始 bytes，conversation history 只保存附件引用元数据；前端通过 Element Plus 拖拽上传和粘贴图片能力管理草稿附件，并在消息气泡内展示附件卡片。模型目录增加附件能力声明，避免向当前不支持附件的 provider 暴露错误入口。

**Tech Stack:** Go 1.25, Gin, GORM/SQLite, Vue 3 + TypeScript, Element Plus, Vitest, vue-tsc

---

## Background & Constraints

- 当前 `core/providers/types.Message` 已有 `Attachments []Attachment`，`openai_completions` 与 `google` client 已支持图片/文本附件；`openai_responses` 尚未支持用户附件输入。
- 当前 `core/agent/conversation_store.go` 会原样持久化 `model.Message`，不能继续把 `Attachment.Data []byte` 长期写入数据库。
- 当前 `app/router/init.go` 统一注册 handler，新增附件资源必须走该路径，不要在 `serve.go` 中零散挂路由。
- 当前后端没有现成 multipart 上传 handler 样例；上传接口需要补新的 handler 测试与实现。
- 当前前端已有 `MessageComposer.spec.ts`、`MessageList.spec.ts`、`ChatView.spec.ts` 可承接上传/展示测试。
- 当前仓库没有对象存储 SDK 依赖。首版以文件系统 driver 为可运行实现，对象存储接口与配置先设计好，必要时第二阶段再接 SDK。
- 不要创建 commit，除非用户显式要求。

---

### Task 1: Define attachment domain models, repository, and storage abstraction

**Files:**
- Create: `core/attachments/model.go`
- Create: `core/attachments/store.go`
- Create: `core/attachments/filesystem.go`
- Create: `core/attachments/types.go`
- Modify: `app/migration/define.go`
- Modify: `app/migration/init.go`
- Modify: `app/config/app.go`
- Modify: `conf/app.yaml`
- Test: `core/attachments/store_test.go`
- Test: `app/migration/task_migration_test.go`

**Step 1: Write the failing tests**

Add focused Go tests that define the first attachment persistence contract.

- In `core/attachments/store_test.go`, add tests like:
  - `TestAttachmentStoreCreateDraftPersistsMetadataAndStorageKey`
  - `TestAttachmentStorePromoteDraftToSentBindsConversationAndKeepsMetadata`
  - `TestFilesystemStoreOpenDeleteAndGCExpired`
- In `app/migration/task_migration_test.go`, add a migration test like `TestMigrationAddsConversationAttachmentsTable`.

Require the tests to verify:

- Draft attachments persist metadata without storing raw bytes in DB.
- Filesystem storage writes files to a configured root and can reopen/delete them.
- The new attachment table is created by migrations in a stable way.

**Step 2: Run the focused tests to confirm they fail**

Run:

```bash
go test ./core/attachments -run 'TestAttachmentStore|TestFilesystemStore' -v
go test ./app/migration -run '^TestMigrationAddsConversationAttachmentsTable$' -v
```

Expected: FAIL because `core/attachments` and the migration do not exist yet.

**Step 3: Implement the minimal attachment domain layer**

- Create `core/attachments/model.go` with a GORM model for `conversation_attachments`.
- Create `core/attachments/store.go` with repository methods for create/get/promote/delete/list-expired.
- Create `core/attachments/filesystem.go` implementing the first concrete storage driver.
- Create `core/attachments/types.go` with status/lifecycle constants and storage interfaces.
- Extend `app/config/app.go` and `conf/app.yaml` with attachment storage config:
  - storage backend
  - filesystem root
  - draft TTL
  - sent retention placeholder
- Register the new model in `app/migration/define.go` and append a new migration in `app/migration/init.go`.

Keep the first version narrow:

- Support `draft` and `sent` statuses.
- Support only filesystem storage implementation.
- Leave object storage as config/interface placeholders for follow-up work.

**Step 4: Re-run the focused tests**

Run:

```bash
go test ./core/attachments -run 'TestAttachmentStore|TestFilesystemStore' -v
go test ./app/migration -run '^TestMigrationAddsConversationAttachmentsTable$' -v
```

Expected: PASS.

**Step 5: Run package-level regression tests**

Run:

```bash
go test ./core/attachments ./app/migration
```

Expected: PASS.

**Step 6: Do not commit**

Do not create a commit in this session unless the user explicitly asks.

---

### Task 2: Add attachment HTTP APIs and router wiring

**Files:**
- Create: `app/handlers/attachment_handler.go`
- Create: `app/handlers/attachment_handler_test.go`
- Modify: `app/router/deps.go`
- Modify: `app/router/init.go`
- Modify: `app/commands/serve.go`
- Modify: `app/handlers/swagger_types.go`
- Modify: `docs/swagger/docs.go`
- Modify: `docs/swagger/swagger.json`
- Modify: `docs/swagger/swagger.yaml`

**Step 1: Write the failing tests**

Add handler tests for multipart upload and attachment read/delete behavior.

- Create `app/handlers/attachment_handler_test.go` with tests like:
  - `TestAttachmentHandlerUploadCreatesDraftAttachment`
  - `TestAttachmentHandlerRejectsUnsupportedMimeType`
  - `TestAttachmentHandlerGetContentRequiresOwnership`
  - `TestAttachmentHandlerDeleteDraftAttachment`

Require the tests to verify:

- `POST /attachments` accepts `multipart/form-data` and returns attachment metadata.
- Only the owner can fetch/delete an attachment.
- Unsupported/empty uploads fail with a clear error.

**Step 2: Run the focused tests to confirm they fail**

Run:

```bash
go test ./app/handlers -run '^TestAttachmentHandler' -v
```

Expected: FAIL because the new handler and routes do not exist.

**Step 3: Implement the minimal API surface**

- Create `app/handlers/attachment_handler.go` with:
  - `POST /attachments`
  - `GET /attachments/:id`
  - `GET /attachments/:id/content`
  - `DELETE /attachments/:id`
- Use existing auth/session middleware patterns from other handlers.
- Extend `router.Dependencies` and `router.Init(...)` to register the new handler centrally.
- Wire attachment services in `app/commands/serve.go` and pass them through router deps.
- Add Swagger DTOs in `app/handlers/swagger_types.go`.
- Regenerate or update `docs/swagger/*` so the generated docs expose the new routes and schemas.

Prefer explicit typed request/response structs over map-based payloads.

**Step 4: Re-run the focused tests**

Run:

```bash
go test ./app/handlers -run '^TestAttachmentHandler' -v
```

Expected: PASS.

**Step 5: Run package-level regression tests**

Run:

```bash
go test ./app/handlers
```

Expected: PASS.

**Step 6: Do not commit**

Do not create a commit in this session unless the user explicitly asks.

---

### Task 3: Extend model catalog with attachment capability flags

**Files:**
- Modify: `core/types/provider_config.go`
- Modify: `core/types/provider_config_test.go`
- Modify: `core/agent/executor.go`
- Modify: `app/handlers/swagger_types.go`
- Modify: `webapp/src/types/api.ts`
- Modify: `webapp/src/lib/api.ts`

**Step 1: Write the failing tests**

Add tests that lock model capability parsing and catalog exposure.

- In `core/types/provider_config_test.go`, add tests like:
  - `TestLLMProviderParsesAttachmentCapability`
  - `TestLLMProviderDefaultsAttachmentCapabilityToFalse`
- Add or extend API normalization tests in `webapp/src/lib/api.spec.ts` to assert model capability fields survive boundary normalization.

Require the tests to verify:

- YAML model config can declare whether attachments are supported.
- Omitted capability defaults to false.
- Frontend model catalog can decide whether to show uploader affordances.

**Step 2: Run the focused tests to confirm they fail**

Run:

```bash
go test ./core/types -run 'TestLLMProviderParsesAttachmentCapability|TestLLMProviderDefaultsAttachmentCapabilityToFalse' -v
pnpm --dir webapp exec vitest run src/lib/api.spec.ts -t "normalizes model attachment capability"
```

Expected: FAIL because capability fields are not present yet.

**Step 3: Implement the minimal capability contract**

- Extend `core/types.LLMModel` with an attachment capability field or nested capability struct.
- Keep the YAML schema backward compatible by defaulting to false.
- Extend model catalog serialization in `core/agent/executor.go` so the frontend receives the capability info.
- Extend `webapp/src/types/api.ts` and `webapp/src/lib/api.ts` normalization to keep the capability on model entries.

Set the current configured models so capability matches actual implementation reality:

- `openai_completions`: supported
- `google`: supported
- `openai_responses`: unsupported until a later task implements it

**Step 4: Re-run the focused tests**

Run:

```bash
go test ./core/types -run 'TestLLMProviderParsesAttachmentCapability|TestLLMProviderDefaultsAttachmentCapabilityToFalse' -v
pnpm --dir webapp exec vitest run src/lib/api.spec.ts -t "normalizes model attachment capability"
```

Expected: PASS.

**Step 5: Run package-level regression tests**

Run:

```bash
go test ./core/types
pnpm --dir webapp exec vitest run src/lib/api.spec.ts
```

Expected: PASS.

**Step 6: Do not commit**

Do not create a commit in this session unless the user explicitly asks.

---

### Task 4: Extend `agent.run` input and persist reference-only attachments in conversation history

**Files:**
- Modify: `core/agent/executor.go`
- Modify: `core/agent/conversation_store.go`
- Modify: `core/agent/conversation_store_test.go`
- Modify: `core/agent/executor_test.go`
- Modify: `app/handlers/task_handler.go`
- Modify: `app/handlers/task_handler_test.go`
- Modify: `app/handlers/conversation_handler_test.go`
- Modify: `webapp/src/types/api.ts`
- Modify: `webapp/src/lib/api.ts`

**Step 1: Write the failing tests**

Add backend tests that define the message persistence contract.

- In `core/agent/conversation_store_test.go`, add tests like:
  - `TestConversationStorePersistsAttachmentReferencesWithoutRawData`
  - `TestListReplayMessagesReturnsAttachmentReferences`
- In `core/agent/executor_test.go`, add tests like:
  - `TestAgentExecutorPromotesDraftAttachmentsAndHydratesProviderAttachments`
  - `TestAgentExecutorRejectsExpiredAttachmentBeforeModelCall`
- In `app/handlers/task_handler_test.go`, add a test like `TestCreateTaskAcceptsAttachmentIDsInAgentRunInput`.
- In `app/handlers/conversation_handler_test.go`, add a test like `TestGetConversationMessagesIncludesAttachmentMetadata`.

Require the tests to verify:

- `agent.run.input.attachment_ids` passes through task creation.
- Executor hydrates bytes before provider call.
- Conversation persistence stores reference metadata only, not raw bytes.
- Conversation read APIs return attachment metadata to the frontend.

**Step 2: Run the focused tests to confirm they fail**

Run:

```bash
go test ./core/agent -run 'TestConversationStorePersistsAttachmentReferencesWithoutRawData|TestListReplayMessagesReturnsAttachmentReferences|TestAgentExecutorPromotesDraftAttachmentsAndHydratesProviderAttachments|TestAgentExecutorRejectsExpiredAttachmentBeforeModelCall' -v
go test ./app/handlers -run 'TestCreateTaskAcceptsAttachmentIDsInAgentRunInput|TestGetConversationMessagesIncludesAttachmentMetadata' -v
```

Expected: FAIL because attachment ids are not part of `agent.run` and conversation messages do not expose attachment refs.

**Step 3: Implement the minimal runtime and persistence changes**

- Extend `RunTaskInput` with `AttachmentIDs []string`.
- Extend `TaskHandler` request validation/canonicalization to keep `attachment_ids`.
- Inject the attachment store into executor dependencies.
- In `core/agent/executor.go`:
  - validate ownership/status for attachment ids
  - promote drafts to sent when binding them to a user message
  - hydrate runtime `model.Attachment` bytes before provider call
- In `core/agent/conversation_store.go`:
  - introduce a persisted message shape that stores attachment refs without `Data []byte`
  - decode the persisted shape back into a replay-friendly message representation
- Ensure conversation message APIs surface attachment metadata.

Keep the code narrow: avoid reworking unrelated replay/provider-state logic.

**Step 4: Re-run the focused tests**

Run:

```bash
go test ./core/agent -run 'TestConversationStorePersistsAttachmentReferencesWithoutRawData|TestListReplayMessagesReturnsAttachmentReferences|TestAgentExecutorPromotesDraftAttachmentsAndHydratesProviderAttachments|TestAgentExecutorRejectsExpiredAttachmentBeforeModelCall' -v
go test ./app/handlers -run 'TestCreateTaskAcceptsAttachmentIDsInAgentRunInput|TestGetConversationMessagesIncludesAttachmentMetadata' -v
```

Expected: PASS.

**Step 5: Run package-level regression tests**

Run:

```bash
go test ./core/agent ./app/handlers
```

Expected: PASS.

**Step 6: Do not commit**

Do not create a commit in this session unless the user explicitly asks.

---

### Task 5: Support attachment content in `openai_responses` or explicitly guard it off

**Files:**
- Modify: `core/providers/client/openai_responses/utils.go`
- Modify: `core/providers/client/openai_responses/utils_test.go`
- Modify: `core/providers/client/openai_responses/client.go`
- Test: `core/providers/client/openai_responses/utils_test.go`

**Step 1: Write the failing tests**

Add tests for user messages with attachments on the responses client path.

- Add tests like:
  - `TestBuildResponseInputEncodesImageAttachmentForUserMessage`
  - `TestBuildResponseInputEncodesTextAttachmentForUserMessage`
  - `TestBuildResponseInputRejectsUnsupportedAttachmentType`

Require the tests to verify the request input shape matches the OpenAI Responses API encoding you choose.

**Step 2: Run the focused tests to confirm they fail**

Run:

```bash
go test ./core/providers/client/openai_responses -run 'TestBuildResponseInputEncodesImageAttachmentForUserMessage|TestBuildResponseInputEncodesTextAttachmentForUserMessage|TestBuildResponseInputRejectsUnsupportedAttachmentType' -v
```

Expected: FAIL because `buildResponseInput(...)` currently only forwards message text for user messages.

**Step 3: Implement the minimal responses attachment support**

- Extend `buildResponseInput(...)` so user messages with attachments produce the proper multi-part input items.
- Keep text-only behavior unchanged.
- If the SDK/API shape proves too risky or unclear, stop here and explicitly leave capability false for `openai_responses` rather than shipping silent no-op behavior.

This task is intentionally gated behind tests because silent attachment loss is worse than a visible unsupported capability.

**Step 4: Re-run the focused tests**

Run:

```bash
go test ./core/providers/client/openai_responses -run 'TestBuildResponseInputEncodesImageAttachmentForUserMessage|TestBuildResponseInputEncodesTextAttachmentForUserMessage|TestBuildResponseInputRejectsUnsupportedAttachmentType' -v
```

Expected: PASS if implementation proceeds; otherwise capability must remain false and the test scope should be replaced with an explicit guard test.

**Step 5: Run package-level regression tests**

Run:

```bash
go test ./core/providers/client/openai_responses
```

Expected: PASS.

**Step 6: Do not commit**

Do not create a commit in this session unless the user explicitly asks.

---

### Task 6: Add frontend upload APIs, drag-and-drop, and paste-image handling

**Files:**
- Modify: `webapp/src/types/api.ts`
- Modify: `webapp/src/lib/api.ts`
- Modify: `webapp/src/components/MessageComposer.vue`
- Modify: `webapp/src/components/MessageComposer.spec.ts`
- Modify: `webapp/src/views/ChatView.vue`
- Modify: `webapp/src/views/ChatView.spec.ts`

**Step 1: Write the failing tests**

Add frontend tests for draft attachment UX.

- In `webapp/src/components/MessageComposer.spec.ts`, add tests like:
  - `it('shows drag upload area when model supports attachments')`
  - `it('disables send while attachments are uploading')`
  - `it('emits remove for failed draft attachment')`
  - `it('uploads pasted image files from clipboard')`
- In `webapp/src/views/ChatView.spec.ts`, add tests like:
  - `it('sends attachment_ids with run task request')`
  - `it('hides uploader for models without attachment capability')`

Require the tests to verify:

- Uploader only appears for supported models.
- Drag/drop and paste image paths feed the same upload API.
- The run-task request includes `attachment_ids`.

**Step 2: Run the focused tests to confirm they fail**

Run:

```bash
pnpm --dir webapp exec vitest run src/components/MessageComposer.spec.ts -t "drag upload area"
pnpm --dir webapp exec vitest run src/views/ChatView.spec.ts -t "sends attachment_ids with run task request"
```

Expected: FAIL because the frontend has no attachment queue or upload API integration yet.

**Step 3: Implement the minimal draft attachment UX**

- Extend `webapp/src/types/api.ts` with attachment metadata/request types.
- Extend `webapp/src/lib/api.ts` with:
  - `uploadAttachment(...)`
  - `deleteAttachment(...)`
  - `fetchAttachmentContentURL(...)` or equivalent helper
  - `buildRunTaskRequest(...)` support for `attachment_ids`
- Update `MessageComposer.vue` to:
  - use Element Plus drag upload area
  - maintain a local draft attachment queue
  - handle paste events for image clipboard items
  - block send while uploads are pending
- Update `ChatView.vue` to own the attachment queue state and pass `attachment_ids` into task creation.

Keep the first version functional and minimal; do not overbuild with generalized upload managers.

**Step 4: Re-run the focused tests**

Run:

```bash
pnpm --dir webapp exec vitest run src/components/MessageComposer.spec.ts -t "drag upload area"
pnpm --dir webapp exec vitest run src/views/ChatView.spec.ts -t "sends attachment_ids with run task request"
```

Expected: PASS.

**Step 5: Run file-level regression tests**

Run:

```bash
pnpm --dir webapp exec vitest run src/components/MessageComposer.spec.ts src/views/ChatView.spec.ts src/lib/api.spec.ts
```

Expected: PASS.

**Step 6: Do not commit**

Do not create a commit in this session unless the user explicitly asks.

---

### Task 7: Render attachments inside message history and transcript recovery

**Files:**
- Modify: `webapp/src/components/MessageList.vue`
- Modify: `webapp/src/components/MessageList.spec.ts`
- Modify: `webapp/src/lib/transcript.ts`
- Modify: `webapp/src/lib/transcript.spec.ts`
- Modify: `webapp/src/lib/api.ts`

**Step 1: Write the failing tests**

Add frontend tests that lock historical attachment rendering.

- In `webapp/src/components/MessageList.spec.ts`, add tests like:
  - `it('renders image attachment thumbnails inside a user message')`
  - `it('renders file cards for non-image attachments')`
  - `it('shows expired attachment state')`
- In `webapp/src/lib/transcript.spec.ts`, add tests like:
  - `it('builds transcript entries from conversation messages with attachments')`
  - `it('keeps attachment metadata on reply and user entries')`

Require the tests to verify:

- Attachments stay attached to their message bubble.
- Transcript recovery from conversation history preserves attachment metadata.
- Expired attachments still render a status card.

**Step 2: Run the focused tests to confirm they fail**

Run:

```bash
pnpm --dir webapp exec vitest run src/components/MessageList.spec.ts -t "renders image attachment thumbnails"
pnpm --dir webapp exec vitest run src/lib/transcript.spec.ts -t "builds transcript entries from conversation messages with attachments"
```

Expected: FAIL because attachment metadata is not part of transcript/message rendering yet.

**Step 3: Implement the minimal history rendering path**

- Extend normalized conversation messages to include attachments.
- Update `webapp/src/lib/transcript.ts` so user/reply entries preserve attachment metadata.
- Update `MessageList.vue` to render attachment cards within the corresponding message block.
- Reuse backend content endpoints or generated URLs for image preview/download actions.

Keep attachments as metadata on the existing message entries; do not invent a second transcript kind unless the UI truly needs it.

**Step 4: Re-run the focused tests**

Run:

```bash
pnpm --dir webapp exec vitest run src/components/MessageList.spec.ts -t "renders image attachment thumbnails"
pnpm --dir webapp exec vitest run src/lib/transcript.spec.ts -t "builds transcript entries from conversation messages with attachments"
```

Expected: PASS.

**Step 5: Run file-level regression tests**

Run:

```bash
pnpm --dir webapp exec vitest run src/components/MessageList.spec.ts src/lib/transcript.spec.ts
```

Expected: PASS.

**Step 6: Do not commit**

Do not create a commit in this session unless the user explicitly asks.

---

### Task 8: Add cleanup jobs and replay-expiration behavior

**Files:**
- Modify: `core/attachments/store.go`
- Modify: `core/attachments/store_test.go`
- Modify: `core/agent/executor.go`
- Modify: `core/agent/executor_test.go`
- Modify: `app/commands/serve.go`
- Modify: `app/config/app.go`

**Step 1: Write the failing tests**

Add tests that define expiration and cleanup behavior.

- In `core/attachments/store_test.go`, add tests like:
  - `TestAttachmentStoreGCExpiresDraftAttachments`
  - `TestAttachmentStoreMarksSentAttachmentExpiredWithoutRemovingMetadata`
- In `core/agent/executor_test.go`, add tests like:
  - `TestAgentExecutorAllowsContinuationWhenExpiredAttachmentIsOutsideReplayTail`
  - `TestAgentExecutorFailsPreciseReplayWhenRequiredAttachmentExpired`

Require the tests to verify:

- Draft attachments are actually cleaned by TTL.
- Sent attachment metadata can remain readable after entity expiration.
- Replay behavior fails explicitly when exact reproduction is impossible.

**Step 2: Run the focused tests to confirm they fail**

Run:

```bash
go test ./core/attachments -run 'TestAttachmentStoreGCExpiresDraftAttachments|TestAttachmentStoreMarksSentAttachmentExpiredWithoutRemovingMetadata' -v
go test ./core/agent -run 'TestAgentExecutorAllowsContinuationWhenExpiredAttachmentIsOutsideReplayTail|TestAgentExecutorFailsPreciseReplayWhenRequiredAttachmentExpired' -v
```

Expected: FAIL because cleanup scheduling and replay-expiration behavior are not implemented yet.

**Step 3: Implement the minimal cleanup and expiration rules**

- Add repository helpers to list and expire eligible attachments.
- Wire a lightweight GC loop from `app/commands/serve.go` using configured intervals.
- In executor replay paths, distinguish between:
  - expired attachment outside replay tail: allow continuation
  - expired attachment required for exact replay: fail explicitly

Prefer explicit errors over silent downgrade.

**Step 4: Re-run the focused tests**

Run:

```bash
go test ./core/attachments -run 'TestAttachmentStoreGCExpiresDraftAttachments|TestAttachmentStoreMarksSentAttachmentExpiredWithoutRemovingMetadata' -v
go test ./core/agent -run 'TestAgentExecutorAllowsContinuationWhenExpiredAttachmentIsOutsideReplayTail|TestAgentExecutorFailsPreciseReplayWhenRequiredAttachmentExpired' -v
```

Expected: PASS.

**Step 5: Run package-level regression tests**

Run:

```bash
go test ./core/attachments ./core/agent
```

Expected: PASS.

**Step 6: Do not commit**

Do not create a commit in this session unless the user explicitly asks.

---

### Task 9: Run end-to-end verification across backend and frontend boundaries

**Files:**
- Modify only if needed based on failures from verification

**Step 1: Run focused Go attachment and agent tests**

Run:

```bash
go test ./core/attachments ./core/agent ./app/handlers ./app/migration ./core/types ./core/providers/client/openai_responses
```

Expected: PASS.

**Step 2: Run frontend attachment-related tests**

Run:

```bash
pnpm --dir webapp exec vitest run src/lib/api.spec.ts src/lib/transcript.spec.ts src/components/MessageComposer.spec.ts src/components/MessageList.spec.ts src/views/ChatView.spec.ts
```

Expected: PASS.

**Step 3: Run frontend type-check**

Run:

```bash
pnpm --dir webapp exec vue-tsc -b
```

Expected: PASS.

**Step 4: Run broad backend verification**

Run:

```bash
go test ./...
go build ./cmd/...
go list ./...
```

Expected: PASS.

**Step 5: Do not commit**

Do not create a commit in this session unless the user explicitly asks.
