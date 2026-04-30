package agent

import (
	"context"
	"fmt"
	"strings"

	"local-agent/internal/core"
	"local-agent/internal/einoapp"
	"local-agent/internal/tools/ops"
)

// PlanDecision is the planner outcome for one planning pass.
type PlanDecision string

const (
	PlanDecisionAnswer   PlanDecision = "answer"
	PlanDecisionTool     PlanDecision = "tool"
	PlanDecisionContinue PlanDecision = "continue"
	PlanDecisionStop     PlanDecision = "stop"
)

// PlanInput is the normalized planning context for one workflow iteration.
type PlanInput struct {
	ConversationID string
	UserMessage    string
	StepIndex      int
	LastToolResult *core.ToolResult
	LastProposal   *core.ToolProposal
}

// Plan is the planner output before routing.
type Plan struct {
	Decision       PlanDecision       `json:"decision,omitempty"`
	Preamble       string             `json:"preamble,omitempty"`
	Message        string             `json:"message,omitempty"`
	ToolProposal   *core.ToolProposal `json:"tool_proposal,omitempty"`
	CodePlan       *CodePlan          `json:"code_plan,omitempty"`
	Reason         string             `json:"reason,omitempty"`
	PlannerSource  string             `json:"planner_source,omitempty"`
	CandidateCount int                `json:"candidate_count,omitempty"`
}

// Planner produces either a direct answer or a structured tool proposal.
type Planner interface {
	Plan(ctx context.Context, input PlanInput) (Plan, error)
}

// HeuristicPlanner provides a deterministic MVP planner.
type HeuristicPlanner struct {
	Adapter einoapp.ProposalToolAdapter
}

// Plan delegates first-pass planning to Planner V2 while keeping legacy
// after-tool continuation behavior for existing workflows.
func (p HeuristicPlanner) Plan(ctx context.Context, input PlanInput) (Plan, error) {
	if input.LastToolResult != nil {
		if next, ok := p.planAfterTool(input); ok {
			return next, nil
		}
		return Plan{
			Decision: PlanDecisionStop,
			Message:  summarizeToolResult(input.LastToolResult),
			Reason:   "heuristic planner stops after one tool result",
		}, nil
	}

	return (HybridPlanner{Adapter: p.Adapter}).Plan(ctx, input)
}

func (p HeuristicPlanner) planAfterTool(input PlanInput) (Plan, bool) {
	if input.LastProposal == nil || input.LastToolResult == nil {
		return Plan{}, false
	}
	workspace := extractWorkspace(input.UserMessage)
	switch input.LastProposal.Tool {
	case "runbook.plan":
		if !wantsRunbookExecution(strings.ToLower(strings.TrimSpace(input.UserMessage))) {
			return Plan{
				Decision: PlanDecisionStop,
				Message:  summarizeToolResult(input.LastToolResult),
				Reason:   "runbook plan requested without execution intent",
			}, true
		}
		step, ok := firstExecutableRunbookStep(input.LastToolResult)
		if !ok {
			return Plan{
				Decision: PlanDecisionStop,
				Message:  "Runbook plan 已生成，但没有可自动映射的执行步骤。",
				Reason:   "runbook plan has no executable step",
			}, true
		}
		proposal := p.Adapter.NewProposal(step.Tool, step.Input, "执行 runbook step: "+step.Text, nil)
		return Plan{
			Decision:     PlanDecisionTool,
			Preamble:     "Runbook 已规划，我会把下一步转换为结构化 Ops 工具调用。",
			ToolProposal: &proposal,
			Reason:       "execute first runbook step through workflow",
		}, true
	case "code.run_tests":
		passed, _ := input.LastToolResult.Output["passed"].(bool)
		if passed {
			return Plan{
				Decision: PlanDecisionStop,
				Message:  "测试已通过。",
				Reason:   "code tests passed",
			}, true
		}
		proposal := p.Adapter.NewProposal("code.parse_test_failure", map[string]any{
			"workspace": workspace,
			"command":   fmtString(input.LastToolResult.Output["command"]),
			"stdout":    fmtString(input.LastToolResult.Output["stdout"]),
			"stderr":    fmtString(input.LastToolResult.Output["stderr"]),
			"exit_code": input.LastToolResult.Output["exit_code"],
		}, "解析测试失败输出", []string{"code.read"})
		return Plan{
			Decision:     PlanDecisionTool,
			Preamble:     "测试没有通过，我会解析失败信息。",
			ToolProposal: &proposal,
			CodePlan:     codeTestPlan(workspace, input.UserMessage),
			Reason:       "parse failed test output",
		}, true
	case "code.fix_test_failure_loop":
		if finalPassed, _ := input.LastToolResult.Output["final_passed"].(bool); finalPassed {
			return Plan{
				Decision: PlanDecisionStop,
				Message:  "测试已通过，不需要生成修复 patch。",
				CodePlan: codeFixPlan(workspace, input.UserMessage),
				Reason:   "fix loop tests passed",
			}, true
		}
		return Plan{
			Decision: PlanDecisionStop,
			Message:  summarizeFixLoop(input.LastToolResult),
			CodePlan: codeFixPlan(workspace, input.UserMessage),
			Reason:   "fix loop waiting for patch proposal",
		}, true
	case "code.parse_test_failure":
		return Plan{
			Decision: PlanDecisionStop,
			Message:  summarizeParsedFailure(input.LastToolResult),
			CodePlan: codeTestPlan(workspace, input.UserMessage),
			Reason:   "parsed test failure",
		}, true
	case "code.read_file":
		normalized := strings.ToLower(strings.TrimSpace(input.UserMessage))
		if wantsPatchValidate(normalized) {
			proposal := p.Adapter.NewProposal("code.validate_patch", map[string]any{
				"diff": input.LastToolResult.Output["content"],
			}, "验证 patch 文件内容", []string{"code.plan"})
			return Plan{
				Decision:     PlanDecisionTool,
				Preamble:     "patch 文件已读取，我会继续校验是否能干净应用。",
				ToolProposal: &proposal,
				CodePlan:     newCodePlan(CodeTaskPatch, workspace, input.UserMessage, []CodePlanStep{{Tool: "code.read_file", Purpose: "读取 patch 文件"}, {Tool: "code.validate_patch", Purpose: "验证 patch 内容", Input: proposal.Input}}),
				Reason:       "validate patch after reading patch file",
			}, true
		}
		if wantsPatchApply(normalized) {
			proposal := p.Adapter.NewProposal("code.apply_patch", map[string]any{
				"diff": input.LastToolResult.Output["content"],
			}, "应用 patch 文件内容", []string{"fs.write", "code.modify"})
			return Plan{
				Decision:     PlanDecisionTool,
				Preamble:     "patch 文件已读取，我会请求审批后应用这个 snapshot。",
				ToolProposal: &proposal,
				CodePlan:     newCodePlan(CodeTaskPatch, workspace, input.UserMessage, []CodePlanStep{{Tool: "code.read_file", Purpose: "读取 patch 文件"}, {Tool: "code.apply_patch", Purpose: "审批后应用 patch", Input: proposal.Input, RequiresApproval: true}}),
				Reason:       "apply patch after reading patch file",
			}, true
		}
	case "code.inspect_project":
		normalized := strings.ToLower(strings.TrimSpace(input.UserMessage))
		if wantsRunTests(normalized) {
			proposal := p.Adapter.NewProposal("code.run_tests", map[string]any{
				"workspace":        workspace,
				"use_detected":     true,
				"timeout_seconds":  300,
				"max_output_bytes": 200000,
			}, "运行项目测试", []string{"code.test", "process.read", "fs.read"})
			return Plan{
				Decision:     PlanDecisionTool,
				Preamble:     "项目检查完成，我会继续运行测试。",
				ToolProposal: &proposal,
				CodePlan:     codeTestPlan(workspace, input.UserMessage),
				Reason:       "continue from project inspection to tests",
			}, true
		}
	case "code.apply_patch":
		if workspace != "." || wantsRunTests(strings.ToLower(strings.TrimSpace(input.UserMessage))) {
			proposal := p.Adapter.NewProposal("code.run_tests", map[string]any{
				"workspace":        workspace,
				"use_detected":     true,
				"timeout_seconds":  300,
				"max_output_bytes": 200000,
			}, "patch 应用后重新运行测试", []string{"code.test", "process.read", "fs.read"})
			return Plan{
				Decision:     PlanDecisionTool,
				Preamble:     "patch 已应用，我会继续重新运行测试。",
				ToolProposal: &proposal,
				CodePlan:     codeFixPlan(workspace, input.UserMessage),
				Reason:       "rerun tests after approved patch application",
			}, true
		}
	}
	return Plan{}, false
}

func clonePlan(plan Plan) Plan {
	cp := plan
	if plan.ToolProposal != nil {
		proposal := cloneProposal(*plan.ToolProposal)
		cp.ToolProposal = &proposal
	}
	cp.CodePlan = cloneCodePlan(plan.CodePlan)
	return cp
}

func newCodePlan(kind CodeTaskKind, workspace, goal string, steps []CodePlanStep) *CodePlan {
	plan := &CodePlan{
		Kind:      kind,
		Workspace: workspace,
		Goal:      strings.TrimSpace(goal),
		Steps:     make([]CodePlanStep, 0, len(steps)),
	}
	for _, step := range steps {
		cp := step
		cp.Input = core.CloneMap(step.Input)
		cp.RequiresApproval = codeStepRequiresApproval(step.Tool)
		if cp.Purpose == "" {
			cp.Purpose = step.Tool
		}
		if cp.RequiresApproval {
			plan.RequiresApproval = true
		}
		plan.Steps = append(plan.Steps, cp)
	}
	return plan
}

func codeInspectPlan(workspace, goal string) *CodePlan {
	return newCodePlan(CodeTaskInspect, workspace, goal, []CodePlanStep{
		{Tool: "code.inspect_project", Purpose: "检查项目语言、配置和测试命令", Input: map[string]any{"path": workspace}},
		{Tool: "code.search_text", Purpose: "按需求搜索相关代码"},
		{Tool: "code.read_file", Purpose: "读取相关文件"},
	})
}

func codeTestPlan(workspace, goal string) *CodePlan {
	return newCodePlan(CodeTaskTest, workspace, goal, []CodePlanStep{
		{Tool: "code.inspect_project", Purpose: "检查项目结构"},
		{Tool: "code.run_tests", Purpose: "运行 allowlisted 测试命令", Input: map[string]any{"workspace": workspace, "use_detected": true}},
		{Tool: "code.parse_test_failure", Purpose: "解析失败输出"},
	})
}

func codeFixPlan(workspace, goal string) *CodePlan {
	plan := newCodePlan(CodeTaskFix, workspace, goal, []CodePlanStep{
		{Tool: "code.fix_test_failure_loop", Purpose: "运行测试并整理失败上下文", Input: map[string]any{"workspace": workspace, "max_iterations": 3, "stop_on_approval": true}},
		{Tool: "code.propose_patch", Purpose: "由 planner/model 生成 patch proposal"},
		{Tool: "code.apply_patch", Purpose: "审批后应用具体 patch snapshot"},
		{Tool: "code.run_tests", Purpose: "patch 应用后重新运行测试", Input: map[string]any{"workspace": workspace, "use_detected": true}},
		{Tool: "code.parse_test_failure", Purpose: "如果仍失败，解析下一轮失败输出"},
	})
	plan.MaxIterations = 3
	plan.Iteration = 1
	return plan
}

func codeStepRequiresApproval(tool string) bool {
	switch tool {
	case "code.apply_patch", "git.add", "git.commit", "git.restore", "git.clean":
		return true
	default:
		return false
	}
}

func wantsRunTests(normalized string) bool {
	return containsAny(normalized, []string{"跑测试", "运行测试", "测试", "run tests", "go test", "npm test", "pytest", "cargo test", "make test"})
}

func wantsPatchValidate(normalized string) bool {
	return containsAny(normalized, []string{"validate patch", "patch validate", "验证 patch", "校验 patch", "dry-run patch"})
}

func wantsPatchApply(normalized string) bool {
	return containsAny(normalized, []string{"apply patch", "patch apply", "应用 patch"})
}

func wantsRunbookExecution(normalized string) bool {
	return strings.Contains(normalized, "执行") || strings.Contains(normalized, "execute") || strings.Contains(normalized, "排查") || strings.Contains(normalized, "diagnose") || strings.Contains(normalized, "按 runbook")
}

func firstExecutableRunbookStep(result *core.ToolResult) (ops.RunbookPlanStep, bool) {
	if result == nil {
		return ops.RunbookPlanStep{}, false
	}
	switch plan := result.Output["plan"].(type) {
	case ops.RunbookPlan:
		for _, step := range plan.Steps {
			if step.Tool != "" {
				return step, true
			}
		}
	case map[string]any:
		steps, _ := plan["steps"].([]any)
		for _, item := range steps {
			raw, ok := item.(map[string]any)
			if !ok {
				continue
			}
			tool, _ := raw["tool"].(string)
			if tool == "" {
				continue
			}
			input, _ := raw["input"].(map[string]any)
			text, _ := raw["text"].(string)
			return ops.RunbookPlanStep{
				Index: toolsInt(raw["index"]),
				Text:  text,
				Tool:  tool,
				Input: core.CloneMap(input),
			}, true
		}
	}
	return ops.RunbookPlanStep{}, false
}

func toolsInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func containsAny(value string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func extractQuoted(value string) string {
	for _, quote := range []string{"\"", "'", "`"} {
		start := strings.Index(value, quote)
		if start < 0 {
			continue
		}
		rest := value[start+len(quote):]
		end := strings.Index(rest, quote)
		if end >= 0 {
			return strings.TrimSpace(rest[:end])
		}
	}
	return ""
}

func extractPathAfter(value string, markers []string) string {
	fields := strings.Fields(value)
	for idx, field := range fields {
		lower := strings.ToLower(field)
		for _, marker := range markers {
			if lower == strings.ToLower(marker) && idx+1 < len(fields) {
				return strings.Trim(fields[idx+1], "`'\"")
			}
		}
	}
	return extractQuoted(value)
}

func extractWorkspace(value string) string {
	for _, marker := range []string{"workspace:", "workspace：", "工作区:", "工作区："} {
		idx := strings.Index(strings.ToLower(value), strings.ToLower(marker))
		if idx < 0 {
			continue
		}
		rest := strings.TrimSpace(value[idx+len(marker):])
		if rest == "" {
			break
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			break
		}
		workspace := strings.Trim(fields[0], "`'\"，,。;；")
		if workspace != "" {
			return workspace
		}
	}
	return "."
}

func fmtString(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(strings.Trim(fmt.Sprint(value), "\x00"))
}

func summarizeParsedFailure(result *core.ToolResult) string {
	if result == nil {
		return "测试失败解析完成。"
	}
	if summary, ok := result.Output["summary"].(string); ok && summary != "" {
		return "测试失败解析完成：" + summary
	}
	return "测试失败解析完成。"
}

func summarizeFixLoop(result *core.ToolResult) string {
	if result == nil {
		return "测试修复循环已停止：需要生成 patch proposal 并在审批后应用。"
	}
	if summary, ok := result.Output["summary"].(string); ok && summary != "" {
		return summary
	}
	if reason, ok := result.Output["stopped_reason"].(string); ok && reason != "" {
		return "测试修复循环已停止：" + reason
	}
	return "测试修复循环已停止：需要生成 patch proposal 并在审批后应用。"
}
