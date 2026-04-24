package gittools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	switch e.Operation {
	case "diff_summary":
		staged, _ := input["staged"].(bool)
		return e.executeDiffSummary(ctx, workspace, paths, staged)
	case "commit_message_proposal":
		return e.executeCommitMessageProposal(ctx, workspace, paths)
	case "commit":
		return e.executeCommit(ctx, workspace, paths, input)
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
	case "diff_summary":
		return appendPathspec([]string{"diff", "--stat"}, paths), nil
	case "commit_message_proposal":
		return []string{"status", "--short"}, nil
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

func (e *Executor) executeDiffSummary(ctx context.Context, workspace string, paths []string, staged bool) (*core.ToolResult, error) {
	startedAt := time.Now().UTC()
	base := []string{"diff"}
	if staged {
		base = append(base, "--cached")
	}
	stat, err := e.runGit(ctx, workspace, appendPathspec(append([]string(nil), append(base, "--stat")...), paths))
	if err != nil {
		return nil, err
	}
	numstat, err := e.runGit(ctx, workspace, appendPathspec(append([]string(nil), append(base, "--numstat")...), paths))
	if err != nil {
		return nil, err
	}
	nameStatus, err := e.runGit(ctx, workspace, appendPathspec(append([]string(nil), append(base, "--name-status")...), paths))
	if err != nil {
		return nil, err
	}
	files, additions, deletions := parseNumstat(numstat.Stdout)
	output := map[string]any{
		"workspace":     filepath.ToSlash(workspace),
		"operation":     e.Operation,
		"staged":        staged,
		"paths":         paths,
		"file_count":    len(files),
		"changed_files": files,
		"additions":     additions,
		"deletions":     deletions,
		"stat":          stat.Stdout,
		"name_status":   parseNameStatus(nameStatus.Stdout),
		"summary":       fmt.Sprintf("%d file(s) changed, +%d/-%d", len(files), additions, deletions),
		"truncated":     stat.Truncated || numstat.Truncated || nameStatus.Truncated,
		"duration_ms":   time.Since(startedAt).Milliseconds(),
		"read_only":     true,
		"commit_scoped": staged,
	}
	return &core.ToolResult{Output: output, StartedAt: startedAt, FinishedAt: time.Now().UTC()}, nil
}

func (e *Executor) executeCommitMessageProposal(ctx context.Context, workspace string, paths []string) (*core.ToolResult, error) {
	startedAt := time.Now().UTC()
	status, err := e.runGit(ctx, workspace, []string{"status", "--short"})
	if err != nil {
		return nil, err
	}
	staged, err := e.runGit(ctx, workspace, appendPathspec([]string{"diff", "--cached", "--numstat"}, paths))
	if err != nil {
		return nil, err
	}
	files, additions, deletions := parseNumstat(staged.Stdout)
	message := proposeCommitMessage(files, additions, deletions)
	output := map[string]any{
		"workspace":         filepath.ToSlash(workspace),
		"operation":         e.Operation,
		"paths":             paths,
		"status":            status.Stdout,
		"staged_file_count": len(files),
		"staged_files":      files,
		"additions":         additions,
		"deletions":         deletions,
		"message":           message,
		"summary":           "commit message proposal only; no commit was created",
		"pre_commit_checks": map[string]any{
			"git_status_checked":      true,
			"staged_diff_checked":     true,
			"commit_message_nonempty": strings.TrimSpace(message) != "",
		},
		"warnings":    commitProposalWarnings(files),
		"read_only":   true,
		"truncated":   status.Truncated || staged.Truncated,
		"duration_ms": time.Since(startedAt).Milliseconds(),
	}
	return &core.ToolResult{Output: output, StartedAt: startedAt, FinishedAt: time.Now().UTC()}, nil
}

func (e *Executor) executeCommit(ctx context.Context, workspace string, paths []string, input map[string]any) (*core.ToolResult, error) {
	message, _ := input["message"].(string)
	message = strings.TrimSpace(message)
	if message == "" {
		return nil, errors.New("git.commit requires message")
	}
	startedAt := time.Now().UTC()
	status, err := e.runGit(ctx, workspace, []string{"status", "--short"})
	if err != nil {
		return nil, err
	}
	staged, err := e.runGit(ctx, workspace, appendPathspec([]string{"diff", "--cached", "--name-only"}, paths))
	if err != nil {
		return nil, err
	}
	stagedFiles := nonEmptyLines(staged.Stdout)
	outsideScopedFiles := []string{}
	if len(paths) > 0 {
		allStaged, err := e.runGit(ctx, workspace, []string{"diff", "--cached", "--name-only"})
		if err != nil {
			return nil, err
		}
		outsideScopedFiles = outsideFiles(nonEmptyLines(allStaged.Stdout), stagedFiles)
		if len(outsideScopedFiles) > 0 {
			return &core.ToolResult{
				Output: map[string]any{
					"workspace":            filepath.ToSlash(workspace),
					"operation":            e.Operation,
					"command":              "git commit -m " + shellQuoteForDisplay(message),
					"paths":                paths,
					"message":              security.RedactString(message),
					"status":               status.Stdout,
					"staged_files":         stagedFiles,
					"outside_scoped_files": outsideScopedFiles,
					"pre_commit_checks": map[string]any{
						"git_status_checked":        true,
						"staged_diff_checked":       true,
						"commit_message_nonempty":   true,
						"has_staged_changes":        len(stagedFiles) > 0,
						"staged_changes_pathscoped": false,
					},
					"exit_code":   1,
					"error":       "git.commit paths do not cover all staged changes",
					"truncated":   status.Truncated || staged.Truncated || allStaged.Truncated,
					"duration_ms": time.Since(startedAt).Milliseconds(),
				},
				StartedAt:  startedAt,
				FinishedAt: time.Now().UTC(),
			}, nil
		}
	}
	if len(stagedFiles) == 0 {
		return &core.ToolResult{
			Output: map[string]any{
				"workspace": filepath.ToSlash(workspace),
				"operation": e.Operation,
				"command":   "git commit -m " + shellQuoteForDisplay(message),
				"paths":     paths,
				"message":   security.RedactString(message),
				"status":    status.Stdout,
				"pre_commit_checks": map[string]any{
					"git_status_checked":        true,
					"staged_diff_checked":       true,
					"commit_message_nonempty":   true,
					"has_staged_changes":        false,
					"staged_changes_pathscoped": len(paths) == 0,
				},
				"exit_code":   1,
				"error":       "git.commit requires staged changes",
				"truncated":   status.Truncated || staged.Truncated,
				"duration_ms": time.Since(startedAt).Milliseconds(),
			},
			StartedAt:  startedAt,
			FinishedAt: time.Now().UTC(),
		}, nil
	}

	args := []string{"commit", "-m", message}
	timeout := tools.GetInt(input, "timeout_seconds", e.timeoutSeconds())
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

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
		"workspace":    filepath.ToSlash(workspace),
		"operation":    e.Operation,
		"command":      "git " + strings.Join(args, " "),
		"args":         args,
		"paths":        paths,
		"message":      security.RedactString(message),
		"status":       status.Stdout,
		"staged_files": stagedFiles,
		"stdout":       stdoutText,
		"stderr":       stderrText,
		"exit_code":    exitCode,
		"truncated":    status.Truncated || staged.Truncated || stdoutTruncated || stderrTruncated,
		"duration_ms":  finishedAt.Sub(startedAt).Milliseconds(),
		"pre_commit_checks": map[string]any{
			"git_status_checked":        true,
			"staged_diff_checked":       true,
			"commit_message_nonempty":   true,
			"has_staged_changes":        true,
			"staged_changes_pathscoped": true,
		},
	}
	if runErr != nil {
		output["error"] = security.RedactString(runErr.Error())
	}
	return &core.ToolResult{Output: output, StartedAt: startedAt, FinishedAt: finishedAt}, nil
}

type gitCapture struct {
	Stdout    string
	Stderr    string
	ExitCode  int
	Truncated bool
}

func (e *Executor) runGit(ctx context.Context, workspace string, args []string) (gitCapture, error) {
	timeout := e.timeoutSeconds()
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "git", args...)
	cmd.Dir = workspace
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	stdoutText, stdoutTruncated := truncateBytes(security.RedactString(stdout.String()), e.maxOutputBytes())
	stderrText, stderrTruncated := truncateBytes(security.RedactString(stderr.String()), e.maxOutputBytes())
	exitCode := processExitCode(runErr)
	if runCtx.Err() == context.DeadlineExceeded {
		exitCode = -1
	}
	capture := gitCapture{
		Stdout:    stdoutText,
		Stderr:    stderrText,
		ExitCode:  exitCode,
		Truncated: stdoutTruncated || stderrTruncated,
	}
	if runErr != nil {
		return capture, fmt.Errorf("git %s failed: %s", strings.Join(args, " "), firstNonEmpty(stderrText, runErr.Error()))
	}
	return capture, nil
}

func parseNumstat(output string) ([]string, int, int) {
	var files []string
	additions := 0
	deletions := 0
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		if fields[0] != "-" {
			if n, err := strconv.Atoi(fields[0]); err == nil {
				additions += n
			}
		}
		if fields[1] != "-" {
			if n, err := strconv.Atoi(fields[1]); err == nil {
				deletions += n
			}
		}
		files = append(files, filepath.ToSlash(strings.Join(fields[2:], " ")))
	}
	return files, additions, deletions
}

func parseNameStatus(output string) []map[string]any {
	var items []map[string]any
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		items = append(items, map[string]any{
			"status": fields[0],
			"path":   filepath.ToSlash(strings.Join(fields[1:], " ")),
		})
	}
	return items
}

func proposeCommitMessage(files []string, additions, deletions int) string {
	if len(files) == 0 {
		return "chore: no staged changes"
	}
	verb := "update"
	if additions > 0 && deletions == 0 {
		verb = "add"
	}
	if deletions > additions && additions == 0 {
		verb = "remove"
	}
	scope := "workspace"
	if len(files) == 1 {
		scope = filepath.Base(files[0])
	}
	return fmt.Sprintf("chore: %s %s", verb, scope)
}

func commitProposalWarnings(files []string) []string {
	if len(files) == 0 {
		return []string{"no staged diff detected; stage files with git.add before git.commit"}
	}
	return nil
}

func nonEmptyLines(value string) []string {
	var lines []string
	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, filepath.ToSlash(line))
		}
	}
	return lines
}

func outsideFiles(allFiles, scopedFiles []string) []string {
	scoped := map[string]bool{}
	for _, file := range scopedFiles {
		scoped[file] = true
	}
	var outside []string
	for _, file := range allFiles {
		if !scoped[file] {
			outside = append(outside, file)
		}
	}
	return outside
}

func shellQuoteForDisplay(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
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
