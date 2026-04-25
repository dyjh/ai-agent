package semantic_test

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"local-agent/internal/agent/planner/candidate"
	"local-agent/internal/agent/planner/catalog"
	"local-agent/internal/agent/planner/intent"
	"local-agent/internal/agent/planner/normalize"
	"local-agent/internal/agent/planner/semantic"
	"local-agent/internal/agent/planner/validate"
)

func TestLLMSemanticPlannerSelectsCandidateTool(t *testing.T) {
	req := normalize.New().Normalize("find containing `TODO` workspace: .")
	candidates := []candidate.ToolCandidate{candidateFor("code.search_text")}
	planner := semantic.NewLLMPlanner(fakeModel{response: `{"decision":"tool","confidence":0.9,"domain":"code","steps":[{"tool":"code.search_text","purpose":"search","input":{"path":".","query":"TODO"}}]}`}, semantic.Config{SemanticEnabled: true, MaxRetries: 1})
	plan, err := planner.Plan(context.Background(), req, intent.New().Classify(req), candidates)
	if err != nil {
		t.Fatalf("Plan error = %v", err)
	}
	if len(plan.Steps) != 1 || plan.Steps[0].Tool != "code.search_text" {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestSemanticUnknownToolRejectedByValidator(t *testing.T) {
	req := normalize.New().Normalize("find containing `TODO` workspace: .")
	candidates := []candidate.ToolCandidate{candidateFor("code.search_text")}
	plan := semantic.SemanticPlan{
		Decision:   semantic.SemanticPlanTool,
		Confidence: 0.9,
		Steps:      []semantic.SemanticPlanStep{{Tool: "unknown.tool", Input: map[string]any{}}},
	}
	result := validate.New(catalog.New(nil), validate.Options{
		Request:          &req,
		CandidateToolIDs: []string{candidates[0].ToolID},
	}).Validate(plan)
	if result.Valid {
		t.Fatalf("unknown tool validated")
	}
}

func TestLocalCandidatePlannerMissingRequiredSlotClarifies(t *testing.T) {
	req := normalize.New().Normalize("workspace: /tmp/demo")
	candidates := []candidate.ToolCandidate{{
		ToolID: "code.search_text",
		Score:  3,
		Reason: "test",
		Card: catalog.ToolCard{
			ToolID:         "code.search_text",
			Domain:         "code",
			Title:          "Search",
			Description:    "Search text",
			RequiredSlots:  []string{"quoted_text"},
			Defaults:       map[string]any{"path": ".", "limit": 50},
			AutoSelectable: true,
		},
	}}
	plan := semantic.PlanFromCandidates(req, "conv", candidates)
	result := validate.New(catalog.New(nil), validate.Options{
		Request:          &req,
		CandidateToolIDs: []string{"code.search_text"},
	}).Validate(plan)
	if result.Valid || result.Clarify == "" {
		t.Fatalf("result = %+v, want missing slot clarification", result)
	}
}

func candidateFor(tool string) candidate.ToolCandidate {
	spec, _ := catalog.New(nil).Tool(tool)
	card := catalog.ToolCard{ToolID: spec.ToolID, Domain: spec.Domain, Title: spec.Title, Description: spec.Description, Defaults: spec.Defaults, RequiredSlots: spec.RequiredSlots}
	if spec.Card != nil {
		card = *spec.Card
	}
	return candidate.ToolCandidate{ToolID: tool, Score: 3, Reason: "test", Card: card}
}

type fakeModel struct {
	response string
}

func (m fakeModel) Generate(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	return &schema.Message{Role: schema.Assistant, Content: m.response}, nil
}
