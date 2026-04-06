package builtin

import (
	"context"
	"fmt"
	"os"
	"time"

	corelog "github.com/EquentR/agent_runtime/core/log"
	coretools "github.com/EquentR/agent_runtime/core/tools"
	"github.com/EquentR/agent_runtime/core/types"
)

func newReadFileTool(env runtimeEnv) coretools.Tool {
	return coretools.Tool{
		Name:        "read_file",
		Description: "Read file contents with optional line window",
		Source:      "builtin",
		Parameters: objectSchema([]string{"path"}, map[string]types.SchemaProperty{
			"path":       {Type: "string", Description: "File path relative to workspace"},
			"start_line": {Type: "integer", Description: "1-based line number to start from"},
			"line_count": {Type: "integer", Description: "Maximum number of lines to read"},
		}),
		Handler: func(ctx context.Context, arguments map[string]any) (string, error) {
			pathArg, err := requiredStringArg(arguments, "path")
			if err != nil {
				return "", err
			}
			startLine, err := intArg(arguments, "start_line", 1)
			if err != nil {
				return "", err
			}
			lineCount, err := intArg(arguments, "line_count", 0)
			if err != nil {
				return "", err
			}
			if startLine < 1 {
				return "", fmt.Errorf("start_line must be >= 1")
			}
			if lineCount < 0 {
				return "", fmt.Errorf("line_count must be >= 0")
			}

			startedAt := time.Now()
			logToolStart(ctx, "read_file", corelog.String("path", pathArg), corelog.Int("start_line", startLine), corelog.Int("line_count", lineCount))
			filePath, relPath, err := env.resolveWorkspaceFile(pathArg, true)
			if err != nil {
				logToolFailure(ctx, "read_file", err, corelog.String("path", pathArg))
				return "", err
			}

			data, err := os.ReadFile(filePath)
			if err != nil {
				logToolFailure(ctx, "read_file", err, corelog.String("path", relPath))
				return "", err
			}
			lines := splitLinesWithEndings(string(data))
			totalLines := len(lines)
			startIndex := startLine - 1
			if startIndex > totalLines {
				startIndex = totalLines
			}
			endIndex := totalLines
			if lineCount > 0 && startIndex+lineCount < endIndex {
				endIndex = startIndex + lineCount
			}

			content := ""
			if startIndex < endIndex {
				content = joinLines(lines[startIndex:endIndex])
			}
			logToolFinish(ctx, "read_file", corelog.String("path", relPath), corelog.Int("total_lines", totalLines), corelog.Int("content_length", len(content)), corelog.Duration("duration", time.Since(startedAt)))

			return jsonResult(struct {
				Path       string `json:"path"`
				StartLine  int    `json:"start_line"`
				EndLine    int    `json:"end_line"`
				TotalLines int    `json:"total_lines"`
				Content    string `json:"content"`
			}{
				Path:       relPath,
				StartLine:  startLine,
				EndLine:    endIndex,
				TotalLines: totalLines,
				Content:    content,
			})
		},
	}
}
