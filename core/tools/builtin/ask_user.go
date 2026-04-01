package builtin

import (
	"context"
	"fmt"

	coretools "github.com/EquentR/agent_runtime/core/tools"
	"github.com/EquentR/agent_runtime/core/types"
)

func newAskUserTool(runtimeEnv) coretools.Tool {
	return coretools.Tool{
		Name:        "ask_user",
		Description: "Request structured human clarification and pause the current task",
		Source:      "builtin",
		Parameters: objectSchema([]string{"question"}, map[string]types.SchemaProperty{
			"question":     {Type: "string", Description: "Question shown to the human responder"},
			"options":      {Type: "array", Description: "Optional choice labels presented to the user", Items: &types.SchemaProperty{Type: "string"}},
			"allow_custom": {Type: "boolean", Description: "Whether the human can provide custom free-form text"},
			"placeholder":  {Type: "string", Description: "Optional placeholder for custom input"},
			"multiple":     {Type: "boolean", Description: "Whether multiple options may be selected"},
		}),
		Handler: func(context.Context, map[string]any) (string, error) {
			return "", fmt.Errorf("ask_user is handled by the agent runtime")
		},
	}
}
