# MCP

`core/mcp` 负责承接 Agent Runtime 内部的 MCP 抽象边界。

当前阶段先提供 client-first 的最小能力：

- 统一的 `Client` interface
- 远端 tool 列表读取
- 远端 tool 调用
- 远端 prompt 列表读取
- 远端 prompt 获取与文本化包装
- 与 `core/tools` 的解耦集成

## 设计原则

- `core/tools` 只依赖 `core/mcp`，不直接依赖第三方 SDK
- 第三方实现放在子目录 adapter 中，例如 `core/mcp/mark3labs`
- `core/mcp` 对外暴露运行时真正关心的最小数据结构，而不是完整 MCP spec

## 当前实现

- `core/mcp`：内部接口与通用类型
- `core/mcp/mark3labs`：基于 `github.com/mark3labs/mcp-go` 的 client adapter

## 当前接入方式

- `mark3labs.NewStdioClient(...)`：启动外部 MCP stdio server，并在内部完成初始化
- `mark3labs.NewSSEClient(...)`：连接 legacy SSE MCP server，并在内部完成初始化
- `mark3labs.NewStreamableHTTPClient(...)`：连接 streamable HTTP MCP server，并在内部完成初始化

## Prompt 包装

- `core/tools.RegisterMCPPrompts(...)` 可把 MCP prompt 包装成本地工具
- prompt 参数会被转换成字符串 schema，便于直接暴露给 LLM
- 远端 prompt 结果会被整理成可读文本，供 tool result 直接回传

后续如果需要补充 server、resources、prompts、session 等能力，应继续优先扩展 `core/mcp` 的内部抽象，再追加 adapter。
