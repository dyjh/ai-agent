package tests

import (
	"context"
	"testing"
	"time"

	"local-agent/internal/agent"
	"local-agent/internal/config"
	"local-agent/internal/core"
	toolscore "local-agent/internal/tools"
)

func TestApprovalResumeMismatchAndIdempotency(t *testing.T) {
	runtime, _, executor := newWorkflowRuntime(core.ToolProposal{
		ID:        "tool_write_idempotent",
		Tool:      "shell.exec",
		Input:     map[string]any{"command": "pnpm add axios"},
		Purpose:   "install dependency",
		CreatedAt: time.Now().UTC(),
	})

	events := startWorkflow(t, runtime, "conv_idempotent", "install axios")
	runID := eventValue(t, events, "run.started").RunID
	approvalID := eventValue(t, events, "approval.requested").ApprovalID

	if _, err := runtime.Resume(context.Background(), runID, "apr_wrong", true); err == nil {
		t.Fatalf("expected run/approval mismatch to fail")
	}

	if _, err := runtime.Resume(context.Background(), runID, approvalID, true); err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if executor.count != 1 {
		t.Fatalf("executor count = %d, want 1", executor.count)
	}
	if _, err := runtime.Resume(context.Background(), runID, approvalID, true); err == nil {
		t.Fatalf("expected duplicate resume to fail")
	}
}

func TestApprovalSnapshotHashMismatchBlocksExecution(t *testing.T) {
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

	outcome, err := router.Propose(context.Background(), "run_hash", "conv_hash", core.ToolProposal{
		ID:   "tool_hash",
		Tool: "shell.exec",
		Input: map[string]any{
			"command": "pnpm add axios",
		},
	})
	if err != nil {
		t.Fatalf("Propose() error = %v", err)
	}
	record, err := approvals.Get(outcome.Approval.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	record.SnapshotHash = "sha256:bad"
	if err := approvals.Hydrate(*record); err != nil {
		t.Fatalf("Hydrate() error = %v", err)
	}
	if _, err := approvals.Approve(outcome.Approval.ID); err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if _, err := router.ExecuteApproved(context.Background(), outcome.Approval.ID); err == nil {
		t.Fatalf("expected snapshot hash mismatch to block execution")
	}
	if executor.lastInput != nil {
		t.Fatalf("executor should not run when snapshot hash mismatches")
	}
}
