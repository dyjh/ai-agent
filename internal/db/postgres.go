package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Open opens a PostgreSQL pool.
func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	if url == "" {
		return nil, nil
	}
	return pgxpool.New(ctx, url)
}
