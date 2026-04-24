package gittools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"local-agent/internal/core"
	"local-agent/internal/security"
	"local-agent/internal/tools"
)

const (
	defaultGitTimeoutSeconds = 30
	defaultGitOutputBytes    = int64(20000)
)

// Executor runs a fixed git operation inside a workspace.
type Executor struct {
	Root           string
	Operation      string
	TimeoutSeconds int
	MaxOutputBytes int64
}

// Execute implements the git.* tools.
func (e *Executor) Execute(ctx context.Context, input map[string]any) (*core.ToolResult, error) {
	workspace, err := e.resolveWorkspace(input)
	if err != nil {
		return nil, err
	}
	paths, err := e.resolvePaths(workspace, input)
	if err != nil {
		return nil, err
	}
	args, err := e.gitArgs(input, paths)
	if err != nil {
		return nil, err
	}

	timeout := tools.GetInt(input, "timeout_seconds", e.timeoutSeconds())
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	startedAt := time.Now().UTC()
	cmd := exec.CommandContext(runCtx, "git", args...)
	cmd.Dir = workspace
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	finishedAt := time.Now().UTC()

	stdoutText, stdoutTruncated := truncateBytes(security.RedactString(stdout.String()), e.maxOutputBytes())
	stderrText, stderrTruncated := truncateBytes(security.RedactString(stderr.String()), e.maxOutputBytes())
	exitCode := processExitCode(runErr)
	if runCtx.Err() == context.DeadlineExceeded {
		exitCode = -1
	}
	output := map[string]any{
		"workspace":   filepath.ToSlash(workspace),
		"operation":   e.Operation,
		"command":     "git " + strings.Join(args, " "),
		"args":        args,
		"paths":       paths,
		"stdout":      stdoutText,
		"stderr":      stderrText,
		"exit_code":   exitCode,
		"truncated":   stdoutTruncated || stderrTruncated,
		"duration_ms": finishedAt.Sub(startedAt).Milliseconds(),
	}
	if message, _ := input["message"].(string); message != "" {
		output["message"] = security.RedactString(message)
	}
	if runErr != nil {
		output["error"] = security.RedactString(runErr.Error())
	}
	return &core.ToolResult{
		Output:     output,
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
	}, nil
}

func (e *Executor) gitArgs(input map[string]any, paths []string) ([]string, error) {
	switch e.Operation {
	case "status":
		args := []string{"status", "--short", "--branch"}
		return appendPathspec(args, paths), nil
	case "diff":
		return appendPathspec([]string{"diff"}, paths), nil
	case "log":
		limit := tools.GetInt(input, "limit", 20)
		if limit <= 0 || limit > 100 {
			limit = 20
		}
		return []string{"log", "--oneline", "-n", fmt.Sprint(limit)}, nil
	case "branch":
		return []string{"branch", "--show-current"}, nil
	case "add":
		if len(paths) == 0 {
			return nil, errors.New("git.add requires at least one path")
		}
		return appendPathspec([]string{"add"}, paths), nil
	case "commit":
		message, _ := input["message"].(string)
		if strings.TrimSpace(message) == "" {
			return nil, errors.New("git.commit requires message")
		}
		return []string{"commit", "-m", message}, nil
	case "restore":
		if len(paths) == 0 {
			return nil, errors.New("git.restore requires at least one path")
		}
		return appendPathspec([]string{"restore"}, paths), nil
	case "clean":
		return appendPathspec([]string{"clean", "-fd"}, paths), nil
	default:
		return nil, fmt.Errorf("unsupported git operation: %s", e.Operation)
	}
}

func appendPathspec(args []string, paths []string) []string {
	if len(paths) == 0 {
		return args
	}
	out := append([]string(nil), args...)
	out = append(out, "--")
	out = append(out, paths...)
	return out
}

func (e *Executor) resolveWorkspace(input map[string]any) (string, error) {
	path, _ := input["workspace"].(string)
	if strings.TrimSpace(path) == "" {
		path = "."
	}
	root, err := filepath.Abs(defaultRoot(e.Root))
	if err != nil {
		return "", err
	}
	root = filepath.Clean(root)
	var workspace string
	if filepath.IsAbs(path) {
		workspace = filepath.Clean(path)
	} else {
		workspace = filepath.Clean(filepath.Join(root, path))
	}
	rel, err := filepath.Rel(root, workspace)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", errors.New("workspace escapes configured root")
	}
	return workspace, nil
}

func (e *Executor) resolvePaths(workspace string, input map[string]any) ([]string, error) {
	rawPaths := stringSlice(input["paths"])
	paths := make([]string, 0, len(rawPaths))
	for _, path := range rawPaths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		var abs string
		if filepath.IsAbs(path) {
			abs = filepath.Clean(path)
		} else {
			abs = filepath.Clean(filepath.Join(workspace, path))
		}
		rel, err := filepath.Rel(workspace, abs)
		if err != nil {
			return nil, err
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
			return nil, fmt.Errorf("path escapes workspace: %s", path)
		}
		paths = append(paths, filepath.ToSlash(rel))
	}
	return paths, nil
}

func (e *Executor) timeoutSeconds() int {
	if e.TimeoutSeconds > 0 {
		return e.TimeoutSeconds
	}
	return defaultGitTimeoutSeconds
}

func (e *Executor) maxOutputBytes() int64 {
	if e.MaxOutputBytes > 0 {
		return e.MaxOutputBytes
	}
	return defaultGitOutputBytes
}

func defaultRoot(root string) string {
	if strings.TrimSpace(root) == "" {
		return "."
	}
	return root
}

func stringSlice(raw any) []string {
	switch typed := raw.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func truncateBytes(value string, limit int64) (string, bool) {
	if limit <= 0 || int64(len(value)) <= limit {
		return value, false
	}
	return value[:limit], true
}

func processExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}
