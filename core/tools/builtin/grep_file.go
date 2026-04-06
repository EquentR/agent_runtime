package builtin

import (
	"context"
	"os"

	coretools "github.com/EquentR/agent_runtime/core/tools"
	"github.com/EquentR/agent_runtime/core/types"
)

func newGrepFileTool(env runtimeEnv) coretools.Tool {
	return coretools.Tool{
		Name:        "grep_file",
		Description: "Search for matching lines in a single file",
		Source:      "builtin",
		Parameters: objectSchema([]string{"path", "pattern"}, map[string]types.SchemaProperty{
			"path":      {Type: "string", Description: "File path relative to workspace"},
			"pattern":   {Type: "string", Description: "Text or regular expression to search for"},
			"use_regex": {Type: "boolean", Description: "Treat pattern as a regular expression"},
		}),
		Handler: func(_ context.Context, arguments map[string]any) (string, error) {
			pathArg, err := requiredStringArg(arguments, "path")
			if err != nil {
				return "", err
			}
			pattern, err := requiredStringArg(arguments, "pattern")
			if err != nil {
				return "", err
			}
			useRegex, err := boolArg(arguments, "use_regex", false)
			if err != nil {
				return "", err
			}

			matcher, err := newLineMatcher(pattern, useRegex)
			if err != nil {
				return "", err
			}
			filePath, relPath, err := env.resolveWorkspaceFile(pathArg, true)
			if err != nil {
				return "", err
			}

			data, err := os.ReadFile(filePath)
			if err != nil {
				return "", err
			}

			matches := findLineMatches(string(data), matcher)
			trimmed, truncated := trimSlice(matches, env.outputBudget.searchMaxMatches)
			for i := range trimmed {
				trimmed[i].Text = truncateMatchText(trimmed[i].Text, env.outputBudget.matchTextMaxBytes)
			}

			return jsonResult(struct {
				Path            string      `json:"path"`
				Matches         []lineMatch `json:"matches"`
				TotalMatches    int         `json:"total_matches"`
				ReturnedMatches int         `json:"returned_matches"`
				Truncated       bool        `json:"truncated"`
			}{Path: relPath, Matches: trimmed, TotalMatches: len(matches), ReturnedMatches: len(trimmed), Truncated: truncated})
		},
	}
}
