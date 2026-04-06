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

func newListFilesTool(env runtimeEnv) coretools.Tool {
	return coretools.Tool{
		Name:        "list_files",
		Description: "List files in a workspace directory",
		Source:      "builtin",
		Parameters: objectSchema([]string{"path"}, map[string]types.SchemaProperty{
			"path":      {Type: "string", Description: "Directory path relative to workspace"},
			"recursive": {Type: "boolean", Description: "Whether to recurse into subdirectories"},
			"max_depth": {Type: "integer", Description: "Maximum depth relative to the requested directory"},
		}),
		Handler: func(_ context.Context, arguments map[string]any) (string, error) {
			pathArg, err := requiredStringArg(arguments, "path")
			if err != nil {
				return "", err
			}
			recursive, err := boolArg(arguments, "recursive", false)
			if err != nil {
				return "", err
			}
			maxDepth, err := intArg(arguments, "max_depth", 0)
			if err != nil {
				return "", err
			}

			dirPath, _, err := env.resolveWorkspaceDir(pathArg, true)
			if err != nil {
				return "", err
			}

			if !recursive {
				maxDepth = 1
			}

			type entry struct {
				Path string `json:"path"`
				Type string `json:"type"`
			}

			entries := make([]entry, 0)
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

				relToWorkspace, err := filepath.Rel(env.workspaceRoot, path)
				if err != nil {
					return err
				}
				entryType := "file"
				if d.IsDir() {
					entryType = "dir"
				}
				entries = append(entries, entry{Path: filepath.ToSlash(relToWorkspace), Type: entryType})

				if d.IsDir() && maxDepth > 0 && depth == maxDepth {
					return fs.SkipDir
				}
				return nil
			})
			if err != nil {
				return "", err
			}

			sort.Slice(entries, func(i int, j int) bool {
				return entries[i].Path < entries[j].Path
			})
			trimmed, truncated := trimSlice(entries, env.outputBudget.listMaxEntries)
			remainingCount := 0
			if truncated {
				remainingCount = len(entries) - len(trimmed)
			}
			return jsonResult(struct {
				Entries         []entry `json:"entries"`
				ReturnedEntries int     `json:"returned_entries"`
				RemainingCount  int     `json:"remaining_count"`
				Truncated       bool    `json:"truncated"`
			}{Entries: trimmed, ReturnedEntries: len(trimmed), RemainingCount: remainingCount, Truncated: truncated})
		},
	}
}
