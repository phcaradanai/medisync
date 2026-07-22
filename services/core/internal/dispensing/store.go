package dispensing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/adm-chura3inter/medisync/services/core/internal/platform/pagination"
	"github.com/adm-chura3inter/medisync/services/core/internal/testutil"
)

// dbConn is the narrow database interface for the dispensing store.
// *pgxpool.Pool satisfies this interface; tests inject deterministic fakes.
type dbConn interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Compile-time check that *pgxpool.Pool satisfies dbConn.
var _ dbConn = (*pgxpool.Pool)(nil)

// Store persists prescriptions to PostgreSQL. The exported NewStore
// constructor accepts *pgxpool.Pool for production wiring; unit tests
// inject a deterministic fake through NewStoreWithDB.
type Store struct {
	db dbConn
}

// NewStore creates a Store backed by a pgx connection pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{db: pool}
}

// NewStoreWithDB creates a Store backed by an arbitrary dbConn.
// Exported for use by integration and unit tests.
func NewStoreWithDB(db dbConn) *Store {
	return &Store{db: db}
}

// NewStoreWithExecer creates a Store backed by a testutil.Execer (write-only).
// Use this for unit tests that only need Insert. Read methods will panic
// because Execer does not support Query / QueryRow.
func NewStoreWithExecer(db testutil.Execer) *Store {
	return &Store{db: execerAdapter{db}}
}

// execerAdapter wraps testutil.Execer to satisfy dbConn.
// Query / QueryRow panic because Execer is write-only.
type execerAdapter struct {
	testutil.Execer
}

func (a execerAdapter) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	panic("dispensing store: Query called on Execer adapter — use NewStoreWithDB instead")
}

func (a execerAdapter) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	panic("dispensing store: QueryRow called on Execer adapter — use NewStoreWithDB instead")
}

// Insert stores a new prescription in READY state. Returns false when the
// (prescription_id, source_system) pair already exists — replayed events are
// silently deduplicated, per the intake idempotency rule.
// This method is the M1 intake path; do NOT modify its signature or behavior.
func (s *Store) Insert(ctx context.Context, p Prescription) (bool, error) {
	items, err := json.Marshal(p.Items)
	if err != nil {
		return false, fmt.Errorf("marshal prescription items: %w", err)
	}

	tag, err := s.db.Exec(ctx,
		`INSERT INTO medisync.prescription
		   (prescription_id, source_system, hn, patient_name, ward_id, items, state, issued_at, project_id)
		 VALUES ($1, $2, $3, $4, $5, $6, 'READY', $7, $8)
		 ON CONFLICT ON CONSTRAINT prescription_external_key DO NOTHING`,
		p.PrescriptionID, p.SourceSystem, p.HN, p.PatientName, p.WardID, items, p.IssuedAt, nullableString(p.ProjectID))
	if err != nil {
		return false, fmt.Errorf("insert prescription: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// ProjectIDByCode resolves an external immutable project business code to the
// internal foreign key. External feeders never need to know database UUIDs.
func (s *Store) ProjectIDByCode(ctx context.Context, code string) (string, error) {
	var id string
	err := s.db.QueryRow(ctx, `SELECT id FROM medisync.projects WHERE code=$1 AND active=true`, code).Scan(&id)
	return id, err
}

// GetByID fetches a prescription by internal UUID. Returns nil when not found.
func (s *Store) GetByID(ctx context.Context, id string) (*PrescriptionRow, error) {
	row := s.db.QueryRow(ctx,
		`SELECT id, prescription_id, source_system, hn, patient_name, ward_id,
		        items, state, failure_reason, issued_at, created_at, updated_at
		   FROM medisync.prescription WHERE id = $1`, id)
	return scanPrescription(row)
}

// GetByPrescriptionID fetches a prescription by external (prescription_id, source_system).
// Returns nil when not found.
func (s *Store) GetByPrescriptionID(ctx context.Context, prescriptionID, sourceSystem string) (*PrescriptionRow, error) {
	var row pgx.Row
	if sourceSystem != "" {
		row = s.db.QueryRow(ctx,
			`SELECT id, prescription_id, source_system, hn, patient_name, ward_id,
			        items, state, failure_reason, issued_at, created_at, updated_at
			   FROM medisync.prescription
			  WHERE prescription_id = $1 AND source_system = $2`, prescriptionID, sourceSystem)
	} else {
		row = s.db.QueryRow(ctx,
			`SELECT id, prescription_id, source_system, hn, patient_name, ward_id,
			        items, state, failure_reason, issued_at, created_at, updated_at
			   FROM medisync.prescription
			  WHERE prescription_id = $1`, prescriptionID)
	}
	return scanPrescription(row)
}

// ListByWard returns prescriptions filtered by wards and (optionally) states.
// When states is empty, all non-terminal states are returned.
// Used by ListPrescriptions handler; ward-scoping is enforced server-side.
func (s *Store) ListByWard(ctx context.Context, wardIDs []string, states []State, pageSize int32, pageToken string) ([]*PrescriptionRow, string, int64, error) {
	pageSize = pagination.NormalizePageSize(pageSize)
	var args []any
	argIdx := 1

	var whereClauses []string
	allWards := len(wardIDs) == 1 && wardIDs[0] == ""
	if !allWards && len(wardIDs) == 0 {
		whereClauses = append(whereClauses, "FALSE")
	} else if !allWards {
		whereClauses = append(whereClauses, fmt.Sprintf("ward_id = ANY($%d)", argIdx))
		args = append(args, wardIDs)
		argIdx++
	}
	if len(states) > 0 {
		placeholders := ""
		for i, st := range states {
			if i > 0 {
				placeholders += ", "
			}
			placeholders += fmt.Sprintf("$%d", argIdx)
			args = append(args, string(st))
			argIdx++
		}
		whereClauses = append(whereClauses, fmt.Sprintf("state IN (%s)", placeholders))
	} else {
		// Default: non-terminal states only.
		whereClauses = append(whereClauses, "state NOT IN ('DISPENSED', 'FAILED', 'CANCELLED', 'EXPIRED')")
	}

	filterWhereSQL := "WHERE "
	for i, clause := range whereClauses {
		if i > 0 {
			filterWhereSQL += " AND "
		}
		filterWhereSQL += clause
	}

	var totalCount int64
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM medisync.prescription %s", filterWhereSQL)
	if err := s.db.QueryRow(ctx, countQuery, args...).Scan(&totalCount); err != nil {
		return nil, "", 0, fmt.Errorf("count prescriptions: %w", err)
	}

	whereSQL := filterWhereSQL
	if pageToken != "" {
		whereSQL += fmt.Sprintf(
			" AND (created_at, id) < (SELECT created_at, id FROM medisync.prescription WHERE id = $%d)",
			argIdx,
		)
		args = append(args, pageToken)
		argIdx++
	}

	query := fmt.Sprintf(
		`SELECT id, prescription_id, source_system, hn, patient_name, ward_id,
		        items, state, failure_reason, issued_at, created_at, updated_at
		   FROM medisync.prescription %s
		  ORDER BY created_at DESC, id DESC LIMIT $%d`, whereSQL, argIdx)
	args = append(args, pageSize+1)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, "", 0, fmt.Errorf("list prescriptions: %w", err)
	}
	defer rows.Close()

	var results []*PrescriptionRow
	for rows.Next() {
		pr, err := scanPrescription(rows)
		if err != nil {
			return nil, "", 0, err
		}
		results = append(results, pr)
	}
	if err := rows.Err(); err != nil {
		return nil, "", 0, fmt.Errorf("iterate prescription rows: %w", err)
	}

	var nextPageToken string
	if len(results) > int(pageSize) {
		nextPageToken = results[pageSize-1].ID
		results = results[:pageSize]
	}
	return results, nextPageToken, totalCount, nil
}

// TransitionState validates the state transition and atomically updates the
// prescription state. When transitioning to DISPENSING from READY, it also
// inserts an outbox row for medisync.dispense.requested in the same transaction.
//
// The caller must pass a tx (pgx.Tx) so that the state update and outbox insert
// are atomic. The tx is started, committed, and rolled back by the caller.
func (s *Store) TransitionState(ctx context.Context, tx pgx.Tx, id string, from, to State, outboxPayload []byte) (*PrescriptionRow, error) {
	// Validate the transition.
	if err := from.CanTransitionTo(to); err != nil {
		return nil, fmt.Errorf("invalid transition: %w", err)
	}

	// Atomically update state — the WHERE current_state = $from clause
	// prevents lost updates and enforces the guard at the database level.
	row := tx.QueryRow(ctx,
		`UPDATE medisync.prescription
		   SET state = $1, updated_at = now()
		 WHERE id = $2 AND state = $3
		 RETURNING id, prescription_id, source_system, hn, patient_name, ward_id,
		           items, state, failure_reason, issued_at, created_at, updated_at`,
		string(to), id, string(from))
	pr, err := scanPrescription(row)
	if err != nil {
		return nil, err
	}
	if pr == nil {
		return nil, fmt.Errorf("prescription %s not found or not in state %s", id, from)
	}

	// When transitioning READY → DISPENSING, insert the outbox row for
	// medisync.dispense.requested in the same transaction.
	if from == StateReady && to == StateDispensing && len(outboxPayload) > 0 {
		_, err := tx.Exec(ctx,
			`INSERT INTO medisync.outbox (subject, payload) VALUES ($1, $2)`,
			"medisync.dispense.requested", outboxPayload)
		if err != nil {
			return nil, fmt.Errorf("insert outbox: %w", err)
		}
	}

	return pr, nil
}

// scanPrescription maps a pgx.Row or pgx.Rows to a PrescriptionRow.
// Returns nil when the row is empty (pgx.ErrNoRows).
func scanPrescription(row pgx.Row) (*PrescriptionRow, error) {
	var pr PrescriptionRow
	var itemsRaw []byte
	var stateStr string
	var issuedAt *time.Time
	var createdAt, updatedAt time.Time

	err := row.Scan(&pr.ID, &pr.PrescriptionID, &pr.SourceSystem, &pr.HN, &pr.PatientName,
		&pr.WardID, &itemsRaw, &stateStr, &pr.FailureReason, &issuedAt, &createdAt, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan prescription: %w", err)
	}

	pr.State = State(stateStr)
	pr.IssuedAt = issuedAt
	pr.CreatedAt = createdAt
	pr.UpdatedAt = updatedAt

	if len(itemsRaw) > 0 {
		if err := json.Unmarshal(itemsRaw, &pr.Items); err != nil {
			return nil, fmt.Errorf("unmarshal prescription items: %w", err)
		}
	}

	return &pr, nil
}
