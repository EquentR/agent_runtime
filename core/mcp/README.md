# MCP

`core/mcp` 负责承接 Agent Runtime 内部的 MCP 抽象边界。

## 目录结构

```
core/mcp/
├── client.go          package mcp        — Client 接口与通用类型
├── types.go           package mcp        — ToolDescriptor、CallRequest、PromptDescriptor 等数据结构
├── config/
│   └── config.go      package mcp_config — YAML 配置结构与工厂方法
└── mark3labs/
    └── client.go      package mark3labs  — 基于 mark3labs/mcp-go SDK 的适配器
```

## 设计原则

- `core/tools` 只依赖 `core/mcp`，不直接依赖第三方 SDK
- 第三方实现放在子目录 adapter 中，当前为 `core/mcp/mark3labs`
- `core/mcp` 对外暴露运行时真正关心的最小数据结构，而不是完整 MCP spec

## 当前实现

- `core/mcp`：`Client` 接口与通用类型（`ToolDescriptor`、`CallRequest`、`CallResult`、`PromptDescriptor`、`GetPromptRequest`、`GetPromptResult`）
- `core/mcp/mark3labs`：基于 `github.com/mark3labs/mcp-go` 的 client adapter，实现 `coremcp.Client` 接口
- `core/mcp/config`：YAML 反序列化配置（`MCP`、`MCPServerConfig`），通过 `NewClient()` 工厂方法按 transport 类型构建对应的 mark3labs client

## 当前接入方式

- `mark3labs.NewStdioClient(command, env, args...)`：启动外部 MCP stdio server，并完成初始化
- `mark3labs.NewSSEClient(baseURL, options...)`：连接 legacy SSE MCP server
- `mark3labs.NewStreamableHTTPClient(baseURL, options...)`：连接 streamable HTTP MCP server
- `mark3labs.NewClient(raw)`：包装已初始化的原生 mark3labs client

## Prompt 包装

- `core/tools.RegisterMCPPrompts(...)` 可把 MCP prompt 包装成本地工具
- prompt 参数会被转换成字符串 schema，便于直接暴露给 LLM
- 远端 prompt 结果会被整理成可读文本，供 tool result 直接回传

## 配置方式

`conf/app.yaml` 中通过 `mcp.servers` 数组配置，每个 server 指定 `name`、`enabled`、`transport`（`stdio` / `sse` / `streamable_http`）及对应参数。

后续如需补充 server、resources、sessions 等能力，应继续优先扩展 `core/mcp` 的内部抽象，再追加 adapter。
