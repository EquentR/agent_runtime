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
- 提示词路由：通过 document + binding + resolver 体系，按场景、阶段、模型分发提示词。

### 任务调度

- 持久化任务队列，支持创建、查询、取消、重试，以及通过 SSE 订阅任务事件。
- Worker pool 并发执行，可通过 `concurrency_key` 对同一资源下的任务做串行保护。
- `waiting` 状态支持任务挂起与恢复——等待审批或用户回复时自动暂停，条件满足后继续执行。
- 支持父子任务基本编排结构，为 fan-out / fan-in 和子 agent 调度预留基础能力。

### 🙋 人工介入

- 工具审批：在工具调用前请求人工确认，支持查询状态、提交通过或拒绝决策。
- 人工问答：任务运行期间可通过 `ask_user` 向用户发起结构化提问，等待回复后继续。
- 审批记录与问答记录均写入数据库，任务恢复执行后结果可追溯。
- 审计轨迹：记录每次任务执行的关键节点，支持事后回放和问题排查。

### 模型与工具

- 统一的 `ChatRequest` / `ChatResponse` / `Stream` 调用抽象，屏蔽不同 provider 的差异。
- 已适配 Gemini、OpenAI-compatible Chat Completions 和 OpenAI Responses API。
- 内建工具涵盖文件读写、命令执行、HTTP 请求、进程管理和 Web 搜索，运行范围限制在配置的工作区目录内。
- 预留 MCP tools / prompts 接入点，支持对接外部工具生态。

### 🖥️ 前端

- 基于 Vue 3 + TypeScript + Vite，提供登录页、聊天页、会话侧边栏和管理后台。
- 聊天页支持模型切换、流式消息展示、会话恢复、中止运行、审批决策和人工问题回复。
- 消息列表可切换是否展示思考过程与工具调用详情，方便控制信息密度。
- 管理后台提供提示词维护和审计记录查看功能，权限通过角色控制。

## 📦 近期主要更新

- **提示词管理**：从运行时内部配置升级为独立资源，可通过 HTTP API 和管理后台进行增删改查。
- **任务调度**：新增并发 worker、挂起恢复、状态细化，事件轨迹更完整。
- **人工介入**：执行链新增审批流和人工问答流，支持在工具调用前后等待人类决策。
- **模型调用**：修复 OpenAI completions 流式工具调用参数块的连续性问题，减少 reasoning 与 tool call 展示错乱。
- **前端**：补全审批卡片展示、中文界面文案、动态页面标题、思考与工具调用显示切换。

## 🚀 快速开始

### 1. 安装依赖

```bash
go mod download
pnpm --dir webapp install
```

### 2. 准备配置

默认配置文件为 `conf/app.yaml`。

- 配置值中可直接使用环境变量占位符，如 `${OPENAI_BASE_URL}`、`${OPENAI_API_KEY}`、`${TAVILY_API_KEY}`，启动时自动展开。
- `workspaceDir`：内建文件工具的根目录，留空时使用当前工作目录。
- `tasks.workerCount`：后台任务的并发 worker 数量。
- `llmProviders`：配置可用的模型列表，前端模型选择器和 `agent.run` 均从此处读取。

### 3. 启动后端

```bash
go build -o bin/example_agent ./cmd/example_agent
./bin/example_agent -config conf/app.yaml
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
| `models` | 查询可用的 provider 和模型列表 |
| `tasks` | 创建任务、查询详情、取消、重试、订阅 SSE 事件流 |
| `conversations` | 查询会话列表、会话详情、历史消息，删除会话 |
| `prompts` | 提示词文档与绑定规则管理（需管理员权限） |
| `approvals` | 查询审批记录、提交审批决策 |
| `interactions` | 查询待回复问题、提交用户回复 |
| `audit` | 查询任务运行审计记录 |
| `swagger` | 在浏览器中浏览和调试 API |

### 前端页面

| 路径 | 说明 |
|------|------|
| `/login` | 登录页 |
| `/chat` | 聊天页，含会话侧栏、模型选择、流式消息、审批与问答内嵌交互 |
| `/admin/prompts` | 提示词管理页（管理员） |
| `/admin/audit` | 审计记录页（管理员） |

## 🗂️ 目录说明

| 路径 | 说明 |
|------|------|
| `cmd/example_agent` | 可执行入口，读取配置并启动服务 |
| `app` | 应用层，负责依赖组装、数据库迁移、路由注册和 HTTP handler |
| `core/agent` | Agent 执行器、流式处理、会话存储 |
| `core/tasks` | 任务存储、调度管理、事件流、并发执行、挂起恢复 |
| `core/prompt` | 提示词文档、绑定规则与分发逻辑 |
| `core/approvals` | 审批记录存储 |
| `core/interactions` | 人工问答记录存储 |
| `core/audit` | 任务运行审计记录与事件追踪 |
| `core/tools` | 内建工具注册表与 MCP 接入点 |
| `core/providers` | 模型调用抽象与各 provider 适配器 |
| `core/memory` | 上下文长度管理与记忆压缩 |
| `pkg` | 数据库、日志、迁移、HTTP 工具等基础设施 |
| `webapp` | 前端应用，包含聊天界面和管理后台 |

## 📍 当前状态

- 后端与前端均已可运行，适合本地联调、功能演示和在此基础上继续开发。
- 当前功能集中在单 agent 运行、任务调度、人工介入和管理后台，暂不包含完整产品化功能。
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
