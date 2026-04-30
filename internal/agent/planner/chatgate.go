package planner

import (
	"strings"

	"local-agent/internal/agent/planner/intent"
	"local-agent/internal/agent/planner/normalize"
)

type chatGateDecision string

const (
	chatGateDirectAnswer chatGateDecision = "direct_answer"
	chatGateMaybeTool    chatGateDecision = "maybe_tool"
	chatGateClarify      chatGateDecision = "clarify"
)

func chatGate(cls intent.IntentClassification, req normalize.NormalizedRequest) chatGateDecision {
	text := strings.TrimSpace(req.Original)
	if text == "" {
		return chatGateClarify
	}
	if cls.NeedTool || req.ExplicitToolID != "" {
		return chatGateMaybeTool
	}
	if hasStructuralToolScope(req) || looksExternalAction(text) || looksLikeShellRequest(text) {
		return chatGateMaybeTool
	}
	return chatGateDirectAnswer
}

func hasStructuralToolScope(req normalize.NormalizedRequest) bool {
	return req.Workspace != "" ||
		len(req.PossibleFiles) > 0 ||
		req.HostID != "" ||
		req.KBID != "" ||
		req.RunID != "" ||
		req.ApprovalID != "" ||
		len(req.URLs) > 0
}

func looksExternalAction(text string) bool {
	lower := strings.ToLower(text)
	if containsAny(lower, []string{"记住", "忘记"}) ||
		(containsAny(lower, []string{"以后", "偏好", "喜欢"}) && containsAny(lower, []string{"回答", "使用", "用中文", "语言"})) {
		return true
	}
	if containsAny(lower, []string{"知识库", "kb"}) && containsAny(lower, []string{"回答", "引用", "来源", "检索", "根据"}) {
		return true
	}
	actionPhrases := []string{
		"看", "查看", "读取", "打开", "搜索", "定位", "获取", "检查", "确认", "列出", "运行", "执行", "测试", "检测", "跑", "安装", "重启", "删除", "修改", "修复", "实现", "应用",
		"read", "open", "search", "find", "get", "inspect", "check", "list", "run", "execute", "test", "install", "restart", "delete", "modify", "fix", "implement", "apply",
	}
	resourcePhrases := []string{
		"本地", "本机", "机器", "系统", "进程", "cpu", "占用", "磁盘", "内存", "日志", "文件", "目录", "路径", "仓库", "项目", "代码", "服务", "主机", "知识库", "审批", "容器", "pod", "bug", "测试", "失败", "功能", "patch", "diff",
		"workspace", "tool_id", "approval_id", "run_id", "host_id", "kb_id", "docker", "k8s", "kubectl", "git", "mcp", "url",
		"local", "system", "process", "cpu", "disk", "memory", "log", "file", "directory", "path", "repo", "repository", "code", "service", "host", "knowledge base", "approval", "container", "bug", "test", "failure", "feature",
	}
	return containsAny(lower, actionPhrases) && containsAny(lower, resourcePhrases)
}

func looksLikeShellRequest(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if strings.Contains(lower, "shell") || strings.Contains(lower, "bash") || strings.Contains(lower, "命令") || strings.Contains(lower, "终端") {
		return true
	}
	fields := strings.Fields(lower)
	if len(fields) == 0 {
		return false
	}
	switch fields[0] {
	case "cat", "ls", "pwd", "ps", "grep", "rg", "sed", "head", "tail", "docker", "kubectl":
		return true
	case "go":
		return len(fields) > 1 && containsAny(fields[1], []string{"test", "run", "build", "env", "mod", "get", "install", "list", "version", "fmt", "vet"})
	case "npm", "pnpm", "yarn":
		return len(fields) > 1 && !looksLikeLanguageQuestionToken(fields[1])
	case "python":
		return len(fields) > 1 && (strings.HasPrefix(fields[1], "-") || strings.HasSuffix(fields[1], ".py"))
	default:
		return false
	}
}

func looksLikeLanguageQuestionToken(value string) bool {
	return value == "的" || value == "语言" || value == "是什么" || value == "和" || strings.Contains(value, "什么")
}

func containsAny(value string, phrases []string) bool {
	for _, phrase := range phrases {
		if strings.Contains(value, phrase) {
			return true
		}
	}
	return false
}
