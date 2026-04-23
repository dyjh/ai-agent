package agent

import (
	"context"
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
		return Plan{
			Decision: PlanDecisionStop,
			Message:  summarizeToolResult(input.LastToolResult),
			Reason:   "heuristic planner stops after one tool result",
		}, nil
	}

	normalized := strings.ToLower(strings.TrimSpace(input.UserMessage))
	switch {
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

func clonePlan(plan Plan) Plan {
	cp := plan
	if plan.ToolProposal != nil {
		proposal := cloneProposal(*plan.ToolProposal)
		cp.ToolProposal = &proposal
	}
	return cp
}
