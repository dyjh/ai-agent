package tests

import (
	"context"
	"strings"
	"testing"
	"time"

	"local-agent/internal/agent"
	"local-agent/internal/core"
	"local-agent/internal/db/repo"
)

func TestDurableRunStateRoundTripAndRedaction(t *testing.T) {
	store := repo.NewMemoryStore()
	stateStore := agent.NewPersistentRunStateStore(store.AgentRuns, store.AgentRunSteps)

	state := agent.RunState{
		RunID:          "run_redacted",
		ConversationID: "conv_redacted",
		Status:         agent.RunStatusPausedForApproval,
		CurrentStep:    "request_approval",
		UserMessage:    "api_key=secret-value",
		Proposal: &core.ToolProposal{
			ID:    "tool_secret",
			Tool:  "shell.exec",
			Input: map[string]any{"command": "echo token=secret-value"},
		},
		Policy: &core.PolicyDecision{
			ApprovalPayload: map[string]any{"token": "secret-value"},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := stateStore.Save(context.Background(), state); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	step := agent.RunStep{
		StepID: "step_redacted",
		RunID:  state.RunID,
		Index:  1,
		Type:   agent.RunStepTypeRequestApproval,
		Status: agent.RunStepStatusPaused,
		Approval: &core.ApprovalRecord{
			ID:            "apr_redacted",
			RunID:         state.RunID,
			InputSnapshot: map[string]any{"command": "echo token=secret-value"},
			SnapshotHash:  "sha256:test",
			Status:        core.ApprovalPending,
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := stateStore.SaveStep(context.Background(), step); err != nil {
		t.Fatalf("SaveStep() error = %v", err)
	}

	got, err := stateStore.Get(context.Background(), state.RunID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status != agent.RunStatusPausedForApproval {
		t.Fatalf("status = %s, want paused_for_approval", got.Status)
	}
	if strings.Contains(got.UserMessage, "secret-value") {
		t.Fatalf("expected user message to be redacted, got %q", got.UserMessage)
	}
	if got.Proposal == nil || !strings.Contains(got.Proposal.Input["command"].(string), "[REDACTED]") {
		t.Fatalf("expected proposal input to be redacted, got %#v", got.Proposal)
	}

	got.Policy.ApprovalPayload["token"] = "mutated"
	gotAgain, err := stateStore.Get(context.Background(), state.RunID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if gotAgain.Policy.ApprovalPayload["token"] == "mutated" {
		t.Fatalf("expected deep-cloned state payload")
	}

	steps, err := stateStore.ListSteps(context.Background(), state.RunID)
	if err != nil {
		t.Fatalf("ListSteps() error = %v", err)
	}
	if len(steps) != 1 {
		t.Fatalf("steps len = %d, want 1", len(steps))
	}
	if steps[0].Approval == nil || steps[0].Approval.InputSnapshot["command"] != "echo token=secret-value" {
		t.Fatalf("expected durable approval snapshot to remain exact for resume")
	}
}
