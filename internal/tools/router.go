package tools

import (
	"context"
	"fmt"
	"time"

	"local-agent/internal/core"
)

// LocalRouter enforces inference, policy, approval, and execution boundaries.
type LocalRouter struct {
	registry  *Registry
	inferrer  EffectInferrer
	policy    PolicyDecider
	approvals ApprovalCenter
	events    EventWriter
}

// NewRouter creates a router.
func NewRouter(registry *Registry, inferrer EffectInferrer, policy PolicyDecider, approvals ApprovalCenter, events EventWriter) *LocalRouter {
	return &LocalRouter{
		registry:  registry,
		inferrer:  inferrer,
		policy:    policy,
		approvals: approvals,
		events:    events,
	}
}

// Propose infers effects and either auto-executes or queues approval.
func (r *LocalRouter) Propose(ctx context.Context, runID, conversationID string, proposal core.ToolProposal) (*RouteResult, error) {
	r.emit(core.Event{
		Type:           "tool.proposed",
		RunID:          runID,
		ConversationID: conversationID,
		Tool:           proposal.Tool,
		ToolCallID:     proposal.ID,
		Payload: map[string]any{
			"input": core.CloneMap(proposal.Input),
		},
		CreatedAt: time.Now().UTC(),
	})

	inference, err := r.inferrer.Infer(ctx, proposal)
	if err != nil {
		return nil, err
	}
	r.emit(core.Event{
		Type:           "effect.inferred",
		RunID:          runID,
		ConversationID: conversationID,
		Tool:           proposal.Tool,
		ToolCallID:     proposal.ID,
		RiskLevel:      inference.RiskLevel,
		Payload: map[string]any{
			"inference": inference,
		},
		CreatedAt: time.Now().UTC(),
	})

	decision, err := r.policy.Decide(ctx, proposal, inference)
	if err != nil {
		return nil, err
	}
	r.emit(core.Event{
		Type:           "policy.decided",
		RunID:          runID,
		ConversationID: conversationID,
		Tool:           proposal.Tool,
		ToolCallID:     proposal.ID,
		RiskLevel:      inference.RiskLevel,
		Payload: map[string]any{
			"decision": decision,
		},
		CreatedAt: time.Now().UTC(),
	})

	result := &RouteResult{
		Proposal:  proposal,
		Inference: inference,
		Decision:  decision,
	}

	if decision.RequiresApproval {
		approval, err := r.approvals.Create(runID, conversationID, proposal, inference, decision)
		if err != nil {
			return nil, err
		}
		result.Approval = approval

		r.emit(core.Event{
			Type:           "approval.requested",
			RunID:          runID,
			ConversationID: conversationID,
			ApprovalID:     approval.ID,
			RiskLevel:      inference.RiskLevel,
			Payload: map[string]any{
				"summary":        approval.Summary,
				"explanation":    approval.Explanation,
				"input_snapshot": approval.InputSnapshot,
				"risk_trace":     decision.RiskTrace,
			},
			CreatedAt: time.Now().UTC(),
		})

		return result, nil
	}

	if !decision.Allowed {
		now := time.Now().UTC()
		denied := &core.ToolResult{
			ToolCallID: proposal.ID,
			Output: map[string]any{
				"status":     "denied",
				"reason":     decision.Reason,
				"risk_trace": decision.RiskTrace,
			},
			Error:      decision.Reason,
			StartedAt:  now,
			FinishedAt: now,
		}
		result.Result = denied
		r.emit(core.Event{
			Type:           "policy.denied",
			RunID:          runID,
			ConversationID: conversationID,
			Tool:           proposal.Tool,
			ToolCallID:     proposal.ID,
			RiskLevel:      decision.RiskLevel,
			Payload: map[string]any{
				"decision": decision,
			},
			CreatedAt: now,
		})
		return result, nil
	}

	execResult, err := r.execute(ctx, proposal, runID, conversationID)
	if err != nil {
		return nil, err
	}
	result.Result = execResult
	return result, nil
}

// ExecuteApproved executes the exact approved snapshot.
func (r *LocalRouter) ExecuteApproved(ctx context.Context, approvalID string) (*core.ToolResult, error) {
	approval, err := r.approvals.Get(approvalID)
	if err != nil {
		return nil, err
	}
	if approval.Status != core.ApprovalApproved {
		return nil, fmt.Errorf("approval %s is not approved", approvalID)
	}
	if verifier, ok := r.approvals.(interface{ VerifySnapshotHash(string) error }); ok {
		if err := verifier.VerifySnapshotHash(approvalID); err != nil {
			return nil, err
		}
	}

	proposal := approval.Proposal
	proposal.Input = core.CloneMap(approval.InputSnapshot)

	return r.execute(ctx, proposal, approval.RunID, approval.ConversationID)
}

// ExecuteReadOnly executes only when policy already allows the proposal.
func (r *LocalRouter) ExecuteReadOnly(ctx context.Context, proposal core.ToolProposal) (*core.ToolResult, error) {
	inference, err := r.inferrer.Infer(ctx, proposal)
	if err != nil {
		return nil, err
	}
	decision, err := r.policy.Decide(ctx, proposal, inference)
	if err != nil {
		return nil, err
	}
	if decision.RequiresApproval {
		return nil, fmt.Errorf("proposal requires approval")
	}
	return r.execute(ctx, proposal, "", "")
}

func (r *LocalRouter) execute(ctx context.Context, proposal core.ToolProposal, runID, conversationID string) (*core.ToolResult, error) {
	executor, err := r.registry.Executor(proposal.Tool)
	if err != nil {
		return nil, err
	}

	r.emit(core.Event{
		Type:           "tool.started",
		RunID:          runID,
		ConversationID: conversationID,
		Tool:           proposal.Tool,
		ToolCallID:     proposal.ID,
		CreatedAt:      time.Now().UTC(),
	})

	result, execErr := executor.Execute(ctx, core.CloneMap(proposal.Input))
	if result != nil && result.ToolCallID == "" {
		result.ToolCallID = proposal.ID
	}

	if result != nil {
		r.emit(core.Event{
			Type:           "tool.output",
			RunID:          runID,
			ConversationID: conversationID,
			Tool:           proposal.Tool,
			ToolCallID:     proposal.ID,
			Payload:        core.CloneMap(result.Output),
			CreatedAt:      time.Now().UTC(),
		})
	}

	payload := map[string]any{}
	if result != nil {
		payload = core.CloneMap(result.Output)
	}
	if execErr != nil {
		payload["error"] = execErr.Error()
	}

	eventType := "tool.completed"
	if execErr != nil {
		eventType = "tool.failed"
	}
	r.emit(core.Event{
		Type:           eventType,
		RunID:          runID,
		ConversationID: conversationID,
		Tool:           proposal.Tool,
		ToolCallID:     proposal.ID,
		Payload:        payload,
		CreatedAt:      time.Now().UTC(),
	})

	return result, execErr
}

func (r *LocalRouter) emit(event core.Event) {
	if r.events == nil {
		return
	}
	_ = r.events.WriteRun(event)
}
