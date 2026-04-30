package agent

import (
	"context"

	v2 "local-agent/internal/agent/planner"
	"local-agent/internal/agent/planner/catalog"
	"local-agent/internal/agent/planner/compile"
	"local-agent/internal/agent/planner/fastpath"
	"local-agent/internal/agent/planner/intent"
	"local-agent/internal/agent/planner/normalize"
	"local-agent/internal/agent/planner/semantic"
	"local-agent/internal/agent/planner/validate"
	"local-agent/internal/core"
	"local-agent/internal/einoapp"
)

// HybridPlanner adapts Planner V2 to the existing agent.Planner interface.
type HybridPlanner struct {
	Adapter        einoapp.ProposalToolAdapter
	Registry       catalog.Registry
	Catalog        catalog.PlanningCatalog
	Semantic       semantic.LLMSemanticPlanner
	Mode           string
	Config         semantic.Config
	SensitivePaths []string
}

// NewHybridPlanner wires Planner V2 for runtime use.
func NewHybridPlanner(registry catalog.Registry, model semantic.ChatModel, cfg semantic.Config, sensitivePaths []string) HybridPlanner {
	cat := catalog.New(registry)
	return HybridPlanner{
		Registry:       registry,
		Catalog:        cat,
		Semantic:       semantic.NewLLMPlanner(model, cfg),
		Mode:           cfg.Mode,
		Config:         semantic.NormalizeConfig(cfg),
		SensitivePaths: append([]string(nil), sensitivePaths...),
	}
}

// Plan implements the existing Planner interface.
func (p HybridPlanner) Plan(ctx context.Context, input PlanInput) (Plan, error) {
	if input.LastToolResult != nil {
		if next, ok := (HeuristicPlanner{Adapter: p.Adapter}).planAfterTool(input); ok {
			return next, nil
		}
		return Plan{
			Decision: PlanDecisionStop,
			Message:  summarizeToolResult(input.LastToolResult),
			Reason:   "hybrid planner stops after one tool result",
		}, nil
	}
	engine := p.engine()
	compiled, err := engine.Plan(ctx, v2.Request{
		ConversationID: input.ConversationID,
		UserMessage:    input.UserMessage,
	})
	if err != nil {
		return Plan{}, err
	}
	return p.toPlan(compiled, input), nil
}

func (p HybridPlanner) engine() v2.HybridPlanner {
	cat := p.Catalog
	if len(cat.All()) == 0 {
		cat = catalog.New(p.Registry)
	}
	mode := v2.Mode(p.Mode)
	if mode == "" && p.Config.Mode != "" {
		mode = v2.Mode(p.Config.Mode)
	}
	if mode == "" {
		mode = v2.ModeHybrid
	}
	return v2.HybridPlanner{
		Normalizer: normalize.New(),
		Classifier: intent.New(),
		FastPath:   fastpath.New(),
		Semantic:   p.Semantic,
		Validator:  validate.New(cat, validate.Options{SensitivePaths: p.SensitivePaths}),
		Compiler:   compile.New(cat, p.adapter()),
		Catalog:    cat,
		Mode:       mode,
		Config:     p.Config,
	}
}

func (p HybridPlanner) adapter() einoapp.ProposalToolAdapter {
	return p.Adapter
}

func (p HybridPlanner) toPlan(compiled compile.CompiledPlan, input PlanInput) Plan {
	switch compiled.Decision {
	case compile.DecisionTool:
		plan := Plan{
			Decision:       PlanDecisionTool,
			Preamble:       preambleForProposal(compiled.ToolProposal, compiled.Preamble),
			ToolProposal:   cloneProposalPtr(compiled.ToolProposal),
			Reason:         compiled.Reason,
			PlannerSource:  string(compiled.PlannerSource),
			CandidateCount: compiled.CandidateCount,
		}
		if compiled.ToolProposal != nil {
			plan.CodePlan = codePlanForProposal(*compiled.ToolProposal, input.UserMessage)
		}
		return plan
	default:
		return Plan{
			Decision:       PlanDecisionAnswer,
			Message:        compiled.Message,
			Reason:         compiled.Reason,
			PlannerSource:  string(compiled.PlannerSource),
			CandidateCount: compiled.CandidateCount,
		}
	}
}

func cloneProposalPtr(proposal *core.ToolProposal) *core.ToolProposal {
	if proposal == nil {
		return nil
	}
	cp := cloneProposal(*proposal)
	return &cp
}

func preambleForProposal(proposal *core.ToolProposal, fallback string) string {
	if proposal == nil {
		return fallback
	}
	if proposal.Tool == "memory.extract_candidates" {
		if queue, _ := proposal.Input["queue"].(bool); !queue {
			return "我会先运行记忆候选提取；疑似敏感内容不会写入 review queue 或长期记忆。"
		}
	}
	return fallback
}

func codePlanForProposal(proposal core.ToolProposal, goal string) *CodePlan {
	workspace := workspaceFromProposal(proposal)
	switch proposal.Tool {
	case "code.search_text":
		return newCodePlan(CodeTaskSearch, workspace, goal, []CodePlanStep{{Tool: proposal.Tool, Purpose: proposal.Purpose, Input: proposal.Input}})
	case "code.read_file":
		return newCodePlan(CodeTaskInspect, workspace, goal, []CodePlanStep{{Tool: proposal.Tool, Purpose: proposal.Purpose, Input: proposal.Input}})
	case "code.inspect_project":
		return codeInspectPlan(workspace, goal)
	case "code.run_tests":
		return newCodePlan(CodeTaskTest, workspace, goal, []CodePlanStep{{Tool: "code.inspect_project", Purpose: "检测项目结构", Input: map[string]any{"path": workspace}}, {Tool: "code.run_tests", Purpose: proposal.Purpose, Input: proposal.Input}, {Tool: "code.parse_test_failure", Purpose: "如失败则解析测试输出"}})
	case "code.fix_test_failure_loop":
		return codeFixPlan(workspace, goal)
	case "git.status", "git.diff", "git.diff_summary", "git.commit_message_proposal", "git.log":
		return newCodePlan(CodeTaskGit, workspace, goal, []CodePlanStep{{Tool: proposal.Tool, Purpose: proposal.Purpose, Input: proposal.Input}})
	default:
		return nil
	}
}

func workspaceFromProposal(proposal core.ToolProposal) string {
	if value, _ := proposal.Input["workspace"].(string); value != "" {
		return value
	}
	if value, _ := proposal.Input["path"].(string); value != "" {
		return extractWorkspaceFromPath(value)
	}
	return "."
}

func extractWorkspaceFromPath(path string) string {
	if path == "" {
		return "."
	}
	return extractWorkspace("workspace: " + path)
}
