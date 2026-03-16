package builtin

import "github.com/EquentR/agent_runtime/core/types"

func objectSchema(required []string, properties map[string]types.SchemaProperty) types.JSONSchema {
	return types.JSONSchema{
		Type:       "object",
		Properties: properties,
		Required:   required,
	}
}

func stringArrayProperty(description string) types.SchemaProperty {
	return types.SchemaProperty{
		Type:        "array",
		Description: description,
		Items: &types.SchemaProperty{
			Type: "string",
		},
	}
}
