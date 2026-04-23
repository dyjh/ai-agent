package tools

import (
	"context"

	"local-agent/internal/core"
)

// Executor runs an approved or read-only tool invocation.
type Executor interface {
	Execute(ctx context.Context, input map[string]any) (*core.ToolResult, error)
}

// EffectInferrer infers concrete effects from a proposal snapshot.
type EffectInferrer interface {
	Infer(ctx context.Context, proposal core.ToolProposal) (core.EffectInferenceResult, error)
}

// PolicyDecider decides whether a proposal can auto-run.
type PolicyDecider interface {
	Decide(ctx context.Context, proposal core.ToolProposal, inference core.EffectInferenceResult) (core.PolicyDecision, error)
}

// ApprovalCenter manages immutable approval snapshots.
type ApprovalCenter interface {
	Create(runID, conversationID string, proposal core.ToolProposal, inference core.EffectInferenceResult, decision core.PolicyDecision) (*core.ApprovalRecord, error)
	Get(id string) (*core.ApprovalRecord, error)
	Pending() ([]core.ApprovalRecord, error)
	Approve(id string) (*core.ApprovalRecord, error)
	Reject(id, reason string) (*core.ApprovalRecord, error)
}

// EventWriter writes runtime events to JSONL/audit sinks.
type EventWriter interface {
	WriteRun(event core.Event) error
}

// RouteResult contains the outcome of proposing a tool call.
type RouteResult struct {
	Proposal  core.ToolProposal          `json:"proposal"`
	Inference core.EffectInferenceResult `json:"inference"`
	Decision  core.PolicyDecision        `json:"decision"`
	Approval  *core.ApprovalRecord       `json:"approval,omitempty"`
	Result    *core.ToolResult           `json:"result,omitempty"`
}

// Router governs side effects.
type Router interface {
	Propose(ctx context.Context, runID, conversationID string, proposal core.ToolProposal) (*RouteResult, error)
	ExecuteApproved(ctx context.Context, approvalID string) (*core.ToolResult, error)
	ExecuteReadOnly(ctx context.Context, proposal core.ToolProposal) (*core.ToolResult, error)
}

// NotImplementedExecutor is a placeholder executor for not-yet-wired tools.
type NotImplementedExecutor struct {
	Tool string
}

// Execute returns a structured not-implemented response.
func (n NotImplementedExecutor) Execute(_ context.Context, input map[string]any) (*core.ToolResult, error) {
	return &core.ToolResult{
		ToolCallID: n.Tool,
		Output: map[string]any{
			"status": "not_implemented",
			"input":  core.CloneMap(input),
		},
		Error: "tool executor not implemented",
	}, nil
}
