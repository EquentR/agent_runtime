package agent

import (
	"errors"
	"strings"
	"testing"

	coretypes "github.com/EquentR/agent_runtime/core/types"
)

func TestLoopGuardWarnsAndStopsRepeatedWriteOverwriteSuccess(t *testing.T) {
	guard := NewLoopGuard(LoopGuardOptions{})
	first := guard.AfterToolResult(coretypes.ToolCall{ID: "call_1", Name: "write_file", Arguments: `{"path":"skills/foo.md","mode":"overwrite","content":"one"}`}, mustDecodeLoopGuardArgs(t, `{"path":"skills/foo.md","mode":"overwrite","content":"one"}`), "", nil)
	if first.Decision != LoopGuardAllow {
		t.Fatalf("first decision = %q, want allow", first.Decision)
	}
	second := guard.AfterToolResult(coretypes.ToolCall{ID: "call_2", Name: "write_file", Arguments: `{"path":"skills/foo.md","mode":"overwrite","content":"two"}`}, mustDecodeLoopGuardArgs(t, `{"path":"skills/foo.md","mode":"overwrite","content":"two"}`), "", nil)
	if second.Decision != LoopGuardWarn || second.RepeatCount != 2 || second.StopStrategy != "" {
		t.Fatalf("second result = %#v, want warn repeat_count=2", second)
	}
	if !strings.Contains(second.WarningText, "write_file") || !strings.Contains(second.WarningText, "skills/foo.md") {
		t.Fatalf("warning text = %q, want tool and path", second.WarningText)
	}
	third := guard.AfterToolResult(coretypes.ToolCall{ID: "call_3", Name: "write_file", Arguments: `{"path":"skills/foo.md","mode":"overwrite","content":"three"}`}, mustDecodeLoopGuardArgs(t, `{"path":"skills/foo.md","mode":"overwrite","content":"three"}`), "", nil)
	if third.Decision != LoopGuardStop || third.StopStrategy != LoopGuardStopStrategySafeCompletion || third.RepeatCount != 3 {
		t.Fatalf("third result = %#v, want safe completion stop", third)
	}
}

func TestLoopGuardTreatsMissingWriteModeAsOverwrite(t *testing.T) {
	guard := NewLoopGuard(LoopGuardOptions{})
	first := coretypes.ToolCall{ID: "call_1", Name: "write_file", Arguments: `{"path":"skills/foo.md","content":"one"}`}
	if result := guard.AfterToolResult(first, mustDecodeLoopGuardArgs(t, first.Arguments), "", nil); result.Decision != LoopGuardAllow {
		t.Fatalf("first result = %#v, want allow", result)
	}
	second := coretypes.ToolCall{ID: "call_2", Name: "write_file", Arguments: `{"path":"skills/foo.md","content":"two"}`}
	if result := guard.AfterToolResult(second, mustDecodeLoopGuardArgs(t, second.Arguments), "", nil); result.Decision != LoopGuardWarn {
		t.Fatalf("second result = %#v, want warn", result)
	}
	third := coretypes.ToolCall{ID: "call_3", Name: "write_file", Arguments: `{"path":"skills/foo.md","content":"three"}`}
	result := guard.AfterToolResult(third, mustDecodeLoopGuardArgs(t, third.Arguments), "", nil)
	if result.Decision != LoopGuardStop || result.StopStrategy != LoopGuardStopStrategySafeCompletion {
		t.Fatalf("third result = %#v, want safe completion stop", result)
	}
	if !strings.Contains(result.FinalMessage, "skills/foo.md") {
		t.Fatalf("final message = %q, want written path", result.FinalMessage)
	}
}

func TestLoopGuardAllowsWriteOverwriteDifferentTargets(t *testing.T) {
	guard := NewLoopGuard(LoopGuardOptions{})
	for _, call := range []coretypes.ToolCall{
		{ID: "call_1", Name: "write_file", Arguments: `{"path":"skills/a.md","mode":"overwrite","content":"one"}`},
		{ID: "call_2", Name: "write_file", Arguments: `{"path":"skills/b.md","mode":"overwrite","content":"two"}`},
		{ID: "call_3", Name: "write_file", Arguments: `{"path":"skills/c.md","mode":"overwrite","content":"three"}`},
	} {
		result := guard.AfterToolResult(call, mustDecodeLoopGuardArgs(t, call.Arguments), "", nil)
		if result.Decision != LoopGuardAllow {
			t.Fatalf("result for %s = %#v, want allow", call.ID, result)
		}
	}
}

func TestLoopGuardAllowsWriteReplaceLinesDifferentRanges(t *testing.T) {
	guard := NewLoopGuard(LoopGuardOptions{})
	calls := []coretypes.ToolCall{
		{ID: "call_1", Name: "write_file", Arguments: `{"path":"skills/foo.md","mode":"replace_lines","start_line":1,"end_line":3,"content":"one"}`},
		{ID: "call_2", Name: "write_file", Arguments: `{"path":"skills/foo.md","mode":"replace_lines","start_line":4,"end_line":6,"content":"two"}`},
		{ID: "call_3", Name: "write_file", Arguments: `{"path":"skills/foo.md","mode":"replace_lines","start_line":7,"end_line":9,"content":"three"}`},
	}
	for _, call := range calls {
		result := guard.AfterToolResult(call, mustDecodeLoopGuardArgs(t, call.Arguments), "", nil)
		if result.Decision != LoopGuardAllow {
			t.Fatalf("result for %s = %#v, want allow", call.ID, result)
		}
	}
}

func TestLoopGuardStopsRepeatedWriteReplaceLinesAsFailedLoop(t *testing.T) {
	guard := NewLoopGuard(LoopGuardOptions{})
	call := coretypes.ToolCall{ID: "call_1", Name: "write_file", Arguments: `{"path":"skills/foo.md","mode":"replace_lines","start_line":1,"end_line":3,"content":"one"}`}
	if result := guard.AfterToolResult(call, mustDecodeLoopGuardArgs(t, call.Arguments), "", nil); result.Decision != LoopGuardAllow {
		t.Fatalf("first result = %#v, want allow", result)
	}
	call.ID = "call_2"
	if result := guard.AfterToolResult(call, mustDecodeLoopGuardArgs(t, call.Arguments), "", nil); result.Decision != LoopGuardWarn {
		t.Fatalf("second result = %#v, want warn", result)
	}
	call.ID = "call_3"
	result := guard.AfterToolResult(call, mustDecodeLoopGuardArgs(t, call.Arguments), "", nil)
	if result.Decision != LoopGuardStop || result.StopStrategy != LoopGuardStopStrategyFailedLoop {
		t.Fatalf("third result = %#v, want failed-loop stop", result)
	}
	if !errors.Is(result.Err, ErrToolLoopDetected) {
		t.Fatalf("third error = %v, want ErrToolLoopDetected", result.Err)
	}
}

func TestLoopGuardStopsRepeatedListFilesError(t *testing.T) {
	guard := NewLoopGuard(LoopGuardOptions{})
	errPathRequired := errors.New("path is required")
	call := coretypes.ToolCall{ID: "call_1", Name: "list_files", Arguments: `{}`}
	if result := guard.AfterToolResult(call, mustDecodeLoopGuardArgs(t, call.Arguments), "", errPathRequired); result.Decision != LoopGuardAllow {
		t.Fatalf("first result = %#v, want allow", result)
	}
	call.ID = "call_2"
	if result := guard.AfterToolResult(call, mustDecodeLoopGuardArgs(t, call.Arguments), "", errPathRequired); result.Decision != LoopGuardWarn || result.Reason != LoopGuardReasonSameErrorRepeated {
		t.Fatalf("second result = %#v, want same-error warning", result)
	}
	call.ID = "call_3"
	result := guard.AfterToolResult(call, mustDecodeLoopGuardArgs(t, call.Arguments), "", errPathRequired)
	if result.Decision != LoopGuardStop || result.StopStrategy != LoopGuardStopStrategyFailedLoop {
		t.Fatalf("third result = %#v, want failed-loop stop", result)
	}
	if !errors.Is(result.Err, ErrToolLoopDetected) {
		t.Fatalf("third error = %v, want ErrToolLoopDetected", result.Err)
	}
}

func TestLoopGuardReadFileWindowRules(t *testing.T) {
	guard := NewLoopGuard(LoopGuardOptions{})
	differentWindows := []coretypes.ToolCall{
		{ID: "call_1", Name: "read_file", Arguments: `{"path":"skills/foo.md","start_line":1,"line_count":20}`},
		{ID: "call_2", Name: "read_file", Arguments: `{"path":"skills/foo.md","start_line":21,"line_count":20}`},
	}
	for _, call := range differentWindows {
		if result := guard.AfterToolResult(call, mustDecodeLoopGuardArgs(t, call.Arguments), "ok", nil); result.Decision != LoopGuardAllow {
			t.Fatalf("different-window result = %#v, want allow", result)
		}
	}
	repeated := coretypes.ToolCall{ID: "call_3", Name: "read_file", Arguments: `{"path":"skills/foo.md","start_line":21,"line_count":20}`}
	if result := guard.AfterToolResult(repeated, mustDecodeLoopGuardArgs(t, repeated.Arguments), "ok", nil); result.Decision != LoopGuardWarn {
		t.Fatalf("repeated-window second result = %#v, want warn", result)
	}
	repeated.ID = "call_4"
	result := guard.AfterToolResult(repeated, mustDecodeLoopGuardArgs(t, repeated.Arguments), "ok", nil)
	if result.Decision != LoopGuardStop || result.StopStrategy != LoopGuardStopStrategyFailedLoop {
		t.Fatalf("repeated-window third result = %#v, want failed-loop stop", result)
	}
	if !errors.Is(result.Err, ErrToolLoopDetected) {
		t.Fatalf("repeated-window third error = %v, want ErrToolLoopDetected", result.Err)
	}
}

func TestLoopGuardSearchIdentityAndInteractionExclusion(t *testing.T) {
	guard := NewLoopGuard(LoopGuardOptions{})
	searchA := coretypes.ToolCall{ID: "call_1", Name: "search_file", Arguments: `{"path":"core","pattern":"LoopGuard"}`}
	searchB := coretypes.ToolCall{ID: "call_2", Name: "search_file", Arguments: `{"path":"core","pattern":"LoopGuardOptions"}`}
	if result := guard.AfterToolResult(searchA, mustDecodeLoopGuardArgs(t, searchA.Arguments), "ok", nil); result.Decision != LoopGuardAllow {
		t.Fatalf("searchA result = %#v, want allow", result)
	}
	if result := guard.AfterToolResult(searchB, mustDecodeLoopGuardArgs(t, searchB.Arguments), "ok", nil); result.Decision != LoopGuardAllow {
		t.Fatalf("searchB result = %#v, want allow", result)
	}
	for index := 0; index < 4; index++ {
		call := coretypes.ToolCall{ID: "ask", Name: "ask_user", Arguments: `{"question":"continue?"}`}
		if result := guard.AfterToolResult(call, mustDecodeLoopGuardArgs(t, call.Arguments), "", nil); result.Decision != LoopGuardAllow {
			t.Fatalf("ask_user result = %#v, want allow", result)
		}
	}
}

func TestLoopGuardStopsRepeatedSearchPatternAsFailedLoop(t *testing.T) {
	guard := NewLoopGuard(LoopGuardOptions{})
	call := coretypes.ToolCall{ID: "call_1", Name: "search_file", Arguments: `{"path":"core","pattern":"LoopGuard"}`}
	if result := guard.AfterToolResult(call, mustDecodeLoopGuardArgs(t, call.Arguments), "ok", nil); result.Decision != LoopGuardAllow {
		t.Fatalf("first result = %#v, want allow", result)
	}
	call.ID = "call_2"
	if result := guard.AfterToolResult(call, mustDecodeLoopGuardArgs(t, call.Arguments), "ok", nil); result.Decision != LoopGuardWarn {
		t.Fatalf("second result = %#v, want warn", result)
	}
	call.ID = "call_3"
	result := guard.AfterToolResult(call, mustDecodeLoopGuardArgs(t, call.Arguments), "ok", nil)
	if result.Decision != LoopGuardStop || result.StopStrategy != LoopGuardStopStrategyFailedLoop {
		t.Fatalf("third result = %#v, want failed-loop stop", result)
	}
	if !errors.Is(result.Err, ErrToolLoopDetected) {
		t.Fatalf("third error = %v, want ErrToolLoopDetected", result.Err)
	}
}

func TestLoopGuardExplicitDisableAllowsRepeats(t *testing.T) {
	guard := NewLoopGuard(LoopGuardOptions{Enabled: loopGuardBool(false)})
	call := coretypes.ToolCall{ID: "call_1", Name: "write_file", Arguments: `{"path":"skills/foo.md","mode":"overwrite","content":"one"}`}
	for index := 0; index < 4; index++ {
		call.ID = "call_" + string(rune('1'+index))
		result := guard.AfterToolResult(call, mustDecodeLoopGuardArgs(t, call.Arguments), "", nil)
		if result.Decision != LoopGuardAllow {
			t.Fatalf("result %d = %#v, want allow", index+1, result)
		}
	}
}

func TestLoopGuardCustomThresholdsRemainEnabledWithoutEnabledOption(t *testing.T) {
	guard := NewLoopGuard(LoopGuardOptions{SameTargetSuccessSoftLimit: 3, SameTargetSuccessHardLimit: 4})
	call := coretypes.ToolCall{ID: "call_1", Name: "write_file", Arguments: `{"path":"skills/foo.md","mode":"overwrite","content":"one"}`}
	if result := guard.AfterToolResult(call, mustDecodeLoopGuardArgs(t, call.Arguments), "", nil); result.Decision != LoopGuardAllow {
		t.Fatalf("first result = %#v, want allow", result)
	}
	call.ID = "call_2"
	if result := guard.AfterToolResult(call, mustDecodeLoopGuardArgs(t, call.Arguments), "", nil); result.Decision != LoopGuardAllow {
		t.Fatalf("second result = %#v, want allow below custom soft limit", result)
	}
	call.ID = "call_3"
	if result := guard.AfterToolResult(call, mustDecodeLoopGuardArgs(t, call.Arguments), "", nil); result.Decision != LoopGuardWarn || result.SoftLimit != 3 || result.HardLimit != 4 {
		t.Fatalf("third result = %#v, want custom warn at 3/4", result)
	}
	call.ID = "call_4"
	if result := guard.AfterToolResult(call, mustDecodeLoopGuardArgs(t, call.Arguments), "", nil); result.Decision != LoopGuardStop || result.StopStrategy != LoopGuardStopStrategySafeCompletion {
		t.Fatalf("fourth result = %#v, want custom safe-completion stop", result)
	}
}

func TestLoopGuardDecodesNilArgumentsFromToolCall(t *testing.T) {
	guard := NewLoopGuard(LoopGuardOptions{})
	call := coretypes.ToolCall{ID: "call_1", Name: "write_file", Arguments: `{"path":"skills/foo.md","mode":"overwrite","content":"one"}`}
	if result := guard.AfterToolResult(call, nil, "", nil); result.Decision != LoopGuardAllow {
		t.Fatalf("first result = %#v, want allow", result)
	}
	call.ID = "call_2"
	if result := guard.AfterToolResult(call, nil, "", nil); result.Decision != LoopGuardWarn {
		t.Fatalf("second result = %#v, want warn from decoded arguments", result)
	}
	call.ID = "call_3"
	if result := guard.AfterToolResult(call, nil, "", nil); result.Decision != LoopGuardStop || result.StopStrategy != LoopGuardStopStrategySafeCompletion {
		t.Fatalf("third result = %#v, want safe-completion stop from decoded arguments", result)
	}
}

func mustDecodeLoopGuardArgs(t *testing.T, raw string) map[string]interface{} {
	t.Helper()
	args, err := decodeToolArguments(coretypes.ToolCall{Name: "test_tool", Arguments: raw})
	if err != nil {
		t.Fatalf("decodeToolArguments(%s) error = %v", raw, err)
	}
	return args
}

func loopGuardBool(value bool) *bool {
	return &value
}
