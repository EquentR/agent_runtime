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

func newDeleteFileTool(env runtimeEnv) coretools.Tool {
	return coretools.Tool{
		Name:        "delete_file",
		Description: "Delete a file in the workspace",
		Source:      "builtin",
		Parameters: objectSchema([]string{"path"}, map[string]types.SchemaProperty{
			"path": {Type: "string", Description: "File path relative to workspace"},
		}),
		ApprovalMode: types.ToolApprovalModeAlways,
		ApprovalEvaluator: func(arguments map[string]any) coretools.ApprovalRequirement {
			pathArg, _ := arguments["path"].(string)
			return coretools.ApprovalRequirement{
				ArgumentsSummary: fmt.Sprintf("path=%s", pathArg),
				RiskLevel:        coretools.RiskLevelHigh,
				Reason:           "deletes a workspace file",
			}
		},
		Handler: func(ctx context.Context, arguments map[string]any) (string, error) {
			pathArg, err := requiredStringArg(arguments, "path")
			if err != nil {
				return "", err
			}
			startedAt := time.Now()
			logToolStart(ctx, "delete_file", corelog.String("path", pathArg))

			filePath, relPath, err := env.resolveWorkspaceFile(pathArg, true)
			if err != nil {
				logToolFailure(ctx, "delete_file", err, corelog.String("path", pathArg))
				return "", err
			}
			if err := os.Remove(filePath); err != nil {
				logToolFailure(ctx, "delete_file", err, corelog.String("path", relPath))
				return "", err
			}
			logToolFinish(ctx, "delete_file", corelog.String("path", relPath), corelog.Duration("duration", time.Since(startedAt)))
			return jsonResult(struct {
				Path    string `json:"path"`
				Deleted bool   `json:"deleted"`
			}{Path: relPath, Deleted: true})
		},
	}
}
