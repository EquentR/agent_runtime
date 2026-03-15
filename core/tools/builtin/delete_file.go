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
		Description: "Delete a file after explicit confirmation",
		Source:      "builtin",
		Parameters: objectSchema([]string{"path", "confirm"}, map[string]types.SchemaProperty{
			"path":    {Type: "string", Description: "File path relative to workspace"},
			"confirm": {Type: "boolean", Description: "Must be true to confirm deletion"},
		}),
		Handler: func(_ context.Context, arguments map[string]any) (string, error) {
			pathArg, err := requiredStringArg(arguments, "path")
			if err != nil {
				return "", err
			}
			confirm, err := boolArg(arguments, "confirm", false)
			if err != nil {
				return "", err
			}
			if !confirm {
				return "", fmt.Errorf("delete_file requires confirm=true")
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
