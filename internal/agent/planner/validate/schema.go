package validate

import (
	"fmt"
	"strings"

	"local-agent/internal/agent/planner/catalog"
	"local-agent/internal/agent/planner/semantic"
)

func (v Validator) validateStep(step *semantic.SemanticPlanStep) stepValidation {
	result := stepValidation{}
	spec, ok := v.Catalog.Tool(step.Tool)
	if !ok {
		result.Errors = append(result.Errors, "unknown tool: "+step.Tool)
		return result
	}
	if plannerBlockedTool(step.Tool) {
		result.Errors = append(result.Errors, "tool is not planner auto-selectable: "+step.Tool)
		return result
	}
	if !v.Options.AllowCrossCandidate && len(v.Options.CandidateToolIDs) > 0 && !containsTool(v.Options.CandidateToolIDs, step.Tool) {
		result.Errors = append(result.Errors, "tool was not in candidate set: "+step.Tool)
		result.Clarify = "候选工具不足以安全处理这个请求，请补充目标或改用明确的工具参数。"
		return result
	}
	if !spec.AutoSelectable {
		result.Warnings = append(result.Warnings, "dangerous or approval-gated tool cannot execute without policy approval")
	}
	if spec.RequiresApproval {
		result.Warnings = append(result.Warnings, "tool requires approval before execution")
	}
	if isDangerousSpec(spec) && !spec.RequiresApproval {
		result.Errors = append(result.Errors, "dangerous tool must require approval: "+step.Tool)
	}
	if step.Input == nil {
		step.Input = map[string]any{}
	}
	applyToolCardDefaults(step, spec)
	applyDefaults(step)
	safety := v.validateSafety(*step)
	result.Errors = append(result.Errors, safety.Errors...)
	result.Warnings = append(result.Warnings, safety.Warnings...)
	for _, slot := range spec.RequiredSlots {
		if !v.slotSatisfied(slot, *step) {
			result.Errors = append(result.Errors, fmt.Sprintf("missing required slot %s for %s", slot, step.Tool))
			result.Clarify = clarifyForMissing(step.Tool, slot)
		}
	}
	for _, field := range mergeRequiredFields(requiredFieldsFromSchema(spec.InputSchema), requiredFields(step.Tool)) {
		value, exists := step.Input[field]
		if !exists || value == nil || strings.TrimSpace(fmt.Sprint(value)) == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("missing required input %s for %s", field, step.Tool))
			result.Clarify = clarifyForMissing(step.Tool, field)
		}
	}
	for field, typ := range spec.InputSchema {
		value, exists := step.Input[field]
		if !exists || value == nil || fmt.Sprint(value) == "" {
			continue
		}
		if !typeMatches(value, fmt.Sprint(typ)) {
			result.Errors = append(result.Errors, fmt.Sprintf("invalid input type for %s.%s", step.Tool, field))
		}
	}
	return result
}

func containsTool(tools []string, tool string) bool {
	for _, item := range tools {
		if strings.TrimSpace(item) == tool {
			return true
		}
	}
	return false
}

func plannerBlockedTool(tool string) bool {
	switch tool {
	case "mcp.call_tool", "skill.run":
		return true
	default:
		return false
	}
}

func applyDefaults(step *semantic.SemanticPlanStep) {
	switch step.Tool {
	case "code.search_text":
		if _, ok := step.Input["path"]; !ok {
			step.Input["path"] = "."
		}
		if _, ok := step.Input["limit"]; !ok {
			step.Input["limit"] = 50
		}
	case "code.read_file":
		if _, ok := step.Input["max_bytes"]; !ok {
			step.Input["max_bytes"] = 200000
		}
	case "code.inspect_project":
		if _, ok := step.Input["path"]; !ok {
			step.Input["path"] = "."
		}
	case "code.run_tests":
		if _, ok := step.Input["workspace"]; !ok {
			step.Input["workspace"] = "."
		}
		if _, ok := step.Input["use_detected"]; !ok {
			step.Input["use_detected"] = true
		}
		if _, ok := step.Input["timeout_seconds"]; !ok {
			step.Input["timeout_seconds"] = 300
		}
		if _, ok := step.Input["max_output_bytes"]; !ok {
			step.Input["max_output_bytes"] = 200000
		}
	case "code.fix_test_failure_loop":
		if _, ok := step.Input["workspace"]; !ok {
			step.Input["workspace"] = "."
		}
		if _, ok := step.Input["use_detected"]; !ok {
			step.Input["use_detected"] = true
		}
		if _, ok := step.Input["max_iterations"]; !ok {
			step.Input["max_iterations"] = 3
		}
		if _, ok := step.Input["stop_on_approval"]; !ok {
			step.Input["stop_on_approval"] = true
		}
		if _, ok := step.Input["auto_rerun_tests"]; !ok {
			step.Input["auto_rerun_tests"] = true
		}
		if _, ok := step.Input["failure_context_max"]; !ok {
			step.Input["failure_context_max"] = 3
		}
	case "git.status", "git.diff", "git.diff_summary", "git.commit_message_proposal":
		if _, ok := step.Input["workspace"]; !ok {
			step.Input["workspace"] = "."
		}
	case "git.log":
		if _, ok := step.Input["workspace"]; !ok {
			step.Input["workspace"] = "."
		}
		if _, ok := step.Input["limit"]; !ok {
			step.Input["limit"] = 20
		}
	case "ops.local.logs_tail":
		if _, ok := step.Input["max_lines"]; !ok {
			step.Input["max_lines"] = 100
		}
	case "ops.docker.logs", "ops.k8s.logs":
		if _, ok := step.Input["max_lines"]; !ok {
			step.Input["max_lines"] = 200
		}
	case "kb.answer":
		if _, ok := step.Input["top_k"]; !ok {
			step.Input["top_k"] = 5
		}
		if _, ok := step.Input["rerank"]; !ok {
			step.Input["rerank"] = true
		}
	case "kb.retrieve":
		if _, ok := step.Input["top_k"]; !ok {
			step.Input["top_k"] = 5
		}
		if _, ok := step.Input["mode"]; !ok {
			step.Input["mode"] = "hybrid"
		}
	}
}

func applyToolCardDefaults(step *semantic.SemanticPlanStep, spec catalog.PlanningToolSpec) {
	for key, value := range spec.Defaults {
		if _, ok := step.Input[key]; !ok {
			step.Input[key] = value
		}
	}
}

func requiredFieldsFromSchema(schema map[string]any) []string {
	raw, ok := schema["required"]
	if !ok {
		return nil
	}
	switch typed := raw.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if value := strings.TrimSpace(fmt.Sprint(item)); value != "" {
				out = append(out, value)
			}
		}
		return out
	default:
		return nil
	}
}

func mergeRequiredFields(groups ...[]string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, group := range groups {
		for _, field := range group {
			field = strings.TrimSpace(field)
			if field == "" {
				continue
			}
			if _, ok := seen[field]; ok {
				continue
			}
			seen[field] = struct{}{}
			out = append(out, field)
		}
	}
	return out
}

func requiredFields(tool string) []string {
	switch tool {
	case "shell.exec":
		return []string{"command"}
	case "code.read_file":
		return []string{"path"}
	case "code.search_text":
		return []string{"path", "query"}
	case "code.inspect_project":
		return []string{"path"}
	case "kb.answer", "kb.retrieve":
		return []string{"query"}
	case "memory.extract_candidates":
		return []string{"text"}
	case "memory.item_archive":
		return []string{"id"}
	case "ops.local.service_restart":
		return []string{"service"}
	case "ops.local.logs_tail":
		return []string{"path"}
	case "ops.docker.logs", "ops.docker.restart":
		return []string{"container"}
	case "ops.k8s.logs":
		return []string{"target"}
	case "ops.k8s.get":
		return []string{"resource"}
	default:
		return nil
	}
}

func clarifyForMissing(tool, field string) string {
	return fmt.Sprintf("请补充 %s 的 %s 参数。", tool, field)
}

func typeMatches(value any, typ string) bool {
	switch strings.ToLower(strings.TrimSpace(typ)) {
	case "string":
		_, ok := value.(string)
		return ok
	case "number", "integer":
		switch value.(type) {
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
			return true
		default:
			return false
		}
	case "boolean", "bool":
		_, ok := value.(bool)
		return ok
	case "array":
		switch value.(type) {
		case []any, []string:
			return true
		default:
			return false
		}
	case "object":
		_, ok := value.(map[string]any)
		return ok
	default:
		return true
	}
}

func (v Validator) slotSatisfied(slot string, step semantic.SemanticPlanStep) bool {
	slot = strings.TrimSpace(slot)
	if slot == "" {
		return true
	}
	if v.Options.Request == nil {
		return true
	}
	req := *v.Options.Request
	switch slot {
	case "workspace":
		return strings.TrimSpace(req.Workspace) != "" || strings.TrimSpace(fmt.Sprint(step.Input["workspace"])) != "" || strings.TrimSpace(fmt.Sprint(step.Input["path"])) != ""
	case "quoted_text":
		return len(req.QuotedTexts) > 0 || strings.TrimSpace(fmt.Sprint(step.Input["query"])) != ""
	case "possible_file", "file_path":
		return len(req.PossibleFiles) > 0 || strings.TrimSpace(fmt.Sprint(step.Input["path"])) != ""
	case "url":
		return len(req.URLs) > 0
	case "host_id":
		return strings.TrimSpace(req.HostID) != "" || strings.TrimSpace(fmt.Sprint(step.Input["host_id"])) != ""
	case "kb_id":
		return strings.TrimSpace(req.KBID) != "" || strings.TrimSpace(fmt.Sprint(step.Input["kb_id"])) != ""
	case "run_id":
		return strings.TrimSpace(req.RunID) != "" || strings.TrimSpace(fmt.Sprint(step.Input["run_id"])) != ""
	case "approval_id":
		return strings.TrimSpace(req.ApprovalID) != "" || strings.TrimSpace(fmt.Sprint(step.Input["approval_id"])) != ""
	case "explicit_tool_id":
		return strings.TrimSpace(req.ExplicitToolID) != ""
	case "number":
		return len(req.Numbers) > 0
	default:
		return strings.TrimSpace(fmt.Sprint(step.Input[slot])) != ""
	}
}

func isDangerousSpec(spec catalog.PlanningToolSpec) bool {
	if strings.EqualFold(spec.RiskLevel, "dangerous") || strings.EqualFold(spec.RiskLevel, "high") {
		return true
	}
	for _, effect := range spec.DefaultEffects {
		effect = strings.ToLower(effect)
		if strings.Contains(effect, "danger") || strings.Contains(effect, "delete") {
			return true
		}
	}
	return false
}
