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

func newMoveFileTool(env runtimeEnv) coretools.Tool {
	return coretools.Tool{
		Name:        "move_file",
		Description: "Move or rename a file within the workspace",
		Source:      "builtin",
		Parameters: objectSchema([]string{"source", "destination"}, map[string]types.SchemaProperty{
			"source":      {Type: "string", Description: "Source file path relative to workspace"},
			"destination": {Type: "string", Description: "Destination file path relative to workspace"},
			"create_dirs": {Type: "boolean", Description: "Create destination directories if needed"},
			"overwrite":   {Type: "boolean", Description: "Overwrite the destination if it exists"},
		}),
		Handler: func(ctx context.Context, arguments map[string]any) (string, error) {
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
			startedAt := time.Now()
			logToolStart(ctx, "move_file", corelog.String("source", sourceArg), corelog.String("destination", destinationArg), corelog.Bool("overwrite", overwrite))

			sourcePath, sourceRel, err := env.resolveWorkspaceFile(sourceArg, true)
			if err != nil {
				logToolFailure(ctx, "move_file", err, corelog.String("source", sourceArg), corelog.String("destination", destinationArg))
				return "", err
			}
			destinationPath, destinationRel, err := env.resolveWorkspaceFile(destinationArg, false)
			if err != nil {
				logToolFailure(ctx, "move_file", err, corelog.String("source", sourceRel), corelog.String("destination", destinationArg))
				return "", err
			}
			if samePath(sourcePath, destinationPath) {
				return "", fmt.Errorf("source and destination must be different")
			}
			if err := ensureParentDir(destinationPath, createDirs); err != nil {
				logToolFailure(ctx, "move_file", err, corelog.String("source", sourceRel), corelog.String("destination", destinationRel))
				return "", err
			}

			if _, err := os.Stat(destinationPath); err == nil {
				if !overwrite {
					return "", fmt.Errorf("destination already exists: %s", destinationRel)
				}
				if err := os.Remove(destinationPath); err != nil {
					logToolFailure(ctx, "move_file", err, corelog.String("source", sourceRel), corelog.String("destination", destinationRel))
					return "", err
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				logToolFailure(ctx, "move_file", err, corelog.String("source", sourceRel), corelog.String("destination", destinationRel))
				return "", err
			}

			if err := os.Rename(sourcePath, destinationPath); err != nil {
				if !isCrossDeviceError(err) {
					logToolFailure(ctx, "move_file", err, corelog.String("source", sourceRel), corelog.String("destination", destinationRel))
					return "", err
				}
				if err := copyFileContents(sourcePath, destinationPath); err != nil {
					logToolFailure(ctx, "move_file", err, corelog.String("source", sourceRel), corelog.String("destination", destinationRel))
					return "", err
				}
				if err := os.Remove(sourcePath); err != nil {
					logToolFailure(ctx, "move_file", err, corelog.String("source", sourceRel), corelog.String("destination", destinationRel))
					return "", err
				}
			}
			logToolFinish(ctx, "move_file", corelog.String("source", sourceRel), corelog.String("destination", destinationRel), corelog.Duration("duration", time.Since(startedAt)))

			return jsonResult(struct {
				Source      string `json:"source"`
				Destination string `json:"destination"`
				Moved       bool   `json:"moved"`
			}{Source: sourceRel, Destination: destinationRel, Moved: true})
		},
	}
}
