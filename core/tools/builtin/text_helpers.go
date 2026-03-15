package builtin

import (
	"fmt"
	"regexp"
	"strings"
)

type lineMatch struct {
	Line int    `json:"line"`
	Text string `json:"text"`
}

func splitLinesWithEndings(text string) []string {
	if text == "" {
		return nil
	}

	lines := make([]string, 0, strings.Count(text, "\n")+1)
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			lines = append(lines, text[start:i+1])
			start = i + 1
		}
	}
	if start < len(text) {
		lines = append(lines, text[start:])
	}
	return lines
}

func joinLines(lines []string) string {
	return strings.Join(lines, "")
}

func newLineMatcher(pattern string, useRegex bool) (func(string) bool, error) {
	if useRegex {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("compile regex: %w", err)
		}
		return re.MatchString, nil
	}
	return func(line string) bool {
		return strings.Contains(line, pattern)
	}, nil
}

func findLineMatches(text string, matcher func(string) bool) []lineMatch {
	lines := splitLinesWithEndings(text)
	results := make([]lineMatch, 0)
	for index, line := range lines {
		trimmed := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if matcher(trimmed) {
			results = append(results, lineMatch{
				Line: index + 1,
				Text: trimmed,
			})
		}
	}
	return results
}
