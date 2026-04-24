package agent

import (
	"context"
	"fmt"
	"strings"

	"local-agent/internal/core"
	"local-agent/internal/einoapp"
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
	switch {
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
			Reason:       "matched patch validation intent",
		}, nil
	case wantsGitStatus(normalized):
		proposal := p.Adapter.NewProposal("git.status", map[string]any{
			"workspace": ".",
		}, "查看 git 工作区状态", []string{"git.read"})
		return Plan{
			Decision:     PlanDecisionTool,
			Preamble:     "我会先读取当前 Git 工作区状态。",
			ToolProposal: &proposal,
			Reason:       "matched git status intent",
		}, nil
	case wantsGitDiff(normalized):
		proposal := p.Adapter.NewProposal("git.diff", map[string]any{
			"workspace": ".",
		}, "查看 git diff", []string{"git.read"})
		return Plan{
			Decision:     PlanDecisionTool,
			Preamble:     "我会先读取当前 Git diff。",
			ToolProposal: &proposal,
			Reason:       "matched git diff intent",
		}, nil
	case wantsGitLog(normalized):
		proposal := p.Adapter.NewProposal("git.log", map[string]any{
			"workspace": ".",
			"limit":     20,
		}, "查看 git log", []string{"git.read"})
		return Plan{
			Decision:     PlanDecisionTool,
			Preamble:     "我会先读取最近的 Git 提交记录。",
			ToolProposal: &proposal,
			Reason:       "matched git log intent",
		}, nil
	case wantsRunTests(normalized):
		proposal := p.Adapter.NewProposal("code.run_tests", map[string]any{
			"workspace":        ".",
			"use_detected":     true,
			"timeout_seconds":  300,
			"max_output_bytes": 200000,
		}, "运行项目测试", []string{"code.test", "process.read", "fs.read"})
		return Plan{
			Decision:     PlanDecisionTool,
			Preamble:     "我会先检测并运行项目测试。",
			ToolProposal: &proposal,
			Reason:       "matched code test intent",
		}, nil
	case wantsCodeRead(normalized):
		path := extractPathAfter(input.UserMessage, []string{"read", "读取", "查看"})
		if path == "" {
			path = "."
		}
		proposal := p.Adapter.NewProposal("code.read_file", map[string]any{
			"path":      path,
			"max_bytes": 200000,
		}, "读取工作区文件", []string{"code.read"})
		return Plan{
			Decision:     PlanDecisionTool,
			Preamble:     "我会先读取相关文件内容。",
			ToolProposal: &proposal,
			Reason:       "matched code read intent",
		}, nil
	case wantsCodeSearch(normalized):
		query := extractQuoted(input.UserMessage)
		if query == "" {
			query = strings.TrimSpace(input.UserMessage)
		}
		proposal := p.Adapter.NewProposal("code.search_text", map[string]any{
			"path":  ".",
			"query": query,
			"limit": 50,
		}, "搜索工作区代码", []string{"code.read"})
		return Plan{
			Decision:     PlanDecisionTool,
			Preamble:     "我会先在工作区搜索相关代码。",
			ToolProposal: &proposal,
			Reason:       "matched code search intent",
		}, nil
	case wantsCodeWork(normalized):
		proposal := p.Adapter.NewProposal("code.inspect_project", map[string]any{
			"path": ".",
		}, "检查项目结构和测试命令", []string{"code.read"})
		return Plan{
			Decision:     PlanDecisionTool,
			Preamble:     "我会先检查项目结构、语言和测试命令，再决定后续代码工具调用。",
			ToolProposal: &proposal,
			Reason:       "matched code work intent",
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
	switch input.LastProposal.Tool {
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
			"workspace": ".",
			"command":   fmtString(input.LastToolResult.Output["command"]),
			"stdout":    fmtString(input.LastToolResult.Output["stdout"]),
			"stderr":    fmtString(input.LastToolResult.Output["stderr"]),
			"exit_code": input.LastToolResult.Output["exit_code"],
		}, "解析测试失败输出", []string{"code.read"})
		return Plan{
			Decision:     PlanDecisionTool,
			Preamble:     "测试没有通过，我会解析失败信息。",
			ToolProposal: &proposal,
			Reason:       "parse failed test output",
		}, true
	case "code.parse_test_failure":
		return Plan{
			Decision: PlanDecisionStop,
			Message:  summarizeParsedFailure(input.LastToolResult),
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
				Reason:       "validate patch after reading patch file",
			}, true
		}
	case "code.inspect_project":
		normalized := strings.ToLower(strings.TrimSpace(input.UserMessage))
		if wantsRunTests(normalized) {
			proposal := p.Adapter.NewProposal("code.run_tests", map[string]any{
				"workspace":        ".",
				"use_detected":     true,
				"timeout_seconds":  300,
				"max_output_bytes": 200000,
			}, "运行项目测试", []string{"code.test", "process.read", "fs.read"})
			return Plan{
				Decision:     PlanDecisionTool,
				Preamble:     "项目检查完成，我会继续运行测试。",
				ToolProposal: &proposal,
				Reason:       "continue from project inspection to tests",
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
	return cp
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

func wantsCodeSearch(normalized string) bool {
	return containsAny(normalized, []string{"搜索代码", "查找代码", "search code", "search_text", "grep"})
}

func wantsCodeRead(normalized string) bool {
	return containsAny(normalized, []string{"读取文件", "read file", "查看文件", "打开文件"})
}

func wantsPatchValidate(normalized string) bool {
	return containsAny(normalized, []string{"validate patch", "patch validate", "验证 patch", "校验 patch", "dry-run patch"})
}

func wantsGitStatus(normalized string) bool {
	return strings.Contains(normalized, "git status") || strings.Contains(normalized, "工作区状态")
}

func wantsGitDiff(normalized string) bool {
	return strings.Contains(normalized, "git diff") || strings.Contains(normalized, "查看 diff") || strings.Contains(normalized, "代码 diff")
}

func wantsGitLog(normalized string) bool {
	return strings.Contains(normalized, "git log") || strings.Contains(normalized, "提交记录")
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
