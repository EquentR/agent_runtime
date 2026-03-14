package model

import "testing"

func TestSplitLeadingThinkBlock_StripsOnlyLeadingFirstBlock(t *testing.T) {
	reasoning, answer := SplitLeadingThinkBlock("<think>plan first</think>Answer <think>keep</think>")

	if reasoning != "plan first" {
		t.Fatalf("reasoning = %q, want %q", reasoning, "plan first")
	}
	if answer != "Answer <think>keep</think>" {
		t.Fatalf("answer = %q, want %q", answer, "Answer <think>keep</think>")
	}
}

func TestSplitLeadingThinkBlock_IgnoresNonLeadingThinkBlock(t *testing.T) {
	reasoning, answer := SplitLeadingThinkBlock("Answer <think>keep</think>")

	if reasoning != "" {
		t.Fatalf("reasoning = %q, want empty", reasoning)
	}
	if answer != "Answer <think>keep</think>" {
		t.Fatalf("answer = %q, want original content", answer)
	}
}

func TestLeadingThinkStreamSplitter_SeparatesReasoningAcrossChunks(t *testing.T) {
	splitter := NewLeadingThinkStreamSplitter()

	if got := splitter.Consume("<thi"); got != "" {
		t.Fatalf("first chunk = %q, want empty", got)
	}
	if got := splitter.Consume("nk>plan first</think>An"); got != "An" {
		t.Fatalf("second chunk = %q, want %q", got, "An")
	}
	if got := splitter.Consume("swer"); got != "swer" {
		t.Fatalf("third chunk = %q, want %q", got, "swer")
	}
	if got := splitter.Finalize(); got != "" {
		t.Fatalf("finalize = %q, want empty", got)
	}
	if splitter.Reasoning() != "plan first" {
		t.Fatalf("reasoning = %q, want %q", splitter.Reasoning(), "plan first")
	}
}

func TestLeadingThinkStreamSplitter_FlushesRawContentWhenThinkUnclosed(t *testing.T) {
	splitter := NewLeadingThinkStreamSplitter()
	if got := splitter.Consume("<think>plan"); got != "" {
		t.Fatalf("consume = %q, want empty", got)
	}
	if got := splitter.Finalize(); got != "<think>plan" {
		t.Fatalf("finalize = %q, want raw content", got)
	}
	if splitter.Reasoning() != "" {
		t.Fatalf("reasoning = %q, want empty", splitter.Reasoning())
	}
}
