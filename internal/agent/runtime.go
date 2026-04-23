package agent

import (
	"context"
	"errors"
	"fmt"
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
	Store          *repo.Store
	Planner        Planner
	Runner         einoapp.Runner
	ContextBuilder *ContextBuilder
	Router         tools.Router
	Approvals      *ApprovalCenter
	Events         EventWriter
	StateStore     *RunStateStore
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
	state := RunState{
		RunID:          runID,
		ConversationID: request.ConversationID,
		Status:         RunStatusReceived,
		CurrentStep:    "received_user_message",
		UserMessage:    request.Content,
		CreatedAt:      timeutil.NowUTC(),
		UpdatedAt:      timeutil.NowUTC(),
	}
	r.saveState(state)

	r.emit(ctx, sink, core.Event{
		Type:           "run.started",
		RunID:          runID,
		ConversationID: request.ConversationID,
		CreatedAt:      timeutil.NowUTC(),
	})
	r.transition(ctx, &state, RunStatusReceived, "received_user_message", sink, nil)

	userMessage := core.Message{
		ID:             ids.New("msg"),
		ConversationID: request.ConversationID,
		Role:           "user",
		Content:        request.Content,
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
	r.transition(ctx, &state, RunStatusContextBuilt, "BuildContextNode", sink, nil)

	plan, err := r.Planner.Plan(ctx, request.Content)
	if err != nil {
		r.fail(ctx, &state, err, sink)
		return nil, err
	}
	state.Plan = &plan
	r.transition(ctx, &state, RunStatusPlanned, "PlanNode", sink, nil)
	if plan.Preamble != "" {
		r.emit(ctx, sink, core.Event{
			Type:           "assistant.delta",
			RunID:          runID,
			ConversationID: request.ConversationID,
			Content:        plan.Preamble,
			CreatedAt:      time.Now().UTC(),
		})
	}

	response := &RunResponse{RunID: runID}

	if plan.ToolProposal != nil {
		proposal := cloneProposal(*plan.ToolProposal)
		state.Proposal = &proposal
		r.transition(ctx, &state, RunStatusToolProposed, "ToolProposalNode", sink, map[string]any{
			"tool":         proposal.Tool,
			"tool_call_id": proposal.ID,
		})

		outcome, err := r.Router.Propose(ctx, runID, request.ConversationID, *plan.ToolProposal)
		if err != nil {
			r.fail(ctx, &state, err, sink)
			return nil, err
		}
		state.Inference = &outcome.Inference
		r.transition(ctx, &state, RunStatusEffectInferred, "EffectInferenceNode", sink, map[string]any{
			"risk_level": outcome.Inference.RiskLevel,
			"effects":    outcome.Inference.Effects,
		})
		state.Policy = &outcome.Decision
		r.transition(ctx, &state, RunStatusPolicyDecided, "PolicyDecisionNode", sink, map[string]any{
			"requires_approval": outcome.Decision.RequiresApproval,
			"risk_level":        outcome.Decision.RiskLevel,
		})
		if outcome.Approval != nil {
			response.Approval = outcome.Approval
			state.ApprovalID = outcome.Approval.ID
			r.transition(ctx, &state, RunStatusApprovalRequested, "ApprovalInterruptNode", sink, map[string]any{
				"approval_id": outcome.Approval.ID,
				"risk_level":  outcome.Inference.RiskLevel,
			})
			r.emitNoRunLog(ctx, sink, core.Event{
				Type:           "approval.requested",
				RunID:          runID,
				ConversationID: request.ConversationID,
				ApprovalID:     outcome.Approval.ID,
				RiskLevel:      outcome.Inference.RiskLevel,
				Payload: map[string]any{
					"summary":        outcome.Approval.Summary,
					"input_snapshot": outcome.Approval.InputSnapshot,
				},
				CreatedAt: time.Now().UTC(),
			})
			r.transition(ctx, &state, RunStatusPausedForApproval, "ApprovalInterruptNode", sink, map[string]any{
				"approval_id": outcome.Approval.ID,
			})
			assistantMessage, err := r.persistAssistant(ctx, request.ConversationID, "该操作需要审批后才能执行。", 1, runID, sink)
			if err != nil {
				r.fail(ctx, &state, err, sink)
				return nil, err
			}
			response.AssistantMessage = assistantMessage
			response.State = cloneStatePtr(&state)
			r.emit(ctx, sink, core.Event{
				Type:           "run.paused",
				RunID:          runID,
				ConversationID: request.ConversationID,
				ApprovalID:     outcome.Approval.ID,
				CreatedAt:      time.Now().UTC(),
			})
			return response, nil
		}

		r.transition(ctx, &state, RunStatusToolExecuting, "ExecuteToolNode", sink, map[string]any{
			"tool":         outcome.Proposal.Tool,
			"tool_call_id": outcome.Proposal.ID,
		})
		response.ToolResult = outcome.Result
		state.ToolResult = outcome.Result
		if outcome.Result != nil && outcome.Result.Error != "" {
			state.Error = outcome.Result.Error
			r.transition(ctx, &state, RunStatusToolFailed, "ExecuteToolNode", sink, map[string]any{"error": outcome.Result.Error})
		} else {
			r.transition(ctx, &state, RunStatusToolCompleted, "ExecuteToolNode", sink, nil)
		}
		r.transition(ctx, &state, RunStatusAssistantSummarize, "SummarizeNode", sink, nil)
		summary := summarizeToolResult(outcome.Result)
		assistantMessage, err := r.persistAssistant(ctx, request.ConversationID, summary, 1, runID, sink)
		if err != nil {
			r.fail(ctx, &state, err, sink)
			return nil, err
		}
		response.AssistantMessage = assistantMessage
		if outcome.Result != nil {
			r.emitNoRunLog(ctx, sink, core.Event{
				Type:           "tool.output",
				RunID:          runID,
				ConversationID: request.ConversationID,
				ToolCallID:     outcome.Result.ToolCallID,
				Payload:        outcome.Result.Output,
				CreatedAt:      time.Now().UTC(),
			})
		}
		r.transition(ctx, &state, RunStatusCompleted, "PersistNode", sink, nil)
		response.State = cloneStatePtr(&state)
		r.emit(ctx, sink, core.Event{Type: "run.completed", RunID: runID, ConversationID: request.ConversationID, CreatedAt: time.Now().UTC()})
		return response, nil
	}

	r.transition(ctx, &state, RunStatusAssistantSummarize, "SummarizeNode", sink, nil)
	modelMessage, err := r.Runner.Run(ctx, einoapp.AgentInput{Messages: messages})
	if err != nil {
		r.fail(ctx, &state, err, sink)
		return nil, err
	}
	assistantMessage, err := r.persistAssistant(ctx, request.ConversationID, modelMessage.Content, 0, runID, sink)
	if err != nil {
		r.fail(ctx, &state, err, sink)
		return nil, err
	}
	response.AssistantMessage = assistantMessage
	r.transition(ctx, &state, RunStatusCompleted, "PersistNode", sink, nil)
	response.State = cloneStatePtr(&state)
	r.emit(ctx, sink, core.Event{Type: "run.completed", RunID: runID, ConversationID: request.ConversationID, CreatedAt: time.Now().UTC()})
	return response, nil
}

// ResumeApproval resumes a run by looking up the run id from the approval record.
func (r *Runtime) ResumeApproval(ctx context.Context, approvalID string, approved bool, sink EventSink) (*RunResponse, error) {
	if r.Approvals == nil {
		return nil, errors.New("approval center is not configured")
	}
	approval, err := r.Approvals.Get(approvalID)
	if err != nil {
		return nil, err
	}
	if approval.RunID == "" {
		return nil, errors.New("approval is not attached to a workflow run")
	}
	return r.ResumeRun(ctx, approval.RunID, approvalID, approved, sink)
}

// ResumeRun continues a paused run without re-planning or changing the approved snapshot.
func (r *Runtime) ResumeRun(ctx context.Context, runID, approvalID string, approved bool, sink EventSink) (*RunResponse, error) {
	if r.Approvals == nil {
		return nil, errors.New("approval center is not configured")
	}
	state, err := r.stateStore().Get(runID)
	if err != nil {
		return nil, err
	}
	if state.ApprovalID != approvalID {
		return nil, fmt.Errorf("approval %s does not belong to run %s", approvalID, runID)
	}
	if state.Status != RunStatusPausedForApproval {
		return nil, fmt.Errorf("run %s is not paused for approval", runID)
	}

	approval, err := r.Approvals.Get(approvalID)
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
		r.transition(ctx, state, RunStatusApprovalRejected, "ApprovalInterruptNode", sink, map[string]any{
			"approval_id": approvalID,
		})
		r.emit(ctx, sink, core.Event{
			Type:           "approval.rejected",
			RunID:          runID,
			ConversationID: state.ConversationID,
			ApprovalID:     approvalID,
			CreatedAt:      time.Now().UTC(),
		})
		assistantMessage, err := r.persistAssistant(ctx, state.ConversationID, "操作已被拒绝，未执行。", 0, runID, sink)
		if err != nil {
			r.fail(ctx, state, err, sink)
			return nil, err
		}
		response.AssistantMessage = assistantMessage
		response.State = cloneStatePtr(state)
		r.emit(ctx, sink, core.Event{
			Type:           "run.completed",
			RunID:          runID,
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
	r.transition(ctx, state, RunStatusApprovalApproved, "ApprovalInterruptNode", sink, map[string]any{
		"approval_id": approvalID,
	})
	r.emit(ctx, sink, core.Event{
		Type:           "approval.approved",
		RunID:          runID,
		ConversationID: state.ConversationID,
		ApprovalID:     approvalID,
		CreatedAt:      time.Now().UTC(),
	})

	r.transition(ctx, state, RunStatusToolExecuting, "ExecuteToolNode", sink, map[string]any{
		"approval_id":  approvalID,
		"tool":         approval.Proposal.Tool,
		"tool_call_id": approval.Proposal.ID,
	})
	result, err := r.Router.ExecuteApproved(ctx, approvalID)
	if err != nil {
		state.Error = err.Error()
		r.transition(ctx, state, RunStatusToolFailed, "ExecuteToolNode", sink, map[string]any{"error": err.Error()})
		r.fail(ctx, state, err, sink)
		return nil, err
	}
	state.ToolResult = result
	response.ToolResult = result
	r.transition(ctx, state, RunStatusToolCompleted, "ExecuteToolNode", sink, nil)
	if result != nil {
		r.emitNoRunLog(ctx, sink, core.Event{
			Type:           "tool.output",
			RunID:          runID,
			ConversationID: state.ConversationID,
			ApprovalID:     approvalID,
			ToolCallID:     result.ToolCallID,
			Payload:        result.Output,
			CreatedAt:      time.Now().UTC(),
		})
	}

	r.transition(ctx, state, RunStatusAssistantSummarize, "SummarizeNode", sink, nil)
	assistantMessage, err := r.persistAssistant(ctx, state.ConversationID, summarizeToolResult(result), 1, runID, sink)
	if err != nil {
		r.fail(ctx, state, err, sink)
		return nil, err
	}
	response.AssistantMessage = assistantMessage
	r.transition(ctx, state, RunStatusCompleted, "PersistNode", sink, nil)
	response.State = cloneStatePtr(state)
	r.emit(ctx, sink, core.Event{Type: "run.completed", RunID: runID, ConversationID: state.ConversationID, CreatedAt: time.Now().UTC()})
	return response, nil
}

func (r *Runtime) persistAssistant(ctx context.Context, conversationID, content string, toolCallCount int, runID string, sink EventSink) (*core.Message, error) {
	message := &core.Message{
		ID:             ids.New("msg"),
		ConversationID: conversationID,
		Role:           "assistant",
		Content:        content,
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

func (r *Runtime) transition(ctx context.Context, state *RunState, status RunStatus, step string, sink EventSink, payload map[string]any) {
	if state == nil {
		return
	}
	state.Status = status
	state.CurrentStep = step
	state.UpdatedAt = timeutil.NowUTC()
	r.saveState(*state)

	eventPayload := map[string]any{
		"status":       string(status),
		"current_step": step,
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
	r.emit(ctx, sink, core.Event{
		Type:           "run.state",
		RunID:          state.RunID,
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
	r.transition(ctx, state, RunStatusFailed, "PersistNode", sink, map[string]any{"error": err.Error()})
	r.emit(ctx, sink, core.Event{
		Type:           "run.failed",
		RunID:          state.RunID,
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

func (r *Runtime) saveState(state RunState) {
	r.stateStore().Save(state)
}

func (r *Runtime) stateStore() *RunStateStore {
	if r.StateStore == nil {
		r.StateStore = NewRunStateStore()
	}
	return r.StateStore
}

func cloneStatePtr(state *RunState) *RunState {
	if state == nil {
		return nil
	}
	cp := cloneRunState(*state)
	return &cp
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
	return "工具执行完成。"
}

func estimateTokens(content string) int {
	if content == "" {
		return 0
	}
	return len(content)/4 + 1
}
