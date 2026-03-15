package builtin

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"

	coretools "github.com/EquentR/agent_runtime/core/tools"
	"github.com/EquentR/agent_runtime/core/types"
)

func newCheckCommandTool(env runtimeEnv) coretools.Tool {
	return coretools.Tool{
		Name:        "check_command",
		Description: "Check whether a command exists and optionally query its version",
		Source:      "builtin",
		Parameters: objectSchema([]string{"name"}, map[string]types.SchemaProperty{
			"name":         {Type: "string", Description: "Command name to look up"},
			"version_args": {Type: "array", Description: "Optional version arguments"},
		}),
		Handler: func(ctx context.Context, arguments map[string]any) (string, error) {
			name, err := requiredStringArg(arguments, "name")
			if err != nil {
				return "", err
			}
			versionArgs, err := stringSliceArg(arguments, "version_args")
			if err != nil {
				return "", err
			}

			path, err := exec.LookPath(name)
			if err != nil {
				return jsonResult(struct {
					Found   bool   `json:"found"`
					Path    string `json:"path"`
					Version string `json:"version"`
				}{Found: false, Path: "", Version: ""})
			}

			result := struct {
				Found   bool   `json:"found"`
				Path    string `json:"path"`
				Version string `json:"version"`
			}{Found: true, Path: path}

			if len(versionArgs) > 0 {
				versionCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
				defer cancel()
				cmd := exec.CommandContext(versionCtx, path, versionArgs...)
				var output bytes.Buffer
				cmd.Stdout = &output
				cmd.Stderr = &output
				_ = cmd.Run()
				result.Version = strings.TrimSpace(output.String())
			}

			return jsonResult(result)
		},
	}
}
