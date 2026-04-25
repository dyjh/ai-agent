package tests

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"local-agent/internal/agent"
	"local-agent/internal/config"
	"local-agent/internal/core"
	toolscore "local-agent/internal/tools"
	"local-agent/internal/tools/code"
	"local-agent/internal/tools/ops"
)

func TestTargetedCapabilitySmokeOpsAndCodeSearch(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<h1>欢迎来到最小静态站点</h1>\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	registry := toolscore.NewRegistry()
	registry.Register(core.ToolSpec{
		ID:             "ops.local.system_info",
		Provider:       "local",
		Name:           "ops.local.system_info",
		Description:    "Read local system information",
		DefaultEffects: []string{"read", "system.read"},
	}, &ops.LocalExecutor{Operation: "system_info"})
	registry.Register(core.ToolSpec{
		ID:             "code.search_text",
		Provider:       "local",
		Name:           "code.search_text",
		Description:    "Search text in workspace files",
		DefaultEffects: []string{"read", "code.read"},
	}, &code.SearchExecutor{Workspace: code.Workspace{Root: root}})

	policy := config.Default().Policy
	router := toolscore.NewRouter(
		registry,
		agent.NewEffectInferrer(policy),
		agent.NewPolicyEngine(policy),
		agent.NewApprovalCenter(),
		nil,
	)

	opsOutcome, err := router.Propose(ctx, "targeted_capability", "conv_targeted", core.ToolProposal{
		ID:      "tool_ops_system_info",
		Tool:    "ops.local.system_info",
		Input:   map[string]any{},
		Purpose: "读取本机系统概况",
	})
	if err != nil {
		t.Fatalf("Propose(ops.local.system_info) error = %v", err)
	}
	if opsOutcome.Approval != nil || opsOutcome.Result == nil {
		t.Fatalf("ops route outcome = %+v, want auto-executed result", opsOutcome)
	}
	if opsOutcome.Result.Output["operation"] != "system_info" {
		t.Fatalf("ops output = %+v, want system_info operation", opsOutcome.Result.Output)
	}

	searchOutcome, err := router.Propose(ctx, "targeted_capability", "conv_targeted", core.ToolProposal{
		ID:   "tool_code_search_text",
		Tool: "code.search_text",
		Input: map[string]any{
			"path":  ".",
			"query": "最小静态站点",
			"limit": 10,
		},
		Purpose: "定位包含指定文本的文件",
	})
	if err != nil {
		t.Fatalf("Propose(code.search_text) error = %v", err)
	}
	if searchOutcome.Approval != nil || searchOutcome.Result == nil {
		t.Fatalf("code search route outcome = %+v, want auto-executed result", searchOutcome)
	}
	matches, ok := searchOutcome.Result.Output["matches"].([]map[string]any)
	if !ok || len(matches) != 1 {
		t.Fatalf("matches = %#v, want one typed match", searchOutcome.Result.Output["matches"])
	}
	if matches[0]["path"] != "index.html" {
		t.Fatalf("match path = %v, want index.html", matches[0]["path"])
	}
}

func TestTargetedPlannerMapsLiveRegressionPhrases(t *testing.T) {
	planner := agent.HeuristicPlanner{}
	cases := []struct {
		name         string
		message      string
		wantTool     string
		wantInput    map[string]any
		wantCodeKind agent.CodeTaskKind
	}{
		{
			name:     "ops system overview",
			message:  "请获取这台本地机器的系统概况",
			wantTool: "ops.local.system_info",
		},
		{
			name:     "code locate containing text",
			message:  "请定位包含 `最小静态站点` 的文件，workspace: /www/wwwroot/test",
			wantTool: "code.search_text",
			wantInput: map[string]any{
				"path":  "/www/wwwroot/test",
				"query": "最小静态站点",
			},
			wantCodeKind: agent.CodeTaskSearch,
		},
		{
			name:     "code open workspace file",
			message:  "请打开 workspace: /www/wwwroot/test 中的 `index.html` 并确认页面标题",
			wantTool: "code.read_file",
			wantInput: map[string]any{
				"path": "/www/wwwroot/test/index.html",
			},
			wantCodeKind: agent.CodeTaskInspect,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := planner.Plan(context.Background(), agent.PlanInput{UserMessage: tc.message})
			if err != nil {
				t.Fatalf("Plan() error = %v", err)
			}
			if plan.ToolProposal == nil {
				t.Fatalf("Plan() produced no tool proposal, decision=%s reason=%s", plan.Decision, plan.Reason)
			}
			if plan.ToolProposal.Tool != tc.wantTool {
				t.Fatalf("tool = %s, want %s (reason=%s)", plan.ToolProposal.Tool, tc.wantTool, plan.Reason)
			}
			for key, want := range tc.wantInput {
				if got := plan.ToolProposal.Input[key]; got != want {
					t.Fatalf("input[%s] = %v, want %v", key, got, want)
				}
			}
			if tc.wantCodeKind != "" && (plan.CodePlan == nil || plan.CodePlan.Kind != tc.wantCodeKind) {
				t.Fatalf("code plan = %#v, want kind %s", plan.CodePlan, tc.wantCodeKind)
			}
		})
	}
}
