package tests

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"local-agent/internal/agent"
	v2 "local-agent/internal/agent/planner"
	"local-agent/internal/agent/planner/catalog"
	"local-agent/internal/agent/planner/compile"
	"local-agent/internal/agent/planner/fastpath"
	"local-agent/internal/agent/planner/intent"
	"local-agent/internal/agent/planner/normalize"
	"local-agent/internal/agent/planner/semantic"
	"local-agent/internal/agent/planner/validate"
	"local-agent/internal/config"
	"local-agent/internal/einoapp"
)

func TestPlannerV2HybridFastPathHit(t *testing.T) {
	planner := agent.HeuristicPlanner{}
	plan, err := planner.Plan(context.Background(), agent.PlanInput{UserMessage: "请获取这台本地机器的系统概况"})
	if err != nil {
		t.Fatalf("Plan error = %v", err)
	}
	if plan.ToolProposal == nil || plan.ToolProposal.Tool != "ops.local.system_info" {
		t.Fatalf("plan = %+v, want ops.local.system_info", plan)
	}
}

func TestPlannerV2SemanticFallback(t *testing.T) {
	planner := newTestV2Planner(`{"decision":"tool","confidence":0.9,"domain":"code","steps":[{"tool":"code.search_text","purpose":"search","input":{"path":".","query":"TODO","limit":50}}]}`, v2.ModeSemantic)
	compiled, err := planner.Plan(context.Background(), v2.Request{UserMessage: "find containing `TODO` workspace: ."})
	if err != nil {
		t.Fatalf("Plan error = %v", err)
	}
	if compiled.ToolProposal == nil || compiled.ToolProposal.Tool != "code.search_text" {
		t.Fatalf("compiled = %+v", compiled)
	}
}

func TestPlannerV2SemanticValidationFailureClarifies(t *testing.T) {
	planner := newTestV2Planner(`{"decision":"tool","confidence":0.9,"domain":"code","steps":[{"tool":"unknown.tool","purpose":"bad","input":{}}]}`, v2.ModeSemantic)
	compiled, err := planner.Plan(context.Background(), v2.Request{UserMessage: "find containing `TODO` workspace: ."})
	if err != nil {
		t.Fatalf("Plan error = %v", err)
	}
	if compiled.Decision != compile.DecisionAnswer || compiled.Message == "" {
		t.Fatalf("compiled = %+v, want clarification answer", compiled)
	}
}

func TestPlannerV2NoToolRequestAnswers(t *testing.T) {
	planner := newTestV2Planner(``, v2.ModeHybrid)
	compiled, err := planner.Plan(context.Background(), v2.Request{UserMessage: "你好"})
	if err != nil {
		t.Fatalf("Plan error = %v", err)
	}
	if compiled.Decision != compile.DecisionAnswer || compiled.ToolProposal != nil {
		t.Fatalf("compiled = %+v, want answer", compiled)
	}
}

func TestPlannerV2NoLLMAvailableDeterministicStillWorks(t *testing.T) {
	planner := newTestV2Planner(``, v2.ModeHybrid)
	planner.Semantic = semantic.LLMSemanticPlanner{}
	compiled, err := planner.Plan(context.Background(), v2.Request{UserMessage: "请定位包含 `最小静态站点` 的文件，workspace: /www/wwwroot/test"})
	if err != nil {
		t.Fatalf("Plan error = %v", err)
	}
	if compiled.ToolProposal == nil || compiled.ToolProposal.Tool != "code.search_text" {
		t.Fatalf("compiled = %+v", compiled)
	}
}

func TestPlannerV2SafetyRegressionLLMBlockedTools(t *testing.T) {
	planner := newTestV2Planner(`{"decision":"tool","confidence":0.99,"domain":"mcp","steps":[{"tool":"mcp.call_tool","purpose":"bad","input":{"server_id":"s","tool_name":"x","args":{}}}]}`, v2.ModeSemantic)
	compiled, err := planner.Plan(context.Background(), v2.Request{UserMessage: "find containing `TODO` workspace: ."})
	if err != nil {
		t.Fatalf("Plan error = %v", err)
	}
	if compiled.ToolProposal != nil {
		t.Fatalf("mcp.call_tool must not compile to proposal: %+v", compiled.ToolProposal)
	}
}

func TestPlannerV2SafetyRegressionApprovalNotBypassed(t *testing.T) {
	planner := newTestV2Planner(`{"decision":"tool","confidence":0.99,"domain":"git","steps":[{"tool":"git.clean","purpose":"clean","input":{"workspace":"."}}]}`, v2.ModeSemantic)
	compiled, err := planner.Plan(context.Background(), v2.Request{UserMessage: "find containing `TODO` workspace: ."})
	if err != nil {
		t.Fatalf("Plan error = %v", err)
	}
	if compiled.ToolProposal == nil || compiled.ToolProposal.Tool != "git.clean" {
		t.Fatalf("compiled = %+v", compiled)
	}
	policy := config.Default().Policy
	inference, err := agent.NewEffectInferrer(policy).Infer(context.Background(), *compiled.ToolProposal)
	if err != nil {
		t.Fatalf("Infer error = %v", err)
	}
	decision, err := agent.NewPolicyEngine(policy).Decide(context.Background(), *compiled.ToolProposal, inference)
	if err != nil {
		t.Fatalf("Decide error = %v", err)
	}
	if !decision.RequiresApproval {
		t.Fatalf("decision = %+v, want approval required", decision)
	}
}

func newTestV2Planner(response string, mode v2.Mode) v2.HybridPlanner {
	cat := catalog.New(nil)
	return v2.HybridPlanner{
		Normalizer: normalize.New(),
		Classifier: intent.New(),
		FastPath:   fastpath.New(),
		Semantic: semantic.NewLLMPlanner(fakeSemanticModel{response: response}, semantic.Config{
			Mode:                    string(mode),
			SemanticEnabled:         true,
			MaxRetries:              1,
			RequireSchemaValidation: true,
		}),
		Validator: validate.New(cat, validate.Options{SensitivePaths: config.Default().Policy.SensitivePaths}),
		Compiler:  compile.New(cat, einoapp.ProposalToolAdapter{}),
		Catalog:   cat,
		Mode:      mode,
	}
}

type fakeSemanticModel struct {
	response string
}

func (m fakeSemanticModel) Generate(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	return &schema.Message{Role: schema.Assistant, Content: m.response}, nil
}
