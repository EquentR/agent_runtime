# Agent Runtime - 编码指南

## 适用范围
- 以代码为准。这个仓库已经包含可运行的参考应用，入口在 `cmd/example_agent`，装配层在 `app/*`。
- 保持现有分层：`app -> core -> pkg`。
- 产品装配放在 `app`，可复用 runtime 能力放在 `core`，共享基础设施放在 `pkg`。

## 启动与配置
- 先沿当前启动链阅读和改动，不要额外引入旁路启动方式。顺序参考 `cmd/example_agent/main.go` -> `app/commands/serve.go` -> `app/router/init.go`。
- 应用配置通过 `conf/app.yaml` 加载；`cmd/example_agent/main.go` 会在 YAML 反序列化前先展开环境变量。
- 新增运行时依赖时，先在 `app/commands/serve.go` 装配，再通过 `app/router/deps.go` 往下传，不要直接引入新的全局变量。

## HTTP 与 Handler 模式
- 路由通过带 `Register` 方法的 handler 类型注册。现有例子见 `app/handlers/task_handler.go`、`app/handlers/conversation_handler.go`、`app/handlers/auth_handler.go`。
- HTTP 入参/出参与响应整形留在 handler，非平凡业务逻辑下沉到 `app/logics`。参考 `app/handlers/auth_handler.go` 和 `app/logics/auth_logic.go`。
- 新 handler 统一加入 `app/router/init.go` 的集中注册列表，不要从 `serve.go` 临时注册路由。
- JSON 接口沿用 `pkg/rest` 的包装方式，优先复用 `resp.HandlerWrapper(...)`，不要为新接口重复写一套原始 Gin 样板。

## Tasks、Conversations 与 Agent Run
- 后台执行统一按 task 处理。`core/tasks/manager.go` 和 `app/handlers/task_handler.go` 已定义创建、查询、取消、重试、SSE 订阅这一套模式。
- 多轮对话状态统一持久化到 `core/agent/conversation_store.go`，不要在 `app` 再造一套 conversation 存储。
- `agent.run` 是当前默认执行路径。`core/agent/executor.go` 展示了 provider 解析、memory、tools、task runtime 和 conversation persistence 的组合方式。
- task 涉及 conversation 时，沿用现有权限校验方式，参考 `app/handlers/task_handler.go` 与 `app/handlers/conversation_handler.go`。

## Providers、Tools 与 MCP
- 面向模型的抽象放在 `core/providers/types` 和 `core/types`；provider 专属 client 放在 `core/providers/client/*`。
- provider/model 解析方式以 `app/commands/serve.go` 和 `core/agent/executor.go` 为准；模型目录接口由 `app/handlers/model_catalog_handler.go` 暴露。
- 本地工具通过 `core/tools/register.go` 注册；内建工具放在 `core/tools/builtin/*`，并受 `WorkspaceRoot` 约束。
- 远端 MCP 能力通过 `core/mcp` 和 `core/tools` 的注册辅助接入，不要把第三方 MCP SDK 细节泄漏到 `app`。

## 数据库、迁移与文档
- 数据库结构变更要在 `app/migration/init.go` / `app/migration/define.go` 注册，不要只改 model。
- Swagger 注解跟着 handler 定义走；请求/响应结构变化后，重新生成 `docs/swagger/*`。现有例子见 `app/handlers/task_handler.go`、`app/handlers/conversation_handler.go`、`app/handlers/swagger_handler.go`。
- SQLite、日志、迁移和 REST 基础设施已经分别在 `pkg/db`、`pkg/log`、`pkg/migrate`、`pkg/rest` 中实现；优先扩展这些包，不要在 `app` 或 `core` 里重复造基础设施。

## 优先阅读的文件
- 启动/配置：`cmd/example_agent/main.go`、`app/commands/serve.go`、`conf/app.yaml`
- 路由/API：`app/router/init.go`、`app/handlers/task_handler.go`、`app/handlers/conversation_handler.go`、`app/handlers/auth_handler.go`
- Agent runtime：`core/agent/executor.go`、`core/agent/conversation_store.go`、`core/memory/manager.go`
- 任务系统：`core/tasks/manager.go`、`core/tasks/store.go`
- Providers/Tools/MCP：`core/tools/register.go`、`core/tools/builtin/README.md`、`core/providers/README.md`、`core/mcp/README.md`

## 验证
- 默认仓库校验命令是 `go test ./...`、`go build ./cmd/...` 和 `go list ./...`。
- 小范围改动可以先跑相关包测试，再在交付前补全上述全量校验。
