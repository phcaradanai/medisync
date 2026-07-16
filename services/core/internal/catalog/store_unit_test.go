package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/adm-chura3inter/medisync/services/core/internal/platform/audit"
)

// ── Test fakes ──────────────────────────────────────────────────────

// fakeDB implements dbConn for unit tests. It records calls and returns
// configured values.
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

// fakeRow implements pgx.Row. It holds destination pointer pairs and an error.
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

// rowWithDrug returns a fakeRow that fills dest with a sample drug.
// Matches the 13-column scan used by all catalog queries.
func rowWithDrug(d Drug) *fakeRow {
	return &fakeRow{
		scanFn: func(dest ...any) error {
			if len(dest) != 13 {
				return fmt.Errorf("expected 13 dests, got %d", len(dest))
			}
			*(dest[0].(*string)) = d.ID
			*(dest[1].(*string)) = d.Code
			*(dest[2].(*string)) = d.Name
			*(dest[3].(*string)) = d.DisplayName
			*(dest[4].(*string)) = d.GenericName
			*(dest[5].(*string)) = d.Form
			*(dest[6].(*string)) = d.Strength
			*(dest[7].(*string)) = d.Unit
			*(dest[8].(*string)) = d.StickerNote
			*(dest[9].(*bool)) = d.Active
			*(dest[10].(*string)) = d.ProjectID
			if dt, ok := dest[11].(*time.Time); ok {
				*dt = d.CreatedAt
			}
			if dt, ok := dest[12].(*time.Time); ok {
				*dt = d.UpdatedAt
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

// fakeRows implements pgx.Rows for testing List operations.
type fakeRows struct {
	drugs   []*Drug
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
	return r.current <= len(r.drugs)
}

func (r *fakeRows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	if r.current < 1 || r.current > len(r.drugs) {
		return errors.New("no row to scan")
	}
	d := r.drugs[r.current-1]
	if len(dest) != 13 {
		return fmt.Errorf("expected 13 dests, got %d", len(dest))
	}
	*(dest[0].(*string)) = d.ID
	*(dest[1].(*string)) = d.Code
	*(dest[2].(*string)) = d.Name
	*(dest[3].(*string)) = d.DisplayName
	*(dest[4].(*string)) = d.GenericName
	*(dest[5].(*string)) = d.Form
	*(dest[6].(*string)) = d.Strength
	*(dest[7].(*string)) = d.Unit
	*(dest[8].(*string)) = d.StickerNote
	*(dest[9].(*bool)) = d.Active
	*(dest[10].(*string)) = d.ProjectID
	if dt, ok := dest[11].(*time.Time); ok {
		*dt = d.CreatedAt
	}
	if dt, ok := dest[12].(*time.Time); ok {
		*dt = d.UpdatedAt
	}
	return nil
}

func (r *fakeRows) CommandTag() pgconn.CommandTag { return pgconn.CommandTag{} }
func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeRows) Values() ([]any, error) { return nil, nil }
func (r *fakeRows) RawValues() [][]byte { return nil }
func (r *fakeRows) Conn() *pgx.Conn { return nil }

// ── scanDrug tests ──────────────────────────────────────────────────

func TestScanDrugSuccess(t *testing.T) {
	expected := Drug{
		ID:          "550e8400-e29b-41d4-a716-446655440000",
		Code:        "PARA-500",
		Name:        "Paracetamol",
		GenericName: "Paracetamol",
		Form:        "tablet",
		Strength:    "500mg",
		Unit:        "tab",
		StickerNote: "Take with food",
		Active:      true,
	}

	row := &fakeRow{
		scanFn: func(dest ...any) error {
			if len(dest) != 13 {
				return fmt.Errorf("expected 13 dests, got %d", len(dest))
			}
			*(dest[0].(*string)) = expected.ID
			*(dest[1].(*string)) = expected.Code
			*(dest[2].(*string)) = expected.Name
			*(dest[3].(*string)) = expected.DisplayName
			*(dest[4].(*string)) = expected.GenericName
			*(dest[5].(*string)) = expected.Form
			*(dest[6].(*string)) = expected.Strength
			*(dest[7].(*string)) = expected.Unit
			*(dest[8].(*string)) = expected.StickerNote
			*(dest[9].(*bool)) = expected.Active
			*(dest[10].(*string)) = expected.ProjectID
			// CreatedAt and UpdatedAt at positions 11 and 12
			return nil
		},
	}

	d, err := scanDrug(row)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d == nil {
		t.Fatal("expected drug, got nil")
	}
	if d.ID != expected.ID {
		t.Errorf("ID = %q, want %q", d.ID, expected.ID)
	}
	if d.Code != expected.Code {
		t.Errorf("Code = %q, want %q", d.Code, expected.Code)
	}
	if d.Name != expected.Name {
		t.Errorf("Name = %q, want %q", d.Name, expected.Name)
	}
	if d.Active != expected.Active {
		t.Errorf("Active = %v, want %v", d.Active, expected.Active)
	}
}

func TestScanDrugNoRows(t *testing.T) {
	d, err := scanDrug(rowWithNoRows())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != nil {
		t.Errorf("expected nil drug for no rows, got %+v", d)
	}
}

func TestScanDrugError(t *testing.T) {
	scanErr := errors.New("connection lost")
	d, err := scanDrug(rowWithError(scanErr))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "scan drug") {
		t.Errorf("error should wrap with 'scan drug': %v", err)
	}
	if !strings.Contains(err.Error(), "connection lost") {
		t.Errorf("error should contain original: %v", err)
	}
	if d != nil {
		t.Errorf("expected nil drug on error, got %+v", d)
	}
}

// ── Store.Create tests ──────────────────────────────────────────────

func TestCreateDrugSuccess(t *testing.T) {
	now := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
	expected := Drug{
		ID:        "new-uuid",
		Code:      "PARA-500",
		Name:      "Paracetamol",
		Active:    true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	db := &fakeDB{queryRow: rowWithDrug(expected)}
	store := NewStoreWithDB(db, nil)

	d, err := store.Create(context.Background(), Drug{
		Code: "PARA-500",
		Name: "Paracetamol",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if d == nil {
		t.Fatal("expected drug, got nil")
	}
	if d.Code != "PARA-500" {
		t.Errorf("Code = %q, want PARA-500", d.Code)
	}
	if d.ID != "new-uuid" {
		t.Errorf("ID = %q, want new-uuid", d.ID)
	}

	call := db.lastQueryRow()
	if !strings.Contains(call.sql, "INSERT INTO catalog.drug") {
		t.Error("SQL should be INSERT INTO catalog.drug")
	}
	if len(call.args) != 9 {
		t.Errorf("expected 9 args (with display_name + project_id), got %d", len(call.args))
	}
}

func TestCreateDrugDBError(t *testing.T) {
	db := &fakeDB{queryRow: rowWithError(errors.New("duplicate key"))}
	store := NewStoreWithDB(db, nil)

	_, err := store.Create(context.Background(), Drug{Code: "PARA-500", Name: "P"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate key") {
		t.Errorf("error should contain 'duplicate key': %v", err)
	}
}

// ── Store.GetByID tests ─────────────────────────────────────────────

func TestGetByIDSuccess(t *testing.T) {
	expected := Drug{ID: "drug-1", Code: "PARA-500", Name: "Paracetamol", Active: true}
	db := &fakeDB{queryRow: rowWithDrug(expected)}
	store := NewStoreWithDB(db, nil)

	d, err := store.GetByID(context.Background(), "drug-1")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if d == nil {
		t.Fatal("expected drug, got nil")
	}
	if d.ID != "drug-1" {
		t.Errorf("ID = %q, want drug-1", d.ID)
	}

	call := db.lastQueryRow()
	if !strings.Contains(call.sql, "FROM catalog.drug") {
		t.Error("SQL should reference catalog.drug")
	}
	if !strings.Contains(call.sql, "id = $1") {
		t.Error("SQL should filter by id")
	}
}

func TestGetByIDNotFound(t *testing.T) {
	db := &fakeDB{queryRow: rowWithNoRows()}
	store := NewStoreWithDB(db, nil)

	d, err := store.GetByID(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if d != nil {
		t.Errorf("expected nil for unknown id, got %+v", d)
	}
}

func TestGetByIDDBError(t *testing.T) {
	db := &fakeDB{queryRow: rowWithError(errors.New("timeout"))}
	store := NewStoreWithDB(db, nil)

	_, err := store.GetByID(context.Background(), "id")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Errorf("error should contain 'timeout': %v", err)
	}
}

// ── Store.GetByCode tests ───────────────────────────────────────────

func TestGetByCodeSuccess(t *testing.T) {
	expected := Drug{ID: "drug-2", Code: "AMOX-250", Name: "Amoxicillin", Active: true}
	db := &fakeDB{queryRow: rowWithDrug(expected)}
	store := NewStoreWithDB(db, nil)

	d, err := store.GetByCode(context.Background(), "AMOX-250")
	if err != nil {
		t.Fatalf("GetByCode: %v", err)
	}
	if d == nil {
		t.Fatal("expected drug, got nil")
	}
	if d.Code != "AMOX-250" {
		t.Errorf("Code = %q, want AMOX-250", d.Code)
	}

	call := db.lastQueryRow()
	if !strings.Contains(call.sql, "code = $1") {
		t.Error("SQL should filter by code")
	}
}

// ── Store.Update tests ──────────────────────────────────────────────

func TestUpdateDrugSuccess(t *testing.T) {
	now := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
	expected := Drug{
		ID:        "drug-1",
		Code:      "PARA-500",
		Name:      "Paracetamol Updated",
		Active:    true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	db := &fakeDB{queryRow: rowWithDrug(expected)}
	store := NewStoreWithDB(db, nil)

	d, err := store.Update(context.Background(), Drug{
		ID:   "drug-1",
		Code: "PARA-500",
		Name: "Paracetamol Updated",
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if d == nil {
		t.Fatal("expected drug, got nil")
	}
	if d.Name != "Paracetamol Updated" {
		t.Errorf("Name = %q, want 'Paracetamol Updated'", d.Name)
	}

	call := db.lastQueryRow()
	if !strings.Contains(call.sql, "UPDATE catalog.drug") {
		t.Error("SQL should be UPDATE catalog.drug")
	}
}

func TestUpdateDrugNotFound(t *testing.T) {
	db := &fakeDB{queryRow: rowWithNoRows()}
	store := NewStoreWithDB(db, nil)

	d, err := store.Update(context.Background(), Drug{ID: "ghost", Code: "X", Name: "X"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if d != nil {
		t.Errorf("expected nil drug for not-found, got %+v", d)
	}
}

// ── Store.Deactivate tests ──────────────────────────────────────────

func TestDeactivateDrugSuccess(t *testing.T) {
	expected := Drug{ID: "drug-1", Code: "PARA-500", Name: "Paracetamol", Active: false}
	db := &fakeDB{queryRow: rowWithDrug(expected)}
	store := NewStoreWithDB(db, nil)

	d, err := store.Deactivate(context.Background(), "drug-1")
	if err != nil {
		t.Fatalf("Deactivate: %v", err)
	}
	if d == nil {
		t.Fatal("expected drug, got nil")
	}
	if d.Active {
		t.Error("drug should be inactive after deactivation")
	}

	call := db.lastQueryRow()
	if !strings.Contains(call.sql, "UPDATE catalog.drug") {
		t.Error("SQL should be UPDATE catalog.drug")
	}
	if !strings.Contains(call.sql, "active = false") {
		t.Error("SQL should set active = false")
	}
	if !strings.Contains(call.sql, "active = true") {
		t.Error("SQL should check active = true in WHERE")
	}
}

func TestDeactivateDrugNotFound(t *testing.T) {
	db := &fakeDB{queryRow: rowWithNoRows()}
	store := NewStoreWithDB(db, nil)

	d, err := store.Deactivate(context.Background(), "ghost")
	if err != nil {
		t.Fatalf("Deactivate: %v", err)
	}
	if d != nil {
		t.Errorf("expected nil for not-found, got %+v", d)
	}
}

// ── Store.List tests ─────────────────────────────────────────────────

func TestListDrugsEmpty(t *testing.T) {
	db := &fakeDB{
		queryRows: &fakeRows{drugs: nil},
	}
	store := NewStoreWithDB(db, nil)

	drugs, nextToken, err := store.List(context.Background(), "", false, 50, "", "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(drugs) != 0 {
		t.Errorf("expected 0 drugs, got %d", len(drugs))
	}
	if nextToken != "" {
		t.Errorf("nextToken = %q, want empty", nextToken)
	}
}

func TestListDrugsWithResults(t *testing.T) {
	db := &fakeDB{
		queryRows: &fakeRows{
			drugs: []*Drug{
				{ID: "d1", Code: "PARA-500", Name: "Paracetamol", Active: true},
				{ID: "d2", Code: "AMOX-250", Name: "Amoxicillin", Active: true},
			},
		},
	}
	store := NewStoreWithDB(db, nil)

	drugs, nextToken, err := store.List(context.Background(), "", false, 50, "", "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(drugs) != 2 {
		t.Errorf("expected 2 drugs, got %d", len(drugs))
	}
	if nextToken != "" {
		t.Errorf("nextToken = %q, want empty", nextToken)
	}
}

func TestListDrugsPagination(t *testing.T) {
	db := &fakeDB{
		queryRows: &fakeRows{
			drugs: []*Drug{
				{ID: "d1", Code: "A", Name: "A", Active: true},
				{ID: "d2", Code: "B", Name: "B", Active: true},
				{ID: "d3", Code: "C", Name: "C", Active: true}, // extra for next page
			},
		},
	}
	store := NewStoreWithDB(db, nil)

	drugs, nextToken, err := store.List(context.Background(), "", false, 2, "", "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(drugs) != 2 {
		t.Errorf("expected 2 drugs, got %d", len(drugs))
	}
	if nextToken != "d2" {
		t.Errorf("nextToken = %q, want d2", nextToken)
	}
}

func TestListDrugsWithQuery(t *testing.T) {
	db := &fakeDB{
		queryRows: &fakeRows{
			drugs: []*Drug{
				{ID: "d1", Code: "PARA-500", Name: "Paracetamol", Active: true},
			},
		},
	}
	store := NewStoreWithDB(db, nil)

	_, _, err := store.List(context.Background(), "para", false, 50, "", "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	call := db.lastQuery()
	if !strings.Contains(strings.ToUpper(call.sql), "ILIKE") {
		t.Error("query list should use ILIKE for search")
	}
}

func TestListDrugsDBError(t *testing.T) {
	db := &fakeDB{
		queryErr: errors.New("connection refused"),
	}
	store := NewStoreWithDB(db, nil)

	_, _, err := store.List(context.Background(), "", false, 50, "", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("error should contain 'connection refused': %v", err)
	}
}

// ── writeAudit tests ────────────────────────────────────────────────

func TestWriteAuditNoopWhenNil(t *testing.T) {
	store := NewStoreWithDB(&fakeDB{}, nil)
	// Should not panic when auditWriter is nil.
	store.writeAudit(context.Background(), audit.Entry{
		Action: "test", Entity: "test",
	})
}
