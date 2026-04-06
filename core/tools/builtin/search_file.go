package builtin

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	coretools "github.com/EquentR/agent_runtime/core/tools"
	"github.com/EquentR/agent_runtime/core/types"
)

func newSearchFileTool(env runtimeEnv) coretools.Tool {
	return coretools.Tool{
		Name:        "search_file",
		Description: "Search for matching text across files in a directory",
		Source:      "builtin",
		Parameters: objectSchema([]string{"path", "pattern"}, map[string]types.SchemaProperty{
			"path":      {Type: "string", Description: "Directory path relative to workspace"},
			"pattern":   {Type: "string", Description: "Text or regular expression to search for"},
			"use_regex": {Type: "boolean", Description: "Treat pattern as a regular expression"},
			"max_depth": {Type: "integer", Description: "Maximum depth relative to the requested directory"},
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
			maxDepth, err := intArg(arguments, "max_depth", 0)
			if err != nil {
				return "", err
			}

			matcher, err := newLineMatcher(pattern, useRegex)
			if err != nil {
				return "", err
			}
			dirPath, _, err := env.resolveWorkspaceDir(pathArg, true)
			if err != nil {
				return "", err
			}

			type match struct {
				Path string `json:"path"`
				Line int    `json:"line"`
				Text string `json:"text"`
			}

			matches := make([]match, 0)
			totalMatches := 0
			err = filepath.WalkDir(dirPath, func(path string, d fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if path == dirPath {
					return nil
				}
				relToDir, err := filepath.Rel(dirPath, path)
				if err != nil {
					return err
				}
				depth := strings.Count(filepath.ToSlash(relToDir), "/") + 1

				if d.Type()&os.ModeSymlink != 0 {
					if d.IsDir() {
						return fs.SkipDir
					}
					return nil
				}
				if maxDepth > 0 && depth > maxDepth {
					if d.IsDir() {
						return fs.SkipDir
					}
					return nil
				}
				if d.IsDir() {
					if maxDepth > 0 && depth == maxDepth {
						return fs.SkipDir
					}
					return nil
				}

				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				relToWorkspace, err := filepath.Rel(env.workspaceRoot, path)
				if err != nil {
					return err
				}
				for _, lineMatch := range findLineMatches(string(data), matcher) {
					totalMatches++
					matches = append(matches, match{Path: filepath.ToSlash(relToWorkspace), Line: lineMatch.Line, Text: truncateMatchText(lineMatch.Text, env.outputBudget.matchTextMaxBytes)})
				}
				return nil
			})
			if err != nil {
				return "", err
			}

			sort.Slice(matches, func(i int, j int) bool {
				if matches[i].Path == matches[j].Path {
					return matches[i].Line < matches[j].Line
				}
				return matches[i].Path < matches[j].Path
			})
			trimmed, truncated := trimSlice(matches, env.outputBudget.searchMaxMatches)
			return jsonResult(struct {
				Matches         []match `json:"matches"`
				TotalMatches    int     `json:"total_matches"`
				ReturnedMatches int     `json:"returned_matches"`
				Truncated       bool    `json:"truncated"`
			}{Matches: trimmed, TotalMatches: totalMatches, ReturnedMatches: len(trimmed), Truncated: truncated})
		},
	}
}
