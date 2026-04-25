package semantic

import (
	"strings"

	"local-agent/internal/agent/planner/candidate"
	"local-agent/internal/agent/planner/normalize"
	"local-agent/internal/core"
	"local-agent/internal/security"
)

// PlanFromCandidates builds a conservative local SemanticPlan from Tool Card
// candidates when an external LLM planner is disabled or unavailable. It uses
// structural slots and Tool Card examples/defaults only; it does not execute.
func PlanFromCandidates(req normalize.NormalizedRequest, conversationID string, candidates []candidate.ToolCandidate) SemanticPlan {
	if len(candidates) == 0 {
		return SemanticPlan{
			Decision:           SemanticPlanClarify,
			Goal:               req.Original,
			Confidence:         0.35,
			ClarifyingQuestion: "请补充要使用的工具、范围或参数。",
			Reason:             "no tool card candidates",
		}
	}
	chosen := candidates[0]
	input := inputFromCandidate(req, conversationID, chosen)
	return SemanticPlan{
		Decision:   SemanticPlanTool,
		Goal:       req.Original,
		Confidence: candidateConfidence(chosen.Score),
		Domain:     chosen.Card.Domain,
		Reason:     "selected from tool card candidates: " + chosen.Reason,
		Steps: []SemanticPlanStep{{
			Tool:    chosen.ToolID,
			Purpose: chosen.Card.Title,
			Input:   input,
		}},
	}
}

func inputFromCandidate(req normalize.NormalizedRequest, conversationID string, candidate candidate.ToolCandidate) map[string]any {
	input := nearestExampleInput(req, candidate)
	mergeMissing(input, candidate.Card.Defaults)
	switch candidate.ToolID {
	case "shell.exec":
		if _, ok := input["cwd"]; !ok {
			input["cwd"] = workspaceOrDot(req.Workspace)
		}
	case "code.read_file":
		if len(req.PossibleFiles) > 0 {
			input["path"] = req.PossibleFiles[0]
		}
	case "code.search_text":
		if req.Workspace != "" {
			input["path"] = req.Workspace
		}
		if quoted := firstQuoted(req); quoted != "" {
			input["query"] = quoted
		}
	case "code.inspect_project":
		input["path"] = workspaceOrDot(req.Workspace)
	case "code.run_tests", "code.fix_test_failure_loop":
		input["workspace"] = workspaceOrDot(req.Workspace)
	case "git.status", "git.diff", "git.diff_summary", "git.commit_message_proposal", "git.log":
		input["workspace"] = workspaceOrDot(req.Workspace)
	case "kb.answer", "kb.retrieve":
		input["query"] = req.Original
		if req.KBID != "" {
			input["kb_id"] = req.KBID
		}
	case "memory.extract_candidates":
		queue := !security.ScanText(req.Original).HasSecret
		text := req.Original
		if !queue {
			text = "[REDACTED sensitive memory request]"
		}
		input["conversation_id"] = conversationID
		input["text"] = text
		input["queue"] = queue
	case "memory.item_archive":
		if quoted := firstQuoted(req); quoted != "" {
			input["id"] = quoted
		} else if id := memoryID(req.Original); id != "" {
			input["id"] = id
		}
	case "ops.local.logs_tail":
		if len(req.PossibleFiles) > 0 {
			input["path"] = req.PossibleFiles[0]
		} else if quoted := firstQuoted(req); quoted != "" {
			input["path"] = quoted
		}
	case "ops.docker.logs", "ops.docker.restart":
		if quoted := firstQuoted(req); quoted != "" {
			input["container"] = quoted
		}
	case "ops.local.service_restart":
		if quoted := firstQuoted(req); quoted != "" {
			input["service"] = quoted
		}
	case "ops.k8s.logs":
		if quoted := firstQuoted(req); quoted != "" {
			input["target"] = quoted
		}
	case "ops.k8s.get":
		if _, ok := input["resource"]; !ok {
			input["resource"] = "pods"
		}
	}
	return input
}

func memoryID(message string) string {
	fields := strings.Fields(message)
	for i := len(fields) - 1; i >= 0; i-- {
		token := strings.Trim(fields[i], "`'\"，,。;；")
		if strings.HasPrefix(token, "mem_") || strings.HasPrefix(token, "memory_") {
			return token
		}
	}
	return ""
}

func nearestExampleInput(req normalize.NormalizedRequest, candidate candidate.ToolCandidate) map[string]any {
	var best map[string]any
	bestScore := 0.0
	for _, ex := range candidate.Card.Examples {
		score := candidatepkgSimilarity(req.Original, ex.User)
		if score > bestScore {
			bestScore = score
			best = ex.Input
		}
	}
	if bestScore < 0.35 {
		return map[string]any{}
	}
	return core.CloneMap(best)
}

func candidatepkgSimilarity(a, b string) float64 {
	return candidate.TextSimilarity(a, b)
}

func mergeMissing(target map[string]any, defaults map[string]any) {
	if target == nil {
		return
	}
	for key, value := range defaults {
		if _, ok := target[key]; !ok {
			target[key] = value
		}
	}
}

func firstQuoted(req normalize.NormalizedRequest) string {
	for _, item := range req.QuotedTexts {
		item = strings.TrimSpace(item)
		if item != "" {
			return item
		}
	}
	return ""
}

func workspaceOrDot(workspace string) string {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return "."
	}
	return workspace
}

func candidateConfidence(score float64) float64 {
	switch {
	case score >= 6:
		return 0.95
	case score >= 3:
		return 0.88
	case score >= 1:
		return 0.75
	default:
		return 0.6
	}
}
