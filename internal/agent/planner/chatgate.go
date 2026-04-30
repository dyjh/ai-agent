package planner

import (
	"strings"
	"unicode/utf8"

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
	if isSimpleGreeting(text) || looksLikeDirectChat(text) {
		return chatGateDirectAnswer
	}
	return chatGateMaybeTool
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

func isSimpleGreeting(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	lower = strings.Trim(lower, " ,.?!，。！？")
	switch lower {
	case "hi", "hello", "hey", "你好", "您好", "嗨":
		return true
	default:
		return false
	}
}

func looksLikeDirectChat(text string) bool {
	lower := strings.ToLower(text)
	directPhrases := []string{
		"解释", "说明", "是什么", "为什么", "怎么理解", "写一段", "帮我写", "润色", "翻译", "总结一下",
		"介绍", "聊聊", "头脑风暴",
		"explain", "what is", "why", "write a", "draft", "translate", "summarize",
		"introduce", "brainstorm",
	}
	for _, phrase := range directPhrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return utf8.RuneCountInString(text) <= 12 && strings.HasSuffix(strings.TrimSpace(text), "?")
}

func looksExternalAction(text string) bool {
	lower := strings.ToLower(text)
	actionPhrases := []string{
		"查看", "读取", "打开", "搜索", "定位", "运行", "执行", "测试", "安装", "重启", "删除", "修改", "应用",
		"read", "open", "search", "find", "run", "execute", "test", "install", "restart", "delete", "modify", "apply",
	}
	for _, phrase := range actionPhrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
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
	case "cat", "ls", "pwd", "ps", "grep", "rg", "sed", "head", "tail", "go", "npm", "pnpm", "yarn", "python", "docker", "kubectl":
		return true
	default:
		return false
	}
}
