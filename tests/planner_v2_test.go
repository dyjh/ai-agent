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
	if compiled.Message != "" {
		t.Fatalf("compiled message = %q, want empty so runtime uses runner", compiled.Message)
	}
	if compiled.AnswerMode != compile.AnswerModeRunner {
		t.Fatalf("answer mode = %q, want runner", compiled.AnswerMode)
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
	compiled, err := planner.Plan(context.Background(), v2.Request{UserMessage: "tool_id: git.clean workspace: ."})
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

func TestPlannerV2SemanticStrictDisablesFastPath(t *testing.T) {
	planner := newStrictTestV2Planner(`{"decision":"tool","confidence":0.93,"domain":"ops","steps":[{"tool":"ops.local.system_info","purpose":"inspect system","input":{}}]}`)
	compiled, err := planner.Plan(context.Background(), v2.Request{UserMessage: "请获取这台本地机器的系统概况"})
	if err != nil {
		t.Fatalf("Plan error = %v", err)
	}
	if compiled.ToolProposal == nil || compiled.ToolProposal.Tool != "ops.local.system_info" {
		t.Fatalf("compiled = %+v, want ops.local.system_info", compiled)
	}
	if compiled.PlannerSource != semantic.PlannerSourceSemanticLLM {
		t.Fatalf("planner source = %s, want semantic_llm", compiled.PlannerSource)
	}
	if compiled.Route != "tool_needed" || compiled.RouteSource == "" {
		t.Fatalf("route trace = %q/%q, want tool_needed with source", compiled.Route, compiled.RouteSource)
	}
}

func TestPlannerV2SemanticStrictDisablesCandidateFallback(t *testing.T) {
	planner := newStrictTestV2Planner(``)
	compiled, err := planner.Plan(context.Background(), v2.Request{UserMessage: "请定位包含 `最小静态站点` 的文件，workspace: /www/wwwroot/test"})
	if err != nil {
		t.Fatalf("Plan error = %v", err)
	}
	if compiled.ToolProposal != nil {
		t.Fatalf("compiled = %+v, semantic_strict must not fallback to candidates", compiled)
	}
	if compiled.PlannerSource != semantic.PlannerSourceToolUnavailable {
		t.Fatalf("planner source = %s, want tool_planner_unavailable", compiled.PlannerSource)
	}
}

func TestPlannerV2SemanticStrictLLMSelectsTool(t *testing.T) {
	planner := newStrictTestV2Planner(`{"decision":"tool","confidence":0.93,"domain":"ops","steps":[{"tool":"ops.local.system_info","purpose":"inspect system","input":{}}]}`)
	compiled, err := planner.Plan(context.Background(), v2.Request{UserMessage: "查看本地系统"})
	if err != nil {
		t.Fatalf("Plan error = %v", err)
	}
	if compiled.ToolProposal == nil || compiled.ToolProposal.Tool != "ops.local.system_info" {
		t.Fatalf("compiled = %+v, want semantic-selected system_info", compiled)
	}
	if compiled.PlannerSource != semantic.PlannerSourceSemanticLLM {
		t.Fatalf("planner source = %s, want semantic_llm", compiled.PlannerSource)
	}
}

func TestPlannerV2SemanticStrictSystemReleaseUsesOpsTool(t *testing.T) {
	planner := newStrictTestV2Planner(`{"decision":"tool","confidence":0.93,"domain":"ops","steps":[{"tool":"ops.local.system_info","purpose":"inspect OS release","input":{}}]}`)
	compiled, err := planner.Plan(context.Background(), v2.Request{UserMessage: "查看本地操作系统发行版"})
	if err != nil {
		t.Fatalf("Plan error = %v", err)
	}
	if compiled.Route != "tool_needed" || compiled.CandidateCount == 0 {
		t.Fatalf("compiled = %+v, want tool route with candidates", compiled)
	}
	if compiled.ToolProposal == nil || compiled.ToolProposal.Tool != "ops.local.system_info" {
		t.Fatalf("compiled = %+v, want ops.local.system_info", compiled)
	}
	if compiled.PlannerSource != semantic.PlannerSourceSemanticLLM {
		t.Fatalf("planner source = %s, want semantic_llm", compiled.PlannerSource)
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
	if decision.RequiresApproval {
		t.Fatalf("decision = %+v, read-only ops tool should auto execute", decision)
	}
}

func TestPlannerV2SemanticStrictChatGateSkipsToolPlanner(t *testing.T) {
	model := &countingSemanticModel{response: `{"decision":"tool","confidence":0.9,"domain":"ops","steps":[{"tool":"ops.local.system_info","purpose":"bad","input":{}}]}`}
	planner := newStrictTestV2PlannerWithModel(model)
	compiled, err := planner.Plan(context.Background(), v2.Request{UserMessage: "你好"})
	if err != nil {
		t.Fatalf("Plan error = %v", err)
	}
	if model.calls != 0 {
		t.Fatalf("semantic model calls = %d, want 0", model.calls)
	}
	if compiled.Decision != compile.DecisionAnswer || compiled.ToolProposal != nil {
		t.Fatalf("compiled = %+v, want direct answer plan", compiled)
	}
	if compiled.Message != "" {
		t.Fatalf("compiled message = %q, want empty so runtime uses runner", compiled.Message)
	}
	if compiled.AnswerMode != compile.AnswerModeRunner {
		t.Fatalf("answer mode = %q, want runner", compiled.AnswerMode)
	}
	if compiled.SemanticPlan.Decision != semantic.SemanticPlanNoTool {
		t.Fatalf("semantic decision = %s, want no_tool", compiled.SemanticPlan.Decision)
	}
	if compiled.PlannerSource != semantic.PlannerSourceConversationRouter {
		t.Fatalf("planner source = %s, want conversation_router", compiled.PlannerSource)
	}
}

func TestPlannerV2SemanticStrictGeneralKnowledgeUsesRunner(t *testing.T) {
	model := &countingSemanticModel{response: `{"decision":"tool","confidence":0.9,"domain":"ops","steps":[{"tool":"ops.local.system_info","purpose":"bad","input":{}}]}`}
	planner := newStrictTestV2PlannerWithModel(model)
	compiled, err := planner.Plan(context.Background(), v2.Request{UserMessage: "神经内科分为哪些大类"})
	if err != nil {
		t.Fatalf("Plan error = %v", err)
	}
	if model.calls != 0 {
		t.Fatalf("semantic model calls = %d, want 0", model.calls)
	}
	if compiled.Decision != compile.DecisionAnswer || compiled.ToolProposal != nil {
		t.Fatalf("compiled = %+v, want answer-by-runner plan", compiled)
	}
	if compiled.Message != "" {
		t.Fatalf("compiled message = %q, want empty so runtime uses runner", compiled.Message)
	}
	if compiled.AnswerMode != compile.AnswerModeRunner {
		t.Fatalf("answer mode = %q, want runner", compiled.AnswerMode)
	}
	if compiled.SemanticPlan.Decision != semantic.SemanticPlanNoTool {
		t.Fatalf("semantic decision = %s, want no_tool", compiled.SemanticPlan.Decision)
	}
	if compiled.CandidateCount != 0 {
		t.Fatalf("candidate count = %d, want no candidate selection for direct answer", compiled.CandidateCount)
	}
	if compiled.Route != "direct_answer" || compiled.RouteSource == "" {
		t.Fatalf("route trace = %q/%q, want direct_answer with source", compiled.Route, compiled.RouteSource)
	}
}

func TestPlannerV2SemanticStrictCommonKnowledgeQuestionsUseRunner(t *testing.T) {
	model := &countingSemanticModel{response: `{"decision":"tool","confidence":0.9,"domain":"ops","steps":[{"tool":"ops.local.system_info","purpose":"bad","input":{}}]}`}
	planner := newStrictTestV2PlannerWithModel(model)
	inputs := []string{
		"Redis 持久化分为哪些方式",
		"MySQL 索引有哪些类型",
		"VPC 和子网是什么关系",
		"Go 的 goroutine 和线程有什么区别",
		"神经内科包括哪些方向",
	}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			compiled, err := planner.Plan(context.Background(), v2.Request{UserMessage: input})
			if err != nil {
				t.Fatalf("Plan error = %v", err)
			}
			if compiled.Route != "direct_answer" || compiled.RouteSource == "" {
				t.Fatalf("route trace = %q/%q, want direct_answer with source", compiled.Route, compiled.RouteSource)
			}
			if compiled.PlannerSource == semantic.PlannerSourceSemanticLLM {
				t.Fatalf("planner source = %s, ordinary knowledge must not enter semantic tool planner", compiled.PlannerSource)
			}
			if compiled.ToolProposal != nil || compiled.CandidateCount != 0 || compiled.AnswerMode != compile.AnswerModeRunner || compiled.Message != "" {
				t.Fatalf("compiled = %+v, want runner-only no-tool plan", compiled)
			}
		})
	}
	if model.calls != 0 {
		t.Fatalf("semantic model calls = %d, want 0", model.calls)
	}
}

func TestPlannerV2SemanticStrictRejectsToolPlannerAnswer(t *testing.T) {
	planner := newStrictTestV2Planner(`{"decision":"answer","confidence":0.9,"answer":"general answer"}`)
	compiled, err := planner.Plan(context.Background(), v2.Request{UserMessage: "查看本地系统"})
	if err != nil {
		t.Fatalf("Plan error = %v", err)
	}
	if compiled.ToolProposal != nil {
		t.Fatalf("compiled = %+v, want no tool proposal", compiled)
	}
	if compiled.AnswerMode != compile.AnswerModePlannerClarify || compiled.Message == "" {
		t.Fatalf("compiled = %+v, want planner clarification", compiled)
	}
}

func TestPlannerV2SemanticStrictToolPlannerNoToolUsesRunnerMode(t *testing.T) {
	planner := newStrictTestV2Planner(`{"decision":"no_tool","confidence":0.82,"reason":"no tool needed"}`)
	compiled, err := planner.Plan(context.Background(), v2.Request{UserMessage: "查看本地系统"})
	if err != nil {
		t.Fatalf("Plan error = %v", err)
	}
	if compiled.Route != "tool_needed" {
		t.Fatalf("route = %q, want tool_needed", compiled.Route)
	}
	if compiled.PlannerSource != semantic.PlannerSourceSemanticLLM || compiled.SemanticPlan.Decision != semantic.SemanticPlanNoTool {
		t.Fatalf("compiled = %+v, want semantic no_tool", compiled)
	}
	if compiled.ToolProposal != nil || compiled.Message != "" || compiled.AnswerMode != compile.AnswerModeRunner {
		t.Fatalf("compiled = %+v, want runner mode without tool/message", compiled)
	}
}

func TestPlannerV2SemanticStrictRejectsImplicitShellFallback(t *testing.T) {
	planner := newStrictTestV2Planner(`{"decision":"tool","confidence":0.9,"domain":"shell","steps":[{"tool":"shell.exec","purpose":"read distro","input":{"command":"cat /etc/os-release"}}]}`)
	compiled, err := planner.Plan(context.Background(), v2.Request{UserMessage: "查看系统发行版"})
	if err != nil {
		t.Fatalf("Plan error = %v", err)
	}
	if compiled.ToolProposal != nil {
		t.Fatalf("compiled = %+v, shell fallback must be rejected", compiled)
	}
	if compiled.Decision != compile.DecisionAnswer || compiled.Message == "" {
		t.Fatalf("compiled = %+v, want clarification", compiled)
	}
}

func TestPlannerV2SemanticStrictToolPlannerUnavailableDoesNotBlockDirectAnswer(t *testing.T) {
	planner := newStrictTestV2Planner(``)
	compiled, err := planner.Plan(context.Background(), v2.Request{UserMessage: "神经内科分为哪些大类"})
	if err != nil {
		t.Fatalf("Plan error = %v", err)
	}
	if compiled.Route != "direct_answer" || compiled.ToolProposal != nil || compiled.Message != "" || compiled.AnswerMode != compile.AnswerModeRunner {
		t.Fatalf("compiled = %+v, want direct answer by runner despite unavailable tool planner", compiled)
	}
}

func TestPlannerV2ExplicitShellProposalRequiresApproval(t *testing.T) {
	planner := newStrictTestV2Planner(`{"decision":"tool","confidence":0.9,"domain":"shell","steps":[{"tool":"shell.exec","purpose":"read distro","input":{"command":"cat /etc/os-release"}}]}`)
	compiled, err := planner.Plan(context.Background(), v2.Request{UserMessage: "用 shell 执行 cat /etc/os-release"})
	if err != nil {
		t.Fatalf("Plan error = %v", err)
	}
	if compiled.ToolProposal == nil || compiled.ToolProposal.Tool != "shell.exec" {
		t.Fatalf("compiled = %+v, want shell proposal", compiled)
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
		t.Fatalf("decision = %+v, shell proposal must require approval", decision)
	}
}

func TestPlannerV2SemanticStrictWorkspaceReadUsesToolPlanner(t *testing.T) {
	planner := newStrictTestV2Planner(`{"decision":"tool","confidence":0.92,"domain":"code","steps":[{"tool":"code.read_file","purpose":"read file","input":{"path":"/tmp/demo/main.go","max_bytes":200000}}]}`)
	compiled, err := planner.Plan(context.Background(), v2.Request{UserMessage: "请读取 workspace: /tmp/demo 中的 main.go"})
	if err != nil {
		t.Fatalf("Plan error = %v", err)
	}
	if compiled.Route != "tool_needed" || compiled.CandidateCount == 0 {
		t.Fatalf("compiled = %+v, want tool route with candidates", compiled)
	}
	if compiled.ToolProposal == nil || compiled.ToolProposal.Tool != "code.read_file" {
		t.Fatalf("compiled = %+v, want code.read_file", compiled)
	}
	if got := compiled.ToolProposal.Input["path"]; got != "/tmp/demo/main.go" {
		t.Fatalf("path = %v, want /tmp/demo/main.go", got)
	}
	if compiled.AnswerMode == compile.AnswerModeRunner {
		t.Fatalf("compiled = %+v, workspace read must not use ordinary runner", compiled)
	}
}

func TestPlannerV2HighRiskToolStillRequiresApproval(t *testing.T) {
	planner := newStrictTestV2Planner(`{"decision":"tool","confidence":0.9,"domain":"ops","steps":[{"tool":"ops.local.service_restart","purpose":"restart service","input":{"service":"nginx"}}]}`)
	compiled, err := planner.Plan(context.Background(), v2.Request{UserMessage: "重启服务 `nginx`"})
	if err != nil {
		t.Fatalf("Plan error = %v", err)
	}
	if compiled.ToolProposal == nil || compiled.ToolProposal.Tool != "ops.local.service_restart" {
		t.Fatalf("compiled = %+v, want service_restart", compiled)
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
		t.Fatalf("decision = %+v, high-risk tool must require approval", decision)
	}
}

func TestPlannerV2CandidateSelectorOnlyProvidesContext(t *testing.T) {
	planner := newStrictTestV2Planner(`{"decision":"tool","confidence":0.9,"domain":"ops","steps":[{"tool":"ops.local.processes","purpose":"inspect processes","input":{}}]}`)
	compiled, err := planner.Plan(context.Background(), v2.Request{UserMessage: "请获取这台本地机器的系统概况"})
	if err != nil {
		t.Fatalf("Plan error = %v", err)
	}
	if compiled.ToolProposal == nil || compiled.ToolProposal.Tool != "ops.local.processes" {
		t.Fatalf("compiled = %+v, want LLM-selected candidate rather than selector top1", compiled)
	}
	if compiled.PlannerSource != semantic.PlannerSourceSemanticLLM {
		t.Fatalf("planner source = %s, want semantic_llm", compiled.PlannerSource)
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

func newStrictTestV2Planner(response string) v2.HybridPlanner {
	return newStrictTestV2PlannerWithModel(fakeSemanticModel{response: response})
}

func newStrictTestV2PlannerWithModel(model semantic.ChatModel) v2.HybridPlanner {
	cat := catalog.New(nil)
	cfg := semantic.Config{
		Mode:                    string(v2.ModeSemanticStrict),
		SemanticEnabled:         true,
		MaxRetries:              1,
		RequireSchemaValidation: true,
		ConversationRouter: semantic.ConversationRouterConfig{
			Enabled:      true,
			Mode:         "lightweight",
			FallbackMode: "lightweight",
			MaxRetries:   1,
			RequireJSON:  true,
		},
		ChatGate: semantic.ChatGateConfig{Enabled: true, Mode: "lightweight"},
		ToolPlanner: semantic.ToolPlannerConfig{
			RequireLLMForToolChoice:    true,
			EnableFastPath:             false,
			AllowCandidateFallback:     false,
			CandidateSelectorAsContext: true,
		},
		Shell: semantic.ShellPlannerConfig{AllowAutoFallback: false},
		Debug: semantic.DebugConfig{ExposePlannerSource: true},
	}
	return v2.HybridPlanner{
		Normalizer: normalize.New(),
		Classifier: intent.New(),
		FastPath:   fastpath.New(),
		Semantic:   semantic.NewLLMPlanner(model, cfg),
		Validator:  validate.New(cat, validate.Options{SensitivePaths: config.Default().Policy.SensitivePaths}),
		Compiler:   compile.New(cat, einoapp.ProposalToolAdapter{}),
		Catalog:    cat,
		Mode:       v2.ModeSemanticStrict,
		Config:     cfg,
	}
}

type fakeSemanticModel struct {
	response string
}

func (m fakeSemanticModel) Generate(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	return &schema.Message{Role: schema.Assistant, Content: m.response}, nil
}

type countingSemanticModel struct {
	response string
	calls    int
}

func (m *countingSemanticModel) Generate(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	m.calls++
	return &schema.Message{Role: schema.Assistant, Content: m.response}, nil
}
