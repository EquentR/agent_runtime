# Agent Runtime

一个基于 Go 的轻量级 Agent Runtime，用于承载 LLM provider、tool calling、MCP、memory、durable task manager 与基础服务装配能力。当前仓库更接近可运行的 runtime skeleton，加上一个基于 Gin + SQLite 的示例应用，而不是完整的 agent product。

## 当前状态

- 已落地的核心能力集中在 `core/providers`、`core/tools`、`core/mcp`、`core/memory`、`core/tasks`、`core/types` 与 `pkg/*`
- 当前唯一命令行入口是 `cmd/example_agent`
- 示例应用会读取 `conf/app.yaml`，展开环境变量后启动 HTTP 服务、SQLite、migration、后台 task manager 与 Swagger UI 路由
- `core/agent`、`core/rag` 目前尚未落地，不应按 README 旧规划假定它们已经存在

## 已实现能力

- **LLM Providers**：统一的 `ChatRequest` / `ChatResponse` / `Stream` 抽象，已接入 Google Gemini、OpenAI-compatible chat completions、OpenAI Responses API
- **Tool System**：本地工具注册表、内建文件/命令/HTTP/web search 工具、MCP tool 与 prompt 包装
- **MCP**：`core/mcp` 抽象层与 `mark3labs` adapter，支持远端 tools / prompts 集成
- **Memory**：会话压缩记忆、token budget 分配、summary 注入
- **Task Manager**：`core/tasks` 提供持久化任务快照、事件流、后台串行 runner、取消、重试与 SSE 观测基础能力
- **Infrastructure**：SQLite、migration、Gin REST、Zap 日志等基础设施
- **Example App**：`app/*` 下提供最小可运行的服务装配、任务 API、Swagger UI、handler、logic 与 migration 示例

## 项目结构

```text
agent_runtime
├── cmd/example_agent        # 示例程序入口，加载配置并启动服务
├── app                      # 示例应用装配层
│   ├── commands             # 启动与 graceful shutdown
│   ├── config               # 应用配置聚合
│   ├── handlers             # HTTP handler 注册层
│   ├── logics               # 业务逻辑层
│   ├── migration            # 应用级 migration 注册
│   └── router               # 路由装配
├── core                     # runtime 核心能力
│   ├── mcp                  # MCP 抽象与 adapter
│   ├── memory               # 会话压缩记忆
│   ├── providers            # LLM 抽象与 provider client
│   ├── tasks                # durable task manager、event store、runner
│   ├── tools                # tool registry 与 builtin tools
│   └── types                # provider/tool/cost 等通用类型
├── pkg                      # 基础设施层
│   ├── db                   # SQLite 初始化
│   ├── log                  # Zap 与 GORM logger
│   ├── migrate              # 数据迁移框架
│   └── rest                 # Gin REST 封装
├── conf                     # 配置文件
├── data                     # SQLite 数据目录
├── logs                     # 日志目录
└── docs                     # 说明文档与 swagger 产物
```

## 快速开始

### 安装依赖

```bash
go mod download
go mod tidy
```

### 构建示例应用

```bash
go build -o bin/example_agent ./cmd/example_agent
```

### 运行示例应用

```bash
./bin/example_agent -config conf/app.yaml
```

启动后可直接在浏览器访问 Swagger UI：

```text
http://127.0.0.1:18080/api/v1/swagger/index.html
```

### 运行测试

```bash
go test ./...
go build ./cmd/...
go list ./...
```

### 生成 Swagger 文档

```bash
swag init -g "cmd/example_agent/main.go" -o "docs/swagger" --outputTypes json,yaml --parseDependency --parseInternal
```


生成产物位于 `docs/swagger/swagger.json` 与 `docs/swagger/swagger.yaml`，Swagger UI 页面会直接读取其中的 `swagger.json`。

## 配置说明

- 配置入口是 `conf/app.yaml`
- 加载 YAML 前会先执行环境变量展开，因此可以直接写 `${ENV_NAME}`
- provider 配置当前使用 `apiKey` 字段
- `pkg/rest.Config` 当前字段名是 `staticPaths`，`conf/app.yaml` 也已经使用同名字段

## 开发约定

- 依赖方向保持单向：`app -> core -> pkg`
- `pkg` 不得导入 `core` 或 `app`
- `cmd` 与 `app/commands` 只负责装配、启动、信号处理，不承载核心业务
- LLM、tool、MCP 抽象统一放在 `core`
- durable task manager 与后台任务运行时统一放在 `core/tasks`
- 数据库、日志、REST、migration 等基础设施统一放在 `pkg`
- 数据库结构变更要在 `app/migration` 注册 migration，不要只改 model

## 验证现状（2026-03-15）

- `go list ./...`：通过
- `go build ./cmd/...`：通过
- `go test ./...`：通过

更多仓库内协作约定请查看 `AGENTS.md`。
