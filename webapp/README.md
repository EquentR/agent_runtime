# Agent Runtime Webapp

`webapp` 是 `agent_runtime` 的参考前端。它的目标不是只提供一个聊天输入框，而是把后端已经具备的会话、任务、审批、人工交互和管理能力真正呈现出来，作为可联调、可演示、可继续演进的 UI 基线。

## 前端目标

- 目标是把 agent 任务执行过程可视化，而不是只显示最终回答。
- 目标是把会话恢复、流式转录、审批处理、人工回复整合到同一条聊天工作流中。
- 目标是为管理员提供提示词和审计的基础管理界面。
- 目标是与后端 API 契约保持显式边界，通过 `src/lib` 和 `src/types` 统一做归一化与类型约束。

## 当前页面与能力

- `/login`：登录页，负责会话进入与重定向。
- `/chat`：主聊天页，负责会话列表、模型选择、流式消息展示、任务恢复、取消运行、审批决策和人工问题回复。
- `/admin/prompts`：提示词管理页，面向管理员维护 prompt documents 与 bindings。
- `/admin/audit`：审计会话页，面向管理员查看任务运行轨迹。

## 当前交互特性

- 支持按 provider / model 选择模型，并与后端模型目录同步。
- 支持流式展示消息、工具调用、思考过程和最终回复。
- 支持思考与工具调用显示切换，便于控制信息密度。
- 支持会话草稿与活动任务状态持久化，刷新后可以继续恢复界面状态。
- 支持在聊天流中内嵌处理工具审批与人工问题回复。
- 支持动态文档标题与移动端侧边栏适配。

## 目录职责

- `src/views`：页面级视图。
- `src/components`：聊天与管理台复用组件。
- `src/lib/api.ts`：HTTP API 调用边界。
- `src/lib/transcript.ts`：流式事件到转录条目的归一化逻辑。
- `src/lib/session.ts`：会话同步与登录态辅助。
- `src/lib/chat-state.ts`：聊天页本地状态持久化。
- `src/types/api.ts`：后端契约在前端侧的类型定义。

## 开发命令

- 需要 Node 22+ 版本与 pnpm 包管理器

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

## 开发约定

- 使用 Vue 3 `<script setup lang="ts">`。
- 类型与契约优先收敛到 `src/types` 和 `src/lib`，不要把接口兼容逻辑散落到页面组件。
- 聊天转录相关改动优先检查 `MessageList.vue`、`ChatView.vue` 和 `src/lib/transcript.ts` 是否需要一起调整。
- 管理台相关改动优先检查路由守卫和管理员权限判断是否同步。

## 相关后端依赖

- 聊天页依赖 `tasks`、`conversations`、`models`、`approvals`、`interactions` API。
- 管理台依赖 `prompts` 和 `audit` API。
- 登录态与路由守卫依赖 `auth` API。
