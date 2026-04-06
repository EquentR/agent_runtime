package builtin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	corelog "github.com/EquentR/agent_runtime/core/log"
	coretools "github.com/EquentR/agent_runtime/core/tools"
	"github.com/EquentR/agent_runtime/core/types"
)

func newWriteFileTool(env runtimeEnv) coretools.Tool {
	return coretools.Tool{
		Name:        "write_file",
		Description: "Write, insert, append, or replace file content",
		Source:      "builtin",
		Parameters: objectSchema([]string{"path", "content"}, map[string]types.SchemaProperty{
			"path":        {Type: "string", Description: "File path relative to workspace"},
			"content":     {Type: "string", Description: "Content to write"},
			"mode":        {Type: "string", Description: "Write mode", Enum: []string{"overwrite", "append", "insert", "replace_lines"}},
			"start_line":  {Type: "integer", Description: "1-based line number for insert/replace operations"},
			"end_line":    {Type: "integer", Description: "Inclusive end line for replace_lines"},
			"create_dirs": {Type: "boolean", Description: "Create parent directories if needed"},
		}),
		Handler: func(ctx context.Context, arguments map[string]any) (string, error) {
			pathArg, err := requiredStringArg(arguments, "path")
			if err != nil {
				return "", err
			}
			contentValue, ok := arguments["content"]
			if !ok {
				return "", fmt.Errorf("content is required")
			}
			content, ok := contentValue.(string)
			if !ok {
				return "", fmt.Errorf("content must be a string")
			}
			mode, ok, err := optionalStringArg(arguments, "mode")
			if err != nil {
				return "", err
			}
			if !ok || mode == "" {
				mode = "overwrite"
			}
			createDirs, err := boolArg(arguments, "create_dirs", false)
			if err != nil {
				return "", err
			}
			startLine, err := intArg(arguments, "start_line", 1)
			if err != nil {
				return "", err
			}
			endLine, err := intArg(arguments, "end_line", 0)
			if err != nil {
				return "", err
			}

			startedAt := time.Now()
			logToolStart(ctx, "write_file", corelog.String("path", pathArg), corelog.String("mode", mode), corelog.Int("content_length", len(content)))
			filePath, relPath, err := env.resolveWorkspaceFile(pathArg, false)
			if err != nil {
				logToolFailure(ctx, "write_file", err, corelog.String("path", pathArg), corelog.String("mode", mode))
				return "", err
			}
			if err := ensureParentDir(filePath, createDirs); err != nil {
				logToolFailure(ctx, "write_file", err, corelog.String("path", relPath), corelog.String("mode", mode))
				return "", err
			}

			switch mode {
			case "overwrite":
				if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
					logToolFailure(ctx, "write_file", err, corelog.String("path", relPath), corelog.String("mode", mode))
					return "", err
				}
			case "append":
				f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
				if err != nil {
					logToolFailure(ctx, "write_file", err, corelog.String("path", relPath), corelog.String("mode", mode))
					return "", err
				}
				if _, err := f.WriteString(content); err != nil {
					_ = f.Close()
					logToolFailure(ctx, "write_file", err, corelog.String("path", relPath), corelog.String("mode", mode))
					return "", err
				}
				if err := f.Close(); err != nil {
					logToolFailure(ctx, "write_file", err, corelog.String("path", relPath), corelog.String("mode", mode))
					return "", err
				}
			case "insert":
				if startLine < 1 {
					return "", fmt.Errorf("start_line must be >= 1")
				}
				existing, err := os.ReadFile(filePath)
				if err != nil && !errors.Is(err, os.ErrNotExist) {
					logToolFailure(ctx, "write_file", err, corelog.String("path", relPath), corelog.String("mode", mode))
					return "", err
				}
				lines := splitLinesWithEndings(string(existing))
				if startLine > len(lines)+1 {
					return "", fmt.Errorf("start_line is out of range")
				}
				insertLines := splitLinesWithEndings(content)
				updated := append(append(append([]string(nil), lines[:startLine-1]...), insertLines...), lines[startLine-1:]...)
				if err := os.WriteFile(filePath, []byte(joinLines(updated)), 0o644); err != nil {
					logToolFailure(ctx, "write_file", err, corelog.String("path", relPath), corelog.String("mode", mode))
					return "", err
				}
			case "replace_lines":
				if startLine < 1 || endLine < startLine {
					return "", fmt.Errorf("invalid start_line/end_line range")
				}
				existing, err := os.ReadFile(filePath)
				if err != nil {
					logToolFailure(ctx, "write_file", err, corelog.String("path", relPath), corelog.String("mode", mode))
					return "", err
				}
				lines := splitLinesWithEndings(string(existing))
				if endLine > len(lines) {
					return "", fmt.Errorf("end_line is out of range")
				}
				replacementLines := splitLinesWithEndings(content)
				updated := append(append(append([]string(nil), lines[:startLine-1]...), replacementLines...), lines[endLine:]...)
				if err := os.WriteFile(filePath, []byte(joinLines(updated)), 0o644); err != nil {
					logToolFailure(ctx, "write_file", err, corelog.String("path", relPath), corelog.String("mode", mode))
					return "", err
				}
			default:
				return "", fmt.Errorf("unsupported write mode: %s", mode)
			}
			logToolFinish(ctx, "write_file", corelog.String("path", relPath), corelog.String("mode", mode), corelog.Int("content_length", len(content)), corelog.Duration("duration", time.Since(startedAt)))

			return jsonResult(struct {
				Path    string `json:"path"`
				Mode    string `json:"mode"`
				Updated bool   `json:"updated"`
			}{Path: relPath, Mode: mode, Updated: true})
		},
	}
}
