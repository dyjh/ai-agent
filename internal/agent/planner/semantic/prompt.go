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
			"Select only tools from the provided candidate_tools.",
			"Do not invent tools.",
			"Do not output shell commands as a substitute for a candidate tool.",
			"If candidate tools are insufficient or parameters are missing, use decision=clarify.",
			"If no tool is needed, use decision=answer.",
			"Do not include secrets in the output.",
			"Unknown or ambiguous requests should use decision=clarify.",
		},
		"candidate_tools":    candidates,
		"normalized_request": req,
		"classification":     cls,
		"output_schema": map[string]any{
			"decision":   "answer|tool|multi_step|clarify",
			"goal":       "string",
			"confidence": "number 0..1",
			"domain":     "string",
			"steps": []map[string]any{{
				"tool":       "catalog tool id",
				"purpose":    "short purpose",
				"input":      "object matching tool input schema",
				"depends_on": "optional array of prior step indexes",
			}},
			"answer":              "for direct answers only",
			"clarifying_question": "for clarify only",
			"reason":              "short reason",
		},
	}
	data, _ := json.MarshalIndent(payload, "", "  ")
	return string(data)
}
