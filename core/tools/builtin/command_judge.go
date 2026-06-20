package builtin

import (
	"context"
	"strings"

	"github.com/EquentR/agent_runtime/core/workspaces"
)

type CommandVerdict string

const (
	CommandVerdictSafe        CommandVerdict = "safe"
	CommandVerdictNeutral     CommandVerdict = "neutral"
	CommandVerdictRisky       CommandVerdict = "risky"
	CommandVerdictUnavailable CommandVerdict = "unavailable"
)

type CommandJudgeRequest struct {
	Command       string
	Arguments     map[string]any
	WorkspaceMode workspaces.Mode
	ToolName      string
}

type CommandJudgeResult struct {
	Verdict CommandVerdict
	Reason  string
}

type CommandJudge interface {
	Evaluate(ctx context.Context, request CommandJudgeRequest) (CommandJudgeResult, error)
}

func normalizeCommandVerdict(value string) CommandVerdict {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(CommandVerdictSafe):
		return CommandVerdictSafe
	case string(CommandVerdictNeutral):
		return CommandVerdictNeutral
	case string(CommandVerdictRisky):
		return CommandVerdictRisky
	case string(CommandVerdictUnavailable):
		return CommandVerdictUnavailable
	default:
		return CommandVerdictUnavailable
	}
}
