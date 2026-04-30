package semantic

// PlannerSource records which planner component produced the final decision.
type PlannerSource string

const (
	PlannerSourceFastPath           PlannerSource = "fastpath"
	PlannerSourceSemanticLLM        PlannerSource = "semantic_llm"
	PlannerSourceCandidateFallback  PlannerSource = "candidate_fallback"
	PlannerSourceNoToolAnswer       PlannerSource = "no_tool_answer"
	PlannerSourceConversationRouter PlannerSource = "conversation_router"
	PlannerSourceClarify            PlannerSource = "clarify"
	PlannerSourceToolUnavailable    PlannerSource = "tool_planner_unavailable"
	PlannerSourceExplicitTool       PlannerSource = "explicit_tool_request"
)

// SemanticPlanDecision is the structured planner decision before compilation.
type SemanticPlanDecision string

const (
	SemanticPlanAnswer               SemanticPlanDecision = "answer"
	SemanticPlanNoTool               SemanticPlanDecision = "no_tool"
	SemanticPlanTool                 SemanticPlanDecision = "tool"
	SemanticPlanMultiStep            SemanticPlanDecision = "multi_step"
	SemanticPlanClarify              SemanticPlanDecision = "clarify"
	SemanticPlanCapabilityLimitation SemanticPlanDecision = "capability_limitation"
)

// SemanticPlan is the only shape an LLM semantic planner may emit.
type SemanticPlan struct {
	Decision           SemanticPlanDecision `json:"decision"`
	Goal               string               `json:"goal,omitempty"`
	Confidence         float64              `json:"confidence"`
	Language           string               `json:"language,omitempty"`
	Domain             string               `json:"domain,omitempty"`
	PlannerSource      PlannerSource        `json:"planner_source,omitempty"`
	Steps              []SemanticPlanStep   `json:"steps,omitempty"`
	Answer             string               `json:"answer,omitempty"`
	ClarifyingQuestion string               `json:"clarifying_question,omitempty"`
	CapabilityMessage  string               `json:"capability_message,omitempty"`
	Reason             string               `json:"reason,omitempty"`
}

// SemanticPlanStep describes one proposed tool step. It is non-executable.
type SemanticPlanStep struct {
	Tool      string         `json:"tool"`
	Purpose   string         `json:"purpose"`
	Input     map[string]any `json:"input"`
	DependsOn []int          `json:"depends_on,omitempty"`
}
