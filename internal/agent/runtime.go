package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"local-agent/internal/core"
	"local-agent/internal/db/repo"
	"local-agent/internal/einoapp"
	"local-agent/internal/ids"
	"local-agent/internal/security"
	"local-agent/internal/timeutil"
	"local-agent/internal/tools"
)

// Runtime executes the end-to-end conversation loop.
type Runtime struct {
	Store            *repo.Store
	Planner          Planner
	Runner           einoapp.Runner
	ContextBuilder   *ContextBuilder
	Router           tools.Router
	Approvals        *ApprovalCenter
	Events           EventWriter
	StateStore       RunStateStore
	MaxWorkflowSteps int
}

var _ einoapp.AgentWorkflow = (*Runtime)(nil)

// Start runs the workflow and returns the captured event stream.
func (r *Runtime) Start(ctx context.Context, input einoapp.AgentInput) (einoapp.AgentEventStream, error) {
	sink := &memoryEventSink{}
	if _, err := r.Run(ctx, RunRequest{
		ConversationID: input.ConversationID,
		Content:        input.Message,
	}, sink); err != nil {
		return nil, err
	}
	return einoapp.NewSliceEventStream(sink.Events()), nil
}

// Resume resumes a paused workflow from an approval response.
func (r *Runtime) Resume(ctx context.Context, runID string, approvalID string, approved bool) (einoapp.AgentEventStream, error) {
	sink := &memoryEventSink{}
	if _, err := r.ResumeRun(ctx, runID, approvalID, approved, sink); err != nil {
		return nil, err
	}
	return einoapp.NewSliceEventStream(sink.Events()), nil
}

// Run executes one user message.
func (r *Runtime) Run(ctx context.Context, request RunRequest, sink EventSink) (*RunResponse, error) {
	runID := ids.New("run")
	now := timeutil.NowUTC()
	state := RunState{
		RunID:          runID,
		ConversationID: request.ConversationID,
		Status:         RunStatusReceived,
		CurrentStep:    string(RunStepTypeBuildContext),
		UserMessage:    request.Content,
		MaxSteps:       r.maxWorkflowSteps(),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := r.saveState(ctx, state); err != nil {
		return nil, err
	}

	r.emit(ctx, sink, core.Event{
		Type:           "run.started",
		RunID:          runID,
		ConversationID: request.ConversationID,
		CreatedAt:      now,
	})

	userMessage := core.Message{
		ID:             ids.New("msg"),
		ConversationID: request.ConversationID,
		Role:           "user",
		Content:        security.RedactString(request.Content),
		CreatedAt:      time.Now().UTC(),
	}
	if r.Store != nil && r.Store.Messages != nil {
		if err := r.Store.Messages.CreateMessage(ctx, userMessage); err != nil {
			return nil, err
		}
	}
	if r.Store != nil && r.Store.Conversations != nil && request.ConversationID != "" {
		_ = r.Store.Conversations.Touch(ctx, request.ConversationID)
	}

	messages, err := r.ContextBuilder.Build(ctx, request.ConversationID, request.Content)
	if err != nil {
		r.fail(ctx, &state, err, sink)
		return nil, err
	}
	state.Context = AgentContext{Messages: messages}
	contextStep, err := r.recordStep(ctx, &state, RunStepTypeBuildContext, RunStepStatusCompleted, func(step *RunStep) {
		step.Summary = "context built"
	})
	if err != nil {
		r.fail(ctx, &state, err, sink)
		return nil, err
	}
	r.transition(ctx, &state, RunStatusContextBuilt, contextStep, sink, nil)

	return r.continueWorkflow(ctx, &state, sink, nil)
}

// ResumeApproval resumes a run by looking up the run id from the approval record.
func (r *Runtime) ResumeApproval(ctx context.Context, approvalID string, approved bool, sink EventSink) (*RunResponse, error) {
	if r.Approvals == nil {
		return nil, errors.New("approval center is not configured")
	}
	approval, err := r.Approvals.Get(approvalID)
	if err != nil && !errors.Is(err, errApprovalNotFound) {
		return nil, err
	}
	if approval != nil && approval.RunID != "" {
		return r.ResumeRun(ctx, approval.RunID, approvalID, approved, sink)
	}

	items, err := r.stateStore().ListByStatus(ctx, []RunStatus{RunStatusPausedForApproval}, 100)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.ApprovalID == approvalID {
			return r.ResumeRun(ctx, item.RunID, approvalID, approved, sink)
		}
	}
	return nil, errors.New("approval is not attached to a workflow run")
}

// ResumeRun continues a paused run without re-planning or changing the approved snapshot.
func (r *Runtime) ResumeRun(ctx context.Context, runID, approvalID string, approved bool, sink EventSink) (*RunResponse, error) {
	if r.Approvals == nil {
		return nil, errors.New("approval center is not configured")
	}
	state, err := r.stateStore().Get(ctx, runID)
	if err != nil {
		return nil, err
	}
	if state.ApprovalID != approvalID {
		return nil, fmt.Errorf("approval %s does not belong to run %s", approvalID, runID)
	}
	switch state.Status {
	case RunStatusCompleted, RunStatusFailed, RunStatusCancelled:
		return nil, fmt.Errorf("run %s is already terminal (%s)", runID, state.Status)
	case RunStatusPausedForApproval:
	default:
		return nil, fmt.Errorf("run %s is not paused for approval", runID)
	}

	approval, err := r.hydrateApproval(ctx, runID, approvalID)
	if err != nil {
		return nil, err
	}
	if approval.RunID != runID {
		return nil, fmt.Errorf("approval %s is attached to run %s, not %s", approvalID, approval.RunID, runID)
	}

	response := &RunResponse{RunID: runID}
	if !approved {
		rejected, err := r.Approvals.Reject(approvalID, "rejected from workflow")
		if err != nil {
			r.fail(ctx, state, err, sink)
			return nil, err
		}
		response.Approval = rejected
		rejectStep, stepErr := r.recordStep(ctx, state, RunStepTypeRequestApproval, RunStepStatusCancelled, func(step *RunStep) {
			step.Approval = cloneApproval(*rejected)
			step.Summary = "approval rejected"
		})
		if stepErr != nil {
			r.fail(ctx, state, stepErr, sink)
			return nil, stepErr
		}
		r.transition(ctx, state, RunStatusApprovalRejected, rejectStep, sink, map[string]any{
			"approval_id": approvalID,
		})
		r.emit(ctx, sink, core.Event{
			Type:           "approval.rejected",
			RunID:          runID,
			StepID:         rejectStep.StepID,
			StepIndex:      rejectStep.Index,
			ConversationID: state.ConversationID,
			ApprovalID:     approvalID,
			CreatedAt:      time.Now().UTC(),
		})

		summaryStep, stepErr := r.recordStep(ctx, state, RunStepTypeSummarize, RunStepStatusCompleted, func(step *RunStep) {
			step.Summary = "操作已被拒绝，未执行。"
		})
		if stepErr != nil {
			r.fail(ctx, state, stepErr, sink)
			return nil, stepErr
		}
		r.transition(ctx, state, RunStatusAssistantSummarize, summaryStep, sink, nil)
		assistantMessage, err := r.persistAssistant(ctx, state.ConversationID, "操作已被拒绝，未执行。", 0, runID, sink)
		if err != nil {
			r.fail(ctx, state, err, sink)
			return nil, err
		}
		response.AssistantMessage = assistantMessage

		finalStep, stepErr := r.recordStep(ctx, state, RunStepTypeFinalize, RunStepStatusCompleted, func(step *RunStep) {
			step.Summary = "run completed after rejection"
		})
		if stepErr != nil {
			r.fail(ctx, state, stepErr, sink)
			return nil, stepErr
		}
		r.transition(ctx, state, RunStatusApprovalRejected, finalStep, sink, nil)
		response.State = cloneStatePtr(state)
		r.emit(ctx, sink, core.Event{
			Type:           "run.completed",
			RunID:          runID,
			StepID:         finalStep.StepID,
			StepIndex:      finalStep.Index,
			ConversationID: state.ConversationID,
			Payload:        map[string]any{"status": RunStatusApprovalRejected},
			CreatedAt:      time.Now().UTC(),
		})
		return response, nil
	}

	approvedRecord, err := r.Approvals.Approve(approvalID)
	if err != nil {
		r.fail(ctx, state, err, sink)
		return nil, err
	}
	response.Approval = approvedRecord
	approvalStep, stepErr := r.recordStep(ctx, state, RunStepTypeRequestApproval, RunStepStatusCompleted, func(step *RunStep) {
		step.Approval = cloneApproval(*approvedRecord)
		step.Summary = "approval approved"
	})
	if stepErr != nil {
		r.fail(ctx, state, stepErr, sink)
		return nil, stepErr
	}
	r.transition(ctx, state, RunStatusApprovalApproved, approvalStep, sink, map[string]any{
		"approval_id": approvalID,
	})
	r.emit(ctx, sink, core.Event{
		Type:           "approval.approved",
		RunID:          runID,
		StepID:         approvalStep.StepID,
		StepIndex:      approvalStep.Index,
		ConversationID: state.ConversationID,
		ApprovalID:     approvalID,
		CreatedAt:      time.Now().UTC(),
	})

	r.transition(ctx, state, RunStatusToolExecuting, approvalStep, sink, map[string]any{
		"approval_id":  approvalID,
		"tool":         approval.Proposal.Tool,
		"tool_call_id": approval.Proposal.ID,
	})
	result, err := r.Router.ExecuteApproved(ctx, approvalID)
	if err != nil {
		state.Error = err.Error()
		executeStep, stepErr := r.recordStep(ctx, state, RunStepTypeExecuteTool, RunStepStatusFailed, func(step *RunStep) {
			step.Proposal = &approval.Proposal
			step.Approval = cloneApproval(*approvedRecord)
			step.Error = err.Error()
		})
		if stepErr == nil {
			r.transition(ctx, state, RunStatusToolFailed, executeStep, sink, map[string]any{"error": err.Error()})
		}
		r.fail(ctx, state, err, sink)
		return nil, err
	}
	state.ToolResult = result
	executeStep, stepErr := r.recordStep(ctx, state, RunStepTypeExecuteTool, RunStepStatusCompleted, func(step *RunStep) {
		step.Proposal = &approval.Proposal
		step.Approval = cloneApproval(*approvedRecord)
		step.ToolResult = result
		step.Summary = "approved snapshot executed"
	})
	if stepErr != nil {
		r.fail(ctx, state, stepErr, sink)
		return nil, stepErr
	}
	r.transition(ctx, state, RunStatusToolCompleted, executeStep, sink, nil)
	if result != nil {
		r.emitNoRunLog(ctx, sink, core.Event{
			Type:           "tool.output",
			RunID:          runID,
			StepID:         executeStep.StepID,
			StepIndex:      executeStep.Index,
			ConversationID: state.ConversationID,
			ApprovalID:     approvalID,
			ToolCallID:     result.ToolCallID,
			Payload:        result.Output,
			CreatedAt:      time.Now().UTC(),
		})
	}
	state.ApprovalID = ""
	if err := r.saveState(ctx, *state); err != nil {
		return nil, err
	}
	return r.continueWorkflow(ctx, state, sink, result)
}

// ListRuns returns recent runs filtered by status.
func (r *Runtime) ListRuns(ctx context.Context, statuses []RunStatus, limit int) ([]RunState, error) {
	return r.stateStore().ListByStatus(ctx, statuses, limit)
}

// GetRun returns one run snapshot.
func (r *Runtime) GetRun(ctx context.Context, runID string) (*RunState, error) {
	return r.stateStore().Get(ctx, runID)
}

// ListRunSteps returns one run's step history.
func (r *Runtime) ListRunSteps(ctx context.Context, runID string) ([]RunStep, error) {
	return r.stateStore().ListSteps(ctx, runID)
}

// CancelRun marks a run as cancelled.
func (r *Runtime) CancelRun(ctx context.Context, runID string, sink EventSink) (*RunState, error) {
	state, err := r.stateStore().Get(ctx, runID)
	if err != nil {
		return nil, err
	}
	switch state.Status {
	case RunStatusCompleted, RunStatusFailed, RunStatusCancelled:
		return nil, fmt.Errorf("run %s is already terminal (%s)", runID, state.Status)
	}
	if state.ApprovalID != "" && r.Approvals != nil {
		if approval, err := r.Approvals.Get(state.ApprovalID); err == nil && approval.Status == core.ApprovalPending {
			_, _ = r.Approvals.Reject(state.ApprovalID, "run cancelled")
		}
	}
	cancelStep, err := r.recordStep(ctx, state, RunStepTypeCancel, RunStepStatusCancelled, func(step *RunStep) {
		step.Summary = "run cancelled"
	})
	if err != nil {
		return nil, err
	}
	r.transition(ctx, state, RunStatusCancelled, cancelStep, sink, nil)
	r.emit(ctx, sink, core.Event{
		Type:           "run.cancelled",
		RunID:          state.RunID,
		StepID:         cancelStep.StepID,
		StepIndex:      cancelStep.Index,
		ConversationID: state.ConversationID,
		CreatedAt:      time.Now().UTC(),
	})
	return cloneStatePtr(state), nil
}

func (r *Runtime) continueWorkflow(ctx context.Context, state *RunState, sink EventSink, lastToolResult *core.ToolResult) (*RunResponse, error) {
	response := &RunResponse{RunID: state.RunID}
	for {
		if state.StepCount >= state.MaxSteps {
			err := fmt.Errorf("workflow exceeded max steps (%d)", state.MaxSteps)
			r.fail(ctx, state, err, sink)
			return nil, err
		}
		state.StepCount++

		plan, err := r.Planner.Plan(ctx, PlanInput{
			ConversationID: state.ConversationID,
			UserMessage:    state.UserMessage,
			StepIndex:      state.StepCount,
			LastToolResult: lastToolResult,
			LastProposal:   state.Proposal,
		})
		if err != nil {
			r.fail(ctx, state, err, sink)
			return nil, err
		}
		plan = normalizePlan(plan, lastToolResult)
		state.Plan = &plan
		planStep, stepErr := r.recordStep(ctx, state, RunStepTypePlan, RunStepStatusCompleted, func(step *RunStep) {
			step.Summary = string(plan.Decision)
			if plan.Reason != "" {
				step.Summary = plan.Reason
			}
			if plan.PlannerSource != "" && !containsPlannerSource(step.Summary) {
				if step.Summary == "" {
					step.Summary = "planner_source=" + plan.PlannerSource
				} else {
					step.Summary = "planner_source=" + plan.PlannerSource + "; " + step.Summary
				}
			}
			step.CodePlan = cloneCodePlan(plan.CodePlan)
			if plan.ToolProposal != nil {
				proposal := cloneProposal(*plan.ToolProposal)
				step.Proposal = &proposal
			}
		})
		if stepErr != nil {
			r.fail(ctx, state, stepErr, sink)
			return nil, stepErr
		}
		r.transition(ctx, state, RunStatusPlanned, planStep, sink, map[string]any{
			"decision":        plan.Decision,
			"planner_source":  plan.PlannerSource,
			"candidate_count": plan.CandidateCount,
			"tool":            plannedTool(plan),
		})
		if plan.Preamble != "" {
			r.emit(ctx, sink, core.Event{
				Type:           "assistant.delta",
				RunID:          state.RunID,
				StepID:         planStep.StepID,
				StepIndex:      planStep.Index,
				ConversationID: state.ConversationID,
				Content:        plan.Preamble,
				CreatedAt:      time.Now().UTC(),
			})
		}

		switch plan.Decision {
		case PlanDecisionContinue:
			continueStep, err := r.recordStep(ctx, state, RunStepTypeContinue, RunStepStatusCompleted, func(step *RunStep) {
				step.Summary = plan.Reason
			})
			if err != nil {
				r.fail(ctx, state, err, sink)
				return nil, err
			}
			r.transition(ctx, state, RunStatusPlanned, continueStep, sink, map[string]any{
				"decision": plan.Decision,
			})
			continue
		case PlanDecisionAnswer:
			message := plan.Message
			if message == "" {
				modelMessage, err := r.Runner.Run(ctx, einoapp.AgentInput{Messages: state.Context.Messages})
				if err != nil {
					r.fail(ctx, state, err, sink)
					return nil, err
				}
				message = modelMessage.Content
			}
			return r.completeWithMessage(ctx, state, sink, response, message)
		case PlanDecisionStop:
			message := plan.Message
			if message == "" {
				message = summarizeToolResult(lastToolResult)
			}
			return r.completeWithMessage(ctx, state, sink, response, message)
		case PlanDecisionTool:
		default:
			err := fmt.Errorf("unsupported plan decision %q", plan.Decision)
			r.fail(ctx, state, err, sink)
			return nil, err
		}

		if plan.ToolProposal == nil {
			err := errors.New("planner decided tool execution without a tool proposal")
			r.fail(ctx, state, err, sink)
			return nil, err
		}
		proposal := cloneProposal(*plan.ToolProposal)
		state.Proposal = &proposal
		proposeStep, err := r.recordStep(ctx, state, RunStepTypeProposeTool, RunStepStatusCompleted, func(step *RunStep) {
			step.Proposal = &proposal
			step.Summary = proposal.Tool
		})
		if err != nil {
			r.fail(ctx, state, err, sink)
			return nil, err
		}
		r.transition(ctx, state, RunStatusToolProposed, proposeStep, sink, map[string]any{
			"tool":            proposal.Tool,
			"tool_call_id":    proposal.ID,
			"planner_source":  plan.PlannerSource,
			"candidate_count": plan.CandidateCount,
		})

		outcome, err := r.Router.Propose(ctx, state.RunID, state.ConversationID, *plan.ToolProposal)
		if err != nil {
			r.fail(ctx, state, err, sink)
			return nil, err
		}

		state.Inference = &outcome.Inference
		inferenceStep, err := r.recordStep(ctx, state, RunStepTypeInferEffect, RunStepStatusCompleted, func(step *RunStep) {
			step.Proposal = &proposal
			step.Inference = &outcome.Inference
			step.Summary = outcome.Inference.ReasonSummary
		})
		if err != nil {
			r.fail(ctx, state, err, sink)
			return nil, err
		}
		r.transition(ctx, state, RunStatusEffectInferred, inferenceStep, sink, map[string]any{
			"risk_level": outcome.Inference.RiskLevel,
			"effects":    outcome.Inference.Effects,
		})

		state.Policy = &outcome.Decision
		policyStep, err := r.recordStep(ctx, state, RunStepTypeDecidePolicy, RunStepStatusCompleted, func(step *RunStep) {
			step.Proposal = &proposal
			step.Inference = &outcome.Inference
			step.Policy = &outcome.Decision
			step.Summary = outcome.Decision.Reason
		})
		if err != nil {
			r.fail(ctx, state, err, sink)
			return nil, err
		}
		r.transition(ctx, state, RunStatusPolicyDecided, policyStep, sink, map[string]any{
			"requires_approval": outcome.Decision.RequiresApproval,
			"risk_level":        outcome.Decision.RiskLevel,
		})

		if outcome.Approval != nil {
			response.Approval = outcome.Approval
			state.ApprovalID = outcome.Approval.ID
			approvalStep, err := r.recordStep(ctx, state, RunStepTypeRequestApproval, RunStepStatusPaused, func(step *RunStep) {
				step.Proposal = &proposal
				step.Inference = &outcome.Inference
				step.Policy = &outcome.Decision
				step.Approval = cloneApproval(*outcome.Approval)
				step.Summary = outcome.Approval.Summary
			})
			if err != nil {
				r.fail(ctx, state, err, sink)
				return nil, err
			}
			r.transition(ctx, state, RunStatusApprovalRequested, approvalStep, sink, map[string]any{
				"approval_id": outcome.Approval.ID,
				"risk_level":  outcome.Inference.RiskLevel,
			})
			r.emitNoRunLog(ctx, sink, core.Event{
				Type:           "approval.requested",
				RunID:          state.RunID,
				StepID:         approvalStep.StepID,
				StepIndex:      approvalStep.Index,
				ConversationID: state.ConversationID,
				ApprovalID:     outcome.Approval.ID,
				RiskLevel:      outcome.Inference.RiskLevel,
				Payload: map[string]any{
					"summary":        outcome.Approval.Summary,
					"explanation":    outcome.Approval.Explanation,
					"input_snapshot": outcome.Approval.InputSnapshot,
					"risk_trace":     outcome.Decision.RiskTrace,
				},
				CreatedAt: time.Now().UTC(),
			})
			r.transition(ctx, state, RunStatusPausedForApproval, approvalStep, sink, map[string]any{
				"approval_id": outcome.Approval.ID,
			})
			assistantMessage, err := r.persistAssistant(ctx, state.ConversationID, "该操作需要审批后才能执行。", 1, state.RunID, sink)
			if err != nil {
				r.fail(ctx, state, err, sink)
				return nil, err
			}
			response.AssistantMessage = assistantMessage
			response.State = cloneStatePtr(state)
			r.emit(ctx, sink, core.Event{
				Type:           "run.paused",
				RunID:          state.RunID,
				StepID:         approvalStep.StepID,
				StepIndex:      approvalStep.Index,
				ConversationID: state.ConversationID,
				ApprovalID:     outcome.Approval.ID,
				CreatedAt:      time.Now().UTC(),
			})
			return response, nil
		}

		executeStatus := RunStepStatusCompleted
		if outcome.Result != nil && outcome.Result.Error != "" {
			executeStatus = RunStepStatusFailed
		}
		executeStep, err := r.recordStep(ctx, state, RunStepTypeExecuteTool, executeStatus, func(step *RunStep) {
			step.Proposal = &outcome.Proposal
			step.Inference = &outcome.Inference
			step.Policy = &outcome.Decision
			step.ToolResult = outcome.Result
			step.Summary = "tool executed"
			if outcome.Result != nil && outcome.Result.Error != "" {
				step.Error = outcome.Result.Error
			}
		})
		if err != nil {
			r.fail(ctx, state, err, sink)
			return nil, err
		}
		r.transition(ctx, state, RunStatusToolExecuting, executeStep, sink, map[string]any{
			"tool":         outcome.Proposal.Tool,
			"tool_call_id": outcome.Proposal.ID,
		})
		response.ToolResult = outcome.Result
		state.ToolResult = outcome.Result
		if outcome.Result != nil && outcome.Result.Error != "" {
			state.Error = outcome.Result.Error
			r.transition(ctx, state, RunStatusToolFailed, executeStep, sink, map[string]any{"error": outcome.Result.Error})
		} else {
			r.transition(ctx, state, RunStatusToolCompleted, executeStep, sink, nil)
		}
		if outcome.Result != nil {
			r.emitNoRunLog(ctx, sink, core.Event{
				Type:           "tool.output",
				RunID:          state.RunID,
				StepID:         executeStep.StepID,
				StepIndex:      executeStep.Index,
				ConversationID: state.ConversationID,
				ToolCallID:     outcome.Result.ToolCallID,
				Payload:        outcome.Result.Output,
				CreatedAt:      time.Now().UTC(),
			})
		}
		lastToolResult = outcome.Result
	}
}

func (r *Runtime) completeWithMessage(ctx context.Context, state *RunState, sink EventSink, response *RunResponse, message string) (*RunResponse, error) {
	summaryStep, err := r.recordStep(ctx, state, RunStepTypeSummarize, RunStepStatusCompleted, func(step *RunStep) {
		step.Summary = message
	})
	if err != nil {
		r.fail(ctx, state, err, sink)
		return nil, err
	}
	r.transition(ctx, state, RunStatusAssistantSummarize, summaryStep, sink, nil)
	assistantMessage, err := r.persistAssistant(ctx, state.ConversationID, message, 0, state.RunID, sink)
	if err != nil {
		r.fail(ctx, state, err, sink)
		return nil, err
	}
	response.AssistantMessage = assistantMessage

	finalStep, err := r.recordStep(ctx, state, RunStepTypeFinalize, RunStepStatusCompleted, func(step *RunStep) {
		step.Summary = "run completed"
	})
	if err != nil {
		r.fail(ctx, state, err, sink)
		return nil, err
	}
	r.transition(ctx, state, RunStatusCompleted, finalStep, sink, nil)
	response.State = cloneStatePtr(state)
	r.emit(ctx, sink, core.Event{
		Type:           "run.completed",
		RunID:          state.RunID,
		StepID:         finalStep.StepID,
		StepIndex:      finalStep.Index,
		ConversationID: state.ConversationID,
		CreatedAt:      time.Now().UTC(),
	})
	return response, nil
}

func (r *Runtime) persistAssistant(ctx context.Context, conversationID, content string, toolCallCount int, runID string, sink EventSink) (*core.Message, error) {
	message := &core.Message{
		ID:             ids.New("msg"),
		ConversationID: conversationID,
		Role:           "assistant",
		Content:        security.RedactString(content),
		CreatedAt:      time.Now().UTC(),
	}
	if r.Store != nil && r.Store.Messages != nil {
		if err := r.Store.Messages.CreateMessage(ctx, *message); err != nil {
			return nil, err
		}
	}
	if r.Store != nil && r.Store.Usage != nil {
		usage := core.MessageUsage{
			ID:             ids.New("usage"),
			MessageID:      message.ID,
			ConversationID: conversationID,
			InputTokens:    estimateTokens(content),
			OutputTokens:   estimateTokens(content),
			TotalTokens:    estimateTokens(content) * 2,
			ToolCallCount:  toolCallCount,
			CreatedAt:      time.Now().UTC(),
		}
		if err := r.Store.Usage.CreateUsage(ctx, usage); err != nil {
			return nil, err
		}
		_ = r.Store.Usage.IncrementRollup(ctx, conversationID, usage)
	}
	r.emit(ctx, sink, core.Event{
		Type:           "assistant.message",
		RunID:          runID,
		ConversationID: conversationID,
		Content:        content,
		CreatedAt:      time.Now().UTC(),
	})
	return message, nil
}

func (r *Runtime) recordStep(ctx context.Context, state *RunState, stepType RunStepType, status RunStepStatus, fill func(step *RunStep)) (*RunStep, error) {
	if state == nil {
		return nil, errors.New("run state is nil")
	}
	now := timeutil.NowUTC()
	step := &RunStep{
		StepID:    ids.New("step"),
		RunID:     state.RunID,
		Index:     state.CurrentStepIndex + 1,
		Type:      stepType,
		Status:    status,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if fill != nil {
		fill(step)
	}
	state.CurrentStep = string(stepType)
	state.CurrentStepIndex = step.Index
	state.UpdatedAt = now
	if err := r.stateStore().SaveStep(ctx, *step); err != nil {
		return nil, err
	}
	if err := r.saveState(ctx, *state); err != nil {
		return nil, err
	}
	return step, nil
}

func (r *Runtime) transition(ctx context.Context, state *RunState, status RunStatus, step *RunStep, sink EventSink, payload map[string]any) {
	if state == nil {
		return
	}
	state.Status = status
	if step != nil {
		state.CurrentStep = string(step.Type)
		state.CurrentStepIndex = step.Index
	}
	state.UpdatedAt = timeutil.NowUTC()
	_ = r.saveState(ctx, *state)

	eventPayload := map[string]any{
		"status":             string(status),
		"current_step":       state.CurrentStep,
		"current_step_index": state.CurrentStepIndex,
		"step_count":         state.StepCount,
		"max_steps":          state.MaxSteps,
	}
	for key, value := range payload {
		eventPayload[key] = value
	}
	if state.ApprovalID != "" {
		eventPayload["approval_id"] = state.ApprovalID
	}
	if state.Proposal != nil {
		eventPayload["tool"] = state.Proposal.Tool
		eventPayload["tool_call_id"] = state.Proposal.ID
	}
	if step != nil {
		eventPayload["step_id"] = step.StepID
		eventPayload["step_index"] = step.Index
	}
	r.emit(ctx, sink, core.Event{
		Type:           "run.state",
		RunID:          state.RunID,
		StepID:         stepID(step),
		StepIndex:      stepIndex(step),
		ConversationID: state.ConversationID,
		ApprovalID:     state.ApprovalID,
		Payload:        eventPayload,
		CreatedAt:      time.Now().UTC(),
	})
}

func (r *Runtime) fail(ctx context.Context, state *RunState, err error, sink EventSink) {
	if state == nil || err == nil {
		return
	}
	state.Error = err.Error()
	failStep, stepErr := r.recordStep(ctx, state, RunStepTypeFinalize, RunStepStatusFailed, func(step *RunStep) {
		step.Error = err.Error()
	})
	if stepErr == nil {
		r.transition(ctx, state, RunStatusFailed, failStep, sink, map[string]any{"error": err.Error()})
	}
	r.emit(ctx, sink, core.Event{
		Type:           "run.failed",
		RunID:          state.RunID,
		StepID:         stepID(failStep),
		StepIndex:      stepIndex(failStep),
		ConversationID: state.ConversationID,
		Content:        err.Error(),
		CreatedAt:      time.Now().UTC(),
	})
}

func (r *Runtime) emit(ctx context.Context, sink EventSink, event core.Event) {
	r.emitWithRunLog(ctx, sink, event, true)
}

func (r *Runtime) emitNoRunLog(ctx context.Context, sink EventSink, event core.Event) {
	r.emitWithRunLog(ctx, sink, event, false)
}

func (r *Runtime) emitWithRunLog(ctx context.Context, sink EventSink, event core.Event, writeRunLog bool) {
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	if sink != nil {
		sink.Emit(event)
	}
	if writeRunLog && r.Events != nil {
		_ = r.Events.WriteRun(event)
	}
	if r.Store != nil && r.Store.AgentEvents != nil {
		payload := security.RedactMap(event.Payload)
		content := security.RedactString(event.Content)
		if content != "" {
			if payload == nil {
				payload = map[string]any{}
			}
			payload["content"] = content
		}
		_ = r.Store.AgentEvents.CreateEvent(ctx, core.AgentEvent{
			ID:             ids.New("evt"),
			ConversationID: event.ConversationID,
			RunID:          event.RunID,
			EventType:      event.Type,
			Payload:        payload,
			CreatedAt:      event.CreatedAt,
		})
	}
}

func (r *Runtime) saveState(ctx context.Context, state RunState) error {
	return r.stateStore().Save(ctx, state)
}

func (r *Runtime) stateStore() RunStateStore {
	if r.StateStore == nil {
		r.StateStore = NewRunStateStore()
	}
	return r.StateStore
}

func (r *Runtime) maxWorkflowSteps() int {
	if r.MaxWorkflowSteps > 0 {
		return r.MaxWorkflowSteps
	}
	return 6
}

func (r *Runtime) hydrateApproval(ctx context.Context, runID, approvalID string) (*core.ApprovalRecord, error) {
	approval, err := r.Approvals.Get(approvalID)
	if err == nil {
		return approval, nil
	}
	if !errors.Is(err, errApprovalNotFound) {
		return nil, err
	}
	steps, stepErr := r.stateStore().ListSteps(ctx, runID)
	if stepErr != nil {
		return nil, stepErr
	}
	for idx := len(steps) - 1; idx >= 0; idx-- {
		if steps[idx].Approval == nil || steps[idx].Approval.ID != approvalID {
			continue
		}
		if err := r.Approvals.Hydrate(*steps[idx].Approval); err != nil {
			return nil, err
		}
		return r.Approvals.Get(approvalID)
	}
	return nil, fmt.Errorf("approval %s exists in neither approval store nor durable run state", approvalID)
}

func cloneStatePtr(state *RunState) *RunState {
	if state == nil {
		return nil
	}
	cp := cloneRunState(*state)
	return &cp
}

func normalizePlan(plan Plan, lastToolResult *core.ToolResult) Plan {
	if plan.Decision != "" {
		return clonePlan(plan)
	}
	switch {
	case plan.ToolProposal != nil:
		plan.Decision = PlanDecisionTool
	case plan.Message != "":
		plan.Decision = PlanDecisionAnswer
	case lastToolResult != nil:
		plan.Decision = PlanDecisionStop
	default:
		plan.Decision = PlanDecisionAnswer
	}
	return clonePlan(plan)
}

func containsPlannerSource(summary string) bool {
	return strings.Contains(summary, "planner_source=")
}

func plannedTool(plan Plan) string {
	if plan.ToolProposal == nil {
		return ""
	}
	return plan.ToolProposal.Tool
}

func stepID(step *RunStep) string {
	if step == nil {
		return ""
	}
	return step.StepID
}

func stepIndex(step *RunStep) int {
	if step == nil {
		return 0
	}
	return step.Index
}

type memoryEventSink struct {
	events []core.Event
}

func (s *memoryEventSink) Emit(event core.Event) {
	s.events = append(s.events, event)
}

func (s *memoryEventSink) Events() []core.Event {
	return append([]core.Event(nil), s.events...)
}

func summarizeToolResult(result *core.ToolResult) string {
	if result == nil {
		return "工具执行完成。"
	}
	if stdout, ok := result.Output["stdout"].(string); ok && stdout != "" {
		return fmt.Sprintf("工具执行完成，输出如下：\n%s", stdout)
	}
	if status, ok := result.Output["status"].(string); ok && status != "" {
		return "工具执行完成，状态为 " + status + "。"
	}
	if result.Error != "" {
		return "工具执行失败：" + result.Error
	}
	return "工具执行完成。"
}

func estimateTokens(content string) int {
	if content == "" {
		return 0
	}
	return len(content)/4 + 1
}
