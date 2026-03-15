# Agent Runtime - AI 编码指南

## 项目快照（2026-03-15）
- 模块路径固定为 `github.com/EquentR/agent_runtime`，新增导入必须遵循该路径
- 当前唯一命令行入口是 `cmd/example_agent`，可启动一个基于 Gin + SQLite 的示例应用
- 现阶段真正已落地的核心能力集中在 `core/providers`、`core/tools`、`core/mcp`、`core/memory`、`core/types` 与 `pkg/*`
- `README.md` 仍保留一部分规划性描述；遇到文档和代码不一致时，优先以当前目录、导入关系和可执行验证结果为准

## 当前目录地图（以代码实况为准）
- `cmd/example_agent`：读取 `conf/app.yaml`，做环境变量展开，装配并启动应用
- `app/config`：聚合 `server`、`sqlite`、`log`、`llmProvider`、`embeddingProvider`、`rerankProvider` 配置
- `app/commands`：负责服务启动和 graceful shutdown，不承载核心业务逻辑
- `app/router`：路由装配、静态资源注册、handler 汇总入口
- `app/handlers`：HTTP handler 注册层；当前包含示例接口
- `app/logics`：业务逻辑层；当前仅有 `ExampleLogic`
- `app/migration`：应用级数据库迁移定义与 bootstrap
- `core/providers`：统一 LLM 抽象和 provider adapter
  - `core/providers/types`：`ChatRequest`、`ChatResponse`、`Stream`、reasoning/tool call/multimodal 结构
  - `core/providers/client/google`：Gemini adapter
  - `core/providers/client/openai_completions`：OpenAI-compatible chat completions adapter
  - `core/providers/client/openai_responses`：OpenAI Responses API adapter
  - `core/providers/tools`：本地 token counter 与异步计数能力
- `core/tools`：工具注册表、本地工具定义、MCP tools/prompts 包装
- `core/tools/builtin`：文件、命令、HTTP、web search 等内建工具；文件操作受工作目录限制
- `core/mcp`：MCP 抽象、配置与 `mark3labs` adapter
- `core/memory`：会话压缩记忆、token budget 分配与 summary 注入
- `core/types`：provider 配置、模型成本、tool schema 等通用类型
- `pkg/db`、`pkg/log`、`pkg/migrate`、`pkg/rest`：共享基础设施层
- `conf`、`data`、`logs`：运行时配置、SQLite 数据文件、日志输出目录

## 与 README 的现状差异
- `core/memory` 已实现并有测试，不应再按“未落地能力”处理
- `app/migration` 已进入实际启动链路，负责数据库版本升级
- `core/agent`、`core/rag` 当前目录尚未落地；不要按 README 预设它们已存在
- 涉及架构调整时，先看真实目录和 `go list ./...` 输出，再决定放在哪一层

## 当前启动链路
- `cmd/example_agent/main.go`：打印版本信息，读取 YAML 配置并做 `os.ExpandEnv`
- `app/commands/serve.go`：初始化日志、SQLite、migration、Gin engine 和 router
- `app/router/init.go`：注册 `handlers.NewExampleHandler()`
- `app/handlers/example_handler.go`：暴露示例接口并调用 `app/logics/example_logic.go`
- 当前示例应用更像 runtime skeleton，不是完整的 agent loop 产品

## 依赖边界
- 继续保持单向依赖：`app -> core -> pkg`
- `pkg` 不得导入 `core` 或 `app`
- `cmd` 与 `app/commands` 只负责装配、启动、信号处理，不写核心业务
- LLM、tool、MCP 抽象统一放在 `core`；具体 provider/adapter 细节隐藏在对应子包内
- 数据库、日志、REST 封装等跨模块基础设施统一放在 `pkg`

## 关键依赖
- Web：`github.com/gin-gonic/gin`、`github.com/soulteary/gin-static`
- DB：`github.com/glebarez/sqlite`、`gorm.io/gorm`
- Logging：`go.uber.org/zap`、`gopkg.in/natefinch/lumberjack.v2`
- LLM：`google.golang.org/genai`、`github.com/sashabaranov/go-openai`、`github.com/openai/openai-go`
- MCP：`github.com/mark3labs/mcp-go`
- Token 计数：`github.com/pkoukk/tiktoken-go`

## 配置与编码约定
- 配置入口是 `conf/app.yaml`，反序列化前会先展开环境变量
- `app/config.Config` 已预留 `llmProvider`、`embeddingProvider`、`rerankProvider`，即使当前示例 app 还未完整消费这些配置
- `core/types/BaseProvider` 当前 YAML tag 使用 `apiKey`；修改 provider 配置或测试时，要同步检查 tag、示例配置和测试数据
- `pkg/rest.Config` 当前字段是 `staticPaths`，而 `conf/app.yaml` 示例仍写成 `staticPath`；动到静态资源或配置加载逻辑时先统一这一处
- 文档和注释保持中英混用风格：中文解释功能，术语保留 English

## 开发时优先查看的文件
- 启动与配置：`cmd/example_agent/main.go`、`app/config/app.go`、`app/commands/serve.go`、`conf/app.yaml`
- 路由与 API：`app/router/init.go`、`app/router/types.go`、`app/handlers/example_handler.go`
- 数据迁移：`app/migration/init.go`、`app/migration/define.go`、`pkg/migrate/*`
- Provider 抽象：`core/providers/types/*`、`core/types/provider_config.go`
- Tool/MCP：`core/tools/register.go`、`core/tools/builtin/README.md`、`core/mcp/README.md`
- 基础设施：`pkg/db/sqlite.go`、`pkg/log/logger.go`、`pkg/rest/*`

## 验证现状（2026-03-15 已验证）
- `go list ./...`：通过
- `go build ./cmd/...`：通过
- `go test ./...`：通过
- 当前全量包测试可作为默认验收命令；改动较小时可先跑包级测试，再补全量测试

## 提交前检查
- 改 provider/模型配置时，同时检查 `core/types/provider_config.go`、对应测试和 `conf` 示例
- 改 HTTP 启动链时，同时检查 `cmd/example_agent`、`app/commands`、`app/router`、`pkg/rest`
- 改数据库结构时，在 `app/migration` 注册 migration，不要只改 model
- 改工具系统时，优先扩展 `core/tools` / `core/mcp`，避免把 adapter 细节泄漏到 `app`
- 如果变更触及 README 的规划与现实差异，顺手更新相关文档，避免再次漂移
