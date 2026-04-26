# 内建工具

此包定义的 17 个内建工具（built-in tools）为 Agent 运行时提供文件操作、命令执行、网络请求、进程管理和技能加载能力。

## 文件操作工具（8 个）

所有文件类工具的操作范围限制在工作目录内，不支持符号链接和 `..` 路径穿越。

| 工具 | 说明 | 审批模式 |
|------|------|----------|
| `list_files` | 列出目录下的文件和子目录，支持递归与深度限制 | 自动 |
| `read_file` | 读取文件内容，支持行窗口（start_line + line_count），自动识别并拒绝二进制文件 | 自动 |
| `write_file` | 写入文件，支持四种模式：overwrite（覆盖）、append（追加）、insert（指定行插入）、replace_lines（行范围替换） | 自动 |
| `search_file` | 递归搜索目录下文件内容，支持文本和正则表达式匹配 | 自动 |
| `grep_file` | 在单个文件中搜索匹配行，支持正则表达式 | 自动 |
| `delete_file` | 删除文件 | **始终需审批**（高风险） |
| `move_file` | 移动或重命名文件，跨设备时自动回退到 copy+delete | 自动 |
| `copy_file` | 复制文件到目标位置 | 自动 |

## 命令与系统工具（5 个）

| 工具 | 说明 | 审批模式 |
|------|------|----------|
| `exec_command` | 执行命令（支持 shell 包装、working directory、timeout），输出有字节预算限制 | **条件审批**（检测到 rm/del/kill/shutdown/包管理器安装等危险命令时要求审批） |
| `check_command` | 检查命令是否存在于 PATH，可选查询版本信息 | 自动 |
| `list_processes` | 列出当前系统进程（跨平台：Windows tasklist / Unix ps） | 自动 |
| `kill_process` | 按 PID 终止进程 | **始终需审批**（高风险） |
| `get_system_info` | 获取系统信息：OS、架构、hostname、CPU 数量、Go 版本 | 自动 |

## 网络工具（2 个）

| 工具 | 说明 | 审批模式 |
|------|------|----------|
| `http_request` | 发送 HTTP 请求，响应体有字节预算限制 | 自动 |
| `web_search` | Web 搜索，首批支持 Tavily、SerpAPI、Bing | 自动 |

## Agent 工具（2 个）

| 工具 | 说明 | 审批模式 |
|------|------|----------|
| `ask_user` | 向用户发起结构化提问（支持选项、自定义回答、多选），触发人工交互挂起与恢复 | 由 runtime 拦截处理 |
| `using_skills` | 按名加载 workspace 技能内容（`skills/<name>/SKILL.md`），结果标记为 ephemeral 不写入会话历史 | 自动 |

## 输出预算

所有产生大量输出的工具均受 `OutputBudgetOptions` 限制，可在注册时配置：

- `read_file`：默认行数、最大行数限制
- `exec_command`：stdout / stderr 字节上限
- `http_request`：响应体字节上限
- `search_file` / `grep_file`：最大匹配数和匹配文本字节上限
- `list_files`：最大条目数
- `web_search`：最大结果数

超预算时返回截断标记和继续使用的指示信息。
