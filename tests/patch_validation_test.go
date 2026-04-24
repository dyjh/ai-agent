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

func TestProposePatchAcceptsUnifiedDiffPreview(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nconst Old = true\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	diff := "--- a/main.go\n+++ b/main.go\n@@ -1,3 +1,3 @@\n package main\n \n-const Old = true\n+const New = true\n"
	result, err := (&code.ProposePatchExecutor{Workspace: code.Workspace{Root: root}}).Execute(context.Background(), map[string]any{
		"diff": diff,
	})
	if err != nil {
		t.Fatalf("ProposePatchExecutor.Execute(diff) error = %v", err)
	}
	if result.Output["valid"] != true {
		t.Fatalf("valid = %v, conflicts=%v", result.Output["valid"], result.Output["conflicts"])
	}
	if result.Output["requires_approval_before_apply"] != true {
		t.Fatalf("requires_approval_before_apply = %v, want true", result.Output["requires_approval_before_apply"])
	}
	files := result.Output["files"].([]map[string]any)
	if files[0]["old_sha256"] == "" || files[0]["new_sha256"] == "" {
		t.Fatalf("file hashes missing: %#v", files[0])
	}
	if parsed, ok := result.Output["parsed_diff"].(code.UnifiedDiff); !ok || len(parsed.Files) != 1 {
		t.Fatalf("parsed_diff = %#v, want one file", result.Output["parsed_diff"])
	}
}

func TestUnifiedDiffParserHashEscapeAndRollbackMetadata(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte("package main\n\nconst Old = true\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	workspace := code.Workspace{Root: root}
	diff := "--- a/main.go\n+++ b/main.go\n@@ -1,3 +1,3 @@\n package main\n \n-const Old = true\n+const New = true\n"

	parsed, err := code.ParseUnifiedDiff(diff)
	if err != nil {
		t.Fatalf("ParseUnifiedDiff() error = %v", err)
	}
	if len(parsed.Files) != 1 || parsed.Files[0].Hunks[0].OldStart != 1 {
		t.Fatalf("parsed diff = %#v", parsed)
	}

	currentHash := fileSHA256(t, path)
	dryRun, err := (&code.DryRunPatchExecutor{Workspace: workspace}).Execute(context.Background(), map[string]any{
		"diff":            diff,
		"expected_sha256": currentHash,
	})
	if err != nil {
		t.Fatalf("DryRunPatchExecutor.Execute() error = %v", err)
	}
	if dryRun.Output["valid"] != true {
		t.Fatalf("valid = %v, conflicts=%v", dryRun.Output["valid"], dryRun.Output["conflicts"])
	}
	stats := dryRun.Output["statistics"].(map[string]any)
	if stats["additions"] != 1 || stats["deletions"] != 1 {
		t.Fatalf("statistics = %#v, want +1/-1", stats)
	}
	files := dryRun.Output["files"].([]map[string]any)
	if _, ok := files[0]["rollback_snapshot"].(map[string]any); !ok {
		t.Fatalf("rollback_snapshot = %#v", files[0]["rollback_snapshot"])
	}

	hashMismatch, err := (&code.ValidatePatchExecutor{Workspace: workspace}).Execute(context.Background(), map[string]any{
		"diff":            diff,
		"expected_sha256": "wrong",
	})
	if err != nil {
		t.Fatalf("ValidatePatchExecutor hash mismatch error = %v", err)
	}
	if hashMismatch.Output["valid"] != false {
		t.Fatalf("valid = %v, want false for hash mismatch", hashMismatch.Output["valid"])
	}

	escapeDiff := "--- a/main.go\n+++ b/../outside.go\n@@ -1,1 +1,1 @@\n-package main\n+package outside\n"
	if _, err := (&code.ValidatePatchExecutor{Workspace: workspace}).Execute(context.Background(), map[string]any{
		"diff": escapeDiff,
	}); err == nil {
		t.Fatalf("expected path escape to fail")
	}

	applied, err := (&code.ApplyPatchExecutor{Workspace: workspace}).Execute(context.Background(), map[string]any{
		"diff":            diff,
		"expected_sha256": currentHash,
	})
	if err != nil {
		t.Fatalf("ApplyPatchExecutor.Execute() error = %v", err)
	}
	if _, ok := applied.Output["rollback_snapshot"].(map[string]any); !ok {
		t.Fatalf("apply rollback_snapshot = %#v", applied.Output["rollback_snapshot"])
	}
}

func fileSHA256(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	result, err := (&code.ProposePatchExecutor{Workspace: code.Workspace{Root: filepath.Dir(path)}}).Execute(context.Background(), map[string]any{
		"path":    filepath.Base(path),
		"content": string(data),
	})
	if err != nil {
		t.Fatalf("hash helper propose patch: %v", err)
	}
	files := result.Output["files"].([]map[string]any)
	return files[0]["old_sha256"].(string)
}
