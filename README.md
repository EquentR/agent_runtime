# Agent Runtime

`agent_runtime` 是一个面向 Go 的 Agent 后端框架，附带配套前端，用于构建支持持久化任务调度、工具执行、人工审核和审计记录的 AI Agent 服务。

## 🎯 设计目标

- 将 agent 执行从一次性请求升级为可持久化、可观察、支持断点恢复的后台任务。
- 将模型、工具、提示词、会话与任务调度统一在同一套运行时中，减少业务层的重复组装。
- 将人工审批、人工问答与审计记录纳入标准执行流程，由内建存储和 API 统一管理。
- 同时提供后端参考实现和前端参考界面，可直接用于开发调试、功能演示和二次开发。

## ⚡ 功能概览

### Agent 运行时

- 统一的 `agent.run` 执行入口，负责驱动模型调用、工具调用、会话写入和事件推送。
- 会话持久化：自动维护对话标题、最近消息、消息计数等字段，无需业务层手动管理。
- 短期记忆压缩：上下文接近长度上限时，自动压缩历史消息并保留关键工作记忆。
- 长期记忆：跨会话持久化用户偏好与关键事实，通过 LLM 做增量压缩。
- 提示词路由：通过 document + binding + resolver 体系，按场景、阶段、模型分发提示词。
- 技能注入：运行时按需解析 workspace skills，通过 `using_skills` 工具动态加载。
- 用户工作区隔离：`workspaceDir` 作为模板和技能来源，运行时写入隔离在 `workspaces.root` 下的用户 home / conversation workspace。

### 任务调度

- 持久化任务队列，支持创建、查询、取消、重试，以及通过 SSE 订阅任务事件。
- Worker pool 并发执行，可通过 `concurrency_key` 对同一资源下的任务做串行保护。
- `waiting` 状态支持任务挂起与恢复——等待审批或用户回复时自动暂停，条件满足后继续执行。
- 支持父子任务基本编排结构，为 fan-out / fan-in 和子 agent 调度预留基础能力。
- `agent.run` 支持 `mutable` / `readonly` workspace 模式；可写模式按会话复用同一个 workspace，未变更时不会提示合并。

### 技能系统

- 基于工作目录 `skills/<name>/SKILL.md` 的技能包定义，运行时按需加载与注入。
- `using_skills` 内建工具可在运行中动态加载技能内容，其结果不写入会话历史（ephemeral）。
- 技能列表 API 支持前端浏览与按名查询。

### 🙋 人工介入

- 工具审批：在工具调用前请求人工确认，支持查询状态、提交通过或拒绝决策。
- 人工问答：任务运行期间可通过 `ask_user` 向用户发起结构化提问，等待回复后继续。
- 审批记录与问答记录均写入数据库，任务恢复执行后结果可追溯。
- 审计轨迹：记录每次任务执行的关键节点，支持事后回放和问题排查。

### 模型与工具

- 统一的 `ChatRequest` / `ChatResponse` / `Stream` 调用抽象，屏蔽不同 provider 的差异。
- 已适配 Gemini、OpenAI-compatible Chat Completions 和 OpenAI Responses API。
- Responses API 适配保留 provider state 的完整 output replay，支持 reasoning、assistant message 与连续工具调用跨轮延续。
- 内建 19 个工具，涵盖文件读写、命令执行、HTTP 请求、进程管理、Web 搜索、技能加载、图像生成/编辑和人工问答，运行范围限制在配置的工作区目录内。
- MCP tools / prompts 接入点已就绪，支持 stdio、SSE 和 Streamable HTTP 三种传输方式对接外部工具生态。

### 🖥️ 前端

- 基于 Vue 3 + TypeScript + Vite，提供登录页、聊天页、个人资料页、会话侧边栏和管理后台。
- 聊天页支持模型切换、流式消息展示、会话恢复、中止运行、审批决策和人工问题回复。
- 聊天页支持工作区只读/可写切换，并在会话工作区存在待合并变更时显示轻量确认控件。
- 消息列表可切换是否展示思考过程与工具调用详情，方便控制信息密度。
- 管理后台覆盖用户、模型、提示词、应用设置、审计会话与后台操作审计，权限通过角色控制。

## 📦 近期主要更新

- **会话级工作区**：`mutable` 运行按 `conversation_id` 复用 workspace，基于 `.workspace-baseline.json` 判断是否存在真实文件变更，最终合并回用户 `home`。
- **工作区隔离**：新增 `workspaces.root`，把用户 home、conversation workspace 与备份目录从模板 `workspaceDir` 中拆出，避免不同用户或任务互相污染。
- **技能系统**：新增 workspace skills 支持，通过 `using_skills` 工具在运行中动态加载技能内容。
- **附件系统**：支持文件上传与在消息中引用附件，附件可在 provider 层按需消费。
- **提示词管理**：从运行时内部配置升级为独立资源，可通过 HTTP API 和管理后台进行增删改查。
- **任务调度**：新增并发 worker、挂起恢复、状态细化，事件轨迹更完整。
- **人工介入**：执行链新增审批流和人工问答流，支持在工具调用前后等待人类决策。
- **模型调用**：新增 OpenAI Responses API 适配、请求预算管控、provider state 完整 output replay 与连续工具调用跨轮复用。
- **前端**：补全审批卡片展示、中文界面文案、动态页面标题、思考与工具调用显示切换，以及工作区合并提示和会话跳转错误处理。

## 🚀 快速开始

### 0. 环境要求

| 运行时 | 最低版本 | 说明 |
|--------|----------|------|
| Go | 1.25.0 | 见 `go.mod` |
| Node.js | 20.19 / 22.12+ | Vite 8 与 Vitest 4 的最低要求；推荐 22.x LTS |
| pnpm | 10.x | `webapp/package.json` 的 `packageManager` 字段声明为 10.26.0 |

主要依赖版本（前端）：Vue 3.5、TypeScript 5.9、Vite 8、Vitest 4。

### 1. 安装依赖

```bash
go mod download
pnpm --dir webapp install
```

### 2. 准备配置

默认配置文件为 `conf/app.yaml`。

- 配置值中可直接使用环境变量占位符，如 `${OPENAI_BASE_URL}`、`${OPENAI_API_KEY}`、`${TAVILY_API_KEY}`，启动时自动展开。
- `workspaceDir`：工作区模板根目录，也是 workspace skills 的来源目录；默认 `workspace`。
- `workspaces.root`：用户实际运行工作区根目录，默认 `data/workspaces`，其下按 `users/{user_id}/home`、`users/{user_id}/tasks/{workspace_id}` 和 `users/{user_id}/backups` 组织。
- `server`：HTTP 监听地址、API 前缀与静态资源路径。
- `sqlite` / `log`：数据库与日志输出配置。
- `security`：应用密钥、Cookie 安全开关、公开注册开关、SMTP 与 Turnstile 等鉴权相关设置。
- `tasks.workerCount`：后台任务的并发 worker 数量。
- `tools`：内建工具的运行参数，含 `webSearch`（搜索 provider）与 `imageGen`（图像生成 provider）。
- `attachments`：附件存储后端、根目录、草稿 TTL、已发送附件保留时长与 GC 间隔。
- `llmRequestTimeout` / `llmProviders`：模型调用超时与可用模型列表，前端模型选择器和 `agent.run` 均从此处读取。
- 如需对接 MCP 外部工具，可在配置中添加 `mcp` 节并配置 `servers`（详见 `core/mcp/README.md`）。

### 3. 启动后端

```bash
go build -o bin/ice_art ./cmd/ice_art
./bin/ice_art -config conf/app.yaml
```

默认监听地址：

- API：`http://127.0.0.1:18080/api/v1`
- Swagger UI：`http://127.0.0.1:18080/api/v1/swagger/index.html`

### 4. 启动前端

```bash
pnpm --dir webapp dev
```

## 📡 API 与页面一览

### HTTP API

| 路由 | 说明 |
|------|------|
| `auth` | 注册、登录、退出、获取当前用户 |
| `settings` | 公开应用设置（注册开关、Turnstile 配置等） |
| `users/me` | 当前用户资料、邮箱验证、密码修改 |
| `users/me/models` | 当前用户的模型偏好与连通性测试 |
| `models` | 查询当前用户可用的 provider 和模型目录 |
| `tasks` | 创建任务、查询详情、取消、重试、订阅 SSE 事件流 |
| `tasks/:id/approvals` | 查询审批记录、提交审批决策 |
| `tasks/:id/interactions` | 查询待回复问题、提交用户回复 |
| `conversations` | 查询会话列表、会话详情、历史消息，删除会话 |
| `conversations/:id/workspace` | 查询、确认或丢弃会话级 workspace 待合并变更 |
| `prompts` | 提示词文档与绑定规则管理 |
| `skills` | 查询 workspace 技能列表与详情 |
| `attachments` | 上传文件附件、查询附件元数据与内容 |
| `audit` | 查询任务运行审计记录 |
| `admin/workspaces` | 管理员：查询用户 workspace home 与待合并任务摘要 |
| `admin/users` | 管理员：用户管理 |
| `admin/models` | 管理员：模型配置与连通性测试 |
| `admin/settings` | 管理员：应用设置维护、SMTP 测试 |
| `admin/audit-events` | 管理员：后台操作审计 |
| `swagger` | 在浏览器中浏览和调试 API |

### 前端页面

| 路径 | 说明 |
|------|------|
| `/login` | 登录页 |
| `/chat/:conversationId?` | 聊天页，含会话侧栏、模型选择、流式消息、审批与问答内嵌交互 |
| `/profile` | 个人资料页（修改资料、邮箱验证、密码修改） |
| `/admin/dashboard` | 管理后台总览（管理员） |
| `/admin/users` | 用户管理（管理员） |
| `/admin/models` | 模型配置（管理员） |
| `/admin/settings` | 应用设置（管理员） |
| `/admin/prompts` | 提示词管理（管理员） |
| `/admin/audit` | 任务审计会话（管理员） |
| `/admin/audit-events` | 后台操作审计（管理员） |

## 🗂️ 目录说明

| 路径 | 说明 |
|------|------|
| `cmd/ice_art` | 可执行入口，读取配置并启动服务 |
| `app/commands` / `app/config` / `app/router` | 应用层装配：服务启动、配置解析、路由注册 |
| `app/handlers` / `app/logics` | HTTP handler 与应用层业务逻辑 |
| `app/migration` | 数据库迁移注册与启动引导 |
| `app/models` | 应用级数据库模型（用户、设置等） |
| `app/logging` | 把 `pkg/log` 适配为 `core/log` 接口的桥接层 |
| `core/agent` | Agent 执行器、流式处理、会话存储、workspace 解析、任务桥接、审计输出 |
| `core/tasks` | 任务存储、调度管理、事件流、并发执行、挂起恢复 |
| `core/prompt` | 提示词文档、绑定规则与分发逻辑 |
| `core/runtimeprompt` | 运行时提示词构建与渲染 |
| `core/forcedprompt` | 强制注入的系统级提示词 provider |
| `core/skills` | 工作目录技能包扫描、解析、只读查询与运行时注入 |
| `core/attachments` | 附件存储后端、元数据与文件系统访问 |
| `core/approvals` | 审批记录存储 |
| `core/interactions` | 人工问答记录存储 |
| `core/audit` | 任务运行审计记录与事件追踪 |
| `core/tools` | 内建工具注册表与 MCP 接入点 |
| `core/providers` | 模型调用抽象与各 provider 适配器 |
| `core/workspaces` | 用户 home / conversation workspace 管理、baseline manifest、合并/丢弃与备份 |
| `core/memory` | 短期上下文管理、长期记忆持久化与上下文预算控制 |
| `core/mcp` | MCP 抽象接口、通用类型与 mark3labs 适配器 |
| `core/types` | 跨模块共享的领域类型（模型配置、工具元数据、任务元数据、成本等） |
| `core/log` | 领域日志门面，由 `app/logging` 适配到 `pkg/log` |
| `core/rag` | 预留：检索增强生成（尚未实现） |
| `pkg` | 数据库、日志、迁移、HTTP、JSON、邮件、密钥等基础设施 |
| `webapp` | 前端应用，包含聊天界面、个人资料与管理后台 |
| `docs` | 设计文档、Swagger 生成产物与子计划 |
| `workspace` | 默认工作区，含 `skills/<name>/SKILL.md` 等技能包；可由 `workspaceDir` 配置覆盖 |

## 📍 当前状态

- 后端与前端均已可运行，适合本地联调、功能演示和在此基础上继续开发。
- 当前功能覆盖：task 驱动的 agent 执行、会话持久化、会话级可合并 workspace、19 个内建工具、3 个模型 provider 适配、workspace skills 系统、短期/长期记忆管理、人工审批/问答、审计追溯、附件上传、提示词管理、用户与模型管理后台、MCP 外部工具接入。
- `core/rag` 为预留目录，尚未实现；多 agent 编排等能力留待后续扩展。

## ✅ 运行验证

后端：

```bash
go test ./...
go build ./cmd/...
go list ./...
```

前端：

```bash
pnpm --dir webapp exec vue-tsc -b
pnpm --dir webapp test
```

更多协作规范、目录职责和改动边界说明请查阅 `AGENTS.md`。
## Release 与镜像

发布二进制名称为 `ice_art`，Windows 平台为 `ice_art.exe`。发布构建会先构建 `webapp`，再通过 Go `embed` 将前端静态文件打包进后端二进制，因此 release 包不再需要携带 `static/web` 目录。

推送到 `main` 或 `master` 只触发自动化测试和构建检查，不发布 Release 或镜像。推送 `v*` tag 会触发 GitHub Release，生成以下六个平台资产：

```text
ice_art_linux_amd64.tar.gz
ice_art_linux_arm64.tar.gz
ice_art_darwin_amd64.tar.gz
ice_art_darwin_arm64.tar.gz
ice_art_windows_amd64.zip
ice_art_windows_arm64.zip
```

容器镜像只在推送 `v*` tag 时发布到 GHCR，并写入版本 tag：

```text
ghcr.io/equentr/ice-art:v0.2.0
ghcr.io/equentr/ice-art:0.2.0
ghcr.io/equentr/ice-art:sha-<commit>
ghcr.io/equentr/ice-art:latest
```

Docker Compose 示例见 `compose.example.yml`。复制 `.env.example` 为 `.env`，填写 `APP_SECRET`、`OPENAI_API_KEY` 等环境变量后启动：

```bash
docker compose -f compose.example.yml up -d
```
