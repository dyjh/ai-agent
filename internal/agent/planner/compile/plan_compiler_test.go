package compile

import (
	"testing"

	"local-agent/internal/agent/planner/catalog"
	"local-agent/internal/agent/planner/semantic"
	"local-agent/internal/einoapp"
)

func TestPlanCompilerToolProposal(t *testing.T) {
	compiler := New(catalog.New(nil), einoapp.ProposalToolAdapter{})
	compiled := compiler.Compile(semantic.SemanticPlan{
		Decision:   semantic.SemanticPlanTool,
		Confidence: 0.9,
		Steps: []semantic.SemanticPlanStep{{
			Tool:    "code.search_text",
			Purpose: "search",
			Input:   map[string]any{"path": ".", "query": "TODO", "limit": 50},
		}},
	})
	if compiled.Decision != DecisionTool || compiled.ToolProposal == nil || compiled.ToolProposal.Tool != "code.search_text" {
		t.Fatalf("compiled = %+v", compiled)
	}
}

func TestPlanCompilerClarifyAndAnswer(t *testing.T) {
	compiler := New(catalog.New(nil), einoapp.ProposalToolAdapter{})
	answer := compiler.Compile(semantic.SemanticPlan{Decision: semantic.SemanticPlanAnswer, Answer: "ok"})
	if answer.Decision != DecisionAnswer || answer.Message != "ok" {
		t.Fatalf("answer = %+v", answer)
	}
	clarify := compiler.Compile(semantic.SemanticPlan{Decision: semantic.SemanticPlanClarify, ClarifyingQuestion: "which file?"})
	if clarify.Decision != DecisionAnswer || clarify.Message != "which file?" {
		t.Fatalf("clarify = %+v", clarify)
	}
	noTool := compiler.Compile(semantic.SemanticPlan{Decision: semantic.SemanticPlanNoTool, Answer: "must not be used"})
	if noTool.Decision != DecisionAnswer || noTool.AnswerMode != AnswerModeRunner || noTool.Message != "" {
		t.Fatalf("noTool = %+v, want empty message for runner", noTool)
	}
	capability := compiler.Compile(semantic.SemanticPlan{Decision: semantic.SemanticPlanCapabilityLimitation, CapabilityMessage: "缺少可用连接器"})
	if capability.Decision != DecisionAnswer || capability.AnswerMode != AnswerModeCapabilityLimitation || capability.Message == "" {
		t.Fatalf("capability = %+v, want capability limitation message", capability)
	}
}

func TestPlanCompilerMultiStepCompilesFirstStep(t *testing.T) {
	compiler := New(catalog.New(nil), einoapp.ProposalToolAdapter{})
	compiled := compiler.Compile(semantic.SemanticPlan{
		Decision: semantic.SemanticPlanMultiStep,
		Steps: []semantic.SemanticPlanStep{
			{Tool: "git.status", Input: map[string]any{"workspace": "."}},
			{Tool: "git.diff", Input: map[string]any{"workspace": "."}},
		},
	})
	if compiled.ToolProposal == nil || compiled.ToolProposal.Tool != "git.status" {
		t.Fatalf("compiled = %+v", compiled)
	}
}
