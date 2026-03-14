package tools

import (
	"sync"
)

// AsyncTokenCounter token计数器，封装计数逻辑
// 提供统一的Append、FinallyCalc接口，避免各API适配器重复实现
// 仅在FinallyCalc时执行计数，不进行定时计数
type AsyncTokenCounter struct {
	counter      *TokenCounter
	buffer       string
	bufferMu     sync.Mutex
	currentCount int64
	promptCount  int64 // prompt的token数
	countMu      sync.RWMutex
}

// NewAsyncTokenCounter 创建token计数器
// mode: 计数模式
// model: 模型名称（仅在tokenizer模式下使用）
func NewAsyncTokenCounter(mode CountMode, model string) (*AsyncTokenCounter, error) {
	counter, err := NewTokenCounter(mode, model)
	if err != nil {
		return nil, err
	}

	atc := &AsyncTokenCounter{
		counter: counter,
	}

	return atc, nil
}

func NewCl100kAsyncTokenCounter() (*AsyncTokenCounter, error) {
	counter, err := NewCl100kTokenCounter()
	if err != nil {
		return nil, err
	}

	atc := &AsyncTokenCounter{
		counter: counter,
	}
	return atc, nil
}

// Append 追加内容到buffer
func (atc *AsyncTokenCounter) Append(content string) {
	if content == "" {
		return
	}
	atc.bufferMu.Lock()
	atc.buffer += content
	atc.bufferMu.Unlock()
}

// GetCount 获取当前计数
func (atc *AsyncTokenCounter) GetCount() int64 {
	atc.countMu.RLock()
	defer atc.countMu.RUnlock()
	return atc.currentCount
}

// SetPromptCount 设置prompt的token数
func (atc *AsyncTokenCounter) SetPromptCount(count int64) {
	atc.countMu.Lock()
	atc.promptCount = count
	atc.countMu.Unlock()
}

// GetPromptCount 获取prompt的token数
func (atc *AsyncTokenCounter) GetPromptCount() int64 {
	atc.countMu.RLock()
	defer atc.countMu.RUnlock()
	return atc.promptCount
}

// GetTotalCount 获取总token数（prompt + completion）
func (atc *AsyncTokenCounter) GetTotalCount() int64 {
	atc.countMu.RLock()
	defer atc.countMu.RUnlock()
	return atc.promptCount + atc.currentCount
}

// CountPromptMessages 计算prompt消息的token数
func (atc *AsyncTokenCounter) CountPromptMessages(messages []string) int {
	if atc.counter == nil {
		return 0
	}
	return atc.counter.CountMessages(messages)
}

// FinallyCalc 最终计数，确保计算完整内容
// 该方法会阻塞直到计数完成
func (atc *AsyncTokenCounter) FinallyCalc() int64 {
	// 执行最终计数
	atc.bufferMu.Lock()
	text := atc.buffer
	atc.bufferMu.Unlock()

	if atc.counter != nil && text != "" {
		count := atc.counter.Count(text)
		atc.countMu.Lock()
		atc.currentCount = int64(count)
		atc.countMu.Unlock()
	}

	return atc.GetCount()
}

// Close 释放资源
func (atc *AsyncTokenCounter) Close() {

	if atc.counter != nil {
		atc.counter.Close()
	}
}
