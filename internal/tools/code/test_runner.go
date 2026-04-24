package code

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"local-agent/internal/core"
	"local-agent/internal/security"
	"local-agent/internal/tools"
)

const (
	defaultTestTimeoutSeconds = 300
	defaultMaxTestOutputBytes = int64(200000)
	maxParsedFailureTextBytes = 8000
)

// RunTestsExecutor runs an allowlisted test command inside the workspace.
type RunTestsExecutor struct {
	Workspace             Workspace
	DefaultTimeoutSeconds int
	MaxOutputBytes        int64
}

// ParseTestFailureExecutor parses test output into structured failures.
type ParseTestFailureExecutor struct {
	Workspace Workspace
}

// FixTestFailureLoopExecutor provides a bounded first-step test repair loop.
type FixTestFailureLoopExecutor struct {
	Workspace             Workspace
	DefaultTimeoutSeconds int
	MaxOutputBytes        int64
	MaxIterations         int
}

// Execute implements code.run_tests.
func (e *RunTestsExecutor) Execute(ctx context.Context, input map[string]any) (*core.ToolResult, error) {
	workspacePath, _ := input["workspace"].(string)
	root, relRoot, err := e.Workspace.resolve(workspacePath)
	if err != nil {
		return nil, err
	}
	spec, err := e.testCommand(root, input)
	if err != nil {
		return nil, err
	}
	if err := validateTestCommand(spec.Name, spec.Args); err != nil {
		return nil, err
	}
	if pattern, _ := input["test_name_pattern"].(string); pattern != "" {
		spec.Args = appendTestPattern(spec.Name, spec.Args, pattern)
	}

	timeoutSeconds := tools.GetInt(input, "timeout_seconds", e.defaultTimeout())
	if timeoutSeconds <= 0 {
		timeoutSeconds = e.defaultTimeout()
	}
	maxOutputBytes := getInt64(input, "max_output_bytes", e.defaultMaxOutput())
	if maxOutputBytes <= 0 {
		maxOutputBytes = e.defaultMaxOutput()
	}

	runCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	startedAt := time.Now().UTC()
	cmd := exec.CommandContext(runCtx, spec.Name, spec.Args...)
	cmd.Dir = root
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	finishedAt := time.Now().UTC()
	timedOut := runCtx.Err() == context.DeadlineExceeded
	exit := processExitCode(runErr)
	if timedOut {
		exit = -1
	}

	stdoutText, stdoutTruncated := truncateBytes(security.RedactString(stdout.String()), maxOutputBytes)
	stderrText, stderrTruncated := truncateBytes(security.RedactString(stderr.String()), maxOutputBytes)
	passed := runErr == nil && !timedOut
	output := map[string]any{
		"workspace":        relRoot,
		"command":          spec.CommandString(),
		"args":             append([]string(nil), spec.Args...),
		"exit_code":        exit,
		"passed":           passed,
		"duration_ms":      finishedAt.Sub(startedAt).Milliseconds(),
		"stdout":           stdoutText,
		"stderr":           stderrText,
		"truncated":        stdoutTruncated || stderrTruncated,
		"detected_command": spec.Detected,
		"timed_out":        timedOut,
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

// Execute implements code.parse_test_failure.
func (e *ParseTestFailureExecutor) Execute(_ context.Context, input map[string]any) (*core.ToolResult, error) {
	command, _ := input["command"].(string)
	stdout, _ := input["stdout"].(string)
	stderr, _ := input["stderr"].(string)
	language, _ := input["language"].(string)
	exitCode := tools.GetInt(input, "exit_code", 0)
	if language == "" {
		language = languageFromCommand(command)
	}

	stdout = security.RedactString(stdout)
	stderr = security.RedactString(stderr)
	combined := stdout
	if stderr != "" {
		combined += "\n" + stderr
	}

	parser := "generic"
	confidence := 0.35
	var failures []map[string]any
	switch language {
	case "go":
		failures = parseGoFailures(combined)
		parser = "go"
		confidence = 0.8
	case "python":
		failures = parsePythonFailures(combined)
		parser = "python"
		confidence = 0.7
	case "javascript", "typescript", "node":
		failures = parseNodeFailures(combined)
		parser = "node"
		confidence = 0.65
	case "rust":
		failures = parseRustFailures(combined)
		parser = "rust"
		confidence = 0.7
	}
	if len(failures) == 0 {
		failures = parseGenericFailures(combined)
		if len(failures) > 0 {
			parser = "generic"
			confidence = 0.45
		}
	}
	passed := exitCode == 0
	summary := summarizeFailures(passed, failures, combined)
	if len(failures) == 0 && !passed {
		confidence = 0.25
	}

	return &core.ToolResult{
		Output: map[string]any{
			"passed":      passed,
			"failures":    failures,
			"summary":     summary,
			"language":    language,
			"parser_used": parser,
			"confidence":  confidence,
		},
	}, nil
}

// Execute implements code.fix_test_failure_loop.
func (e *FixTestFailureLoopExecutor) Execute(ctx context.Context, input map[string]any) (*core.ToolResult, error) {
	maxIterations := tools.GetInt(input, "max_iterations", e.defaultMaxIterations())
	if maxIterations <= 0 {
		maxIterations = e.defaultMaxIterations()
	}
	if maxIterations > e.defaultMaxIterations() {
		maxIterations = e.defaultMaxIterations()
	}
	testInput := map[string]any{
		"workspace":         stringValue(input, "workspace"),
		"command":           stringValue(input, "test_command"),
		"use_detected":      stringValue(input, "test_command") == "",
		"timeout_seconds":   e.defaultTimeout(),
		"max_output_bytes":  e.defaultMaxOutput(),
		"test_name_pattern": stringValue(input, "test_name_pattern"),
	}
	runResult, err := (&RunTestsExecutor{
		Workspace:             e.Workspace,
		DefaultTimeoutSeconds: e.defaultTimeout(),
		MaxOutputBytes:        e.defaultMaxOutput(),
	}).Execute(ctx, testInput)
	if err != nil {
		return nil, err
	}
	passed, _ := runResult.Output["passed"].(bool)
	if passed {
		return &core.ToolResult{Output: map[string]any{
			"iterations":         1,
			"final_passed":       true,
			"applied_patches":    []string{},
			"remaining_failures": []map[string]any{},
			"summary":            "tests passed; no repair needed",
		}}, nil
	}
	parseResult, err := (&ParseTestFailureExecutor{Workspace: e.Workspace}).Execute(ctx, map[string]any{
		"workspace": stringValue(input, "workspace"),
		"command":   fmt.Sprint(runResult.Output["command"]),
		"stdout":    fmt.Sprint(runResult.Output["stdout"]),
		"stderr":    fmt.Sprint(runResult.Output["stderr"]),
		"exit_code": runResult.Output["exit_code"],
	})
	if err != nil {
		return nil, err
	}
	failures, _ := parseResult.Output["failures"].([]map[string]any)
	return &core.ToolResult{Output: map[string]any{
		"iterations":         1,
		"final_passed":       false,
		"applied_patches":    []string{},
		"remaining_failures": failures,
		"summary":            "tests failed; generate a code.propose_patch proposal and pause for approval before applying changes",
		"next_tool":          "code.propose_patch",
		"stop_on_approval":   boolValue(input, "stop_on_approval", true),
		"max_iterations":     maxIterations,
		"last_test_result":   runResult.Output,
		"last_parse_result":  parseResult.Output,
	}}, nil
}

type testCommandSpec struct {
	Name     string
	Args     []string
	Detected bool
}

func (s testCommandSpec) CommandString() string {
	if len(s.Args) == 0 {
		return s.Name
	}
	return s.Name + " " + strings.Join(s.Args, " ")
}

func (e *RunTestsExecutor) testCommand(root string, input map[string]any) (testCommandSpec, error) {
	command, _ := input["command"].(string)
	args := stringSlice(input["args"])
	useDetected, _ := input["use_detected"].(bool)
	detected := false
	if strings.TrimSpace(command) == "" && useDetected {
		commands := detectTestCommands(root)
		if len(commands) == 0 {
			return testCommandSpec{}, errors.New("no test command detected")
		}
		command = commands[0]
		detected = true
	}
	if strings.TrimSpace(command) == "" {
		return testCommandSpec{}, errors.New("command is required unless use_detected=true")
	}
	parts, err := splitCommandLine(command)
	if err != nil {
		return testCommandSpec{}, err
	}
	if len(parts) == 0 {
		return testCommandSpec{}, errors.New("command is empty")
	}
	return testCommandSpec{Name: parts[0], Args: append(parts[1:], args...), Detected: detected}, nil
}

func validateTestCommand(name string, args []string) error {
	if strings.ContainsAny(name, "|;&<>") {
		return errors.New("test command must be a structured executable, not shell syntax")
	}
	if len(args) > 0 {
		for _, arg := range args {
			if arg == "&&" || arg == ";" || arg == "|" || strings.Contains(arg, ">") {
				return errors.New("test command arguments cannot contain shell control operators")
			}
		}
	}
	switch name {
	case "go":
		return requireFirstArg(args, "test")
	case "npm", "pnpm", "yarn":
		if len(args) > 0 && args[0] == "test" {
			return nil
		}
		if len(args) > 1 && args[0] == "run" && args[1] == "test" {
			return nil
		}
	case "pytest":
		return nil
	case "python", "python3":
		if len(args) >= 2 && args[0] == "-m" && args[1] == "pytest" {
			return nil
		}
	case "cargo":
		return requireFirstArg(args, "test")
	case "make":
		return requireFirstArg(args, "test")
	}
	return fmt.Errorf("unsupported test command: %s %s", name, strings.Join(args, " "))
}

func requireFirstArg(args []string, want string) error {
	if len(args) == 0 || args[0] != want {
		return fmt.Errorf("expected first argument %q", want)
	}
	return nil
}

func appendTestPattern(name string, args []string, pattern string) []string {
	out := append([]string(nil), args...)
	switch name {
	case "go":
		out = append(out, "-run", pattern)
	case "pytest", "python", "python3":
		out = append(out, "-k", pattern)
	case "cargo":
		out = append(out, "--", pattern)
	}
	return out
}

func languageFromCommand(command string) string {
	parts, _ := splitCommandLine(command)
	if len(parts) == 0 {
		return "unknown"
	}
	switch parts[0] {
	case "go":
		return "go"
	case "pytest", "python", "python3":
		return "python"
	case "npm", "pnpm", "yarn", "node", "npx":
		return "javascript"
	case "cargo":
		return "rust"
	default:
		return "unknown"
	}
}

func parseGoFailures(output string) []map[string]any {
	lines := strings.Split(output, "\n")
	failRe := regexp.MustCompile(`^--- FAIL: ([^\s(]+)`)
	fileRe := regexp.MustCompile(`^\s+([^:\s]+\.go):(\d+):\s*(.*)$`)
	pkgRe := regexp.MustCompile(`^FAIL\s+([^\s]+)`)
	var failures []map[string]any
	var current map[string]any
	for _, line := range lines {
		if match := failRe.FindStringSubmatch(line); match != nil {
			current = map[string]any{"test_name": match[1], "message": strings.TrimSpace(line)}
			failures = append(failures, current)
			continue
		}
		if current != nil {
			if match := fileRe.FindStringSubmatch(line); match != nil {
				current["file"] = match[1]
				current["line"] = atoi(match[2])
				if strings.TrimSpace(match[3]) != "" {
					current["message"] = strings.TrimSpace(match[3])
				}
				current["stack_trace"] = truncateForParse(output)
				continue
			}
			if match := pkgRe.FindStringSubmatch(line); match != nil {
				current["package"] = match[1]
			}
		}
	}
	return failures
}

func parsePythonFailures(output string) []map[string]any {
	fileRe := regexp.MustCompile(`(?m)([A-Za-z0-9_./\\-]+\.py):(\d+):?\s*(.*)`)
	testRe := regexp.MustCompile(`(?m)^_+ ([^_\n]+) _+$`)
	testName := ""
	if match := testRe.FindStringSubmatch(output); match != nil {
		testName = strings.TrimSpace(match[1])
	}
	var failures []map[string]any
	for _, match := range fileRe.FindAllStringSubmatch(output, 8) {
		failures = append(failures, map[string]any{
			"file":        filepathSlash(match[1]),
			"line":        atoi(match[2]),
			"test_name":   testName,
			"message":     firstNonEmpty(strings.TrimSpace(match[3]), firstErrorLine(output)),
			"stack_trace": truncateForParse(output),
		})
	}
	return failures
}

func parseNodeFailures(output string) []map[string]any {
	failRe := regexp.MustCompile(`(?m)^FAIL\s+(.+\.(?:test|spec)\.[jt]sx?)`)
	testRe := regexp.MustCompile(`(?m)^\s*(?:-|\*)?\s*(?:[xX]|FAIL|Error)\s+(.+)$`)
	locRe := regexp.MustCompile(`(?m)([A-Za-z0-9_./\\-]+\.[jt]sx?):(\d+):(\d+)`)
	testName := ""
	if match := testRe.FindStringSubmatch(output); match != nil {
		testName = strings.TrimSpace(match[1])
	}
	var failures []map[string]any
	for _, match := range failRe.FindAllStringSubmatch(output, 8) {
		item := map[string]any{
			"file":        filepathSlash(strings.TrimSpace(match[1])),
			"test_name":   testName,
			"message":     firstErrorLine(output),
			"stack_trace": truncateForParse(output),
		}
		if loc := locRe.FindStringSubmatch(output); loc != nil {
			item["file"] = filepathSlash(loc[1])
			item["line"] = atoi(loc[2])
		}
		failures = append(failures, item)
	}
	if len(failures) == 0 {
		if loc := locRe.FindStringSubmatch(output); loc != nil {
			failures = append(failures, map[string]any{
				"file":        filepathSlash(loc[1]),
				"line":        atoi(loc[2]),
				"test_name":   testName,
				"message":     firstErrorLine(output),
				"stack_trace": truncateForParse(output),
			})
		}
	}
	return failures
}

func parseRustFailures(output string) []map[string]any {
	threadRe := regexp.MustCompile(`(?m)^thread '([^']+)' panicked at ([^:\n]+):(\d+):(\d+):?\s*(.*)$`)
	var failures []map[string]any
	for _, match := range threadRe.FindAllStringSubmatch(output, 8) {
		failures = append(failures, map[string]any{
			"file":        filepathSlash(match[2]),
			"line":        atoi(match[3]),
			"test_name":   match[1],
			"message":     firstNonEmpty(strings.TrimSpace(match[5]), "rust test panicked"),
			"stack_trace": truncateForParse(output),
		})
	}
	return failures
}

func parseGenericFailures(output string) []map[string]any {
	message := firstErrorLine(output)
	if message == "" {
		return nil
	}
	return []map[string]any{{
		"message":     message,
		"stack_trace": truncateForParse(output),
		"hint":        "inspect the referenced files and rerun a narrower test when possible",
	}}
}

func summarizeFailures(passed bool, failures []map[string]any, output string) string {
	if passed {
		return "tests passed"
	}
	if len(failures) == 0 {
		return firstNonEmpty(firstErrorLine(output), "tests failed but no structured failure could be parsed")
	}
	first := failures[0]
	if file, ok := first["file"].(string); ok && file != "" {
		if line, ok := first["line"].(int); ok && line > 0 {
			return fmt.Sprintf("%d failure(s), first at %s:%d", len(failures), file, line)
		}
		return fmt.Sprintf("%d failure(s), first in %s", len(failures), file)
	}
	return fmt.Sprintf("%d failure(s): %s", len(failures), fmt.Sprint(first["message"]))
}

func splitCommandLine(input string) ([]string, error) {
	var (
		parts   []string
		current strings.Builder
		inQuote rune
		escaped bool
	)
	for _, r := range input {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if inQuote != 0 {
			if r == inQuote {
				inQuote = 0
			} else {
				current.WriteRune(r)
			}
			continue
		}
		switch r {
		case '\'', '"':
			inQuote = r
		case ' ', '\t', '\n':
			if current.Len() > 0 {
				parts = append(parts, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}
	if escaped {
		current.WriteRune('\\')
	}
	if inQuote != 0 {
		return nil, errors.New("unterminated quote in command")
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	return parts, nil
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

func truncateBytes(value string, limit int64) (string, bool) {
	if limit <= 0 || int64(len(value)) <= limit {
		return value, false
	}
	return value[:limit], true
}

func truncateForParse(value string) string {
	if len(value) <= maxParsedFailureTextBytes {
		return value
	}
	return value[:maxParsedFailureTextBytes]
}

func firstErrorLine(output string) string {
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		if strings.Contains(lower, "error") || strings.Contains(lower, "fail") || strings.HasPrefix(trimmed, "E ") || strings.HasPrefix(trimmed, "AssertionError") {
			return trimmed
		}
	}
	for _, line := range strings.Split(output, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
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

func getInt64(input map[string]any, key string, fallback int64) int64 {
	raw, ok := input[key]
	if !ok {
		return fallback
	}
	switch value := raw.(type) {
	case int:
		return int64(value)
	case int64:
		return value
	case float64:
		return int64(value)
	default:
		return fallback
	}
}

func boolValue(input map[string]any, key string, fallback bool) bool {
	raw, ok := input[key]
	if !ok {
		return fallback
	}
	value, ok := raw.(bool)
	if !ok {
		return fallback
	}
	return value
}

func stringValue(input map[string]any, key string) string {
	value, _ := input[key].(string)
	return value
}

func atoi(value string) int {
	n, _ := strconv.Atoi(value)
	return n
}

func filepathSlash(value string) string {
	return strings.ReplaceAll(value, "\\", "/")
}

func (e *RunTestsExecutor) defaultTimeout() int {
	if e.DefaultTimeoutSeconds > 0 {
		return e.DefaultTimeoutSeconds
	}
	return defaultTestTimeoutSeconds
}

func (e *RunTestsExecutor) defaultMaxOutput() int64 {
	if e.MaxOutputBytes > 0 {
		return e.MaxOutputBytes
	}
	return defaultMaxTestOutputBytes
}

func (e *FixTestFailureLoopExecutor) defaultTimeout() int {
	if e.DefaultTimeoutSeconds > 0 {
		return e.DefaultTimeoutSeconds
	}
	return defaultTestTimeoutSeconds
}

func (e *FixTestFailureLoopExecutor) defaultMaxOutput() int64 {
	if e.MaxOutputBytes > 0 {
		return e.MaxOutputBytes
	}
	return defaultMaxTestOutputBytes
}

func (e *FixTestFailureLoopExecutor) defaultMaxIterations() int {
	if e.MaxIterations > 0 {
		return e.MaxIterations
	}
	return 3
}
