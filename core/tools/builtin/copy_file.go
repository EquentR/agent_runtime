package builtin

import (
	"context"
	"errors"
	"fmt"
	"os"

	coretools "github.com/EquentR/agent_runtime/core/tools"
	"github.com/EquentR/agent_runtime/core/types"
)

func newCopyFileTool(env runtimeEnv) coretools.Tool {
	return coretools.Tool{
		Name:        "copy_file",
		Description: "Copy a file within the workspace",
		Source:      "builtin",
		Parameters: objectSchema([]string{"source", "destination"}, map[string]types.SchemaProperty{
			"source":      {Type: "string", Description: "Source file path relative to workspace"},
			"destination": {Type: "string", Description: "Destination file path relative to workspace"},
			"create_dirs": {Type: "boolean", Description: "Create destination directories if needed"},
			"overwrite":   {Type: "boolean", Description: "Overwrite the destination if it exists"},
		}),
		Handler: func(_ context.Context, arguments map[string]any) (string, error) {
			sourceArg, err := requiredStringArg(arguments, "source")
			if err != nil {
				return "", err
			}
			destinationArg, err := requiredStringArg(arguments, "destination")
			if err != nil {
				return "", err
			}
			createDirs, err := boolArg(arguments, "create_dirs", false)
			if err != nil {
				return "", err
			}
			overwrite, err := boolArg(arguments, "overwrite", false)
			if err != nil {
				return "", err
			}

			sourcePath, sourceRel, err := env.resolveWorkspaceFile(sourceArg, true)
			if err != nil {
				return "", err
			}
			destinationPath, destinationRel, err := env.resolveWorkspaceFile(destinationArg, false)
			if err != nil {
				return "", err
			}
			if samePath(sourcePath, destinationPath) {
				return "", fmt.Errorf("source and destination must be different")
			}
			if err := ensureParentDir(destinationPath, createDirs); err != nil {
				return "", err
			}

			if _, err := os.Stat(destinationPath); err == nil {
				if !overwrite {
					return "", fmt.Errorf("destination already exists: %s", destinationRel)
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				return "", err
			}

			if err := copyFileContents(sourcePath, destinationPath); err != nil {
				return "", err
			}
			return jsonResult(struct {
				Source      string `json:"source"`
				Destination string `json:"destination"`
				Copied      bool   `json:"copied"`
			}{Source: sourceRel, Destination: destinationRel, Copied: true})
		},
	}
}
