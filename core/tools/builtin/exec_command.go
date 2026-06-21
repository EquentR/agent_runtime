package builtin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	corelog "github.com/EquentR/agent_runtime/core/log"
	coretools "github.com/EquentR/agent_runtime/core/tools"
	"github.com/EquentR/agent_runtime/core/types"
	"github.com/EquentR/agent_runtime/core/workspaces"
)

func newExecCommandTool(env runtimeEnv) coretools.Tool {
	return coretools.Tool{
		Name:         "exec_command",
		Description:  "Execute a command in the workspace",
		Source:       "builtin",
		ApprovalMode: types.ToolApprovalModeConditional,
		ApprovalEvaluator: func(arguments map[string]any) coretools.ApprovalRequirement {
			return evaluateExecCommandApproval(context.Background(), env.workspaceMode, arguments, env.commandJudge)
		},
		Parameters: objectSchema([]string{"command"}, map[string]types.SchemaProperty{
			"command":           {Type: "string", Description: "Command to execute"},
			"args":              stringArrayProperty("Optional command arguments"),
			"use_shell":         {Type: "boolean", Description: "Execute through the system shell"},
			"working_directory": {Type: "string", Description: "Working directory relative to workspace"},
			"timeout_seconds":   {Type: "integer", Description: "Per-call timeout in seconds"},
		}),
		Handler: func(ctx context.Context, arguments map[string]any) (string, error) {
			command, err := requiredStringArg(arguments, "command")
			if err != nil {
				return "", err
			}
			args, err := stringSliceArg(arguments, "args")
			if err != nil {
				return "", err
			}
			useShell, err := boolArg(arguments, "use_shell", false)
			if err != nil {
				return "", err
			}
			workingDirectory, ok, err := optionalStringArg(arguments, "working_directory")
			if err != nil {
				return "", err
			}
			cwd := env.workspaceRoot
			cwdValue := "."
			if ok && workingDirectory != "" {
				cwd, cwdValue, err = env.resolveWorkspaceDir(workingDirectory, true)
				if err != nil {
					return "", err
				}
			}

			timeout, err := intArg(arguments, "timeout_seconds", int(env.commandTimeout/time.Second))
			if err != nil {
				return "", err
			}
			startedAt := time.Now()
			logToolStart(ctx, "exec_command", corelog.String("command", command), corelog.Int("args_count", len(args)), corelog.String("cwd", cwdValue), corelog.Int("timeout_seconds", timeout), corelog.Bool("use_shell", useShell))
			commandCtx, cancel := context.WithTimeout(ctx, clampDuration(time.Duration(timeout)*time.Second, minCommandTimeout, maxCommandTimeout))
			defer cancel()

			cmd := buildExecCommand(commandCtx, command, args, useShell)
			cmd.Dir = cwd

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			runErr := cmd.Run()
			shapedOutput := shapeCommandOutput(stdout.String(), stderr.String(), env.outputBudget)
			result := struct {
				Success             bool   `json:"success"`
				ExitCode            int    `json:"exit_code"`
				Stdout              string `json:"stdout"`
				Stderr              string `json:"stderr"`
				TimedOut            bool   `json:"timed_out"`
				Cwd                 string `json:"cwd"`
				StdoutTruncated     bool   `json:"stdout_truncated"`
				StderrTruncated     bool   `json:"stderr_truncated"`
				OriginalStdoutBytes int    `json:"original_stdout_bytes"`
				OriginalStderrBytes int    `json:"original_stderr_bytes"`
				ReturnedStdoutBytes int    `json:"returned_stdout_bytes"`
				ReturnedStderrBytes int    `json:"returned_stderr_bytes"`
			}{
				Success:             runErr == nil,
				ExitCode:            0,
				Stdout:              shapedOutput.Stdout,
				Stderr:              shapedOutput.Stderr,
				TimedOut:            errors.Is(commandCtx.Err(), context.DeadlineExceeded),
				Cwd:                 cwdValue,
				StdoutTruncated:     shapedOutput.StdoutTruncated,
				StderrTruncated:     shapedOutput.StderrTruncated,
				OriginalStdoutBytes: shapedOutput.OriginalStdoutBytes,
				OriginalStderrBytes: shapedOutput.OriginalStderrBytes,
				ReturnedStdoutBytes: shapedOutput.ReturnedStdoutBytes,
				ReturnedStderrBytes: shapedOutput.ReturnedStderrBytes,
			}

			if runErr != nil {
				result.Success = false
				var exitErr *exec.ExitError
				switch {
				case errors.As(runErr, &exitErr):
					result.ExitCode = exitErr.ExitCode()
				case result.TimedOut:
					result.ExitCode = -1
					if result.Stderr == "" {
						result.Stderr = runErr.Error()
					}
				default:
					result.ExitCode = -1
					if result.Stderr == "" {
						result.Stderr = runErr.Error()
					}
				}
				logToolFailure(ctx, "exec_command", runErr, corelog.String("command", command), corelog.Int("args_count", len(args)), corelog.Int("exit_code", result.ExitCode), corelog.Bool("timed_out", result.TimedOut), corelog.Duration("duration", time.Since(startedAt)))
			} else {
				logToolFinish(ctx, "exec_command", corelog.String("command", command), corelog.Int("args_count", len(args)), corelog.Int("exit_code", result.ExitCode), corelog.Bool("timed_out", result.TimedOut), corelog.Duration("duration", time.Since(startedAt)))
			}

			return jsonResult(result)
		},
	}
}

func evaluateExecCommandApproval(ctx context.Context, mode workspaces.Mode, arguments map[string]any, judge CommandJudge) coretools.ApprovalRequirement {
	command, _ := arguments["command"].(string)
	command = strings.TrimSpace(command)
	if command == "" {
		return coretools.ApprovalRequirement{}
	}

	args, err := stringSliceArg(arguments, "args")
	if err != nil {
		args = nil
	}
	commandLine := strings.TrimSpace(strings.Join(append([]string{command}, args...), " "))
	tokens := unwrapCommandTokens(command, args)
	if len(tokens) == 0 {
		return coretools.ApprovalRequirement{}
	}

	judgeCommand := commandLine
	if wrappedCommand, ok := shellWrappedCommand(command, args); ok {
		judgeCommand = wrappedCommand
	}

	if requirement := evaluateExecCommandWithJudge(ctx, mode, arguments, judge, commandLine, judgeCommand); requirement.Decision != "" || requirement.Required || requirement.Reason != "" {
		return requirement
	}
	return coretools.ApprovalRequirement{}
}

func evaluateExecCommandWithJudge(ctx context.Context, mode workspaces.Mode, arguments map[string]any, judge CommandJudge, commandLine string, judgeCommand string) coretools.ApprovalRequirement {
	if judge == nil {
		return coretools.ApprovalRequirement{
			Decision:         coretools.ApprovalDecisionRequireApproval,
			Required:         true,
			ArgumentsSummary: fmt.Sprintf("command=%s", commandLine),
			RiskLevel:        coretools.RiskLevelMedium,
			Reason:           fmt.Sprintf("command judge unavailable; human approval required: %s", commandLine),
		}
	}

	normalizedMode := workspaceModeFromArguments(mode, arguments)
	result, err := judge.Evaluate(ctx, CommandJudgeRequest{
		Command:       judgeCommand,
		Arguments:     cloneApprovalArguments(arguments),
		WorkspaceMode: normalizedMode,
		ToolName:      "exec_command",
	})
	if err != nil {
		return coretools.ApprovalRequirement{
			Decision:         coretools.ApprovalDecisionRequireApproval,
			Required:         true,
			ArgumentsSummary: fmt.Sprintf("command=%s", commandLine),
			RiskLevel:        coretools.RiskLevelMedium,
			Reason:           fmt.Sprintf("command judge unavailable; human approval required: %s", commandLine),
		}
	}

	switch normalizeCommandVerdict(string(result.Verdict)) {
	case CommandVerdictSafe:
		return coretools.ApprovalRequirement{
			Decision:         coretools.ApprovalDecisionAllow,
			ArgumentsSummary: fmt.Sprintf("command=%s", commandLine),
			Reason:           strings.TrimSpace(result.Reason),
		}
	case CommandVerdictNeutral:
		if normalizedMode == workspaces.ModeReadonly {
			return coretools.ApprovalRequirement{
				Decision:         coretools.ApprovalDecisionRequireApproval,
				Required:         true,
				ArgumentsSummary: fmt.Sprintf("command=%s", commandLine),
				RiskLevel:        coretools.RiskLevelMedium,
				Reason:           firstNonEmpty(strings.TrimSpace(result.Reason), fmt.Sprintf("command judge marked command neutral in readonly workspace: %s", commandLine)),
			}
		}
		return coretools.ApprovalRequirement{
			Decision:         coretools.ApprovalDecisionAllow,
			ArgumentsSummary: fmt.Sprintf("command=%s", commandLine),
			Reason:           strings.TrimSpace(result.Reason),
		}
	case CommandVerdictRisky:
		if normalizedMode == workspaces.ModeReadonly {
			return coretools.ApprovalRequirement{
				Decision:         coretools.ApprovalDecisionBlock,
				Required:         false,
				ArgumentsSummary: fmt.Sprintf("command=%s", commandLine),
				RiskLevel:        coretools.RiskLevelHigh,
				Reason:           firstNonEmpty(strings.TrimSpace(result.Reason), fmt.Sprintf("command judge marked command risky in readonly workspace: %s", commandLine)),
			}
		}
		return coretools.ApprovalRequirement{
			Decision:         coretools.ApprovalDecisionRequireApproval,
			Required:         true,
			ArgumentsSummary: fmt.Sprintf("command=%s", commandLine),
			RiskLevel:        coretools.RiskLevelHigh,
			Reason:           firstNonEmpty(strings.TrimSpace(result.Reason), fmt.Sprintf("command judge marked command risky: %s", commandLine)),
		}
	case CommandVerdictUnavailable:
		fallthrough
	default:
		return coretools.ApprovalRequirement{
			Decision:         coretools.ApprovalDecisionRequireApproval,
			Required:         true,
			ArgumentsSummary: fmt.Sprintf("command=%s", commandLine),
			RiskLevel:        coretools.RiskLevelMedium,
			Reason:           firstNonEmpty(strings.TrimSpace(result.Reason), fmt.Sprintf("command judge unavailable; human approval required: %s", commandLine)),
		}
	}
}

func workspaceModeFromArguments(fallback workspaces.Mode, arguments map[string]any) workspaces.Mode {
	if fallback == workspaces.ModeReadonly || fallback == workspaces.ModeMutable {
		return fallback
	}
	raw := strings.TrimSpace(fmt.Sprint(arguments["workspace_mode"]))
	switch workspaces.Mode(raw) {
	case workspaces.ModeReadonly:
		return workspaces.ModeReadonly
	case workspaces.ModeMutable:
		return workspaces.ModeMutable
	default:
		return workspaces.ModeMutable
	}
}

func cloneApprovalArguments(arguments map[string]any) map[string]any {
	if len(arguments) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(arguments))
	for key, value := range arguments {
		cloned[key] = value
	}
	return cloned
}

func commandTokens(command string, args []string) []string {
	combined := append([]string{command}, args...)
	tokens := make([]string, 0, len(combined))
	for _, part := range combined {
		for _, token := range strings.Fields(strings.ToLower(strings.TrimSpace(part))) {
			if token != "" {
				tokens = append(tokens, token)
			}
		}
	}
	return tokens
}

func unwrapCommandTokens(command string, args []string) []string {
	tokens := commandTokens(command, args)
	if len(tokens) == 0 {
		return nil
	}

	if wrapped, ok := wrappedCommandTokens(tokens[0], args); ok {
		tokens = wrapped
	}
	return stripCommandPrefixes(tokens)
}

func wrappedCommandTokens(head string, args []string) ([]string, bool) {
	if commandString, ok := shellWrappedCommand(head, args); ok {
		return commandTokens(commandString, nil), true
	}
	return nil, false
}

func shellWrappedCommand(head string, args []string) (string, bool) {
	if len(args) < 2 {
		return "", false
	}

	switch strings.ToLower(strings.TrimSpace(head)) {
	case "sh", "bash", "zsh", "ksh", "dash":
		for i := 0; i < len(args)-1; i++ {
			switch strings.ToLower(strings.TrimSpace(args[i])) {
			case "-c", "-lc":
				return strings.TrimSpace(args[i+1]), true
			}
		}
	case "cmd":
		for i := 0; i < len(args)-1; i++ {
			switch strings.ToLower(strings.TrimSpace(args[i])) {
			case "/c", "/k":
				return strings.TrimSpace(args[i+1]), true
			}
		}
	case "powershell", "pwsh":
		for i := 0; i < len(args)-1; i++ {
			switch strings.ToLower(strings.TrimSpace(args[i])) {
			case "-command", "-c":
				return strings.TrimSpace(args[i+1]), true
			}
		}
	}

	return "", false
}

func stripCommandPrefixes(tokens []string) []string {
	current := append([]string(nil), tokens...)
	for {
		next, changed := trimKnownPrefix(current)
		if !changed {
			return current
		}
		current = next
		if len(current) == 0 {
			return current
		}
	}
}

func trimKnownPrefix(tokens []string) ([]string, bool) {
	if len(tokens) == 0 {
		return tokens, false
	}

	switch tokens[0] {
	case "sudo":
		return trimSudoPrefix(tokens)
	case "env":
		return trimEnvPrefix(tokens)
	case "nohup":
		if len(tokens) == 1 {
			return []string{}, true
		}
		return tokens[1:], true
	case "start-process":
		return trimStartProcessPrefix(tokens)
	default:
		return tokens, false
	}
}

func trimSudoPrefix(tokens []string) ([]string, bool) {
	if len(tokens) == 1 {
		return []string{}, true
	}
	index := 1
	for index < len(tokens) {
		token := tokens[index]
		if token == "--" {
			if index+1 >= len(tokens) {
				return []string{}, true
			}
			return tokens[index+1:], true
		}
		if !strings.HasPrefix(token, "-") {
			return tokens[index:], true
		}
		if token == "-u" || token == "-g" || token == "-h" || token == "-p" || token == "-c" || token == "-r" || token == "-t" || token == "-a" {
			index += 2
			continue
		}
		index++
	}
	return []string{}, true
}

func trimEnvPrefix(tokens []string) ([]string, bool) {
	if len(tokens) == 1 {
		return []string{}, true
	}
	index := 1
	for index < len(tokens) {
		token := tokens[index]
		if token == "--" {
			if index+1 >= len(tokens) {
				return []string{}, true
			}
			return tokens[index+1:], true
		}
		if strings.Contains(token, "=") && !strings.HasPrefix(token, "=") {
			index++
			continue
		}
		if strings.HasPrefix(token, "-") {
			index++
			continue
		}
		return tokens[index:], true
	}
	return []string{}, true
}

func trimStartProcessPrefix(tokens []string) ([]string, bool) {
	if len(tokens) == 1 {
		return []string{}, true
	}
	command := ""
	argumentTokens := []string{}
	for index := 1; index < len(tokens); index++ {
		token := tokens[index]
		if token == "-argumentlist" {
			if index+1 < len(tokens) {
				argumentTokens = append(argumentTokens, commandTokens(trimShellQuotes(tokens[index+1]), nil)...)
				index++
			}
			continue
		}
		if strings.HasPrefix(token, "-") {
			continue
		}
		if command == "" {
			command = trimShellQuotes(token)
			continue
		}
	}
	if command == "" {
		return []string{}, true
	}
	return append([]string{strings.ToLower(command)}, argumentTokens...), true
}

func trimShellQuotes(value string) string {
	return strings.Trim(value, `"'`)
}

func buildExecCommand(ctx context.Context, command string, args []string, useShell bool) *exec.Cmd {
	if !useShell {
		return exec.CommandContext(ctx, command, args...)
	}
	fullCommand := strings.TrimSpace(strings.Join(append([]string{command}, args...), " "))
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "cmd", "/C", fullCommand)
	}
	return exec.CommandContext(ctx, "/bin/sh", "-c", fullCommand)
}
