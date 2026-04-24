package agent

import "local-agent/internal/core"

// CodeTaskKind identifies the high-level workflow class for code tasks.
type CodeTaskKind string

const (
	CodeTaskInspect CodeTaskKind = "inspect"
	CodeTaskSearch  CodeTaskKind = "search"
	CodeTaskPatch   CodeTaskKind = "patch"
	CodeTaskTest    CodeTaskKind = "test"
	CodeTaskFix     CodeTaskKind = "fix"
	CodeTaskGit     CodeTaskKind = "git"
)

// CodePlan is a schema-driven, non-executable description of a code workflow.
// Each step still has to be converted into a ToolProposal and routed through
// ToolRouter, EffectInference, PolicyEngine, ApprovalCenter, and Executor.
type CodePlan struct {
	Kind             CodeTaskKind   `json:"kind"`
	Workspace        string         `json:"workspace"`
	Goal             string         `json:"goal"`
	Steps            []CodePlanStep `json:"steps"`
	RequiresApproval bool           `json:"requires_approval"`
	MaxIterations    int            `json:"max_iterations,omitempty"`
	Iteration        int            `json:"iteration,omitempty"`
}

// CodePlanStep describes one proposed tool step without executing it.
type CodePlanStep struct {
	Tool             string         `json:"tool"`
	Purpose          string         `json:"purpose"`
	Input            map[string]any `json:"input,omitempty"`
	RequiresApproval bool           `json:"requires_approval,omitempty"`
}

func cloneCodePlan(plan *CodePlan) *CodePlan {
	if plan == nil {
		return nil
	}
	cp := *plan
	cp.Steps = make([]CodePlanStep, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		step.Input = core.CloneMap(step.Input)
		cp.Steps = append(cp.Steps, step)
	}
	return &cp
}
