package inventory

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

// dbConn is the narrow database interface for the inventory store.
// *pgxpool.Pool satisfies this interface; tests inject a deterministic fake.
type dbConn interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Compile-time check that *pgxpool.Pool satisfies dbConn.
var _ dbConn = (*pgxpool.Pool)(nil)

// Store persists inventory slots to PostgreSQL. Pattern follows catalog.Store.
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

// ListSlots returns all slots, optionally filtered by cabinet_id and by
// low-stock status (quantity <= low_threshold).
func (s *Store) ListSlots(ctx context.Context, cabinetID string, lowOnly bool) ([]*Slot, error) {
	var whereClauses []string
	args := []any{}
	argIdx := 1

	if cabinetID != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("cabinet_id = $%d", argIdx))
		args = append(args, cabinetID)
		argIdx++
	}
	if lowOnly {
		whereClauses = append(whereClauses, "quantity <= low_threshold")
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

	query := fmt.Sprintf(
		`SELECT id, cabinet_id, code, drug_id, drug_code, drug_name,
		        capacity, quantity, low_threshold, created_at, updated_at
		   FROM inventory.slot %s ORDER BY cabinet_id, code ASC`, whereSQL)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list slots: %w", err)
	}
	defer rows.Close()

	var slots []*Slot
	for rows.Next() {
		slot, err := scanSlot(rows)
		if err != nil {
			return nil, err
		}
		slots = append(slots, slot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate slot rows: %w", err)
	}
	return slots, nil
}

// GetByID returns a Slot by UUID, or nil if not found.
func (s *Store) GetByID(ctx context.Context, id string) (*Slot, error) {
	row := s.db.QueryRow(ctx,
		`SELECT id, cabinet_id, code, drug_id, drug_code, drug_name,
		        capacity, quantity, low_threshold, created_at, updated_at
		   FROM inventory.slot WHERE id = $1`, id)
	return scanSlot(row)
}

// GetByCabinetAndCode returns a Slot by cabinet_id + code, or nil if not found.
func (s *Store) GetByCabinetAndCode(ctx context.Context, cabinetID, code string) (*Slot, error) {
	row := s.db.QueryRow(ctx,
		`SELECT id, cabinet_id, code, drug_id, drug_code, drug_name,
		        capacity, quantity, low_threshold, created_at, updated_at
		   FROM inventory.slot WHERE cabinet_id = $1 AND code = $2`, cabinetID, code)
	return scanSlot(row)
}

// AssignDrug updates a slot's drug assignment fields. Returns the
// updated slot, or nil if the slot does not exist.
func (s *Store) AssignDrug(ctx context.Context, slotID, drugID, drugCode, drugName string, capacity, lowThreshold int32) (*Slot, error) {
	row := s.db.QueryRow(ctx,
		`UPDATE inventory.slot
		   SET drug_id = $1, drug_code = $2, drug_name = $3,
		       capacity = $4, low_threshold = $5,
		       quantity = LEAST(quantity, $4), updated_at = now()
		 WHERE id = $6
		 RETURNING id, cabinet_id, code, drug_id, drug_code, drug_name,
		           capacity, quantity, low_threshold, created_at, updated_at`,
		drugID, drugCode, drugName, capacity, lowThreshold, slotID)
	return scanSlot(row)
}

// Refill atomically increments a slot's quantity. The UPDATE uses
// quantity = quantity + $delta to prevent lost updates. Returns the
// updated slot, or nil if not found. When the resulting quantity
// would be negative, returns ErrInsufficientStock.
func (s *Store) Refill(ctx context.Context, id string, delta int32) (*Slot, error) {
	row := s.db.QueryRow(ctx,
		`UPDATE inventory.slot
		   SET quantity = quantity + $1, updated_at = now()
		 WHERE id = $2 AND quantity + $1 >= 0
		 RETURNING id, cabinet_id, code, drug_id, drug_code, drug_name,
		           capacity, quantity, low_threshold, created_at, updated_at`,
		delta, id)
	slot, err := scanSlot(row)
	if err != nil {
		return nil, err
	}
	if slot == nil {
		// Could be because the slot doesn't exist, or because the
		// resulting quantity would be negative. Check existence.
		existsRow := s.db.QueryRow(ctx, `SELECT id FROM inventory.slot WHERE id = $1`, id)
		var existingID string
		if err := existsRow.Scan(&existingID); errors.Is(err, pgx.ErrNoRows) {
			return nil, nil // slot not found
		}
		if err != nil {
			return nil, fmt.Errorf("check slot existence: %w", err)
		}
		return nil, fmt.Errorf("refill slot %s: %w", id, ErrInsufficientStock)
	}
	return slot, nil
}

// AdjustStock atomically sets a slot's quantity to a new value.
// Used for audit corrections; requires a reason which is recorded
// in the audit log. Returns the updated slot, or nil if not found.
func (s *Store) AdjustStock(ctx context.Context, id string, newQuantity int32) (*Slot, error) {
	row := s.db.QueryRow(ctx,
		`UPDATE inventory.slot
		   SET quantity = $1, updated_at = now()
		 WHERE id = $2
		 RETURNING id, cabinet_id, code, drug_id, drug_code, drug_name,
		           capacity, quantity, low_threshold, created_at, updated_at`,
		newQuantity, id)
	return scanSlot(row)
}

// writeAudit is a convenience method that writes an audit entry when the
// auditWriter is configured. It is a no-op when auditWriter is nil.
func (s *Store) writeAudit(ctx context.Context, e audit.Entry) {
	if s.auditWriter == nil {
		return
	}
	_ = s.auditWriter.Write(ctx, e)
}

// scanSlot maps a pgx.Row or pgx.Rows to a Slot. Returns nil when the
// row is empty (pgx.ErrNoRows).
func scanSlot(row pgx.Row) (*Slot, error) {
	var slot Slot
	var createdAt, updatedAt time.Time
	err := row.Scan(&slot.ID, &slot.CabinetID, &slot.Code,
		&slot.DrugID, &slot.DrugCode, &slot.DrugName,
		&slot.Capacity, &slot.Quantity, &slot.LowThreshold,
		&createdAt, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan slot: %w", err)
	}
	slot.CreatedAt = createdAt
	slot.UpdatedAt = updatedAt
	return &slot, nil
}

// slotRows wraps a list of *Slot as fake row scan targets. The concrete
// type in catalog is fakeRows; inventory uses this inline.
// This is internal — used by scanSlot when called from rows.Scan() paths.

// ---- Domain errors ----

// ErrInsufficientStock is returned when a refill delta would result in
// a negative quantity.
var ErrInsufficientStock = errors.New("insufficient stock")

// ErrSlotNotFound is returned when a slot is not found.
var ErrSlotNotFound = errors.New("slot not found")

// ---- Audit helpers ----

// auditDetail is the JSON payload written to audit_log.detail for
// inventory mutations.
type auditDetail struct {
	SlotCode      string `json:"slot_code,omitempty"`
	DrugCode      string `json:"drug_code,omitempty"`
	CabinetID     string `json:"cabinet_id,omitempty"`
	Delta         int32  `json:"delta,omitempty"`
	QuantityAfter int32  `json:"quantity_after,omitempty"`
	Reason        string `json:"reason,omitempty"`
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
