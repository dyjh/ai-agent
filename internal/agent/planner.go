package agent

import (
	"context"
	"strings"

	"local-agent/internal/core"
	"local-agent/internal/einoapp"
)

// Plan is the planner output before routing.
type Plan struct {
	Preamble     string
	DirectAnswer string
	ToolProposal *core.ToolProposal
}

// Planner produces either a direct answer or a structured tool proposal.
type Planner interface {
	Plan(ctx context.Context, userMessage string) (Plan, error)
}

// HeuristicPlanner provides a deterministic MVP planner.
type HeuristicPlanner struct {
	Adapter einoapp.ProposalToolAdapter
}

// Plan turns common local-ops intents into tool proposals and falls back to direct answering.
func (p HeuristicPlanner) Plan(_ context.Context, userMessage string) (Plan, error) {
	normalized := strings.ToLower(strings.TrimSpace(userMessage))
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
			Preamble:     "我会先查询当前进程的 CPU 使用情况。",
			ToolProposal: &proposal,
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
			Preamble:     "我会先查询当前工作目录。",
			ToolProposal: &proposal,
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
			Preamble:     "我会先读取当前目录内容。",
			ToolProposal: &proposal,
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
			Preamble:     "这个操作会修改依赖或工作区内容，我会先走审批流程。",
			ToolProposal: &proposal,
		}, nil
	default:
		return Plan{}, nil
	}
}
