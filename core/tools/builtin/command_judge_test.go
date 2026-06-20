package builtin

import (
	"context"
	"testing"

	"github.com/EquentR/agent_runtime/core/workspaces"
)

type stubCommandJudge struct {
	result CommandJudgeResult
	err    error
}

var _ CommandJudge = (*stubCommandJudge)(nil)

func (s *stubCommandJudge) Evaluate(ctx context.Context, request CommandJudgeRequest) (CommandJudgeResult, error) {
	_ = ctx
	_ = request
	if s == nil {
		return CommandJudgeResult{}, nil
	}
	return s.result, s.err
}

func TestNormalizeCommandVerdict(t *testing.T) {
	cases := map[string]CommandVerdict{
		"safe":        CommandVerdictSafe,
		"SAFE":        CommandVerdictSafe,
		" neutral ":   CommandVerdictNeutral,
		"risky":       CommandVerdictRisky,
		"unavailable": CommandVerdictUnavailable,
		"":            CommandVerdictUnavailable,
		"unknown":     CommandVerdictUnavailable,
	}

	for raw, want := range cases {
		if got := normalizeCommandVerdict(raw); got != want {
			t.Fatalf("normalizeCommandVerdict(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestCommandJudgeContractCarriesWorkspaceModeAndVerdict(t *testing.T) {
	var judge CommandJudge = &stubCommandJudge{
		result: CommandJudgeResult{
			Verdict: CommandVerdictNeutral,
			Reason:  "uncertain",
		},
	}

	got, err := judge.Evaluate(context.Background(), CommandJudgeRequest{
		Command:       "exec_command",
		Arguments:     map[string]any{"command": "go", "args": []any{"env", "GOOS"}},
		WorkspaceMode: workspaces.ModeReadonly,
		ToolName:      "exec_command",
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if got.Verdict != CommandVerdictNeutral {
		t.Fatalf("Evaluate() verdict = %q, want %q", got.Verdict, CommandVerdictNeutral)
	}
	if got.Reason != "uncertain" {
		t.Fatalf("Evaluate() reason = %q, want %q", got.Reason, "uncertain")
	}
}
