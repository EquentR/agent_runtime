# 附件投递策略机制设计

## 背景

当前上传附件会在执行前被 hydrate 成 `model.Attachment`，随后各 provider builder 根据 MIME 类型把附件 bytes 拼进模型请求。这个路径把“用户上传过文件”和“应该直接发给 LLM 的文件”混成了一件事，导致文本、SVG、PDF、Word 等文件可能被错误直传，或在 provider 层报 unsupported attachment type。

新机制把上传保存、workspace 可用性、LLM 直传三件事拆开：上传文件默认成为运行时输入；只有投递策略明确允许的文件才进入 provider 请求。

## 目标

- 所有 agent 任务都可以使用附件上传入口，只要当前运行模式有 workspace 和文件工具。
- 栅格图片在模型配置声明支持图片输入时继续直传给 LLM。
- SVG、文本、JSON、XML、Markdown、代码、PDF、Office 文档和未知二进制默认不直传，只放入任务 workspace。
- workspace-only 文件通过稳定附件清单告知模型，由模型使用 workspace 工具或已选 skill 自行处理。
- `.attachments/` 是运行时输入目录，不参与 mutable workspace 的 baseline、manifest、pending merge 和 merge back。
- 不改现有 `capabilities.attachments` 字段结构，只收窄其语义和 UI 文案。

## 非目标

- 不实现 PDF/OCR/Office 文档摄取。
- 不实现 RAG 或文档索引。
- 不把 PDF 作为默认直传类型。
- 不新增复杂的用户级投递配置界面。
- 不要求 provider builder 继续兜底解析文本、PDF 或 SVG 附件。

## 能力语义

保留现有模型能力字段：

```yaml
capabilities:
  attachments: true
```

第一版不改 schema，仅调整语义：

- 后端语义：`attachments=true` 表示该模型支持图片直传输入。
- 前端文案：把“支持附件”改为“支持图片输入”或“支持图片直传”。
- 上传入口不再由该字段控制；上传入口取决于当前任务是否具备 workspace 和文件工具。

兼容规则：

- `attachments=true`：栅格图片可 direct-to-LLM，其余类型 workspace-only。
- `attachments=false`：所有上传文件都 workspace-only。

未来若需要支持 PDF 直传，可以新增更细粒度能力字段，但本设计不做该扩展。

## 投递策略

新增中心化 planner，在 executor 发模型请求前对附件做投递决策。provider builder 只接收 planner 允许直传的附件。

建议目标类型：

```text
direct_to_llm
workspace_only
workspace_plus_direct
reject
```

第一版实际只使用：

```text
direct_to_llm
workspace_only
reject
```

分类规则：

| 类型 | 示例 | 默认投递 |
| --- | --- | --- |
| 栅格图片 | PNG、JPEG、WebP、GIF、BMP、TIFF | `attachments=true` 时 direct-to-LLM，否则 workspace-only |
| SVG | `image/svg+xml`、`.svg` | workspace-only |
| 文本 | TXT、MD、JSON、XML、CSV、代码文件 | workspace-only |
| PDF | `application/pdf`、`.pdf` | workspace-only |
| Office | DOCX、XLSX、PPTX 等 | workspace-only |
| 压缩包 | ZIP、TAR、GZ 等 | workspace-only 或按安全策略 reject |
| 未知二进制 | octet-stream、空 MIME | workspace-only 或按安全策略 reject |

拒绝只用于文件不可安全落地或不可访问的场景，例如空文件、超限、过期、越权、对象丢失、明确危险类型。

## Executor 流程

在 `resolveExecutorWorkspace(...)` 得到当前 workspace 后、追加 user message 前执行附件规划：

```text
1. 读取 task input 中的 attachment_ids。
2. 校验附件归属、状态、过期时间和底层存储对象。
3. 按 MIME、扩展名、文件头和模型能力生成 delivery plan。
4. 对 workspace-only 附件复制原始文件到当前 workspace 的 .attachments/。
5. 对 direct-to-LLM 附件读取 bytes，生成 model.Attachment。
6. 构造附件清单文本，追加到本轮 user message content。
7. 持久化 user message，保存附件引用但不保存 raw Data。
8. Runner/provider 请求只携带 direct-to-LLM 附件。
```

这会把 `model.Message.Attachments` 的运行时含义收窄为“本次 provider 请求允许直传的附件”。会话展示仍可通过持久化的附件引用显示用户上传过的文件。

## Workspace 目录

workspace-only 文件放入固定运行时目录：

```text
.attachments/
  att_xxx/
    original_filename.ext
    metadata.json
```

`metadata.json` 内容：

```json
{
  "attachment_id": "att_xxx",
  "file_name": "report.pdf",
  "mime_type": "application/pdf",
  "size_bytes": 12345,
  "kind": "document",
  "delivery": "workspace_only",
  "reason": "pdf defaults to workspace because provider pdf support is not portable"
}
```

`.attachments/` 必须被 workspace manifest 忽略：

- 不进入 baseline。
- 不触发 pending merge。
- 不 merge 回 home workspace。
- 工具仍可读、列出、搜索和执行相关命令。

如果模型需要保留、转换或编辑上传文件，它应显式把结果复制或写入普通 workspace 路径。普通路径的变更继续参与 mutable workspace 的 merge 流。

## 模型可见清单

对 workspace-only 文件，不把内容塞进 prompt，只注入可行动清单：

```text
Uploaded files are available in the workspace:

- report.pdf
  attachment_id: att_xxx
  path: .attachments/att_xxx/report.pdf
  mime_type: application/pdf
  size_bytes: 12345
  delivery: workspace_only
  note: Use workspace tools or selected skills to inspect this file when relevant.
```

对 direct-to-LLM 图片，也保留短引用，便于后续工具用 `attachment_id` 做图片编辑：

```text
- source.png
  attachment_id: att_image
  delivery: direct_to_llm
  note: This image was sent directly to the model when supported.
```

## 前端行为

上传按钮和拖拽区在 agent workspace 可用时显示，不再按模型 `capabilities.attachments` 隐藏。

模型配置保留字段但改文案：

- 原文案：支持附件。
- 新文案：支持图片输入，或支持图片直传。

发送消息时仍传 `attachment_ids`。前端无需预判哪些文件会直传，后端 planner 是唯一投递决策来源。

Transcript 继续显示用户上传的附件 chip。上传 metadata 可继续返回 `kind`，也可以新增内部说明字段；第一版不要求 UI 展示投递策略。

## Provider 边界

provider builder 不再负责决定哪些文件应该直传，只处理 planner 已批准的 direct attachments。

第一版需要保证：

- SVG 不会以 `image/*` 规则进入图片直传。
- 文本、JSON、XML、Markdown 不会被 provider builder 拼进 prompt。
- PDF 和 Office 文件不会进入 provider request。
- OpenAI Chat、OpenAI Responses、Google 等 builder 都只接收栅格图片 direct attachments。

如果未来支持 PDF 直传，应新增 provider-specific 能力和 builder 分支，而不是让 PDF 默认走通用附件路径。

## 错误处理

上传期错误：

- 文件为空。
- 文件超过系统上传上限。
- MIME 与扩展名冲突且无法安全分类。
- 安全策略拒绝的类型。

规划期错误：

- 附件不存在。
- 附件不属于当前用户。
- 附件已过期。
- 底层对象丢失。
- 复制到 workspace 失败。

降级行为：

- 模型不支持图片输入时，栅格图片降级为 workspace-only。
- provider 不支持非图片附件不再导致任务失败，因为这些附件不会进入 provider request。

## 测试边界

后端分类测试：

- PNG/JPEG/WebP 在 `attachments=true` 时 direct-to-LLM。
- PNG/JPEG/WebP 在 `attachments=false` 时 workspace-only。
- SVG 始终 workspace-only。
- TXT/MD/JSON/XML/CSV 始终 workspace-only。
- PDF 始终 workspace-only。
- DOCX/XLSX/PPTX 始终 workspace-only。
- 过期、越权和对象丢失返回规划错误。

executor 测试：

- workspace-only 附件复制到 `.attachments/att_xxx/`。
- `.attachments/` 不触发 pending merge。
- user message 包含附件清单。
- provider request 不包含 workspace-only 附件 bytes。
- direct image 仍作为 provider attachment 发送。
- 会话持久化继续不保存 raw Data。

provider 测试：

- builder 不再收到文本、PDF、SVG 附件。
- `image/svg+xml` 不再被当作图片直传。
- 栅格图片直传行为保持兼容。

前端测试：

- 上传按钮不再由模型 `capabilities.attachments` 控制。
- 模型配置文案改为图片输入含义。
- transcript 附件显示保持不变。

## 迁移策略

不做数据库迁移。

现有 `capabilities.attachments` 配置继续可读，但说明和 UI 文案更新为图片输入能力。旧配置中 `attachments=true` 的模型保持图片直传能力；`attachments=false` 的模型仍可上传文件，只是不直传任何附件。

历史消息中的附件引用保持兼容。历史回放 hydrate 时也应经过相同 planner，避免旧文本或 SVG 附件在后续回放中重新进入 provider request。

