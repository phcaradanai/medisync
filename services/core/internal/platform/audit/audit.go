// Package audit writes the append-only audit trail. Every state-mutating
// operation in any bounded context must record an Entry.
package audit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/adm-chura3inter/medisync/services/core/internal/testutil"
)

type Entry struct {
	TraceID string
	// Actor is a user id or "system" for event-driven transitions.
	Actor    string
	Action   string // e.g. "prescription.received"
	Entity   string // e.g. "prescription"
	EntityID string
	// ProjectID scopes this entry to a project for multi-tenant isolation.
	ProjectID string
	Detail    any // JSON-serializable context; nil becomes {}
}

// Writer persists audit entries to PostgreSQL.
type Writer struct {
	db testutil.Execer
}

// NewWriter creates a Writer backed by a pgx connection pool.
func NewWriter(pool *pgxpool.Pool) *Writer {
	return &Writer{db: pool}
}

// NewWriterWithDB creates a Writer backed by an arbitrary Execer.
func NewWriterWithDB(db testutil.Execer) *Writer {
	return &Writer{db: db}
}

func (w *Writer) Write(ctx context.Context, e Entry) error {
	if e.Action == "" || e.Entity == "" {
		return fmt.Errorf("audit entry requires action and entity")
	}
	if e.Actor == "" {
		e.Actor = "system"
	}

	detail := []byte("{}")
	if e.Detail != nil {
		b, err := json.Marshal(e.Detail)
		if err != nil {
			return fmt.Errorf("marshal audit detail: %w", err)
		}
		detail = b
	}

	_, err := w.db.Exec(ctx,
		`INSERT INTO audit.audit_log (trace_id, actor, action, entity, entity_id, project_id, detail)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		e.TraceID, e.Actor, e.Action, e.Entity, e.EntityID, e.ProjectID, detail)
	if err != nil {
		return fmt.Errorf("write audit log: %w", err)
	}
	return nil
}
