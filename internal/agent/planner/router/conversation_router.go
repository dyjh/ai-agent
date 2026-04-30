package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"local-agent/internal/agent/planner/intent"
	"local-agent/internal/agent/planner/normalize"
)

// ConversationRoute is the high-level route for a user message. It never names
// a concrete tool and never contains a final natural-language answer.
type ConversationRoute string

const (
	RouteDirectAnswer ConversationRoute = "direct_answer"
	RouteToolNeeded   ConversationRoute = "tool_needed"
	RouteClarify      ConversationRoute = "clarify"
)

// RouteSource records which router path made the route decision.
type RouteSource string

const (
	RouteSourceLLM         RouteSource = "conversation_router_llm"
	RouteSourceLightweight RouteSource = "conversation_router_lightweight"
	RouteSourceFallback    RouteSource = "conversation_router_fallback"
)

// ConversationRouteDecision is the router's structured output.
type ConversationRouteDecision struct {
	Route      ConversationRoute `json:"route"`
	Source     RouteSource       `json:"route_source,omitempty"`
	Reason     string            `json:"reason,omitempty"`
	Confidence float64           `json:"confidence"`
}

// ConversationRouteInput contains context for routing only.
type ConversationRouteInput struct {
	ConversationID string                      `json:"conversation_id,omitempty"`
	UserMessage    string                      `json:"user_message"`
	Normalized     normalize.NormalizedRequest `json:"normalized"`
	Classification intent.IntentClassification `json:"classification"`
}

// ConversationRouter decides whether the message is chat, tool work, or a
// clarification request. It must not select or execute tools.
type ConversationRouter interface {
	Route(ctx context.Context, input ConversationRouteInput) (ConversationRouteDecision, error)
}

// ChatModel is the Eino chat model surface needed by LLMConversationRouter.
type ChatModel interface {
	Generate(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error)
}

// LightweightConversationRouter is a conservative local fallback. It only
// routes to tools for structural slots or clear local/external operation scope.
type LightweightConversationRouter struct{}

// Route implements ConversationRouter.
func (LightweightConversationRouter) Route(_ context.Context, input ConversationRouteInput) (ConversationRouteDecision, error) {
	text := strings.TrimSpace(input.UserMessage)
	if text == "" {
		return ConversationRouteDecision{Route: RouteClarify, Source: RouteSourceLightweight, Confidence: 0.95, Reason: "empty user message"}, nil
	}
	if input.Classification.NeedClarify {
		return ConversationRouteDecision{Route: RouteClarify, Source: RouteSourceLightweight, Confidence: input.Classification.Confidence, Reason: input.Classification.Reason}, nil
	}
	if input.Classification.NeedTool || hasStructuralToolScope(input.Normalized) || looksLikeLocalOrExternalOperation(text) || looksLikeShellRequest(text) {
		return ConversationRouteDecision{Route: RouteToolNeeded, Source: RouteSourceLightweight, Confidence: maxConfidence(input.Classification.Confidence, 0.72), Reason: "request references local state, structured scope, or tool execution"}, nil
	}
	return ConversationRouteDecision{Route: RouteDirectAnswer, Source: RouteSourceLightweight, Confidence: maxConfidence(input.Classification.Confidence, 0.65), Reason: "no local state, file, tool, shell, repository, or external connector access required"}, nil
}

// LLMConversationRouter asks a model to decide the route as JSON only.
type LLMConversationRouter struct {
	Model       ChatModel
	MaxRetries  int
	RequireJSON bool
	Fallback    ConversationRouter
}

// Route implements ConversationRouter.
func (r LLMConversationRouter) Route(ctx context.Context, input ConversationRouteInput) (ConversationRouteDecision, error) {
	if r.Model == nil {
		return r.fallback(ctx, input, errors.New("conversation router model unavailable"))
	}
	maxRetries := r.MaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}
	prompt := BuildPrompt(input)
	var last string
	for attempt := 0; attempt <= maxRetries; attempt++ {
		content := prompt
		if attempt > 0 {
			content = repairPrompt(input, last)
		}
		msg, err := r.Model.Generate(ctx, []*schema.Message{
			{Role: schema.System, Content: "You are an AI Conversation Router. Output only route JSON. Do not answer the user and do not choose tools."},
			{Role: schema.User, Content: content},
		})
		if err != nil {
			return r.fallback(ctx, input, err)
		}
		last = strings.TrimSpace(msg.Content)
		decision, err := ParseRouteDecision(last, r.RequireJSON)
		if err == nil {
			decision.Source = RouteSourceLLM
			return decision, nil
		}
	}
	return r.fallback(ctx, input, fmt.Errorf("conversation router returned invalid JSON route"))
}

func (r LLMConversationRouter) fallback(ctx context.Context, input ConversationRouteInput, cause error) (ConversationRouteDecision, error) {
	if r.Fallback != nil {
		decision, err := r.Fallback.Route(ctx, input)
		if err == nil {
			decision.Source = RouteSourceFallback
			if decision.Reason == "" {
				decision.Reason = cause.Error()
			} else {
				decision.Reason = decision.Reason + "; fallback_after=" + cause.Error()
			}
		}
		return decision, err
	}
	return ConversationRouteDecision{}, cause
}

// BuildPrompt returns the strict router prompt. The model must decide only the
// route and must not answer, select tools, or produce ToolProposal content.
func BuildPrompt(input ConversationRouteInput) string {
	payload := map[string]any{
		"role": "AI Conversation Router",
		"task": "Decide whether the current user message should be handled as ordinary chat, tool planning, or clarification.",
		"route_definitions": map[string]any{
			string(RouteDirectAnswer): []string{
				"General knowledge Q&A.",
				"Concept explanations.",
				"Writing, translation, summarization, brainstorming.",
				"Programming concept explanations.",
				"General medical, financial, or legal explanations when no local files or external systems are needed.",
			},
			string(RouteToolNeeded): []string{
				"User asks to view, read, search, modify, or inspect local files.",
				"User asks about local system, processes, disk, memory, logs, docker, k8s, git, code, or tests.",
				"User asks to execute a command.",
				"User provides workspace, file path, URL, host_id, kb_id, run_id, approval_id, or an explicit tool id.",
				"User explicitly asks to call a tool or external connector.",
			},
			string(RouteClarify): []string{
				"The goal is unclear.",
				"Required information is missing.",
				"It is not possible to determine chat versus operation.",
				"The request is too vague and executing tools would be risky.",
			},
		},
		"safety_rules": []string{
			"Do not answer the user's question.",
			"Do not choose a specific tool.",
			"Do not produce shell commands.",
			"Do not claim anything was executed.",
			"Only decide the route.",
			"If no local state, file, tool, system, repository, shell, docker, k8s, git, knowledge base, or external connector access is required, choose direct_answer.",
			"If uncertain, choose clarify rather than tool_needed.",
		},
		"input": map[string]any{
			"conversation_id": input.ConversationID,
			"user_message":    input.UserMessage,
			"normalized":      input.Normalized,
			"classification":  input.Classification,
		},
		"output_schema": map[string]any{
			"route":      "direct_answer|tool_needed|clarify",
			"confidence": "number 0.0..1.0",
			"reason":     "short reason",
		},
	}
	data, _ := json.MarshalIndent(payload, "", "  ")
	return string(data)
}

// ParseRouteDecision parses route JSON and validates the route enum.
func ParseRouteDecision(value string, requireJSON bool) (ConversationRouteDecision, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return ConversationRouteDecision{}, errors.New("empty route response")
	}
	var decision ConversationRouteDecision
	if err := json.Unmarshal([]byte(value), &decision); err != nil {
		if requireJSON {
			return ConversationRouteDecision{}, err
		}
		start := strings.Index(value, "{")
		end := strings.LastIndex(value, "}")
		if start < 0 || end <= start {
			return ConversationRouteDecision{}, err
		}
		if err := json.Unmarshal([]byte(value[start:end+1]), &decision); err != nil {
			return ConversationRouteDecision{}, err
		}
	}
	switch decision.Route {
	case RouteDirectAnswer, RouteToolNeeded, RouteClarify:
	default:
		return ConversationRouteDecision{}, fmt.Errorf("invalid route %q", decision.Route)
	}
	if decision.Confidence < 0 {
		decision.Confidence = 0
	}
	if decision.Confidence > 1 {
		decision.Confidence = 1
	}
	return decision, nil
}

func repairPrompt(input ConversationRouteInput, last string) string {
	return BuildPrompt(input) + "\n\nThe previous response was invalid. Return only JSON matching the output_schema. Previous response:\n" + last
}

func hasStructuralToolScope(req normalize.NormalizedRequest) bool {
	return req.ExplicitToolID != "" ||
		req.Workspace != "" ||
		len(req.PossibleFiles) > 0 ||
		len(req.URLs) > 0 ||
		req.HostID != "" ||
		req.KBID != "" ||
		req.RunID != "" ||
		req.ApprovalID != ""
}

func looksLikeLocalOrExternalOperation(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	if containsAny(lower, []string{"记住", "忘记"}) ||
		(containsAny(lower, []string{"以后", "偏好", "喜欢"}) && containsAny(lower, []string{"回答", "使用", "用中文", "语言"})) {
		return true
	}
	if containsAny(lower, []string{"知识库", "kb"}) && containsAny(lower, []string{"回答", "引用", "来源", "检索", "根据"}) {
		return true
	}
	return containsAny(lower, []string{
		"workspace", "tool_id", "approval_id", "run_id", "host_id", "kb_id",
	}) || (containsAny(lower, []string{
		"看", "查看", "读取", "打开", "搜索", "定位", "获取", "检查", "确认", "列出", "运行", "执行", "测试", "检测", "跑", "安装", "重启", "删除", "修改", "修复", "实现", "应用",
		"read", "open", "search", "find", "get", "inspect", "check", "list", "run", "execute", "test", "install", "restart", "delete", "modify", "fix", "implement", "apply",
	}) && containsAny(lower, []string{
		"本地", "本机", "机器", "系统", "进程", "cpu", "占用", "磁盘", "内存", "日志", "文件", "目录", "路径", "仓库", "项目", "代码", "服务", "主机", "知识库", "审批", "容器", "pod", "bug", "测试", "失败", "功能", "patch", "diff",
		"docker", "k8s", "kubectl", "git", "mcp", "url",
		"local", "system", "process", "cpu", "disk", "memory", "log", "file", "directory", "path", "repo", "repository", "code", "service", "host", "knowledge base", "approval", "container", "bug", "test", "failure", "feature",
	}))
}

func looksLikeShellRequest(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if strings.Contains(lower, "shell") || strings.Contains(lower, "bash") || strings.Contains(lower, "终端") {
		return true
	}
	if strings.Contains(lower, "执行") && strings.Contains(lower, "命令") {
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

func containsAny(value string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func maxConfidence(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
