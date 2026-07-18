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
		e.TraceID, e.Actor, e.Action, e.Entity, e.EntityID, nilIfEmpty(e.ProjectID), detail)
	if err != nil {
		return fmt.Errorf("write audit log: %w", err)
	}
	return nil
}

// nilIfEmpty returns nil when s is empty, otherwise returns s.
// Use this for nullable UUID columns to avoid PostgreSQL casting errors.
func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// AuditEntry is a read model returned by List.
type AuditEntry struct {
	ID        string `json:"id"`
	TraceID   string `json:"trace_id"`
	Actor     string `json:"actor"`
	Action    string `json:"action"`
	Entity    string `json:"entity"`
	EntityID  string `json:"entity_id"`
	ProjectID string `json:"project_id"`
	Detail    string `json:"detail"`
	CreatedAt string `json:"created_at"`
}

// List returns audit entries with pagination. Requires a connection pool.
func List(ctx context.Context, pool *pgxpool.Pool, projectID string, pageSize int32, pageToken string) ([]AuditEntry, int64, string, error) {
	if pageSize <= 0 || pageSize > 200 { pageSize = 50 }
	offset := 0
	if pageToken != "" { fmt.Sscanf(pageToken, "%d", &offset) }

	var total int64
	args := []any{}
	countSQL := "SELECT COUNT(*) FROM audit.audit_log"
	whereSQL := ""
	if projectID != "" {
		whereSQL = " WHERE project_id = $1"
		args = append(args, projectID)
	}
	if err := pool.QueryRow(ctx, countSQL+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, "", fmt.Errorf("count audit: %w", err)
	}

	sql := "SELECT id, trace_id, actor, action, entity, entity_id, COALESCE(project_id::text,''), COALESCE(detail::text,'{}'), created_at::text FROM audit.audit_log" + whereSQL + " ORDER BY created_at DESC LIMIT $" + fmt.Sprintf("%d OFFSET $%d", len(args)+1, len(args)+2)
	args = append(args, pageSize+1, offset)

	rows, err := pool.Query(ctx, sql, args...)
	if err != nil { return nil, 0, "", fmt.Errorf("list audit: %w", err) }
	defer rows.Close()

	var entries []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.TraceID, &e.Actor, &e.Action, &e.Entity, &e.EntityID, &e.ProjectID, &e.Detail, &e.CreatedAt); err != nil {
			return nil, 0, "", fmt.Errorf("scan audit: %w", err)
		}
		entries = append(entries, e)
	}

	nextToken := ""
	if len(entries) > int(pageSize) {
		entries = entries[:pageSize]
		nextToken = fmt.Sprintf("%d", offset+int(pageSize))
	}
	return entries, total, nextToken, nil
}
