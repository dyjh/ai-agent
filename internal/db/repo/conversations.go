package repo

import (
	"context"

	"local-agent/internal/core"
)

func (p *postgresConversations) CreateConversation(ctx context.Context, conversation core.Conversation) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO conversations (id, title, project_key, created_at, updated_at, archived)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, conversation.ID, conversation.Title, conversation.ProjectKey, conversation.CreatedAt, conversation.UpdatedAt, conversation.Archived)
	return err
}

func (p *postgresConversations) List(ctx context.Context) ([]core.Conversation, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id, title, project_key, created_at, updated_at, archived
		FROM conversations
		ORDER BY updated_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []core.Conversation
	for rows.Next() {
		var item core.Conversation
		if err := rows.Scan(&item.ID, &item.Title, &item.ProjectKey, &item.CreatedAt, &item.UpdatedAt, &item.Archived); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (p *postgresConversations) Get(ctx context.Context, id string) (*core.Conversation, error) {
	var item core.Conversation
	err := p.pool.QueryRow(ctx, `
		SELECT id, title, project_key, created_at, updated_at, archived
		FROM conversations
		WHERE id = $1
	`, id).Scan(&item.ID, &item.Title, &item.ProjectKey, &item.CreatedAt, &item.UpdatedAt, &item.Archived)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (p *postgresConversations) Touch(ctx context.Context, id string) error {
	_, err := p.pool.Exec(ctx, `UPDATE conversations SET updated_at = now() WHERE id = $1`, id)
	return err
}
