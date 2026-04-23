package repo

import "github.com/jackc/pgx/v5/pgxpool"

// NewPostgresStore wires repository implementations backed by pgx.
func NewPostgresStore(pool *pgxpool.Pool) *Store {
	if pool == nil {
		return nil
	}
	return &Store{
		Conversations: &postgresConversations{pool: pool},
		Messages:      &postgresMessages{pool: pool},
		Usage:         &postgresUsage{pool: pool},
		AgentEvents:   &postgresEvents{pool: pool},
	}
}

type postgresConversations struct {
	pool *pgxpool.Pool
}

type postgresMessages struct {
	pool *pgxpool.Pool
}

type postgresUsage struct {
	pool *pgxpool.Pool
}

type postgresEvents struct {
	pool *pgxpool.Pool
}

var _ ConversationRepository = (*postgresConversations)(nil)
var _ MessageRepository = (*postgresMessages)(nil)
var _ UsageRepository = (*postgresUsage)(nil)
var _ AgentEventRepository = (*postgresEvents)(nil)
