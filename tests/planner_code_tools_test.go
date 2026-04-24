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
	if plan.CodePlan == nil || plan.CodePlan.Kind != agent.CodeTaskTest {
		t.Fatalf("code plan = %#v, want test plan", plan.CodePlan)
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
	if plan.CodePlan == nil || len(plan.CodePlan.Steps) == 0 {
		t.Fatalf("expected schema-driven code plan")
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

func TestPlannerCreatesFixCodePlanAndRerunsAfterPatch(t *testing.T) {
	planner := agent.HeuristicPlanner{}
	plan, err := planner.Plan(context.Background(), agent.PlanInput{
		UserMessage: "请修复测试失败",
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if plan.ToolProposal == nil || plan.ToolProposal.Tool != "code.fix_test_failure_loop" {
		t.Fatalf("tool = %#v, want code.fix_test_failure_loop", plan.ToolProposal)
	}
	if plan.CodePlan == nil || plan.CodePlan.Kind != agent.CodeTaskFix || !plan.CodePlan.RequiresApproval {
		t.Fatalf("code plan = %#v, want fix plan requiring approval", plan.CodePlan)
	}

	next, err := planner.Plan(context.Background(), agent.PlanInput{
		UserMessage: "请修复测试失败",
		LastProposal: &core.ToolProposal{
			Tool: "code.apply_patch",
		},
		LastToolResult: &core.ToolResult{
			Output: map[string]any{"status": "applied"},
		},
	})
	if err != nil {
		t.Fatalf("Plan() after apply error = %v", err)
	}
	if next.ToolProposal == nil || next.ToolProposal.Tool != "code.run_tests" {
		t.Fatalf("tool = %#v, want code.run_tests after apply_patch", next.ToolProposal)
	}
}

func TestPlannerPropagatesWorkspaceHint(t *testing.T) {
	planner := agent.HeuristicPlanner{}
	plan, err := planner.Plan(context.Background(), agent.PlanInput{
		UserMessage: "请检测并运行测试，workspace: services/api",
	})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if got := plan.ToolProposal.Input["workspace"]; got != "services/api" {
		t.Fatalf("workspace = %v, want services/api", got)
	}
	if plan.CodePlan == nil || plan.CodePlan.Workspace != "services/api" {
		t.Fatalf("code plan workspace = %#v, want services/api", plan.CodePlan)
	}

	gitPlan, err := planner.Plan(context.Background(), agent.PlanInput{
		UserMessage: "请总结 git diff，workspace: packages/web",
	})
	if err != nil {
		t.Fatalf("Plan(git) error = %v", err)
	}
	if got := gitPlan.ToolProposal.Input["workspace"]; got != "packages/web" {
		t.Fatalf("git workspace = %v, want packages/web", got)
	}

	readPlan, err := planner.Plan(context.Background(), agent.PlanInput{
		UserMessage: "请读取文件 `main.go`，workspace: cmd/agent",
	})
	if err != nil {
		t.Fatalf("Plan(read) error = %v", err)
	}
	if got := readPlan.ToolProposal.Input["path"]; got != "cmd/agent/main.go" {
		t.Fatalf("read path = %v, want cmd/agent/main.go", got)
	}
}
