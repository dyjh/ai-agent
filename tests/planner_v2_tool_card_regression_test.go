package tests

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"local-agent/internal/agent"
)

func TestPlannerV2ToolCardRegressionCases(t *testing.T) {
	planner := agent.HeuristicPlanner{}
	cases := []struct {
		name      string
		message   string
		wantTool  string
		wantInput map[string]any
	}{
		{
			name:     "bug ops system overview cn",
			message:  "请获取这台本地机器的系统概况",
			wantTool: "ops.local.system_info",
		},
		{
			name:     "bug code search cn",
			message:  "请定位包含 `最小静态站点` 的文件，workspace: /www/wwwroot/test",
			wantTool: "code.search_text",
			wantInput: map[string]any{
				"path":  "/www/wwwroot/test",
				"query": "最小静态站点",
				"limit": 50,
			},
		},
		{
			name:     "bug code read cn",
			message:  "请打开 workspace: /www/wwwroot/test 中的 `index.html` 并确认页面标题",
			wantTool: "code.read_file",
			wantInput: map[string]any{
				"path":      "/www/wwwroot/test/index.html",
				"max_bytes": 200000,
			},
		},
		{
			name:     "ops system overview en",
			message:  "Get local machine system overview",
			wantTool: "ops.local.system_info",
		},
		{
			name:     "code search en",
			message:  "Find files containing `hello` in workspace /tmp/demo",
			wantTool: "code.search_text",
			wantInput: map[string]any{
				"path":  "/tmp/demo",
				"query": "hello",
				"limit": 50,
			},
		},
		{
			name:     "code read en",
			message:  "Open `index.html` in workspace /tmp/demo",
			wantTool: "code.read_file",
			wantInput: map[string]any{
				"path":      "/tmp/demo/index.html",
				"max_bytes": 200000,
			},
		},
		{
			name:     "mixed search",
			message:  "Please 查找包含 `hello` in workspace /tmp/demo",
			wantTool: "code.search_text",
			wantInput: map[string]any{
				"path":  "/tmp/demo",
				"query": "hello",
				"limit": 50,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := planner.Plan(context.Background(), agent.PlanInput{UserMessage: tc.message})
			if err != nil {
				t.Fatalf("Plan error = %v", err)
			}
			if plan.ToolProposal == nil || plan.ToolProposal.Tool != tc.wantTool {
				t.Fatalf("plan = %+v, want tool %s", plan, tc.wantTool)
			}
			for key, want := range tc.wantInput {
				if got := plan.ToolProposal.Input[key]; fmt.Sprint(got) != fmt.Sprint(want) {
					t.Fatalf("input[%s] = %v, want %v", key, got, want)
				}
			}
		})
	}
}

func TestPlannerV2ToolCardAmbiguousLogsClarifies(t *testing.T) {
	plan, err := (agent.HeuristicPlanner{}).Plan(context.Background(), agent.PlanInput{UserMessage: "帮我看一下日志"})
	if err != nil {
		t.Fatalf("Plan error = %v", err)
	}
	if plan.ToolProposal != nil {
		t.Fatalf("plan = %+v, want clarification without tool", plan)
	}
	if !strings.Contains(plan.Message, "补充") && !strings.Contains(plan.Message, "path") {
		t.Fatalf("message = %q, want clarification", plan.Message)
	}
}
