# Agent 工具调用防循环设计

## 背景

在对 `run_467a9f1f-2121-468a-9759-f94de468acce` 的审计分析中，运行时没有卡在 `write_file` 工具内部，而是模型在工具已经成功后持续发起新的工具调用。典型轨迹是：

- `write_file` 已成功写入 `skills目录介绍-2026-05-31.md`。
- 后续多个 step 继续对同一路径执行 `overwrite`。
- 每次工具都返回成功，但模型始终没有输出不带 `tool_calls` 的最终 assistant 消息。

因此，防循环能力应放在 agent runtime 层，作为模型行为的安全兜底。它不应依赖单个工具自行判断，也不应只靠提示词约束模型。

## 目标

- 识别同一 run 内重复、低进展或无进展的工具调用模式。
- 对可恢复的循环先软提醒模型，对明显循环进行硬停止。
- 区分“重复成功”和“重复失败”两类循环，采用不同终止策略。
- 针对不同工具类型使用不同签名和阈值，避免误杀正常多步任务。
- 将防循环判断写入审计，便于后续复盘。

## 非目标

- 不修改默认 step 步数或 `MaxSteps`。
- 不改变模型选择工具的基本机制。
- 不改变工具本身的业务语义，例如 `write_file` 仍然负责写文件。
- 不在第一版引入复杂的语义 diff 或 LLM 评估器来判断“是否有进展”。
- 不把所有重复工具调用都视为错误；文件分页读取、搜索迭代、不同路径处理仍应允许。

## 设计概览

新增一个运行时 `LoopGuard`，挂在 `core/agent` 的工具执行链路中。它观察模型输出的工具调用、工具参数、执行结果和连续调用历史，并给出三种决策：

- `allow`：允许工具执行。
- `warn`：允许本次执行，但向下一轮上下文注入一条运行时提醒。
- `stop`：拒绝继续执行，按循环类型安全停止或失败。

整体流程：

```text
model.completed
  -> LoopGuard.BeforeToolCall(...)
  -> execute tool
  -> LoopGuard.AfterToolResult(...)
  -> maybe inject loop warning before next model request
  -> maybe stop run with loop_guard_triggered
```

## 组件设计

### 1. 工具调用画像

每次工具调用都生成规范化画像。画像不是简单地序列化完整 arguments，而是按工具类型提取对循环判断真正重要的字段。

通用字段：

```text
tool_name
operation_kind
target_key
argument_fingerprint
outcome_kind
error_fingerprint
```

示例：

```text
write_file + overwrite + skills目录介绍-2026-05-31.md + success
list_files + list + "" + error:path_is_required
read_file + read_window + skills/foo/SKILL.md:1:80 + success
```

`argument_fingerprint` 用于识别完全重复调用；`target_key` 用于识别“同一目标被反复操作”。对 `write_file overwrite`，循环判断应主要看 `target_key + operation_kind + outcome_kind`，因为模型可能每轮生成不同 content，但行为本质仍是反复整文件覆盖同一路径。

### 2. 软提醒与硬拦截

防循环采用两段式策略。

软提醒：

- 触发条件：同一目标的可疑重复达到软阈值。
- 行为：本次工具仍执行，但在下一次模型请求前追加一条 system 角色运行时提醒。
- 目的：给模型一次自我修正机会。

提醒文本应短而具体，例如：

```text
The previous write_file call already succeeded for this target. Do not call write_file again unless the user explicitly requested another revision. If the task is complete, respond without tool calls and mention only the saved path.
```

硬拦截：

- 触发条件：同一目标重复达到硬阈值，或同一错误重复达到硬阈值。
- 行为：不再执行新的重复工具调用，结束当前 run。
- 审计记录：写入 `loop.guard.stopped`。

建议默认阈值：

```text
same_target_success_soft_limit = 2
same_target_success_hard_limit = 3
same_error_soft_limit = 2
same_error_hard_limit = 3
```

### 3. 按工具类型定义规则

#### `write_file`

规则：

- `mode = overwrite` 且同一路径连续成功多次，判定为重复产出循环。
- 如果 content 完全相同，风险更高，可直接计为强重复。
- 如果 content 不同但同一路径连续整文件覆盖，仍计为同目标重复。
- `append`、`insert`、`replace_lines` 不使用同一阈值；它们可能是正常分段编辑，应按 `path + mode + line_range` 判断。

默认策略：

```text
同一路径 overwrite 成功 2 次：warn
同一路径 overwrite 成功 3 次：stop as safe completion
```

#### `list_files`

规则：

- 同一参数同一错误连续出现，判定为错误恢复循环。
- 空 `path` 导致的 `path is required` 应归一为稳定错误码。
- 不同路径的目录探索不算循环。

默认策略：

```text
同参数同错误 2 次：warn
同参数同错误 3 次：stop as failed loop
```

#### `read_file`

规则：

- 不同 `start_line` 或 `line_count` 的窗口读取应允许。
- 同一路径同一窗口反复读取，且中间没有新的用户请求或不同工具结果，才计为重复。
- 读取多个不同 skill 文件属于正常行为。

#### `search_file` / `grep_file`

规则：

- 完全相同 query、path、flags 的连续重复才计入循环。
- 相近但不同 query 不做第一版语义合并。

#### 交互与审批工具

`ask_user`、审批类交互不参与普通工具循环判断。它们受等待态和交互状态机控制，误判代价高。

### 4. 审计事件

新增审计事件类型：

```text
loop.guard.warning
loop.guard.stopped
```

`loop.guard.warning` payload：

```json
{
  "reason": "same_target_write_repeated",
  "tool_name": "write_file",
  "target": "skills目录介绍-2026-05-31.md",
  "operation": "overwrite",
  "repeat_count": 2,
  "soft_limit": 2,
  "hard_limit": 3
}
```

`loop.guard.stopped` payload：

```json
{
  "reason": "same_target_write_repeated",
  "tool_name": "write_file",
  "target": "skills目录介绍-2026-05-31.md",
  "operation": "overwrite",
  "repeat_count": 3,
  "stop_strategy": "safe_completion"
}
```

这些事件应出现在 audit replay 时间线上，并和对应 step/tool 调用相邻，方便定位循环是何时被识别和截断的。

### 5. 终止策略

防循环停止不应只有一种失败结果。根据循环类型分两类。

#### 重复成功类循环

适用场景：

- `write_file overwrite` 同一路径多次成功。
- 工具结果显示任务主要产物已经生成。
- 后续重复只是在同一目标上继续改写，没有新的外部信息输入。

策略：

- 不再执行第 N 次重复工具调用。
- 生成一个确定性的最终 assistant 消息。
- run 以成功或受控停止结束，`stop_reason = loop_guard_safe_completion`。

示例最终消息：

```text
文件已写入 `skills目录介绍-2026-05-31.md`。运行时检测到模型正在重复覆盖同一文件，因此已停止继续调用工具。
```

#### 重复失败类循环

适用场景：

- 同一工具、同一参数、同一错误连续出现。
- 软提醒后模型仍未修正。

策略：

- run 失败，错误为 `ErrToolLoopDetected`。
- error data 包含工具名、参数摘要和重复错误。
- 不再继续消耗模型和工具调用。

示例错误：

```text
tool loop detected: list_files repeatedly failed with "path is required"
```

## 数据结构

建议在 `core/agent` 中新增小而独立的 guard 结构：

```go
type LoopGuardDecision string

const (
	LoopGuardAllow LoopGuardDecision = "allow"
	LoopGuardWarn  LoopGuardDecision = "warn"
	LoopGuardStop  LoopGuardDecision = "stop"
)

type LoopGuardResult struct {
	Decision     LoopGuardDecision
	Reason       string
	ToolName     string
	Target       string
	RepeatCount  int
	StopStrategy string
	WarningText  string
}
```

Runner 只消费决策，不需要知道每个工具的具体判定细节。

## 集成点

第一版集成在 `Runner.executeAssistantToolCalls` 附近：

1. 解码 tool arguments 后，调用 `LoopGuard.BeforeToolCall`。
2. 若返回 `stop`，不执行工具，结束 run。
3. 若返回 `warn`，本次仍执行，但记录待注入提醒。
4. 工具执行完成后，调用 `LoopGuard.AfterToolResult` 记录成功或失败。
5. 下一轮构建请求前，把待注入提醒加入上下文。

这样可以保持边界清晰：工具不需要知道循环策略，provider 不需要知道工具语义，Runner 负责运行时控制。

## 错误处理

- LoopGuard 自身失败不应导致任务崩溃；记录 warning 后默认 allow。
- 无法解析参数时沿用现有工具参数错误路径，同时计入重复失败判断。
- 对安全停止生成的最终消息，应明确说明“运行时检测到重复调用并停止”，避免用户误以为模型自然完成。
- 如果停止时没有任何成功产物，不使用 safe completion，改为失败。

## 配置

第一版可先使用代码默认值，不引入 UI 配置。保留内部 option，便于测试和后续调参：

```go
type LoopGuardOptions struct {
	Enabled                    bool
	SameTargetSuccessSoftLimit int
	SameTargetSuccessHardLimit int
	SameErrorSoftLimit         int
	SameErrorHardLimit         int
}
```

默认启用。测试中可关闭或调整阈值。

## 测试要求

- `write_file overwrite` 同一路径连续成功 2 次后注入 warning。
- `write_file overwrite` 同一路径连续成功 3 次后触发 safe completion。
- `write_file overwrite` 不同路径不触发循环。
- `write_file replace_lines` 不同 line range 不触发循环。
- `list_files` 同参数同错误连续 3 次后返回 `ErrToolLoopDetected`。
- `read_file` 同文件不同窗口读取不触发循环。
- `read_file` 同文件同窗口连续重复超过阈值触发 warning/stop。
- 防循环触发时写入 `loop.guard.warning` 和 `loop.guard.stopped` 审计事件。
- safe completion 的最终消息会被持久化到 conversation。
- 重复失败类循环不会被标记为成功。

## 验收标准

- 指定审计案例中的重复 `write_file overwrite` 会在第三次同目标成功覆盖前后被运行时截断。
- 工具调用不会继续无限消耗到用户手动取消。
- 正常读取多个 `SKILL.md` 文件不会被误判。
- 防循环结果可以从 audit replay 中直接看出原因、目标、重复次数和停止策略。
- 实现不修改默认 step 步数。
