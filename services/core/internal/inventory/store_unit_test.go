package inventory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ── Test fakes ──────────────────────────────────────────────────────

// fakeDB implements dbConn for unit tests.
type fakeDB struct {
	execCalls     []execCall
	execTag       pgconn.CommandTag
	execErr       error
	queryRowCalls []queryRowCall
	queryRow      pgx.Row
	queryCalls    []queryCall
	queryRows     *fakeRows
	queryErr      error
}

type execCall struct {
	sql  string
	args []any
}

type queryRowCall struct {
	sql  string
	args []any
}

type queryCall struct {
	sql  string
	args []any
}

func (f *fakeDB) Exec(_ context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	f.execCalls = append(f.execCalls, execCall{sql: sql, args: arguments})
	return f.execTag, f.execErr
}

func (f *fakeDB) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	f.queryCalls = append(f.queryCalls, queryCall{sql: sql, args: args})
	if f.queryRows != nil {
		return f.queryRows, f.queryErr
	}
	return nil, f.queryErr
}

func (f *fakeDB) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	f.queryRowCalls = append(f.queryRowCalls, queryRowCall{sql: sql, args: args})
	if strings.Contains(strings.ToUpper(sql), "COUNT(*)") {
		count := int64(0)
		if f.queryRows != nil {
			count = int64(len(f.queryRows.slots))
		}
		return &fakeRow{scanFn: func(dest ...any) error {
			*(dest[0].(*int64)) = count
			return nil
		}}
	}
	return f.queryRow
}

func (f *fakeDB) lastExec() execCall {
	if len(f.execCalls) == 0 {
		return execCall{}
	}
	return f.execCalls[len(f.execCalls)-1]
}

func (f *fakeDB) lastQueryRow() queryRowCall {
	if len(f.queryRowCalls) == 0 {
		return queryRowCall{}
	}
	return f.queryRowCalls[len(f.queryRowCalls)-1]
}

func (f *fakeDB) lastQuery() queryCall {
	if len(f.queryCalls) == 0 {
		return queryCall{}
	}
	return f.queryCalls[len(f.queryCalls)-1]
}

// ── fakeRow ─────────────────────────────────────────────────────────

type fakeRow struct {
	scanErr error
	scanFn  func(dest ...any) error
}

func (r *fakeRow) Scan(dest ...any) error {
	if r.scanFn != nil {
		return r.scanFn(dest...)
	}
	return r.scanErr
}

// rowWithSlot returns a fakeRow that fills dest with a sample slot.
// Matches the 16-column scan used by inventory queries.
func rowWithSlot(sl Slot) *fakeRow {
	return &fakeRow{
		scanFn: func(dest ...any) error {
			if len(dest) != 16 {
				return fmt.Errorf("expected 16 dests, got %d", len(dest))
			}
			*(dest[0].(*string)) = sl.ID
			*(dest[1].(*string)) = sl.CabinetID
			*(dest[2].(*string)) = sl.Code
			*(dest[3].(*string)) = sl.DisplayName
			*(dest[4].(*string)) = sl.DrugID
			*(dest[5].(*string)) = sl.DrugCode
			*(dest[6].(*string)) = sl.DrugName
			*(dest[7].(*int32)) = sl.Capacity
			*(dest[8].(*int32)) = sl.Quantity
			*(dest[9].(*int32)) = sl.LowThreshold
			*(dest[10].(*string)) = sl.ProjectID
			*(dest[11].(*int32)) = sl.Shelf
			*(dest[12].(*int32)) = sl.RowNum
			if dt, ok := dest[13].(**time.Time); ok {
				*dt = sl.ExpiryDate
			}
			if dt, ok := dest[14].(*time.Time); ok {
				*dt = sl.CreatedAt
			}
			if dt, ok := dest[15].(*time.Time); ok {
				*dt = sl.UpdatedAt
			}
			return nil
		},
	}
}

// rowWithNoRows returns a fakeRow that returns pgx.ErrNoRows.
func rowWithNoRows() *fakeRow {
	return &fakeRow{scanErr: pgx.ErrNoRows}
}

// rowWithError returns a fakeRow that returns an arbitrary error.
func rowWithError(err error) *fakeRow {
	return &fakeRow{scanErr: err}
}

// ── fakeRows ────────────────────────────────────────────────────────

type fakeRows struct {
	slots   []*Slot
	current int
	closed  bool
	scanErr error
}

func (r *fakeRows) Close() {
	r.closed = true
}

func (r *fakeRows) Err() error {
	return nil
}

func (r *fakeRows) Next() bool {
	r.current++
	return r.current <= len(r.slots)
}

func (r *fakeRows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	if r.current < 1 || r.current > len(r.slots) {
		return errors.New("no row to scan")
	}
	sl := r.slots[r.current-1]
	if len(dest) != 16 {
		return fmt.Errorf("expected 16 dests, got %d", len(dest))
	}
	*(dest[0].(*string)) = sl.ID
	*(dest[1].(*string)) = sl.CabinetID
	*(dest[2].(*string)) = sl.Code
	*(dest[3].(*string)) = sl.DisplayName
	*(dest[4].(*string)) = sl.DrugID
	*(dest[5].(*string)) = sl.DrugCode
	*(dest[6].(*string)) = sl.DrugName
	*(dest[7].(*int32)) = sl.Capacity
	*(dest[8].(*int32)) = sl.Quantity
	*(dest[9].(*int32)) = sl.LowThreshold
	*(dest[10].(*string)) = sl.ProjectID
	if dt, ok := dest[11].(**time.Time); ok {
		*dt = sl.ExpiryDate
	}
	*(dest[12].(*int32)) = sl.Shelf
	*(dest[13].(*int32)) = sl.RowNum
	if dt, ok := dest[14].(*time.Time); ok {
		*dt = sl.CreatedAt
	}
	if dt, ok := dest[15].(*time.Time); ok {
		*dt = sl.UpdatedAt
	}
	return nil
}

func (r *fakeRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeRows) Values() ([]any, error)                       { return nil, nil }
func (r *fakeRows) RawValues() [][]byte                          { return nil }
func (r *fakeRows) Conn() *pgx.Conn                              { return nil }

// ── rowWithSlot11 ───────────────────────────────────────────────────

// rowWithSlot11 returns a fakeRow matching the slot scan.
func rowWithSlot11(sl Slot) *fakeRow {
	return &fakeRow{
		scanFn: func(dest ...any) error {
			if len(dest) != 16 {
				return fmt.Errorf("expected 16 dests, got %d", len(dest))
			}
			*(dest[0].(*string)) = sl.ID
			*(dest[1].(*string)) = sl.CabinetID
			*(dest[2].(*string)) = sl.Code
			*(dest[3].(*string)) = sl.DisplayName
			*(dest[4].(*string)) = sl.DrugID
			*(dest[5].(*string)) = sl.DrugCode
			*(dest[6].(*string)) = sl.DrugName
			*(dest[7].(*int32)) = sl.Capacity
			*(dest[8].(*int32)) = sl.Quantity
			*(dest[9].(*int32)) = sl.LowThreshold
			*(dest[10].(*string)) = sl.ProjectID
			if dt, ok := dest[11].(**time.Time); ok {
				*dt = sl.ExpiryDate
			}
			if dt, ok := dest[12].(*time.Time); ok {
				*dt = sl.CreatedAt
			}
			if dt, ok := dest[13].(*time.Time); ok {
				*dt = sl.UpdatedAt
			}
			return nil
		},
	}
}

// ── scanSlot tests ──────────────────────────────────────────────────

func TestScanSlotSuccess(t *testing.T) {
	now := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
	expiryDate := now.AddDate(1, 0, 0)
	expected := Slot{
		ID:         "slot-1",
		CabinetID:  "cab-1",
		Code:       "A01",
		DrugID:     "drug-1",
		DrugCode:   "PARA-500",
		DrugName:   "Paracetamol",
		Capacity:   100,
		Quantity:   50,
		ExpiryDate: &expiryDate,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	row := &fakeRow{
		scanFn: func(dest ...any) error {
			if len(dest) != 16 {
				return fmt.Errorf("expected 16 dests, got %d", len(dest))
			}
			*(dest[0].(*string)) = expected.ID
			*(dest[1].(*string)) = expected.CabinetID
			*(dest[2].(*string)) = expected.Code
			*(dest[3].(*string)) = expected.DisplayName
			*(dest[4].(*string)) = expected.DrugID
			*(dest[5].(*string)) = expected.DrugCode
			*(dest[6].(*string)) = expected.DrugName
			*(dest[7].(*int32)) = expected.Capacity
			*(dest[8].(*int32)) = expected.Quantity
			*(dest[9].(*int32)) = expected.LowThreshold
			*(dest[10].(*string)) = expected.ProjectID
			if dt, ok := dest[11].(**time.Time); ok {
				*dt = expected.ExpiryDate
			}
			if dt, ok := dest[12].(*time.Time); ok {
				*dt = expected.CreatedAt
			}
			if dt, ok := dest[13].(*time.Time); ok {
				*dt = expected.UpdatedAt
			}
			return nil
		},
	}

	slot, err := scanSlot(row)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if slot == nil {
		t.Fatal("expected slot, got nil")
	}
	if slot.ID != expected.ID {
		t.Errorf("ID = %q, want %q", slot.ID, expected.ID)
	}
	if slot.Code != expected.Code {
		t.Errorf("Code = %q, want %q", slot.Code, expected.Code)
	}
	if slot.Quantity != expected.Quantity {
		t.Errorf("Quantity = %d, want %d", slot.Quantity, expected.Quantity)
	}
	if slot.ExpiryDate == nil || !slot.ExpiryDate.Equal(expiryDate) {
		t.Errorf("ExpiryDate = %v, want %v", slot.ExpiryDate, expiryDate)
	}
}

func TestScanSlotNoRows(t *testing.T) {
	slot, err := scanSlot(rowWithNoRows())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if slot != nil {
		t.Errorf("expected nil slot for no rows, got %+v", slot)
	}
}

func TestScanSlotError(t *testing.T) {
	scanErr := errors.New("connection lost")
	slot, err := scanSlot(rowWithError(scanErr))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "scan slot") {
		t.Errorf("error should wrap with 'scan slot': %v", err)
	}
	if slot != nil {
		t.Errorf("expected nil slot on error, got %+v", slot)
	}
}

// ── Store.GetByID tests ─────────────────────────────────────────────

func TestGetByIDSuccess(t *testing.T) {
	now := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
	expected := Slot{ID: "slot-1", CabinetID: "cab-1", Code: "A01", Quantity: 50,
		CreatedAt: now, UpdatedAt: now}
	db := &fakeDB{queryRow: rowWithSlot11(expected)}
	store := NewStoreWithDB(db, nil)

	slot, err := store.GetByID(context.Background(), "slot-1")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if slot == nil {
		t.Fatal("expected slot, got nil")
	}
	if slot.ID != "slot-1" {
		t.Errorf("ID = %q, want slot-1", slot.ID)
	}

	call := db.lastQueryRow()
	if !strings.Contains(call.sql, "FROM inventory.slot") {
		t.Error("SQL should reference inventory.slot")
	}
}

func TestGetByIDNotFound(t *testing.T) {
	db := &fakeDB{queryRow: rowWithNoRows()}
	store := NewStoreWithDB(db, nil)

	slot, err := store.GetByID(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if slot != nil {
		t.Errorf("expected nil for unknown id, got %+v", slot)
	}
}

// ── Store.ListSlots tests ───────────────────────────────────────────

func TestListSlotsEmpty(t *testing.T) {
	db := &fakeDB{queryRows: &fakeRows{}, queryErr: nil}
	store := NewStoreWithDB(db, nil)

	slots, nextToken, totalCount, err := store.ListSlots(context.Background(), "", "", false, 50, "")
	if err != nil {
		t.Fatalf("ListSlots: %v", err)
	}
	if len(slots) != 0 {
		t.Errorf("expected 0 slots, got %d", len(slots))
	}
	if nextToken != "" || totalCount != 0 {
		t.Errorf("pagination = token %q, total %d", nextToken, totalCount)
	}
}

func TestListSlotsWithResults(t *testing.T) {
	now := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
	slots := []*Slot{
		{ID: "s1", CabinetID: "cab-1", Code: "A01", Quantity: 10, CreatedAt: now, UpdatedAt: now},
		{ID: "s2", CabinetID: "cab-1", Code: "A02", Quantity: 20, CreatedAt: now, UpdatedAt: now},
	}
	db := &fakeDB{queryRows: &fakeRows{slots: slots}}
	store := NewStoreWithDB(db, nil)

	result, nextToken, totalCount, err := store.ListSlots(context.Background(), "", "", false, 1, "")
	if err != nil {
		t.Fatalf("ListSlots: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("expected 1 slot, got %d", len(result))
	}
	if nextToken != "s1" || totalCount != 2 {
		t.Errorf("pagination = token %q, total %d", nextToken, totalCount)
	}
}

func TestListSlotsFilterByCabinet(t *testing.T) {
	db := &fakeDB{queryRows: &fakeRows{}}
	store := NewStoreWithDB(db, nil)

	_, _, _, err := store.ListSlots(context.Background(), "cab-1", "", false, 50, "")
	if err != nil {
		t.Fatalf("ListSlots: %v", err)
	}

	call := db.lastQuery()
	if !strings.Contains(call.sql, "cabinet_id = $1") {
		t.Error("SQL should filter by cabinet_id")
	}
}

func TestListSlotsLowOnly(t *testing.T) {
	db := &fakeDB{queryRows: &fakeRows{}}
	store := NewStoreWithDB(db, nil)

	_, _, _, err := store.ListSlots(context.Background(), "", "", true, 50, "")
	if err != nil {
		t.Fatalf("ListSlots: %v", err)
	}

	call := db.lastQuery()
	if !strings.Contains(call.sql, "quantity <= low_threshold") {
		t.Error("SQL should filter by low threshold")
	}
}

func TestListSlotsCursor(t *testing.T) {
	db := &fakeDB{queryRows: &fakeRows{}}
	store := NewStoreWithDB(db, nil)

	_, _, _, err := store.ListSlots(context.Background(), "", "", false, 25, "slot-1")
	if err != nil {
		t.Fatalf("ListSlots: %v", err)
	}
	call := db.lastQuery()
	if !strings.Contains(call.sql, "created_at < (SELECT created_at FROM inventory.slot WHERE id = $1)") {
		t.Errorf("SQL missing created_at cursor: %s", call.sql)
	}
	if got := call.args[len(call.args)-1]; got != int32(26) {
		t.Errorf("LIMIT arg = %v, want 26", got)
	}
}

// ── Store.AssignDrug tests ──────────────────────────────────────────

func TestAssignDrugSuccess(t *testing.T) {
	now := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
	expected := Slot{
		ID: "slot-1", CabinetID: "cab-1", Code: "A01",
		DrugID: "drug-1", DrugCode: "PARA-500", DrugName: "Paracetamol",
		Capacity: 100, Quantity: 50, LowThreshold: 10,
		CreatedAt: now, UpdatedAt: now,
	}
	db := &fakeDB{queryRow: rowWithSlot11(expected)}
	store := NewStoreWithDB(db, nil)

	slot, err := store.AssignDrug(context.Background(), "slot-1", "drug-1", "PARA-500", "Paracetamol", 100, 10)
	if err != nil {
		t.Fatalf("AssignDrug: %v", err)
	}
	if slot == nil {
		t.Fatal("expected slot, got nil")
	}
	if slot.DrugID != "drug-1" {
		t.Errorf("DrugID = %q, want drug-1", slot.DrugID)
	}

	call := db.lastQueryRow()
	if !strings.Contains(call.sql, "UPDATE inventory.slot") {
		t.Error("SQL should be UPDATE inventory.slot")
	}
}

func TestAssignDrugNotFound(t *testing.T) {
	db := &fakeDB{queryRow: rowWithNoRows()}
	store := NewStoreWithDB(db, nil)

	slot, err := store.AssignDrug(context.Background(), "ghost", "drug-1", "", "", 100, 10)
	if err != nil {
		t.Fatalf("AssignDrug: %v", err)
	}
	if slot != nil {
		t.Errorf("expected nil for unknown id, got %+v", slot)
	}
}

// ── Store.Refill tests ──────────────────────────────────────────────

func TestRefillSuccess(t *testing.T) {
	now := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
	expected := Slot{
		ID: "slot-1", CabinetID: "cab-1", Code: "A01",
		DrugID: "drug-1", DrugCode: "PARA-500",
		Capacity: 100, Quantity: 60, LowThreshold: 10,
		CreatedAt: now, UpdatedAt: now,
	}
	db := &fakeDB{queryRow: rowWithSlot11(expected)}
	store := NewStoreWithDB(db, nil)

	slot, err := store.Refill(context.Background(), "slot-1", 10, nil)
	if err != nil {
		t.Fatalf("Refill: %v", err)
	}
	if slot == nil {
		t.Fatal("expected slot, got nil")
	}
	if slot.Quantity != 60 {
		t.Errorf("Quantity = %d, want 60", slot.Quantity)
	}

	call := db.lastQueryRow()
	if !strings.Contains(call.sql, "quantity = quantity + $1") {
		t.Error("SQL should use atomic increment")
	}
}

func TestRefillInsufficientStock(t *testing.T) {
	// Simulate: slot exists but refill would go negative.
	// First QueryRow: UPDATE returns no rows (WHERE fails).
	// Second QueryRow: existence check returns the slot id.
	callCount := 0
	db := &fakeDB{
		queryRow: &fakeRow{
			scanFn: func(dest ...any) error {
				callCount++
				if callCount == 1 {
					return pgx.ErrNoRows // Refill returned no rows
				}
				// Existence check: slot exists
				*(dest[0].(*string)) = "slot-1"
				return nil
			},
		},
	}
	store := NewStoreWithDB(db, nil)

	_, err := store.Refill(context.Background(), "slot-1", -100, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrInsufficientStock) {
		t.Errorf("expected ErrInsufficientStock, got %v", err)
	}
}

func TestRefillNotFound(t *testing.T) {
	// Both the refill and the existence check return no rows.
	db := &fakeDB{queryRow: rowWithNoRows()}
	store := NewStoreWithDB(db, nil)

	slot, err := store.Refill(context.Background(), "ghost", 10, nil)
	if err != nil {
		t.Fatalf("Refill: %v", err)
	}
	if slot != nil {
		t.Errorf("expected nil for unknown id, got %+v", slot)
	}
}

// ── Store.AdjustStock tests ─────────────────────────────────────────

func TestAdjustStockSuccess(t *testing.T) {
	now := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
	expected := Slot{
		ID: "slot-1", CabinetID: "cab-1", Code: "A01",
		DrugID: "drug-1", DrugCode: "PARA-500",
		Capacity: 100, Quantity: 42, LowThreshold: 10,
		CreatedAt: now, UpdatedAt: now,
	}
	db := &fakeDB{queryRow: rowWithSlot11(expected)}
	store := NewStoreWithDB(db, nil)

	slot, err := store.AdjustStock(context.Background(), "slot-1", 42)
	if err != nil {
		t.Fatalf("AdjustStock: %v", err)
	}
	if slot == nil {
		t.Fatal("expected slot, got nil")
	}
	if slot.Quantity != 42 {
		t.Errorf("Quantity = %d, want 42", slot.Quantity)
	}

	call := db.lastQueryRow()
	if !strings.Contains(call.sql, "UPDATE inventory.slot") {
		t.Error("SQL should be UPDATE inventory.slot")
	}
	if !strings.Contains(call.sql, "quantity = $1") {
		t.Error("SQL should SET quantity directly")
	}
}

func TestAdjustStockNotFound(t *testing.T) {
	db := &fakeDB{queryRow: rowWithNoRows()}
	store := NewStoreWithDB(db, nil)

	slot, err := store.AdjustStock(context.Background(), "ghost", 10)
	if err != nil {
		t.Fatalf("AdjustStock: %v", err)
	}
	if slot != nil {
		t.Errorf("expected nil for unknown id, got %+v", slot)
	}
}

// ── Interface compliance ────────────────────────────────────────────

func TestStoreImplementsSlotStore(t *testing.T) {
	var _ SlotStore = (*Store)(nil)
}

// ── toJSON tests ────────────────────────────────────────────────────

func TestToJSONNil(t *testing.T) {
	result := toJSON(nil)
	if string(result) != "{}" {
		t.Errorf("expected {}, got %s", string(result))
	}
}

func TestToJSONValue(t *testing.T) {
	d := auditDetail{SlotCode: "A01", DrugCode: "PARA-500"}
	result := toJSON(d)
	if !strings.Contains(string(result), `"slot_code":"A01"`) {
		t.Errorf("expected slot_code in JSON, got %s", string(result))
	}
}
