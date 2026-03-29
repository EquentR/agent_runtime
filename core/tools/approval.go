package tools

import "github.com/EquentR/agent_runtime/core/types"

type RiskLevel string

const (
	RiskLevelLow    RiskLevel = "low"
	RiskLevelMedium RiskLevel = "medium"
	RiskLevelHigh   RiskLevel = "high"
)

type ApprovalRequirement struct {
	Required         bool
	ArgumentsSummary string
	RiskLevel        RiskLevel
	Reason           string
}

type ApprovalEvaluator func(arguments map[string]any) ApprovalRequirement

type ApprovalPolicy struct {
	Mode      types.ToolApprovalMode
	Evaluator ApprovalEvaluator
}

func IsValidApprovalMode(mode types.ToolApprovalMode) bool {
	switch mode {
	case "", types.ToolApprovalModeNever, types.ToolApprovalModeAlways, types.ToolApprovalModeConditional:
		return true
	default:
		return false
	}
}

func (p ApprovalPolicy) Evaluate(arguments map[string]any) ApprovalRequirement {
	requirement := ApprovalRequirement{}
	if p.Evaluator != nil {
		requirement = p.Evaluator(arguments)
	}

	switch p.Mode {
	case types.ToolApprovalModeAlways:
		requirement.Required = true
		return requirement
	case types.ToolApprovalModeConditional:
		return requirement
	default:
		return ApprovalRequirement{}
	}
}
