package agent

import (
	"errors"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"

	"local-agent/internal/core"
)

// RunStatus captures the explicit workflow state for a single agent run.
type RunStatus string

const (
	RunStatusReceived           RunStatus = "received_user_message"
	RunStatusContextBuilt       RunStatus = "context_built"
	RunStatusPlanned            RunStatus = "planned"
	RunStatusToolProposed       RunStatus = "tool_proposed"
	RunStatusEffectInferred     RunStatus = "effect_inferred"
	RunStatusPolicyDecided      RunStatus = "policy_decided"
	RunStatusApprovalRequested  RunStatus = "approval_requested"
	RunStatusPausedForApproval  RunStatus = "paused_for_approval"
	RunStatusApprovalApproved   RunStatus = "approval_approved"
	RunStatusApprovalRejected   RunStatus = "approval_rejected"
	RunStatusToolExecuting      RunStatus = "tool_executing"
	RunStatusToolCompleted      RunStatus = "tool_completed"
	RunStatusToolFailed         RunStatus = "tool_failed"
	RunStatusAssistantSummarize RunStatus = "assistant_summarizing"
	RunStatusCompleted          RunStatus = "completed"
	RunStatusFailed             RunStatus = "failed"
	RunStatusCancelled          RunStatus = "cancelled"
)

// AgentContext stores the prompt context assembled for a run.
type AgentContext struct {
	Messages []*schema.Message `json:"messages,omitempty"`
}

// RunState is the serializable workflow snapshot used for pause/resume.
type RunState struct {
	RunID          string                      `json:"run_id"`
	ConversationID string                      `json:"conversation_id"`
	Status         RunStatus                   `json:"status"`
	CurrentStep    string                      `json:"current_step,omitempty"`
	UserMessage    string                      `json:"user_message,omitempty"`
	Context        AgentContext                `json:"context,omitempty"`
	Plan           *Plan                       `json:"plan,omitempty"`
	Proposal       *core.ToolProposal          `json:"proposal,omitempty"`
	Inference      *core.EffectInferenceResult `json:"inference,omitempty"`
	Policy         *core.PolicyDecision        `json:"policy,omitempty"`
	ApprovalID     string                      `json:"approval_id,omitempty"`
	ToolResult     *core.ToolResult            `json:"tool_result,omitempty"`
	Error          string                      `json:"error,omitempty"`
	CreatedAt      time.Time                   `json:"created_at"`
	UpdatedAt      time.Time                   `json:"updated_at"`
}

// RunStateStore keeps resumable run state for the local single-user process.
type RunStateStore struct {
	mu     sync.RWMutex
	states map[string]RunState
}

// NewRunStateStore constructs an in-memory run state store.
func NewRunStateStore() *RunStateStore {
	return &RunStateStore{states: map[string]RunState{}}
}

// Save stores a copy of a run state.
func (s *RunStateStore) Save(state RunState) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[state.RunID] = cloneRunState(state)
}

// Get returns a copy of a run state.
func (s *RunStateStore) Get(runID string) (*RunState, error) {
	if s == nil {
		return nil, errors.New("run state store is not configured")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, ok := s.states[runID]
	if !ok {
		return nil, errors.New("run state not found")
	}
	cp := cloneRunState(state)
	return &cp, nil
}

func cloneRunState(state RunState) RunState {
	cp := state
	cp.Context.Messages = append([]*schema.Message(nil), state.Context.Messages...)
	if state.Plan != nil {
		plan := *state.Plan
		if state.Plan.ToolProposal != nil {
			proposal := cloneProposal(*state.Plan.ToolProposal)
			plan.ToolProposal = &proposal
		}
		cp.Plan = &plan
	}
	if state.Proposal != nil {
		proposal := cloneProposal(*state.Proposal)
		cp.Proposal = &proposal
	}
	if state.Inference != nil {
		inference := *state.Inference
		inference.Effects = append([]string(nil), state.Inference.Effects...)
		cp.Inference = &inference
	}
	if state.Policy != nil {
		policy := *state.Policy
		policy.ApprovalPayload = core.CloneMap(state.Policy.ApprovalPayload)
		cp.Policy = &policy
	}
	if state.ToolResult != nil {
		result := *state.ToolResult
		result.Output = core.CloneMap(state.ToolResult.Output)
		cp.ToolResult = &result
	}
	return cp
}

func cloneProposal(proposal core.ToolProposal) core.ToolProposal {
	cp := proposal
	cp.Input = core.CloneMap(proposal.Input)
	cp.ExpectedEffects = append([]string(nil), proposal.ExpectedEffects...)
	return cp
}
