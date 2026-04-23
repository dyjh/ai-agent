package repo

import (
	"context"

	"local-agent/internal/core"
)

func (p *postgresUsage) CreateUsage(ctx context.Context, usage core.MessageUsage) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO message_usage (id, message_id, conversation_id, model, input_tokens, output_tokens, total_tokens, tool_call_count, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, usage.ID, usage.MessageID, usage.ConversationID, usage.Model, usage.InputTokens, usage.OutputTokens, usage.TotalTokens, usage.ToolCallCount, usage.CreatedAt)
	return err
}

func (p *postgresUsage) IncrementRollup(ctx context.Context, conversationID string, usage core.MessageUsage) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO conversation_usage_rollups (
			conversation_id, total_input_tokens, total_output_tokens, total_tokens, total_messages, total_runs, updated_at
		)
		VALUES ($1, $2, $3, $4, 1, 1, now())
		ON CONFLICT (conversation_id) DO UPDATE
		SET total_input_tokens = conversation_usage_rollups.total_input_tokens + EXCLUDED.total_input_tokens,
			total_output_tokens = conversation_usage_rollups.total_output_tokens + EXCLUDED.total_output_tokens,
			total_tokens = conversation_usage_rollups.total_tokens + EXCLUDED.total_tokens,
			total_messages = conversation_usage_rollups.total_messages + 1,
			total_runs = conversation_usage_rollups.total_runs + 1,
			updated_at = now()
	`, conversationID, usage.InputTokens, usage.OutputTokens, usage.TotalTokens)
	return err
}
