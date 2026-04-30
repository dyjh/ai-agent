package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"

	"local-agent/internal/core"
	"local-agent/internal/db/repo"
	"local-agent/internal/security"
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

// RunStepType identifies a workflow step stage.
type RunStepType string

const (
	RunStepTypeBuildContext    RunStepType = "build_context"
	RunStepTypePlan            RunStepType = "plan"
	RunStepTypeContinue        RunStepType = "continue"
	RunStepTypeProposeTool     RunStepType = "propose_tool"
	RunStepTypeInferEffect     RunStepType = "infer_effect"
	RunStepTypeDecidePolicy    RunStepType = "decide_policy"
	RunStepTypeRequestApproval RunStepType = "request_approval"
	RunStepTypeExecuteTool     RunStepType = "execute_tool"
	RunStepTypeSummarize       RunStepType = "summarize"
	RunStepTypeFinalize        RunStepType = "finalize"
	RunStepTypeCancel          RunStepType = "cancel"
)

// RunStepStatus captures an individual step lifecycle.
type RunStepStatus string

const (
	RunStepStatusPending   RunStepStatus = "pending"
	RunStepStatusCompleted RunStepStatus = "completed"
	RunStepStatusPaused    RunStepStatus = "paused"
	RunStepStatusFailed    RunStepStatus = "failed"
	RunStepStatusCancelled RunStepStatus = "cancelled"
)

// AgentContext stores the prompt context assembled for a run.
type AgentContext struct {
	Messages []*schema.Message `json:"messages,omitempty"`
}

// RunStep is the serializable workflow trace for one step.
type RunStep struct {
	StepID         string                      `json:"step_id"`
	RunID          string                      `json:"run_id"`
	Index          int                         `json:"index"`
	Type           RunStepType                 `json:"type"`
	Status         RunStepStatus               `json:"status"`
	Route          string                      `json:"route,omitempty"`
	RouteSource    string                      `json:"route_source,omitempty"`
	PlannerSource  string                      `json:"planner_source,omitempty"`
	CandidateCount int                         `json:"candidate_count,omitempty"`
	PlannedTool    string                      `json:"planned_tool,omitempty"`
	CodePlan       *CodePlan                   `json:"code_plan,omitempty"`
	Proposal       *core.ToolProposal          `json:"proposal,omitempty"`
	Inference      *core.EffectInferenceResult `json:"inference,omitempty"`
	Policy         *core.PolicyDecision        `json:"policy,omitempty"`
	Approval       *core.ApprovalRecord        `json:"approval,omitempty"`
	ToolResult     *core.ToolResult            `json:"tool_result,omitempty"`
	Summary        string                      `json:"summary,omitempty"`
	Error          string                      `json:"error,omitempty"`
	CreatedAt      time.Time                   `json:"created_at"`
	UpdatedAt      time.Time                   `json:"updated_at"`
}

// RunState is the serializable workflow snapshot used for pause/resume.
type RunState struct {
	RunID            string                      `json:"run_id"`
	ConversationID   string                      `json:"conversation_id"`
	Status           RunStatus                   `json:"status"`
	CurrentStep      string                      `json:"current_step,omitempty"`
	CurrentStepIndex int                         `json:"current_step_index,omitempty"`
	StepCount        int                         `json:"step_count,omitempty"`
	MaxSteps         int                         `json:"max_steps,omitempty"`
	UserMessage      string                      `json:"user_message,omitempty"`
	Context          AgentContext                `json:"context,omitempty"`
	Plan             *Plan                       `json:"plan,omitempty"`
	Proposal         *core.ToolProposal          `json:"proposal,omitempty"`
	Inference        *core.EffectInferenceResult `json:"inference,omitempty"`
	Policy           *core.PolicyDecision        `json:"policy,omitempty"`
	ApprovalID       string                      `json:"approval_id,omitempty"`
	ToolResult       *core.ToolResult            `json:"tool_result,omitempty"`
	Error            string                      `json:"error,omitempty"`
	CreatedAt        time.Time                   `json:"created_at"`
	UpdatedAt        time.Time                   `json:"updated_at"`
}

// RunStateStore persists run snapshots and step history.
type RunStateStore interface {
	Save(ctx context.Context, state RunState) error
	Get(ctx context.Context, runID string) (*RunState, error)
	ListByStatus(ctx context.Context, statuses []RunStatus, limit int) ([]RunState, error)
	SaveStep(ctx context.Context, step RunStep) error
	ListSteps(ctx context.Context, runID string) ([]RunStep, error)
	Delete(ctx context.Context, runID string) error
}

// InMemoryRunStateStore keeps resumable run state inside the current process.
type InMemoryRunStateStore struct {
	mu     sync.RWMutex
	states map[string]RunState
	steps  map[string][]RunStep
}

// PersistentRunStateStore persists redacted run state snapshots and durable step history.
type PersistentRunStateStore struct {
	runs  repo.AgentRunRepository
	steps repo.AgentRunStepRepository
}

// NewRunStateStore constructs an in-memory run state store.
func NewRunStateStore() *InMemoryRunStateStore {
	return &InMemoryRunStateStore{
		states: map[string]RunState{},
		steps:  map[string][]RunStep{},
	}
}

// NewPersistentRunStateStore constructs a durable store backed by repositories.
func NewPersistentRunStateStore(runs repo.AgentRunRepository, steps repo.AgentRunStepRepository) *PersistentRunStateStore {
	return &PersistentRunStateStore{runs: runs, steps: steps}
}

// Save stores a copy of a run state.
func (s *InMemoryRunStateStore) Save(_ context.Context, state RunState) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[state.RunID] = cloneRunState(state)
	return nil
}

// Get returns a copy of a run state.
func (s *InMemoryRunStateStore) Get(_ context.Context, runID string) (*RunState, error) {
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

// ListByStatus returns recent run snapshots by status.
func (s *InMemoryRunStateStore) ListByStatus(_ context.Context, statuses []RunStatus, limit int) ([]RunState, error) {
	if s == nil {
		return nil, errors.New("run state store is not configured")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	allowed := map[RunStatus]struct{}{}
	for _, status := range statuses {
		allowed[status] = struct{}{}
	}
	items := make([]RunState, 0, len(s.states))
	for _, state := range s.states {
		if len(allowed) > 0 {
			if _, ok := allowed[state.Status]; !ok {
				continue
			}
		}
		items = append(items, cloneRunState(state))
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

// SaveStep stores a copy of a step.
func (s *InMemoryRunStateStore) SaveStep(_ context.Context, step RunStep) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items := append([]RunStep(nil), s.steps[step.RunID]...)
	replaced := false
	for idx := range items {
		if items[idx].StepID == step.StepID {
			items[idx] = cloneRunStep(step)
			replaced = true
			break
		}
	}
	if !replaced {
		items = append(items, cloneRunStep(step))
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Index < items[j].Index
	})
	s.steps[step.RunID] = items
	return nil
}

// ListSteps returns copies of the step history.
func (s *InMemoryRunStateStore) ListSteps(_ context.Context, runID string) ([]RunStep, error) {
	if s == nil {
		return nil, errors.New("run state store is not configured")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := append([]RunStep(nil), s.steps[runID]...)
	out := make([]RunStep, 0, len(items))
	for _, step := range items {
		out = append(out, cloneRunStep(step))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Index < out[j].Index
	})
	return out, nil
}

// Delete removes a run snapshot and its steps.
func (s *InMemoryRunStateStore) Delete(_ context.Context, runID string) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.states, runID)
	delete(s.steps, runID)
	return nil
}

// Save persists a redacted run snapshot.
func (s *PersistentRunStateStore) Save(ctx context.Context, state RunState) error {
	if s == nil || s.runs == nil {
		return errors.New("persistent run state store is not configured")
	}
	return s.runs.UpsertRun(ctx, toRunRecord(state))
}

// Get returns the stored run snapshot.
func (s *PersistentRunStateStore) Get(ctx context.Context, runID string) (*RunState, error) {
	if s == nil || s.runs == nil {
		return nil, errors.New("persistent run state store is not configured")
	}
	record, err := s.runs.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	state, err := runStateFromRecord(*record)
	if err != nil {
		return nil, err
	}
	return &state, nil
}

// ListByStatus returns run snapshots filtered by status.
func (s *PersistentRunStateStore) ListByStatus(ctx context.Context, statuses []RunStatus, limit int) ([]RunState, error) {
	if s == nil || s.runs == nil {
		return nil, errors.New("persistent run state store is not configured")
	}
	names := make([]string, 0, len(statuses))
	for _, status := range statuses {
		names = append(names, string(status))
	}
	records, err := s.runs.ListRunsByStatus(ctx, names, limit)
	if err != nil {
		return nil, err
	}
	items := make([]RunState, 0, len(records))
	for _, record := range records {
		state, err := runStateFromRecord(record)
		if err != nil {
			return nil, err
		}
		items = append(items, state)
	}
	return items, nil
}

// SaveStep persists step history.
func (s *PersistentRunStateStore) SaveStep(ctx context.Context, step RunStep) error {
	if s == nil || s.steps == nil {
		return errors.New("persistent run state store is not configured")
	}
	return s.steps.UpsertStep(ctx, toRunStepRecord(step))
}

// ListSteps returns step history for a run.
func (s *PersistentRunStateStore) ListSteps(ctx context.Context, runID string) ([]RunStep, error) {
	if s == nil || s.steps == nil {
		return nil, errors.New("persistent run state store is not configured")
	}
	records, err := s.steps.ListStepsByRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	items := make([]RunStep, 0, len(records))
	for _, record := range records {
		step, err := runStepFromRecord(record)
		if err != nil {
			return nil, err
		}
		items = append(items, step)
	}
	return items, nil
}

// Delete removes persisted run state and steps.
func (s *PersistentRunStateStore) Delete(ctx context.Context, runID string) error {
	if s == nil {
		return errors.New("persistent run state store is not configured")
	}
	if s.steps != nil {
		if err := s.steps.DeleteStepsByRun(ctx, runID); err != nil {
			return err
		}
	}
	if s.runs != nil {
		return s.runs.DeleteRun(ctx, runID)
	}
	return nil
}

func cloneRunState(state RunState) RunState {
	cp := state
	cp.Context.Messages = cloneMessages(state.Context.Messages)
	if state.Plan != nil {
		plan := clonePlan(*state.Plan)
		cp.Plan = &plan
	}
	if state.Proposal != nil {
		proposal := cloneProposal(*state.Proposal)
		cp.Proposal = &proposal
	}
	if state.Inference != nil {
		inference := *state.Inference
		inference.Effects = append([]string(nil), state.Inference.Effects...)
		inference.Signals = append([]string(nil), state.Inference.Signals...)
		cp.Inference = &inference
	}
	if state.Policy != nil {
		policy := *state.Policy
		policy.ApprovalPayload = core.CloneMap(state.Policy.ApprovalPayload)
		policy.RiskTrace = cloneRiskTrace(state.Policy.RiskTrace)
		cp.Policy = &policy
	}
	if state.ToolResult != nil {
		result := *state.ToolResult
		result.Output = core.CloneMap(state.ToolResult.Output)
		cp.ToolResult = &result
	}
	return cp
}

func cloneRunStep(step RunStep) RunStep {
	cp := step
	cp.CodePlan = cloneCodePlan(step.CodePlan)
	if step.Proposal != nil {
		proposal := cloneProposal(*step.Proposal)
		cp.Proposal = &proposal
	}
	if step.Inference != nil {
		inference := *step.Inference
		inference.Effects = append([]string(nil), step.Inference.Effects...)
		inference.Signals = append([]string(nil), step.Inference.Signals...)
		cp.Inference = &inference
	}
	if step.Policy != nil {
		policy := *step.Policy
		policy.ApprovalPayload = core.CloneMap(step.Policy.ApprovalPayload)
		policy.RiskTrace = cloneRiskTrace(step.Policy.RiskTrace)
		cp.Policy = &policy
	}
	if step.Approval != nil {
		cp.Approval = cloneApproval(*step.Approval)
	}
	if step.ToolResult != nil {
		result := *step.ToolResult
		result.Output = core.CloneMap(step.ToolResult.Output)
		cp.ToolResult = &result
	}
	return cp
}

func cloneMessages(messages []*schema.Message) []*schema.Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]*schema.Message, 0, len(messages))
	for _, message := range messages {
		if message == nil {
			continue
		}
		cp := *message
		out = append(out, &cp)
	}
	return out
}

func cloneProposal(proposal core.ToolProposal) core.ToolProposal {
	cp := proposal
	cp.Input = core.CloneMap(proposal.Input)
	cp.ExpectedEffects = append([]string(nil), proposal.ExpectedEffects...)
	return cp
}

func sanitizeRunState(state RunState) RunState {
	cp := cloneRunState(state)
	cp.UserMessage = security.RedactString(cp.UserMessage)
	cp.Error = security.RedactString(cp.Error)
	for idx := range cp.Context.Messages {
		if cp.Context.Messages[idx] == nil {
			continue
		}
		cp.Context.Messages[idx].Content = security.RedactString(cp.Context.Messages[idx].Content)
	}
	if cp.Plan != nil {
		cp.Plan.Message = security.RedactString(cp.Plan.Message)
		cp.Plan.Reason = security.RedactString(cp.Plan.Reason)
		if cp.Plan.ToolProposal != nil {
			cp.Plan.ToolProposal.Input = security.RedactMap(cp.Plan.ToolProposal.Input)
		}
		sanitizeCodePlan(cp.Plan.CodePlan)
	}
	if cp.Proposal != nil {
		cp.Proposal.Input = security.RedactMap(cp.Proposal.Input)
	}
	if cp.Policy != nil {
		cp.Policy.ApprovalPayload = security.RedactMap(cp.Policy.ApprovalPayload)
		cp.Policy.Reason = security.RedactString(cp.Policy.Reason)
		if cp.Policy.RiskTrace != nil {
			cp.Policy.RiskTrace.Reason = security.RedactString(cp.Policy.RiskTrace.Reason)
		}
	}
	if cp.ToolResult != nil {
		cp.ToolResult.Output = security.RedactMap(cp.ToolResult.Output)
		cp.ToolResult.Error = security.RedactString(cp.ToolResult.Error)
	}
	return cp
}

func sanitizeRunStep(step RunStep) RunStep {
	cp := cloneRunStep(step)
	cp.Summary = security.RedactString(cp.Summary)
	cp.Error = security.RedactString(cp.Error)
	if cp.CodePlan != nil {
		sanitizeCodePlan(cp.CodePlan)
	}
	if cp.Proposal != nil {
		cp.Proposal.Input = security.RedactMap(cp.Proposal.Input)
	}
	if cp.Policy != nil {
		cp.Policy.ApprovalPayload = security.RedactMap(cp.Policy.ApprovalPayload)
		cp.Policy.Reason = security.RedactString(cp.Policy.Reason)
		if cp.Policy.RiskTrace != nil {
			cp.Policy.RiskTrace.Reason = security.RedactString(cp.Policy.RiskTrace.Reason)
		}
	}
	if cp.ToolResult != nil {
		cp.ToolResult.Output = security.RedactMap(cp.ToolResult.Output)
		cp.ToolResult.Error = security.RedactString(cp.ToolResult.Error)
	}
	if cp.Approval != nil {
		cp.Approval.Summary = security.RedactString(cp.Approval.Summary)
		cp.Approval.Reason = security.RedactString(cp.Approval.Reason)
		if cp.Approval.Explanation != nil {
			cp.Approval.Explanation.Summary = security.RedactString(cp.Approval.Explanation.Summary)
			cp.Approval.Explanation.WhyNeeded = security.RedactString(cp.Approval.Explanation.WhyNeeded)
			cp.Approval.Explanation.RollbackPlan = security.RedactMap(cp.Approval.Explanation.RollbackPlan)
		}
	}
	return cp
}

func cloneRiskTrace(trace *core.RiskTrace) *core.RiskTrace {
	if trace == nil {
		return nil
	}
	cp := *trace
	cp.Effects = append([]string(nil), trace.Effects...)
	cp.Signals = append([]string(nil), trace.Signals...)
	return &cp
}

func sanitizeCodePlan(plan *CodePlan) {
	if plan == nil {
		return
	}
	plan.Goal = security.RedactString(plan.Goal)
	for idx := range plan.Steps {
		plan.Steps[idx].Purpose = security.RedactString(plan.Steps[idx].Purpose)
		plan.Steps[idx].Input = security.RedactMap(plan.Steps[idx].Input)
	}
}

func toRunRecord(state RunState) core.AgentRunRecord {
	sanitized := sanitizeRunState(state)
	return core.AgentRunRecord{
		RunID:            state.RunID,
		ConversationID:   state.ConversationID,
		Status:           string(state.Status),
		CurrentStep:      state.CurrentStep,
		CurrentStepIndex: state.CurrentStepIndex,
		StepCount:        state.StepCount,
		MaxSteps:         state.MaxSteps,
		UserMessage:      security.RedactString(state.UserMessage),
		ApprovalID:       state.ApprovalID,
		Error:            security.RedactString(state.Error),
		StateJSON:        marshalToMap(sanitized),
		CreatedAt:        state.CreatedAt,
		UpdatedAt:        state.UpdatedAt,
	}
}

func runStateFromRecord(record core.AgentRunRecord) (RunState, error) {
	var state RunState
	if len(record.StateJSON) > 0 {
		if err := unmarshalFromMap(record.StateJSON, &state); err != nil {
			return RunState{}, err
		}
	}
	state.RunID = record.RunID
	state.ConversationID = record.ConversationID
	state.Status = RunStatus(record.Status)
	state.CurrentStep = record.CurrentStep
	state.CurrentStepIndex = record.CurrentStepIndex
	state.StepCount = record.StepCount
	state.MaxSteps = record.MaxSteps
	state.UserMessage = record.UserMessage
	state.ApprovalID = record.ApprovalID
	state.Error = record.Error
	state.CreatedAt = record.CreatedAt
	state.UpdatedAt = record.UpdatedAt
	return state, nil
}

func toRunStepRecord(step RunStep) core.AgentRunStepRecord {
	sanitized := sanitizeRunStep(step)
	record := core.AgentRunStepRecord{
		StepID:         step.StepID,
		RunID:          step.RunID,
		StepIndex:      step.Index,
		StepType:       string(step.Type),
		Status:         string(step.Status),
		CodePlanJSON:   marshalToMap(sanitized.CodePlan),
		ProposalJSON:   marshalToMap(sanitized.Proposal),
		InferenceJSON:  marshalToMap(sanitized.Inference),
		PolicyJSON:     marshalToMap(sanitized.Policy),
		ToolResultJSON: marshalToMap(sanitized.ToolResult),
		Summary:        sanitized.Summary,
		Error:          sanitized.Error,
		CreatedAt:      step.CreatedAt,
		UpdatedAt:      step.UpdatedAt,
	}
	if step.Approval != nil {
		record.ApprovalJSON = marshalToMap(*step.Approval)
	}
	return record
}

func runStepFromRecord(record core.AgentRunStepRecord) (RunStep, error) {
	step := RunStep{
		StepID:    record.StepID,
		RunID:     record.RunID,
		Index:     record.StepIndex,
		Type:      RunStepType(record.StepType),
		Status:    RunStepStatus(record.Status),
		Summary:   record.Summary,
		Error:     record.Error,
		CreatedAt: record.CreatedAt,
		UpdatedAt: record.UpdatedAt,
	}
	if len(record.ProposalJSON) > 0 {
		var proposal core.ToolProposal
		if err := unmarshalFromMap(record.ProposalJSON, &proposal); err != nil {
			return RunStep{}, err
		}
		step.Proposal = &proposal
	}
	if len(record.CodePlanJSON) > 0 {
		var codePlan CodePlan
		if err := unmarshalFromMap(record.CodePlanJSON, &codePlan); err != nil {
			return RunStep{}, err
		}
		step.CodePlan = &codePlan
	}
	if len(record.InferenceJSON) > 0 {
		var inference core.EffectInferenceResult
		if err := unmarshalFromMap(record.InferenceJSON, &inference); err != nil {
			return RunStep{}, err
		}
		step.Inference = &inference
	}
	if len(record.PolicyJSON) > 0 {
		var policy core.PolicyDecision
		if err := unmarshalFromMap(record.PolicyJSON, &policy); err != nil {
			return RunStep{}, err
		}
		step.Policy = &policy
	}
	if len(record.ApprovalJSON) > 0 {
		var approval core.ApprovalRecord
		if err := unmarshalFromMap(record.ApprovalJSON, &approval); err != nil {
			return RunStep{}, err
		}
		step.Approval = &approval
	}
	if len(record.ToolResultJSON) > 0 {
		var result core.ToolResult
		if err := unmarshalFromMap(record.ToolResultJSON, &result); err != nil {
			return RunStep{}, err
		}
		step.ToolResult = &result
	}
	return step, nil
}

func marshalToMap(value any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{}
	}
	return out
}

func unmarshalFromMap(input map[string]any, target any) error {
	raw, err := json.Marshal(input)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}
