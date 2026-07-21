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

func uniqueSlotCode(t *testing.T, prefix string) string {
	t.Helper()
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

type integrationScope struct {
	projectID string
	kioskCode string
}

type rowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func seedProject(t *testing.T, db rowQuerier) (string, string) {
	t.Helper()
	unique := time.Now().UnixNano()
	var projectID, projectCode string
	if err := db.QueryRow(context.Background(),
		`INSERT INTO medisync.projects (name, slug, display_name)
		 VALUES ($1, $2, $1) RETURNING id, code`,
		fmt.Sprintf("Inventory Integration %d", unique), fmt.Sprintf("inventory-integration-%d", unique),
	).Scan(&projectID, &projectCode); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	return projectID, projectCode
}

func seedKiosk(t *testing.T, db rowQuerier, projectID string, sequence int32) string {
	t.Helper()
	var kioskCode string
	if err := db.QueryRow(context.Background(),
		`INSERT INTO medisync.kiosks (display_name, pin_hash, project_id, kiosk_sequence)
		 VALUES ($1, 'integration-test-pin-hash', $2, $3) RETURNING code`,
		fmt.Sprintf("Inventory Test Cabinet %d", sequence), projectID, sequence,
	).Scan(&kioskCode); err != nil {
		t.Fatalf("seed kiosk: %v", err)
	}
	return kioskCode
}

func seedScope(t *testing.T, db rowQuerier) integrationScope {
	t.Helper()
	projectID, _ := seedProject(t, db)
	return integrationScope{projectID: projectID, kioskCode: seedKiosk(t, db, projectID, 1)}
}

// seedSlot inserts a bare slot row (no drug assignment) and returns it.
func seedSlot(t *testing.T, store *Store, scope integrationScope, code string) *Slot {
	t.Helper()
	slot, err := store.CreateSlot(context.Background(), scope.kioskCode, code, code, scope.projectID, 0, 0, 1, 1, nil)
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

	scope := seedScope(t, store.db.(pgx.Tx))
	code := uniqueSlotCode(t, "GID")
	slot := seedSlot(t, store, scope, code)

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

	scope := seedScope(t, store.db.(pgx.Tx))
	code := uniqueSlotCode(t, "GBC")
	seeded := seedSlot(t, store, scope, code)

	got, err := store.GetByCabinetAndCode(context.Background(), scope.kioskCode, code)
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

	projectID, _ := seedProject(t, tx)
	scope1 := integrationScope{projectID: projectID, kioskCode: seedKiosk(t, tx, projectID, 1)}
	scope2 := integrationScope{projectID: projectID, kioskCode: seedKiosk(t, tx, projectID, 2)}
	seedSlot(t, store, scope1, uniqueSlotCode(t, "L1"))
	seedSlot(t, store, scope2, uniqueSlotCode(t, "L2"))

	slots, nextToken, totalCount, err := store.ListSlots(context.Background(), "", projectID, false, 1, "")
	if err != nil {
		t.Fatalf("ListSlots: %v", err)
	}
	if len(slots) != 1 || nextToken == "" || totalCount != 2 {
		t.Errorf("page 1 = len %d, token %q, total %d", len(slots), nextToken, totalCount)
	}

	page2, nextToken2, totalCount2, err := store.ListSlots(context.Background(), "", projectID, false, 1, nextToken)
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

	projectID, _ := seedProject(t, tx)
	scope1 := integrationScope{projectID: projectID, kioskCode: seedKiosk(t, tx, projectID, 1)}
	scope2 := integrationScope{projectID: projectID, kioskCode: seedKiosk(t, tx, projectID, 2)}
	seedSlot(t, store, scope1, uniqueSlotCode(t, "FC1"))
	seedSlot(t, store, scope2, uniqueSlotCode(t, "FC2"))

	slots, _, totalCount, err := store.ListSlots(context.Background(), scope1.kioskCode, projectID, false, 50, "")
	if err != nil {
		t.Fatalf("ListSlots: %v", err)
	}
	if len(slots) != 1 {
		t.Errorf("expected 1 slot for cabinet %s, got %d", scope1.kioskCode, len(slots))
	}
	if totalCount != 1 {
		t.Errorf("totalCount = %d, want 1", totalCount)
	}
}

func TestStoreAssignDrug_Integration(t *testing.T) {
	store, _, cleanup := txStore(t)
	defer cleanup()

	scope := seedScope(t, store.db.(pgx.Tx))
	slot := seedSlot(t, store, scope, uniqueSlotCode(t, "AD"))

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

	scope := seedScope(t, store.db.(pgx.Tx))
	slot := seedSlot(t, store, scope, uniqueSlotCode(t, "RF"))
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

	scope := seedScope(t, store.db.(pgx.Tx))
	slot := seedSlot(t, store, scope, uniqueSlotCode(t, "RIS"))
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

	scope := seedScope(t, store.db.(pgx.Tx))
	slot := seedSlot(t, store, scope, uniqueSlotCode(t, "AS"))
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

func TestStoreUpdateEmergencyConfigByBusinessCodes_Integration(t *testing.T) {
	store, _, cleanup := txStore(t)
	defer cleanup()

	scope := seedScope(t, store.db.(pgx.Tx))
	code := uniqueSlotCode(t, "EMERGENCY")
	seedSlot(t, store, scope, code)

	updated, err := store.UpdateEmergencyConfig(context.Background(), scope.kioskCode, code, scope.projectID, true, 3)
	if err != nil {
		t.Fatalf("UpdateEmergencyConfig: %v", err)
	}
	if updated == nil || !updated.EmergencyDrug || updated.EmergencyMaxQuantity != 3 {
		t.Fatalf("emergency config = %+v, want enabled with max 3", updated)
	}

	otherProjectID, _ := seedProject(t, store.db.(pgx.Tx))
	notUpdated, err := store.UpdateEmergencyConfig(context.Background(), scope.kioskCode, code, otherProjectID, false, 1)
	if err != nil {
		t.Fatalf("cross-project UpdateEmergencyConfig: %v", err)
	}
	if notUpdated != nil {
		t.Fatal("cross-project business-code update must not find the slot")
	}
}

// ── Schema verification on fresh Postgres ───────────────────────────

func TestInventorySchemaExists_Integration(t *testing.T) {
	pool := integrationPool(t)
	defer pool.Close()

	var exists bool
	err := pool.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname = 'medisync')`).Scan(&exists)
	if err != nil {
		t.Fatalf("check inventory schema: %v", err)
	}
	if !exists {
		t.Fatal("medisync schema does not exist — migrations may not have run")
	}
}

func TestSlotTableExists_Integration(t *testing.T) {
	pool := integrationPool(t)
	defer pool.Close()

	var exists bool
	err := pool.QueryRow(context.Background(),
		`SELECT EXISTS(
		   SELECT 1 FROM information_schema.tables
		   WHERE table_schema = 'medisync' AND table_name = 'slot'
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
		"capacity", "quantity", "low_threshold", "project_id", "shelf", "row_num",
		"emergency_drug", "emergency_max_quantity", "created_at", "updated_at",
	}

	for _, col := range expectedCols {
		var exists bool
		err := pool.QueryRow(context.Background(),
			`SELECT EXISTS(
			   SELECT 1 FROM information_schema.columns
			   WHERE table_schema = 'medisync' AND table_name = 'slot'
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

	scope := seedScope(t, store.db.(pgx.Tx))
	code := uniqueSlotCode(t, "UNIQ")
	_, err := store.db.(pgx.Tx).Exec(context.Background(),
		`INSERT INTO medisync.slot (cabinet_id, code, project_id, capacity, quantity, low_threshold)
		 VALUES ($1, $2, $3, 0, 0, 0)`, scope.kioskCode, code, scope.projectID)
	if err != nil {
		t.Fatalf("first insert: %v", err)
	}

	// Second insert with the same cabinet_id + code should fail.
	_, err = store.db.(pgx.Tx).Exec(context.Background(),
		`INSERT INTO medisync.slot (cabinet_id, code, project_id, capacity, quantity, low_threshold)
		 VALUES ($1, $2, $3, 0, 0, 0)`, scope.kioskCode, code, scope.projectID)
	if err == nil {
		t.Fatal("expected unique constraint violation, got nil")
	}
}

func TestSlotCapacityCheck_Integration(t *testing.T) {
	store, _, cleanup := txStore(t)
	defer cleanup()

	scope := seedScope(t, store.db.(pgx.Tx))
	code := uniqueSlotCode(t, "CC")
	// Negative capacity should be rejected by the CHECK constraint.
	_, err := store.db.(pgx.Tx).Exec(context.Background(),
		`INSERT INTO medisync.slot (cabinet_id, code, project_id, capacity, quantity, low_threshold)
		 VALUES ($1, $2, $3, -1, 0, 0)`, scope.kioskCode, code, scope.projectID)
	if err == nil {
		t.Fatal("expected check constraint violation for negative capacity")
	}
}
