# Ice Art 自升级与系统服务设计

## 1. 背景与目标

Ice Art 已通过 GitHub Releases 发布 Windows、Linux 和 macOS 的 amd64/arm64 原生包，并通过 GHCR 发布 Docker 镜像。Release 二进制使用 ldflags 注入版本和 commit，前端资源嵌入 Go 二进制。

本设计为管理员提供浏览器内的版本检查、Release Notes 查看和一键升级能力。Windows 与 Linux 原生发行包支持下载、验证、备份、替换、重启、健康检查和失败回滚；Docker、macOS 和源码构建只显示更新提示。

同时提供官方 PowerShell 和 Bash 脚本，将便携发行目录注册为 Windows Service 或 systemd 服务，使直接运行和系统服务两种部署方式都能使用同一升级流程。

## 2. 成功标准

- 服务启动后检查一次稳定版 Release，之后每小时检查一次，并使用 ETag 与本地缓存控制 GitHub 请求量。
- 只有已登录且状态正常的管理员可以查看升级后台、触发检查、安装和回滚。
- 安装包必须同时通过 Ed25519 签名和 SHA-256 校验；缺少或未通过任一校验时不得安装。
- Windows 和 Linux 的直接运行、Windows Service、systemd 四种场景都能完成升级并自动恢复服务。
- 升级前默认创建精简备份，管理员可以选择完整备份。
- 新版本未在 90 秒内通过目标版本健康检查时，自动恢复旧二进制、数据库、配置和本次修改的模板文件。
- Docker、macOS、开发构建和来源不明的构建只显示 Release 与更新指引，不执行自替换。
- 升级行为可由现有后台审计和独立文件日志共同追溯。

## 3. 范围与非目标

### 3.1 本期范围

- 固定检查 `EquentR/agent_runtime` 的最新稳定 Release。
- Windows amd64/arm64 和 Linux amd64/arm64 原生包自升级。
- Docker、macOS 和 source/dev 构建更新提示。
- Windows Service 生命周期支持和 systemd unit。
- PowerShell/Bash 服务安装、卸载、启动、停止和状态查询脚本。
- 最近一次升级前备份的主动回滚。
- GitHub 标准代理、可选 Token、API 镜像和资产下载镜像。

### 3.2 非目标

- 自动静默安装。
- prerelease/beta 更新通道。
- Docker 容器内替换自身。
- macOS 自升级。
- 任意历史版本降级。
- MSI、deb、rpm 或图形安装器。
- 自动解决用户修改过的配置或 workspace 文件冲突。

## 4. 总体架构

采用“单二进制临时辅助模式”。主服务负责检查、下载、验证、维护排空和生成升级任务；切换阶段由平台适配器以隐藏的 `update-helper` 子命令运行同一个 `ice_art` 二进制。直接运行和 Windows Service 从升级临时目录启动 helper；systemd 由安装脚本注册的按需 oneshot unit 启动 helper。helper 在主进程退出后完成备份、文件替换、启动、健康检查和回滚，完成后退出，不增加常驻 updater 进程。

新增模块边界：

- `core/updater`：Release 客户端、版本比较、签名与哈希验证、资产选择、安全解压、备份计划、发行清单合并、状态机和升级日志。
- `core/updater/platform`：直接运行、Windows Service、systemd 的进程与服务控制适配；平台代码通过 Go build constraints 隔离。
- `app/commands`：注入当前版本、commit、发行模式、配置、数据库关闭钩子、任务排空和运行模式检测。
- `app/router`：通过现有依赖结构下传 updater，不增加全局业务依赖。
- `app/handlers`：管理员升级 API、二次认证、状态码映射和 Swagger 注解。
- `webapp`：系统升级页面、导航徽标、Release 弹窗、确认流程和重启重连。
- `scripts`：服务安装脚本、unit 模板和发布签名工具。

`cmd/ice_art` 增加由 ldflags 注入的 `Distribution`：

- `release`：官方原生发行包。
- `container`：官方 Docker 镜像。
- `source`：默认值，表示源码或开发构建。

是否允许安装同时取决于 `Distribution == release`、`runtime.GOOS`、`runtime.GOARCH`、运行模式和目录权限。不得只依赖 `/.dockerenv` 等环境特征判断发行身份。

## 5. Release 来源与可信验证

### 5.1 发布产物

现有 Release 资产名保持不变：

- `ice_art_windows_amd64.zip`
- `ice_art_windows_arm64.zip`
- `ice_art_linux_amd64.tar.gz`
- `ice_art_linux_arm64.tar.gz`
- `ice_art_darwin_amd64.tar.gz`
- `ice_art_darwin_arm64.tar.gz`
- `SHA256SUMS`
- `SHA256SUMS.sig`

每个归档内部还包含 `build-info.json`，记录 version、commit、distribution、GOOS 和 GOARCH。归档本身的哈希位于已签名的 `SHA256SUMS` 中，因此 updater 可以在不执行 staged 二进制的情况下验证包身份。

Release workflow 使用仓库 Secret 中的 Ed25519 私钥签名 `SHA256SUMS`。程序和仓库只保存公钥。密钥轮换通过在程序中暂时信任旧、新两个带 ID 的公钥完成；至少发布一个双公钥过渡版本后才能移除旧公钥。

### 5.2 检查与验证顺序

1. 请求固定仓库的 latest release，忽略 draft 和 prerelease。
2. 校验 tag 为规范 SemVer；仅当目标版本高于当前版本时显示可升级。
3. 按当前 OS/架构选择唯一资产。
4. 下载 `SHA256SUMS` 和 `SHA256SUMS.sig`，先用内置公钥验证签名。
5. 从已验证清单中读取目标资产哈希，再校验下载文件 SHA-256。
6. 安全解压到同一文件系统内的暂存目录。
7. 读取归档内 `build-info.json`，校验包结构、二进制平台/架构、distribution 和目标版本，之后才允许进入维护阶段。

安全解压拒绝绝对路径、父目录跳转、符号链接、硬链接、设备文件、重复目标路径、超限文件数量、超限单文件和超限总解压体积。所有写入目标必须在 canonicalized 暂存根目录内。

### 5.3 网络入口

默认使用 GitHub 官方 API 和资产地址，并支持：

- Go 标准库识别的 `HTTP_PROXY`、`HTTPS_PROXY` 和 `NO_PROXY`。
- 通过配置指定名称的可选 Token 环境变量，Token 不进入数据库、日志或前端响应，并且只发送给官方 `api.github.com`，不得转发给自定义 API 或资产镜像。
- 可配置 GitHub API base URL。
- 可配置资产下载 URL 模板。

镜像只能改变网络入口。owner、repository、tag、资产名和签名公钥固定；GitHub 原始页面链接始终由官方仓库和经过校验的 tag 生成。镜像内容未通过官方签名时不得安装。

## 6. 检查调度与缓存

- 服务完成启动后异步检查一次，不阻塞正常启动。
- 此后每小时检查一次；间隔可以通过 YAML 配置调整，但不得小于 15 分钟。
- 使用 ETag/If-None-Match，并将最后一次成功结果原子写入 `data/updates/release-cache.json`。
- GitHub 不可达时保留最近一次成功结果，同时明确标记数据时间和本次检查错误。
- 后台检查失败只记录日志，不改变 readiness，不进入维护模式。
- 手动检查与定时检查共享 singleflight，避免并发请求。

## 7. 升级状态机与数据流

状态机为：

```text
idle -> checking -> available
available -> downloading -> verifying -> staged
staged -> draining -> backing_up -> replacing -> restarting -> health_check -> succeeded
                                                        \-> rolling_back -> rolled_back
任意替换前状态 -> failed
任意替换后不可恢复状态 -> recovery_required
```

完整流程：

1. 管理员查看 Release Notes，选择精简或完整备份，重新输入密码。
2. 服务端验证一次性授权、目标版本、运行模式、目录权限和剩余磁盘空间。
3. 在正常服务期间下载、验证和解压，避免把网络时间计入停机窗口。
4. 写入 owner-only 的 staged 状态和升级任务文件。
5. 进入维护模式，停止创建新任务和其他写操作，等待运行任务最多 5 分钟。
6. 若超时，默认取消升级并退出维护模式；管理员可重新授权并二次确认强制升级。
7. 主进程启动临时 helper，停止后台循环，关闭 listener、任务管理器和数据库连接。
8. helper 确认主进程退出和数据库关闭后创建一致性备份。
9. helper 应用二进制与发行模板变更，然后启动目标版本。
10. helper 使用一次性健康令牌轮询本机服务，最多等待 90 秒。
11. 目标版本完成迁移、任务恢复初始化和监听后返回目标版本与 ready 状态。
12. 成功时提交升级事务并执行保留策略；失败时停止新版本并恢复旧版本与数据。

下载完成后若管理员取消或维护排空失败，staged 文件可以保留到缓存过期，但不得在未重新授权的情况下继续安装。跨进程 operation 使用安装根内的 OS 文件锁和带 generation 的 journal 共同保护；主进程、helper 和新版本启动恢复逻辑在写状态前都必须持有同一把锁，避免 helper 与恢复流程并发操作文件。

## 8. 维护模式与任务排空

维护模式由应用级 coordinator 控制，并在 router 层统一执行：

- 禁止任务创建、设置修改、附件写入、审批响应等所有状态变更，返回 HTTP 503 和稳定错误码 `maintenance_mode`。
- 允许登录态读取、升级状态查询和内部健康检查。
- 新任务停止入队；已经 running 的任务等待最多 5 分钟。
- waiting 状态的审批或提问已经持久化，不阻塞升级。
- 强制升级会取消运行上下文，保持现有任务恢复语义，并在 UI 与审计中记录被中断任务数量。
- 普通用户界面根据 503 错误显示系统维护提示，不把请求失败误报为会话丢失。

维护模式只在本次进程内生效。helper 的升级任务文件承担重启阶段的事务状态；新版本启动后在提交成功前继续拒绝普通写操作。

## 9. 备份、发行清单与配置合并

### 9.1 安装根目录

安装根目录默认为当前可执行文件所在目录。直接解压运行和系统服务都使用自包含布局；安装脚本支持显式指定安装目录。helper 使用 canonical executable path 确认根目录，拒绝来自其他目录或跨文件系统的替换任务。

### 9.2 精简与完整备份

精简备份包含：

- 当前二进制、安装脚本和发行清单。
- `conf/app.yaml`。
- 主进程退出后的 SQLite 一致性副本。
- 本次将被升级器修改的 workspace 模板文件。
- 当前/目标版本、哈希、时间、操作人和恢复清单。

完整备份额外包含 `data/`、`conf/` 和 `workspace/`，但排除日志、下载缓存、暂存目录和已有升级备份。复制不跟随指向根目录外部的链接。确认页面显示预计字节数、预计可用空间和完整备份可能造成的停机时间。

备份写入 `data/updates/backups/<operation-id>`，目录包含不可缺少的 updater ownership marker 和 manifest。自动清理只处理 marker、canonical path 和 manifest 均有效的目录。

默认最多保留 3 次、最长 30 天；数量和天数均可配置。为当前可执行回滚提供依据的最近备份不得在事务未完成时清理。

### 9.3 配置与 workspace 合并

Release 增加发行文件清单，记录 updater 管理文件的相对路径、类型和哈希：

- `conf/app.yaml` 永不覆盖；新默认配置写入 `conf/app.yaml.new`。
- 用户未修改的官方模板自动更新。
- 用户修改过的文件保留，新版写为同目录 `.new` 文件。
- 新增且目标不存在的文件直接添加。
- 新版已删除的官方文件只在当前文件与旧发行哈希相同时移除。
- 第一次升级没有旧清单时，不覆盖任何已存在文件；冲突内容写为 `.new`。

前端已经嵌入二进制，不存在独立前端目录替换步骤。若用户显式配置外部静态目录，升级器视为用户数据，不自动修改。

## 10. helper、进程切换与健康检查

### 10.1 一次性任务

直接运行和 Windows Service 模式下，主进程把当前二进制复制到 `data/updates/helpers/<operation-id>`，从该副本启动隐藏的 `update-helper` 子命令。systemd 模式下，主进程写入固定 pending trigger，由官方安装脚本注册的 oneshot updater unit 从受保护路径执行同一子命令。任务文件仅允许当前服务账户和平台 helper 访问，包含安装根、当前/目标版本、staged 路径、备份模式、原始启动信息、运行模式、主进程 PID 和截止时间。

直接运行和 Windows Service 使用通过匿名管道传递的一次性随机密钥计算任务 HMAC；密钥不写入任务文件、命令行或日志。systemd oneshot 由 root 启动，任务真实性依赖 owner-only pending 目录、OS 文件锁、固定安装根和已签名发行资产；helper 的全部写入目标仍须由硬编码规则推导，不能由任务文件提供任意路径。所有模式都校验任务未过期、PID/可执行路径匹配、路径位于允许根目录且目标资产已经通过发行签名。

### 10.2 直接运行

helper 保留必要的 argv、工作目录和非敏感环境，等待旧进程退出后替换文件，再以相同启动方式拉起新版本。Token、密码等敏感值不落盘；确需继承的运行环境只存在于 helper 进程内存。

### 10.3 Linux systemd

安装脚本除主服务外还注册一个不常驻的 `ice-art-update.path` 和 `ice-art-update.service` oneshot unit。低权限主服务只能在固定的 owner-only pending 目录创建 trigger；path unit 仅据此启动 root helper。oneshot unit 将安装根和主 service unit 名作为 root-owned 固定参数传入，任务文件无权覆盖。root helper 从固定安装根读取任务，重新完成签名、哈希、路径、锁和任务期限校验，然后停止该固定主服务、备份、原子替换并启动目标版本。

这种设计不改变主服务默认的 cgroup 清理策略，也不给 `ice-art` 用户通用 sudo 或 systemctl 权限。root helper 只能安装已由内置公钥授权的固定平台资产，且只能写安装根内预定义的程序、备份和模板路径。健康失败时 oneshot helper 停止目标服务、原子恢复旧版本并重新启动。未通过官方脚本安装 updater units 的 systemd 环境只提示更新，不允许一键升级。

### 10.4 Windows Service

`ice_art.exe` 实现 Windows Service dispatcher，并保留普通控制台模式。helper 等待服务主进程完全退出后替换文件，通过 SCM 启动服务。安装脚本只向服务账户授予查询、启动和停止 Ice Art 服务所需的 service DACL 权限。

回滚时 helper 停止目标服务、等待句柄释放、恢复旧文件和数据库，再启动旧服务。针对杀毒软件或索引器造成的短暂文件锁，使用有上限的指数退避；超过上限进入明确的 `recovery_required`，不循环破坏文件。

### 10.5 健康握手

helper 使用 owner-only pending job 中的一次性 token 调用本机健康接口。成功响应必须同时满足：

- token 匹配且未过期。
- 返回版本等于目标 tag。
- 数据库迁移完成。
- task manager 与恢复初始化完成。
- HTTP listener 已经接受请求。

普通健康请求只返回通用状态，不返回 token、文件路径或升级任务内容。

## 11. 服务安装脚本

提供幂等的 `install-service.ps1` 与 `install-service.sh`，动作统一为 `install`、`uninstall`、`start`、`stop`、`restart`、`status` 和 `dry-run`。

共同约定：

- 默认安装根为脚本所在发行目录，允许 `--install-dir` 指定其他绝对路径。
- 工作目录固定为实际安装根，配置参数指向该根下的 `conf/app.yaml`。
- 默认使用低权限账户，也允许显式指定账户或高权限模式。
- 重复安装更新定义但不清理数据。
- 普通 uninstall 只移除服务定义，保留整个安装目录。
- `--purge` 需要额外明确确认，并且只删除脚本记录且验证过的安装根。

Windows 默认使用 `LocalService`，通过目录 ACL 授予安装根所需读写权限，并通过 service DACL 限制为自身服务控制。Linux 默认创建无登录 shell 的 `ice-art` 系统用户，使其拥有安装目录；主 unit 设置明确的 WorkingDirectory、ExecStart、Restart、RestartSec 和停止超时，并安装上述 path/oneshot updater units。所有 unit 文件中的安装根都由脚本做 systemd escaping，不拼接未经验证的 shell 文本。

脚本不下载未验证二进制。若支持从 Release 安装，必须复用同一公钥校验工具和固定官方仓库规则。

## 12. 管理员 API 与二次认证

新增 API：

```text
GET  /api/v1/admin/updates/status
POST /api/v1/admin/updates/check
POST /api/v1/admin/updates/authorize
POST /api/v1/admin/updates/install
POST /api/v1/admin/updates/rollback
```

所有接口使用现有 `RequireAdmin` 中间件。检查和查看不需要二次认证；安装、强制安装和回滚要求：

- 管理员重新输入当前密码。
- `authorize` 返回约 5 分钟有效的一次性 token。
- token 绑定用户、登录 session、操作类型、目标版本或备份 ID。
- token 只能消费一次，服务端仅保存 token 哈希。
- 请求必须满足同源 Origin 校验和 update 专用 CSRF token 校验。
- install、force install、rollback 分别授权，不可互换。

全局只允许一个检查网络请求和一个变更操作。重复请求使用 operation ID 幂等返回已有状态；冲突操作返回 HTTP 409。

状态响应至少包含当前版本、commit、distribution、运行模式、最新 Release、签名状态、检查时间、缓存时间、安装支持状态及原因、活动 operation、备份摘要和最近错误。响应不得包含 Token、代理凭据、真实密码、HMAC、内部健康 token 或不必要的绝对路径。

## 13. 管理员界面

后台新增“系统升级”导航项，并在存在已验证的新版本时显示徽标。页面沿用现有后台工作台布局，包含：

- 当前版本、commit、发行模式与运行方式。
- 最新版本、发布时间、检查时间、缓存状态和签名状态。
- 立即检查、查看 Release、升级三个操作。
- Docker、macOS、source/dev 或服务配置错误时的只提示原因和操作指引。

“查看 Release”使用对话框安全渲染版本、发布时间和 Markdown Release Notes，并提供由固定官方仓库和 tag 生成的 GitHub 链接。Markdown 禁止原始 HTML、脚本、事件属性和危险 URL scheme。

升级确认对话框显示目标版本、备份模式、预计体积、可用空间、运行任务数、维护影响和管理员密码输入。排空超时后单独显示强制升级二次确认，不复用原授权。

升级页每 2 秒轮询 operation 状态。服务重启断线时进入重连状态，不立即显示失败；恢复连接后按持久化 operation 继续展示。普通用户收到维护状态时显示全局维护提示。

主动回滚只针对最近一次升级前备份。对话框明确说明会恢复数据库并丢失备份时间点之后的数据；执行回滚前强制创建一份当前状态安全快照，以便人工恢复。

## 14. 审计、日志与恢复

管理员触发的检查、安装、强制安装和主动回滚写入现有后台操作审计，记录 actor、目标版本、备份模式、运行模式、结果和 operation ID，不记录密码或 Token。定时检查只写普通日志，不制造后台审计噪音。

helper 不能依赖可能被回滚的数据库，因此将原子状态写入 `data/updates/state.json`，并将阶段事件追加到 `data/updates/update.log`。每条记录包含 operation ID、阶段、UTC 时间、版本和清洗后的错误。

启动时发现未完成事务：

- 目标版本已经 ready：提交成功并完成清理。
- 仍是旧版本且文件未替换：标记失败并恢复正常服务。
- 文件已替换但目标版本未 ready：执行自动回滚。
- 文件与 manifest 无法证明属于旧版或目标版：进入 `recovery_required`，停止自动写入并给出本地恢复命令。

提供本地只读状态和显式恢复子命令，供浏览器不可用时诊断。恢复命令必须验证 ownership marker、manifest 和安装根，默认只报告计划；实际执行要求显式确认。

## 15. 配置

应用配置新增 `updates` 段，默认值适用于官方发行包：

```yaml
updates:
  enabled: true
  checkInterval: 1h
  runtimeMode: auto
  serviceName: ""
  githubApiBaseUrl: https://api.github.com
  downloadUrlTemplate: ""
  githubTokenEnv: GITHUB_TOKEN
  drainTimeout: 5m
  healthTimeout: 90s
  backup:
    defaultMode: compact
    retainCount: 3
    retainDays: 30
```

官方 owner/repository、stable 通道、资产命名和签名公钥不可通过运行配置改变。`downloadUrlTemplate` 为空时使用 Release API 返回的官方资产 URL。URL、duration 和保留值在配置加载边界严格校验；非法配置禁用安装但不阻止服务启动，并在管理员状态页显示原因。

运行模式默认自动识别；安装脚本通过 root/Administrator 控制的启动参数显式传入服务模式与服务名，优先于 YAML，但不改写用户的 `conf/app.yaml`。若检测到服务环境但配置、官方 helper 定义或权限不足，只允许检查更新，不允许安装。

## 16. 错误处理规则

- GitHub、镜像或代理错误：保留缓存，服务正常运行。
- Release/tag/资产不合法：显示不可安装原因，不下载或替换。
- 签名或哈希失败：删除 staged 内容，记录安全错误，禁止继续。
- 空间不足或目录不可写：预检失败，不进入维护模式。
- 任务排空超时：退出维护，除非管理员重新授权强制升级。
- 备份失败：不替换任何程序文件，重新启动旧服务。
- 文件替换失败：按 manifest 恢复已修改文件，再启动旧服务。
- 新版本启动、迁移或健康检查失败：自动恢复旧二进制、数据库、配置和模板。
- 自动回滚也失败：进入 `recovery_required`，保留双方文件和完整日志，不继续自动重试破坏性操作。

任何错误都必须保留可操作的阶段、原因和下一步；前端不得只显示笼统的“升级失败”。

## 17. 测试策略

### 17.1 Go 单元测试

- SemVer、stable Release 过滤和发行模式能力判断。
- ETag、缓存过期、singleflight 和镜像 URL 规则。
- Ed25519 正确签名、错误签名、未知 key ID、哈希不匹配。
- zip/tar 路径逃逸、链接、重复路径和解压限额。
- 磁盘估算、备份 manifest、保留策略和安全清理。
- 发行模板未修改、已修改、新增、删除、首次升级冲突规则。
- 状态机合法/非法迁移、原子 journal 和启动恢复判定。
- 二次认证 token 的绑定、过期、单次消费和操作隔离。

### 17.2 Handler 与前端测试

- 未登录、普通用户、禁用管理员和有效管理员的权限矩阵。
- Origin/CSRF、密码错误、并发 operation 和稳定状态码。
- 维护模式下读写请求边界。
- 更新徽标、Release 弹窗、安全 Markdown、备份选择、密码复核、强制确认、重启重连和只提示模式。

### 17.3 集成与平台测试

- 使用假 GitHub 服务、测试 Ed25519 密钥、临时安装根和假健康进程完成成功升级。
- 覆盖下载失败、备份失败、目标启动失败、迁移失败、helper 中断和自动回滚。
- GitHub Actions 增加 Windows 与 Linux updater 测试，分别验证 Windows 文件句柄释放后替换和 Linux 原子 rename。
- 服务安装脚本支持 dry-run；测试自定义安装目录、低权限默认值、重复安装、保留数据卸载和 purge 防护。
- systemd unit 和 Windows Service 定义做确定性生成测试；真实服务生命周期在 Windows/Linux VM 做发布前人工验收。

### 17.4 发布与全仓库验证

- Release workflow 在上传前使用发行公钥独立验证签名和全部资产哈希。
- 运行 `go test ./...`、`go build ./cmd/...` 和 `go list ./...`。
- 运行 `pnpm --dir webapp exec vue-tsc -b`、相关 Vitest、全量 Vitest 和前端构建。
- Windows/Linux 的直接运行和系统服务各验收一次成功升级、一次启动失败回滚和一次主动回滚。

## 18. 实施分解

实施计划应按以下依赖顺序展开：

1. 发行身份、签名产物和安全 Release 客户端。
2. updater 状态机、journal、安全解压、备份与模板合并。
3. 维护 coordinator、优雅关闭和 helper 直接运行切换。
4. Windows Service、systemd 与官方安装脚本。
5. 管理员 API、二次认证和审计。
6. 管理员升级界面与重连体验。
7. 平台集成测试、发布验证和运维恢复文档。

每一阶段先写可观察行为测试，再实现最小功能，并在平台切换能力完成后立即进行 Windows/Linux 实机验证，不把全部风险推迟到最终发布。
