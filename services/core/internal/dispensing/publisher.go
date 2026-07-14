package dispensing

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
)

// OutboxPublisher polls the dispensing.outbox table for unpublished rows
// and publishes them to NATS JetStream. A row is marked published only
// after the NATS publish succeeds, providing at-least-once delivery.
//
// Polling is continuous until the context is cancelled. Publish failures
// are logged and retried on the next poll cycle (idempotent subscribers
// must handle duplicates).
type OutboxPublisher struct {
	pool    *pgxpool.Pool
	js      jetstream.JetStream
	log     *slog.Logger
	poll    time.Duration
	batch   int
}

// NewOutboxPublisher creates an OutboxPublisher with defaults tuned for
// the dispense flow (low volume, near-immediate delivery).
func NewOutboxPublisher(pool *pgxpool.Pool, js jetstream.JetStream, log *slog.Logger) *OutboxPublisher {
	return &OutboxPublisher{
		pool:  pool,
		js:    js,
		log:   log.With("component", "dispensing.outbox-publisher"),
		poll:  500 * time.Millisecond,
		batch: 10,
	}
}

// Start begins the polling loop. It blocks until ctx is cancelled.
// Call this in its own goroutine.
func (p *OutboxPublisher) Start(ctx context.Context) {
	ticker := time.NewTicker(p.poll)
	defer ticker.Stop()

	p.log.Info("outbox publisher started",
		"poll", p.poll.String(),
		"batch", p.batch,
	)

	for {
		select {
		case <-ctx.Done():
			p.log.Info("outbox publisher stopping")
			return
		case <-ticker.C:
			if err := p.publishBatch(ctx); err != nil {
				p.log.Warn("outbox publish batch failed", "error", err.Error())
			}
		}
	}
}

// publishBatch reads one batch of unpublished outbox rows, publishes each
// to NATS, and marks them published in a single transaction per row.
func (p *OutboxPublisher) publishBatch(ctx context.Context) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// SELECT ... FOR UPDATE SKIP LOCKED prevents concurrent publishers
	// from racing on the same rows, and SKIP LOCKED keeps it non-blocking.
	rows, err := tx.Query(ctx,
		`SELECT id, subject, payload
		   FROM dispensing.outbox
		  WHERE NOT published
		  ORDER BY created_at
		  LIMIT $1
		    FOR UPDATE SKIP LOCKED`,
		p.batch,
	)
	if err != nil {
		return fmt.Errorf("query outbox: %w", err)
	}
	defer rows.Close()

	type row struct {
		id      int64
		subject string
		payload []byte
	}
	var batch []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.subject, &r.payload); err != nil {
			return fmt.Errorf("scan outbox row: %w", err)
		}
		batch = append(batch, r)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate outbox rows: %w", err)
	}

	if len(batch) == 0 {
		return tx.Rollback(ctx) //nolint:errcheck
	}

	for _, r := range batch {
		// Publish to NATS. Use the subject from the outbox row to allow
		// the same publisher to handle future subjects beyond dispense
		// requests. The payload is protojson, ready to send as-is.
		if _, err := p.js.Publish(ctx, r.subject, r.payload); err != nil {
			return fmt.Errorf("publish outbox[%d] to %s: %w", r.id, r.subject, err)
		}

		// Mark as published after successful NATS publish.
		if _, err := tx.Exec(ctx,
			`UPDATE dispensing.outbox SET published = true WHERE id = $1`,
			r.id,
		); err != nil {
			return fmt.Errorf("mark outbox[%d] published: %w", r.id, err)
		}

		p.log.Debug("outbox published",
			"id", r.id,
			"subject", r.subject,
		)
	}

	return tx.Commit(ctx)
}
