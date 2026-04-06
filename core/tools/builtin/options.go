package builtin

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	corelog "github.com/EquentR/agent_runtime/core/log"
	coretools "github.com/EquentR/agent_runtime/core/tools"
)

const (
	defaultCommandTimeout = 10 * time.Second
	defaultHTTPTimeout    = 30 * time.Second
	maxCommandTimeout     = 120 * time.Second
	minCommandTimeout     = 1 * time.Second
)

type Options struct {
	WorkspaceRoot  string
	CommandTimeout time.Duration
	HTTPClient     *http.Client
	WebSearch      WebSearchOptions
}

type WebSearchOptions struct {
	DefaultProvider string
	Tavily          *TavilyConfig
	SerpAPI         *SerpAPIConfig
	Bing            *BingConfig
}

type TavilyConfig struct {
	APIKey  string
	BaseURL string
}

type SerpAPIConfig struct {
	APIKey  string
	BaseURL string
}

type BingConfig struct {
	APIKey  string
	BaseURL string
}

type runtimeEnv struct {
	workspaceRoot  string
	commandTimeout time.Duration
	httpClient     *http.Client
	webSearch      WebSearchOptions
}

func normalizeOptions(options Options) (runtimeEnv, error) {
	root := options.WorkspaceRoot
	if root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return runtimeEnv{}, fmt.Errorf("resolve workspace root: %w", err)
		}
		root = cwd
	}

	root, err := filepath.Abs(root)
	if err != nil {
		return runtimeEnv{}, fmt.Errorf("resolve workspace root: %w", err)
	}
	root = filepath.Clean(root)

	info, err := os.Lstat(root)
	if err != nil {
		return runtimeEnv{}, fmt.Errorf("workspace root is invalid: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return runtimeEnv{}, fmt.Errorf("workspace root cannot be a symlink")
	}
	if !info.IsDir() {
		return runtimeEnv{}, fmt.Errorf("workspace root must be a directory")
	}

	timeout := options.CommandTimeout
	if timeout <= 0 {
		timeout = defaultCommandTimeout
	}

	client := options.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultHTTPTimeout}
	}

	return runtimeEnv{
		workspaceRoot:  root,
		commandTimeout: clampDuration(timeout, minCommandTimeout, maxCommandTimeout),
		httpClient:     client,
		webSearch:      options.WebSearch,
	}, nil
}

func clampDuration(value time.Duration, min time.Duration, max time.Duration) time.Duration {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func (e runtimeEnv) httpClientWithTimeout(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		return e.httpClient
	}
	clone := *e.httpClient
	clone.Timeout = timeout
	return &clone
}

func toolLogFields(ctx context.Context, toolName string, extra ...corelog.Field) []corelog.Field {
	fields := []corelog.Field{
		corelog.String("component", "tool"),
		corelog.String("tool_name", toolName),
	}
	if runtime, ok := coretools.RuntimeFromContext(ctx); ok && runtime != nil {
		if runtime.TaskID != "" {
			fields = append(fields, corelog.String("task_id", runtime.TaskID))
		}
		if runtime.StepID != "" {
			fields = append(fields, corelog.String("step_id", runtime.StepID))
		}
		if runtime.Actor != "" {
			fields = append(fields, corelog.String("actor", runtime.Actor))
		}
	}
	fields = append(fields, extra...)
	return fields
}

func logToolStart(ctx context.Context, toolName string, extra ...corelog.Field) {
	corelog.Info("tool started", toolLogFields(ctx, toolName, extra...)...)
}

func logToolFinish(ctx context.Context, toolName string, extra ...corelog.Field) {
	corelog.Info("tool finished", toolLogFields(ctx, toolName, extra...)...)
}

func logToolFailure(ctx context.Context, toolName string, err error, extra ...corelog.Field) {
	fields := toolLogFields(ctx, toolName, extra...)
	if err != nil {
		fields = append(fields, corelog.Err(err))
	}
	corelog.Error("tool failed", fields...)
}
