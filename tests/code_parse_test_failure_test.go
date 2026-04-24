package tests

import (
	"context"
	"testing"

	"local-agent/internal/core"
	"local-agent/internal/tools/code"
)

func TestCodeParseTestFailureGo(t *testing.T) {
	output := "--- FAIL: TestAdd (0.00s)\n    math_test.go:12: got 1 want 2\nFAIL\texample.test\t0.01s\n"
	result := parseFailure(t, map[string]any{"command": "go test ./...", "stdout": output, "exit_code": 1})
	failures := result.Output["failures"].([]map[string]any)
	if len(failures) != 1 {
		t.Fatalf("failures = %v, want one", failures)
	}
	if failures[0]["file"] != "math_test.go" || failures[0]["line"] != 12 || failures[0]["test_name"] != "TestAdd" {
		t.Fatalf("unexpected failure: %v", failures[0])
	}
}

func TestCodeParseTestFailurePythonNodeRustAndGeneric(t *testing.T) {
	cases := []struct {
		name    string
		input   map[string]any
		wantKey string
	}{
		{
			name:    "python",
			input:   map[string]any{"command": "pytest", "stdout": "____ test_add ____\nE AssertionError: boom\ntests/test_math.py:9: AssertionError\n", "exit_code": 1},
			wantKey: "tests/test_math.py",
		},
		{
			name:    "node",
			input:   map[string]any{"command": "npm test", "stdout": "FAIL src/foo.test.ts\n  ● adds\nsrc/foo.test.ts:4:10\nExpected true\n", "exit_code": 1},
			wantKey: "src/foo.test.ts",
		},
		{
			name:    "rust",
			input:   map[string]any{"command": "cargo test", "stderr": "thread 'tests::adds' panicked at src/lib.rs:8:5:\nboom\n", "exit_code": 1},
			wantKey: "src/lib.rs",
		},
		{
			name:    "generic",
			input:   map[string]any{"command": "make test", "stdout": "FAILED unknown thing\n", "exit_code": 1},
			wantKey: "FAILED unknown thing",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := parseFailure(t, tc.input)
			failures := result.Output["failures"].([]map[string]any)
			if len(failures) == 0 {
				t.Fatalf("expected at least one failure")
			}
			if result.Output["summary"] == "" {
				t.Fatalf("expected summary")
			}
			if !containsFailureValue(failures[0], tc.wantKey) {
				t.Fatalf("failure %v does not contain %q", failures[0], tc.wantKey)
			}
		})
	}
}

func parseFailure(t *testing.T, input map[string]any) *core.ToolResult {
	t.Helper()
	result, err := (&code.ParseTestFailureExecutor{}).Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("ParseTestFailureExecutor.Execute() error = %v", err)
	}
	return result
}

func containsFailureValue(failure map[string]any, want string) bool {
	for _, value := range failure {
		if value == want {
			return true
		}
	}
	return false
}
