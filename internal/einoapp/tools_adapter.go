package einoapp

import (
	"time"

	"local-agent/internal/core"
	"local-agent/internal/ids"
)

// ProposalToolAdapter exposes ToolSpecs to the planner while preserving the approval boundary.
type ProposalToolAdapter struct{}

// NewProposal creates a tool proposal rather than executing a tool directly.
func (ProposalToolAdapter) NewProposal(tool string, input map[string]any, purpose string, expectedEffects []string) core.ToolProposal {
	return core.ToolProposal{
		ID:              ids.New("tool"),
		Tool:            tool,
		Input:           core.CloneMap(input),
		Purpose:         purpose,
		ExpectedEffects: expectedEffects,
		CreatedAt:       time.Now().UTC(),
	}
}
