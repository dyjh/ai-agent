package shell

import (
	"bytes"
	"context"
	"os/exec"
	"time"

	"local-agent/internal/core"
	"local-agent/internal/tools"
)

// Executor executes an approved shell snapshot.
type Executor struct {
	DefaultShell   string
	DefaultTimeout int
	MaxOutputChars int
}

// Execute runs the snapshot under the requested shell.
func (e *Executor) Execute(ctx context.Context, input map[string]any) (*core.ToolResult, error) {
	command, err := tools.GetString(input, "command")
	if err != nil {
		return nil, err
	}

	shellName, _ := input["shell"].(string)
	if shellName == "" {
		shellName = e.DefaultShell
	}
	if shellName == "" {
		shellName = "bash"
	}

	cwd, _ := input["cwd"].(string)
	timeoutSeconds := tools.GetInt(input, "timeout_seconds", e.DefaultTimeout)
	if timeoutSeconds <= 0 {
		timeoutSeconds = 60
	}

	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	startedAt := time.Now().UTC()
	cmd := buildCommand(runCtx, shellName, command)
	if cwd != "" {
		cmd.Dir = cwd
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	finishedAt := time.Now().UTC()

	stdoutStr := truncate(stdout.String(), e.MaxOutputChars)
	stderrStr := truncate(stderr.String(), e.MaxOutputChars)

	result := &core.ToolResult{
		ToolCallID: "",
		Output: map[string]any{
			"command":   command,
			"cwd":       cwd,
			"stdout":    stdoutStr,
			"stderr":    stderrStr,
			"exit_code": exitCode(runErr),
		},
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
	}
	if runErr != nil {
		result.Error = runErr.Error()
	}
	return result, runErr
}

func buildCommand(ctx context.Context, shellName, command string) *exec.Cmd {
	switch shellName {
	case "powershell", "pwsh":
		return exec.CommandContext(ctx, shellName, "-Command", command)
	case "cmd":
		return exec.CommandContext(ctx, shellName, "/C", command)
	default:
		return exec.CommandContext(ctx, shellName, "-lc", command)
	}
}

func truncate(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit]
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if ok := errorAs(err, &exitErr); ok {
		return exitErr.ExitCode()
	}
	return -1
}

func errorAs(err error, target interface{}) bool {
	return execErrorAs(err, target)
}
