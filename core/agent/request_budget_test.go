package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/EquentR/agent_runtime/core/forcedprompt"
	"github.com/EquentR/agent_runtime/core/memory"
	model "github.com/EquentR/agent_runtime/core/providers/types"
	"github.com/EquentR/agent_runtime/core/runtimeprompt"
	coretypes "github.com/EquentR/agent_runtime/core/types"
)

func TestBuildBudgetedRequestRetriesAfterReserveCompression(t *testing.T) {
	mgr, err := memory.NewManager(memory.Options{
		MaxContextTokens: 120,
		Counter:          fakeTokenCounter{},
		Compressor: func(context.Context, memory.CompressionRequest) (string, error) {
			return "compressed memory", nil
		},
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	mgr.AddMessages([]model.Message{{Role: model.RoleUser, Content: strings.Repeat("a", 70)}, {Role: model.RoleAssistant, Content: strings.Repeat("b", 70)}})

	runner, err := NewRunner(&stubClient{}, nil, Options{
		Model:                "test-model",
		LLMModel:             &coretypes.LLMModel{Context: coretypes.LLMContextConfig{Max: 120}},
		Memory:               mgr,
		RuntimePromptBuilder: runtimeprompt.NewBuilder(nil),
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	_, messages, decision, err := runner.buildBudgetedRequest(context.Background(), false)
	if err != nil {
		t.Fatalf("buildBudgetedRequest() error = %v", err)
	}
	if !decision.CompressionAttempted || decision.FinalPath != "compressed" {
		t.Fatalf("budget decision = %#v, want successful compression path", decision)
	}
	if len(messages) == 0 {
		t.Fatal("messages = empty, want rebuilt request")
	}
}

func TestBuildBudgetedRequestTrimsOldToolMessagesWhenCompressionIsInsufficient(t *testing.T) {
	messages := []model.Message{
		{Role: model.RoleUser, Content: strings.Repeat("u", 30)},
		{Role: model.RoleTool, ToolCallId: "call_1", Content: strings.Repeat("tool", 40)},
		{Role: model.RoleAssistant, Content: strings.Repeat("assistant", 20)},
	}
	trimmed, changed := trimMessagesForBudget(messages, 60, fakeTokenCounter{})
	if !changed {
		t.Fatal("trimMessagesForBudget() changed = false, want true")
	}
	if trimmed[1].Role != model.RoleTool || !strings.Contains(trimmed[1].Content, "trimmed tool output") {
		t.Fatalf("trimmed tool message = %#v, want summarized tool output", trimmed[1])
	}
}

func TestBuildBudgetedRequestReturnsErrContextBudgetExceededWhenStillUnsafe(t *testing.T) {
	runner, err := NewRunner(&stubClient{}, nil, Options{
		Model:    "test-model",
		LLMModel: &coretypes.LLMModel{Context: coretypes.LLMContextConfig{Max: 20}},
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	_, _, _, err = runner.buildBudgetedRequestFromContext(context.Background(), memory.RuntimeContext{Body: []model.Message{{Role: model.RoleUser, Content: strings.Repeat("x", 200)}}}, false)
	if !errors.Is(err, ErrContextBudgetExceeded) {
		t.Fatalf("buildBudgetedRequestFromContext() error = %v, want ErrContextBudgetExceeded", err)
	}
}

func TestBuildBudgetedRequestFromContextCountsRenderedPromptOverhead(t *testing.T) {
	runner, err := NewRunner(&stubClient{}, nil, Options{
		Model:                "test-model",
		LLMModel:             &coretypes.LLMModel{Context: coretypes.LLMContextConfig{Max: 60}},
		RuntimePromptBuilder: runtimeprompt.NewBuilder(forcedprompt.NewProvider()),
		Now:                  func() time.Time { return time.Date(2026, time.April, 6, 9, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	_, _, _, err = runner.buildBudgetedRequestFromContext(context.Background(), memory.RuntimeContext{Body: []model.Message{{Role: model.RoleUser, Content: "hi"}}}, false)
	if !errors.Is(err, ErrContextBudgetExceeded) {
		t.Fatalf("buildBudgetedRequestFromContext() error = %v, want ErrContextBudgetExceeded from rendered prompt overhead", err)
	}
}

func TestBuildBudgetedRequestUsesReserveAwareCompressionBeforeTrim(t *testing.T) {
	mgr, err := memory.NewManager(memory.Options{
		MaxContextTokens: 100,
		Counter:          fakeTokenCounter{},
		Compressor: func(context.Context, memory.CompressionRequest) (string, error) {
			return "compressed memory", nil
		},
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	mgr.AddMessages([]model.Message{{Role: model.RoleUser, Content: strings.Repeat("a", 30)}, {Role: model.RoleAssistant, Content: strings.Repeat("b", 30)}})

	runner, err := NewRunner(&stubClient{}, nil, Options{
		Model:                "test-model",
		LLMModel:             &coretypes.LLMModel{Context: coretypes.LLMContextConfig{Max: 100}},
		Memory:               mgr,
		RuntimePromptBuilder: runtimeprompt.NewBuilder(nil),
		SystemPrompt:         strings.Repeat("p", 50),
		Now:                  func() time.Time { return time.Date(2026, time.April, 6, 9, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}

	_, messages, decision, err := runner.buildBudgetedRequest(context.Background(), false)
	if err != nil {
		t.Fatalf("buildBudgetedRequest() error = %v", err)
	}
	if !decision.CompressionAttempted || !decision.CompressionSucceeded {
		t.Fatalf("budget decision = %#v, want reserve-aware compression attempt and success", decision)
	}
	if decision.TrimApplied {
		t.Fatalf("budget decision = %#v, want compression path before deterministic trim", decision)
	}
	if decision.FinalPath != "compressed" {
		t.Fatalf("budget decision = %#v, want compressed final path", decision)
	}
	if len(messages) == 0 {
		t.Fatal("messages = empty, want rebuilt request")
	}
}
