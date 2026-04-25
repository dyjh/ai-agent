package fastpath

import (
	"local-agent/internal/agent/planner/intent"
	"local-agent/internal/agent/planner/normalize"
	"local-agent/internal/agent/planner/semantic"
)

// Input is the deterministic fast-path planning input.
type Input struct {
	ConversationID string
	Request        normalize.NormalizedRequest
	Classification intent.IntentClassification
}

// DeterministicFastPath handles only strong structured requests. Natural
// language tool selection belongs to Tool Cards plus semantic planning.
type DeterministicFastPath struct{}

// New returns a deterministic fast path planner.
func New() DeterministicFastPath {
	return DeterministicFastPath{}
}

// Plan intentionally does not route natural language. Current structured UI,
// run, and approval resume flows are handled outside first-pass planning.
func (DeterministicFastPath) Plan(Input) (semantic.SemanticPlan, bool) {
	return semantic.SemanticPlan{}, false
}
