package builtin

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"time"

	coretools "github.com/EquentR/agent_runtime/core/tools"
	"github.com/EquentR/agent_runtime/core/types"
)

func newExecCommandTool(env runtimeEnv) coretools.Tool {
	return coretools.Tool{
		Name:        "exec_command",
		Description: "Execute a command in the workspace",
		Source:      "builtin",
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
			}

			return jsonResult(result)
		},
	}
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
