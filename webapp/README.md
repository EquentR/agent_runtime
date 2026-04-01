# Agent Runtime Webapp

`webapp` 是 `agent_runtime` 的参考前端，将后端已具备的会话、任务、审批、人工交互和管理能力完整呈现为可联调、可演示的 UI 基线。

## 🎯 设计目标

- 把 agent 任务执行过程可视化，呈现中间步骤而非仅最终回答。
- 把会话恢复、流式转录、审批处理、人工回复整合到同一条聊天工作流中。
- 为管理员提供提示词维护和任务审计的基础管理界面。
- 通过 `src/lib` 和 `src/types` 与后端 API 契约保持显式边界，统一做归一化与类型约束。

## 📄 当前页面与能力

- `/login`：登录页，负责会话进入与重定向。
- `/chat`：主聊天页，提供会话列表、模型选择、流式消息展示、任务恢复、取消运行、审批决策和人工问题回复。
- `/admin/prompts`：提示词管理页，面向管理员维护 prompt documents 与 bindings。
- `/admin/audit`：审计会话页，面向管理员查看任务运行轨迹。

## ✨ 当前交互特性

- 按 provider / model 选择模型，并与后端模型目录同步。
- 流式展示消息、工具调用、思考过程和最终回复。
- 支持思考与工具调用显示切换，便于控制信息密度。
- 会话草稿与活动任务状态本地持久化，刷新后可恢复界面状态。
- 在聊天流中内嵌处理工具审批与人工问题回复。
- 动态文档标题与移动端侧边栏适配。

## 🗂️ 目录职责

| 路径 | 职责 |
|---|---|
| `src/views` | 页面级视图 |
| `src/components` | 聊天与管理台复用组件 |
| `src/lib/api.ts` | HTTP API 调用边界 |
| `src/lib/transcript.ts` | 流式事件到转录条目的归一化逻辑 |
| `src/lib/session.ts` | 会话同步与登录态辅助 |
| `src/lib/chat-state.ts` | 聊天页本地状态持久化 |
| `src/types/api.ts` | 后端契约在前端侧的类型定义 |

## 🛠️ 开发命令

> 需要 Node 22+ 与 pnpm 包管理器。以下命令均在 `webapp/` 目录下执行，或从仓库根目录加 `--dir webapp` 前缀运行。

安装依赖：

```bash
pnpm install
```

本地开发：

```bash
pnpm dev
```

类型检查：

```bash
pnpm exec vue-tsc -b
```

测试：

```bash
pnpm test
```

构建：

```bash
pnpm build
```

## 📐 开发约定

- 使用 Vue 3 `<script setup lang="ts">`，仅类型导入用 `import type`。
- 类型与契约优先收敛到 `src/types` 和 `src/lib`，不要把接口兼容逻辑散落到页面组件。
- 聊天转录相关改动优先检查 `MessageList.vue`、`ChatView.vue` 和 `src/lib/transcript.ts` 是否需要一起调整。
- 管理台相关改动优先检查路由守卫和管理员权限判断是否同步。

## 🔗 相关后端依赖

- 聊天页依赖 `tasks`、`conversations`、`models`、`approvals`、`interactions` API。
- 管理台依赖 `prompts` 和 `audit` API。
- 登录态与路由守卫依赖 `auth` API。
