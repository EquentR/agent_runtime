package model

import (
	"strings"
	"unicode"
)

const (
	thinkOpenTag  = "<think>"
	thinkCloseTag = "</think>"
)

func SplitLeadingThinkBlock(content string) (string, string) {
	splitter := NewLeadingThinkStreamSplitter()
	out := splitter.Consume(content)
	out += splitter.Finalize()
	return splitter.Reasoning(), out
}

func JoinReasoning(parts ...string) string {
	seen := make(map[string]struct{}, len(parts))
	merged := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		merged = append(merged, trimmed)
	}
	return strings.Join(merged, "\n")
}

type LeadingThinkStreamSplitter struct {
	buffer    string
	reasoning string
	done      bool
}

func NewLeadingThinkStreamSplitter() *LeadingThinkStreamSplitter {
	return &LeadingThinkStreamSplitter{}
}

func (s *LeadingThinkStreamSplitter) Consume(chunk string) string {
	if chunk == "" {
		return ""
	}
	if s.done {
		return chunk
	}

	s.buffer += chunk
	trimmedOffset := leadingWhitespaceOffset(s.buffer)
	rest := s.buffer[trimmedOffset:]
	if rest == "" {
		return ""
	}
	if strings.HasPrefix(rest, thinkOpenTag) {
		closeIdx := strings.Index(rest[len(thinkOpenTag):], thinkCloseTag)
		if closeIdx < 0 {
			return ""
		}
		s.reasoning = strings.TrimSpace(rest[len(thinkOpenTag) : len(thinkOpenTag)+closeIdx])
		answer := rest[len(thinkOpenTag)+closeIdx+len(thinkCloseTag):]
		s.buffer = ""
		s.done = true
		return answer
	}
	if strings.HasPrefix(thinkOpenTag, rest) {
		return ""
	}

	out := s.buffer
	s.buffer = ""
	s.done = true
	return out
}

func (s *LeadingThinkStreamSplitter) Finalize() string {
	if s.done {
		return ""
	}
	out := s.buffer
	s.buffer = ""
	s.done = true
	return out
}

func (s *LeadingThinkStreamSplitter) Reasoning() string {
	return s.reasoning
}

func leadingWhitespaceOffset(text string) int {
	for idx, r := range text {
		if !unicode.IsSpace(r) {
			return idx
		}
	}
	return len(text)
}
