// Package postgres owns the connection pool and schema migrations.
package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPool connects and pings, retrying until ctx expires so `docker compose
// up` ordering doesn't matter.
func NewPool(ctx context.Context, url string, log *slog.Logger) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}

	for {
		pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err = pool.Ping(pingCtx)
		cancel()
		if err == nil {
			return pool, nil
		}

		select {
		case <-ctx.Done():
			pool.Close()
			return nil, fmt.Errorf("database not reachable before deadline: %w", err)
		case <-time.After(2 * time.Second):
			log.Info("waiting for database", "error", err.Error())
		}
	}
}
