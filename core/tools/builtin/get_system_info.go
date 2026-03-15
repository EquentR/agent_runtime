package builtin

import (
	"context"
	"os"
	"runtime"

	coretools "github.com/EquentR/agent_runtime/core/tools"
	"github.com/EquentR/agent_runtime/core/types"
)

func newGetSystemInfoTool(_ runtimeEnv) coretools.Tool {
	return coretools.Tool{
		Name:        "get_system_info",
		Description: "Get basic system and runtime information",
		Source:      "builtin",
		Parameters:  objectSchema(nil, map[string]types.SchemaProperty{}),
		Handler: func(_ context.Context, _ map[string]any) (string, error) {
			hostname, err := os.Hostname()
			if err != nil {
				return "", err
			}
			return jsonResult(struct {
				OS        string `json:"os"`
				Arch      string `json:"arch"`
				Hostname  string `json:"hostname"`
				NumCPU    int    `json:"num_cpu"`
				GoVersion string `json:"go_version"`
			}{
				OS:        runtime.GOOS,
				Arch:      runtime.GOARCH,
				Hostname:  hostname,
				NumCPU:    runtime.NumCPU(),
				GoVersion: runtime.Version(),
			})
		},
	}
}
