package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"local-agent/internal/core"
	"local-agent/internal/security"
	"local-agent/internal/tools"
)

// Runner executes registered local skills via their manifest runtime.
type Runner struct {
	Manager        *Manager
	Sandbox        SandboxRunner
	Sandboxes      []SandboxRunner
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
	prepared, err := prepareExecution(entry, args, int64(r.MaxOutputChars), r.availableSandboxes())
	if err != nil {
		return failedResult(entry.Registration.ID, time.Time{}, 0, err.Error()), err
	}

	result, runErr := prepared.Runner.Run(ctx, prepared.Request)
	if result == nil {
		return failedResult(entry.Registration.ID, time.Time{}, 0, fmt.Sprintf("skill execution failed: %v", runErr)), runErr
	}

	rawStdout := string(result.Stdout)
	rawStderr := string(result.Stderr)
	stdoutText := truncate(security.RedactString(rawStdout), r.MaxOutputChars)
	stderrText := truncate(security.RedactString(rawStderr), r.MaxOutputChars)

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
	case runErr != nil && strings.Contains(runErr.Error(), "timed out"):
		status = "timeout"
	case runErr != nil:
		status = "error"
	}

	toolResult := &core.ToolResult{
		Output: map[string]any{
			"skill_id":          entry.Registration.ID,
			"status":            status,
			"output":            output,
			"stdout":            stdoutText,
			"stderr":            stderrText,
			"exit_code":         result.ExitCode,
			"duration_ms":       result.FinishedAt.Sub(result.StartedAt).Milliseconds(),
			"warnings":          append([]string(nil), result.Warnings...),
			"metadata":          cloneMetadata(result.Metadata),
			"execution_profile": prepared.Profile,
		},
		StartedAt:  result.StartedAt,
		FinishedAt: result.FinishedAt,
	}
	if runErr != nil {
		toolResult.Error = runErr.Error()
	}
	return toolResult, runErr
}

func (r *Runner) availableSandboxes() []SandboxRunner {
	if len(r.Sandboxes) > 0 {
		return cloneSandboxRunners(r.Sandboxes)
	}
	if r.Sandbox != nil {
		return []SandboxRunner{r.Sandbox}
	}
	if r.Manager != nil {
		return r.Manager.AvailableSandboxes()
	}
	return defaultSandboxRunners()
}

func cloneMetadata(input map[string]any) map[string]any {
	if len(input) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
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
	return security.RedactMap(input)
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
