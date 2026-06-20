package builtin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/EquentR/agent_runtime/core/workspaces"
)

func workspaceModeFromRoot(root string) workspaces.Mode {
	trimmedRoot := strings.TrimSpace(root)
	if trimmedRoot == "" {
		return workspaces.ModeMutable
	}

	statePath := filepath.Join(trimmedRoot, workspaces.StateFileName)
	data, err := os.ReadFile(statePath)
	if err != nil {
		return workspaces.ModeMutable
	}

	var state struct {
		Mode workspaces.Mode `json:"mode"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return workspaces.ModeMutable
	}
	if state.Mode == workspaces.ModeReadonly {
		return workspaces.ModeReadonly
	}
	return workspaces.ModeMutable
}

func WorkspaceModeFromRoot(root string) workspaces.Mode {
	return workspaceModeFromRoot(root)
}
