package skills

import (
	"context"

	"local-agent/internal/core"
)

// Runner is a placeholder executor for skill.run.
type Runner struct{}

// Execute returns a structured placeholder result until skill execution is wired.
func (r *Runner) Execute(_ context.Context, input map[string]any) (*core.ToolResult, error) {
	return &core.ToolResult{
		Output: map[string]any{
			"status": "registered_only",
			"input":  input,
		},
		Error: "skill execution not implemented in MVP yet",
	}, nil
}
