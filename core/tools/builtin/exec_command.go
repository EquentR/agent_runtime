package builtin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"time"

	corelog "github.com/EquentR/agent_runtime/core/log"
	coretools "github.com/EquentR/agent_runtime/core/tools"
	"github.com/EquentR/agent_runtime/core/types"
)

func newExecCommandTool(env runtimeEnv) coretools.Tool {
	return coretools.Tool{
		Name:              "exec_command",
		Description:       "Execute a command in the workspace",
		Source:            "builtin",
		ApprovalMode:      types.ToolApprovalModeConditional,
		ApprovalEvaluator: evaluateExecCommandApproval,
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
			result := struct {
				Success  bool   `json:"success"`
				ExitCode int    `json:"exit_code"`
				Stdout   string `json:"stdout"`
				Stderr   string `json:"stderr"`
				TimedOut bool   `json:"timed_out"`
				Cwd      string `json:"cwd"`
			}{
				Success:  runErr == nil,
				ExitCode: 0,
				Stdout:   stdout.String(),
				Stderr:   stderr.String(),
				TimedOut: errors.Is(commandCtx.Err(), context.DeadlineExceeded),
				Cwd:      cwdValue,
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

func evaluateExecCommandApproval(arguments map[string]any) coretools.ApprovalRequirement {
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

	head := tokens[0]
	switch {
	case isDeletionCommand(head, tokens[1:]):
		return coretools.ApprovalRequirement{
			Required:         true,
			ArgumentsSummary: fmt.Sprintf("command=%s", commandLine),
			RiskLevel:        coretools.RiskLevelHigh,
			Reason:           fmt.Sprintf("command may delete files or directories: %s", commandLine),
		}
	case isProcessKillCommand(head):
		return coretools.ApprovalRequirement{
			Required:         true,
			ArgumentsSummary: fmt.Sprintf("command=%s", commandLine),
			RiskLevel:        coretools.RiskLevelHigh,
			Reason:           fmt.Sprintf("command may terminate processes: %s", commandLine),
		}
	case isSystemMutationCommand(head, tokens[1:]):
		return coretools.ApprovalRequirement{
			Required:         true,
			ArgumentsSummary: fmt.Sprintf("command=%s", commandLine),
			RiskLevel:        coretools.RiskLevelHigh,
			Reason:           fmt.Sprintf("command may mutate the system outside the workspace: %s", commandLine),
		}
	default:
		return coretools.ApprovalRequirement{}
	}
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

func isDeletionCommand(head string, args []string) bool {
	if slices.Contains([]string{"rm", "rmdir", "del", "erase", "rd", "unlink", "remove-item", "shred"}, head) {
		return true
	}
	return head == "git" && len(args) > 0 && args[0] == "clean"
}

func isProcessKillCommand(head string) bool {
	return slices.Contains([]string{"kill", "pkill", "killall", "taskkill", "stop-process"}, head)
}

func isSystemMutationCommand(head string, args []string) bool {
	if slices.Contains([]string{"shutdown", "reboot", "halt", "poweroff", "mount", "umount", "mkfs", "diskpart", "useradd", "userdel", "usermod", "groupadd", "groupdel"}, head) {
		return true
	}
	if slices.Contains([]string{"apt", "apt-get", "yum", "dnf", "zypper", "apk", "pacman", "brew", "pip", "pip3", "npm", "pnpm"}, head) {
		return len(args) > 0 && slices.Contains([]string{"install", "uninstall", "remove", "upgrade", "update", "add"}, args[0])
	}
	if slices.Contains([]string{"systemctl", "service", "sc", "launchctl"}, head) {
		return len(args) > 0 && slices.Contains([]string{"start", "stop", "restart", "reload", "enable", "disable", "mask", "unmask", "delete"}, args[0])
	}
	return false
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
