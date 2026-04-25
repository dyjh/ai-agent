package agent

import (
	"context"
	"fmt"
	"strings"

	"local-agent/internal/core"
	"local-agent/internal/einoapp"
	"local-agent/internal/security"
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
	Decision     PlanDecision       `json:"decision,omitempty"`
	Preamble     string             `json:"preamble,omitempty"`
	Message      string             `json:"message,omitempty"`
	ToolProposal *core.ToolProposal `json:"tool_proposal,omitempty"`
	CodePlan     *CodePlan          `json:"code_plan,omitempty"`
	Reason       string             `json:"reason,omitempty"`
}

// Planner produces either a direct answer or a structured tool proposal.
type Planner interface {
	Plan(ctx context.Context, input PlanInput) (Plan, error)
}

// HeuristicPlanner provides a deterministic MVP planner.
type HeuristicPlanner struct {
	Adapter einoapp.ProposalToolAdapter
}

// Plan turns common local-ops intents into tool proposals and falls back to direct answering.
func (p HeuristicPlanner) Plan(_ context.Context, input PlanInput) (Plan, error) {
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

	normalized := strings.ToLower(strings.TrimSpace(input.UserMessage))
	workspace := extractWorkspace(input.UserMessage)
	switch {
	case wantsMemoryExtract(normalized):
		queueReview := !security.ContainsSensitiveString(input.UserMessage)
		preamble := "我会先提取候选记忆并放入 review queue；不会直接写入长期记忆正文。"
		if !queueReview {
			preamble = "我会先运行记忆候选提取；疑似敏感内容不会写入 review queue 或长期记忆。"
		}
		proposal := p.Adapter.NewProposal("memory.extract_candidates", map[string]any{
			"conversation_id": input.ConversationID,
			"text":            input.UserMessage,
			"project_key":     extractProjectKey(input.UserMessage),
			"queue":           queueReview,
		}, "提取长期记忆候选并进入 review queue", []string{"memory.review.write"})
		return Plan{
			Decision:     PlanDecisionTool,
			Preamble:     preamble,
			ToolProposal: &proposal,
			Reason:       "matched memory extraction intent",
		}, nil
	case wantsMemoryArchive(normalized):
		id := extractQuoted(input.UserMessage)
		if id == "" {
			id = extractPathAfter(input.UserMessage, []string{"memory", "记忆", "item"})
		}
		if id == "" {
			fields := strings.Fields(input.UserMessage)
			if len(fields) > 0 {
				id = strings.Trim(fields[len(fields)-1], "`'\"，,。;；")
			}
		}
		proposal := p.Adapter.NewProposal("memory.item_archive", map[string]any{
			"id": id,
		}, "归档指定 memory item", []string{"fs.write", "memory.modify"})
		return Plan{
			Decision:     PlanDecisionTool,
			Preamble:     "我会把这次忘记/归档请求转换为 memory item 归档操作，并走审批。",
			ToolProposal: &proposal,
			Reason:       "matched memory archive intent",
		}, nil
	case wantsPatchApply(normalized):
		path := extractPathAfter(input.UserMessage, []string{"file", "文件"})
		if path == "" {
			path = extractQuoted(input.UserMessage)
		}
		proposal := p.Adapter.NewProposal("code.read_file", map[string]any{
			"path":      path,
			"max_bytes": 500000,
		}, "读取 patch 文件用于审批后应用", []string{"code.read"})
		return Plan{
			Decision:     PlanDecisionTool,
			Preamble:     "我会先读取 patch 文件内容，再将具体 patch snapshot 送入审批流程。",
			ToolProposal: &proposal,
			CodePlan:     newCodePlan(CodeTaskPatch, workspace, input.UserMessage, []CodePlanStep{{Tool: "code.read_file", Purpose: "读取 patch 文件", Input: proposal.Input}, {Tool: "code.apply_patch", Purpose: "审批后应用 patch", RequiresApproval: true}}),
			Reason:       "matched patch apply intent",
		}, nil
	case wantsPatchValidate(normalized):
		path := extractPathAfter(input.UserMessage, []string{"file", "文件"})
		if path == "" {
			path = extractQuoted(input.UserMessage)
		}
		proposal := p.Adapter.NewProposal("code.read_file", map[string]any{
			"path":      path,
			"max_bytes": 500000,
		}, "读取 patch 文件用于 dry-run validation", []string{"code.read"})
		return Plan{
			Decision:     PlanDecisionTool,
			Preamble:     "我会先读取 patch 文件内容，再做 dry-run 校验。",
			ToolProposal: &proposal,
			CodePlan:     newCodePlan(CodeTaskPatch, workspace, input.UserMessage, []CodePlanStep{{Tool: "code.read_file", Purpose: "读取 patch 文件", Input: proposal.Input}, {Tool: "code.validate_patch", Purpose: "校验 patch 内容"}}),
			Reason:       "matched patch validation intent",
		}, nil
	case wantsGitStatus(normalized):
		proposal := p.Adapter.NewProposal("git.status", map[string]any{
			"workspace": workspace,
		}, "查看 git 工作区状态", []string{"git.read"})
		return Plan{
			Decision:     PlanDecisionTool,
			Preamble:     "我会先读取当前 Git 工作区状态。",
			ToolProposal: &proposal,
			CodePlan:     newCodePlan(CodeTaskGit, workspace, input.UserMessage, []CodePlanStep{{Tool: "git.status", Purpose: "查看 git 工作区状态", Input: proposal.Input}}),
			Reason:       "matched git status intent",
		}, nil
	case wantsGitDiff(normalized):
		proposal := p.Adapter.NewProposal("git.diff", map[string]any{
			"workspace": workspace,
		}, "查看 git diff", []string{"git.read"})
		return Plan{
			Decision:     PlanDecisionTool,
			Preamble:     "我会先读取当前 Git diff。",
			ToolProposal: &proposal,
			CodePlan:     newCodePlan(CodeTaskGit, workspace, input.UserMessage, []CodePlanStep{{Tool: "git.diff", Purpose: "读取 git diff", Input: proposal.Input}}),
			Reason:       "matched git diff intent",
		}, nil
	case wantsGitDiffSummary(normalized):
		proposal := p.Adapter.NewProposal("git.diff_summary", map[string]any{
			"workspace": workspace,
		}, "总结 git diff", []string{"git.read"})
		return Plan{
			Decision:     PlanDecisionTool,
			Preamble:     "我会先生成当前 Git diff 摘要。",
			ToolProposal: &proposal,
			CodePlan:     newCodePlan(CodeTaskGit, workspace, input.UserMessage, []CodePlanStep{{Tool: "git.diff_summary", Purpose: "总结 git diff", Input: proposal.Input}}),
			Reason:       "matched git diff summary intent",
		}, nil
	case wantsGitCommitMessage(normalized):
		proposal := p.Adapter.NewProposal("git.commit_message_proposal", map[string]any{
			"workspace": workspace,
		}, "生成提交信息建议", []string{"git.read"})
		return Plan{
			Decision:     PlanDecisionTool,
			Preamble:     "我会先基于当前 Git 状态和 staged diff 生成提交信息建议。",
			ToolProposal: &proposal,
			CodePlan:     newCodePlan(CodeTaskGit, workspace, input.UserMessage, []CodePlanStep{{Tool: "git.status", Purpose: "检查提交前状态", Input: map[string]any{"workspace": workspace}}, {Tool: "git.diff_summary", Purpose: "总结 staged diff", Input: map[string]any{"workspace": workspace, "staged": true}}, {Tool: "git.commit_message_proposal", Purpose: "生成提交信息建议", Input: proposal.Input}}),
			Reason:       "matched git commit message proposal intent",
		}, nil
	case wantsGitLog(normalized):
		proposal := p.Adapter.NewProposal("git.log", map[string]any{
			"workspace": workspace,
			"limit":     20,
		}, "查看 git log", []string{"git.read"})
		return Plan{
			Decision:     PlanDecisionTool,
			Preamble:     "我会先读取最近的 Git 提交记录。",
			ToolProposal: &proposal,
			CodePlan:     newCodePlan(CodeTaskGit, workspace, input.UserMessage, []CodePlanStep{{Tool: "git.log", Purpose: "读取最近提交记录", Input: proposal.Input}}),
			Reason:       "matched git log intent",
		}, nil
	case wantsFixTests(normalized):
		proposal := p.Adapter.NewProposal("code.fix_test_failure_loop", map[string]any{
			"workspace":           workspace,
			"use_detected":        true,
			"max_iterations":      3,
			"stop_on_approval":    true,
			"auto_rerun_tests":    true,
			"failure_context_max": 3,
		}, "运行测试并进入有界修复循环", []string{"code.test", "process.read", "fs.read", "code.plan"})
		return Plan{
			Decision:     PlanDecisionTool,
			Preamble:     "我会先运行测试并整理失败上下文，后续修改仍需要 patch 审批。",
			ToolProposal: &proposal,
			CodePlan:     codeFixPlan(workspace, input.UserMessage),
			Reason:       "matched code fix loop intent",
		}, nil
	case wantsRunTests(normalized):
		proposal := p.Adapter.NewProposal("code.run_tests", map[string]any{
			"workspace":        workspace,
			"use_detected":     true,
			"timeout_seconds":  300,
			"max_output_bytes": 200000,
		}, "运行项目测试", []string{"code.test", "process.read", "fs.read"})
		return Plan{
			Decision:     PlanDecisionTool,
			Preamble:     "我会先检测并运行项目测试。",
			ToolProposal: &proposal,
			CodePlan:     newCodePlan(CodeTaskTest, workspace, input.UserMessage, []CodePlanStep{{Tool: "code.inspect_project", Purpose: "检测项目结构", Input: map[string]any{"path": workspace}}, {Tool: "code.run_tests", Purpose: "运行测试", Input: proposal.Input}, {Tool: "code.parse_test_failure", Purpose: "如失败则解析测试输出"}}),
			Reason:       "matched code test intent",
		}, nil
	case wantsCodeRead(normalized):
		path := extractPathAfter(input.UserMessage, []string{"read", "读取", "查看"})
		if path == "" {
			path = "."
		}
		path = scopedWorkspacePath(workspace, path)
		proposal := p.Adapter.NewProposal("code.read_file", map[string]any{
			"path":      path,
			"max_bytes": 200000,
		}, "读取工作区文件", []string{"code.read"})
		return Plan{
			Decision:     PlanDecisionTool,
			Preamble:     "我会先读取相关文件内容。",
			ToolProposal: &proposal,
			CodePlan:     newCodePlan(CodeTaskInspect, workspace, input.UserMessage, []CodePlanStep{{Tool: "code.read_file", Purpose: "读取工作区文件", Input: proposal.Input}}),
			Reason:       "matched code read intent",
		}, nil
	case wantsCodeSearch(normalized):
		query := extractQuoted(input.UserMessage)
		if query == "" {
			query = strings.TrimSpace(input.UserMessage)
		}
		proposal := p.Adapter.NewProposal("code.search_text", map[string]any{
			"path":  workspace,
			"query": query,
			"limit": 50,
		}, "搜索工作区代码", []string{"code.read"})
		return Plan{
			Decision:     PlanDecisionTool,
			Preamble:     "我会先在工作区搜索相关代码。",
			ToolProposal: &proposal,
			CodePlan:     newCodePlan(CodeTaskSearch, workspace, input.UserMessage, []CodePlanStep{{Tool: "code.search_text", Purpose: "搜索相关代码", Input: proposal.Input}}),
			Reason:       "matched code search intent",
		}, nil
	case wantsKBAnswer(normalized):
		mode := "normal"
		requireCitations := false
		if strings.Contains(normalized, "只根据知识库") || strings.Contains(normalized, "kb_only") {
			mode = "kb_only"
			requireCitations = true
		}
		if strings.Contains(normalized, "无引用不回答") || strings.Contains(normalized, "没有引用不要回答") || strings.Contains(normalized, "no_citation_no_answer") {
			mode = "no_citation_no_answer"
			requireCitations = true
		}
		if strings.Contains(normalized, "引用") || strings.Contains(normalized, "citation") || strings.Contains(normalized, "来源") {
			requireCitations = true
		}
		proposal := p.Adapter.NewProposal("kb.answer", map[string]any{
			"kb_id":             extractKBID(input.UserMessage),
			"query":             input.UserMessage,
			"mode":              mode,
			"top_k":             5,
			"require_citations": requireCitations,
			"rerank":            true,
		}, "基于知识库证据回答并返回引用", []string{"kb.read"})
		return Plan{
			Decision:     PlanDecisionTool,
			Preamble:     "我会先检索知识库证据，再基于引用回答。",
			ToolProposal: &proposal,
			Reason:       "matched KB answer intent",
		}, nil
	case wantsKBRetrieve(normalized):
		proposal := p.Adapter.NewProposal("kb.retrieve", map[string]any{
			"kb_id":  extractKBID(input.UserMessage),
			"query":  input.UserMessage,
			"mode":   "hybrid",
			"top_k":  5,
			"rerank": true,
		}, "检索知识库并返回引用证据", []string{"kb.read"})
		return Plan{
			Decision:     PlanDecisionTool,
			Preamble:     "我会使用 hybrid retrieval 检索知识库证据。",
			ToolProposal: &proposal,
			Reason:       "matched KB retrieval intent",
		}, nil
	case wantsCodeWork(normalized):
		proposal := p.Adapter.NewProposal("code.inspect_project", map[string]any{
			"path": workspace,
		}, "检查项目结构和测试命令", []string{"code.read"})
		return Plan{
			Decision:     PlanDecisionTool,
			Preamble:     "我会先检查项目结构、语言和测试命令，再决定后续代码工具调用。",
			ToolProposal: &proposal,
			CodePlan:     codeInspectPlan(workspace, input.UserMessage),
			Reason:       "matched code work intent",
		}, nil
	case wantsRunbookOps(normalized):
		runbookID := extractQuoted(input.UserMessage)
		if runbookID == "" {
			runbookID = extractPathAfter(input.UserMessage, []string{"runbook", "排查", "runbooks"})
		}
		if runbookID == "" {
			runbookID = "diagnose-local-high-cpu"
		}
		proposal := p.Adapter.NewProposal("runbook.plan", map[string]any{
			"runbook_id": runbookID,
			"host_id":    "local",
		}, "规划运维 runbook", []string{"runbook.read"})
		return Plan{
			Decision:     PlanDecisionTool,
			Preamble:     "我会先把 runbook 解析为结构化运维步骤，执行时每步仍走工具路由和审批。",
			ToolProposal: &proposal,
			Reason:       "matched runbook ops intent",
		}, nil
	case wantsSSHOps(normalized):
		hostID := extractHostID(input.UserMessage)
		if hostID == "" {
			hostID = "local"
		}
		tool := "ops.ssh.processes"
		inputMap := map[string]any{"host_id": hostID}
		purpose := "查看 SSH 主机进程"
		if strings.Contains(normalized, "日志") || strings.Contains(normalized, "log") {
			tool = "ops.ssh.logs_tail"
			inputMap["path"] = fallbackLogPath(input.UserMessage)
			inputMap["max_lines"] = 100
			purpose = "读取 SSH 主机日志"
		}
		proposal := p.Adapter.NewProposal(tool, inputMap, purpose, nil)
		return Plan{
			Decision:     PlanDecisionTool,
			Preamble:     "我会使用结构化 SSH 运维工具；远程写操作会进入审批。",
			ToolProposal: &proposal,
			Reason:       "matched ssh ops intent",
		}, nil
	case wantsDockerOps(normalized):
		tool := "ops.docker.ps"
		inputMap := map[string]any{}
		purpose := "查看 Docker 容器状态"
		if strings.Contains(normalized, "stats") || strings.Contains(normalized, "资源") {
			tool = "ops.docker.stats"
			purpose = "查看 Docker 容器资源使用情况"
		}
		if strings.Contains(normalized, "log") || strings.Contains(normalized, "日志") {
			tool = "ops.docker.logs"
			inputMap["container"] = fallbackTarget(input.UserMessage, "container")
			inputMap["max_lines"] = 200
			purpose = "读取 Docker 容器日志"
		}
		if strings.Contains(normalized, "restart") || strings.Contains(normalized, "重启") {
			tool = "ops.docker.restart"
			inputMap["container"] = fallbackTarget(input.UserMessage, "container")
			purpose = "重启 Docker 容器"
		}
		proposal := p.Adapter.NewProposal(tool, inputMap, purpose, nil)
		return Plan{
			Decision:     PlanDecisionTool,
			Preamble:     "我会使用结构化 Docker 运维工具；变更容器状态的操作会进入审批。",
			ToolProposal: &proposal,
			Reason:       "matched docker ops intent",
		}, nil
	case wantsK8sOps(normalized):
		tool := "ops.k8s.get"
		inputMap := map[string]any{"resource": extractK8sResource(input.UserMessage, "pods")}
		purpose := "查看 Kubernetes 资源"
		if strings.Contains(normalized, "log") || strings.Contains(normalized, "日志") {
			tool = "ops.k8s.logs"
			inputMap = map[string]any{"target": fallbackTarget(input.UserMessage, "pod"), "max_lines": 200}
			purpose = "读取 Kubernetes Pod 日志"
		}
		if strings.Contains(normalized, "describe") || strings.Contains(normalized, "描述") {
			tool = "ops.k8s.describe"
			inputMap = map[string]any{"resource": "pod", "name": fallbackTarget(input.UserMessage, "pod")}
			purpose = "Describe Kubernetes 资源"
		}
		if strings.Contains(normalized, "apply") {
			tool = "ops.k8s.apply"
			inputMap = map[string]any{"manifest_path": fallbackTarget(input.UserMessage, "manifest.yaml")}
			purpose = "应用 Kubernetes manifest"
		}
		proposal := p.Adapter.NewProposal(tool, inputMap, purpose, nil)
		return Plan{
			Decision:     PlanDecisionTool,
			Preamble:     "我会使用结构化 Kubernetes 运维工具；apply/delete/restart 会进入审批。",
			ToolProposal: &proposal,
			Reason:       "matched k8s ops intent",
		}, nil
	case wantsLocalRestart(normalized):
		service := extractQuoted(input.UserMessage)
		if service == "" {
			service = extractPathAfter(input.UserMessage, []string{"service", "服务"})
		}
		if service == "" {
			service = "unknown"
		}
		proposal := p.Adapter.NewProposal("ops.local.service_restart", map[string]any{
			"service": service,
		}, "重启本地服务", []string{"service.restart", "system.write"})
		return Plan{
			Decision:     PlanDecisionTool,
			Preamble:     "这个运维操作会改变本机服务状态，我会先请求审批并附带 rollback plan。",
			ToolProposal: &proposal,
			Reason:       "matched local service restart intent",
		}, nil
	case wantsLocalOps(normalized):
		tool, purpose, inputMap := localOpsToolFor(input.UserMessage, normalized)
		proposal := p.Adapter.NewProposal(tool, inputMap, purpose, nil)
		return Plan{
			Decision:     PlanDecisionTool,
			Preamble:     "我会使用结构化本地运维只读工具进行排查。",
			ToolProposal: &proposal,
			Reason:       "matched local ops intent",
		}, nil
	case strings.Contains(normalized, "cpu") && strings.Contains(normalized, "进程"):
		proposal := p.Adapter.NewProposal("shell.exec", map[string]any{
			"shell":           "bash",
			"command":         "ps -eo pid,pcpu,comm --sort=-pcpu | head -n 5",
			"cwd":             ".",
			"timeout_seconds": 10,
			"purpose":         "查询 CPU 占用最高的进程",
		}, "查询 CPU 占用最高的进程", []string{"process.read"})
		return Plan{
			Decision:     PlanDecisionTool,
			Preamble:     "我会先查询当前进程的 CPU 使用情况。",
			ToolProposal: &proposal,
			Reason:       "matched CPU process inspection intent",
		}, nil
	case strings.Contains(normalized, "当前目录") || normalized == "pwd":
		proposal := p.Adapter.NewProposal("shell.exec", map[string]any{
			"shell":           "bash",
			"command":         "pwd",
			"cwd":             ".",
			"timeout_seconds": 5,
			"purpose":         "查询当前工作目录",
		}, "查询当前工作目录", []string{"system.read"})
		return Plan{
			Decision:     PlanDecisionTool,
			Preamble:     "我会先查询当前工作目录。",
			ToolProposal: &proposal,
			Reason:       "matched current directory intent",
		}, nil
	case strings.Contains(normalized, "列出") && strings.Contains(normalized, "文件"):
		proposal := p.Adapter.NewProposal("shell.exec", map[string]any{
			"shell":           "bash",
			"command":         "ls -la",
			"cwd":             ".",
			"timeout_seconds": 5,
			"purpose":         "列出当前目录文件",
		}, "列出当前目录文件", []string{"code.read"})
		return Plan{
			Decision:     PlanDecisionTool,
			Preamble:     "我会先读取当前目录内容。",
			ToolProposal: &proposal,
			Reason:       "matched list files intent",
		}, nil
	case strings.Contains(normalized, "安装") || strings.Contains(normalized, "依赖"):
		command := "pnpm add axios"
		if strings.Contains(normalized, "go") {
			command = "go get github.com/sirupsen/logrus"
		}
		proposal := p.Adapter.NewProposal("shell.exec", map[string]any{
			"shell":           "bash",
			"command":         command,
			"cwd":             ".",
			"timeout_seconds": 60,
			"purpose":         "安装依赖",
		}, "安装依赖", []string{"package.install", "fs.write"})
		return Plan{
			Decision:     PlanDecisionTool,
			Preamble:     "这个操作会修改依赖或工作区内容，我会先走审批流程。",
			ToolProposal: &proposal,
			Reason:       "matched install dependency intent",
		}, nil
	default:
		return Plan{
			Decision: PlanDecisionAnswer,
			Reason:   "no deterministic tool match",
		}, nil
	}
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
		if wantsCodeWork(strings.ToLower(strings.TrimSpace(input.UserMessage))) || wantsRunTests(strings.ToLower(strings.TrimSpace(input.UserMessage))) {
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

func wantsCodeWork(normalized string) bool {
	return containsAny(normalized, []string{
		"修 bug", "修复", "实现", "改代码", "修改代码", "代码任务", "看代码", "检查项目",
		"inspect project", "fix bug", "implement", "code", "workspace",
	})
}

func wantsRunTests(normalized string) bool {
	return containsAny(normalized, []string{"跑测试", "运行测试", "测试", "run tests", "go test", "npm test", "pytest", "cargo test", "make test"})
}

func wantsFixTests(normalized string) bool {
	return containsAny(normalized, []string{"修复测试", "修测试", "fix tests", "fix failing test", "fix test failure", "测试失败", "failed tests"})
}

func wantsCodeSearch(normalized string) bool {
	return containsAny(normalized, []string{"搜索代码", "查找代码", "search code", "search_text", "grep"})
}

func wantsCodeRead(normalized string) bool {
	return containsAny(normalized, []string{"读取文件", "read file", "查看文件", "打开文件"})
}

func wantsKBAnswer(normalized string) bool {
	return containsAny(normalized, []string{
		"只根据知识库", "知识库回答", "基于知识库", "引用来源", "给出引用", "citation", "citations",
		"kb_only", "no_citation_no_answer", "无引用不回答", "没有引用不要回答",
	})
}

func wantsKBRetrieve(normalized string) bool {
	return containsAny(normalized, []string{"检索知识库", "搜索知识库", "knowledge base search", "kb.retrieve", "hybrid retrieval"})
}

func wantsMemoryExtract(normalized string) bool {
	return containsAny(normalized, []string{"记住", "记一下", "以后", "remember", "always", "never"}) &&
		!wantsMemoryArchive(normalized)
}

func wantsMemoryArchive(normalized string) bool {
	return containsAny(normalized, []string{"忘记", "删除这条记忆", "归档记忆", "archive memory", "forget this memory"})
}

func wantsPatchValidate(normalized string) bool {
	return containsAny(normalized, []string{"validate patch", "patch validate", "验证 patch", "校验 patch", "dry-run patch"})
}

func wantsPatchApply(normalized string) bool {
	return containsAny(normalized, []string{"apply patch", "patch apply", "应用 patch"})
}

func wantsGitStatus(normalized string) bool {
	return strings.Contains(normalized, "git status") || strings.Contains(normalized, "工作区状态")
}

func wantsGitDiff(normalized string) bool {
	if wantsGitDiffSummary(normalized) {
		return false
	}
	return strings.Contains(normalized, "git diff") || strings.Contains(normalized, "查看 diff") || strings.Contains(normalized, "代码 diff")
}

func wantsGitDiffSummary(normalized string) bool {
	return strings.Contains(normalized, "git diff summary") || strings.Contains(normalized, "diff summary") || strings.Contains(normalized, "diff 摘要") || strings.Contains(normalized, "总结 diff")
}

func wantsGitCommitMessage(normalized string) bool {
	return strings.Contains(normalized, "commit message") || strings.Contains(normalized, "提交信息") || strings.Contains(normalized, "commit-message")
}

func wantsGitLog(normalized string) bool {
	return strings.Contains(normalized, "git log") || strings.Contains(normalized, "提交记录")
}

func wantsLocalOps(normalized string) bool {
	return containsAny(normalized, []string{
		"本机 cpu", "cpu 占用", "cpu占用", "磁盘空间", "disk usage", "内存", "memory usage",
		"系统信息", "网络信息", "最近日志", "local ops",
	})
}

func wantsLocalRestart(normalized string) bool {
	return (strings.Contains(normalized, "重启") || strings.Contains(normalized, "restart")) &&
		(strings.Contains(normalized, "服务") || strings.Contains(normalized, "service"))
}

func wantsDockerOps(normalized string) bool {
	return strings.Contains(normalized, "docker")
}

func wantsK8sOps(normalized string) bool {
	return strings.Contains(normalized, "k8s") || strings.Contains(normalized, "kubernetes") || strings.Contains(normalized, "kubectl")
}

func wantsRunbookOps(normalized string) bool {
	return strings.Contains(normalized, "runbook") || strings.Contains(normalized, "运行手册") || strings.Contains(normalized, "按 runbook")
}

func wantsRunbookExecution(normalized string) bool {
	return strings.Contains(normalized, "执行") || strings.Contains(normalized, "execute") || strings.Contains(normalized, "排查") || strings.Contains(normalized, "diagnose") || strings.Contains(normalized, "按 runbook")
}

func wantsSSHOps(normalized string) bool {
	return strings.Contains(normalized, "ssh")
}

func localOpsToolFor(message, normalized string) (string, string, map[string]any) {
	switch {
	case strings.Contains(normalized, "磁盘") || strings.Contains(normalized, "disk"):
		return "ops.local.disk_usage", "查看本机磁盘使用情况", map[string]any{}
	case strings.Contains(normalized, "内存") || strings.Contains(normalized, "memory"):
		return "ops.local.memory_usage", "查看本机内存使用情况", map[string]any{}
	case strings.Contains(normalized, "网络") || strings.Contains(normalized, "network"):
		return "ops.local.network_info", "查看本机网络接口信息", map[string]any{}
	case strings.Contains(normalized, "日志") || strings.Contains(normalized, "log"):
		path := fallbackLogPath(message)
		return "ops.local.logs_tail", "读取本机日志尾部", map[string]any{"path": path, "max_lines": 100}
	case strings.Contains(normalized, "系统") || strings.Contains(normalized, "system"):
		return "ops.local.system_info", "查看本机系统信息", map[string]any{}
	default:
		return "ops.local.processes", "查看本机进程和 CPU 占用", map[string]any{}
	}
}

func fallbackLogPath(message string) string {
	path := extractQuoted(message)
	if path == "" {
		path = "/var/log/syslog"
	}
	return path
}

func fallbackTarget(message, fallback string) string {
	if quoted := extractQuoted(message); quoted != "" {
		return quoted
	}
	fields := strings.Fields(message)
	for idx, field := range fields {
		lower := strings.ToLower(strings.Trim(field, "`'\"，,。;；"))
		if lower == "container" || lower == "容器" || lower == "pod" || lower == "pods" || lower == "service" || lower == "服务" {
			if idx+1 < len(fields) {
				return strings.Trim(fields[idx+1], "`'\"，,。;；")
			}
		}
	}
	return fallback
}

func extractHostID(message string) string {
	fields := strings.Fields(message)
	for idx, field := range fields {
		lower := strings.ToLower(strings.Trim(field, "`'\"，,。;；"))
		if lower == "host" || lower == "host_id" || lower == "主机" {
			if idx+1 < len(fields) {
				return strings.Trim(fields[idx+1], "`'\"，,。;；")
			}
		}
	}
	return extractQuoted(message)
}

func extractK8sResource(message, fallback string) string {
	fields := strings.Fields(message)
	for idx, field := range fields {
		lower := strings.ToLower(strings.Trim(field, "`'\"，,。;；"))
		if lower == "get" && idx+1 < len(fields) {
			return strings.Trim(fields[idx+1], "`'\"，,。;；")
		}
	}
	return fallback
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

func extractKBID(value string) string {
	fields := strings.Fields(value)
	for idx, field := range fields {
		lower := strings.ToLower(strings.Trim(field, "`'\"，,。;；:"))
		if (lower == "kb" || lower == "kb_id" || lower == "kbid" || lower == "知识库") && idx+1 < len(fields) {
			return strings.Trim(fields[idx+1], "`'\"，,。;；")
		}
		if strings.HasPrefix(lower, "kb_id=") {
			return strings.TrimPrefix(lower, "kb_id=")
		}
		if strings.HasPrefix(lower, "kb_id:") {
			return strings.TrimPrefix(lower, "kb_id:")
		}
	}
	return ""
}

func extractProjectKey(value string) string {
	for _, marker := range []string{"project:", "project：", "project_key:", "project_key：", "项目:", "项目："} {
		idx := strings.Index(strings.ToLower(value), strings.ToLower(marker))
		if idx < 0 {
			continue
		}
		rest := strings.TrimSpace(value[idx+len(marker):])
		fields := strings.Fields(rest)
		if len(fields) > 0 {
			return strings.Trim(fields[0], "`'\"，,。;；")
		}
	}
	return ""
}

func scopedWorkspacePath(workspace, path string) string {
	workspace = strings.TrimSpace(workspace)
	path = strings.TrimSpace(path)
	if path == "" {
		path = "."
	}
	if workspace == "" || workspace == "." || strings.HasPrefix(path, "/") {
		return path
	}
	if path == "." {
		return workspace
	}
	return strings.TrimRight(workspace, "/") + "/" + strings.TrimLeft(strings.TrimPrefix(path, "./"), "/")
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
