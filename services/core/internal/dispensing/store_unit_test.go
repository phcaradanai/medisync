package dispensing

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/adm-chura3inter/medisync/services/core/internal/testutil"
)

func newTestPrescription(id string) Prescription {
	now := time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
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

// --- SQL and argument verification ---

func TestStoreInsertSQLAndArgs(t *testing.T) {
	fake := &testutil.FakeExecer{
		ReturnTag: pgconn.NewCommandTag("INSERT 0 1"),
	}
	store := NewStoreWithDB(fake)
	p := newTestPrescription("RX-SQL-001")

	inserted, err := store.Insert(context.Background(), p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !inserted {
		t.Error("expected inserted=true")
	}
	if len(fake.Calls) != 1 {
		t.Fatalf("expected 1 Exec call, got %d", len(fake.Calls))
	}

	call := fake.LastCall()

	// Verify SQL template includes expected clauses.
	if !strings.Contains(call.SQL, "INSERT INTO medisync.prescription") {
		t.Error("SQL missing INSERT INTO medisync.prescription")
	}
	if !strings.Contains(call.SQL, "ON CONFLICT ON CONSTRAINT prescription_external_key DO NOTHING") {
		t.Error("SQL missing ON CONFLICT clause")
	}
	if !strings.Contains(call.SQL, "'READY'") {
		t.Errorf("SQL missing READY state literal\ngot: %s", call.SQL)
	}

	// Verify argument values in order.
	if len(call.Args) != 7 {
		t.Fatalf("expected 7 arguments, got %d", len(call.Args))
	}
	if call.Args[0] != "RX-SQL-001" {
		t.Errorf("arg[0] prescription_id = %v, want RX-SQL-001", call.Args[0])
	}
	if call.Args[1] != "test-his" {
		t.Errorf("arg[1] source_system = %v, want test-his", call.Args[1])
	}
	if call.Args[2] != "HN000001" {
		t.Errorf("arg[2] hn = %v, want HN000001", call.Args[2])
	}
	if call.Args[3] != "Test Patient" {
		t.Errorf("arg[3] patient_name = %v, want Test Patient", call.Args[3])
	}
	if call.Args[4] != "WARD-3A" {
		t.Errorf("arg[4] ward_id = %v, want WARD-3A", call.Args[4])
	}
	// arg[5] is the JSON-marshalled items
	if itemsJSON, ok := call.Args[5].([]byte); !ok || len(itemsJSON) == 0 {
		t.Errorf("arg[5] items = %v, want non-empty []byte", call.Args[5])
	}
	// arg[6] is issued_at
	if call.Args[6] == nil {
		t.Error("arg[6] issued_at is nil, want non-nil time")
	}
}

// --- Inserted vs duplicate ---

func TestStoreInsertReturnsInserted(t *testing.T) {
	fake := &testutil.FakeExecer{
		ReturnTag: pgconn.NewCommandTag("INSERT 0 1"), // 1 row affected
	}
	store := NewStoreWithDB(fake)

	inserted, err := store.Insert(context.Background(), newTestPrescription("RX-INS-001"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !inserted {
		t.Error("inserted should be true when RowsAffected == 1")
	}
}

func TestStoreInsertDuplicateReturnsNotInserted(t *testing.T) {
	fake := &testutil.FakeExecer{
		ReturnTag: pgconn.NewCommandTag("INSERT 0 0"), // 0 rows affected = ON CONFLICT DO NOTHING
	}
	store := NewStoreWithDB(fake)

	inserted, err := store.Insert(context.Background(), newTestPrescription("RX-DUP-001"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inserted {
		t.Error("inserted should be false when RowsAffected == 0 (duplicate)")
	}
}

// --- Database error propagation ---

func TestStoreInsertDBError(t *testing.T) {
	fake := &testutil.FakeExecer{
		ReturnErr: errors.New("connection refused"),
	}
	store := NewStoreWithDB(fake)

	_, err := store.Insert(context.Background(), newTestPrescription("RX-ERR-001"))
	if err == nil {
		t.Fatal("expected error from DB, got nil")
	}
	if !strings.Contains(err.Error(), "insert prescription") {
		t.Errorf("error should wrap with 'insert prescription': %v", err)
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("error should contain original: %v", err)
	}
}

// --- READY state behavior ---

func TestStoreInsertSetsReadyState(t *testing.T) {
	fake := &testutil.FakeExecer{
		ReturnTag: pgconn.NewCommandTag("INSERT 0 1"),
	}
	store := NewStoreWithDB(fake)

	_, err := store.Insert(context.Background(), newTestPrescription("RX-RDY-001"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sql := fake.LastCall().SQL
	if !strings.Contains(sql, "'READY'") {
		t.Errorf("SQL must set state='READY'\ngot: %s", sql)
	}
}

// --- Items serialization ---
//
// Note: json.Marshal of []Item{...} cannot fail with any valid Item struct
// because all fields are basic string/int types. The marshal error path in
// Insert is defensive but unreachable in practice. A unit test for marshal
// failure would require an Items type with a custom MarshalJSON that returns
// an error, which is out of scope for the current data model.

// --- Empty items serialized correctly ---

func TestStoreInsertEmptyItems(t *testing.T) {
	fake := &testutil.FakeExecer{
		ReturnTag: pgconn.NewCommandTag("INSERT 0 1"),
	}
	store := NewStoreWithDB(fake)
	p := newTestPrescription("RX-EMPTY-001")
	p.Items = []Item{}

	inserted, err := store.Insert(context.Background(), p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !inserted {
		t.Error("expected inserted=true")
	}

	// Empty items should marshal as "[]"
	itemsJSON := fake.LastCall().Args[5].([]byte)
	if string(itemsJSON) != "[]" {
		t.Errorf("empty items should marshal as '[]', got %q", string(itemsJSON))
	}
}
