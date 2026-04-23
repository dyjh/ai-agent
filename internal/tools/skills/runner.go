package skills

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"local-agent/internal/core"
	"local-agent/internal/tools"
)

var redactionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(api[_-]?key|token|password|secret|authorization|cookie)\s*[:=]\s*([^\s,;]+)`),
	regexp.MustCompile(`(?i)(bearer)\s+[a-z0-9._\-]+`),
}

// Runner executes registered local skills via their manifest runtime.
type Runner struct {
	Manager        *Manager
	MaxOutputChars int
}

// Execute validates the skill invocation and executes the declared runtime snapshot.
func (r *Runner) Execute(ctx context.Context, input map[string]any) (*core.ToolResult, error) {
	skillID, err := tools.GetString(input, "skill_id")
	if err != nil {
		return failedResult("", time.Time{}, 0, err.Error()), err
	}
	args := tools.GetMap(input, "args")
	if args == nil {
		args = map[string]any{}
	}
	if r.Manager == nil {
		err := fmt.Errorf("skill manager is not configured")
		return failedResult(skillID, time.Time{}, 0, err.Error()), err
	}

	entry, err := r.Manager.Resolve(skillID)
	if err != nil {
		return failedResult(skillID, time.Time{}, 0, err.Error()), err
	}
	if !entry.Registration.Enabled {
		err := fmt.Errorf("skill %s is disabled", skillID)
		return failedResult(skillID, time.Time{}, 0, err.Error()), err
	}
	if err := ValidateInput(entry.Manifest.InputSchema, args); err != nil {
		err = fmt.Errorf("skill input validation failed: %w", err)
		return failedResult(skillID, time.Time{}, 0, err.Error()), err
	}

	return r.run(ctx, entry, args)
}

func (r *Runner) run(ctx context.Context, entry RegisteredSkill, args map[string]any) (*core.ToolResult, error) {
	payload, err := json.Marshal(args)
	if err != nil {
		err = fmt.Errorf("marshal skill args: %w", err)
		return failedResult(entry.Registration.ID, time.Time{}, 0, err.Error()), err
	}

	timeout := entry.Manifest.Runtime.TimeoutSeconds
	if timeout <= 0 {
		timeout = 30
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	startedAt := time.Now().UTC()
	cmd, err := buildCommand(runCtx, entry, payload, args)
	if err != nil {
		return failedResult(entry.Registration.ID, startedAt, 0, err.Error()), err
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	finishedAt := time.Now().UTC()
	durationMS := finishedAt.Sub(startedAt).Milliseconds()
	rawStdout := stdout.String()
	stdoutText := truncate(redactString(rawStdout), r.MaxOutputChars)
	stderrText := truncate(redactString(stderr.String()), r.MaxOutputChars)
	exitCode := exitCode(runErr)

	output, parseErr := parseOutput(entry.Manifest.Runtime.OutputMode, rawStdout)
	if parseErr != nil {
		runErr = parseErr
	}
	output = sanitizeOutput(output)
	if parseErr == nil && entry.Manifest.Runtime.OutputMode == OutputModeJSONStdout {
		if raw, err := json.Marshal(output); err == nil {
			stdoutText = truncate(string(raw), r.MaxOutputChars)
		}
	}

	status := "ok"
	switch {
	case runCtx.Err() == context.DeadlineExceeded:
		status = "timeout"
		if runErr == nil {
			runErr = fmt.Errorf("skill %s timed out after %ds", entry.Registration.ID, timeout)
		}
	case runErr != nil:
		status = "error"
	}

	result := &core.ToolResult{
		Output: map[string]any{
			"skill_id":    entry.Registration.ID,
			"status":      status,
			"output":      output,
			"stdout":      stdoutText,
			"stderr":      stderrText,
			"exit_code":   exitCode,
			"duration_ms": durationMS,
		},
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
	}
	if runErr != nil {
		result.Error = runErr.Error()
	}
	return result, runErr
}

func buildCommand(ctx context.Context, entry RegisteredSkill, payload []byte, args map[string]any) (*exec.Cmd, error) {
	runtime := entry.Manifest.Runtime
	commandPath := runtime.Command
	if !filepath.IsAbs(commandPath) {
		commandPath = filepath.Join(entry.Root, commandPath)
	}
	cwd := entry.Root
	if runtime.Cwd != "" {
		if filepath.IsAbs(runtime.Cwd) {
			cwd = runtime.Cwd
		} else {
			cwd = filepath.Join(entry.Root, runtime.Cwd)
		}
	}

	commandArgs := append([]string(nil), runtime.Args...)
	if runtime.InputMode == InputModeArgs {
		commandArgs = append(commandArgs, argsToFlags(args)...)
	}

	var cmd *exec.Cmd
	switch runtime.Type {
	case RuntimeTypeScript:
		cmd = exec.CommandContext(ctx, "/bin/sh", append([]string{commandPath}, commandArgs...)...)
	default:
		cmd = exec.CommandContext(ctx, commandPath, commandArgs...)
	}
	cmd.Dir = cwd

	switch runtime.InputMode {
	case InputModeJSONStdin:
		cmd.Stdin = bytes.NewReader(payload)
	case InputModeEnv:
		cmd.Env = append(os.Environ(), buildEnvArgs(payload, args)...)
	default:
		cmd.Env = os.Environ()
	}
	if runtime.InputMode != InputModeEnv {
		cmd.Env = append(cmd.Env, "SKILL_INPUT_JSON="+string(payload))
	}
	return cmd, nil
}

func parseOutput(mode, stdout string) (map[string]any, error) {
	switch mode {
	case OutputModeText:
		return map[string]any{"text": strings.TrimSpace(stdout)}, nil
	default:
		if strings.TrimSpace(stdout) == "" {
			return map[string]any{}, nil
		}
		var decoded any
		if err := json.Unmarshal([]byte(stdout), &decoded); err != nil {
			return nil, fmt.Errorf("parse skill json stdout: %w", err)
		}
		switch value := decoded.(type) {
		case map[string]any:
			return value, nil
		default:
			return map[string]any{"value": value}, nil
		}
	}
}

func argsToFlags(args map[string]any) []string {
	if len(args) == 0 {
		return nil
	}
	keys := make([]string, 0, len(args))
	for key := range args {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	items := make([]string, 0, len(keys)*2)
	for _, key := range keys {
		items = append(items, "--"+key, scalarString(args[key]))
	}
	return items
}

func buildEnvArgs(payload []byte, args map[string]any) []string {
	items := []string{"SKILL_INPUT_JSON=" + string(payload)}
	keys := make([]string, 0, len(args))
	for key := range args {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		envKey := "SKILL_ARG_" + strings.ToUpper(strings.ReplaceAll(key, "-", "_"))
		items = append(items, envKey+"="+scalarString(args[key]))
	}
	return items
}

func scalarString(value any) string {
	switch item := value.(type) {
	case string:
		return item
	case nil:
		return ""
	default:
		raw, err := json.Marshal(item)
		if err != nil {
			return fmt.Sprint(item)
		}
		return string(raw)
	}
}

func sanitizeOutput(input map[string]any) map[string]any {
	output := map[string]any{}
	for key, value := range input {
		if isSensitiveKey(key) {
			output[key] = "[REDACTED]"
			continue
		}
		output[key] = sanitizeValue(value)
	}
	return output
}

func sanitizeValue(value any) any {
	switch item := value.(type) {
	case string:
		return redactString(item)
	case []any:
		out := make([]any, 0, len(item))
		for _, child := range item {
			out = append(out, sanitizeValue(child))
		}
		return out
	case map[string]any:
		return sanitizeOutput(item)
	default:
		return value
	}
}

func isSensitiveKey(key string) bool {
	key = strings.ToLower(key)
	return strings.Contains(key, "token") ||
		strings.Contains(key, "password") ||
		strings.Contains(key, "secret") ||
		strings.Contains(key, "api_key") ||
		strings.Contains(key, "apikey") ||
		strings.Contains(key, "authorization") ||
		strings.Contains(key, "cookie")
}

func redactString(value string) string {
	out := value
	for _, pattern := range redactionPatterns {
		out = pattern.ReplaceAllStringFunc(out, func(match string) string {
			parts := strings.SplitN(match, "=", 2)
			if len(parts) == 2 {
				return parts[0] + "=[REDACTED]"
			}
			parts = strings.SplitN(match, ":", 2)
			if len(parts) == 2 {
				return parts[0] + ":[REDACTED]"
			}
			if strings.HasPrefix(strings.ToLower(match), "bearer ") {
				return "Bearer [REDACTED]"
			}
			return "[REDACTED]"
		})
	}
	return out
}

func failedResult(skillID string, startedAt time.Time, durationMS int64, errMsg string) *core.ToolResult {
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	finishedAt := startedAt.Add(time.Duration(durationMS) * time.Millisecond)
	if durationMS == 0 {
		finishedAt = time.Now().UTC()
	}
	return &core.ToolResult{
		Output: map[string]any{
			"skill_id":    skillID,
			"status":      "error",
			"output":      map[string]any{},
			"stdout":      "",
			"stderr":      "",
			"exit_code":   -1,
			"duration_ms": durationMS,
		},
		Error:      errMsg,
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
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
	return errors.As(err, target)
}
