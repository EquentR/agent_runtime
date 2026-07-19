# Ice Art 自升级实施计划

**目标：** 按 `docs/superpowers/specs/2026-07-19-self-update-design.md` 完成 Windows/Linux 原生自升级、系统服务安装、管理员升级界面，以及 Docker/macOS/source 构建的更新提示。

**实施原则：** 主 agent 负责全部实现；每阶段先写失败测试再实现。固定后台 reviewer 使用 `gpt-5.6-sol medium` 审查阶段差异。阻塞性和高风险问题立即修复；不阻塞且低重要度的问题记录到最终风险清单。

## 阶段 1：发行信任与 Release 客户端

主要改动：发行身份与 `build-info.json`、Ed25519 签名产物、稳定版 Release/ETag 客户端、镜像规则、SemVer、资产选择、安全下载与 zip/tar 解压。

验收标准：

- 单元测试覆盖签名、哈希、平台资产、缓存、镜像 Token 隔离和压缩包路径逃逸。
- `go test ./core/updater ./scripts/releasepack` 通过。
- Release workflow 可生成并独立验证签名清单与包内构建信息。
- reviewer 无未解决的阻塞性问题。

## 阶段 2：状态机、备份、合并与直接运行 helper

主要改动：持久化 operation/journal、跨进程锁、精简/完整备份、保留策略、发行模板智能合并、一次性 helper 任务、直接运行替换、健康检查和自动/主动回滚。

验收标准：

- 临时安装目录集成测试覆盖成功升级、备份失败、启动失败回滚、未完成事务恢复。
- 首次升级不覆盖用户文件，后续只更新未修改的官方模板。
- helper 拒绝过期、越界、未签名或与安装根不匹配的任务。
- `go test ./core/updater` 通过，reviewer 无未解决的阻塞性问题。

## 阶段 3：维护排空、优雅关闭与系统服务

主要改动：应用维护 coordinator、写请求 503、任务排空与强制中断、可等待的关闭流程、Windows Service dispatcher/SCM 控制、systemd 主服务与 path/oneshot helper、PowerShell/Bash 安装脚本。

验收标准：

- 维护模式请求矩阵和任务排空测试通过。
- Windows/Linux 平台代码均可交叉编译。
- 安装脚本 dry-run、自定义安装目录、幂等安装、保留数据卸载和 purge 防护测试通过。
- reviewer 无未解决的阻塞性问题。

## 阶段 4：管理员 API、二次认证与审计

主要改动：状态/检查/授权/安装/回滚 API，管理员密码复核，一次性操作 Token，同源/CSRF 校验，并发 operation 防护，后台操作审计与恢复状态输出。

验收标准：

- Handler 测试覆盖权限矩阵、密码错误、Token 绑定/过期/单次消费、CSRF、409/503 和审计脱敏。
- 路由集中注册，Swagger 契约同步生成并通过相关测试。
- `go test ./app/handlers ./app/router ./app/logics` 通过。
- reviewer 无未解决的阻塞性问题。

## 阶段 5：管理员升级界面

主要改动：前端 API/类型、后台路由和导航徽标、系统升级页、Release Notes 弹窗、备份/密码/强制确认、状态轮询、服务重启重连和只提示模式。

验收标准：

- Vitest 覆盖管理员导航、签名状态、Markdown 安全渲染、确认流程、重连、失败/回滚和 Docker/macOS/source 提示。
- 页面在桌面和移动视口无重叠、溢出或不可操作控件。
- `pnpm --dir webapp exec vue-tsc -b` 与相关 Vitest 通过。
- reviewer 无未解决的阻塞性问题。

## 阶段 6：集成、发布与收口

主要改动：Windows/Linux CI、端到端升级验证器、恢复命令和运维文档；处理 reviewer 汇总问题并记录延期项。

验收标准：

- `go test ./...`、`go build ./cmd/...`、`go list ./...` 全部通过。
- Windows 和 Linux 目标可交叉编译；发布包校验器通过。
- `pnpm --dir webapp exec vue-tsc -b`、全量 Vitest 和前端 build 通过。
- 使用本地临时安装目录演练一次成功升级和一次自动回滚。
- 对照 spec 完成逐项审计，未完成项仅允许为用户定义的非阻塞低重要度延期项，并在最终结果中明确列出。
