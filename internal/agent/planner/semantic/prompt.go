package semantic

import (
	"encoding/json"

	"local-agent/internal/agent/planner/candidate"
	"local-agent/internal/agent/planner/intent"
	"local-agent/internal/agent/planner/normalize"
)

func BuildPrompt(req normalize.NormalizedRequest, cls intent.IntentClassification, candidates []candidate.ToolCandidate) string {
	payload := map[string]any{
		"security_rules": []string{
			"Do not execute tools.",
			"Do not write files, run shell commands, call MCP, or call external systems.",
			"Only output a SemanticPlan JSON object.",
			"Select only tools from the provided candidate_tools unless cross-candidate selection is explicitly allowed by the runtime.",
			"Do not invent tools.",
			"Do not select tools whose card has auto_selectable=false unless the user explicitly requested that tool and the tool may be proposed for approval.",
			"shell.exec is not a fallback for missing structured tools.",
			"Only choose shell.exec when the user explicitly asks for shell/command execution or provides the exact shell command.",
			"If candidate tools are insufficient, use decision=capability_limitation or decision=clarify; do not invent tools or commands.",
			"If parameters are missing, use decision=clarify.",
			"If no tool is needed, use decision=no_tool, not a natural language answer.",
			"Do not answer general knowledge questions.",
			"Do not include an answer field.",
			"Do not include secrets in the output.",
			"Unknown or ambiguous requests should use decision=clarify.",
			"High-risk tools may only be proposed; never claim they already executed.",
			"Local validation and policy will enforce safety; do not bypass them.",
		},
		"candidate_tools":    candidates,
		"normalized_request": req,
		"classification":     cls,
		"output_schema": map[string]any{
			"decision":   "tool|multi_step|clarify|no_tool|capability_limitation",
			"goal":       "string",
			"confidence": "number 0..1",
			"domain":     "string",
			"steps": []map[string]any{{
				"tool":       "catalog tool id",
				"purpose":    "short purpose",
				"input":      "object matching tool input schema",
				"depends_on": "optional array of prior step indexes",
			}},
			"clarifying_question": "for clarify only",
			"capability_message":  "for capability_limitation only",
			"reason":              "short reason",
		},
	}
	data, _ := json.MarshalIndent(payload, "", "  ")
	return string(data)
}
