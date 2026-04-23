package einoapp

import (
	"context"

	"github.com/cloudwego/eino/schema"
)

// Runner is the Eino model runner abstraction.
type Runner struct {
	Model ChatModel
}

// Run delegates to the configured chat model.
func (r Runner) Run(ctx context.Context, input AgentInput) (*schema.Message, error) {
	return r.Model.Generate(ctx, input.Messages)
}
