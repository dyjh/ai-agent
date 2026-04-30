package tests

import (
	"context"
	"strings"
	"testing"
	"time"

	"local-agent/internal/agent"
	"local-agent/internal/config"
	"local-agent/internal/core"
	"local-agent/internal/db/repo"
	"local-agent/internal/einoapp"
	toolscore "local-agent/internal/tools"
)

func TestWorkflowReadOnlyAutoExecutes(t *testing.T) {
	runtime, planner, executor := newWorkflowRuntime(core.ToolProposal{
		ID:        "tool_read",
		Tool:      "ops.local.system_info",
		Input:     map[string]any{},
		Purpose:   "read local system info",
		CreatedAt: time.Now().UTC(),
	})

	events := startWorkflow(t, runtime, "conv_read", "system info")
	runID := eventValue(t, events, "run.started").RunID

	state, err := runtime.StateStore.Get(context.Background(), runID)
	if err != nil {
		t.Fatalf("Get state: %v", err)
	}
	if state.Status != agent.RunStatusCompleted {
		t.Fatalf("state status = %s, want completed", state.Status)
	}
	if planner.calls != 2 {
		t.Fatalf("planner calls = %d, want 2", planner.calls)
	}
	if executor.count != 1 {
		t.Fatalf("executor count = %d, want 1", executor.count)
	}
	assertHasRunStatus(t, events, agent.RunStatusPolicyDecided)
	assertHasRunStatus(t, events, agent.RunStatusToolCompleted)
	assertHasEvent(t, events, "run.completed")
}

func TestWorkflowApprovalResumeUsesSnapshotWithoutReplanning(t *testing.T) {
	proposal := core.ToolProposal{
		ID:        "tool_write",
		Tool:      "shell.exec",
		Input:     map[string]any{"command": "pnpm add axios"},
		Purpose:   "install dependency",
		CreatedAt: time.Now().UTC(),
	}
	runtime, planner, executor := newWorkflowRuntime(proposal)

	events := startWorkflow(t, runtime, "conv_approval", "install axios")
	approvalEvent := eventValue(t, events, "approval.requested")
	runID := approvalEvent.RunID
	approvalID := approvalEvent.ApprovalID

	state, err := runtime.StateStore.Get(context.Background(), runID)
	if err != nil {
		t.Fatalf("Get state: %v", err)
	}
	if state.Status != agent.RunStatusPausedForApproval {
		t.Fatalf("state status = %s, want paused_for_approval", state.Status)
	}
	if executor.count != 0 {
		t.Fatalf("executor count before approval = %d, want 0", executor.count)
	}

	planner.plan.ToolProposal.Input = map[string]any{"command": "pnpm add lodash"}
	resumeStream, err := runtime.Resume(context.Background(), runID, approvalID, true)
	if err != nil {
		t.Fatalf("Resume approve: %v", err)
	}
	resumeEvents, err := einoapp.DrainEventStream(context.Background(), resumeStream)
	if err != nil {
		t.Fatalf("drain resume events: %v", err)
	}

	if planner.calls != 2 {
		t.Fatalf("planner calls after resume = %d, want 2", planner.calls)
	}
	if executor.count != 1 {
		t.Fatalf("executor count after resume = %d, want 1", executor.count)
	}
	if got := executor.lastInput["command"]; got != "pnpm add axios" {
		t.Fatalf("executed command = %v, want approved snapshot", got)
	}
	state, err = runtime.StateStore.Get(context.Background(), runID)
	if err != nil {
		t.Fatalf("Get final state: %v", err)
	}
	if state.Status != agent.RunStatusCompleted {
		t.Fatalf("final state status = %s, want completed", state.Status)
	}
	assertHasRunStatus(t, resumeEvents, agent.RunStatusApprovalApproved)
	assertHasRunStatus(t, resumeEvents, agent.RunStatusToolCompleted)

	if _, err := runtime.Resume(context.Background(), runID, approvalID, true); err == nil {
		t.Fatalf("expected second resume to fail")
	}
	if executor.count != 1 {
		t.Fatalf("executor count after second resume = %d, want 1", executor.count)
	}
	if len(planner.inputs) < 2 || planner.inputs[1].LastToolResult == nil {
		t.Fatalf("expected follow-up planning only after tool execution")
	}
}

func TestWorkflowApprovalRejectDoesNotExecute(t *testing.T) {
	runtime, _, executor := newWorkflowRuntime(core.ToolProposal{
		ID:        "tool_reject",
		Tool:      "shell.exec",
		Input:     map[string]any{"command": "pnpm add axios"},
		Purpose:   "install dependency",
		CreatedAt: time.Now().UTC(),
	})

	events := startWorkflow(t, runtime, "conv_reject", "install axios")
	approvalEvent := eventValue(t, events, "approval.requested")

	resumeStream, err := runtime.Resume(context.Background(), approvalEvent.RunID, approvalEvent.ApprovalID, false)
	if err != nil {
		t.Fatalf("Resume reject: %v", err)
	}
	resumeEvents, err := einoapp.DrainEventStream(context.Background(), resumeStream)
	if err != nil {
		t.Fatalf("drain reject events: %v", err)
	}

	if executor.count != 0 {
		t.Fatalf("executor count = %d, want 0", executor.count)
	}
	state, err := runtime.StateStore.Get(context.Background(), approvalEvent.RunID)
	if err != nil {
		t.Fatalf("Get state: %v", err)
	}
	if state.Status != agent.RunStatusApprovalRejected {
		t.Fatalf("state status = %s, want approval_rejected", state.Status)
	}
	approval, err := runtime.Approvals.Get(approvalEvent.ApprovalID)
	if err != nil {
		t.Fatalf("Get approval: %v", err)
	}
	if approval.Status != core.ApprovalRejected {
		t.Fatalf("approval status = %s, want rejected", approval.Status)
	}
	assertHasEvent(t, resumeEvents, "approval.rejected")
}

func TestWorkflowAnswerModeRunnerIgnoresPlannerMessage(t *testing.T) {
	runtime, planner, _ := newWorkflowRuntime(core.ToolProposal{})
	planner.plan = agent.Plan{
		Decision:    agent.PlanDecisionAnswer,
		AnswerMode:  agent.AnswerModeRunner,
		Route:       "direct_answer",
		RouteSource: "conversation_router_lightweight",
		Message:     "planner message must not be persisted",
	}
	events := startWorkflow(t, runtime, "conv_runner_mode", "普通聊天问题")
	var planned core.Event
	for _, event := range events {
		if event.Type == "run.state" && event.Payload["status"] == string(agent.RunStatusPlanned) {
			planned = event
			break
		}
	}
	if planned.Type == "" {
		t.Fatalf("missing planned event in %v", eventTypes(events))
	}
	if planned.Payload["route"] != "direct_answer" || planned.Payload["route_source"] != "conversation_router_lightweight" {
		t.Fatalf("planned payload = %+v, want route trace", planned.Payload)
	}
	steps, err := runtime.StateStore.ListSteps(context.Background(), planned.RunID)
	if err != nil {
		t.Fatalf("ListSteps: %v", err)
	}
	if len(steps) < 2 || steps[1].Route != "direct_answer" || steps[1].RouteSource != "conversation_router_lightweight" {
		t.Fatalf("plan step = %+v, want route trace", steps)
	}
	message := eventValue(t, events, "assistant.message")
	if message.Content == planner.plan.Message {
		t.Fatalf("assistant message used planner message, want runner output")
	}
	if !strings.Contains(message.Content, "Mock response:") {
		t.Fatalf("assistant message = %q, want runner output", message.Content)
	}
}

func newWorkflowRuntime(proposal core.ToolProposal) (*agent.Runtime, *workflowPlanner, *workflowExecutor) {
	store := repo.NewMemoryStore()
	approvals := agent.NewApprovalCenter()
	executor := &workflowExecutor{}

	registry := toolscore.NewRegistry()
	registry.Register(core.ToolSpec{ID: "shell.exec", Name: "shell.exec", Description: "shell"}, executor)
	registry.Register(core.ToolSpec{ID: "ops.local.system_info", Name: "ops.local.system_info", Description: "system info"}, executor)
	router := toolscore.NewRouter(
		registry,
		agent.NewEffectInferrer(config.PolicyConfig{SensitivePaths: []string{".env"}}),
		agent.NewPolicyEngine(config.PolicyConfig{MinConfidenceForAutoExecute: 0.85}),
		approvals,
		nil,
	)

	planner := &workflowPlanner{plan: agent.Plan{
		Preamble:     "planning",
		ToolProposal: &proposal,
	}}
	runtime := &agent.Runtime{
		Store:          store,
		Planner:        planner,
		Runner:         einoapp.Runner{Model: einoapp.MockChatModel{}},
		ContextBuilder: &agent.ContextBuilder{Store: store},
		Router:         router,
		Approvals:      approvals,
		StateStore:     agent.NewRunStateStore(),
	}
	return runtime, planner, executor
}

func startWorkflow(t *testing.T, runtime *agent.Runtime, conversationID, message string) []core.Event {
	t.Helper()
	stream, err := runtime.Start(context.Background(), einoapp.AgentInput{
		ConversationID: conversationID,
		Message:        message,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	events, err := einoapp.DrainEventStream(context.Background(), stream)
	if err != nil {
		t.Fatalf("DrainEventStream: %v", err)
	}
	return events
}

func eventValue(t *testing.T, events []core.Event, eventType string) core.Event {
	t.Helper()
	for _, event := range events {
		if event.Type == eventType {
			return event
		}
	}
	t.Fatalf("missing event %s in %v", eventType, eventTypes(events))
	return core.Event{}
}

func assertHasEvent(t *testing.T, events []core.Event, eventType string) {
	t.Helper()
	_ = eventValue(t, events, eventType)
}

func assertHasRunStatus(t *testing.T, events []core.Event, status agent.RunStatus) {
	t.Helper()
	for _, event := range events {
		if event.Type != "run.state" {
			continue
		}
		if event.Payload["status"] == string(status) {
			return
		}
	}
	t.Fatalf("missing run status %s in events %v", status, eventTypes(events))
}

func eventTypes(events []core.Event) []string {
	types := make([]string, 0, len(events))
	for _, event := range events {
		types = append(types, event.Type)
	}
	return types
}

type workflowPlanner struct {
	plan   agent.Plan
	calls  int
	inputs []agent.PlanInput
}

func (p *workflowPlanner) Plan(_ context.Context, input agent.PlanInput) (agent.Plan, error) {
	p.calls++
	p.inputs = append(p.inputs, input)
	if input.LastToolResult != nil {
		return agent.Plan{
			Decision: agent.PlanDecisionStop,
			Message:  "工具执行完成。",
		}, nil
	}
	return p.plan, nil
}

type workflowExecutor struct {
	count     int
	lastInput map[string]any
}

func (e *workflowExecutor) Execute(_ context.Context, input map[string]any) (*core.ToolResult, error) {
	e.count++
	e.lastInput = core.CloneMap(input)
	return &core.ToolResult{Output: core.CloneMap(input)}, nil
}
