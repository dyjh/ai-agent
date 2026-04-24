package tests

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"local-agent/internal/agent"
	"local-agent/internal/config"
	"local-agent/internal/core"
	toolscore "local-agent/internal/tools"
	"local-agent/internal/tools/code"
)

func TestCodeWorkspaceReadSearchAndSensitiveSkip(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc TargetName() {}\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("TOKEN=secret\nTargetName\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	workspace := code.Workspace{Root: root, SensitivePaths: []string{".env"}}
	readResult, err := (&code.ReadExecutor{Workspace: workspace}).Execute(context.Background(), map[string]any{
		"path": "main.go",
	})
	if err != nil {
		t.Fatalf("ReadExecutor.Execute() error = %v", err)
	}
	if readResult.Output["path"] != "main.go" {
		t.Fatalf("path = %v, want main.go", readResult.Output["path"])
	}

	searchResult, err := (&code.SearchExecutor{Workspace: workspace}).Execute(context.Background(), map[string]any{
		"path":  ".",
		"query": "TargetName",
	})
	if err != nil {
		t.Fatalf("SearchExecutor.Execute() error = %v", err)
	}
	matches := searchResult.Output["matches"].([]map[string]any)
	if len(matches) != 1 {
		t.Fatalf("matches = %v, want one non-sensitive match", matches)
	}
	if matches[0]["path"] != "main.go" {
		t.Fatalf("match path = %v, want main.go", matches[0]["path"])
	}
	if skipped := searchResult.Output["skipped_sensitive"]; skipped != 1 {
		t.Fatalf("skipped_sensitive = %v, want 1", skipped)
	}

	if _, err := (&code.ReadExecutor{Workspace: workspace}).Execute(context.Background(), map[string]any{
		"path": "../outside.go",
	}); err == nil {
		t.Fatalf("expected path escape to fail")
	}
}

func TestCodePatchProposalApplyAndHashGuard(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "main.go")
	original := []byte("package main\n\nfunc main() {}\n")
	if err := os.WriteFile(target, original, 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	workspace := code.Workspace{Root: root}

	proposal, err := (&code.ProposePatchExecutor{Workspace: workspace}).Execute(context.Background(), map[string]any{
		"path":    "main.go",
		"content": "package main\n\nfunc main() { println(\"ok\") }\n",
	})
	if err != nil {
		t.Fatalf("ProposePatchExecutor.Execute() error = %v", err)
	}
	if proposal.Output["diff"] == "" {
		t.Fatalf("expected diff preview")
	}

	if _, err := (&code.ApplyPatchExecutor{Workspace: workspace}).Execute(context.Background(), map[string]any{
		"path":            "main.go",
		"content":         "package main\n",
		"expected_sha256": "wrong",
	}); err == nil {
		t.Fatalf("expected hash mismatch to fail")
	}

	_, err = (&code.ApplyPatchExecutor{Workspace: workspace}).Execute(context.Background(), map[string]any{
		"path":            "main.go",
		"content":         "package main\n\nfunc main() { println(\"ok\") }\n",
		"expected_sha256": testSHA256(original),
	})
	if err != nil {
		t.Fatalf("ApplyPatchExecutor.Execute() error = %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(data) != "package main\n\nfunc main() { println(\"ok\") }\n" {
		t.Fatalf("unexpected file content: %q", string(data))
	}
}

func TestCodeListAndProjectInspection(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test\n\ngo 1.23\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	workspace := code.Workspace{Root: root}

	listResult, err := (&code.ListFilesExecutor{Workspace: workspace}).Execute(context.Background(), map[string]any{
		"path": ".",
	})
	if err != nil {
		t.Fatalf("ListFilesExecutor.Execute() error = %v", err)
	}
	entries := listResult.Output["entries"].([]map[string]any)
	if len(entries) != 2 {
		t.Fatalf("entries = %v, want go.mod and main.go", entries)
	}

	inspectResult, err := (&code.InspectProjectExecutor{Workspace: workspace}).Execute(context.Background(), map[string]any{
		"path": ".",
	})
	if err != nil {
		t.Fatalf("InspectProjectExecutor.Execute() error = %v", err)
	}
	if inspectResult.Output["language"] != "go" {
		t.Fatalf("language = %v, want go", inspectResult.Output["language"])
	}
	commands := inspectResult.Output["test_commands"].([]string)
	if !containsString(commands, "go test ./...") {
		t.Fatalf("test_commands = %v, want go test ./...", commands)
	}
}

func TestCodeApplyPatchRequiresApprovalAndUsesSnapshot(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "main.go")
	if err := os.WriteFile(target, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}

	registry := toolscore.NewRegistry()
	registry.Register(core.ToolSpec{
		ID:             "code.apply_patch",
		Name:           "code.apply_patch",
		Description:    "apply patch",
		DefaultEffects: []string{"fs.write", "code.modify"},
	}, &code.ApplyPatchExecutor{Workspace: code.Workspace{Root: root}})

	approvals := agent.NewApprovalCenter()
	router := toolscore.NewRouter(
		registry,
		agent.NewEffectInferrer(config.PolicyConfig{MinConfidenceForAutoExecute: 0.85}),
		agent.NewPolicyEngine(config.PolicyConfig{MinConfidenceForAutoExecute: 0.85}),
		approvals,
		nil,
	)

	outcome, err := router.Propose(context.Background(), "run_code", "conv_code", core.ToolProposal{
		ID:   "tool_code",
		Tool: "code.apply_patch",
		Input: map[string]any{
			"path":    "main.go",
			"content": "package main\n\nconst Snapshot = true\n",
		},
	})
	if err != nil {
		t.Fatalf("Propose() error = %v", err)
	}
	if outcome.Approval == nil {
		t.Fatalf("expected approval for code.apply_patch")
	}
	if _, err := approvals.Approve(outcome.Approval.ID); err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	outcome.Approval.InputSnapshot["content"] = "package main\n\nconst Mutated = true\n"
	if _, err := router.ExecuteApproved(context.Background(), outcome.Approval.ID); err != nil {
		t.Fatalf("ExecuteApproved() error = %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(data) != "package main\n\nconst Snapshot = true\n" {
		t.Fatalf("approved snapshot was not used, got %q", string(data))
	}
}

func TestCodeSensitiveReadRequiresApproval(t *testing.T) {
	inferrer := agent.NewEffectInferrer(config.PolicyConfig{
		SensitivePaths: []string{".env"},
	})
	inference, err := inferrer.Infer(context.Background(), core.ToolProposal{
		Tool: "code.read_file",
		Input: map[string]any{
			"path": ".env",
		},
	})
	if err != nil {
		t.Fatalf("Infer() error = %v", err)
	}
	if !inference.Sensitive || !inference.ApprovalRequired || inference.RiskLevel != "sensitive" {
		t.Fatalf("inference = %+v, want sensitive approval", inference)
	}
}

func testSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
