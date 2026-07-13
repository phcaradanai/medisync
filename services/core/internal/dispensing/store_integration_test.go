//go:build integration

package dispensing

import (
	"context"
	"encoding/json"
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
// Returns the store and the tx handle for read-back verification.
func txStore(t *testing.T) (*Store, pgx.Tx, func()) {
	t.Helper()
	pool := integrationPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	store := NewStoreWithDB(tx)
	cleanup := func() {
		tx.Rollback(ctx) //nolint:errcheck
	}
	return store, tx, cleanup
}

func uniqueID(t *testing.T, prefix string) string {
	t.Helper()
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func newIntegrationPrescription(id string) Prescription {
	now := time.Now()
	return Prescription{
		PrescriptionID: id,
		SourceSystem:   "test-his",
		HN:             "HN000001",
		PatientName:    "Test Patient",
		WardID:         "WARD-3A",
		IssuedAt:       &now,
		Items: []Item{
			{DrugCode: "PARA500", DrugName: "Paracetamol 500 mg", Quantity: 10},
		},
	}
}

func TestStoreInsert_Integration(t *testing.T) {
	store, _, cleanup := txStore(t)
	defer cleanup()

	p := newIntegrationPrescription(uniqueID(t, "RX-INS"))
	inserted, err := store.Insert(context.Background(), p)
	if err != nil {
		t.Fatalf("first Insert: %v", err)
	}
	if !inserted {
		t.Error("first insert should succeed (inserted=true)")
	}
}

func TestStoreInsertDuplicate_Integration(t *testing.T) {
	store, _, cleanup := txStore(t)
	defer cleanup()

	p := newIntegrationPrescription(uniqueID(t, "RX-DUP"))
	inserted, err := store.Insert(context.Background(), p)
	if err != nil {
		t.Fatalf("first Insert: %v", err)
	}
	if !inserted {
		t.Fatal("first insert should succeed")
	}

	// Duplicate with same (prescription_id, source_system).
	inserted, err = store.Insert(context.Background(), p)
	if err != nil {
		t.Fatalf("duplicate Insert: %v", err)
	}
	if inserted {
		t.Error("duplicate insert should return inserted=false (idempotent)")
	}
}

func TestStoreInsertDuplicateDifferentSource_Integration(t *testing.T) {
	store, _, cleanup := txStore(t)
	defer cleanup()

	p := newIntegrationPrescription(uniqueID(t, "RX-DDS"))
	inserted, err := store.Insert(context.Background(), p)
	if err != nil {
		t.Fatalf("first Insert: %v", err)
	}
	if !inserted {
		t.Fatal("first insert should succeed")
	}

	// Same prescription_id, different source_system → distinct row.
	p2 := p
	p2.SourceSystem = "other-his"
	inserted, err = store.Insert(context.Background(), p2)
	if err != nil {
		t.Fatalf("different-source Insert: %v", err)
	}
	if !inserted {
		t.Error("different source_system should create a new row (inserted=true)")
	}
}

func TestStoreInsertStoresItems_Integration(t *testing.T) {
	store, tx, cleanup := txStore(t)
	defer cleanup()

	p := newIntegrationPrescription(uniqueID(t, "RX-ITM"))
	p.Items = []Item{
		{DrugCode: "PARA500", DrugName: "Paracetamol 500 mg", Quantity: 10},
		{DrugCode: "AMOX500", DrugName: "Amoxicillin 500 mg", Quantity: 21, DosageText: "Take 3x daily"},
	}

	inserted, err := store.Insert(context.Background(), p)
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if !inserted {
		t.Fatal("insert should succeed")
	}

	// Read back items via the same transaction.
	var itemsRaw []byte
	err = tx.QueryRow(context.Background(),
		`SELECT items FROM dispensing.prescription WHERE prescription_id = $1 AND source_system = $2`,
		p.PrescriptionID, p.SourceSystem,
	).Scan(&itemsRaw)
	if err != nil {
		t.Fatalf("read back items: %v", err)
	}
	if len(itemsRaw) == 0 {
		t.Error("items must not be empty")
	}

	// Verify the items can be deserialized.
	var items []Item
	if err := json.Unmarshal(itemsRaw, &items); err != nil {
		t.Fatalf("unmarshal items: %v", err)
	}
	if len(items) != 2 {
		t.Errorf("expected 2 items, got %d", len(items))
	}
}

func TestStoreInsertSetsReadyState_Integration(t *testing.T) {
	store, tx, cleanup := txStore(t)
	defer cleanup()

	p := newIntegrationPrescription(uniqueID(t, "RX-STT"))
	_, err := store.Insert(context.Background(), p)
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	// Read back state via the same transaction.
	var state string
	err = tx.QueryRow(context.Background(),
		`SELECT state FROM dispensing.prescription WHERE prescription_id = $1 AND source_system = $2`,
		p.PrescriptionID, p.SourceSystem,
	).Scan(&state)
	if err != nil {
		t.Fatalf("read back state: %v", err)
	}
	if state != "READY" {
		t.Errorf("state = %q, want READY", state)
	}
}
