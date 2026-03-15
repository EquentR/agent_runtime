package builtin

import (
	"fmt"

	coretools "github.com/EquentR/agent_runtime/core/tools"
)

func Register(registry *coretools.Registry, options Options) error {
	if registry == nil {
		return fmt.Errorf("registry cannot be nil")
	}

	env, err := normalizeOptions(options)
	if err != nil {
		return err
	}

	return registry.Register(
		newListFilesTool(env),
		newReadFileTool(env),
		newWriteFileTool(env),
		newSearchFileTool(env),
		newGrepFileTool(env),
		newDeleteFileTool(env),
		newMoveFileTool(env),
		newCopyFileTool(env),
		newExecCommandTool(env),
		newCheckCommandTool(env),
		newListProcessesTool(env),
		newKillProcessTool(env),
		newGetSystemInfoTool(env),
		newHTTPRequestTool(env),
		newWebSearchTool(env),
	)
}
