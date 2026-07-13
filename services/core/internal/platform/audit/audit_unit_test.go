package audit

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/adm-chura3inter/medisync/services/core/internal/testutil"
)

// --- Action/entity validation ---

func TestWriteRequiresActionAndEntity(t *testing.T) {
	fake := &testutil.FakeExecer{}
	w := NewWriterWithDB(fake)

	tests := []struct {
		name  string
		entry Entry
	}{
		{"empty action", Entry{Entity: "prescription", Action: ""}},
		{"empty entity", Entry{Action: "test.action", Entity: ""}},
		{"both empty", Entry{Action: "", Entity: ""}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := w.Write(context.Background(), tt.entry)
			if err == nil {
				t.Error("expected error for missing action/entity, got nil")
			}
		})
	}

	// Validation happens before any DB call — no Exec should have been made.
	if len(fake.Calls) != 0 {
		t.Errorf("validation should prevent DB calls, got %d Exec calls", len(fake.Calls))
	}
}

// --- Default actor ---

func TestWriteDefaultsActor(t *testing.T) {
	fake := &testutil.FakeExecer{}
	w := NewWriterWithDB(fake)

	e := Entry{
		Action:   "test.default.actor",
		Entity:   "test",
		EntityID: "test-1",
		TraceID:  "trace-001",
		// Actor intentionally empty — should default to "system".
	}

	err := w.Write(context.Background(), e)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if len(fake.Calls) != 1 {
		t.Fatalf("expected 1 Exec call, got %d", len(fake.Calls))
	}

	call := fake.LastCall()
	// Args: trace_id, actor, action, entity, entity_id, detail
	if len(call.Args) < 2 {
		t.Fatal("expected at least 2 args")
	}
	if actor, ok := call.Args[1].(string); !ok || actor != "system" {
		t.Errorf("actor arg = %v, want system", call.Args[1])
	}
}

// --- Actor explicitly set ---

func TestWriteWithActor(t *testing.T) {
	fake := &testutil.FakeExecer{}
	w := NewWriterWithDB(fake)

	e := Entry{
		Action:   "test.with.actor",
		Entity:   "test",
		EntityID: "test-2",
		Actor:    "user-42",
		TraceID:  "trace-002",
	}

	err := w.Write(context.Background(), e)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	call := fake.LastCall()
	if actor, ok := call.Args[1].(string); !ok || actor != "user-42" {
		t.Errorf("actor arg = %v, want user-42", call.Args[1])
	}
}

// --- Detail serialization ---

func TestWriteWithDetail(t *testing.T) {
	fake := &testutil.FakeExecer{}
	w := NewWriterWithDB(fake)

	e := Entry{
		Action:   "test.with.detail",
		Entity:   "test",
		EntityID: "test-3",
		TraceID:  "trace-003",
		Detail: map[string]any{
			"ward_id": "WARD-3A",
			"items":   2,
			"nested": map[string]any{
				"key": "value",
			},
		},
	}

	err := w.Write(context.Background(), e)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	call := fake.LastCall()
	// Detail is arg[5] — should be JSON bytes.
	detailRaw, ok := call.Args[5].([]byte)
	if !ok {
		t.Fatalf("detail arg[5] is not []byte, got %T", call.Args[5])
	}

	var detail map[string]any
	if err := json.Unmarshal(detailRaw, &detail); err != nil {
		t.Fatalf("unmarshal detail: %v", err)
	}
	if v, ok := detail["ward_id"]; !ok || v != "WARD-3A" {
		t.Errorf("ward_id = %v", v)
	}
	if v, ok := detail["items"]; !ok || v != float64(2) {
		t.Errorf("items = %v", v)
	}
	if nested, ok := detail["nested"].(map[string]any); !ok || nested["key"] != "value" {
		t.Error("nested detail not preserved")
	}
}

// --- Empty detail (nil) ---

func TestWriteEmptyDetail(t *testing.T) {
	fake := &testutil.FakeExecer{}
	w := NewWriterWithDB(fake)

	e := Entry{
		Action:   "test.empty.detail",
		Entity:   "test",
		EntityID: "test-4",
		TraceID:  "trace-004",
		Detail:   nil,
	}

	err := w.Write(context.Background(), e)
	if err != nil {
		t.Fatalf("Write with nil detail: %v", err)
	}

	call := fake.LastCall()
	detailRaw := call.Args[5].([]byte)
	if string(detailRaw) != "{}" {
		t.Errorf("nil detail should be '{}', got %q", string(detailRaw))
	}
}

// --- Empty structured detail ---

func TestWriteEmptyStructDetail(t *testing.T) {
	fake := &testutil.FakeExecer{}
	w := NewWriterWithDB(fake)

	e := Entry{
		Action:   "test.empty.struct",
		Entity:   "test",
		EntityID: "test-4b",
		TraceID:  "trace-004b",
		Detail:   struct{}{},
	}

	err := w.Write(context.Background(), e)
	if err != nil {
		t.Fatalf("Write with empty struct detail: %v", err)
	}

	call := fake.LastCall()
	detailRaw := call.Args[5].([]byte)
	if string(detailRaw) != "{}" {
		t.Errorf("empty struct detail should be '{}', got %q", string(detailRaw))
	}
}

// --- Detail marshal failure ---

func TestWriteDetailMarshalError(t *testing.T) {
	fake := &testutil.FakeExecer{}
	w := NewWriterWithDB(fake)

	// A channel cannot be marshaled to JSON.
	e := Entry{
		Action:   "test.bad.detail",
		Entity:   "test",
		EntityID: "test-5",
		TraceID:  "trace-005",
		Detail:   make(chan int),
	}

	err := w.Write(context.Background(), e)
	if err == nil {
		t.Error("expected marshal error for unmarshalable detail, got nil")
	}
	if !strings.Contains(err.Error(), "marshal audit detail") {
		t.Errorf("error should wrap with 'marshal audit detail': %v", err)
	}

	// No DB call should have been made.
	if len(fake.Calls) != 0 {
		t.Errorf("marshal error should prevent DB call, got %d", len(fake.Calls))
	}
}

// --- Database error propagation ---

func TestWriteDBError(t *testing.T) {
	fake := &testutil.FakeExecer{
		ReturnErr: errors.New("connection refused"),
	}
	w := NewWriterWithDB(fake)

	e := Entry{
		Action:   "test.db.error",
		Entity:   "test",
		EntityID: "test-6",
		TraceID:  "trace-006",
	}

	err := w.Write(context.Background(), e)
	if err == nil {
		t.Fatal("expected error from DB, got nil")
	}
	if !strings.Contains(err.Error(), "write audit log") {
		t.Errorf("error should wrap with 'write audit log': %v", err)
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("error should contain original: %v", err)
	}
}

// --- SQL and argument verification ---

func TestWriteSQLAndArgs(t *testing.T) {
	fake := &testutil.FakeExecer{
		ReturnTag: pgconn.NewCommandTag("INSERT 0 1"),
	}
	w := NewWriterWithDB(fake)

	e := Entry{
		Action:   "prescription.received",
		Entity:   "prescription",
		EntityID: "RX-001",
		Actor:    "user-99",
		TraceID:  "trace-sql-001",
		Detail:   map[string]any{"count": 3},
	}

	err := w.Write(context.Background(), e)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	call := fake.LastCall()

	if !strings.Contains(call.SQL, "INSERT INTO audit.audit_log") {
		t.Error("SQL missing INSERT INTO audit.audit_log")
	}
	if !strings.Contains(call.SQL, "trace_id") || !strings.Contains(call.SQL, "actor") {
		t.Error("SQL missing expected columns")
	}

	if len(call.Args) != 6 {
		t.Fatalf("expected 6 args, got %d", len(call.Args))
	}
	if call.Args[0] != "trace-sql-001" {
		t.Errorf("arg[0] trace_id = %v", call.Args[0])
	}
	if call.Args[1] != "user-99" {
		t.Errorf("arg[1] actor = %v", call.Args[1])
	}
	if call.Args[2] != "prescription.received" {
		t.Errorf("arg[2] action = %v", call.Args[2])
	}
	if call.Args[3] != "prescription" {
		t.Errorf("arg[3] entity = %v", call.Args[3])
	}
	if call.Args[4] != "RX-001" {
		t.Errorf("arg[4] entity_id = %v", call.Args[4])
	}
	detailRaw := call.Args[5].([]byte)
	if !strings.Contains(string(detailRaw), "count") {
		t.Errorf("arg[5] detail missing 'count': %s", string(detailRaw))
	}
}
