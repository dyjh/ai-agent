package tests

import (
	"context"
	"testing"

	"local-agent/internal/agent"
	"local-agent/internal/core"
)

func TestPlannerUsesCodeToolsForCodeTasks(t *testing.T) {
	planner := agent.HeuristicPlanner{}
	plan, err := planner.Plan(context.Background(), agent.PlanInput{
		UserMessage: "帮我修 bug 并跑测试",
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if plan.ToolProposal == nil || plan.ToolProposal.Tool != "code.run_tests" {
		t.Fatalf("tool = %#v, want code.run_tests for explicit test request", plan.ToolProposal)
	}

	plan, err = planner.Plan(context.Background(), agent.PlanInput{
		UserMessage: "帮我实现这个功能",
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if plan.ToolProposal == nil || plan.ToolProposal.Tool != "code.inspect_project" {
		t.Fatalf("tool = %#v, want code.inspect_project", plan.ToolProposal)
	}
}

func TestPlannerParsesFailedTestsAfterRun(t *testing.T) {
	planner := agent.HeuristicPlanner{}
	plan, err := planner.Plan(context.Background(), agent.PlanInput{
		UserMessage: "跑测试",
		LastProposal: &core.ToolProposal{
			Tool: "code.run_tests",
		},
		LastToolResult: &core.ToolResult{
			Output: map[string]any{
				"passed":    false,
				"command":   "go test ./...",
				"stdout":    "--- FAIL: TestX\n",
				"stderr":    "",
				"exit_code": 1,
			},
		},
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if plan.ToolProposal == nil || plan.ToolProposal.Tool != "code.parse_test_failure" {
		t.Fatalf("tool = %#v, want code.parse_test_failure", plan.ToolProposal)
	}
}
