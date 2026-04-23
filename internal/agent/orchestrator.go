package agent

import (
	"context"

	"local-agent/internal/einoapp"
)

// Orchestrator is the workflow-facing entry point for agent runs.
type Orchestrator struct {
	Workflow einoapp.AgentWorkflow
}

// Start delegates a new message to the configured workflow.
func (o Orchestrator) Start(ctx context.Context, input einoapp.AgentInput) (einoapp.AgentEventStream, error) {
	return o.Workflow.Start(ctx, input)
}

// Resume delegates an approval response to the configured workflow.
func (o Orchestrator) Resume(ctx context.Context, runID string, approvalID string, approved bool) (einoapp.AgentEventStream, error) {
	return o.Workflow.Resume(ctx, runID, approvalID, approved)
}
