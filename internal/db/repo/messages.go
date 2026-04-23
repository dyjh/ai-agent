package repo

import (
	"context"
	"encoding/json"

	"local-agent/internal/core"
)

func (p *postgresMessages) CreateMessage(ctx context.Context, message core.Message) error {
	payload, err := json.Marshal(message.ContentJSON)
	if err != nil {
		return err
	}

	_, err = p.pool.Exec(ctx, `
		INSERT INTO messages (id, conversation_id, role, content, content_json, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, message.ID, message.ConversationID, message.Role, message.Content, payload, message.CreatedAt)
	return err
}

func (p *postgresMessages) ListByConversation(ctx context.Context, conversationID string) ([]core.Message, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id, conversation_id, role, content, content_json, created_at
		FROM messages
		WHERE conversation_id = $1
		ORDER BY created_at ASC
	`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []core.Message
	for rows.Next() {
		var (
			item    core.Message
			payload []byte
		)
		if err := rows.Scan(&item.ID, &item.ConversationID, &item.Role, &item.Content, &payload, &item.CreatedAt); err != nil {
			return nil, err
		}
		if len(payload) > 0 {
			_ = json.Unmarshal(payload, &item.ContentJSON)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
