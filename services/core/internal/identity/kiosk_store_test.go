package identity

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// fakeKioskRow implements pgx.Row for single-row queries (GetByCode, GetByID).
type fakeKioskRow struct {
	scanErr error
	scanFn  func(dest ...any) error
}

func (r *fakeKioskRow) Scan(dest ...any) error {
	if r.scanFn != nil {
		return r.scanFn(dest...)
	}
	return r.scanErr
}

// kioskScanData returns a Scan function that fills dest with a Kiosk (8 columns).
func kioskScanData(k Kiosk) func(dest ...any) error {
	return func(dest ...any) error {
		if len(dest) != 8 {
			return errors.New("expected 8 dests")
		}
		*(dest[0].(*string)) = k.ID
		*(dest[1].(*string)) = k.Code
		*(dest[2].(*string)) = k.DisplayName
		*(dest[3].(*string)) = k.PinHash
		*(dest[4].(*bool)) = k.Active
		*(dest[5].(*string)) = k.ProjectID
		*(dest[6].(*time.Time)) = k.CreatedAt
		*(dest[7].(*time.Time)) = k.UpdatedAt
		return nil
	}
}

// fakeKioskRows implements pgx.Rows for List queries.
type fakeKioskRows struct {
	kiosks  []*Kiosk
	idx     int
	scanErr error
}

func (r *fakeKioskRows) Close()                                       {}
func (r *fakeKioskRows) Err() error                                   { return nil }
func (r *fakeKioskRows) CommandTag() pgconn.CommandTag                { return pgconn.NewCommandTag("SELECT 0") }
func (r *fakeKioskRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeKioskRows) RawValues() [][]byte                          { return nil }
func (r *fakeKioskRows) Values() ([]any, error)                       { return nil, nil }

func (r *fakeKioskRows) Next() bool {
	r.idx++
	return r.idx-1 < len(r.kiosks)
}

func (r *fakeKioskRows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	if r.idx-1 >= len(r.kiosks) {
		return errors.New("fakeKioskRows: index out of range")
	}
	k := r.kiosks[r.idx-1]
	if len(dest) != 8 {
		return errors.New("expected 8 dests")
	}
	*(dest[0].(*string)) = k.ID
	*(dest[1].(*string)) = k.Code
	*(dest[2].(*string)) = k.DisplayName
	*(dest[3].(*string)) = k.PinHash
	*(dest[4].(*bool)) = k.Active
	*(dest[5].(*string)) = k.ProjectID
	*(dest[6].(*time.Time)) = k.CreatedAt
	*(dest[7].(*time.Time)) = k.UpdatedAt
	return nil
}

func (r *fakeKioskRows) Conn() *pgx.Conn { return nil }

var _ pgx.Rows = (*fakeKioskRows)(nil)

func TestKioskStoreCreateSuccess(t *testing.T) {
	db := &fakeDB{execTag: pgconn.NewCommandTag("INSERT 0 1")}
	store := NewKioskStoreWithDB(db)

	err := store.Create(context.Background(), &Kiosk{Code: "K1", DisplayName: "Test Kiosk", PinHash: "$2a$10$hash"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(db.execCalls) != 1 {
		t.Fatalf("expected 1 exec call, got %d", len(db.execCalls))
	}
	if !strings.Contains(db.execCalls[0].sql, "INSERT INTO identity.kiosks") {
		t.Error("SQL should be an INSERT into identity.kiosks")
	}
}

func TestKioskStoreCreateDuplicateCode(t *testing.T) {
	db := &fakeDB{
		execErr: &pgconn.PgError{Code: "23505"},
	}
	store := NewKioskStoreWithDB(db)

	err := store.Create(context.Background(), &Kiosk{Code: "DUP", DisplayName: "Dup", PinHash: "$2a$10$h"})
	if !errors.Is(err, ErrDuplicateKioskCode) {
		t.Errorf("expected ErrDuplicateKioskCode, got %v", err)
	}
}

func TestKioskStoreGetByCodeFound(t *testing.T) {
	k := Kiosk{ID: "k1", Code: "KIOSK-1", DisplayName: "My Kiosk", PinHash: "$2a$10$h", Active: true}
	db := &fakeDB{queryRow: &fakeKioskRow{scanFn: kioskScanData(k)}}
	store := NewKioskStoreWithDB(db)

	got, err := store.GetByCode(context.Background(), "KIOSK-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected kiosk, got nil")
	}
	if got.Code != "KIOSK-1" {
		t.Errorf("Code = %q", got.Code)
	}
	if got.DisplayName != "My Kiosk" {
		t.Errorf("DisplayName = %q", got.DisplayName)
	}
}

func TestKioskStoreGetByCodeNotFound(t *testing.T) {
	db := &fakeDB{queryRow: &fakeKioskRow{scanErr: pgx.ErrNoRows}}
	store := NewKioskStoreWithDB(db)

	got, err := store.GetByCode(context.Background(), "NO-EXIST")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil kiosk for not found, got %+v", got)
	}
}

func TestKioskStoreGetByIDFound(t *testing.T) {
	k := Kiosk{ID: "id-123", Code: "K2", DisplayName: "By ID", PinHash: "$2a$10$h", Active: false}
	db := &fakeDB{queryRow: &fakeKioskRow{scanFn: kioskScanData(k)}}
	store := NewKioskStoreWithDB(db)

	got, err := store.GetByID(context.Background(), "id-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected kiosk, got nil")
	}
	if got.ID != "id-123" {
		t.Errorf("ID = %q", got.ID)
	}
}

func TestKioskStoreGetByIDNotFound(t *testing.T) {
	db := &fakeDB{queryRow: &fakeKioskRow{scanErr: pgx.ErrNoRows}}
	store := NewKioskStoreWithDB(db)

	got, err := store.GetByID(context.Background(), "no-id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil kiosk, got %+v", got)
	}
}

func TestKioskStoreList(t *testing.T) {
	k1 := &Kiosk{ID: "a", Code: "A", DisplayName: "First"}
	k2 := &Kiosk{ID: "b", Code: "B", DisplayName: "Second"}
	db := &fakeDB{
		queryRows:   &fakeKioskRows{kiosks: []*Kiosk{k1, k2}},
		countResult: 2,
		returnCount: true,
	}
	store := NewKioskStoreWithDB(db)

	got, nextToken, totalCount, err := store.List(context.Background(), "", 1, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 kiosk, got %d", len(got))
	}
	if got[0].Code != "A" {
		t.Errorf("got[0].Code = %q", got[0].Code)
	}
	if nextToken != "a" || totalCount != 2 {
		t.Errorf("pagination = token %q, total %d", nextToken, totalCount)
	}
}

func TestKioskStoreListEmpty(t *testing.T) {
	db := &fakeDB{queryRows: &fakeKioskRows{}, returnCount: true}
	store := NewKioskStoreWithDB(db)

	got, nextToken, totalCount, err := store.List(context.Background(), "", 50, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty list, got %d", len(got))
	}
	if nextToken != "" || totalCount != 0 {
		t.Errorf("pagination = token %q, total %d", nextToken, totalCount)
	}
}

func TestKioskStoreUpdateSuccess(t *testing.T) {
	db := &fakeDB{execTag: pgconn.NewCommandTag("UPDATE 1")}
	store := NewKioskStoreWithDB(db)

	err := store.Update(context.Background(), &Kiosk{ID: "k1", DisplayName: "Updated", Active: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(db.lastExec().sql, "UPDATE identity.kiosks") {
		t.Error("SQL should be an UPDATE")
	}
}

func TestKioskStoreUpdateNotFound(t *testing.T) {
	db := &fakeDB{execTag: pgconn.NewCommandTag("UPDATE 0")}
	store := NewKioskStoreWithDB(db)

	err := store.Update(context.Background(), &Kiosk{ID: "no-such", DisplayName: "Nope", Active: true})
	if !errors.Is(err, ErrKioskNotFound) {
		t.Errorf("expected ErrKioskNotFound, got %v", err)
	}
}

func TestKioskStoreUpdatePINSuccess(t *testing.T) {
	db := &fakeDB{execTag: pgconn.NewCommandTag("UPDATE 1")}
	store := NewKioskStoreWithDB(db)

	err := store.UpdatePIN(context.Background(), "k1", "$2a$10$newhash")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(db.lastExec().sql, "SET pin_hash") {
		t.Error("SQL should update pin_hash")
	}
}

func TestKioskStoreUpdatePINNotFound(t *testing.T) {
	db := &fakeDB{execTag: pgconn.NewCommandTag("UPDATE 0")}
	store := NewKioskStoreWithDB(db)

	err := store.UpdatePIN(context.Background(), "no-such", "$2a$10$h")
	if !errors.Is(err, ErrKioskNotFound) {
		t.Errorf("expected ErrKioskNotFound, got %v", err)
	}
}
