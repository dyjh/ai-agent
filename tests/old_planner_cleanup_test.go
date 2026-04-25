package tests

import (
	"context"
	"testing"

	"local-agent/internal/agent"
)

func TestHeuristicPlannerDelegatesToHybridPlanner(t *testing.T) {
	plan, err := (agent.HeuristicPlanner{}).Plan(context.Background(), agent.PlanInput{
		UserMessage: "Get local machine system overview",
	})
	if err != nil {
		t.Fatalf("Plan error = %v", err)
	}
	if plan.ToolProposal == nil || plan.ToolProposal.Tool != "ops.local.system_info" {
		t.Fatalf("plan = %+v, want HybridPlanner tool-card result", plan)
	}
}

func TestOldPlannerLegacyShellSwitchRemoved(t *testing.T) {
	plan, err := (agent.HeuristicPlanner{}).Plan(context.Background(), agent.PlanInput{
		UserMessage: "pwd",
	})
	if err != nil {
		t.Fatalf("Plan error = %v", err)
	}
	if plan.ToolProposal != nil && plan.ToolProposal.Tool == "shell.exec" {
		t.Fatalf("plan = %+v, legacy shell keyword switch should not run", plan)
	}
}
