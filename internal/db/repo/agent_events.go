package repo

import (
	"context"
	"encoding/json"

	"local-agent/internal/core"
)

func (p *postgresEvents) CreateEvent(ctx context.Context, event core.AgentEvent) error {
	payload, err := json.Marshal(event.Payload)
	if err != nil {
		return err
	}

	_, err = p.pool.Exec(ctx, `
		INSERT INTO agent_events (id, conversation_id, run_id, event_type, payload, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, event.ID, event.ConversationID, event.RunID, event.EventType, payload, event.CreatedAt)
	return err
}
