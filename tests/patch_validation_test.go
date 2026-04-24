package tests

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"local-agent/internal/tools/code"
)

func TestPatchValidationFullReplacementHashAndSensitive(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("TOKEN=value\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	workspace := code.Workspace{Root: root, SensitivePaths: []string{".env"}}

	result, err := (&code.ValidatePatchExecutor{Workspace: workspace}).Execute(context.Background(), map[string]any{
		"path":            "main.go",
		"content":         "package main\n\nconst OK = true\n",
		"expected_sha256": "wrong",
	})
	if err != nil {
		t.Fatalf("ValidatePatchExecutor.Execute() error = %v", err)
	}
	if result.Output["valid"] != false {
		t.Fatalf("valid = %v, want false", result.Output["valid"])
	}

	sensitive, err := (&code.ValidatePatchExecutor{Workspace: workspace}).Execute(context.Background(), map[string]any{
		"path":    ".env",
		"content": "TOKEN=changed\n",
	})
	if err != nil {
		t.Fatalf("ValidatePatchExecutor sensitive error = %v", err)
	}
	files := sensitive.Output["sensitive_files"].([]string)
	if !containsString(files, ".env") {
		t.Fatalf("sensitive_files = %v, want .env", files)
	}
}

func TestPatchValidationUnifiedDiffConflictAndDryRun(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nconst Old = true\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	workspace := code.Workspace{Root: root}
	diff := "--- a/main.go\n+++ b/main.go\n@@ -1,3 +1,3 @@\n package main\n \n-const Old = true\n+const New = true\n"
	result, err := (&code.DryRunPatchExecutor{Workspace: workspace}).Execute(context.Background(), map[string]any{
		"diff": diff,
	})
	if err != nil {
		t.Fatalf("DryRunPatchExecutor.Execute() error = %v", err)
	}
	if result.Output["valid"] != true {
		t.Fatalf("valid = %v, conflicts=%v", result.Output["valid"], result.Output["conflicts"])
	}

	conflictDiff := "--- a/main.go\n+++ b/main.go\n@@ -1,2 +1,2 @@\n package main\n-const Missing = true\n+const New = true\n"
	conflict, err := (&code.ValidatePatchExecutor{Workspace: workspace}).Execute(context.Background(), map[string]any{
		"diff": conflictDiff,
	})
	if err != nil {
		t.Fatalf("ValidatePatchExecutor conflict error = %v", err)
	}
	if conflict.Output["valid"] != false {
		t.Fatalf("valid = %v, want false", conflict.Output["valid"])
	}
}
