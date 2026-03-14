# Token Counter Implementation

## Overview
本地token计数器实现，支持两种计数模式，并提供统一的计数抽象：
1. **Rune近似计数**：轻量级，基于字符数估算
2. **Tokenizer精确计数**：使用tiktoken-go库进行精确计数

## Components

### TokenCounter
基础token计数器，提供同步计数功能。

### AsyncTokenCounter
Token计数器封装，提供统一的接口避免各API适配器重复实现。
仅在FinallyCalc时执行计数，不进行定时计数（因为只需要统计最终结果）。

**核心接口**：
- `Append(content)`: 追加内容到buffer
- `SetPromptCount(count)`: 设置prompt的token数
- `GetPromptCount()`: 获取prompt的token数
- `GetCount()`: 获取当前completion的token数
- `GetTotalCount()`: 获取总token数（prompt + completion）
- `CountPromptMessages(messages)`: 计算prompt消息的token数
- `FinallyCalc()`: 最终计数，阻塞直到完成
- `Close()`: 释放资源

## Features

### 计数模式
- **CountModeRune**: 使用 rune 字符数进行近似估算（字符数 × 3/4）
- **CountModeTokenizer**: 使用 tiktoken 进行精确token计数

### 计数策略
- **Buffer机制**: 累积流式输出的内容到buffer，避免token截断
- **延迟计数**: 仅在FinallyCalc时执行计数，提升性能
- **最终计数**: 流结束时调用FinallyCalc()完成计数，确保准确性
- **统一封装**: 所有逻辑封装在AsyncTokenCounter中，各API适配器只需调用接口

## Usage

### 同步计数
```go
// 创建tokenizer模式计数器
counter, err := tools.NewTokenCounter(tools.CountModeTokenizer, "gpt-3.5-turbo")
if err != nil {
    // 降级到rune模式
    counter, _ = tools.NewTokenCounter(tools.CountModeRune, "")
}
defer counter.Close()

// 计数
count := counter.Count("Hello world!")
```

### 流式场景计数（推荐）
```go
// 创建计数器
asyncCounter, err := tools.NewAsyncTokenCounter(tools.CountModeTokenizer, "gpt-3.5-turbo")
if err != nil {
    asyncCounter, _ = tools.NewAsyncTokenCounter(tools.CountModeRune, "")
}
defer asyncCounter.Close()

// 计算并设置prompt tokens
promptTokens := asyncCounter.CountPromptMessages([]string{"Hello", "world"})
asyncCounter.SetPromptCount(int64(promptTokens))

// 流式追加内容
asyncCounter.Append("Hello ")
asyncCounter.Append("world!")

// 流结束时获取最终计数
finalCount := asyncCounter.FinallyCalc()

// 获取完整统计
promptCount := asyncCounter.GetPromptCount()
completionCount := asyncCounter.GetCount()
totalCount := asyncCounter.GetTotalCount()
```

## Integration

在OpenAI Stream适配器中的使用示例：
1. 初始化时创建AsyncTokenCounter（优先tokenizer模式，失败则降级到rune模式）
2. 计算并设置prompt tokens
3. 流式接收时通过`Append()`追加内容
4. 流结束时调用`FinallyCalc()`获取最终计数
5. Close时调用`Close()`清理资源

**优势**：其他API适配器（如Anthropic、Azure等）只需同样调用这几个接口，无需重复实现计数逻辑。

## Dependencies

- `github.com/pkoukk/tiktoken-go`: 轻量级tiktoken实现，用于精确token计数

## Performance

- Rune模式: 非常快，适合实时场景
- Tokenizer模式: 相对较慢但准确
- 延迟计数: 仅在最终结果时计算一次，无定时计数开销，性能最优
