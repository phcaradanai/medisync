package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/adm-chura3inter/medisync/services/core/internal/platform/audit"
)

// dbConn is the narrow database interface for the catalog store.
// *pgxpool.Pool satisfies this interface; tests inject a deterministic fake.
type dbConn interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Compile-time check that *pgxpool.Pool satisfies dbConn.
var _ dbConn = (*pgxpool.Pool)(nil)

// Store persists drugs to PostgreSQL. Pattern follows identity.Store.
type Store struct {
	db          dbConn
	auditWriter *audit.Writer
}

// NewStore creates a Store backed by a pgx connection pool with an
// audit writer. Every mutation writes an audit log entry.
func NewStore(pool *pgxpool.Pool, aw *audit.Writer) *Store {
	return &Store{db: pool, auditWriter: aw}
}

// NewStoreWithDB creates a Store backed by an arbitrary dbConn with an
// audit writer. Exported for use by integration and unit tests.
func NewStoreWithDB(db dbConn, aw *audit.Writer) *Store {
	return &Store{db: db, auditWriter: aw}
}

// Create inserts a new drug into catalog.drug. It returns the created Drug
// with server-generated fields (id, timestamps).
func (s *Store) Create(ctx context.Context, d Drug) (*Drug, error) {
	row := s.db.QueryRow(ctx,
		`INSERT INTO catalog.drug (code, name, display_name, generic_name, form, strength, unit, sticker_note, project_id, barcode)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 RETURNING id, code, name, display_name, generic_name, form, strength, unit, sticker_note,
		           active, project_id, barcode, created_at, updated_at`,
		d.Code, d.Name, d.DisplayName, d.GenericName, d.Form, d.Strength, d.Unit, d.StickerNote, d.ProjectID, d.Barcode)
	return scanDrug(row)
}

// GetByID returns a Drug by UUID, or nil if not found.
func (s *Store) GetByID(ctx context.Context, id string) (*Drug, error) {
	row := s.db.QueryRow(ctx,
		`SELECT id, code, name, display_name, generic_name, form, strength, unit, sticker_note,
		        active, project_id, barcode, created_at, updated_at
		   FROM catalog.drug WHERE id = $1`, id)
	return scanDrug(row)
}

// GetByCode returns a Drug by hospital code, or nil if not found.
func (s *Store) GetByCode(ctx context.Context, code string) (*Drug, error) {
	row := s.db.QueryRow(ctx,
		`SELECT id, code, name, display_name, generic_name, form, strength, unit, sticker_note,
		        active, project_id, barcode, created_at, updated_at
		   FROM catalog.drug WHERE code = $1`, code)
	return scanDrug(row)
}

// List returns drugs matching the optional query string against code, name,
// and generic_name. Pagination uses a cursor based on the drug id.
// When includeInactive is false, only active drugs are returned.
// When projectID is non-empty, results are scoped to that project.
func (s *Store) List(ctx context.Context, query string, includeInactive bool, pageSize int32, pageToken, projectID string) ([]*Drug, string, error) {
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 50
	}

	args := []any{}
	argIdx := 1
	whereClauses := []string{}

	if !includeInactive {
		whereClauses = append(whereClauses, "active = true")
	}

	if query != "" {
		q := "%" + query + "%"
		whereClauses = append(whereClauses,
			fmt.Sprintf("(code ILIKE $%d OR name ILIKE $%d OR generic_name ILIKE $%d)",
				argIdx, argIdx+1, argIdx+2))
		args = append(args, q, q, q)
		argIdx += 3
	}

	if projectID != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("project_id = $%d", argIdx))
		args = append(args, projectID)
		argIdx++
	}

	if pageToken != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("id > $%d", argIdx))
		args = append(args, pageToken)
		argIdx++
	}

	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = "WHERE "
		for i, clause := range whereClauses {
			if i > 0 {
				whereSQL += " AND "
			}
			whereSQL += clause
		}
	}

	querySQL := fmt.Sprintf(
		`SELECT id, code, name, display_name, generic_name, form, strength, unit, sticker_note,
		        active, project_id, barcode, created_at, updated_at
		   FROM catalog.drug %s ORDER BY id ASC LIMIT $%d`,
		whereSQL, argIdx)
	args = append(args, pageSize+1)

	rows, err := s.db.Query(ctx, querySQL, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list drugs: %w", err)
	}
	defer rows.Close()

	var drugs []*Drug
	for rows.Next() {
		var d Drug
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&d.ID, &d.Code, &d.Name, &d.DisplayName, &d.GenericName,
			&d.Form, &d.Strength, &d.Unit, &d.StickerNote,
			&d.Active, &d.ProjectID, &createdAt, &updatedAt); err != nil {
			return nil, "", fmt.Errorf("scan drug row: %w", err)
		}
		d.CreatedAt = createdAt
		d.UpdatedAt = updatedAt
		drugs = append(drugs, &d)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("iterate drug rows: %w", err)
	}

	var nextPageToken string
	if len(drugs) > int(pageSize) {
		nextPageToken = drugs[pageSize-1].ID
		drugs = drugs[:pageSize]
	}

	return drugs, nextPageToken, nil
}

// Update modifies an existing drug. It updates all mutable fields and
// sets updated_at. Returns the updated Drug, or nil if not found.
func (s *Store) Update(ctx context.Context, d Drug) (*Drug, error) {
	row := s.db.QueryRow(ctx,
		`UPDATE catalog.drug
		   SET code = $1, name = $2, generic_name = $3, form = $4,
		       strength = $5, unit = $6, sticker_note = $7, active = $8,
		       display_name = $9, updated_at = now()
		 WHERE id = $10
		 RETURNING id, code, name, display_name, generic_name, form, strength, unit, sticker_note,
		           active, project_id, barcode, created_at, updated_at`,
		d.Code, d.Name, d.GenericName, d.Form, d.Strength, d.Unit,
		d.StickerNote, d.Active, d.DisplayName, d.ID)
	return scanDrug(row)
}

// Deactivate sets active = false on the drug identified by id.
// It is a soft delete — the row is never removed. Returns nil if the
// drug does not exist or is already inactive.
func (s *Store) Deactivate(ctx context.Context, id string) (*Drug, error) {
	row := s.db.QueryRow(ctx,
		`UPDATE catalog.drug SET active = false, updated_at = now()
		 WHERE id = $1 AND active = true
		 RETURNING id, code, name, display_name, generic_name, form, strength, unit, sticker_note,
		           active, project_id, barcode, created_at, updated_at`,
		id)
	return scanDrug(row)
}

// writeAudit is a convenience method that writes an audit entry when the
// auditWriter is configured. It is a no-op when auditWriter is nil.
func (s *Store) writeAudit(ctx context.Context, e audit.Entry) {
	if s.auditWriter == nil {
		return
	}
	_ = s.auditWriter.Write(ctx, e)
}

// scanDrug maps a pgx.Row to a Drug from a 13-column result.
func scanDrug(row pgx.Row) (*Drug, error) {
	var d Drug
	var createdAt, updatedAt time.Time
	err := row.Scan(&d.ID, &d.Code, &d.Name, &d.DisplayName, &d.GenericName,
		&d.Form, &d.Strength, &d.Unit, &d.StickerNote,
		&d.Active, &d.ProjectID, &d.Barcode, &createdAt, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan drug: %w", err)
	}
	d.CreatedAt = createdAt
	d.UpdatedAt = updatedAt
	return &d, nil
}

// auditDetail is the JSON payload written to audit_log.detail for
// catalog mutations.
type auditDetail struct {
	Code  string `json:"code,omitempty"`
	Name  string `json:"name,omitempty"`
	Delta string `json:"delta,omitempty"`
}

// toJSON safely marshals a value to JSON bytes, returning {} on error.
func toJSON(v any) json.RawMessage {
	if v == nil {
		return json.RawMessage("{}")
	}
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("{}")
	}
	return b
}
