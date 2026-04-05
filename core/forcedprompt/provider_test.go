package forcedprompt

import (
	"testing"
	"time"

	"github.com/EquentR/agent_runtime/core/runtimeprompt"
)

func TestProviderSessionSegmentsReturnsBuiltInBlocksInStableOrder(t *testing.T) {
	provider := NewProvider()
	now := time.Date(2026, time.April, 4, 9, 30, 0, 0, time.UTC)

	got, err := provider.SessionSegments(now)
	if err != nil {
		t.Fatalf("SessionSegments() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len(SessionSegments()) = %d, want 3", len(got))
	}

	wantKeys := []string{"current_date", "anti_prompt_injection", "platform_constraints"}
	for i, want := range wantKeys {
		if got[i].SourceType != runtimeprompt.SourceTypeForcedBlock {
			t.Fatalf("segment[%d].SourceType = %q, want forced_block", i, got[i].SourceType)
		}
		if got[i].SourceKey != want {
			t.Fatalf("segment[%d].SourceKey = %q, want %q", i, got[i].SourceKey, want)
		}
		if got[i].Phase != runtimeprompt.PhaseSession {
			t.Fatalf("segment[%d].Phase = %q, want session", i, got[i].Phase)
		}
		if got[i].Role != runtimeprompt.RoleSystem {
			t.Fatalf("segment[%d].Role = %q, want system", i, got[i].Role)
		}
		if !got[i].RuntimeOnly {
			t.Fatalf("segment[%d].RuntimeOnly = false, want true", i)
		}
		if !got[i].AuditVisible {
			t.Fatalf("segment[%d].AuditVisible = false, want true", i)
		}
		if got[i].Content == "" {
			t.Fatalf("segment[%d].Content = empty, want rendered content", i)
		}
	}
}

func TestProviderSessionSegmentsRendersCurrentDateFromInjectedTime(t *testing.T) {
	provider := NewProvider()
	got, err := provider.SessionSegments(time.Date(2026, time.April, 5, 1, 2, 3, 0, time.UTC))
	if err != nil {
		t.Fatalf("SessionSegments() error = %v", err)
	}
	if got[0].SourceKey != "current_date" {
		t.Fatalf("segment[0].SourceKey = %q, want current_date", got[0].SourceKey)
	}
	want := `<system-reminder>
As you answer the user's questions, you can use the following context:
# currentDate
Today's date is 2026/04/05.

IMPORTANT: this context may or may not be relevant to your task. Only use it when relevant.
</system-reminder>`
	if got[0].Content != want {
		t.Fatalf("current_date content = %q, want exact injected date block", got[0].Content)
	}
}
