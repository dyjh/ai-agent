package ops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"local-agent/internal/core"
	"local-agent/internal/security"
)

const (
	defaultOpsTimeoutSeconds = 15
	defaultOpsMaxOutputBytes = int64(20000)
)

// CommandResult captures a fixed command invocation.
type CommandResult struct {
	Command  []string `json:"command"`
	Stdout   string   `json:"stdout"`
	Stderr   string   `json:"stderr"`
	ExitCode int      `json:"exit_code"`
}

// CommandRunner runs fixed command/args pairs. It must not accept shell text.
type CommandRunner interface {
	Run(ctx context.Context, command string, args []string, timeout time.Duration, maxOutputBytes int64) (CommandResult, error)
}

// ExecCommandRunner runs local binaries without shell interpolation.
type ExecCommandRunner struct{}

// Run implements CommandRunner.
func (ExecCommandRunner) Run(ctx context.Context, command string, args []string, timeout time.Duration, maxOutputBytes int64) (CommandResult, error) {
	if timeout <= 0 {
		timeout = defaultOpsTimeoutSeconds * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, command, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := CommandResult{
		Command:  append([]string{command}, args...),
		Stdout:   trimAndRedact(stdout.String(), maxOutputBytes),
		Stderr:   trimAndRedact(stderr.String(), maxOutputBytes),
		ExitCode: commandExitCode(err),
	}
	if err != nil {
		return result, err
	}
	return result, nil
}

func commandResultToolResult(output map[string]any, result CommandResult, err error) (*core.ToolResult, error) {
	if output == nil {
		output = map[string]any{}
	}
	output["command"] = redactCommand(result.Command)
	output["stdout"] = trimAndRedact(result.Stdout, defaultOpsMaxOutputBytes)
	output["stderr"] = trimAndRedact(result.Stderr, defaultOpsMaxOutputBytes)
	output["exit_code"] = result.ExitCode
	toolResult := &core.ToolResult{
		Output:     output,
		StartedAt:  time.Now().UTC(),
		FinishedAt: time.Now().UTC(),
	}
	if err != nil {
		toolResult.Error = security.RedactString(err.Error())
	}
	return toolResult, err
}

func trimAndRedact(value string, maxBytes int64) string {
	value = security.RedactString(value)
	if maxBytes <= 0 {
		maxBytes = defaultOpsMaxOutputBytes
	}
	if int64(len(value)) <= maxBytes {
		return value
	}
	return value[:maxBytes] + "\n[truncated]"
}

func redactCommand(parts []string) []string {
	if len(parts) == 0 {
		return nil
	}
	out := make([]string, 0, len(parts))
	redactNext := false
	for _, part := range parts {
		if redactNext {
			out = append(out, "[REDACTED]")
			redactNext = false
			continue
		}
		lower := strings.ToLower(part)
		if lower == "-i" || lower == "--identity-file" || lower == "--kubeconfig" || lower == "--token" || lower == "--password" {
			out = append(out, part)
			redactNext = true
			continue
		}
		if strings.HasPrefix(lower, "--kubeconfig=") || strings.HasPrefix(lower, "--token=") || strings.Contains(lower, "password=") {
			key := part
			if idx := strings.Index(part, "="); idx >= 0 {
				key = part[:idx+1]
			}
			out = append(out, key+"[REDACTED]")
			continue
		}
		if security.IsSensitivePath(part, nil) {
			out = append(out, "[REDACTED_PATH]")
			continue
		}
		out = append(out, security.RedactString(part))
	}
	return out
}

func commandExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func ensureSafeIdentifier(kind, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s is required", kind)
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			continue
		}
		switch r {
		case '.', '-', '_', '/', ':', '@':
			continue
		default:
			return fmt.Errorf("%s contains unsupported character %q", kind, r)
		}
	}
	return nil
}

func outputLimit(input map[string]any, fallback int64) int64 {
	if fallback <= 0 {
		fallback = defaultOpsMaxOutputBytes
	}
	switch value := input["max_output_bytes"].(type) {
	case int:
		if value > 0 {
			return int64(value)
		}
	case int64:
		if value > 0 {
			return value
		}
	case float64:
		if value > 0 {
			return int64(value)
		}
	}
	return fallback
}

func timeoutFromInput(input map[string]any, fallbackSeconds int) time.Duration {
	if fallbackSeconds <= 0 {
		fallbackSeconds = defaultOpsTimeoutSeconds
	}
	seconds := fallbackSeconds
	switch value := input["timeout_seconds"].(type) {
	case int:
		if value > 0 {
			seconds = value
		}
	case int64:
		if value > 0 {
			seconds = int(value)
		}
	case float64:
		if value > 0 {
			seconds = int(value)
		}
	}
	return time.Duration(seconds) * time.Second
}
