# Multi Provider Model Selection Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 让后端在启动时注入全部已配置 provider/model，前端可动态选择任意 provider/model 发起对话，并在多轮切换模型时按当前模型重新计算上下文预算与自动压缩，同时把每轮 agent loop 的模型与 token 信息展示到前端。

**Architecture:** 新增一个面向运行时的 LLM catalog，统一承载多 provider / 多 model 的解析、展示与 client 构造；`agent.run` executor 改为按请求解析具体 provider/model，并为每轮 run 基于所选模型构建 memory manager，使上下文预算随模型切换即时重算。前端通过新增的 catalog API 获取可选项，不再硬编码默认模型，并把每轮回复绑定的 provider/model + token usage 一并展示。

**Tech Stack:** Go, Gin, Gorm/SQLite, Vue 3, Vitest

---

### Task 1: LLM catalog 与后端注入

**Files:**
- Modify: `core/agent/executor.go`
- Modify: `app/commands/serve.go`
- Modify: `app/router/deps.go`
- Modify: `app/router/init.go`
- Create: `app/handlers/model_catalog_handler.go`
- Test: `core/agent/executor_test.go`
- Test: `app/handlers/model_catalog_handler_test.go`

**Step 1: Write the failing tests**

- 在 `core/agent/executor_test.go` 增加多 provider / 多 model 解析测试，覆盖：
  - 不同 provider 下同名/不同名 model 可正确解析
  - 未配置 provider / model 返回明确错误
- 在 `app/handlers/model_catalog_handler_test.go` 增加 API 测试，覆盖：
  - 返回全部 provider 与各自 models
  - 返回默认 provider/model（取配置中的首个可用项）

**Step 2: Run test to verify it fails**

Run: `go test ./core/agent ./app/handlers`
Expected: FAIL，提示 resolver / handler 尚不支持多 provider catalog

**Step 3: Write minimal implementation**

- 抽象 `ModelResolver` 为多 provider catalog
- `Serve` 启动时注入全部 `c.LLM`
- 新增只读 catalog API，供前端拉取 provider/model 列表

**Step 4: Run test to verify it passes**

Run: `go test ./core/agent ./app/handlers`
Expected: PASS

### Task 2: conversation 切换模型与 memory 预算重算

**Files:**
- Modify: `core/agent/executor.go`
- Modify: `core/agent/conversation_store.go`
- Modify: `core/providers/types/types.go`
- Test: `core/agent/executor_test.go`
- Test: `core/agent/conversation_store_test.go`

**Step 1: Write the failing tests**

- 增加 executor 测试，覆盖同一 conversation 第二轮切换 model 时：
  - 不再报 provider/model mismatch
  - runner 使用新模型上下文预算
  - assistant 持久化消息记录本轮 provider/model
- 增加 store 测试，覆盖 conversation 元数据更新为当前最新 provider/model

**Step 2: Run test to verify it fails**

Run: `go test ./core/agent`
Expected: FAIL，当前实现会拒绝切换 model，且不会初始化 memory manager

**Step 3: Write minimal implementation**

- conversation 已存在时允许更新最新 provider/model
- executor 为每轮 run 创建 `memory.Manager{Model: llmModel}`
- 将本轮 provider/model 绑定到最终 assistant message，便于历史回放展示

**Step 4: Run test to verify it passes**

Run: `go test ./core/agent`
Expected: PASS

### Task 3: task/result/SSE 暴露每轮模型与 token 信息

**Files:**
- Modify: `core/agent/executor.go`
- Modify: `webapp/src/types/api.ts`
- Modify: `webapp/src/lib/api.ts`
- Modify: `webapp/src/lib/transcript.ts`
- Test: `webapp/src/lib/api.spec.ts`
- Test: `webapp/src/lib/transcript.spec.ts`

**Step 1: Write the failing tests**

- 增加前端 normalization 测试，覆盖 run result / persisted message 中的 provider/model 字段
- 增加 transcript 测试，覆盖 `task.finished` 后把 provider/model + usage 绑定到最新 reply

**Step 2: Run test to verify it fails**

Run: `pnpm --dir webapp test -- --runInBand`
Expected: FAIL，当前 transcript entry 不保存模型信息

**Step 3: Write minimal implementation**

- 扩展前端 API 类型
- transcript reply entry 挂载 provider/model 元信息
- 兼容历史消息回放与实时 SSE 完成事件

**Step 4: Run test to verify it passes**

Run: `pnpm --dir webapp test -- --runInBand`
Expected: PASS

### Task 4: 前端动态模型选择 UI

**Files:**
- Modify: `webapp/src/views/ChatView.vue`
- Modify: `webapp/src/components/MessageComposer.vue`
- Modify: `webapp/src/components/MessageList.vue`
- Modify: `webapp/src/lib/chat.ts`
- Test: `webapp/src/views/ChatView.spec.ts`
- Test: `webapp/src/components/MessageList.spec.ts`

**Step 1: Write the failing tests**

- 增加 `ChatView` 测试，覆盖：
  - 初始化拉取 catalog
  - 新对话默认选中后端默认 provider/model
  - 切换下拉框后发起请求带上新 provider/model
  - 选中旧 conversation 时 UI 同步到该 conversation 最新 provider/model
- 增加 `MessageList` 测试，覆盖回复底部展示 provider/model + token

**Step 2: Run test to verify it fails**

Run: `pnpm --dir webapp test -- --runInBand`
Expected: FAIL，当前前端仍使用写死默认值且不展示模型元信息

**Step 3: Write minimal implementation**

- 用 catalog API 驱动选择器
- 发送消息时使用当前选择值
- 回复 footer 展示 `provider/model + token usage`

**Step 4: Run test to verify it passes**

Run: `pnpm --dir webapp test -- --runInBand`
Expected: PASS

### Task 5: 回归验证与文档

**Files:**
- Modify: `docs/swagger/swagger.json`
- Modify: `docs/swagger/swagger.yaml`
- Modify: `docs/swagger/docs.go`

**Step 1: Run backend and frontend verification**

Run: `go test ./...`
Expected: PASS

Run: `pnpm --dir webapp test -- --runInBand`
Expected: PASS

**Step 2: Refresh Swagger if handler structs or routes changed**

Run: `C:\Users\Equent\go\bin\swag.exe init -g cmd/example_agent/main.go -o docs/swagger`
Expected: swagger files updated with catalog API schema

**Step 3: Re-run targeted verification**

Run: `go test ./app/handlers ./core/agent`
Expected: PASS
