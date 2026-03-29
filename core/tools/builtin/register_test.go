package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	coretools "github.com/EquentR/agent_runtime/core/tools"
	coretypes "github.com/EquentR/agent_runtime/core/types"
)

func TestRegisterRegistersPlannedTools(t *testing.T) {
	workspace := t.TempDir()
	registry := coretools.NewRegistry()

	if err := Register(registry, Options{WorkspaceRoot: workspace}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	definitions := registry.List()
	got := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		got = append(got, definition.Name)
	}

	want := []string{
		"check_command",
		"copy_file",
		"delete_file",
		"exec_command",
		"get_system_info",
		"grep_file",
		"http_request",
		"kill_process",
		"list_files",
		"list_processes",
		"move_file",
		"read_file",
		"search_file",
		"web_search",
		"write_file",
	}

	if !slices.Equal(got, want) {
		t.Fatalf("tool names = %#v, want %#v", got, want)
	}
}

func TestRegisterRejectsInvalidWorkspace(t *testing.T) {
	registry := coretools.NewRegistry()
	err := Register(registry, Options{WorkspaceRoot: filepath.Join(t.TempDir(), "missing")})
	if err == nil {
		t.Fatal("Register() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "workspace") {
		t.Fatalf("Register() error = %q, want workspace message", err)
	}
}

func TestRegisterArrayParametersDeclareItems(t *testing.T) {
	registry := newBuiltinRegistry(t, Options{WorkspaceRoot: t.TempDir()})
	definitions := registry.List()

	toolByName := make(map[string]coretypes.Tool)
	for _, definition := range definitions {
		toolByName[definition.Name] = definition
	}

	checkCommand, ok := toolByName["check_command"]
	if !ok {
		t.Fatal("check_command tool missing")
	}
	if checkCommand.Parameters.Properties["version_args"].Items == nil {
		t.Fatal("check_command.version_args items = nil, want string item schema")
	}
	if checkCommand.Parameters.Properties["version_args"].Items.Type != "string" {
		t.Fatalf("check_command.version_args item type = %q, want string", checkCommand.Parameters.Properties["version_args"].Items.Type)
	}

	execCommand, ok := toolByName["exec_command"]
	if !ok {
		t.Fatal("exec_command tool missing")
	}
	if execCommand.Parameters.Properties["args"].Items == nil {
		t.Fatal("exec_command.args items = nil, want string item schema")
	}
	if execCommand.Parameters.Properties["args"].Items.Type != "string" {
		t.Fatalf("exec_command.args item type = %q, want string", execCommand.Parameters.Properties["args"].Items.Type)
	}
}

func TestListFiles(t *testing.T) {
	workspace := t.TempDir()
	mustWriteFile(t, filepath.Join(workspace, "root.txt"), "root")
	mustWriteFile(t, filepath.Join(workspace, "nested", "child.txt"), "child")
	mustWriteFile(t, filepath.Join(workspace, "nested", "deep", "leaf.txt"), "leaf")

	registry := newBuiltinRegistry(t, Options{WorkspaceRoot: workspace})
	raw, err := registry.Execute(context.Background(), "list_files", map[string]any{
		"path":      ".",
		"recursive": true,
		"max_depth": 2,
	})
	if err != nil {
		t.Fatalf("Execute(list_files) error = %v", err)
	}

	var result struct {
		Entries []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"entries"`
	}
	decodeJSON(t, raw, &result)

	paths := make([]string, 0, len(result.Entries))
	for _, entry := range result.Entries {
		paths = append(paths, entry.Path)
	}

	if !slices.Contains(paths, "root.txt") {
		t.Fatalf("entries = %#v, want root.txt", paths)
	}
	if !slices.Contains(paths, filepath.ToSlash(filepath.Join("nested", "child.txt"))) {
		t.Fatalf("entries = %#v, want nested/child.txt", paths)
	}
	if slices.Contains(paths, filepath.ToSlash(filepath.Join("nested", "deep", "leaf.txt"))) {
		t.Fatalf("entries = %#v, want no nested/deep/leaf.txt at depth 2", paths)
	}
}

func TestReadFileReturnsLineWindow(t *testing.T) {
	workspace := t.TempDir()
	mustWriteFile(t, filepath.Join(workspace, "notes.txt"), "one\ntwo\nthree\nfour\n")

	registry := newBuiltinRegistry(t, Options{WorkspaceRoot: workspace})
	raw, err := registry.Execute(context.Background(), "read_file", map[string]any{
		"path":       "notes.txt",
		"start_line": 2,
		"line_count": 2,
	})
	if err != nil {
		t.Fatalf("Execute(read_file) error = %v", err)
	}

	var result struct {
		StartLine int    `json:"start_line"`
		EndLine   int    `json:"end_line"`
		TotalLine int    `json:"total_lines"`
		Content   string `json:"content"`
	}
	decodeJSON(t, raw, &result)

	if result.StartLine != 2 || result.EndLine != 3 || result.TotalLine != 4 {
		t.Fatalf("line window = %#v, want start=2 end=3 total=4", result)
	}
	if result.Content != "two\nthree\n" {
		t.Fatalf("content = %q, want %q", result.Content, "two\nthree\n")
	}
}

func TestWriteFileSupportsInsertAndReplace(t *testing.T) {
	workspace := t.TempDir()
	target := filepath.Join(workspace, "draft.txt")
	mustWriteFile(t, target, "one\nthree\n")

	registry := newBuiltinRegistry(t, Options{WorkspaceRoot: workspace})

	if _, err := registry.Execute(context.Background(), "write_file", map[string]any{
		"path":       "draft.txt",
		"mode":       "insert",
		"start_line": 2,
		"content":    "two\n",
	}); err != nil {
		t.Fatalf("Execute(write_file insert) error = %v", err)
	}

	if got := mustReadText(t, target); got != "one\ntwo\nthree\n" {
		t.Fatalf("file after insert = %q, want %q", got, "one\ntwo\nthree\n")
	}

	if _, err := registry.Execute(context.Background(), "write_file", map[string]any{
		"path":       "draft.txt",
		"mode":       "replace_lines",
		"start_line": 1,
		"end_line":   2,
		"content":    "ONE\nTWO\n",
	}); err != nil {
		t.Fatalf("Execute(write_file replace_lines) error = %v", err)
	}

	if got := mustReadText(t, target); got != "ONE\nTWO\nthree\n" {
		t.Fatalf("file after replace = %q, want %q", got, "ONE\nTWO\nthree\n")
	}
}

func TestSearchFileAndGrepFile(t *testing.T) {
	workspace := t.TempDir()
	mustWriteFile(t, filepath.Join(workspace, "alpha.txt"), "alpha beta\nhello\n")
	mustWriteFile(t, filepath.Join(workspace, "beta.txt"), "gamma\n")

	registry := newBuiltinRegistry(t, Options{WorkspaceRoot: workspace})

	searchRaw, err := registry.Execute(context.Background(), "search_file", map[string]any{
		"path":    ".",
		"pattern": "alpha",
	})
	if err != nil {
		t.Fatalf("Execute(search_file) error = %v", err)
	}

	var searchResult struct {
		Matches []struct {
			Path string `json:"path"`
			Line int    `json:"line"`
			Text string `json:"text"`
		} `json:"matches"`
	}
	decodeJSON(t, searchRaw, &searchResult)

	if len(searchResult.Matches) != 1 {
		t.Fatalf("len(search matches) = %d, want 1", len(searchResult.Matches))
	}
	if searchResult.Matches[0].Path != "alpha.txt" || searchResult.Matches[0].Line != 1 {
		t.Fatalf("search match = %#v, want alpha.txt line 1", searchResult.Matches[0])
	}

	grepRaw, err := registry.Execute(context.Background(), "grep_file", map[string]any{
		"path":      "alpha.txt",
		"pattern":   "^alpha",
		"use_regex": true,
	})
	if err != nil {
		t.Fatalf("Execute(grep_file) error = %v", err)
	}

	var grepResult struct {
		Matches []struct {
			Line int    `json:"line"`
			Text string `json:"text"`
		} `json:"matches"`
	}
	decodeJSON(t, grepRaw, &grepResult)

	if len(grepResult.Matches) != 1 || grepResult.Matches[0].Line != 1 {
		t.Fatalf("grep matches = %#v, want one line 1 match", grepResult.Matches)
	}
}

func TestDeleteFileRequiresConfirmation(t *testing.T) {
	workspace := t.TempDir()
	mustWriteFile(t, filepath.Join(workspace, "trash.txt"), "bye")

	registry := newBuiltinRegistry(t, Options{WorkspaceRoot: workspace})
	tool := toolDefinitionByName(t, registry, "delete_file")
	if _, ok := tool.Parameters.Properties["confirm"]; ok {
		t.Fatal("delete_file confirm parameter present, want absent")
	}
	if tool.ApprovalMode != coretypes.ToolApprovalModeAlways {
		t.Fatalf("delete_file ApprovalMode = %q, want %q", tool.ApprovalMode, coretypes.ToolApprovalModeAlways)
	}
	policy, ok := registry.ApprovalPolicy("delete_file")
	if !ok {
		t.Fatal("ApprovalPolicy(delete_file) ok = false, want true")
	}
	requirement := policy.Evaluate(map[string]any{"path": "trash.txt"})
	if !requirement.Required {
		t.Fatal("delete_file approval Required = false, want true")
	}

	if _, err := registry.Execute(context.Background(), "delete_file", map[string]any{
		"path": "trash.txt",
	}); err != nil {
		t.Fatalf("Execute(delete_file) error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(workspace, "trash.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("os.Stat(trash.txt) error = %v, want not exist", err)
	}
}

func TestMoveFileAndCopyFile(t *testing.T) {
	workspace := t.TempDir()
	mustWriteFile(t, filepath.Join(workspace, "source.txt"), "content")

	registry := newBuiltinRegistry(t, Options{WorkspaceRoot: workspace})
	if _, err := registry.Execute(context.Background(), "copy_file", map[string]any{
		"source":      "source.txt",
		"destination": filepath.ToSlash(filepath.Join("copies", "copied.txt")),
		"create_dirs": true,
	}); err != nil {
		t.Fatalf("Execute(copy_file) error = %v", err)
	}

	if got := mustReadText(t, filepath.Join(workspace, "copies", "copied.txt")); got != "content" {
		t.Fatalf("copied file = %q, want %q", got, "content")
	}

	if _, err := registry.Execute(context.Background(), "move_file", map[string]any{
		"source":      "source.txt",
		"destination": filepath.ToSlash(filepath.Join("moved", "final.txt")),
		"create_dirs": true,
	}); err != nil {
		t.Fatalf("Execute(move_file) error = %v", err)
	}

	if got := mustReadText(t, filepath.Join(workspace, "moved", "final.txt")); got != "content" {
		t.Fatalf("moved file = %q, want %q", got, "content")
	}
	if _, err := os.Stat(filepath.Join(workspace, "source.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("os.Stat(source.txt) error = %v, want not exist", err)
	}
}

func TestFileToolsRejectSymlink(t *testing.T) {
	workspace := t.TempDir()
	target := filepath.Join(workspace, "real.txt")
	mustWriteFile(t, target, "secret")
	link := filepath.Join(workspace, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("os.Symlink() unsupported: %v", err)
	}

	registry := newBuiltinRegistry(t, Options{WorkspaceRoot: workspace})
	_, err := registry.Execute(context.Background(), "read_file", map[string]any{"path": "link.txt"})
	if err == nil {
		t.Fatal("Execute(read_file symlink) error = nil, want non-nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "symlink") {
		t.Fatalf("Execute(read_file symlink) error = %q, want symlink message", err)
	}
}

func TestExecCommand(t *testing.T) {
	workspace := t.TempDir()
	registry := newBuiltinRegistry(t, Options{WorkspaceRoot: workspace, CommandTimeout: 5 * time.Second})
	policy, ok := registry.ApprovalPolicy("exec_command")
	if !ok {
		t.Fatal("ApprovalPolicy(exec_command) ok = false, want true")
	}

	safeRequirement := policy.Evaluate(map[string]any{
		"command": "go",
		"args":    []any{"env", "GOOS"},
	})
	if safeRequirement.Required {
		t.Fatalf("safe exec approval = %#v, want no approval", safeRequirement)
	}

	deleteRequirement := policy.Evaluate(map[string]any{
		"command": "rm",
		"args":    []any{"-rf", "tmp"},
	})
	if !deleteRequirement.Required {
		t.Fatal("rm approval Required = false, want true")
	}
	if deleteRequirement.RiskLevel == "" {
		t.Fatal("rm approval RiskLevel = empty, want non-empty")
	}
	if !strings.Contains(strings.ToLower(deleteRequirement.Reason), "delete") {
		t.Fatalf("rm approval Reason = %q, want delete-focused summary", deleteRequirement.Reason)
	}

	mutationRequirement := policy.Evaluate(map[string]any{
		"command": "apt-get",
		"args":    []any{"install", "ripgrep"},
	})
	if !mutationRequirement.Required {
		t.Fatal("apt-get install approval Required = false, want true")
	}
	if !strings.Contains(strings.ToLower(mutationRequirement.Reason), "system") {
		t.Fatalf("apt-get install Reason = %q, want system-focused summary", mutationRequirement.Reason)
	}

	wrapperCases := []struct {
		name   string
		args   map[string]any
		wantIn string
	}{
		{
			name:   "sh wrapper delete",
			args:   map[string]any{"command": "sh", "args": []any{"-c", "rm -rf tmp"}},
			wantIn: "delete",
		},
		{
			name:   "cmd wrapper delete",
			args:   map[string]any{"command": "cmd", "args": []any{"/C", "del temp.txt"}},
			wantIn: "delete",
		},
		{
			name:   "powershell wrapper kill",
			args:   map[string]any{"command": "powershell", "args": []any{"-Command", "Stop-Process -Id 42"}},
			wantIn: "terminate",
		},
		{
			name:   "sudo prefix delete",
			args:   map[string]any{"command": "sudo", "args": []any{"rm", "-rf", "tmp"}},
			wantIn: "delete",
		},
		{
			name:   "env prefix delete",
			args:   map[string]any{"command": "env", "args": []any{"FOO=1", "BAR=2", "rm", "-rf", "tmp"}},
			wantIn: "delete",
		},
		{
			name:   "nohup prefix kill",
			args:   map[string]any{"command": "nohup", "args": []any{"kill", "123"}},
			wantIn: "terminate",
		},
		{
			name:   "powershell start-process wrapper kill",
			args:   map[string]any{"command": "powershell", "args": []any{"-Command", "Start-Process taskkill -ArgumentList '/PID 123 /F' -Wait"}},
			wantIn: "terminate",
		},
	}
	for _, tc := range wrapperCases {
		t.Run(tc.name, func(t *testing.T) {
			requirement := policy.Evaluate(tc.args)
			if !requirement.Required {
				t.Fatalf("policy.Evaluate(%#v) Required = false, want true", tc.args)
			}
			if !strings.Contains(strings.ToLower(requirement.Reason), tc.wantIn) {
				t.Fatalf("policy.Evaluate(%#v) Reason = %q, want substring %q", tc.args, requirement.Reason, tc.wantIn)
			}
		})
	}

	killCases := []struct {
		name string
		args map[string]any
	}{
		{name: "kill", args: map[string]any{"command": "kill", "args": []any{"123"}}},
		{name: "pkill", args: map[string]any{"command": "pkill", "args": []any{"agent"}}},
		{name: "taskkill", args: map[string]any{"command": "taskkill", "args": []any{"/PID", "123", "/F"}}},
	}
	for _, tc := range killCases {
		t.Run(tc.name, func(t *testing.T) {
			requirement := policy.Evaluate(tc.args)
			if !requirement.Required {
				t.Fatalf("policy.Evaluate(%#v) Required = false, want true", tc.args)
			}
			if !strings.Contains(strings.ToLower(requirement.Reason), "terminate") {
				t.Fatalf("policy.Evaluate(%#v) Reason = %q, want terminate-focused summary", tc.args, requirement.Reason)
			}
		})
	}

	raw, err := registry.Execute(context.Background(), "exec_command", map[string]any{
		"command": "go",
		"args":    []any{"env", "GOOS"},
	})
	if err != nil {
		t.Fatalf("Execute(exec_command) error = %v", err)
	}

	var result struct {
		Success  bool   `json:"success"`
		ExitCode int    `json:"exit_code"`
		Stdout   string `json:"stdout"`
	}
	decodeJSON(t, raw, &result)

	if !result.Success || result.ExitCode != 0 {
		t.Fatalf("exec result = %#v, want success exit 0", result)
	}
	if strings.TrimSpace(result.Stdout) != runtime.GOOS {
		t.Fatalf("stdout = %q, want %q", strings.TrimSpace(result.Stdout), runtime.GOOS)
	}
}

func TestCheckCommand(t *testing.T) {
	workspace := t.TempDir()
	registry := newBuiltinRegistry(t, Options{WorkspaceRoot: workspace})

	raw, err := registry.Execute(context.Background(), "check_command", map[string]any{
		"name":         "go",
		"version_args": []any{"version"},
	})
	if err != nil {
		t.Fatalf("Execute(check_command) error = %v", err)
	}

	var result struct {
		Found   bool   `json:"found"`
		Path    string `json:"path"`
		Version string `json:"version"`
	}
	decodeJSON(t, raw, &result)

	if !result.Found || result.Path == "" {
		t.Fatalf("check result = %#v, want found path", result)
	}
	if !strings.Contains(result.Version, "go version") {
		t.Fatalf("version = %q, want go version output", result.Version)
	}
}

func TestListProcessesReturnsCurrentProcess(t *testing.T) {
	workspace := t.TempDir()
	registry := newBuiltinRegistry(t, Options{WorkspaceRoot: workspace})

	raw, err := registry.Execute(context.Background(), "list_processes", map[string]any{
		"pid": os.Getpid(),
	})
	if err != nil {
		t.Fatalf("Execute(list_processes) error = %v", err)
	}

	var result struct {
		Processes []struct {
			PID int `json:"pid"`
		} `json:"processes"`
	}
	decodeJSON(t, raw, &result)

	if len(result.Processes) == 0 {
		t.Fatal("len(processes) = 0, want >= 1")
	}
	if result.Processes[0].PID != os.Getpid() {
		t.Fatalf("first pid = %d, want %d", result.Processes[0].PID, os.Getpid())
	}
}

func TestKillProcessRequiresApprovalAndTerminatesProcess(t *testing.T) {
	workspace := t.TempDir()
	registry := newBuiltinRegistry(t, Options{WorkspaceRoot: workspace})
	tool := toolDefinitionByName(t, registry, "kill_process")
	if _, ok := tool.Parameters.Properties["confirm"]; ok {
		t.Fatal("kill_process confirm parameter present, want absent")
	}
	if tool.ApprovalMode != coretypes.ToolApprovalModeAlways {
		t.Fatalf("kill_process ApprovalMode = %q, want %q", tool.ApprovalMode, coretypes.ToolApprovalModeAlways)
	}
	policy, ok := registry.ApprovalPolicy("kill_process")
	if !ok {
		t.Fatal("ApprovalPolicy(kill_process) ok = false, want true")
	}
	if requirement := policy.Evaluate(map[string]any{"pid": 123}); !requirement.Required {
		t.Fatal("kill_process approval Required = false, want true")
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess", "--", "sleep")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("helper Start() error = %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	if _, err := registry.Execute(context.Background(), "kill_process", map[string]any{
		"pid": cmd.Process.Pid,
	}); err != nil {
		t.Fatalf("Execute(kill_process) error = %v", err)
	}

	waitDone := make(chan error, 1)
	go func() {
		waitDone <- cmd.Wait()
	}()

	select {
	case err := <-waitDone:
		if err == nil {
			t.Fatal("Wait() error = nil, want killed process error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("helper process did not exit after kill")
	}
}

func toolDefinitionByName(t *testing.T, registry *coretools.Registry, name string) coretypes.Tool {
	t.Helper()
	for _, definition := range registry.List() {
		if definition.Name == name {
			return definition
		}
	}
	t.Fatalf("tool %q not found", name)
	return coretypes.Tool{}
}

func TestGetSystemInfo(t *testing.T) {
	workspace := t.TempDir()
	registry := newBuiltinRegistry(t, Options{WorkspaceRoot: workspace})

	raw, err := registry.Execute(context.Background(), "get_system_info", nil)
	if err != nil {
		t.Fatalf("Execute(get_system_info) error = %v", err)
	}

	var result struct {
		OS       string `json:"os"`
		Arch     string `json:"arch"`
		Hostname string `json:"hostname"`
		NumCPU   int    `json:"num_cpu"`
	}
	decodeJSON(t, raw, &result)

	if result.OS != runtime.GOOS || result.Arch != runtime.GOARCH {
		t.Fatalf("system info = %#v, want os=%q arch=%q", result, runtime.GOOS, runtime.GOARCH)
	}
	if result.Hostname == "" || result.NumCPU <= 0 {
		t.Fatalf("system info = %#v, want hostname and cpu count", result)
	}
}

func TestHTTPRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Test"); got != "yes" {
			t.Fatalf("header X-Test = %q, want %q", got, "yes")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("echo:" + string(body)))
	}))
	defer server.Close()

	workspace := t.TempDir()
	registry := newBuiltinRegistry(t, Options{WorkspaceRoot: workspace, HTTPClient: server.Client()})

	raw, err := registry.Execute(context.Background(), "http_request", map[string]any{
		"method": "POST",
		"url":    server.URL,
		"headers": map[string]any{
			"X-Test": "yes",
		},
		"body": "payload",
	})
	if err != nil {
		t.Fatalf("Execute(http_request) error = %v", err)
	}

	var result struct {
		StatusCode int    `json:"status_code"`
		Body       string `json:"body"`
	}
	decodeJSON(t, raw, &result)

	if result.StatusCode != http.StatusCreated {
		t.Fatalf("status_code = %d, want %d", result.StatusCode, http.StatusCreated)
	}
	if result.Body != "echo:payload" {
		t.Fatalf("body = %q, want %q", result.Body, "echo:payload")
	}
}

func TestWebSearch(t *testing.T) {
	t.Run("tavily default provider", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got := r.URL.Path; got != "/search" {
				t.Fatalf("path = %q, want /search", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"results":[{"title":"Golang","url":"https://go.dev","content":"The Go programming language"}]}`))
		}))
		defer server.Close()

		workspace := t.TempDir()
		registry := newBuiltinRegistry(t, Options{
			WorkspaceRoot: workspace,
			HTTPClient:    server.Client(),
			WebSearch: WebSearchOptions{
				DefaultProvider: "tavily",
				Tavily: &TavilyConfig{
					APIKey:  "test-key",
					BaseURL: server.URL,
				},
			},
		})

		raw, err := registry.Execute(context.Background(), "web_search", map[string]any{"query": "golang"})
		if err != nil {
			t.Fatalf("Execute(web_search) error = %v", err)
		}

		var result struct {
			Provider string `json:"provider"`
			Results  []struct {
				Title string `json:"title"`
				URL   string `json:"url"`
			} `json:"results"`
		}
		decodeJSON(t, raw, &result)

		if result.Provider != "tavily" {
			t.Fatalf("provider = %q, want %q", result.Provider, "tavily")
		}
		if len(result.Results) != 1 || result.Results[0].Title != "Golang" {
			t.Fatalf("results = %#v, want one Golang result", result.Results)
		}
	})

	t.Run("serpapi provider", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got := r.URL.Path; got != "/search.json" {
				t.Fatalf("path = %q, want /search.json", got)
			}
			if got := r.URL.Query().Get("api_key"); got != "serp-key" {
				t.Fatalf("api_key = %q, want %q", got, "serp-key")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"organic_results":[{"title":"Serp Result","link":"https://example.com/serp","snippet":"serp snippet"}]}`))
		}))
		defer server.Close()

		registry := newBuiltinRegistry(t, Options{
			WorkspaceRoot: t.TempDir(),
			HTTPClient:    server.Client(),
			WebSearch: WebSearchOptions{
				SerpAPI: &SerpAPIConfig{APIKey: "serp-key", BaseURL: server.URL},
			},
		})

		raw, err := registry.Execute(context.Background(), "web_search", map[string]any{
			"provider": "serpapi",
			"query":    "golang",
		})
		if err != nil {
			t.Fatalf("Execute(web_search serpapi) error = %v", err)
		}

		var result struct {
			Provider string `json:"provider"`
			Results  []struct {
				Title string `json:"title"`
				URL   string `json:"url"`
			} `json:"results"`
		}
		decodeJSON(t, raw, &result)
		if result.Provider != "serpapi" || len(result.Results) != 1 || result.Results[0].Title != "Serp Result" {
			t.Fatalf("serpapi result = %#v, want one Serp Result", result)
		}
	})

	t.Run("bing provider", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got := r.URL.Path; got != "/v7.0/search" {
				t.Fatalf("path = %q, want /v7.0/search", got)
			}
			if got := r.Header.Get("Ocp-Apim-Subscription-Key"); got != "bing-key" {
				t.Fatalf("bing key = %q, want %q", got, "bing-key")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"webPages":{"value":[{"name":"Bing Result","url":"https://example.com/bing","snippet":"bing snippet"}]}}`))
		}))
		defer server.Close()

		registry := newBuiltinRegistry(t, Options{
			WorkspaceRoot: t.TempDir(),
			HTTPClient:    server.Client(),
			WebSearch: WebSearchOptions{
				Bing: &BingConfig{APIKey: "bing-key", BaseURL: server.URL},
			},
		})

		raw, err := registry.Execute(context.Background(), "web_search", map[string]any{
			"provider": "bing",
			"query":    "golang",
		})
		if err != nil {
			t.Fatalf("Execute(web_search bing) error = %v", err)
		}

		var result struct {
			Provider string `json:"provider"`
			Results  []struct {
				Title string `json:"title"`
				URL   string `json:"url"`
			} `json:"results"`
		}
		decodeJSON(t, raw, &result)
		if result.Provider != "bing" || len(result.Results) != 1 || result.Results[0].Title != "Bing Result" {
			t.Fatalf("bing result = %#v, want one Bing Result", result)
		}
	})

	t.Run("missing provider config", func(t *testing.T) {
		registry := newBuiltinRegistry(t, Options{WorkspaceRoot: t.TempDir()})
		_, err := registry.Execute(context.Background(), "web_search", map[string]any{
			"provider": "bing",
			"query":    "golang",
		})
		if err == nil {
			t.Fatal("Execute(web_search missing provider) error = nil, want non-nil")
		}
	})

	t.Run("provider returns non-2xx", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"bad key"}`))
		}))
		defer server.Close()

		registry := newBuiltinRegistry(t, Options{
			WorkspaceRoot: t.TempDir(),
			HTTPClient:    server.Client(),
			WebSearch: WebSearchOptions{
				DefaultProvider: "tavily",
				Tavily:          &TavilyConfig{APIKey: "bad", BaseURL: server.URL},
			},
		})

		_, err := registry.Execute(context.Background(), "web_search", map[string]any{"query": "golang"})
		if err == nil {
			t.Fatal("Execute(web_search non-2xx) error = nil, want non-nil")
		}
	})
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	select {}
}

func newBuiltinRegistry(t *testing.T, options Options) *coretools.Registry {
	t.Helper()
	registry := coretools.NewRegistry()
	if err := Register(registry, options); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	return registry
}

func mustWriteFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

func mustReadText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	return string(data)
}

func decodeJSON(t *testing.T, raw string, target any) {
	t.Helper()
	if err := json.Unmarshal([]byte(raw), target); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", raw, err)
	}
}
