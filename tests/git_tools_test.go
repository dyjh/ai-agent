package tests

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"local-agent/internal/agent"
	"local-agent/internal/config"
	"local-agent/internal/core"
	toolscore "local-agent/internal/tools"
	"local-agent/internal/tools/gittools"
)

func TestGitToolsPolicyAndSnapshot(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := newGitRepo(t)
	registry := toolscore.NewRegistry()
	registerGitTestTool := func(name, operation string) {
		registry.Register(core.ToolSpec{ID: name, Name: name, DefaultEffects: []string{"git.read"}}, &gittools.Executor{Root: root, Operation: operation, MaxOutputBytes: 20000})
	}
	registerGitTestTool("git.status", "status")
	registerGitTestTool("git.diff", "diff")
	registerGitTestTool("git.log", "log")
	registerGitTestTool("git.add", "add")
	registerGitTestTool("git.commit", "commit")
	registerGitTestTool("git.restore", "restore")
	registerGitTestTool("git.clean", "clean")

	approvals := agent.NewApprovalCenter()
	router := toolscore.NewRouter(
		registry,
		agent.NewEffectInferrer(config.PolicyConfig{MinConfidenceForAutoExecute: 0.85}),
		agent.NewPolicyEngine(config.PolicyConfig{MinConfidenceForAutoExecute: 0.85}),
		approvals,
		nil,
	)

	status, err := router.Propose(context.Background(), "run_git", "conv_git", core.ToolProposal{
		ID: "git_status", Tool: "git.status", Input: map[string]any{"workspace": "."},
	})
	if err != nil {
		t.Fatalf("git.status Propose() error = %v", err)
	}
	if status.Approval != nil || status.Result == nil {
		t.Fatalf("git.status should auto execute, outcome=%+v", status)
	}

	add, err := router.Propose(context.Background(), "run_git", "conv_git", core.ToolProposal{
		ID: "git_add", Tool: "git.add", Input: map[string]any{"workspace": ".", "paths": []any{"file.txt"}},
	})
	if err != nil {
		t.Fatalf("git.add Propose() error = %v", err)
	}
	if add.Approval == nil {
		t.Fatalf("git.add should require approval")
	}
	if add.Approval.InputSnapshot["command"] != "git add -- file.txt" {
		t.Fatalf("approval command snapshot = %v", add.Approval.InputSnapshot["command"])
	}
	if _, err := approvals.Approve(add.Approval.ID); err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	add.Approval.InputSnapshot["paths"] = []any{"other.txt"}
	if _, err := router.ExecuteApproved(context.Background(), add.Approval.ID); err != nil {
		t.Fatalf("ExecuteApproved(git.add) error = %v", err)
	}

	clean, err := router.Propose(context.Background(), "run_git", "conv_git", core.ToolProposal{
		ID: "git_clean", Tool: "git.clean", Input: map[string]any{"workspace": "."},
	})
	if err != nil {
		t.Fatalf("git.clean Propose() error = %v", err)
	}
	if clean.Approval == nil || clean.Inference.RiskLevel != "danger" {
		t.Fatalf("git.clean should require danger approval, inference=%+v approval=%+v", clean.Inference, clean.Approval)
	}
}

func TestGitToolWorkspaceEscapeRejected(t *testing.T) {
	root := t.TempDir()
	_, err := (&gittools.Executor{Root: root, Operation: "status"}).Execute(context.Background(), map[string]any{
		"workspace": "../outside",
	})
	if err == nil {
		t.Fatalf("expected workspace escape to fail")
	}
}

func newGitRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "agent@example.test")
	runGit(t, root, "config", "user.name", "Agent Test")
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGit(t, root, "add", "file.txt")
	runGit(t, root, "commit", "-m", "initial")
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatalf("modify file: %v", err)
	}
	return root
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}
