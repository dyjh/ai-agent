package tests

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"local-agent/internal/tools/code"
)

func TestCodeRunTestsDetectedGoPasses(t *testing.T) {
	root := newGoTestProject(t, "func TestOK(t *testing.T) {}\n")
	result, err := (&code.RunTestsExecutor{Workspace: code.Workspace{Root: root}, DefaultTimeoutSeconds: 10, MaxOutputBytes: 20000}).Execute(context.Background(), map[string]any{
		"use_detected": true,
	})
	if err != nil {
		t.Fatalf("RunTestsExecutor.Execute() error = %v", err)
	}
	if result.Output["passed"] != true {
		t.Fatalf("passed = %v, stdout=%v stderr=%v", result.Output["passed"], result.Output["stdout"], result.Output["stderr"])
	}
	if result.Output["detected_command"] != true {
		t.Fatalf("detected_command = %v, want true", result.Output["detected_command"])
	}
}

func TestCodeRunTestsFailureTruncatesAndRedacts(t *testing.T) {
	root := newGoTestProject(t, `func TestFail(t *testing.T) {
	t.Log("token=abc123")
	t.Log("`+strings.Repeat("A", 600)+`")
	t.Fatalf("boom")
}
`)
	result, err := (&code.RunTestsExecutor{Workspace: code.Workspace{Root: root}, DefaultTimeoutSeconds: 10, MaxOutputBytes: 180}).Execute(context.Background(), map[string]any{
		"command": "go test ./...",
	})
	if err != nil {
		t.Fatalf("RunTestsExecutor.Execute() error = %v", err)
	}
	if result.Output["passed"] != false {
		t.Fatalf("passed = %v, want false", result.Output["passed"])
	}
	if result.Output["truncated"] != true {
		t.Fatalf("truncated = %v, want true", result.Output["truncated"])
	}
	combined := result.Output["stdout"].(string) + result.Output["stderr"].(string)
	if strings.Contains(combined, "abc123") {
		t.Fatalf("secret-like token was not redacted: %q", combined)
	}
}

func TestCodeRunTestsTimeoutAndNoDetectedCommand(t *testing.T) {
	root := newGoTestProject(t, `func TestSlow(t *testing.T) {
	time.Sleep(2 * time.Second)
}
`)
	result, err := (&code.RunTestsExecutor{Workspace: code.Workspace{Root: root}, DefaultTimeoutSeconds: 1, MaxOutputBytes: 20000}).Execute(context.Background(), map[string]any{
		"command":         "go test ./...",
		"timeout_seconds": 1,
	})
	if err != nil {
		t.Fatalf("RunTestsExecutor.Execute() timeout should be structured, got error = %v", err)
	}
	if result.Output["timed_out"] != true {
		t.Fatalf("timed_out = %v, want true", result.Output["timed_out"])
	}

	empty := t.TempDir()
	if _, err := (&code.RunTestsExecutor{Workspace: code.Workspace{Root: empty}}).Execute(context.Background(), map[string]any{
		"use_detected": true,
	}); err == nil {
		t.Fatalf("expected no detected command error")
	}
}

func newGoTestProject(t *testing.T, testBody string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test\n\ngo 1.23\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	imports := "import \"testing\"\n\n"
	if strings.Contains(testBody, "time.") {
		imports = "import (\n\t\"testing\"\n\t\"time\"\n)\n\n"
	}
	body := "package example\n\n" + imports + testBody
	if err := os.WriteFile(filepath.Join(root, "main_test.go"), []byte(body), 0o644); err != nil {
		t.Fatalf("write main_test.go: %v", err)
	}
	return root
}
