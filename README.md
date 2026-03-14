# Agent Runtime
一个基于 Go 的轻量级 Agent Runtime，提供核心智能体循环、工具调用、记忆管理和成本跟踪功能，支持多种 LLM Provider 和工具集成，适用于构建定制化智能体应用。

## 项目现状
本项目结构完整，但功能尚未完全实现，目前已实现的核心包括：
- `core/providers`：已实现 OpenAI、Google Gemini、OpenAI Responses API 等 LLM 集成
- `pkg/db`、`pkg/migrate`：SQLite 数据库和迁移支持
- `pkg/rest`：基于 Gin 的 REST 框架封装

未实现的部分代码：
- `cmd/example_agent`：示例命令行程序入口

## 快速开始

### 依赖安装
```bash
go mod download
go mod tidy
```

### 构建示例
```bash
go build -o bin/example_agent ./cmd/example_agent
```

### 运行测试
```bash
go test ./...                    # 运行全量测试
go test ./core/providers/...    # 仅测试 providers
go test -v ./...                # 详细输出
```

# 项目结构
此项目结构为预定义，后续可能存在变更，目前主要分为以下几个模块：
```
Agent Runtime
├──cmd              # 各阶段的命令行入口，负责加载配置、初始化依赖并启动智能体服务
│   └──example_agent   # 示例智能体应用
├──core             # agent 核心层，实现智能体循环、记忆管理、成本跟踪等核心功能
│   ├──agent        # 智能体核心逻辑
│   ├──providers    # LLM 抽象层，定义统一的消息模型、请求/响应、流式结构和接口
│   ├──tools        # 工具调用相关的接口和实现
│   ├──mcp          # mcp 协议相关实现，此模块可能会被拆分或与 tools 合并
│   ├──rag          # 基于简单向量数据库的检索增强能力实现
│   └──types        # 定义全局通用的数据结构和接口
├──pkg              # 对外暴露的公共接口和工具库，供 core 和业务层使用
│   ├──db           # 数据库相关操作和封装（首批支持 SQLite）
│   ├──rest         # REST 相关封装，提供统一的接口和默认实现（基于 Gin）
│   ├──log          # 日志相关封装，提供统一的日志接口和默认实现（zap）
│   └──migrate      # 数据库迁移相关操作和封装（首批支持 SQLite）
├──app              # 具体的智能体应用实现，基于 core 提供的能力构建实际的智能体服务
│   ├──commands     # 不同的命令行入口，负责加载配置、初始化依赖并启动智能体循环
│   ├──config       # 配置相关的结构定义和加载逻辑
│   ├──handlers     # REST API 的具体处理逻辑，负责接收请求、调用 core 能力并返回响应
│   ├──logics       # 智能体具体的业务逻辑实现
│   └──router       # REST API 的路由定义，负责把不同的 URL 路径映射到对应的 handlers
├──docs             # 项目文档目录
└── ...其他文件
```

## 核心依赖

### LLM 集成
- **OpenAI**: 标准 API 和 Responses API 支持
- **Google Gemini**: 通过 `google.golang.org/genai` 集成
- **其他兼容接口**: 支持 Azure OpenAI 等兼容端点

### 基础设施
- **数据库**: SQLite（via `glebarez/sqlite`）
- **Web 框架**: Gin（via `gin-gonic/gin`）
- **日志**: Zap（配置中）
- **其他**: 详见 `go.mod`

## 开发指南

### 包管理原则
- 依赖方向必须单向：`app -> core -> pkg`
- `pkg` 层不能导入 `core` 或 `app`
- 每个包都应有明确的职责边界

### 编码规范
- 接口优先：在 `core/providers` 和 `core/tools` 定义统一接口
- 隐藏实现细节：具体的 LLM 或工具适配在接口后
- 文档齐全：重要的包应有 README.md 说明

### 测试
- 为新功能编写单元测试
- 使用 `go test -cover` 检查覆盖率
- Integration 测试放在 `*_integration_test.go` 中

## 文档

更多信息请查看 [AI 编码指南](./AGENTS.md)。
