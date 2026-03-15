package builtin

import (
	"context"
	"fmt"
	"os"

	coretools "github.com/EquentR/agent_runtime/core/tools"
	"github.com/EquentR/agent_runtime/core/types"
)

func newKillProcessTool(_ runtimeEnv) coretools.Tool {
	return coretools.Tool{
		Name:        "kill_process",
		Description: "Terminate a process after explicit confirmation",
		Source:      "builtin",
		Parameters: objectSchema([]string{"pid", "confirm"}, map[string]types.SchemaProperty{
			"pid":     {Type: "integer", Description: "Process ID to terminate"},
			"confirm": {Type: "boolean", Description: "Must be true to confirm termination"},
		}),
		Handler: func(_ context.Context, arguments map[string]any) (string, error) {
			pid, err := intArg(arguments, "pid", 0)
			if err != nil {
				return "", err
			}
			if pid <= 0 {
				return "", fmt.Errorf("pid must be > 0")
			}
			confirm, err := boolArg(arguments, "confirm", false)
			if err != nil {
				return "", err
			}
			if !confirm {
				return "", fmt.Errorf("kill_process requires confirm=true")
			}

			process, err := os.FindProcess(pid)
			if err != nil {
				return "", err
			}
			if err := process.Kill(); err != nil {
				return "", err
			}
			return jsonResult(struct {
				Success bool `json:"success"`
				PID     int  `json:"pid"`
			}{Success: true, PID: pid})
		},
	}
}
