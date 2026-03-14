package tools

import (
	"testing"
)

func TestTokenCounter_Rune(t *testing.T) {
	counter, err := NewTokenCounter(CountModeRune, "")
	if err != nil {
		t.Fatalf("Failed to create rune counter: %v", err)
	}
	defer counter.Close()

	tests := []struct {
		name string
		text string
		want int
	}{
		{
			name: "empty string",
			text: "",
			want: 0,
		},
		{
			name: "simple english",
			text: "Hello world",
			want: 8, // 11 runes * 3 / 4 = 8
		},
		{
			name: "mixed content",
			text: "Hello 世界",
			want: 6, // 8 runes * 3 / 4 = 6
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := counter.Count(tt.text)
			if got != tt.want {
				t.Errorf("Count() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTokenCounter_Tokenizer(t *testing.T) {
	counter, err := NewTokenCounter(CountModeTokenizer, "gpt-3.5-turbo")
	if err != nil {
		t.Fatalf("Failed to create tokenizer counter: %v", err)
	}
	defer counter.Close()

	tests := []struct {
		name string
		text string
	}{
		{
			name: "empty string",
			text: "",
		},
		{
			name: "simple english",
			text: "Hello world",
		},
		{
			name: "mixed content",
			text: "Hello 世界! This is a test.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := counter.Count(tt.text)
			// 仅验证非负数和合理性
			if tt.text == "" {
				if got != 0 {
					t.Errorf("Count() = %v, want 0 for empty string", got)
				}
			} else {
				if got <= 0 {
					t.Errorf("Count() = %v, want positive value", got)
				}
			}
		})
	}
}

func TestTokenCounter_Fallback(t *testing.T) {
	// 测试不支持的模型会降级到 rune 模式
	counter, err := NewTokenCounter(CountModeTokenizer, "unsupported-model")
	if err == nil {
		t.Fatal("Expected error for unsupported model")
	}
	if counter != nil {
		t.Fatal("Expected nil counter for unsupported model")
	}
}

func BenchmarkTokenCounter_Rune(b *testing.B) {
	counter, _ := NewTokenCounter(CountModeRune, "")
	text := "Hello world! This is a benchmark test for token counting."

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		counter.Count(text)
	}
}

func BenchmarkTokenCounter_Tokenizer(b *testing.B) {
	counter, err := NewTokenCounter(CountModeTokenizer, "gpt-3.5-turbo")
	if err != nil {
		b.Fatalf("Failed to create counter: %v", err)
	}
	text := "Hello world! This is a benchmark test for token counting."

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		counter.Count(text)
	}
}

func TestAsyncTokenCounter(t *testing.T) {
	counter, err := NewAsyncTokenCounter(CountModeRune, "")
	if err != nil {
		t.Fatalf("Failed to create async counter: %v", err)
	}
	defer counter.Close()

	// 追加内容
	counter.Append("Hello ")
	counter.Append("world!")

	// 在FinallyCalc之前，count应该为0
	count := counter.GetCount()
	if count != 0 {
		t.Errorf("Expected 0 count before FinallyCalc, got %d", count)
	}

	// 最终计数
	finalCount := counter.FinallyCalc()
	if finalCount <= 0 {
		t.Errorf("Expected positive final count, got %d", finalCount)
	}

	// 验证最终计数是准确的
	expected := 9 // "Hello world!" = 12 runes * 3 / 4 = 9
	if finalCount != int64(expected) {
		t.Errorf("Expected %d, got %d", expected, finalCount)
	}
}

func TestAsyncTokenCounter_Tokenizer(t *testing.T) {
	counter, err := NewAsyncTokenCounter(CountModeTokenizer, "gpt-3.5-turbo")
	if err != nil {
		t.Fatalf("Failed to create async counter: %v", err)
	}
	defer counter.Close()

	// 追加内容
	counter.Append("Hello ")
	counter.Append("world! ")
	counter.Append("This is a test.")

	// 最终计数
	finalCount := counter.FinallyCalc()
	if finalCount <= 0 {
		t.Errorf("Expected positive final count, got %d", finalCount)
	}
}

func TestAsyncTokenCounter_EmptyContent(t *testing.T) {
	counter, err := NewAsyncTokenCounter(CountModeRune, "")
	if err != nil {
		t.Fatalf("Failed to create async counter: %v", err)
	}
	defer counter.Close()

	// 不追加任何内容

	count := counter.GetCount()
	if count != 0 {
		t.Errorf("Expected 0 count for empty content, got %d", count)
	}

	finalCount := counter.FinallyCalc()
	if finalCount != 0 {
		t.Errorf("Expected 0 final count for empty content, got %d", finalCount)
	}
}

func TestTokenCounter_CountMessages(t *testing.T) {
	counter, err := NewTokenCounter(CountModeRune, "")
	if err != nil {
		t.Fatalf("Failed to create counter: %v", err)
	}
	defer counter.Close()

	messages := []string{
		"Hello world",
		"How are you?",
		"I'm fine, thank you!",
	}

	count := counter.CountMessages(messages)
	// 应该大于0
	if count <= 0 {
		t.Errorf("Expected positive count, got %d", count)
	}

	// 应该包含消息开销（每条消息约4个token）
	expectedMinCount := len(messages) * 4
	if count < expectedMinCount {
		t.Errorf("Expected at least %d count (message overhead), got %d", expectedMinCount, count)
	}
}

func TestAsyncTokenCounter_PromptAndCompletion(t *testing.T) {
	counter, err := NewAsyncTokenCounter(CountModeRune, "")
	if err != nil {
		t.Fatalf("Failed to create async counter: %v", err)
	}
	defer counter.Close()

	// 设置prompt count
	messages := []string{"Hello", "world"}
	promptCount := counter.CountPromptMessages(messages)
	counter.SetPromptCount(int64(promptCount))

	// 验证prompt count
	if counter.GetPromptCount() <= 0 {
		t.Errorf("Expected positive prompt count, got %d", counter.GetPromptCount())
	}

	// 追加completion内容
	counter.Append("This is a response")

	// 最终计数
	finalCount := counter.FinallyCalc()
	if finalCount <= 0 {
		t.Errorf("Expected positive final count, got %d", finalCount)
	}

	// 验证总计数
	totalCount := counter.GetTotalCount()
	expectedTotal := counter.GetPromptCount() + finalCount
	if totalCount != expectedTotal {
		t.Errorf("Expected total count %d, got %d", expectedTotal, totalCount)
	}
}
