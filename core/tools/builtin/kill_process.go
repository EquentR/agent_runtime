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
		Description: "Terminate a process by PID",
		Source:      "builtin",
		Parameters: objectSchema([]string{"pid"}, map[string]types.SchemaProperty{
			"pid": {Type: "integer", Description: "Process ID to terminate"},
		}),
		ApprovalMode: types.ToolApprovalModeAlways,
		ApprovalEvaluator: func(arguments map[string]any) coretools.ApprovalRequirement {
			pid, _ := intArg(arguments, "pid", 0)
			return coretools.ApprovalRequirement{
				ArgumentsSummary: fmt.Sprintf("pid=%d", pid),
				RiskLevel:        coretools.RiskLevelHigh,
				Reason:           "terminates a running process",
			}
		},
		Handler: func(_ context.Context, arguments map[string]any) (string, error) {
			pid, err := intArg(arguments, "pid", 0)
			if err != nil {
				return "", err
			}
			if pid <= 0 {
				return "", fmt.Errorf("pid must be > 0")
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
