package builtin

import (
	"context"
	"fmt"
	"os"

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
		Handler: func(_ context.Context, arguments map[string]any) (string, error) {
			pathArg, err := requiredStringArg(arguments, "path")
			if err != nil {
				return "", err
			}

			filePath, relPath, err := env.resolveWorkspaceFile(pathArg, true)
			if err != nil {
				return "", err
			}
			if err := os.Remove(filePath); err != nil {
				return "", err
			}
			return jsonResult(struct {
				Path    string `json:"path"`
				Deleted bool   `json:"deleted"`
			}{Path: relPath, Deleted: true})
		},
	}
}
