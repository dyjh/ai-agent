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

func TestWorkflowResumeFromPersistentStateAfterRestart(t *testing.T) {
	store := repo.NewMemoryStore()
	stateStore := agent.NewPersistentRunStateStore(store.AgentRuns, store.AgentRunSteps)
	executor := &resumeExecutor{}

	runtime1 := newPersistentRuntime(store, stateStore, agent.NewApprovalCenter(), executor)
	startStream, err := runtime1.Start(context.Background(), einoapp.AgentInput{
		ConversationID: "conv_resume",
		Message:        "install axios",
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	startEvents, err := einoapp.DrainEventStream(context.Background(), startStream)
	if err != nil {
		t.Fatalf("DrainEventStream() error = %v", err)
	}
	runID := eventValue(t, startEvents, "run.started").RunID
	approvalID := eventValue(t, startEvents, "approval.requested").ApprovalID

	state, err := stateStore.Get(context.Background(), runID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if state.Status != agent.RunStatusPausedForApproval {
		t.Fatalf("status = %s, want paused_for_approval", state.Status)
	}

	runtime2 := newPersistentRuntime(store, stateStore, agent.NewApprovalCenter(), executor)
	if _, err := runtime2.Approvals.Get(approvalID); err == nil {
		t.Fatalf("expected empty approval center after restart simulation")
	}

	resumeStream, err := runtime2.Resume(context.Background(), runID, approvalID, true)
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if _, err := einoapp.DrainEventStream(context.Background(), resumeStream); err != nil {
		t.Fatalf("DrainEventStream() error = %v", err)
	}
	if executor.count != 1 {
		t.Fatalf("executor count = %d, want 1", executor.count)
	}
	if executor.lastInput["command"] != "pnpm add axios" {
		t.Fatalf("executed command = %v, want approved snapshot", executor.lastInput["command"])
	}
	state, err = stateStore.Get(context.Background(), runID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if state.Status != agent.RunStatusCompleted {
		t.Fatalf("status = %s, want completed", state.Status)
	}
}

func newPersistentRuntime(store *repo.Store, stateStore agent.RunStateStore, approvals *agent.ApprovalCenter, executor *resumeExecutor) *agent.Runtime {
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
		Store: store,
		Planner: &persistentPlanner{proposal: core.ToolProposal{
			ID:        "tool_resume",
			Tool:      "shell.exec",
			Input:     map[string]any{"command": "pnpm add axios"},
			Purpose:   "install dependency",
			CreatedAt: time.Now().UTC(),
		}},
		Runner:           einoapp.Runner{Model: einoapp.MockChatModel{}},
		ContextBuilder:   &agent.ContextBuilder{Store: store},
		Router:           router,
		Approvals:        approvals,
		StateStore:       stateStore,
		MaxWorkflowSteps: 4,
	}
}

type persistentPlanner struct {
	proposal core.ToolProposal
}

func (p *persistentPlanner) Plan(_ context.Context, input agent.PlanInput) (agent.Plan, error) {
	if input.LastToolResult != nil {
		return agent.Plan{
			Decision: agent.PlanDecisionStop,
			Message:  "done",
		}, nil
	}
	proposal := p.proposal
	return agent.Plan{
		Decision:     agent.PlanDecisionTool,
		ToolProposal: &proposal,
	}, nil
}

type resumeExecutor struct {
	count     int
	lastInput map[string]any
}

func (e *resumeExecutor) Execute(_ context.Context, input map[string]any) (*core.ToolResult, error) {
	e.count++
	e.lastInput = core.CloneMap(input)
	return &core.ToolResult{
		ToolCallID: "tool_resume",
		Output:     core.CloneMap(input),
	}, nil
}
