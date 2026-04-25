package semantic

// SemanticPlanDecision is the structured planner decision before compilation.
type SemanticPlanDecision string

const (
	SemanticPlanAnswer    SemanticPlanDecision = "answer"
	SemanticPlanTool      SemanticPlanDecision = "tool"
	SemanticPlanMultiStep SemanticPlanDecision = "multi_step"
	SemanticPlanClarify   SemanticPlanDecision = "clarify"
)

// SemanticPlan is the only shape an LLM semantic planner may emit.
type SemanticPlan struct {
	Decision           SemanticPlanDecision `json:"decision"`
	Goal               string               `json:"goal,omitempty"`
	Confidence         float64              `json:"confidence"`
	Language           string               `json:"language,omitempty"`
	Domain             string               `json:"domain,omitempty"`
	Steps              []SemanticPlanStep   `json:"steps,omitempty"`
	Answer             string               `json:"answer,omitempty"`
	ClarifyingQuestion string               `json:"clarifying_question,omitempty"`
	Reason             string               `json:"reason,omitempty"`
}

// SemanticPlanStep describes one proposed tool step. It is non-executable.
type SemanticPlanStep struct {
	Tool      string         `json:"tool"`
	Purpose   string         `json:"purpose"`
	Input     map[string]any `json:"input"`
	DependsOn []int          `json:"depends_on,omitempty"`
}
