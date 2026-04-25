package fastpath

import (
	"strings"

	"local-agent/internal/agent/planner/intent"
	"local-agent/internal/agent/planner/normalize"
	"local-agent/internal/agent/planner/semantic"
	"local-agent/internal/security"
)

// Input is the deterministic fast-path planning input.
type Input struct {
	ConversationID string
	Request        normalize.NormalizedRequest
	Classification intent.IntentClassification
}

// DeterministicFastPath handles only high-confidence, low-ambiguity requests.
type DeterministicFastPath struct{}

// New returns a deterministic fast path planner.
func New() DeterministicFastPath {
	return DeterministicFastPath{}
}

// Plan returns a SemanticPlan and true when a high-confidence local rule matches.
func (DeterministicFastPath) Plan(input Input) (semantic.SemanticPlan, bool) {
	req := input.Request
	signals := signalSet(req.Signals)

	switch {
	case signals["memory_archive"]:
		return one("memory", "memory.item_archive", "归档指定 memory item", map[string]any{"id": memoryID(req.Original)}, 0.9, "fastpath memory archive"), true
	case signals["memory_extract"]:
		queueReview := !security.ScanText(req.Original).HasSecret
		text := req.Original
		if !queueReview {
			text = "[REDACTED sensitive memory request]"
		}
		return one("memory", "memory.extract_candidates", "提取长期记忆候选并进入 review queue", map[string]any{
			"conversation_id": input.ConversationID,
			"text":            text,
			"project_key":     projectKey(req.Original),
			"queue":           queueReview,
		}, 0.92, "fastpath memory extraction"), true
	case signals["patch_apply"]:
		path := firstQuoted(req)
		return one("code", "code.read_file", "读取 patch 文件用于审批后应用", map[string]any{"path": path, "max_bytes": 500000}, 0.88, "fastpath patch apply read"), true
	case signals["patch_validate"]:
		path := firstQuoted(req)
		return one("code", "code.read_file", "读取 patch 文件用于 dry-run validation", map[string]any{"path": path, "max_bytes": 500000}, 0.88, "fastpath patch validation read"), true
	case signals["install_dependency"]:
		command := "pnpm add axios"
		if strings.Contains(req.NormalizedText, "go") {
			command = "go get github.com/sirupsen/logrus"
		}
		return one("code", "shell.exec", "安装依赖", map[string]any{
			"shell":           "bash",
			"command":         command,
			"cwd":             workspaceOrDot(req.Workspace),
			"timeout_seconds": 60,
			"purpose":         "安装依赖",
		}, 0.88, "fastpath dependency install approval"), true
	case signals["git_status"]:
		return git("git.status", req.Workspace, "查看 git 工作区状态"), true
	case signals["git_diff_summary"]:
		return git("git.diff_summary", req.Workspace, "总结 git diff"), true
	case signals["git_diff"]:
		return git("git.diff", req.Workspace, "查看 git diff"), true
	case signals["git_commit_message"]:
		return git("git.commit_message_proposal", req.Workspace, "生成提交信息建议"), true
	case signals["git_log"]:
		return one("git", "git.log", "查看 git log", map[string]any{"workspace": workspaceOrDot(req.Workspace), "limit": 20}, 0.9, "fastpath git log"), true
	case signals["fix_tests"]:
		return codeFix(req), true
	case signals["run_tests"]:
		return codeRunTests(req), true
	case signals["read_file"] && len(req.PossibleFiles) > 0:
		return one("code", "code.read_file", "读取工作区文件", map[string]any{"path": req.PossibleFiles[0], "max_bytes": 200000}, 0.96, "fastpath code read file"), true
	case signals["search_text"] && firstQuoted(req) != "":
		return one("code", "code.search_text", "搜索工作区代码", map[string]any{"path": workspaceOrDot(req.Workspace), "query": firstQuoted(req), "limit": 50}, 0.96, "fastpath code search text"), true
	case signals["kb_answer"]:
		return kbAnswer(req), true
	case signals["kb_retrieve"]:
		return one("rag", "kb.retrieve", "检索知识库并返回引用证据", map[string]any{"kb_id": req.KBID, "query": req.Original, "mode": "hybrid", "top_k": 5, "rerank": true}, 0.9, "fastpath kb retrieve"), true
	case signals["docker_ops"]:
		return docker(req, signals), true
	case signals["k8s_ops"]:
		return k8s(req, signals), true
	case signals["ssh_ops"]:
		return ssh(req, signals), true
	case signals["local_restart"]:
		return one("ops", "ops.local.service_restart", "重启本地服务", map[string]any{"service": target(req.Original, "service", "unknown")}, 0.92, "fastpath local service restart"), true
	case signals["system_overview"]:
		return one("ops", "ops.local.system_info", "查看本机系统信息", map[string]any{}, 0.96, "fastpath ops system overview"), true
	case signals["disk_usage"]:
		return one("ops", "ops.local.disk_usage", "查看本机磁盘使用情况", map[string]any{}, 0.93, "fastpath ops disk usage"), true
	case signals["memory_usage"]:
		return one("ops", "ops.local.memory_usage", "查看本机内存使用情况", map[string]any{}, 0.92, "fastpath ops memory usage"), true
	case signals["logs"] && !input.Classification.NeedClarify:
		return one("ops", "ops.local.logs_tail", "读取本机日志尾部", map[string]any{"path": logPath(req), "max_lines": 100}, 0.86, "fastpath local logs"), true
	case signals["processes"]:
		return one("ops", "ops.local.processes", "查看本机进程和 CPU 占用", map[string]any{}, 0.9, "fastpath ops processes"), true
	case signals["runbook_ops"]:
		runbookID := firstQuoted(req)
		if runbookID == "" {
			runbookID = "diagnose-local-high-cpu"
		}
		return one("ops", "runbook.plan", "规划运维 runbook", map[string]any{"runbook_id": runbookID, "host_id": "local"}, 0.86, "fastpath runbook plan"), true
	case signals["inspect_project"]:
		return one("code", "code.inspect_project", "检查项目结构和测试命令", map[string]any{"path": workspaceOrDot(req.Workspace)}, 0.82, "fastpath code inspect project"), true
	default:
		return semantic.SemanticPlan{}, false
	}
}

func one(domain, tool, purpose string, input map[string]any, confidence float64, reason string) semantic.SemanticPlan {
	return semantic.SemanticPlan{
		Decision:   semantic.SemanticPlanTool,
		Goal:       purpose,
		Confidence: confidence,
		Domain:     domain,
		Reason:     reason,
		Steps:      []semantic.SemanticPlanStep{{Tool: tool, Purpose: purpose, Input: input}},
	}
}

func git(tool, workspace, purpose string) semantic.SemanticPlan {
	return one("git", tool, purpose, map[string]any{"workspace": workspaceOrDot(workspace)}, 0.95, "fastpath "+tool)
}

func codeRunTests(req normalize.NormalizedRequest) semantic.SemanticPlan {
	return one("code", "code.run_tests", "运行项目测试", map[string]any{
		"workspace":        workspaceOrDot(req.Workspace),
		"use_detected":     true,
		"timeout_seconds":  300,
		"max_output_bytes": 200000,
	}, 0.93, "fastpath code run tests")
}

func codeFix(req normalize.NormalizedRequest) semantic.SemanticPlan {
	return one("code", "code.fix_test_failure_loop", "运行测试并进入有界修复循环", map[string]any{
		"workspace":           workspaceOrDot(req.Workspace),
		"use_detected":        true,
		"max_iterations":      3,
		"stop_on_approval":    true,
		"auto_rerun_tests":    true,
		"failure_context_max": 3,
	}, 0.94, "fastpath code fix tests")
}

func kbAnswer(req normalize.NormalizedRequest) semantic.SemanticPlan {
	text := req.NormalizedText
	mode := "normal"
	requireCitations := false
	if strings.Contains(text, "只根据知识库") || strings.Contains(text, "kb_only") {
		mode = "kb_only"
		requireCitations = true
	}
	if strings.Contains(text, "无引用不回答") || strings.Contains(text, "没有引用不要回答") || strings.Contains(text, "no_citation_no_answer") {
		mode = "no_citation_no_answer"
		requireCitations = true
	}
	if strings.Contains(text, "引用") || strings.Contains(text, "citation") || strings.Contains(text, "来源") {
		requireCitations = true
	}
	return one("rag", "kb.answer", "基于知识库证据回答并返回引用", map[string]any{
		"kb_id":             req.KBID,
		"query":             req.Original,
		"mode":              mode,
		"top_k":             5,
		"require_citations": requireCitations,
		"rerank":            true,
	}, 0.92, "fastpath kb answer")
}
