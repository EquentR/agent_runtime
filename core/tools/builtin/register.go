package builtin

import (
	"fmt"

	coretools "github.com/EquentR/agent_runtime/core/tools"
	"github.com/EquentR/agent_runtime/core/workspaces"
)

func Register(registry *coretools.Registry, options Options) error {
	if registry == nil {
		return fmt.Errorf("registry cannot be nil")
	}

	env, err := normalizeOptions(options)
	if err != nil {
		return err
	}

	return registry.Register(visibleBuiltinTools(env)...)
}

func visibleBuiltinTools(env runtimeEnv) []coretools.Tool {
	tools := []coretools.Tool{
		newListFilesTool(env),
		newReadFileTool(env),
		newWriteFileTool(env),
		newSearchFileTool(env),
		newGrepFileTool(env),
		newDeleteFileTool(env),
		newMoveFileTool(env),
		newCopyFileTool(env),
		newAskUserTool(env),
		newUsingSkillsTool(env),
		newExecCommandTool(env),
		newCheckCommandTool(env),
		newListProcessesTool(env),
		newKillProcessTool(env),
		newGetSystemInfoTool(env),
		newHTTPRequestTool(env),
		newWebSearchTool(env),
		newEditImageTool(env),
		newGenerateImageTool(env),
	}
	if env.workspaceMode != workspaces.ModeReadonly {
		return tools
	}

	visible := make([]coretools.Tool, 0, len(tools))
	for _, tool := range tools {
		if readonlyVisibleTool(tool.Name) {
			visible = append(visible, tool)
		}
	}
	return visible
}

func readonlyVisibleTool(name string) bool {
	switch name {
	case "write_file", "delete_file", "move_file", "copy_file", "kill_process":
		return false
	default:
		return true
	}
}
