# Responses API 踩坑记录

这份记录只总结本项目在接 OpenAI Responses API、尤其是 agent loop 多轮调用时，已经实际踩过的坑。

参考过的上下文：
- 最近相关提交：`e4242fa`、`517f8a7`、`848b1b7`
- 这次额外对照：`E:\Develop\OpenSource\crush\internal\agent\agent.go`
- 这次额外对照：`E:\Develop\OpenSource\fantasy\providers\openai\responses_language_model.go`

## 1. 不能只看单轮请求，必须看多轮 replay

单轮请求能过，不代表 agent loop 多轮能过。

Responses API 对历史消息重放非常敏感，尤其是这些内容之间的关系：
- assistant reasoning item
- assistant message
- assistant function_call
- tool function_call_output

如果这些内容在下一轮没有按 API 预期的形态回放，上游很容易拒绝请求，或者虽然不拒绝，但丢失推理连续性。

## 2. 误把“历史里出现过 tool output”当成“当前请求是 tool continuation”

踩坑现象：
- 只要输入里出现过 `function_call_output`
- 就把 `tools` 从请求参数里移除

这个判断是错的。

正确判断应该是：
- 只有“消息尾部”仍然处于 `assistant tool_call -> tool result -> 继续推理` 这个 continuation 场景时
- 才应该在这一次请求里去掉 `tools`

否则会出现：
- 历史里早就有过一轮工具调用
- 但用户已经开启了一个新回合
- 请求却把 `tools` 去掉了
- 导致模型在新回合无法继续调用工具

相关修复思路已经体现在 `848b1b7`：
- 不再看“input 里是否存在 tool output”
- 改成判断 request 是否“以 tool continuation 结尾”

## 3. ProviderState 里有完整 output 时，不应优先降级成 item_reference replay

这是这次补查时发现的另一个关键坑。

之前的错误倾向：
- assistant 历史消息只要带了 `ProviderState`
- 就优先把旧内容压成 `item_reference`
- 而不是优先回放完整结构化 output items

这会导致一个问题：
- 当前本地明明保存了完整的 `reasoning/message/function_call` 结构
- 却在续轮里只发引用
- 结果请求形态和参考实现不一致
- 某些模型/某些多轮场景下更容易被上游拒绝

参考项目的经验更接近下面这个原则：
- 如果本地有完整结构化 output，就优先原样回放这些 items
- 只有在拿不到完整 output 时，才考虑更弱的 replay 方式

本项目这次修正后，`openai_responses_new` 的优先级变成：
- `ProviderData` 原始 output snapshot
- `ProviderState` 完整 output archive
- `ProviderState` item references（仅作为 fallback）
- 最后才退回普通 assistant/content/toolCalls 拼装

这类问题本质上不是“架构问题”，而是“请求参数/消息回放构造不符合 Responses API 习惯”的问题。

## 4. tool continuation 回放时，不能把非 function_call 的旧 item 继续带上

当一轮请求的最后状态是：
- assistant 发出 function call
- tool 返回 function_call_output
- 模型准备继续推理

这时 continuation 请求里最稳妥的做法是：
- 只保留需要继续闭环的那部分内容
- 即 `function_call` + `function_call_output`

不要把旧的这些内容继续混进 continuation：
- reasoning item
- assistant 普通 text message

否则容易形成一种“历史 replay + continuation replay 混杂”的输入，增加上游拒绝概率。

本项目现在已经对 tool continuation 做过滤：
- continuation 时只保留 `function_call`
- 然后接新的 `function_call_output`

## 5. reasoning model 的顶层参数不能按普通模型发

这次也确认了几个参数构造层面的坑：

### 5.1 system role 不能一律发 `system`

对部分 reasoning model：
- system 需要改成 `developer`

对 `o1-mini` / `o1-preview`：
- system 需要直接移除

如果一律发 `system`，会和参考实现偏离，在某些模型上增加拒绝概率。

### 5.2 reasoning 参数不能对所有模型默认下发

之前错误倾向：
- 不区分模型
- 默认带 `reasoning.summary=auto`、`reasoning.effort=medium`

更稳妥的做法：
- 只对 reasoning model 带 `reasoning`

### 5.3 reasoning model 不要继续带 `temperature/top_p`

参考实现会把这些参数视为 reasoning model 的不支持项。

如果继续原样下发：
- 请求形态会偏离参考实现
- 上游更可能因为参数组合不合规而拒绝

## 6. ProviderState 设计要服务“回放”，不是只服务“存档”

`ProviderState` 不是为了把响应存下来就结束。

真正要问的是：
- 下一轮还能不能稳定拿它构造出合法请求？

所以保存状态时，至少要考虑：
- 是否保存完整 output archive
- 是否保留 response id
- 是否能在 tool continuation 时只抽出必需片段
- 是否能在普通 replay 时还原 reasoning/message/function_call 顺序

如果状态只够“展示”，不够“回放”，后面多轮一定继续踩坑。

## 7. 经验总结

接 Responses API 时，最容易犯的错不是 SDK 调错，而是误判“下一轮到底该怎么重放上一轮”。

以后排查这类问题，优先检查这 4 件事：
- 当前请求是不是 tool continuation
- 历史 assistant 是不是被错误降级成 `item_reference`
- reasoning / message / function_call / function_call_output 的顺序是否正确
- 当前模型是否用了不匹配的顶层参数（`system/developer`、`reasoning`、`temperature`、`top_p`）

## 8. 本次对应代码位置

- `core/providers/client/openai_responses_new/utils.go`
- `core/providers/client/openai_responses_new/provider_state.go`
- `core/providers/client/openai_responses_new/utils_test.go`
- `core/agent/memory.go`
- `core/agent/conversation_store.go`

如果后面又遇到“同一个上游单轮可用，但 agent loop 第二轮/第三轮被拒”的问题，先从这里查，不要先怀疑工具框架本身。
