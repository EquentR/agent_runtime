package builtin

import "unicode/utf8"

const (
	defaultReadFileLineCount   = 300
	defaultReadFileMaxLines    = 300
	defaultCommandStdoutBytes  = 16 * 1024
	defaultCommandStderrBytes  = 8 * 1024
	defaultTextResultMaxBytes  = 16 * 1024
	defaultSearchMaxMatches    = 100
	defaultListMaxEntries      = 200
	defaultWebSearchMaxResults = 10
	defaultMatchTextMaxBytes   = 256

	limitReasonLineLimit   = "line_limit"
	limitReasonByteLimit   = "byte_limit"
	limitReasonMatchLimit  = "match_limit"
	limitReasonEntryLimit  = "entry_limit"
	limitReasonResultLimit = "result_limit"
)

type OutputBudgetOptions struct {
	ReadFileDefaultLineCount int
	ReadFileMaxLineCount     int
	CommandStdoutBytes       int
	CommandStderrBytes       int
	TextResultMaxBytes       int
	SearchMaxMatches         int
	ListMaxEntries           int
	WebSearchMaxResults      int
	MatchTextMaxBytes        int
}

type outputBudgetConfig struct {
	readFileDefaultLineCount int
	readFileMaxLineCount     int
	commandStdoutBytes       int
	commandStderrBytes       int
	textResultMaxBytes       int
	searchMaxMatches         int
	listMaxEntries           int
	webSearchMaxResults      int
	matchTextMaxBytes        int
}

type continuationMetadata struct {
	HasMore       bool   `json:"has_more"`
	NextStartLine *int   `json:"next_start_line,omitempty"`
	Truncated     bool   `json:"truncated"`
	LimitReason   string `json:"limit_reason,omitempty"`
}

type boundedCommandOutput struct {
	Stdout              string
	Stderr              string
	StdoutTruncated     bool
	StderrTruncated     bool
	OriginalStdoutBytes int
	OriginalStderrBytes int
	ReturnedStdoutBytes int
	ReturnedStderrBytes int
}

type textBudgetResult struct {
	Text         string
	Truncated    bool
	LimitReason  string
	OriginalSize int
	ReturnedSize int
}

func normalizeOutputBudgetOptions(options OutputBudgetOptions) outputBudgetConfig {
	config := outputBudgetConfig{
		readFileDefaultLineCount: defaultReadFileLineCount,
		readFileMaxLineCount:     defaultReadFileMaxLines,
		commandStdoutBytes:       defaultCommandStdoutBytes,
		commandStderrBytes:       defaultCommandStderrBytes,
		textResultMaxBytes:       defaultTextResultMaxBytes,
		searchMaxMatches:         defaultSearchMaxMatches,
		listMaxEntries:           defaultListMaxEntries,
		webSearchMaxResults:      defaultWebSearchMaxResults,
		matchTextMaxBytes:        defaultMatchTextMaxBytes,
	}
	if options.ReadFileDefaultLineCount > 0 {
		config.readFileDefaultLineCount = options.ReadFileDefaultLineCount
	}
	if options.ReadFileMaxLineCount > 0 {
		config.readFileMaxLineCount = options.ReadFileMaxLineCount
	}
	if options.CommandStdoutBytes > 0 {
		config.commandStdoutBytes = options.CommandStdoutBytes
	}
	if options.CommandStderrBytes > 0 {
		config.commandStderrBytes = options.CommandStderrBytes
	}
	if options.TextResultMaxBytes > 0 {
		config.textResultMaxBytes = options.TextResultMaxBytes
	}
	if options.SearchMaxMatches > 0 {
		config.searchMaxMatches = options.SearchMaxMatches
	}
	if options.ListMaxEntries > 0 {
		config.listMaxEntries = options.ListMaxEntries
	}
	if options.WebSearchMaxResults > 0 {
		config.webSearchMaxResults = options.WebSearchMaxResults
	}
	if options.MatchTextMaxBytes > 0 {
		config.matchTextMaxBytes = options.MatchTextMaxBytes
	}
	if config.readFileMaxLineCount < 1 {
		config.readFileMaxLineCount = defaultReadFileMaxLines
	}
	if config.readFileDefaultLineCount < 1 {
		config.readFileDefaultLineCount = defaultReadFileLineCount
	}
	if config.readFileDefaultLineCount > config.readFileMaxLineCount {
		config.readFileDefaultLineCount = config.readFileMaxLineCount
	}
	return config
}

func (c outputBudgetConfig) resolveReadFileLineCount(requested int, provided bool) (int, string) {
	if !provided {
		return c.readFileDefaultLineCount, limitReasonLineLimit
	}
	if requested <= 0 {
		return c.readFileDefaultLineCount, limitReasonLineLimit
	}
	if requested > c.readFileMaxLineCount {
		return c.readFileMaxLineCount, limitReasonLineLimit
	}
	return requested, ""
}

func newContinuationMetadata(endIndex int, totalLines int, limitReason string) continuationMetadata {
	metadata := continuationMetadata{}
	if endIndex < totalLines {
		nextStartLine := endIndex + 1
		metadata.HasMore = true
		metadata.NextStartLine = &nextStartLine
		metadata.Truncated = true
		metadata.LimitReason = limitReason
	}
	return metadata
}

func limitTextByBytes(text string, maxBytes int) textBudgetResult {
	result := textBudgetResult{Text: text, OriginalSize: len(text), ReturnedSize: len(text)}
	if maxBytes <= 0 || len(text) <= maxBytes {
		return result
	}
	trimmed := text[:maxBytes]
	for len(trimmed) > 0 && !utf8.ValidString(trimmed) {
		trimmed = trimmed[:len(trimmed)-1]
	}
	result.Text = trimmed
	result.Truncated = true
	result.LimitReason = limitReasonByteLimit
	result.ReturnedSize = len(trimmed)
	return result
}

func truncateMatchText(text string, maxBytes int) string {
	return limitTextByBytes(text, maxBytes).Text
}

func trimSlice[T any](items []T, max int) ([]T, bool) {
	if max <= 0 || len(items) <= max {
		return items, false
	}
	return append([]T(nil), items[:max]...), true
}

func shapeCommandOutput(stdout, stderr string, config outputBudgetConfig) boundedCommandOutput {
	stdoutResult := limitTextByBytes(stdout, config.commandStdoutBytes)
	stderrResult := limitTextByBytes(stderr, config.commandStderrBytes)
	return boundedCommandOutput{
		Stdout:              stdoutResult.Text,
		Stderr:              stderrResult.Text,
		StdoutTruncated:     stdoutResult.Truncated,
		StderrTruncated:     stderrResult.Truncated,
		OriginalStdoutBytes: stdoutResult.OriginalSize,
		OriginalStderrBytes: stderrResult.OriginalSize,
		ReturnedStdoutBytes: stdoutResult.ReturnedSize,
		ReturnedStderrBytes: stderrResult.ReturnedSize,
	}
}
