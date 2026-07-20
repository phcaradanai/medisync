//go:build integration

package inventory

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func integrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Fatal("TEST_DATABASE_URL is required for integration tests. Set it to a test database URL.")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Fatalf("ping test database: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
	})
	return pool
}

// txStore creates a Store backed by a transaction that is always rolled back.
func txStore(t *testing.T) (*Store, pgx.Tx, func()) {
	t.Helper()
	pool := integrationPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	store := NewStoreWithDB(tx, nil)
	cleanup := func() {
		tx.Rollback(ctx) //nolint:errcheck
	}
	return store, tx, cleanup
}

func uniqueCabinetCode(t *testing.T, prefix string) (string, string) {
	t.Helper()
	ts := time.Now().UnixNano()
	return fmt.Sprintf("cab-%s-%d", prefix, ts), fmt.Sprintf("%s-%d", prefix, ts)
}

// seedSlot inserts a bare slot row (no drug assignment) and returns it.
func seedSlot(t *testing.T, store *Store, cabinetID, code string) *Slot {
	t.Helper()
	row := store.db.(pgx.Tx).QueryRow(context.Background(),
		`INSERT INTO medisync.slot (cabinet_id, code, capacity, quantity, low_threshold)
		 VALUES ($1, $2, 0, 0, 0)
		 RETURNING id, cabinet_id, code, drug_id, drug_code, drug_name,
		           capacity, quantity, low_threshold, created_at, updated_at`,
		cabinetID, code)
	slot, err := scanSlot(row)
	if err != nil {
		t.Fatalf("seed slot: %v", err)
	}
	if slot == nil {
		t.Fatal("seed slot returned nil")
	}
	return slot
}

// ── CRUD integration tests ──────────────────────────────────────────

func TestStoreGetByID_Integration(t *testing.T) {
	store, _, cleanup := txStore(t)
	defer cleanup()

	cabinetID, code := uniqueCabinetCode(t, "GID")
	slot := seedSlot(t, store, cabinetID, code)

	got, err := store.GetByID(context.Background(), slot.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got == nil {
		t.Fatal("expected slot, got nil")
	}
	if got.ID != slot.ID {
		t.Errorf("ID = %q, want %q", got.ID, slot.ID)
	}
	if got.Code != code {
		t.Errorf("Code = %q, want %q", got.Code, code)
	}
}

func TestStoreGetByIDNotFound_Integration(t *testing.T) {
	store, _, cleanup := txStore(t)
	defer cleanup()

	slot, err := store.GetByID(context.Background(), "00000000-0000-0000-0000-000000000000")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if slot != nil {
		t.Errorf("expected nil for unknown id, got %+v", slot)
	}
}

func TestStoreGetByCabinetAndCode_Integration(t *testing.T) {
	store, _, cleanup := txStore(t)
	defer cleanup()

	cabinetID, code := uniqueCabinetCode(t, "GBC")
	seeded := seedSlot(t, store, cabinetID, code)

	got, err := store.GetByCabinetAndCode(context.Background(), cabinetID, code)
	if err != nil {
		t.Fatalf("GetByCabinetAndCode: %v", err)
	}
	if got == nil {
		t.Fatal("expected slot, got nil")
	}
	if got.ID != seeded.ID {
		t.Errorf("ID = %q, want %q", got.ID, seeded.ID)
	}
}

func TestStoreListSlots_Integration(t *testing.T) {
	store, tx, cleanup := txStore(t)
	defer cleanup()

	// Clear slots in this transaction.
	_, err := tx.Exec(context.Background(), `DELETE FROM medisync.slot`)
	if err != nil {
		t.Fatalf("clear slots: %v", err)
	}

	cab1, code1 := uniqueCabinetCode(t, "L1")
	cab2, code2 := uniqueCabinetCode(t, "L2")
	seedSlot(t, store, cab1, code1)
	seedSlot(t, store, cab2, code2)

	slots, nextToken, totalCount, err := store.ListSlots(context.Background(), "", "", false, 1, "")
	if err != nil {
		t.Fatalf("ListSlots: %v", err)
	}
	if len(slots) != 1 || nextToken == "" || totalCount != 2 {
		t.Errorf("page 1 = len %d, token %q, total %d", len(slots), nextToken, totalCount)
	}

	page2, nextToken2, totalCount2, err := store.ListSlots(context.Background(), "", "", false, 1, nextToken)
	if err != nil {
		t.Fatalf("ListSlots page 2: %v", err)
	}
	if len(page2) != 1 || nextToken2 != "" || totalCount2 != 2 {
		t.Errorf("page 2 = len %d, token %q, total %d", len(page2), nextToken2, totalCount2)
	}
}

func TestStoreListSlotsFilterByCabinet_Integration(t *testing.T) {
	store, tx, cleanup := txStore(t)
	defer cleanup()

	_, err := tx.Exec(context.Background(), `DELETE FROM medisync.slot`)
	if err != nil {
		t.Fatalf("clear slots: %v", err)
	}

	cab1, code1 := uniqueCabinetCode(t, "FC1")
	cab2, code2 := uniqueCabinetCode(t, "FC2")
	seedSlot(t, store, cab1, code1)
	seedSlot(t, store, cab2, code2)

	slots, _, totalCount, err := store.ListSlots(context.Background(), cab1, "", false, 50, "")
	if err != nil {
		t.Fatalf("ListSlots: %v", err)
	}
	if len(slots) != 1 {
		t.Errorf("expected 1 slot for cabinet %s, got %d", cab1, len(slots))
	}
	if totalCount != 1 {
		t.Errorf("totalCount = %d, want 1", totalCount)
	}
}

func TestStoreAssignDrug_Integration(t *testing.T) {
	store, _, cleanup := txStore(t)
	defer cleanup()

	cabinetID, code := uniqueCabinetCode(t, "AD")
	slot := seedSlot(t, store, cabinetID, code)

	assigned, err := store.AssignDrug(context.Background(), slot.ID, "drug-1", "PARA-500", "Paracetamol", 100, 10)
	if err != nil {
		t.Fatalf("AssignDrug: %v", err)
	}
	if assigned == nil {
		t.Fatal("expected slot after assign, got nil")
	}
	if assigned.DrugID != "drug-1" {
		t.Errorf("DrugID = %q, want drug-1", assigned.DrugID)
	}
	if assigned.DrugCode != "PARA-500" {
		t.Errorf("DrugCode = %q, want PARA-500", assigned.DrugCode)
	}
	if assigned.Capacity != 100 {
		t.Errorf("Capacity = %d, want 100", assigned.Capacity)
	}
	if assigned.LowThreshold != 10 {
		t.Errorf("LowThreshold = %d, want 10", assigned.LowThreshold)
	}
}

func TestStoreAssignDrugNotFound_Integration(t *testing.T) {
	store, _, cleanup := txStore(t)
	defer cleanup()

	slot, err := store.AssignDrug(context.Background(), "00000000-0000-0000-0000-000000000000", "drug-1", "", "", 100, 10)
	if err != nil {
		t.Fatalf("AssignDrug: %v", err)
	}
	if slot != nil {
		t.Errorf("expected nil for unknown id, got %+v", slot)
	}
}

func TestStoreRefill_Integration(t *testing.T) {
	store, _, cleanup := txStore(t)
	defer cleanup()

	cabinetID, code := uniqueCabinetCode(t, "RF")
	slot := seedSlot(t, store, cabinetID, code)
	// Assign a drug with capacity.
	_, err := store.AssignDrug(context.Background(), slot.ID, "drug-1", "PARA-500", "Paracetamol", 100, 10)
	if err != nil {
		t.Fatalf("AssignDrug: %v", err)
	}

	refilled, err := store.Refill(context.Background(), slot.ID, 50, nil)
	if err != nil {
		t.Fatalf("Refill: %v", err)
	}
	if refilled == nil {
		t.Fatal("expected slot after refill, got nil")
	}
	if refilled.Quantity != 50 {
		t.Errorf("Quantity = %d, want 50", refilled.Quantity)
	}
}

func TestStoreRefillInsufficientStock_Integration(t *testing.T) {
	store, _, cleanup := txStore(t)
	defer cleanup()

	cabinetID, code := uniqueCabinetCode(t, "RIS")
	slot := seedSlot(t, store, cabinetID, code)
	// Assign a drug with capacity and add some stock.
	_, err := store.AssignDrug(context.Background(), slot.ID, "drug-1", "PARA-500", "Paracetamol", 100, 10)
	if err != nil {
		t.Fatalf("AssignDrug: %v", err)
	}
	_, err = store.Refill(context.Background(), slot.ID, 10, nil)
	if err != nil {
		t.Fatalf("initial refill: %v", err)
	}

	// Try to remove more than we have.
	_, err = store.Refill(context.Background(), slot.ID, -20, nil)
	if err == nil {
		t.Fatal("expected insufficient stock error, got nil")
	}
}

func TestStoreAdjustStock_Integration(t *testing.T) {
	store, _, cleanup := txStore(t)
	defer cleanup()

	cabinetID, code := uniqueCabinetCode(t, "AS")
	slot := seedSlot(t, store, cabinetID, code)
	_, err := store.AssignDrug(context.Background(), slot.ID, "drug-1", "PARA-500", "Paracetamol", 100, 10)
	if err != nil {
		t.Fatalf("AssignDrug: %v", err)
	}
	_, err = store.Refill(context.Background(), slot.ID, 100, nil)
	if err != nil {
		t.Fatalf("initial refill: %v", err)
	}

	adjusted, err := store.AdjustStock(context.Background(), slot.ID, 25)
	if err != nil {
		t.Fatalf("AdjustStock: %v", err)
	}
	if adjusted == nil {
		t.Fatal("expected slot after adjust, got nil")
	}
	if adjusted.Quantity != 25 {
		t.Errorf("Quantity = %d, want 25", adjusted.Quantity)
	}
}

// ── Schema verification on fresh Postgres ───────────────────────────

func TestInventorySchemaExists_Integration(t *testing.T) {
	pool := integrationPool(t)
	defer pool.Close()

	var exists bool
	err := pool.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname = 'inventory')`).Scan(&exists)
	if err != nil {
		t.Fatalf("check inventory schema: %v", err)
	}
	if !exists {
		t.Fatal("inventory schema does not exist — migrations may not have run")
	}
}

func TestSlotTableExists_Integration(t *testing.T) {
	pool := integrationPool(t)
	defer pool.Close()

	var exists bool
	err := pool.QueryRow(context.Background(),
		`SELECT EXISTS(
		   SELECT 1 FROM information_schema.tables
		   WHERE table_schema = 'inventory' AND table_name = 'slot'
		)`).Scan(&exists)
	if err != nil {
		t.Fatalf("check slot table: %v", err)
	}
	if !exists {
		t.Fatal("medisync.slot table does not exist — 0006 migration may not have run")
	}
}

func TestSlotColumns_Integration(t *testing.T) {
	pool := integrationPool(t)
	defer pool.Close()

	expectedCols := []string{
		"id", "cabinet_id", "code", "drug_id", "drug_code", "drug_name",
		"capacity", "quantity", "low_threshold", "created_at", "updated_at",
	}

	for _, col := range expectedCols {
		var exists bool
		err := pool.QueryRow(context.Background(),
			`SELECT EXISTS(
			   SELECT 1 FROM information_schema.columns
			   WHERE table_schema = 'inventory' AND table_name = 'slot'
			   AND column_name = $1
			)`, col).Scan(&exists)
		if err != nil {
			t.Fatalf("check column %s: %v", col, err)
		}
		if !exists {
			t.Errorf("column medisync.slot.%s does not exist", col)
		}
	}
}

func TestSlotUniqueConstraint_Integration(t *testing.T) {
	store, _, cleanup := txStore(t)
	defer cleanup()

	cabinetID, code := uniqueCabinetCode(t, "UNIQ")
	_, err := store.db.(pgx.Tx).Exec(context.Background(),
		`INSERT INTO medisync.slot (cabinet_id, code, capacity, quantity, low_threshold)
		 VALUES ($1, $2, 0, 0, 0)`, cabinetID, code)
	if err != nil {
		t.Fatalf("first insert: %v", err)
	}

	// Second insert with the same cabinet_id + code should fail.
	_, err = store.db.(pgx.Tx).Exec(context.Background(),
		`INSERT INTO medisync.slot (cabinet_id, code, capacity, quantity, low_threshold)
		 VALUES ($1, $2, 0, 0, 0)`, cabinetID, code)
	if err == nil {
		t.Fatal("expected unique constraint violation, got nil")
	}
}

func TestSlotCapacityCheck_Integration(t *testing.T) {
	store, _, cleanup := txStore(t)
	defer cleanup()

	cabinetID, code := uniqueCabinetCode(t, "CC")
	// Negative capacity should be rejected by the CHECK constraint.
	_, err := store.db.(pgx.Tx).Exec(context.Background(),
		`INSERT INTO medisync.slot (cabinet_id, code, capacity, quantity, low_threshold)
		 VALUES ($1, $2, -1, 0, 0)`, cabinetID, code)
	if err == nil {
		t.Fatal("expected check constraint violation for negative capacity")
	}
}
