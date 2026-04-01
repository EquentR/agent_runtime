# Agent Runtime - 面向编码代理的仓库指南

## 适用范围
- 本文件适用于 `github.com/EquentR/agent_runtime` 根目录下的整个仓库。
- 当文档与代码不一致时，以代码为准。
- 保持现有分层：`app -> core -> pkg`。
- 产品装配放在 `app`，可复用 runtime 能力放在 `core`，共享基础设施放在 `pkg`。
- 本仓库的代理协作文档默认使用中文；后续更新 `AGENTS.md` 时继续使用中文。

## 外部规则文件
- 未发现 `.cursor/rules/` 目录。
- 未发现 `.cursorrules` 文件。
- 未发现 `.github/copilot-instructions.md` 文件。
- 如果后续新增这些文件，需要将其中规则并入本指南，并以作用域更小、路径更深的规则为更高优先级。

## 仓库结构
- `cmd/example_agent`：可运行的参考二进制入口。
- `app/commands`：启动装配、依赖构建、服务初始化。
- `app/config`：应用级配置结构与默认值解析。
- `app/router`：路由初始化与依赖下传。
- `app/handlers`：HTTP 请求解析、响应整形、鉴权检查与路由注册。
- `app/logics`：不应放在 handler 中的应用层业务逻辑。
- `app/migration`：迁移注册与启动引导。
- `core/agent`：agent 执行器、runner、流式事件、会话持久化。
- `core/tasks`：任务存储、管理器、worker pool、事件、重试、取消、挂起恢复。
- `core/prompt`：提示词文档、绑定与解析器。
- `core/approvals`：审批记录持久化。
- `core/interactions`：人工交互记录持久化。
- `core/audit`：任务运行审计与事件轨迹。
- `core/tools`：本地工具注册表与 MCP 接入边界。
- `core/providers`：模型抽象与 provider 专属客户端。
- `core/memory`：上下文预算与记忆压缩。
- `core/mcp`：MCP 抽象边界与 adapter。
- `pkg`：数据库、日志、迁移、REST 等通用基础设施。
- `webapp`：Vue 3 + TypeScript + Vite 前端。

## 建议优先阅读
- 启动链：`cmd/example_agent/main.go` -> `app/commands/serve.go` -> `app/router/init.go`。
- 主配置：`conf/app.yaml`。
- 应用装配：`app/commands/serve.go`、`app/config/app.go`。
- Handler 模式：`app/handlers/task_handler.go`、`app/handlers/interaction_handler.go`、`app/handlers/approval_handler.go`、`app/handlers/prompt_handler.go`。
- Agent runtime 主路径：`core/agent/executor.go`、`core/agent/stream.go`、`core/agent/conversation_store.go`。
- 任务系统：`core/tasks/manager.go`、`core/tasks/store.go`、`core/tasks/runtime.go`。
- 人工介入链路：`core/approvals/store.go`、`core/interactions/store.go`、`core/tools/builtin/ask_user.go`。
- 审计：`core/audit/store.go`。
- Tools 与 MCP：`core/tools/register.go`、`core/tools/builtin/register.go`、`core/mcp/README.md`。
- 前端 API 边界：`webapp/src/lib/api.ts`、`webapp/src/types/api.ts`、`webapp/src/lib/transcript.ts`。

## 环境与前置条件
- Go 工具链版本：`go 1.25.0`，见 `go.mod`。
- 前端包管理器：`pnpm`，见 `webapp/package.json`。
- 当前环境下前端类型检查可运行：`pnpm --dir webapp exec vue-tsc -b`。
- Vite 8 与 Vitest 4 需要 Node `20.19+` 或 `22.12+`。

## 已验证命令
- 全仓库 Go 包列表：`go list ./...`
- 全仓库 Go 构建：`go build ./cmd/...`
- 全仓库 Go 测试：`go test ./...`
- 单个 Go 测试示例：`go test ./app/handlers -run TestSwaggerUIRoutesExposeHTMLAndGeneratedDocs`
- `core` 下单个 Go 测试示例：`go test ./core/tasks -run TestStoreCreateTaskPersistsQueuedSnapshotAndCreatedEvent`
- 前端类型检查：`pnpm --dir webapp exec vue-tsc -b`

## 构建、测试与检查命令
- 安装 Go 依赖：`go mod download`
- 在有意更新依赖时整理 Go 依赖：`go mod tidy`
- 列出所有 Go 包：`go list ./...`
- 构建示例二进制：`go build ./cmd/...`
- 运行所有 Go 测试：`go test ./...`
- 运行单个 Go 包测试：`go test ./app/handlers`
- 按精确测试名运行单个 Go 测试：`go test ./app/handlers -run '^TestSwaggerUIRoutesExposeHTMLAndGeneratedDocs$'`
- 运行单个 Go 子测试：`go test ./path/to/pkg -run '^TestName$/SubtestName$'`
- 对某个 Go 包执行覆盖率测试：`go test ./core/agent -cover`
- 安装前端依赖：`pnpm --dir webapp install`
- 前端类型检查：`pnpm --dir webapp exec vue-tsc -b`
- 前端构建：`pnpm --dir webapp build`
- 前端测试：`pnpm --dir webapp test`
- 运行单个 Vitest 文件：`pnpm --dir webapp exec vitest run src/lib/api.spec.ts`
- 按标题运行单个 Vitest 用例：`pnpm --dir webapp exec vitest run src/lib/api.spec.ts -t "includes role from backend auth payloads"`

## Lint 与格式化现状
- 仓库中没有单独的 Go lint 脚本。
- 仓库根目录没有发现 `golangci-lint`、ESLint 或 Prettier 配置。
- Go 代码以 `gofmt` 为准，并用 `go test ./...` 做基本校验。
- 前端严格类型检查由 `webapp/tsconfig.app.json` 控制。
- 在仓库新增正式 lint 配置之前，把 `pnpm --dir webapp exec vue-tsc -b` 加上相关 Vitest 覆盖视为前端质量门槛。

## 架构规则
- 不要发明新的启动路径；沿用现有启动链工作。
- 应用配置从 `conf/app.yaml` 加载；`cmd/example_agent/main.go` 会在 YAML 反序列化前展开环境变量。
- 新增运行时依赖时，在 `app/commands/serve.go` 完成装配。
- 依赖通过 `app/router/deps.go` 向下传递，不要新增全局变量。
- 新 handler 统一在 `app/router/init.go` 中集中注册，不要在 `serve.go` 临时挂路由。
- 面向任务的后台执行逻辑留在 `core/tasks` 流程中，不要在 handler 或 view 层重造状态机。
- 会话持久化统一放在 `core/agent/conversation_store.go`，不要在 `app` 再造一套存储。
- 审批、交互、审计分别复用 `core/approvals`、`core/interactions`、`core/audit`，不要散落成局部表或局部日志。
- provider 专属 SDK 细节留在 `core/providers/client/*` 或 `core/mcp`，不要泄漏到 `app`。
- 提示词分发逻辑留在 `core/prompt`，不要在 handler 或前端里手写路由分发规则。

## HTTP 与 Handler 约定
- 路由通过带 `Register` 方法的 handler 类型统一注册。
- HTTP 入参解析、响应整形、鉴权检查和状态码映射放在 handler 层。
- 非平凡业务逻辑下沉到 `app/logics` 或 `core` 包。
- 优先复用 `pkg/rest` 包装，尤其是 `resp.HandlerWrapper(...)`，不要重复写原始 Gin 样板代码。
- Swagger 注解紧贴对应 handler 方法维护。
- 如果请求或响应结构发生变化，重新生成 `docs/swagger/*`，不要手改生成产物。
- 新增等待态接口时，先确认其是否应复用现有 `tasks`、`approvals`、`interactions` 路由边界，而不是直接再开新资源。

## 数据库与迁移规则
- 数据库 schema 变更必须在 `app/migration/init.go` 与 `app/migration/define.go` 中注册。
- 不要只修改 model 结构体并假设迁移会自动发生。
- 优先复用 `pkg/db`、`pkg/migrate`、`pkg/log`、`pkg/rest` 中已有基础设施，不要重复造轮子。
- 数据库访问使用 `db.WithContext(ctx)` 或等效的上下文感知调用。
- 如果改动涉及任务、审批、交互、审计或提示词表结构，补齐对应 store 测试或迁移测试。

## Go 代码风格
- 以 `gofmt` 输出为准，不要手工整理导入顺序或缩进。
- import 分组遵循 `gofmt` 结果：标准库优先，其后是第三方与内部包。
- import 仅在需要消歧或明显提升可读性时才起别名，例如 `coreagent`、`coretasks`、`resp`。
- 构造函数优先命名为 `NewX`。
- 对外公开的类型和函数使用 `PascalCase`，包内内部实现使用 `camelCase`。
- 请求和响应 DTO 命名要明确，例如 `CreateTaskRequest`、`PromptBindingInput`、`InteractionResponseInput`。
- 使用描述性命名；除极小局部作用域外，避免一字母变量名。
- 优先使用 guard clause 和早返回，避免过深嵌套。
- 依赖缺失要尽早校验，并返回清晰错误，例如 `fmt.Errorf("task manager is not configured")`。
- 给上抛错误增加上下文时，使用 `%w` 包装。
- 领域错误优先使用 sentinel error，并通过 `errors.Is(...)` 判定。
- 领域错误到 HTTP 状态码的映射放在 handler 层，不要下沉到更底层。
- 持久化时间优先使用 `time.Now().UTC()`。
- 对用户输入在边界处用 `strings.TrimSpace` 做清洗。
- 除非现有包本身已经采用该模式，否则不要新增可变的 package 级全局状态。
- 为导出符号和非显然行为补充有价值的注释，但不要写噪音注释。

## Go 测试约定
- 测试文件与被测包放在一起，命名为 `*_test.go`。
- 测试名通常较长且语义明确，沿用 `TestThingDoesSpecificBehavior` 风格。
- 断言优先使用 `t.Fatalf` 或 `t.Fatal`，并在需要时同时给出 got 与 want。
- handler 测试优先使用 `httptest`，数据库相关测试尽量使用真实的内存或临时数据库。
- 测试关注可观察行为，而不是内部实现细节。
- 如果改动了 handler 且影响接口契约，应同步补充或更新 Swagger 相关测试。
- 如果改动了任务等待态、审批流或人工交互链路，优先补充 manager / executor / store 的联动测试。

## 前端代码风格
- 使用 Vue 单文件组件，并采用 `<script setup lang="ts">`。
- 仅类型导入优先使用 `import type`。
- 领域类型放在 `webapp/src/types/*`，归一化和边界处理逻辑放在 `webapp/src/lib/*`。
- 面向 API 的结构优先使用 `interface` 和显式联合类型。
- 代码内部命名使用 `camelCase`；后端载荷字段保持 `snake_case`，并在边界层做归一化。
- 优先使用小而明确的归一化函数，例如 `normalizeConversationMessage`，避免在组件里临时散落兼容逻辑。
- `ref`、`computed`、`watch` 的状态名保持语义清晰。
- 路由鉴权与角色控制集中放在 `webapp/src/router/index.ts`，不要散落到多个 view 中。
- 转录流拼装和审批/交互消息注入集中放在 `webapp/src/lib/transcript.ts`，不要在多个组件里复制流事件处理。
- 保持 `webapp/src` 下现有的相对路径 import 风格。
- 除非用户明确要求，不要额外引入新的 formatter 或 linter 配置。

## 前端测试约定
- 前端测试使用 Vitest 与 Vue Test Utils。
- 测试文件紧邻源码，命名为 `*.spec.ts`。
- 优先用 `describe` 按功能分组，并使用可读性强的 `it(...)` 标题。
- API 模块优先通过 `vi.mock(...)` 模拟，并在 `beforeEach` 中重置 mock。
- 涉及路由的视图测试使用 `createMemoryHistory()`，并等待 `router.isReady()`。
- 如果改动聊天流、审批记录或人工问题展示，优先补充 `MessageList.spec.ts`、`ChatView.spec.ts` 或 `transcript.spec.ts`。

## 代理工作建议
- 修改前先阅读最邻近、最相关的代码路径，并遵循周边既有写法。
- 优先做窄而准的改动，而不是大范围重构。
- 不要把第三方 MCP SDK 细节泄漏到 `app` 层。
- 不要从零散启动代码中注册路由；保持集中注册。
- 除非你是在有意重新生成，否则不要手改 Swagger 生成文件。
- 如果改动同时影响后端契约和前端消费端，请在同一轮里一起更新。
- 如果改动了任务等待原因、审批状态或交互状态，确认前端类型与转录归一化逻辑是否也需要同步更新。

## 交付前检查清单
- 先运行聚焦测试，再补全更大范围的校验。
- 对后端改动，默认最终校验命令是 `go test ./...`、`go build ./cmd/...`、`go list ./...`。
- 对纯前端改动，至少运行 `pnpm --dir webapp exec vue-tsc -b` 与相关 Vitest 命令。
- 如果前端 `build` 或 `test` 因 Node 18 失败，应明确说明需要升级 Node，而不是用临时方案掩盖问题。
