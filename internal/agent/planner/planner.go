package planner

import (
	"context"

	"local-agent/internal/agent/planner/compile"
	"local-agent/internal/agent/planner/semantic"
)

// Request is the package-local planning input. The root agent package adapts
// this to the public Planner interface to avoid import cycles.
type Request struct {
	ConversationID string
	UserMessage    string
}

// SemanticPlanner is the LLM fallback surface. It returns structure only.
type SemanticPlanner interface {
	Plan(ctx context.Context, input SemanticInput) (semantic.SemanticPlan, error)
}

// SemanticInput contains the already-normalized request for semantic fallback.
type SemanticInput struct {
	Request        any
	Classification any
}

// Result is the compiled planner result.
type Result = compile.CompiledPlan
