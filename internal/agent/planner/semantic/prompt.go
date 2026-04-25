package semantic

import (
	"encoding/json"

	"local-agent/internal/agent/planner/catalog"
	"local-agent/internal/agent/planner/intent"
	"local-agent/internal/agent/planner/normalize"
)

func BuildPrompt(req normalize.NormalizedRequest, cls intent.IntentClassification, cat catalog.PlanningCatalog) string {
	tools := cat.All()
	if len(tools) > 40 {
		tools = tools[:40]
	}
	payload := map[string]any{
		"security_rules": []string{
			"Do not execute tools.",
			"Do not write files, run shell commands, call MCP, or call external systems.",
			"Only output a SemanticPlan JSON object.",
			"Select only tools from the provided catalog.",
			"Do not include secrets in the output.",
			"Unknown or ambiguous requests should use decision=clarify.",
		},
		"tool_catalog":       tools,
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
