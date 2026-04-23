package tests

import (
	"context"
	"testing"
	"time"

	"local-agent/internal/agent"
	"local-agent/internal/config"
	"local-agent/internal/core"
	"local-agent/internal/db/repo"
	"local-agent/internal/einoapp"
	toolscore "local-agent/internal/tools"
)

func TestWorkflowMultiStepLoop(t *testing.T) {
	runtime, executor := newSequencedRuntime([]agent.Plan{
		{
			Decision: agent.PlanDecisionTool,
			ToolProposal: &core.ToolProposal{
				ID:        "tool_one",
				Tool:      "shell.exec",
				Input:     map[string]any{"command": "pwd"},
				CreatedAt: time.Now().UTC(),
			},
		},
		{
			Decision: agent.PlanDecisionContinue,
			Reason:   "need one more read-only step",
		},
		{
			Decision: agent.PlanDecisionTool,
			ToolProposal: &core.ToolProposal{
				ID:        "tool_two",
				Tool:      "shell.exec",
				Input:     map[string]any{"command": "ls -la"},
				CreatedAt: time.Now().UTC(),
			},
		},
		{
			Decision: agent.PlanDecisionStop,
			Message:  "done",
		},
	}, 6)

	stream, err := runtime.Start(context.Background(), einoapp.AgentInput{
		ConversationID: "conv_multistep",
		Message:        "inspect workspace",
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	events, err := einoapp.DrainEventStream(context.Background(), stream)
	if err != nil {
		t.Fatalf("DrainEventStream() error = %v", err)
	}
	runID := eventValue(t, events, "run.started").RunID

	state, err := runtime.StateStore.Get(context.Background(), runID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if state.Status != agent.RunStatusCompleted {
		t.Fatalf("status = %s, want completed", state.Status)
	}
	if executor.count != 2 {
		t.Fatalf("executor count = %d, want 2", executor.count)
	}
	steps, err := runtime.StateStore.ListSteps(context.Background(), runID)
	if err != nil {
		t.Fatalf("ListSteps() error = %v", err)
	}
	var toolSteps int
	for _, step := range steps {
		if step.Type == agent.RunStepTypeProposeTool {
			toolSteps++
		}
	}
	if toolSteps != 2 {
		t.Fatalf("tool steps = %d, want 2", toolSteps)
	}
}

func TestWorkflowMaxStepsStopsLoop(t *testing.T) {
	runtime, executor := newSequencedRuntime([]agent.Plan{
		{
			Decision: agent.PlanDecisionTool,
			ToolProposal: &core.ToolProposal{
				ID:        "tool_one",
				Tool:      "shell.exec",
				Input:     map[string]any{"command": "pwd"},
				CreatedAt: time.Now().UTC(),
			},
		},
		{
			Decision: agent.PlanDecisionContinue,
			Reason:   "loop forever",
		},
	}, 1)

	stream, err := runtime.Start(context.Background(), einoapp.AgentInput{
		ConversationID: "conv_max_steps",
		Message:        "loop",
	})
	if err == nil {
		events, drainErr := einoapp.DrainEventStream(context.Background(), stream)
		if drainErr == nil {
			_ = events
		}
	}
	if err == nil {
		t.Fatalf("expected max step overflow to fail")
	}
	if executor.count != 1 {
		t.Fatalf("executor count = %d, want 1", executor.count)
	}
}

func newSequencedRuntime(plans []agent.Plan, maxSteps int) (*agent.Runtime, *sequenceExecutor) {
	store := repo.NewMemoryStore()
	approvals := agent.NewApprovalCenter()
	executor := &sequenceExecutor{}
	planner := &sequencePlanner{plans: append([]agent.Plan(nil), plans...)}
	registry := toolscore.NewRegistry()
	registry.Register(core.ToolSpec{ID: "shell.exec", Name: "shell.exec", Description: "shell"}, executor)
	router := toolscore.NewRouter(
		registry,
		agent.NewEffectInferrer(config.PolicyConfig{SensitivePaths: []string{".env"}}),
		agent.NewPolicyEngine(config.PolicyConfig{MinConfidenceForAutoExecute: 0.85}),
		approvals,
		nil,
	)
	return &agent.Runtime{
		Store:            store,
		Planner:          planner,
		Runner:           einoapp.Runner{Model: einoapp.MockChatModel{}},
		ContextBuilder:   &agent.ContextBuilder{Store: store},
		Router:           router,
		Approvals:        approvals,
		StateStore:       agent.NewRunStateStore(),
		MaxWorkflowSteps: maxSteps,
	}, executor
}

type sequencePlanner struct {
	plans []agent.Plan
	index int
}

func (p *sequencePlanner) Plan(_ context.Context, _ agent.PlanInput) (agent.Plan, error) {
	if p.index >= len(p.plans) {
		return agent.Plan{Decision: agent.PlanDecisionStop, Message: "done"}, nil
	}
	plan := p.plans[p.index]
	p.index++
	return plan, nil
}

type sequenceExecutor struct {
	count int
}

func (e *sequenceExecutor) Execute(_ context.Context, input map[string]any) (*core.ToolResult, error) {
	e.count++
	return &core.ToolResult{
		ToolCallID: "tool_call",
		Output:     core.CloneMap(input),
	}, nil
}
