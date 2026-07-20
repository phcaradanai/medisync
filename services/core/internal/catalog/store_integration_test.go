//go:build integration

package catalog

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

func uniqueCode(t *testing.T, prefix string) string {
	t.Helper()
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// ── CRUD integration tests ──────────────────────────────────────────

func TestStoreCreateAndGetByID_Integration(t *testing.T) {
	store, tx, cleanup := txStore(t)
	defer cleanup()

	code := uniqueCode(t, "PARA")
	d, err := store.Create(context.Background(), Drug{
		Code:        code,
		Name:        "Paracetamol 500mg",
		GenericName: "Paracetamol",
		Form:        "tablet",
		Strength:    "500mg",
		Unit:        "tab",
		StickerNote: "Take with food",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if d == nil {
		t.Fatal("expected drug, got nil")
	}
	if d.ID == "" {
		t.Error("ID should be generated")
	}
	if d.Code != code {
		t.Errorf("Code = %q, want %q", d.Code, code)
	}
	if d.Name != "Paracetamol 500mg" {
		t.Errorf("Name = %q", d.Name)
	}
	if !d.Active {
		t.Error("Active should default to true")
	}
	if d.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
	// Verify full readback.
	got, err := store.GetByID(context.Background(), d.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got == nil {
		t.Fatal("GetByID returned nil")
	}
	if got.ID != d.ID {
		t.Errorf("GetByID ID = %q, want %q", got.ID, d.ID)
	}
	if got.StickerNote != "Take with food" {
		t.Errorf("StickerNote = %q, want 'Take with food'", got.StickerNote)
	}
	// Verify that the row is really in the DB (not just in the tx).
	_ = tx
}

func TestStoreGetByIDNotFound_Integration(t *testing.T) {
	store, _, cleanup := txStore(t)
	defer cleanup()

	d, err := store.GetByID(context.Background(), "00000000-0000-0000-0000-000000000000")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if d != nil {
		t.Errorf("expected nil for unknown id, got %+v", d)
	}
}

func TestStoreGetByCode_Integration(t *testing.T) {
	store, tx, cleanup := txStore(t)
	defer cleanup()

	code := uniqueCode(t, "AMOX")
	_, err := store.Create(context.Background(), Drug{
		Code: code,
		Name: "Amoxicillin 250mg",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	d, err := store.GetByCode(context.Background(), code)
	if err != nil {
		t.Fatalf("GetByCode: %v", err)
	}
	if d == nil {
		t.Fatal("expected drug, got nil")
	}
	if d.Code != code {
		t.Errorf("Code = %q, want %q", d.Code, code)
	}
	_ = tx
}

func TestStoreGetByCodeNotFound_Integration(t *testing.T) {
	store, _, cleanup := txStore(t)
	defer cleanup()

	d, err := store.GetByCode(context.Background(), "NONEXISTENT-999")
	if err != nil {
		t.Fatalf("GetByCode: %v", err)
	}
	if d != nil {
		t.Errorf("expected nil for unknown code, got %+v", d)
	}
}

func TestStoreCodeUniqueConstraint_Integration(t *testing.T) {
	store, _, cleanup := txStore(t)
	defer cleanup()

	code := uniqueCode(t, "UNIQ")
	_, err := store.Create(context.Background(), Drug{Code: code, Name: "First"})
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}

	_, err = store.Create(context.Background(), Drug{Code: code, Name: "Second"})
	if err == nil {
		t.Fatal("expected unique constraint violation, got nil")
	}
}

func TestStoreUpdate_Integration(t *testing.T) {
	store, _, cleanup := txStore(t)
	defer cleanup()

	code := uniqueCode(t, "UPD")
	d, err := store.Create(context.Background(), Drug{
		Code:        code,
		Name:        "Old Name",
		GenericName: "Old Generic",
		Form:        "capsule",
		Strength:    "100mg",
		Unit:        "cap",
		StickerNote: "Old note",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := store.Update(context.Background(), Drug{
		ID:          d.ID,
		Code:        code,
		Name:        "New Name",
		GenericName: "New Generic",
		Form:        "tablet",
		Strength:    "200mg",
		Unit:        "tab",
		StickerNote: "New note",
		Active:      true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated == nil {
		t.Fatal("expected updated drug, got nil")
	}
	if updated.Name != "New Name" {
		t.Errorf("Name = %q, want 'New Name'", updated.Name)
	}
	if updated.GenericName != "New Generic" {
		t.Errorf("GenericName = %q, want 'New Generic'", updated.GenericName)
	}
	if updated.Form != "tablet" {
		t.Errorf("Form = %q, want 'tablet'", updated.Form)
	}
	if updated.UpdatedAt.Before(d.UpdatedAt) {
		t.Error("UpdatedAt should be after creation time")
	}
}

func TestStoreUpdateNotFound_Integration(t *testing.T) {
	store, _, cleanup := txStore(t)
	defer cleanup()

	d, err := store.Update(context.Background(), Drug{
		ID: "00000000-0000-0000-0000-000000000000",
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if d != nil {
		t.Errorf("expected nil for unknown id, got %+v", d)
	}
}

func TestStoreDeactivate_Integration(t *testing.T) {
	store, _, cleanup := txStore(t)
	defer cleanup()

	code := uniqueCode(t, "DEACT")
	d, err := store.Create(context.Background(), Drug{Code: code, Name: "To Deactivate"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !d.Active {
		t.Fatal("new drug should be active")
	}

	deactivated, err := store.Deactivate(context.Background(), d.ID)
	if err != nil {
		t.Fatalf("Deactivate: %v", err)
	}
	if deactivated == nil {
		t.Fatal("expected deactivated drug, got nil")
	}
	if deactivated.Active {
		t.Error("deactivated drug should have Active = false")
	}

	// Second deactivation should return nil (already inactive).
	d2, err := store.Deactivate(context.Background(), d.ID)
	if err != nil {
		t.Fatalf("Second Deactivate: %v", err)
	}
	if d2 != nil {
		t.Errorf("second deactivation should return nil, got %+v", d2)
	}
}

func TestStoreDeactivateNotFound_Integration(t *testing.T) {
	store, _, cleanup := txStore(t)
	defer cleanup()

	d, err := store.Deactivate(context.Background(), "00000000-0000-0000-0000-000000000000")
	if err != nil {
		t.Fatalf("Deactivate: %v", err)
	}
	if d != nil {
		t.Errorf("expected nil for unknown id, got %+v", d)
	}
}

func TestStoreListEmpty_Integration(t *testing.T) {
	store, tx, cleanup := txStore(t)
	defer cleanup()

	// Ensure no drugs in this transaction (the table may have data from other tests).
	_, err := tx.Exec(context.Background(), `DELETE FROM medisync.drug`)
	if err != nil {
		t.Fatalf("clear drugs: %v", err)
	}

	drugs, nextToken, totalCount, err := store.List(context.Background(), "", false, 50, "", "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(drugs) != 0 {
		t.Errorf("expected 0 drugs, got %d", len(drugs))
	}
	if nextToken != "" {
		t.Errorf("nextToken should be empty, got %q", nextToken)
	}
	if totalCount != 0 {
		t.Errorf("totalCount = %d, want 0", totalCount)
	}
}

func TestStoreListWithResults_Integration(t *testing.T) {
	store, tx, cleanup := txStore(t)
	defer cleanup()

	_, err := tx.Exec(context.Background(), `DELETE FROM medisync.drug`)
	if err != nil {
		t.Fatalf("clear drugs: %v", err)
	}

	// Create 3 drugs.
	for i := 0; i < 3; i++ {
		code := uniqueCode(t, fmt.Sprintf("LIST%d", i))
		_, err := store.Create(context.Background(), Drug{Code: code, Name: fmt.Sprintf("Drug %d", i)})
		if err != nil {
			t.Fatalf("Create drug %d: %v", i, err)
		}
	}

	drugs, nextToken, totalCount, err := store.List(context.Background(), "", false, 50, "", "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(drugs) != 3 {
		t.Errorf("expected 3 drugs, got %d", len(drugs))
	}
	if nextToken != "" {
		t.Errorf("nextToken should be empty with pageSize > count, got %q", nextToken)
	}
	if totalCount != 3 {
		t.Errorf("totalCount = %d, want 3", totalCount)
	}
}

func TestStoreListPagination_Integration(t *testing.T) {
	store, tx, cleanup := txStore(t)
	defer cleanup()

	_, err := tx.Exec(context.Background(), `DELETE FROM medisync.drug`)
	if err != nil {
		t.Fatalf("clear drugs: %v", err)
	}

	for i := 0; i < 5; i++ {
		code := uniqueCode(t, fmt.Sprintf("PAGE%d", i))
		_, err := store.Create(context.Background(), Drug{Code: code, Name: fmt.Sprintf("Drug %d", i)})
		if err != nil {
			t.Fatalf("Create drug %d: %v", i, err)
		}
	}

	// First page: 2 items.
	page1, token, totalCount, err := store.List(context.Background(), "", false, 2, "", "")
	if err != nil {
		t.Fatalf("List page 1: %v", err)
	}
	if len(page1) != 2 {
		t.Errorf("page 1: expected 2 drugs, got %d", len(page1))
	}
	if token == "" {
		t.Fatal("nextPageToken should not be empty")
	}
	if totalCount != 5 {
		t.Errorf("totalCount = %d, want 5", totalCount)
	}

	// Second page: use token.
	page2, token2, _, err := store.List(context.Background(), "", false, 2, "", token)
	if err != nil {
		t.Fatalf("List page 2: %v", err)
	}
	if len(page2) != 2 {
		t.Errorf("page 2: expected 2 drugs, got %d", len(page2))
	}
	if token2 == "" {
		t.Fatal("nextPageToken should not be empty for page 2")
	}

	// Third page: remaining 1 item.
	page3, token3, _, err := store.List(context.Background(), "", false, 2, "", token2)
	if err != nil {
		t.Fatalf("List page 3: %v", err)
	}
	if len(page3) != 1 {
		t.Errorf("page 3: expected 1 drug, got %d", len(page3))
	}
	if token3 != "" {
		t.Errorf("page 3 token should be empty, got %q", token3)
	}
}

func TestStoreListSearchQuery_Integration(t *testing.T) {
	store, tx, cleanup := txStore(t)
	defer cleanup()

	_, err := tx.Exec(context.Background(), `DELETE FROM medisync.drug`)
	if err != nil {
		t.Fatalf("clear drugs: %v", err)
	}

	code := uniqueCode(t, "PARA")
	_, err = store.Create(context.Background(), Drug{
		Code:        code,
		Name:        "Paracetamol",
		GenericName: "Paracetamol",
	})
	if err != nil {
		t.Fatalf("Create paracetamol: %v", err)
	}
	_, err = store.Create(context.Background(), Drug{
		Code: uniqueCode(t, "AMOX"),
		Name: "Amoxicillin",
	})
	if err != nil {
		t.Fatalf("Create amoxicillin: %v", err)
	}

	// Search for "para" should find exactly one drug.
	drugs, _, _, err := store.List(context.Background(), "para", false, 50, "", "")
	if err != nil {
		t.Fatalf("List with query: %v", err)
	}
	if len(drugs) != 1 {
		t.Errorf("expected 1 drug matching 'para', got %d", len(drugs))
	}
	if drugs[0].Name != "Paracetamol" {
		t.Errorf("expected Paracetamol, got %s", drugs[0].Name)
	}
}

func TestStoreListIncludeInactive_Integration(t *testing.T) {
	store, tx, cleanup := txStore(t)
	defer cleanup()

	_, err := tx.Exec(context.Background(), `DELETE FROM medisync.drug`)
	if err != nil {
		t.Fatalf("clear drugs: %v", err)
	}

	code := uniqueCode(t, "INACTIVE")
	d, err := store.Create(context.Background(), Drug{Code: code, Name: "Inactive Drug"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Default list should include the drug.
	drugs, _, _, err := store.List(context.Background(), "", false, 50, "", "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(drugs) != 1 {
		t.Errorf("expected 1 active drug, got %d", len(drugs))
	}

	// Deactivate the drug.
	_, err = store.Deactivate(context.Background(), d.ID)
	if err != nil {
		t.Fatalf("Deactivate: %v", err)
	}

	// Default list should now be empty.
	drugs, _, _, err = store.List(context.Background(), "", false, 50, "", "")
	if err != nil {
		t.Fatalf("List after deactivate: %v", err)
	}
	if len(drugs) != 0 {
		t.Errorf("expected 0 active drugs after deactivation, got %d", len(drugs))
	}

	// With includeInactive=true, should get the deactivated drug.
	drugs, _, _, err = store.List(context.Background(), "", true, 50, "", "")
	if err != nil {
		t.Fatalf("List with includeInactive: %v", err)
	}
	if len(drugs) != 1 {
		t.Errorf("expected 1 drug with includeInactive, got %d", len(drugs))
	}
	if drugs[0].Active {
		t.Error("deactivated drug should have Active = false")
	}
}

// ── Schema verification ─────────────────────────────────────────────

func TestCatalogSchemaExists_Integration(t *testing.T) {
	pool := integrationPool(t)
	defer pool.Close()

	var exists bool
	err := pool.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname = 'catalog')`).Scan(&exists)
	if err != nil {
		t.Fatalf("check catalog schema: %v", err)
	}
	if !exists {
		t.Fatal("catalog schema should exist (created by 0001_init.sql)")
	}
}

func TestCatalogDrugTableExists_Integration(t *testing.T) {
	pool := integrationPool(t)
	defer pool.Close()

	var exists bool
	err := pool.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM information_schema.tables
		 WHERE table_schema = 'catalog' AND table_name = 'drug')`).Scan(&exists)
	if err != nil {
		t.Fatalf("check medisync.drug table: %v", err)
	}
	if !exists {
		t.Fatal("medisync.drug table should exist (created by 0005 migration)")
	}
}

func TestCatalogDrugColumnsExist_Integration(t *testing.T) {
	pool := integrationPool(t)
	defer pool.Close()

	expectedColumns := []string{
		"id", "code", "name", "generic_name", "form",
		"strength", "unit", "sticker_note", "active",
		"created_at", "updated_at",
	}

	for _, col := range expectedColumns {
		var exists bool
		err := pool.QueryRow(context.Background(),
			`SELECT EXISTS(SELECT 1 FROM information_schema.columns
			 WHERE table_schema = 'catalog' AND table_name = 'drug'
			 AND column_name = $1)`, col).Scan(&exists)
		if err != nil {
			t.Fatalf("check column %s: %v", col, err)
		}
		if !exists {
			t.Errorf("column medisync.drug.%s should exist", col)
		}
	}
}

func TestCatalogDrugUniqueCode_Integration(t *testing.T) {
	pool := integrationPool(t)
	defer pool.Close()

	code := fmt.Sprintf("UNIQUE-CONSTRAINT-%d", time.Now().UnixNano())
	_, err := pool.Exec(context.Background(),
		`INSERT INTO medisync.drug (code, name) VALUES ($1, 'First')`, code)
	if err != nil {
		t.Fatalf("first insert: %v", err)
	}
	defer pool.Exec(context.Background(), `DELETE FROM medisync.drug WHERE code = $1`, code)

	_, err = pool.Exec(context.Background(),
		`INSERT INTO medisync.drug (code, name) VALUES ($1, 'Second')`, code)
	if err == nil {
		t.Fatal("expected unique constraint violation for duplicate code, got nil")
	}
}
