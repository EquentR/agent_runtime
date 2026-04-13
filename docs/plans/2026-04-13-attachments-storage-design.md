# Attachments Storage And Chat Integration Design

**Goal:** 为 agent chat 增加附件上传、会话内展示、模型调用透传、历史重放与生命周期治理能力，同时保持现有 task/conversation/memory 架构不被破坏。

**Architecture:** 附件采用“两层模型”：会话消息中只持久化附件引用与展示元数据，实际文件内容放在独立 storage 层。上传阶段先进入 `draft` 生命周期，消息发送后转为 `sent` 并绑定到用户消息；executor 在 provider 调用前再按引用加载原始 bytes，memory 与 transcript 只消费附件的规范化上下文信息而不依赖数据库内联字节。默认策略是“会话持久化”，而不是“发送即焚”；未来可在此基础上扩展更严格的 ephemeral 模式。

**Tech Stack:** Go 1.25, Gin, GORM/SQLite, Vue 3 + TypeScript, Element Plus, Vitest, vue-tsc

---

## Background

- 当前 `core/providers/types.Message` 已有 `Attachments []Attachment` 字段，`openai_completions` 与 `google` client 已能把图片/文本附件编码进模型请求。
- 当前 `core/memory/manager.go` 已将附件纳入 token budget 和压缩输入，说明附件在语义上属于对话内容，而不是纯外围资源。
- 当前 `core/agent/conversation_store.go` 会原样持久化 `model.Message` 到 `conversation_messages.message_json`。如果继续直接使用 `Attachment.Data []byte` 做持久化，数据库会被文件字节流污染，且不适合大文件或对象存储场景。
- 当前 REST API、`RunTaskInput`、`webapp/src/types/api.ts`、`MessageComposer.vue` 与 `MessageList.vue` 还没有完整暴露附件能力。前端附件按钮仍是占位实现。
- 当前默认模型 `gpt-5.4` 走 `openai_responses` 路径，而该 client 目前尚未把用户附件编码进 request input。因此附件能力必须纳入模型能力声明，不能默认所有模型都可用。

---

## Product Decisions

### 1. 发送后附件默认采用会话持久化

- 用户上传但未发送：附件处于 `draft` 状态，保留短 TTL，自动清理。
- 用户随消息发送后：附件处于 `sent` 状态，作为会话资源保留，不在成功调用 LLM 后立即删除。
- 删除会话或超过 retention：附件实体进入过期/删除流程，但消息中的附件引用与元数据仍保留，用于历史展示与错误说明。

### 2. 后续消息重放需要重放附件，但仅限仍属于 replay tail 的消息

- 普通续聊：附件与普通用户消息一样参与 replay；只要该轮仍在 short-term tail，就需要按原附件再发给模型。
- memory 压缩后：被压缩进 summary 的旧附件不应每轮重复发送原文件；后续依赖摘要文本继续对话。
- 精确重放场景：如 retry 同一轮、audit replay、同 provider 无损续跑，只要附件实体仍可读取，就应该支持按引用重新 hydrate 并重放。
- 附件已过期时，不应静默降级成“只发文本”并假装成功看过附件。

### 3. 压缩记忆后原附件引用仍需保留在对话记录中

- memory summary 是派生视图，不是事实源。
- 压缩后不删除消息中的附件引用与元数据。
- 对于 memory manager，附件额外提供规范化的 `context_text`：
  - 文本附件：文件名 + 截断后的正文/摘要。
  - 图片附件：先使用占位文案，如 `[image attachment: screenshot.png]`；后续若需要再扩展 OCR/图像描述。

### 4. 前端展示应挂在消息气泡内部，而不是独立 transcript 资源流

- 用户消息/历史消息下方展示附件卡片列表。
- 图片：缩略图 + 打开原图。
- 文本/JSON/代码：文件卡片 + 预览入口。
- 其他二进制：文件卡片 + 下载/查看按钮。
- 过期或已删除：保留卡片占位，展示“附件已过期”或“附件已删除”。

### 5. Storage 是附件真实存储层，不只是一次性中转层

- conversation message：存附件引用与展示元数据。
- attachment store：存附件实体与生命周期状态。
- memory summary：存附件派生上下文文本。

### 6. 必须提供未发送附件的清理机制

- `draft` 附件必须设置短 TTL，例如 24 小时。
- 页面关闭、消息未发送、任务取消都不阻止后台 GC 清理草稿附件。
- 清理后前端引用失效时，应收到明确状态，而不是默默失败。

---

## Domain Model

### Attachment message reference

消息中不再持久化原始 `Attachment.Data`，而是持久化引用型结构。建议新增一个 API/domain 结构，例如：

```go
type AttachmentRef struct {
	ID            string  `json:"id"`
	FileName      string  `json:"file_name"`
	MimeType      string  `json:"mime_type"`
	SizeBytes     int64   `json:"size_bytes"`
	Kind          string  `json:"kind"`
	Status        string  `json:"status"`
	PreviewText   string  `json:"preview_text,omitempty"`
	Width         *int    `json:"width,omitempty"`
	Height        *int    `json:"height,omitempty"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
}
```

建议将 `model.Message` 的持久化形态从“直接写 `Attachments []Attachment`”切换为“消息 JSON 内保存 `AttachmentRefs`”，运行时再转换为 provider 需要的 `[]Attachment`。

### Attachment entity record

建议新增独立表，如 `conversation_attachments`：

- `id`
- `conversation_id`
- `message_seq` 或消息绑定键
- `created_by`
- `storage_backend`
- `storage_key`
- `sha256`
- `file_name`
- `mime_type`
- `size_bytes`
- `kind`
- `status` (`draft` / `sent` / `expired` / `deleted`)
- `lifecycle` (`draft` / `conversation_retained`)
- `preview_text`
- `context_text`
- `width`, `height`
- `expires_at`, `last_accessed_at`, `deleted_at`
- `created_at`, `updated_at`

消息列表和前端历史展示依赖上述实体投影后的引用元数据，而不是依赖存储后端本身。

---

## Storage Abstraction

建议新增 `core/attachments` 包，提供统一存储抽象，避免把文件系统/对象存储细节散到 handler、executor 或前端层。

建议接口：

```go
type Store interface {
	PutDraft(ctx context.Context, input PutDraftInput) (*StoredObject, error)
	PromoteToSent(ctx context.Context, attachmentID string, input PromoteInput) error
	Open(ctx context.Context, attachmentID string) (io.ReadCloser, ObjectMeta, error)
	Delete(ctx context.Context, attachmentID string) error
	Stat(ctx context.Context, attachmentID string) (ObjectMeta, error)
	GenerateAccessURL(ctx context.Context, attachmentID string) (string, error)
	GCExpired(ctx context.Context, now time.Time, limit int) (int, error)
}
```

建议落两个 driver：

- `filesystem`：默认本地开发/单机部署使用，根目录可挂在 workspace/data 下。
- `object_storage`：面向生产部署，接口按 S3 兼容对象存储设计，但首版可以先只定义配置与抽象，必要时第二阶段再引入 SDK 依赖。

当前仓库没有现成的对象存储 SDK 依赖，因此首版实现可先完成文件系统 driver，并把对象存储作为同一接口下的后续扩展点；同时在配置模型中预留字段，避免 API 反复变更。

---

## Backend API Design

建议新增独立附件资源，而不是把文件 base64 塞进 `POST /tasks`。

### Upload API

`POST /attachments`

- 请求：`multipart/form-data`
- 字段：`file`
- 可选：`conversation_id`（如果上传时已经位于既有会话中）
- 返回：草稿态附件元数据与 `attachment_id`

返回示例：

```json
{
  "id": "att_123",
  "file_name": "diagram.png",
  "mime_type": "image/png",
  "size_bytes": 182931,
  "kind": "image",
  "status": "draft",
  "expires_at": "2026-04-14T10:00:00Z"
}
```

### Attachment metadata / content API

- `GET /attachments/{id}`：返回附件元数据与当前状态
- `GET /attachments/{id}/content`：鉴权后返回原始内容或预览流
- `DELETE /attachments/{id}`：删除草稿附件，或为后续“用户主动删除会话附件”保留接口

### Task API integration

现有 `POST /tasks` 的 `agent.run` 入参新增：

```json
{
  "input": {
    "conversation_id": "conv_1",
    "provider_id": "aihubmix",
    "model_id": "glm-5",
    "message": "请分析这张图",
    "attachment_ids": ["att_123"]
  }
}
```

附件上传和消息发送解耦后：

- 前端不会因大文件导致 task request 膨胀。
- 后端能在 executor 侧统一做附件状态校验与 hydrate。
- 同一个附件卡片在发送前可以被移除或失败重试。

### Conversation API integration

`GET /conversations/{id}/messages` 返回的 `ConversationMessage` 需新增：

- `attachments []AttachmentRef`

会话列表接口不必返回附件细节，但单会话消息列表必须返回附件元数据，供历史展示与 transcript 恢复。

---

## Executor Integration

### Run input

`core/agent.RunTaskInput` 新增 `AttachmentIDs []string`。

### Runtime hydration flow

执行路径建议如下：

1. `TaskHandler` 创建 `agent.run` 任务时保存 `attachment_ids`。
2. `core/agent/executor.go` 在构造 `userMessage` 前：
   - 校验附件归属、状态与可访问性。
   - 对 `draft` 附件执行 `PromoteToSent(...)`，绑定到当前会话。
   - 从 attachment store 读取 bytes，组装运行时 `model.Attachment`。
3. 写入会话历史时，不把运行时 `Data []byte` 落库，只落 `AttachmentRef`。
4. provider 调用完成后，无需删除 `sent` 附件。

### Replay / retry integration

- `ConversationStore.ListReplayMessages(...)` 返回的 replay message 仍包含附件引用信息。
- 在真正发起 provider 请求前，再按需要 hydrate replay tail 中的附件。
- memory summary 中已经被压缩掉的旧附件，不需要每轮重新 hydrate。

---

## Provider Capability Model

当前模型目录没有能力声明字段，前端无法区分哪些模型支持附件。建议给 `core/types.LLMModel` 增加显式 capability 字段，例如：

```go
type ModelCapabilities struct {
	Attachments bool `yaml:"attachments" json:"attachments"`
}
```

或更细化：

```go
type ModelCapabilities struct {
	ImageAttachments bool `yaml:"imageAttachments" json:"image_attachments"`
	TextAttachments  bool `yaml:"textAttachments" json:"text_attachments"`
}
```

建议首版至少做到布尔级别的 `attachments` 能力声明，并按实际 client 实现配置：

- `openai_completions`: true
- `google`: true
- `openai_responses`: false（直到补齐实现）

前端只有在当前模型声明支持附件时才展示上传入口；否则保留按钮隐藏或禁用文案，避免用户误以为附件会生效。

---

## Frontend UX

### MessageComposer

`MessageComposer.vue` 建议改为“文本输入 + 附件草稿队列”的组合：

- 使用 Element Plus 拖拽上传能力，作为主要上传入口。
- 保留点击选择文件。
- 监听 `paste` 事件，检测图片 `ClipboardItem`，自动上传。
- 在发送按钮上方或输入框内展示附件草稿卡片。

草稿附件状态：

- `uploading`
- `ready`
- `failed`
- `removing`

发送规则：

- 只要仍有 `uploading` 状态，发送按钮禁用。
- 文本为空但有附件时允许发送。
- 发送成功后清空当前草稿附件队列。

### MessageList

`MessageList.vue` 在消息气泡内渲染附件卡片：

- 图片：缩略图 + 点击打开
- 文本/JSON/代码：文件卡片 + 预览弹层/抽屉
- 其他类型：文件卡片 + 下载按钮
- 已过期：灰态卡片 + “附件已过期”

### ChatView

`ChatView.vue` 负责：

- 维护当前会话草稿附件状态
- 切换会话时清理未发送草稿或保留当前会话草稿缓存
- 调用上传 API 获取 `attachment_id`
- 发送消息时把 `attachment_ids` 塞入 `buildRunTaskRequest(...)`

---

## Error Handling Rules

### Upload phase

- MIME/type 不支持、空文件、超大小、存储失败：`POST /attachments` 直接返回明确错误。
- 前端草稿卡片显示失败态与重试/移除操作。

### Send phase

- 若所选模型不支持附件：阻止上传入口或在发送前显式报错。
- 若 `attachment_id` 不存在、越权、已过期、仍处于不可发送状态：`agent.run` 直接失败。
- 不允许静默丢弃附件并仅发送文本。

### Replay phase

- 普通续聊：若旧附件已被压缩出 replay tail，不影响继续对话。
- 精确 replay / retry：若所需附件实体已过期，则明确失败，并返回“附件已过期，无法精确重放”。

### History rendering phase

- 元数据仍在而实体失效时，历史消息必须保留附件卡片并显示状态。

---

## Cleanup Strategy

### Draft GC

- `draft` TTL 建议默认 24h。
- 定时任务按 `expires_at` 扫描删除 storage 实体并把记录标为 `expired` 或 `deleted`。

### Sent retention

- `sent` 附件跟会话 retention 走。
- 删除会话时同步清理关联附件记录与 storage 对象。
- 若后续需要合规策略，可扩展为 workspace 级 retention 配置。

### Access refresh

- 访问附件内容时可更新 `last_accessed_at`，用于运维观察，但不建议自动延长 retention，避免行为不可预测。

---

## Migration And Compatibility Notes

- 现有 `model.Message.Attachments` 已在多处使用，首版可保留运行时结构不变，但新增一层“持久化 DTO”来避免数据库落 `Data []byte`。
- `ConversationStore.AppendMessages(...)` 需要从“持久化运行时 message”切换为“持久化精简 message payload”。
- `ConversationStore.ListMessages(...)` / `ListReplayMessages(...)` 需要在 decode 时恢复附件引用元数据，并把真正的 bytes hydrate 推迟到 executor 请求构造阶段。
- HTTP swagger 类型、前端 `ConversationMessage`、`RunTaskRequest`、`TaskInput` 都需要同步扩展。

---

## Open Questions Closed By This Design

1. 提供给 LLM 后，后续消息重放需要重放文件吗？
答：需要，但只对仍属于 replay tail 的附件重放；已压缩进 summary 的旧附件不再每轮重复发送。

2. LLM 压缩记忆后，原附件需不需要在对话记录中持久化？
答：需要，保留附件引用与元数据；memory 只是派生摘要。

3. 前端如何展示对话记录中的文件？
答：在消息气泡内部渲染附件卡片，按图片/文本/其他文件分类展示，并保留过期态。

4. storage 适合做成阅后即焚还是实际存储位置？
答：默认应作为会话附件的实际存储位置，而不是一次性中转站。

5. storage 是否建议支持文件系统与对象存储两种？
答：是，统一抽象下支持两种后端；首版先落文件系统实现，对象存储预留接口与配置。

6. 上传后未发送是否需要清理机制？
答：需要，`draft` 附件必须有 TTL + GC。

---

## Recommended Implementation Order

1. 先定义附件领域模型、表结构、storage 抽象与迁移。
2. 再补附件 HTTP API 与鉴权。
3. 再把 `agent.run`、conversation message、executor hydrate 链路打通。
4. 再补前端上传、拖拽、粘贴图片与消息展示。
5. 最后补 provider capability、过期重放语义与完整测试矩阵。
