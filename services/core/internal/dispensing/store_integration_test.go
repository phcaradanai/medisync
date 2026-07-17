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

	"google.golang.org/protobuf/encoding/protojson"

	eventsv1 "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/events/v1"
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

// TestGetByID_Integration fetches a prescription by internal UUID after insert.
func TestGetByID_Integration(t *testing.T) {
	store, tx, cleanup := txStore(t)
	defer cleanup()
	ctx := context.Background()

	p := newIntegrationPrescription(uniqueID(t, "RX-GBY"))
	inserted, err := store.Insert(ctx, p)
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if !inserted {
		t.Fatal("insert should succeed")
	}

	// Read back the row to get its UUID.
	var id string
	err = tx.QueryRow(ctx,
		`SELECT id FROM dispensing.prescription WHERE prescription_id = $1 AND source_system = $2`,
		p.PrescriptionID, p.SourceSystem).Scan(&id)
	if err != nil {
		t.Fatalf("read id: %v", err)
	}

	pr, err := store.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if pr == nil {
		t.Fatal("expected prescription, got nil")
	}
	if pr.PrescriptionID != p.PrescriptionID {
		t.Errorf("prescription_id = %q, want %q", pr.PrescriptionID, p.PrescriptionID)
	}
	if pr.State != StateReady {
		t.Errorf("state = %q, want READY", pr.State)
	}
	if pr.WardID != p.WardID {
		t.Errorf("ward_id = %q, want %q", pr.WardID, p.WardID)
	}
	if len(pr.Items) != len(p.Items) {
		t.Errorf("items count = %d, want %d", len(pr.Items), len(p.Items))
	}
}

// TestGetByIDNotFound_Integration returns nil for unknown ID.
func TestGetByIDNotFound_Integration(t *testing.T) {
	store, _, cleanup := txStore(t)
	defer cleanup()

	pr, err := store.GetByID(context.Background(), "00000000-0000-0000-0000-000000000000")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if pr != nil {
		t.Error("expected nil for unknown UUID")
	}
}

// TestGetByPrescriptionID_Integration fetches by external key.
func TestGetByPrescriptionID_Integration(t *testing.T) {
	store, _, cleanup := txStore(t)
	defer cleanup()
	ctx := context.Background()

	p := newIntegrationPrescription(uniqueID(t, "RX-GBP"))
	_, err := store.Insert(ctx, p)
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	pr, err := store.GetByPrescriptionID(ctx, p.PrescriptionID, p.SourceSystem)
	if err != nil {
		t.Fatalf("GetByPrescriptionID: %v", err)
	}
	if pr == nil {
		t.Fatal("expected prescription, got nil")
	}
	if pr.PrescriptionID != p.PrescriptionID {
		t.Errorf("prescription_id = %q, want %q", pr.PrescriptionID, p.PrescriptionID)
	}
}

// TestListByWard_Integration filters by ward.
func TestListByWard_Integration(t *testing.T) {
	store, _, cleanup := txStore(t)
	defer cleanup()
	ctx := context.Background()

	wardA := "WARD-A"
	wardB := "WARD-B"

	// Insert prescriptions for two wards.
	for _, ward := range []string{wardA, wardA, wardB} {
		p := newIntegrationPrescription(uniqueID(t, "RX-LBW"))
		p.WardID = ward
		_, err := store.Insert(ctx, p)
		if err != nil {
			t.Fatalf("Insert for ward %s: %v", ward, err)
		}
	}

	rows, nextToken, totalCount, err := store.ListByWard(ctx, []string{wardA}, nil, 1, "")
	if err != nil {
		t.Fatalf("ListByWard: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("ward A first page should have 1 prescription, got %d", len(rows))
	}
	if totalCount != 2 {
		t.Errorf("ward A totalCount = %d, want 2", totalCount)
	}
	if nextToken == "" {
		t.Fatal("ward A nextPageToken should not be empty")
	}
	for _, r := range rows {
		if r.WardID != wardA {
			t.Errorf("expected ward %s, got %s", wardA, r.WardID)
		}
	}

	rowsPage2, nextToken2, totalCount2, err := store.ListByWard(ctx, []string{wardA}, nil, 1, nextToken)
	if err != nil {
		t.Fatalf("ListByWard page 2: %v", err)
	}
	if len(rowsPage2) != 1 || nextToken2 != "" || totalCount2 != 2 {
		t.Errorf("ward A page 2 = len %d, token %q, total %d", len(rowsPage2), nextToken2, totalCount2)
	}

	rowsB, _, totalB, err := store.ListByWard(ctx, []string{wardB}, nil, 50, "")
	if err != nil {
		t.Fatalf("ListByWard ward B: %v", err)
	}
	if len(rowsB) != 1 {
		t.Errorf("ward B should have 1 prescription, got %d", len(rowsB))
	}
	if totalB != 1 {
		t.Errorf("ward B totalCount = %d, want 1", totalB)
	}
}

// TestListByWardWithStateFilter_Integration filters by state.
func TestListByWardWithStateFilter_Integration(t *testing.T) {
	store, tx, cleanup := txStore(t)
	defer cleanup()
	ctx := context.Background()

	ward := "WARD-FILTER"

	// Insert a prescription and manually set it to DISPENSING.
	p := newIntegrationPrescription(uniqueID(t, "RX-STF"))
	p.WardID = ward
	_, err := store.Insert(ctx, p)
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	// Use tx to update state to DISPENSING.
	var id string
	err = tx.QueryRow(ctx,
		`UPDATE dispensing.prescription SET state = 'DISPENSING'
		 WHERE prescription_id = $1 AND source_system = $2
		 RETURNING id`, p.PrescriptionID, p.SourceSystem).Scan(&id)
	if err != nil {
		t.Fatalf("update state: %v", err)
	}

	// List with READY state filter — should return 0.
	rows, _, _, err := store.ListByWard(ctx, []string{ward}, []State{StateReady}, 50, "")
	if err != nil {
		t.Fatalf("ListByWard READY: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("READY filter should return 0, got %d", len(rows))
	}

	// List with DISPENSING state filter — should return 1.
	rows, _, _, err = store.ListByWard(ctx, []string{ward}, []State{StateDispensing}, 50, "")
	if err != nil {
		t.Fatalf("ListByWard DISPENSING: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("DISPENSING filter should return 1, got %d", len(rows))
	}
}

// TestTransitionState_Integration validates the guard and writes outbox.
func TestTransitionState_Integration(t *testing.T) {
	store, tx, cleanup := txStore(t)
	defer cleanup()
	ctx := context.Background()

	p := newIntegrationPrescription(uniqueID(t, "RX-TRN"))
	_, err := store.Insert(ctx, p)
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	// Read the inserted row id.
	var id string
	err = tx.QueryRow(ctx,
		`SELECT id FROM dispensing.prescription WHERE prescription_id = $1 AND source_system = $2`,
		p.PrescriptionID, p.SourceSystem).Scan(&id)
	if err != nil {
		t.Fatalf("read id: %v", err)
	}

	// Build outbox payload.
	outboxEvent := &eventsv1.DispenseRequested{
		DispenseId:     "disp-001",
		PrescriptionId: p.PrescriptionID,
		TraceId:        "trace-001",
	}
	outboxPayload, err := protojson.Marshal(outboxEvent)
	if err != nil {
		t.Fatalf("marshal outbox: %v", err)
	}

	// Transition READY → DISPENSING.
	updated, err := store.TransitionState(ctx, tx, id, StateReady, StateDispensing, outboxPayload)
	if err != nil {
		t.Fatalf("TransitionState: %v", err)
	}
	if updated == nil {
		t.Fatal("expected updated prescription")
	}
	if updated.State != StateDispensing {
		t.Errorf("state = %q, want DISPENSING", updated.State)
	}

	// Verify outbox row was inserted (scoped to this test's prescription to avoid
	// cross-test contamination under parallel execution).
	var outboxCount int
	err = tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM dispensing.outbox
		 WHERE subject = 'medisync.dispense.requested'
		 AND payload ->> 'prescriptionId' = $1`,
		p.PrescriptionID,
	).Scan(&outboxCount)
	if err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	if outboxCount != 1 {
		t.Errorf("expected 1 outbox row, got %d", outboxCount)
	}

	// Verify outbox payload is valid JSON (scoped to this test's prescription).
	var payloadRaw []byte
	err = tx.QueryRow(ctx,
		`SELECT payload FROM dispensing.outbox
		 WHERE subject = 'medisync.dispense.requested'
		 AND payload ->> 'prescriptionId' = $1
		 LIMIT 1`,
		p.PrescriptionID,
	).Scan(&payloadRaw)
	if err != nil {
		t.Fatalf("read outbox payload: %v", err)
	}
	var parsed eventsv1.DispenseRequested
	if err := protojson.Unmarshal(payloadRaw, &parsed); err != nil {
		t.Fatalf("unmarshal outbox payload: %v", err)
	}
	if parsed.GetDispenseId() != "disp-001" {
		t.Errorf("dispense_id = %q, want disp-001", parsed.GetDispenseId())
	}
}

// TestTransitionStateInvalid_Integration rejects illegal transitions.
func TestTransitionStateInvalid_Integration(t *testing.T) {
	store, tx, cleanup := txStore(t)
	defer cleanup()
	ctx := context.Background()

	p := newIntegrationPrescription(uniqueID(t, "RX-INV"))
	_, err := store.Insert(ctx, p)
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	var id string
	err = tx.QueryRow(ctx,
		`SELECT id FROM dispensing.prescription WHERE prescription_id = $1 AND source_system = $2`,
		p.PrescriptionID, p.SourceSystem).Scan(&id)
	if err != nil {
		t.Fatalf("read id: %v", err)
	}

	// Attempt READY → DISPENSED (invalid — must go through DISPENSING).
	_, err = store.TransitionState(ctx, tx, id, StateReady, StateDispensed, nil)
	if err == nil {
		t.Fatal("expected error for invalid transition READY → DISPENSED")
	}
}

// TestTransitionStateAtomic_Integration verifies the WHERE clause guard.
func TestTransitionStateAtomic_Integration(t *testing.T) {
	store, tx, cleanup := txStore(t)
	defer cleanup()
	ctx := context.Background()

	p := newIntegrationPrescription(uniqueID(t, "RX-ATM"))
	_, err := store.Insert(ctx, p)
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	var id string
	err = tx.QueryRow(ctx,
		`SELECT id FROM dispensing.prescription WHERE prescription_id = $1 AND source_system = $2`,
		p.PrescriptionID, p.SourceSystem).Scan(&id)
	if err != nil {
		t.Fatalf("read id: %v", err)
	}

	// Transition from a wrong current state — the WHERE clause prevents update.
	_, err = store.TransitionState(ctx, tx, id, StateDispensing, StateDispensed, nil)
	if err == nil {
		t.Fatal("expected error when current state doesn't match")
	}
}

// TestOutboxSchema_Integration verifies the outbox table structure.
func TestOutboxSchema_Integration(t *testing.T) {
	store, tx, cleanup := txStore(t)
	defer cleanup()
	ctx := context.Background()

	// Verify outbox table exists with expected columns.
	columns := []struct {
		name     string
		dataType string
	}{
		{"id", "bigint"},
		{"subject", "text"},
		{"payload", "jsonb"},
		{"created_at", "timestamp with time zone"},
		{"published", "boolean"},
	}

	for _, col := range columns {
		var exists bool
		err := tx.QueryRow(ctx,
			`SELECT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_schema = 'dispensing' AND table_name = 'outbox' AND column_name = $1
				AND data_type = $2)`, col.name, col.dataType).Scan(&exists)
		if err != nil {
			t.Fatalf("check column %s: %v", col.name, err)
		}
		if !exists {
			t.Errorf("outbox column %s (%s) not found", col.name, col.dataType)
		}
	}

	// Verify partial index on unpublished rows.
	var indexExists bool
	err := tx.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM pg_indexes
			WHERE schemaname = 'dispensing' AND tablename = 'outbox'
			AND indexname = 'outbox_unpublished_idx')`).Scan(&indexExists)
	if err != nil {
		t.Fatalf("check index: %v", err)
	}
	if !indexExists {
		t.Error("outbox_unpublished_idx index not found")
	}

	_ = store // unused in this test but required for txStore pattern
}

// TestPrescriptionSchemaUnchanged_Integration verifies M1 schema is intact.
func TestPrescriptionSchemaUnchanged_Integration(t *testing.T) {
	store, tx, cleanup := txStore(t)
	defer cleanup()
	ctx := context.Background()

	// Verify prescription table still has all M1 columns.
	var count int
	err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM information_schema.columns
		 WHERE table_schema = 'dispensing' AND table_name = 'prescription'`).Scan(&count)
	if err != nil {
		t.Fatalf("count columns: %v", err)
	}
	if count != 13 {
		t.Errorf("prescription table should have 13 columns, got %d", count)
	}

	// Verify state CHECK constraint still exists.
	var constraintExists bool
	err = tx.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM pg_constraint c
			JOIN pg_namespace n ON n.oid = c.connamespace
			WHERE n.nspname = 'dispensing'
			AND conname = 'prescription_state_check')`).Scan(&constraintExists)
	if err != nil {
		t.Fatalf("check constraint: %v", err)
	}
	if !constraintExists {
		t.Error("prescription_state_check constraint is missing")
	}

	_ = store
}

// TestInsertStoresAsJSON_Integration verifies items are stored as valid JSON.
func TestInsertStoresAsJSON_Integration(t *testing.T) {
	store, tx, cleanup := txStore(t)
	defer cleanup()
	ctx := context.Background()

	p := newIntegrationPrescription(uniqueID(t, "RX-JSN"))
	p.Items = []Item{
		{DrugCode: "DRUG-A", DrugName: "Drug A", Quantity: 5, DosageText: "Once daily"},
	}

	_, err := store.Insert(ctx, p)
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	var itemsRaw []byte
	err = tx.QueryRow(ctx,
		`SELECT items FROM dispensing.prescription WHERE prescription_id = $1 AND source_system = $2`,
		p.PrescriptionID, p.SourceSystem,
	).Scan(&itemsRaw)
	if err != nil {
		t.Fatalf("read items: %v", err)
	}
	if len(itemsRaw) == 0 {
		t.Fatal("items must not be empty")
	}

	var items []Item
	if err := json.Unmarshal(itemsRaw, &items); err != nil {
		t.Fatalf("unmarshal items: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 item, got %d", len(items))
	}
}
