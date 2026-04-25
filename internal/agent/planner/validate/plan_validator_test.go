package validate

import (
	"strings"
	"testing"

	"local-agent/internal/agent/planner/catalog"
	"local-agent/internal/agent/planner/semantic"
)

func TestPlanValidatorRejectsUnknownTool(t *testing.T) {
	result := New(catalog.New(nil), Options{}).Validate(toolPlan("unknown.tool", map[string]any{}))
	if result.Valid {
		t.Fatalf("unknown tool validated")
	}
}

func TestPlanValidatorRejectsInvalidInputAndClarifiesMissingArgs(t *testing.T) {
	result := New(catalog.New(nil), Options{}).Validate(toolPlan("code.search_text", map[string]any{"path": "."}))
	if result.Valid || result.Clarify == "" {
		t.Fatalf("result = %+v, want invalid clarify", result)
	}
	result = New(catalog.New(nil), Options{}).Validate(toolPlan("code.search_text", map[string]any{"path": ".", "query": "x", "limit": "many"}))
	if result.Valid {
		t.Fatalf("invalid input type validated")
	}
}

func TestPlanValidatorRejectsPathEscapeAndSecretInput(t *testing.T) {
	validator := New(catalog.New(nil), Options{})
	result := validator.Validate(toolPlan("code.read_file", map[string]any{"path": "../secret.txt"}))
	if result.Valid {
		t.Fatalf("path escape validated")
	}
	result = validator.Validate(toolPlan("code.search_text", map[string]any{"path": ".", "query": "api_key=secret-value"}))
	if result.Valid {
		t.Fatalf("secret input validated")
	}
}

func TestPlanValidatorDangerousToolWarning(t *testing.T) {
	result := New(catalog.New(nil), Options{}).Validate(toolPlan("git.clean", map[string]any{"workspace": "."}))
	if !result.Valid || len(result.Warnings) == 0 {
		t.Fatalf("result = %+v, want valid with approval warning", result)
	}
	if !strings.Contains(strings.Join(result.Warnings, " "), "approval") {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

func TestPlanValidatorRejectsPlannerBlockedTool(t *testing.T) {
	result := New(catalog.New(nil), Options{}).Validate(toolPlan("mcp.call_tool", map[string]any{"server_id": "s", "tool_name": "x", "args": map[string]any{}}))
	if result.Valid {
		t.Fatalf("mcp.call_tool must not be planner-selectable")
	}
}

func toolPlan(tool string, input map[string]any) semantic.SemanticPlan {
	return semantic.SemanticPlan{
		Decision:   semantic.SemanticPlanTool,
		Confidence: 0.9,
		Steps:      []semantic.SemanticPlanStep{{Tool: tool, Purpose: "test", Input: input}},
	}
}
