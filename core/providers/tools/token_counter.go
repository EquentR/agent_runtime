package tools

import (
	"sync"
	"unicode/utf8"

	"github.com/pkoukk/tiktoken-go"
)

// CountMode 计数模式
type CountMode int

const (
	// CountModeRune 使用 rune 近似计数
	CountModeRune CountMode = iota
	// CountModeTokenizer 使用 tiktoken 精确计数
	CountModeTokenizer
)

// TokenCounter 本地token计数器
type TokenCounter struct {
	mode     CountMode
	encoding *tiktoken.Tiktoken
	mu       sync.RWMutex
}

// NewTokenCounter 创建token计数器
// mode: 计数模式
// model: 模型名称（仅在 tokenizer 模式下使用）
func NewTokenCounter(mode CountMode, model string) (*TokenCounter, error) {
	tc := &TokenCounter{
		mode: mode,
	}

	if mode == CountModeTokenizer {
		enc, err := tiktoken.EncodingForModel(model)
		if err != nil {
			return nil, err
		}
		tc.encoding = enc
	}

	return tc, nil
}

// NewCl100kTokenCounter 创建默认的cl100k_base token计数器
func NewCl100kTokenCounter() (*TokenCounter, error) {
	tc := &TokenCounter{
		mode: CountModeTokenizer,
	}
	enc, err := tiktoken.GetEncoding(tiktoken.MODEL_CL100K_BASE)
	if err != nil {
		return nil, err
	}
	tc.encoding = enc

	return tc, nil
}

// Count 计算token数量
func (tc *TokenCounter) Count(text string) int {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	switch tc.mode {
	case CountModeRune:
		return tc.countByRune(text)
	case CountModeTokenizer:
		return tc.countByTokenizer(text)
	default:
		return tc.countByRune(text)
	}
}

// countByRune 使用 rune 计数近似估算
// 中文字符通常占 1.5-2 个 token，英文单词约 0.75 个 token
func (tc *TokenCounter) countByRune(text string) int {
	if text == "" {
		return 0
	}
	// 简单近似：按字符数 / 4 估算
	return utf8.RuneCountInString(text) * 3 / 4
}

// countByTokenizer 使用 tiktoken 精确计数
func (tc *TokenCounter) countByTokenizer(text string) int {
	if text == "" || tc.encoding == nil {
		return 0
	}
	tokens := tc.encoding.Encode(text, nil, nil)
	return len(tokens)
}

// CountMessages 计算消息列表的token数
// messages: 消息内容切片
func (tc *TokenCounter) CountMessages(messages []string) int {
	tc.mu.RLock()
	defer tc.mu.RUnlock()

	totalCount := 0
	for _, msg := range messages {
		totalCount += tc.Count(msg)
	}
	// 为消息格式添加一些开销（每条消息约4个token的开销）
	totalCount += len(messages) * 4
	return totalCount
}

// Close 释放资源
func (tc *TokenCounter) Close() {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	// tiktoken-go 的 encoding 无需显式释放
	tc.encoding = nil
}
