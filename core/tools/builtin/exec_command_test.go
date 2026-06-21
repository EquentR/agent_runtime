package builtin

import (
	"context"
	"errors"
	"testing"

	coretools "github.com/EquentR/agent_runtime/core/tools"
	"github.com/EquentR/agent_runtime/core/workspaces"
)

type recordingExecCommandJudge struct {
	requests []CommandJudgeRequest
	result   CommandJudgeResult
	err      error
}

func (j *recordingExecCommandJudge) Evaluate(ctx context.Context, request CommandJudgeRequest) (CommandJudgeResult, error) {
	_ = ctx
	j.requests = append(j.requests, request)
	return j.result, j.err
}

func TestExecCommandApprovalMatrix(t *testing.T) {
	cases := []struct {
		name         string
		mode         workspaces.Mode
		verdict      CommandVerdict
		wantDecision coretools.ApprovalDecision
		wantRequired bool
	}{
		{name: "mutable-safe", mode: workspaces.ModeMutable, verdict: CommandVerdictSafe, wantDecision: coretools.ApprovalDecisionAllow, wantRequired: false},
		{name: "mutable-neutral", mode: workspaces.ModeMutable, verdict: CommandVerdictNeutral, wantDecision: coretools.ApprovalDecisionAllow, wantRequired: false},
		{name: "readonly-neutral", mode: workspaces.ModeReadonly, verdict: CommandVerdictNeutral, wantDecision: coretools.ApprovalDecisionRequireApproval, wantRequired: true},
		{name: "mutable-risky", mode: workspaces.ModeMutable, verdict: CommandVerdictRisky, wantDecision: coretools.ApprovalDecisionRequireApproval, wantRequired: true},
		{name: "readonly-risky", mode: workspaces.ModeReadonly, verdict: CommandVerdictRisky, wantDecision: coretools.ApprovalDecisionBlock, wantRequired: false},
		{name: "unavailable", mode: workspaces.ModeMutable, verdict: CommandVerdictUnavailable, wantDecision: coretools.ApprovalDecisionRequireApproval, wantRequired: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			registry := newBuiltinRegistry(t, Options{
				WorkspaceRoot: t.TempDir(),
				CommandJudge: &stubCommandJudge{
					result: CommandJudgeResult{Verdict: tc.verdict},
				},
			})

			policy, ok := registry.ApprovalPolicy("exec_command")
			if !ok {
				t.Fatal("ApprovalPolicy(exec_command) ok = false, want true")
			}

			requirement := policy.Evaluate(map[string]any{
				"command":        "echo",
				"args":           []any{"hello"},
				"workspace_mode": string(tc.mode),
			})
			if requirement.Decision != tc.wantDecision {
				t.Fatalf("Decision = %q, want %q for verdict %q and mode %q", requirement.Decision, tc.wantDecision, tc.verdict, tc.mode)
			}
			if requirement.Required != tc.wantRequired {
				t.Fatalf("Required = %v, want %v for verdict %q and mode %q", requirement.Required, tc.wantRequired, tc.verdict, tc.mode)
			}
		})
	}
}

func TestExecCommandJudgeUnavailableFallsBackToApproval(t *testing.T) {
	registry := newBuiltinRegistry(t, Options{
		WorkspaceRoot: t.TempDir(),
		CommandJudge: &stubCommandJudge{
			err: errors.New("judge unavailable"),
		},
	})

	policy, ok := registry.ApprovalPolicy("exec_command")
	if !ok {
		t.Fatal("ApprovalPolicy(exec_command) ok = false, want true")
	}

	requirement := policy.Evaluate(map[string]any{
		"command":        "echo",
		"args":           []any{"hello"},
		"workspace_mode": string(workspaces.ModeMutable),
	})
	if requirement.Decision != coretools.ApprovalDecisionRequireApproval {
		t.Fatalf("Decision = %q, want %q when judge errors", requirement.Decision, coretools.ApprovalDecisionRequireApproval)
	}
	if !requirement.Required {
		t.Fatal("judge error should force human approval")
	}
}

func TestExecCommandJudgeMissingRequiresApproval(t *testing.T) {
	registry := newBuiltinRegistry(t, Options{
		WorkspaceRoot: t.TempDir(),
	})

	policy, ok := registry.ApprovalPolicy("exec_command")
	if !ok {
		t.Fatal("ApprovalPolicy(exec_command) ok = false, want true")
	}

	requirement := policy.Evaluate(map[string]any{
		"command":        "echo",
		"args":           []any{"hello"},
		"workspace_mode": string(workspaces.ModeMutable),
	})
	if requirement.Decision != coretools.ApprovalDecisionRequireApproval {
		t.Fatalf("Decision = %q, want %q when judge missing", requirement.Decision, coretools.ApprovalDecisionRequireApproval)
	}
	if !requirement.Required {
		t.Fatal("missing judge should force human approval")
	}
}

func TestExecCommandJudgeUnwrapsShellWrappersBeforeEvaluation(t *testing.T) {
	judge := &recordingExecCommandJudge{
		result: CommandJudgeResult{Verdict: CommandVerdictSafe},
	}
	registry := newBuiltinRegistry(t, Options{
		WorkspaceRoot: t.TempDir(),
		CommandJudge:  judge,
	})

	policy, ok := registry.ApprovalPolicy("exec_command")
	if !ok {
		t.Fatal("ApprovalPolicy(exec_command) ok = false, want true")
	}

	requirement := policy.Evaluate(map[string]any{
		"command":        "sh",
		"args":           []any{"-c", "rm -rf tmp"},
		"workspace_mode": string(workspaces.ModeReadonly),
	})
	if requirement.Required {
		t.Fatal("safe judge verdict should not require approval")
	}
	if len(judge.requests) != 1 {
		t.Fatalf("judge request count = %d, want 1", len(judge.requests))
	}
	if judge.requests[0].WorkspaceMode != workspaces.ModeReadonly {
		t.Fatalf("judge workspace mode = %q, want %q", judge.requests[0].WorkspaceMode, workspaces.ModeReadonly)
	}
	if judge.requests[0].Command != "rm -rf tmp" {
		t.Fatalf("judge command = %q, want %q", judge.requests[0].Command, "rm -rf tmp")
	}
	if requirement.Decision != coretools.ApprovalDecisionAllow {
		t.Fatalf("safe judge verdict decision = %q, want %q", requirement.Decision, coretools.ApprovalDecisionAllow)
	}
}
