# Agent Runtime

`agent_runtime` 是一个面向 Go 的 Agent Runtime 基座，整合模型调用、tool use、conversation persistence、memory compression 和 durable task orchestration，用于构建可运行的 agent backend。

仓库当前包含一个可启动的参考应用，启动后提供带鉴权、模型目录、`agent.run` 任务、SSE 事件流、会话历史和 Swagger UI 的 HTTP API。

## 当前可用能力

- 任务系统支持创建、查询、取消、重试和事件订阅，可用于承载可观察的长执行流程。
- conversation 和 message 支持持久化，并自动维护 title、last message、message count 等展示字段。
- 统一的 `ChatRequest` / `ChatResponse` / `Stream` 抽象已接入 Gemini、OpenAI-compatible chat completions 和 OpenAI Responses API。
- 本地内建工具覆盖文件、命令、HTTP、web search 等常见场景，并限制在 workspace 边界内；可继续接入 MCP tools / prompts。
- `core/memory` 支持在上下文接近预算时压缩 short-term messages，并保留后续对话所需的工作记忆。
- `cmd/example_agent` 与 `app/*` 已串起配置、migration、auth、model catalog、task manager、conversation store 和 API router，可作为参考实现。

## 现在可以直接跑什么

启动示例应用后，默认可用的接口面包括：

- `auth`：注册、登录、退出、获取当前用户
- `models`：读取当前服务加载的 provider / model 目录，方便前端直接渲染模型选择器
- `tasks`：创建任务、读取任务详情、取消、重试、订阅 SSE 事件流
- `conversations`：读取会话列表、会话详情、历史消息、删除会话
- `swagger`：浏览器内直接调试 API

## Quick Start

### 1) 安装依赖

```bash
go mod download
go mod tidy
```

### 2) 准备配置

默认配置文件是 `conf/app.yaml`。

- 启动前会先展开环境变量，所以可以直接在配置里写 `${OPENAI_BASE_URL}`、`${OPENAI_API_KEY}`
- `workspaceDir` 为空时会回落到当前工作目录；配置后会作为内建文件工具的工作区根目录
- 示例配置已经包含 `llmProviders`，可直接按自己的网关 / key 调整

### 3) 构建并运行示例应用

```bash
go build -o bin/example_agent ./cmd/example_agent
./bin/example_agent -config conf/app.yaml
```

默认启动地址：

- API base path: `http://127.0.0.1:18080/api/v1`
- Swagger UI: `http://127.0.0.1:18080/api/v1/swagger/index.html`

## 仓库导览

- `cmd/example_agent`：参考二进制入口，负责读取配置并启动示例服务
- `app`：参考应用装配层，包含 auth、models、tasks、conversations、swagger 等 HTTP handler
- `core/agent`：stream-first agent loop、`agent.run` executor、conversation store、usage / cost 聚合
- `core/providers`：统一模型抽象与 provider adapter
- `core/tools`：本地工具注册表，以及 MCP tools / prompts 的接入桥梁
- `core/mcp`：MCP 抽象边界与 adapter
- `core/memory`：上下文预算管理与压缩记忆
- `core/tasks`：持久化任务、事件流、runner、取消 / 重试基础能力
- `pkg`：SQLite、migration、REST、logging 等基础设施封装

## 当前阶段

这个仓库当前处于可运行的 agent backend runtime skeleton 阶段。

- 已经落地并可直接使用的重点在 `core/agent`、`core/providers`、`core/tools`、`core/mcp`、`core/memory`、`core/tasks` 与 `app/*`
- 当前参考实现聚焦单线程、多轮对话型 agent runtime；`agent.run` 是默认打通的执行入口
- `core/rag` 仍是占位目录，multi-agent、approval workflow、完整 product UI 也还不是当前仓库的主目标

## 验证

```bash
go test ./...
go build ./cmd/...
go list ./...
```

更多仓库内协作约定和目录说明可查看 `AGENTS.md`。
