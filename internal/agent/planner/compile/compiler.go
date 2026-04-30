package compile

import (
	"strings"

	"local-agent/internal/agent/planner/catalog"
	"local-agent/internal/agent/planner/semantic"
	"local-agent/internal/core"
)

// Decision mirrors the root agent PlanDecision without importing the parent package.
type Decision string

const (
	DecisionAnswer Decision = "answer"
	DecisionTool   Decision = "tool"
)

const (
	AnswerModeRunner               = "runner"
	AnswerModePlannerClarify       = "planner_clarify"
	AnswerModeCapabilityLimitation = "capability_limitation"
)

// CompiledPlan is a non-executable bridge from SemanticPlan to ToolProposal.
type CompiledPlan struct {
	Decision       Decision                   `json:"decision"`
	AnswerMode     string                     `json:"answer_mode,omitempty"`
	Route          string                     `json:"route,omitempty"`
	RouteSource    string                     `json:"route_source,omitempty"`
	Message        string                     `json:"message,omitempty"`
	Preamble       string                     `json:"preamble,omitempty"`
	Reason         string                     `json:"reason,omitempty"`
	PlannerSource  semantic.PlannerSource     `json:"planner_source,omitempty"`
	CandidateCount int                        `json:"candidate_count,omitempty"`
	ToolProposal   *core.ToolProposal         `json:"tool_proposal,omitempty"`
	SemanticPlan   semantic.SemanticPlan      `json:"semantic_plan"`
	SelectedStep   *semantic.SemanticPlanStep `json:"selected_step,omitempty"`
}

// Compiler compiles validated SemanticPlan into the existing proposal shape.
type Compiler struct {
	Catalog catalog.PlanningCatalog
	Adapter ProposalAdapter
}

// ProposalAdapter creates ToolProposal without executing it.
type ProposalAdapter interface {
	NewProposal(tool string, input map[string]any, purpose string, expectedEffects []string) core.ToolProposal
}

// New creates a compiler.
func New(cat catalog.PlanningCatalog, adapter ProposalAdapter) Compiler {
	return Compiler{Catalog: cat, Adapter: adapter}
}

// Compile compiles a validated plan. Multi-step plans initially expose the
// first step; the runtime still routes it through ToolRouter and policy.
func (c Compiler) Compile(plan semantic.SemanticPlan) CompiledPlan {
	switch plan.Decision {
	case semantic.SemanticPlanAnswer:
		return CompiledPlan{Decision: DecisionAnswer, Message: plan.Answer, Reason: plan.Reason, PlannerSource: plan.PlannerSource, SemanticPlan: plan}
	case semantic.SemanticPlanNoTool:
		return CompiledPlan{Decision: DecisionAnswer, AnswerMode: AnswerModeRunner, Message: "", Reason: plan.Reason, PlannerSource: plan.PlannerSource, SemanticPlan: plan}
	case semantic.SemanticPlanClarify:
		return CompiledPlan{Decision: DecisionAnswer, AnswerMode: AnswerModePlannerClarify, Message: plan.ClarifyingQuestion, Reason: plan.Reason, PlannerSource: plan.PlannerSource, SemanticPlan: plan}
	case semantic.SemanticPlanCapabilityLimitation:
		return CompiledPlan{Decision: DecisionAnswer, AnswerMode: AnswerModeCapabilityLimitation, Message: capabilityMessage(plan), Reason: plan.Reason, PlannerSource: plan.PlannerSource, SemanticPlan: plan}
	case semantic.SemanticPlanTool, semantic.SemanticPlanMultiStep:
		if len(plan.Steps) == 0 {
			return CompiledPlan{Decision: DecisionAnswer, AnswerMode: AnswerModePlannerClarify, Message: "需要补充要执行的工具和参数。", Reason: "semantic plan has no steps", PlannerSource: plan.PlannerSource, SemanticPlan: plan}
		}
		step := plan.Steps[0]
		spec, _ := c.Catalog.Tool(step.Tool)
		purpose := step.Purpose
		if purpose == "" {
			purpose = spec.Description
		}
		proposal := c.Adapter.NewProposal(step.Tool, step.Input, purpose, spec.DefaultEffects)
		return CompiledPlan{
			Decision:      DecisionTool,
			Preamble:      preambleFor(step.Tool),
			Reason:        plan.Reason,
			PlannerSource: plan.PlannerSource,
			ToolProposal:  &proposal,
			SemanticPlan:  plan,
			SelectedStep:  &step,
		}
	default:
		return CompiledPlan{Decision: DecisionAnswer, Reason: "unsupported semantic decision", PlannerSource: plan.PlannerSource, SemanticPlan: plan}
	}
}

func capabilityMessage(plan semantic.SemanticPlan) string {
	base := "当前可用工具不足，无法安全完成这个操作。"
	detail := strings.TrimSpace(plan.CapabilityMessage)
	if detail != "" {
		if strings.Contains(detail, "工具不足") || strings.Contains(strings.ToLower(detail), "insufficient") || strings.Contains(strings.ToLower(detail), "capability") {
			return detail
		}
		return base + " " + detail
	}
	if plan.Reason != "" {
		return base + " " + plan.Reason
	}
	return base
}

func preambleFor(tool string) string {
	switch tool {
	case "code.read_file":
		return "我会先读取相关文件内容。"
	case "code.search_text":
		return "我会先在工作区搜索相关文本。"
	case "code.inspect_project":
		return "我会先检查项目结构、语言和测试命令。"
	case "code.run_tests":
		return "我会先检测并运行项目测试。"
	case "code.fix_test_failure_loop":
		return "我会先运行测试并整理失败上下文，后续修改仍需要 patch 审批。"
	case "git.status":
		return "我会先读取当前 Git 工作区状态。"
	case "git.diff":
		return "我会先读取当前 Git diff。"
	case "git.diff_summary":
		return "我会先生成当前 Git diff 摘要。"
	case "kb.answer":
		return "我会先检索知识库证据，再基于引用回答。"
	case "memory.extract_candidates":
		return "我会先提取候选记忆并放入 review queue；不会直接写入长期记忆正文。"
	}
	return ""
}
