package tests

import (
	"context"
	"testing"

	"local-agent/internal/agent"
	"local-agent/internal/config"
	"local-agent/internal/core"
	toolscore "local-agent/internal/tools"
)

func TestApprovalSnapshotBehavior(t *testing.T) {
	registry := toolscore.NewRegistry()
	executor := &captureExecutor{}
	registry.Register(core.ToolSpec{
		ID:             "shell.exec",
		Name:           "shell.exec",
		Description:    "shell",
		DefaultEffects: []string{"read"},
	}, executor)

	approvals := agent.NewApprovalCenter()
	router := toolscore.NewRouter(
		registry,
		agent.NewEffectInferrer(config.PolicyConfig{SensitivePaths: []string{".env"}}),
		agent.NewPolicyEngine(config.PolicyConfig{MinConfidenceForAutoExecute: 0.85}),
		approvals,
		nil,
	)

	proposalA := core.ToolProposal{
		ID:   "tool_a",
		Tool: "shell.exec",
		Input: map[string]any{
			"command": "pnpm add axios",
		},
	}
	outcome, err := router.Propose(context.Background(), "run_1", "conv_1", proposalA)
	if err != nil {
		t.Fatalf("Propose() error = %v", err)
	}
	if outcome.Approval == nil {
		t.Fatalf("expected approval to be created")
	}
	if outcome.Approval.SnapshotHash == "" {
		t.Fatalf("expected snapshot hash")
	}
	recordCopy, err := approvals.Get(outcome.Approval.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	recordCopy.InputSnapshot["command"] = "pnpm add lodash"
	storedRecord, err := approvals.Get(outcome.Approval.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if storedRecord.InputSnapshot["command"] != "pnpm add axios" {
		t.Fatalf("approval snapshot should be immutable, got %v", storedRecord.InputSnapshot["command"])
	}
	if err := approvals.VerifySnapshotHash(outcome.Approval.ID); err != nil {
		t.Fatalf("VerifySnapshotHash() error = %v", err)
	}

	if _, err := router.ExecuteApproved(context.Background(), outcome.Approval.ID); err == nil {
		t.Fatalf("expected pending approval to block execution")
	}

	if _, err := approvals.Approve(outcome.Approval.ID); err != nil {
		t.Fatalf("Approve() error = %v", err)
	}

	proposalB := proposalA
	proposalB.Input = map[string]any{"command": "pnpm add lodash"}
	matches, err := approvals.SnapshotMatches(outcome.Approval.ID, proposalB)
	if err != nil {
		t.Fatalf("SnapshotMatches() error = %v", err)
	}
	if matches {
		t.Fatalf("expected changed proposal to require reapproval")
	}

	if _, err := router.ExecuteApproved(context.Background(), outcome.Approval.ID); err != nil {
		t.Fatalf("ExecuteApproved() error = %v", err)
	}
	if got := executor.lastInput["command"]; got != "pnpm add axios" {
		t.Fatalf("executed command = %v, want original approved snapshot", got)
	}

	if _, err := router.ExecuteReadOnly(context.Background(), core.ToolProposal{
		Tool: "shell.exec",
		Input: map[string]any{
			"command": "rm -rf /tmp/demo",
		},
	}); err == nil {
		t.Fatalf("expected dangerous plain-text command to require approval")
	}
}

type captureExecutor struct {
	lastInput map[string]any
}

func (c *captureExecutor) Execute(_ context.Context, input map[string]any) (*core.ToolResult, error) {
	c.lastInput = input
	return &core.ToolResult{Output: input}, nil
}
