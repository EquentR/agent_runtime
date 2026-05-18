# 公网化管理员后台设计

## 概要

项目将从 self-host 场景转向公网使用。第一阶段要先补齐账号安全、运行时配置、用户管理和模型权限，再把现有提示词管理、审计会话纳入统一管理员后台。

设计采用“安全底座优先”的路线：先保证公网注册、登录、邮箱验证、Turnstile、人机验证、敏感配置加密、核心 API 权限 gate 闭环，再实现后台用户/设置界面，最后接入模型自定义和可用范围控制。

## 目标

- 注册支持邮箱唯一、邮箱验证码校验。
- 登录支持用户名或邮箱，并可选接入 Cloudflare Turnstile。
- Turnstile 开启后保护登录、注册、发送/重发邮箱验证码。
- 管理员后台新增用户管理、SMTP 配置、登录安全配置、模型管理、后台操作审计。
- 普通用户新增个人资料页，可查看/修改 `display_name`、换绑并验证邮箱、修改密码、管理自己的自定义 LLM 模型。
- SMTP、Turnstile 使用 YAML 默认值 + 数据库覆盖，保存后立即生效。
- LLM 模型支持按用户界面自定义；YAML 模型支持单模型配置为全局可用或管理员可用。
- 管理员默认可用 YAML 中管理员/全局模型，也可配置自己的自定义模型；不可在聊天任务中使用其他用户配置的模型，但可查看并测试其连通性，测试必须写后台审计。
- 敏感字段使用 `APP_SECRET` 派生密钥做应用层加密。

## 非目标

- 不做邮件模板后台编辑，第一阶段使用内置验证码邮件模板。
- 不做邀请注册；公开注册关闭时第一阶段只支持管理员创建用户并设置初始密码。
- 不做 KMS、密钥轮换、历史密文重加密流程。
- 不允许后台覆盖 YAML 模型的 base URL、API key、成本、上下文等字段；后台只覆盖 YAML 模型的启用状态和可用范围。
- 不重构现有 agent run 审计表为后台操作审计表；后台操作使用独立审计表。

## 已确认决策

- 账号标识：用户名或邮箱都可登录，邮箱必须唯一并验证。
- 注册流程：先创建待验证账号，发送邮箱验证码，验证后才能登录。
- 邮箱验证方式：6 位数字验证码，10 分钟过期，60 秒重发间隔，最多尝试 5 次。
- 首个用户：注册时仍必须填写邮箱，但直接标记邮箱已验证，并自动成为管理员。
- 第二个用户起：公开注册必须 SMTP 可用；SMTP 不可用时隐藏注册入口，后端返回不可用错误。
- 公开注册：后台可开关。关闭后前端隐藏注册 tab，后端注册接口返回 403。
- 管理员创建用户：管理员设置初始密码；用户首次登录后强制改密码并验证邮箱。
- 旧账号迁移：无邮箱的旧账号可登录，但只能进入绑定并验证邮箱、改密、个人设置流程，不能使用核心功能。
- 后台导航：按业务域拆分，一级入口包括用户管理、模型管理、系统设置、提示词管理、审计会话、后台操作审计。
- YAML 模型未声明 scope 时默认 `admin`，普通用户默认不可见不可用。
- 用户自定义模型支持当前已有三类后端类型：OpenAI Responses、OpenAI Chat Completions、Google GenAI。
- API key、SMTP 密码、Turnstile secret 不通过 API 返回明文，只返回 masked 展示；创建/更新时可覆盖。
- 无可用模型时聊天页显示空状态，提示联系管理员或去个人设置添加自定义模型。

## 架构边界

继续保持现有 `app -> core -> pkg` 分层。

`app/handlers` 只负责 HTTP 入参解析、响应整形、鉴权检查和状态码映射。新增路由继续通过带 `Register` 方法的 handler 在 `app/router/init.go` 集中注册，依赖通过 `app/router/deps.go` 下传。

`app/logics` 承载认证、用户管理、邮箱验证、系统设置读写、后台操作审计协调等应用层逻辑。非平凡流程不放在 handler 中。

`core` 承载可复用 runtime 能力。模型目录需要从当前静态 `ModelResolver` 扩展为按用户解析的 runtime catalog：合并 YAML provider/model、YAML override、自定义模型，再按当前用户状态和角色过滤可见/可用结果。任务创建和 executor 都必须使用同一个后端 resolver 校验模型权限，前端 catalog 不能作为权限来源。

`pkg` 新增共享基础设施，例如敏感字段加密和 SMTP sender。加密与发送邮件不散落到 handler。

`webapp` 新增统一后台布局、用户管理、模型管理、系统设置、后台操作审计、个人设置页面。现有提示词管理和审计会话迁移到统一后台框架中。

## 数据模型

### 用户

扩展 `users`：

- `email`：唯一邮箱，首个用户也必须填写。
- `email_verified_at`：邮箱验证时间。
- `display_name`：普通用户可修改，可重复。
- `status`：用户状态枚举，第一阶段固定为 `pending_email_verification`、`active`、`disabled`、`needs_email_binding`。
- `force_password_change`：管理员创建或重置密码后要求首次登录改密。

`username` 保持稳定，不提供用户自助修改。这样可以减少现有 `created_by`、会话归属、审计显示等历史兼容风险。

### 邮箱验证码

新增 `email_verifications`：

- 用户 ID、邮箱、用途。
- 验证码 hash，不保存明文。
- 过期时间、尝试次数、最后发送时间。
- 状态或消费时间。

用途至少覆盖注册验证、邮箱换绑、旧账号补邮箱。

### 系统设置

新增 `system_settings` 表，用于保存 DB 覆盖值。每条记录使用 `key`、`value_json`、`encrypted`、`updated_by`、`created_at`、`updated_at` 字段；敏感字段在写入 `value_json` 前单独加密并以 masked 形式返回 API。

- SMTP：host、port、username、加密后的 password、from、TLS/STARTTLS 选项。
- Turnstile：enabled、site key、加密后的 secret、保护范围。
- 公开注册：enabled。

YAML 提供默认值。后台保存 DB 配置后覆盖 YAML，并立即影响新请求。

### 模型

新增 `llm_model_overrides` 表：

- `provider_id`
- `model_id`
- `enabled`
- `scope`

YAML 模型除 `enabled/scope` 外仍以配置文件为准。

新增 `custom_llm_models` 表：

- `owner_user_id`
- `id`：运行时唯一模型配置 ID。
- `provider_id`：展示与兼容前端选择用的 provider ID，同一 owner 下唯一。
- `model_id`
- `display_name`
- `provider_type`：`openai_responses`、`openai_completions`、`google`
- `base_url`
- 加密后的 `api_key`
- `scope`：`owner`、`admin`、`global`
- `enabled`
- `context_max_tokens`：模型上下文硬上限，必填，用于运行时预算保护。
- `capabilities_json`、`cost_json`：可选模型元数据。

自定义模型第一阶段不单独存储 input/output context 字段。运行时根据 `context_max_tokens` 动态计算：

- `max_context_tokens = context_max_tokens`
- `default_output_tokens = min(8192, floor(context_max_tokens / 4))`
- `input_budget_tokens = context_max_tokens - default_output_tokens`

若 `context_max_tokens` 小于 4，则拒绝保存配置。任务运行时不得让输入预算和输出预算之和超过 `context_max_tokens`。用户后续如果显式配置单次输出上限，也必须满足 `1 <= output_tokens < context_max_tokens`，且默认值仍不超过 8192 tokens。

管理员创建的自定义模型可选择 `owner/admin/global`。普通用户自定义模型只能是 owner-scoped，除非后续明确授权流程。

### 后台操作审计

新增 `admin_audit_events`：

- actor user id/username/email
- target kind/id
- action
- before/after 摘要，避免保存敏感明文
- request IP、user agent
- created_at

记录管理员修改用户角色、启用/禁用、邮箱与验证状态、重置密码、SMTP/Turnstile/注册设置、模型启用/scope 变更、管理员测试其他用户模型等操作。

### 敏感字段加密

SMTP 密码、Turnstile secret、自定义模型 API key 使用 `APP_SECRET` 派生密钥加密保存。服务启动时缺少 `APP_SECRET` 直接失败。测试环境使用显式测试密钥。

第一阶段不设计密钥轮换和历史密文迁移。

## 认证与用户流程

### 首个管理员

当用户表为空时，注册表单仍要求邮箱、用户名、密码。后端创建用户时直接设置：

- role = `admin`
- status = `active`
- email_verified_at = now

该用户不需要 SMTP，也不需要邮箱验证码，用于首次进入后台配置 SMTP 和 Turnstile。

### 公开注册

当公开注册开启且用户表不为空：

1. 若 Turnstile 在注册范围开启，先校验 Turnstile token。
2. 检查 SMTP 是否可用。不可用时前端隐藏注册入口，后端返回不可用错误。
3. 创建 `pending_email_verification` 用户。
4. 发送 6 位邮箱验证码。
5. 用户在验证码页提交验证码。
6. 验证通过后用户变为 `active`，之后才能登录和使用核心功能。

公开注册关闭时，前端隐藏注册 tab，后端注册接口返回 403。

### 登录

登录输入接受用户名或邮箱。若 Turnstile 在登录范围开启，先校验 token。

密码正确后，后端还要检查：

- 用户未禁用。
- 若 `force_password_change` 为 true，只允许进入强制改密流程。
- 若 `needs_email_binding`，只允许进入绑定邮箱与验证流程。
- 若 `pending_email_verification`，不发放完整核心功能访问，只引导验证邮箱。

### 管理员创建用户

管理员输入用户名、邮箱、初始密码。新用户首次登录后必须修改密码并验证邮箱。管理员可以重发验证邮件。

### 普通用户设置

普通用户可以：

- 查看 username、email、role、状态。
- 修改 `display_name`。
- 修改密码。
- 换绑邮箱，换绑需要验证码确认。
- 管理自己的自定义 LLM 模型。

## 运行时权限 Gate

核心 API 不只校验 session，还必须校验用户状态。至少聊天、任务创建、附件上传、模型 catalog、审批/交互等核心能力需要经过同一套 gate：

- session 有效。
- 用户未禁用。
- 用户 active。
- 邮箱已验证。
- 不处于强制改密状态。

个人设置、邮箱绑定/验证、强制改密页面按单独权限放行。管理员后台需要额外校验 role = `admin`。

任务创建必须以后端当前 session 用户校验模型权限，不能信任前端传入的 provider/model。

## 模型权限规则

模型 scope：

- `global`：所有 active 且邮箱已验证用户可用。
- `admin`：管理员角色可用。
- `owner`：仅创建者可用。

YAML 模型：

- 未声明 scope 默认 `admin`。
- 后台可调整 enabled 和 scope。
- 管理员可用 `admin/global` YAML 模型。
- 普通用户只可用 `global` YAML 模型。

自定义模型：

- 普通用户可创建并使用自己的 owner-scoped 模型。
- 管理员可创建自己的模型，并选择 `owner/admin/global`。
- 管理员可以查看其他用户模型。
- 管理员不能在聊天任务中使用其他用户模型。
- 管理员可以测试其他用户模型连通性，但必须写后台操作审计。

模型配置保存后立即影响 `/models` catalog 和新任务，已运行任务不受影响。

无可用模型时，聊天页显示空状态并引导联系管理员或去个人设置添加自定义模型。

自定义模型必须设置 `context_max_tokens`。模型 resolver 在组装 `LLMModel.Context` 时把该值映射为 `Max`，并按默认输出预算规则填充 `Output` 和 `Input`，避免缺失上下文信息导致 memory budget 或模型请求溢出。YAML 模型继续使用配置文件中的 context；如果 YAML 模型缺少 context，沿用现有后端默认值，但后台需要在模型管理页标记为“使用系统默认上下文”。

## 系统设置

### SMTP

SMTP 使用 YAML 默认 + DB 覆盖。管理员后台可见可编辑，普通用户不可见。普通用户只看到自己的邮箱验证状态和重发入口。

第一阶段使用内置验证码邮件模板，不提供后台模板编辑。

### Turnstile

Turnstile 使用 YAML 默认 + DB 覆盖。后台可配置：

- enabled
- site key
- secret
- 保护范围：登录、注册、发送/重发验证码

前端使用 site key 渲染，后端使用 secret 校验 token。

### 公开注册

后台可开关公开注册。关闭后，前端隐藏注册 tab，后端注册接口返回 403。

## API 设计

建议 API 分组：

- `/auth`：登录、注册、登出、当前用户、邮箱验证码发送/验证、首次强制改密。
- `/users/me`：个人资料、display name、邮箱换绑、密码修改、用户自己的模型管理入口。
- `/admin/users`：用户列表、搜索、详情、角色变更、状态变更、邮箱/验证状态调整、重置密码、重发验证邮件。
- `/admin/settings`：SMTP、Turnstile、公开注册配置，配置测试。
- `/admin/models`：YAML 模型 enabled/scope override、自定义模型管理、模型连通性测试。
- `/admin/audit-events`：后台操作审计查询。
- `/models`：返回当前用户可用模型目录。
- `/tasks`：创建任务时重新校验用户状态和模型权限。

请求/响应字段变化需要同步更新 Swagger 注解和生成文件。

## 前端设计

后台采用统一 `AdminLayout`，按业务域拆分：

- 仪表盘
- 用户管理
- 模型管理
- 系统设置
  - SMTP
  - 登录安全
  - 公开注册
- 提示词管理
- 审计会话
- 后台操作审计

普通用户新增个人设置页：

- 账号资料：display name、邮箱状态、邮箱换绑。
- 安全：修改密码、首次登录强制改密。
- 我的模型：添加、测试、禁用、删除自己的自定义模型。

登录/注册页面：

- 登录框接受用户名或邮箱。
- 注册关闭时隐藏注册 tab。
- 注册后进入验证码页。
- Turnstile 开启时在登录、注册、发送/重发验证码流程中展示。

## 交付切分

### Phase 1：安全底座

- 数据库迁移。
- 用户状态与旧账号迁移。
- 邮箱验证码 store/logic。
- SMTP sender 与系统设置 store。
- Turnstile 配置与校验。
- 敏感字段加密。
- 核心 API 用户状态 gate。

### Phase 2：用户与设置后台

- AdminLayout。
- 用户管理页。
- 普通用户个人设置页。
- SMTP 配置页。
- 登录安全/公开注册配置页。
- 登录/注册/验证码前端流程。

### Phase 3：模型权限与自定义模型

- YAML 模型 scope/enabled override。
- 自定义模型 store/logic。
- 自定义模型 `context_max_tokens` 校验和运行时 input/output 预算计算。
- 按用户过滤 `/models` catalog。
- 任务创建强校验。
- 模型测试接口。
- 聊天页无模型空状态。

### Phase 4：审计与收尾

- 后台操作审计查询页。
- 高风险后台操作写审计。
- Swagger 更新。
- 全量测试与构建验证。

## 错误处理

- 邮箱或用户名重复返回明确冲突错误。
- SMTP 未配置且非首个用户注册时，返回服务不可用错误。
- Turnstile 校验失败返回 400 或 401，并避免暴露 secret 细节。
- 验证码过期、尝试次数耗尽、重发过快返回明确业务错误。
- 用户状态不允许访问核心功能时，返回可被前端区分的错误码或错误类型，前端引导到对应页面。
- 模型不可用或无权限时，后端返回明确错误，不能只依赖前端隐藏。

## 测试策略

后端：

- `app/logics`：注册、登录、首个管理员、旧账号状态、验证码、Turnstile、设置覆盖。
- `app/handlers`：auth、users、admin users、settings、models、tasks 权限边界。
- `app/migration`：新增表和 users 扩展列。
- `core`：模型 resolver 合并 YAML + DB，并按用户过滤。
- `core/memory` 与 `core/agent`：自定义模型 context 上限映射、默认输出不超过 8192 tokens、输入和输出预算不超过总 context。
- `pkg`：加密、SMTP sender 可替换测试。

前端：

- `webapp/src/lib/session.ts`：新 session 状态、强制同步、状态跳转。
- `webapp/src/lib/api.ts`：新增 API normalization 和 masked secret 字段。
- `webapp/src/router/index.ts`：admin、profile、验证/改密、核心页面 gate。
- `LoginView`、个人设置页、用户管理页、设置页、模型管理页、Chat 无模型空状态。

最终验证命令：

- `go test ./...`
- `go build ./cmd/...`
- `go list ./...`
- `pnpm --dir webapp exec vue-tsc -b`
- 相关 Vitest 命令

## 残余风险

- `APP_SECRET` 一旦丢失，已加密配置无法解密；第一阶段只要求部署方稳定保存密钥，不提供轮换。
- YAML 模型默认 `admin` 会改变现有普通用户模型可用行为，需要在发布说明中明确。
- 管理员可修改用户邮箱并调整验证状态，必须依赖后台审计和二次确认降低误操作风险。
- 管理员测试其他用户模型会实际使用对方密钥，必须清晰标注并写审计。
