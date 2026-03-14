# Agent Runtime - AI 编码指南

## 项目状态快照（2026-03-14）
- 本仓库已实现核心目录结构：`cmd/`、`core/`、`pkg/`、`app/` 均已创建并填充代码
- 项目结构为预定义，但可能在后续迭代中调整，以 `README.md` 为权威参考
- 固定模块路径：`github.com/EquentR/agent_runtime`，所有导入必须遵循此路径

## 架构地图（基于 `README.md`）
- **`cmd/`**：命令行入口，负责配置加载、依赖初始化和服务启动
- **`core/`**：运行时核心层（`agent`、`providers`、`tools`、`mcp`、`rag`、`types`），实现智能体循环、内存管理、工具调用、成本跟踪
- **`pkg/`**：可复用基础设施层（`db`、`rest`、`log`、`migrate`），被 core 和 app 共享
- **`app/`**：产品层实现（`commands`、`config`、`handlers`、`logics`、`router`），基于 core 能力构建具体应用
- **依赖方向**：必须单向流动 `app -> core -> pkg`，禁止 `pkg` 导入 `core` 或 `app`

## 编码前需要验证的事项
- 确认是在现有文件中实现，还是创建 `README.md` 中规划的新目录
- 创建新包时，严格遵循 README 中的命名，除非用户明确要求结构调整
- 每个新包的所有权清晰界定：是运行时核心、基础设施库还是应用层组装代码

## 开发者工作流程（已验证）
- `go test ./...` 和 `go list ./...` 当前可正常运行已实现的包
- 添加代码后，优先使用包级测试：`go test ./core/...`，然后运行全量 `go test ./...`
- 构建命令：`go build ./cmd/...` 用于验证命令行入口的编译

## 本项目特定的惯例
- 文档（包括 README、注释）采用中英混用风格，优先用中文描述功能，技术术语保留英文
- `cmd` 和 `app/commands` 层保持精简：只含启动和配置逻辑，核心业务逻辑不在此层
- 所有 LLM provider 和工具的抽象接口置于 `core/providers` 和 `core/tools`，禁止在 app 层耦合具体实现
- 跨模块基础设施（日志、HTTP 封装、数据库、迁移）集中放在 `pkg/` 下，不放在 core

## 需要保留的集成点和依赖
- **数据库**：SQLite（via `glebarez/sqlite` 和 `glebarez/go-sqlite`）在 `pkg/db` 和 `pkg/migrate`
- **Web 框架**：Gin（via `gin-gonic/gin`）在 `pkg/rest`
- **日志**：Zap（需补充）在 `pkg/log`
- **LLM 集成**：OpenAI、Google Gemini 等在 `core/providers`，各自隐藏于接口之后
- **RAG 和 MCP**：独立的 `core/rag` 和 `core/mcp` 模块，可能在后续版本合并

## PR/变更检查清单
- 引入新的顶层目录时，参考 `README.md` 的架构描述
- 发现已实现代码与 README 规划有偏差时，更新相应的文档和注释
- 如果故意偏离 README 结构，在 PR 描述中说明原因和权衡

