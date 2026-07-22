//go:build integration

package identity

import (
	"context"
	"fmt"
	"os"
	"strings"
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
	store := NewStoreWithDB(tx)
	cleanup := func() {
		tx.Rollback(ctx) //nolint:errcheck
	}
	return store, tx, cleanup
}

// txStoreWithHasher creates a Store with a CardTokenHasher backed by a
// transaction that is always rolled back.
func txStoreWithHasher(t *testing.T, hasher *CardTokenHasher) (*Store, pgx.Tx, func()) {
	t.Helper()
	pool := integrationPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	store := NewStoreWithDBAndHasher(tx, hasher)
	cleanup := func() {
		tx.Rollback(ctx) //nolint:errcheck
	}
	return store, tx, cleanup
}

func integrationHasher(t *testing.T) *CardTokenHasher {
	t.Helper()
	key := os.Getenv("CARD_TOKEN_HMAC_KEY")
	if key == "" {
		key = "medisync-dev-card-hmac-change-in-prod"
	}
	hasher, err := NewCardTokenHasher(key)
	if err != nil {
		t.Fatalf("NewCardTokenHasher: %v", err)
	}
	return hasher
}

func uniqueUsername(t *testing.T, prefix string) string {
	t.Helper()
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// --- CRUD integration tests ---

func TestStoreGetByUsername_Integration(t *testing.T) {
	store, tx, cleanup := txStore(t)
	defer cleanup()

	// Seed a user directly via the transaction so we can read it back.
	username := uniqueUsername(t, "getbyuser")
	_, err := tx.Exec(context.Background(),
		`INSERT INTO medisync.users (username, password_hash, display_name, role, ward_ids, active)
		 VALUES ($1, '$2a$10$hashhashhashhashhashhashhashh', 'Test User', 'NURSE', '{WARD-3A,WARD-5B}', true)`,
		username)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}

	u, err := store.GetByUsername(context.Background(), username)
	if err != nil {
		t.Fatalf("GetByUsername: %v", err)
	}
	if u == nil {
		t.Fatal("expected user, got nil")
	}
	if u.Username != username {
		t.Errorf("Username = %q, want %q", u.Username, username)
	}
	if u.DisplayName != "Test User" {
		t.Errorf("DisplayName = %q, want 'Test User'", u.DisplayName)
	}
	if u.Role != RoleNurse {
		t.Errorf("Role = %q, want NURSE", u.Role)
	}
	if len(u.WardIDs) != 2 || u.WardIDs[0] != "WARD-3A" || u.WardIDs[1] != "WARD-5B" {
		t.Errorf("WardIDs = %v, want [WARD-3A WARD-5B]", u.WardIDs)
	}
	if !u.Active {
		t.Error("Active should be true")
	}
	if u.ID == "" {
		t.Error("ID should not be empty")
	}
}

func TestStoreGetByUsernameNotFound_Integration(t *testing.T) {
	store, _, cleanup := txStore(t)
	defer cleanup()

	u, err := store.GetByUsername(context.Background(), "nonexistent-user")
	if err != nil {
		t.Fatalf("GetByUsername: %v", err)
	}
	if u != nil {
		t.Errorf("expected nil for unknown user, got %+v", u)
	}
}

func TestStoreGetByID_Integration(t *testing.T) {
	store, tx, cleanup := txStore(t)
	defer cleanup()

	username := uniqueUsername(t, "getbyid")
	var userID string
	err := tx.QueryRow(context.Background(),
		`INSERT INTO medisync.users (username, password_hash, display_name, role, ward_ids, active)
		 VALUES ($1, '$2a$10$hash', 'ID User', 'PHARMACIST', '{WARD-ICU}', true)
		 RETURNING id`, username).Scan(&userID)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}

	u, err := store.GetByID(context.Background(), userID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if u == nil {
		t.Fatal("expected user, got nil")
	}
	if u.ID != userID {
		t.Errorf("ID = %q, want %q", u.ID, userID)
	}
	if u.Role != RolePharmacist {
		t.Errorf("Role = %q, want PHARMACIST", u.Role)
	}
	if len(u.WardIDs) != 1 || u.WardIDs[0] != "WARD-ICU" {
		t.Errorf("WardIDs = %v, want [WARD-ICU]", u.WardIDs)
	}
}

func TestStoreGetByIDNotFound_Integration(t *testing.T) {
	store, _, cleanup := txStore(t)
	defer cleanup()

	u, err := store.GetByID(context.Background(), "00000000-0000-0000-0000-000000000000")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if u != nil {
		t.Errorf("expected nil for unknown ID, got %+v", u)
	}
}

func TestStoreGetByCardToken_Integration(t *testing.T) {
	hasher := integrationHasher(t)
	store, tx, cleanup := txStoreWithHasher(t, hasher)
	defer cleanup()

	username := uniqueUsername(t, "getbycard")
	rawToken := "token-integration-test"
	hashedBytes, err := hasher.Hash(rawToken)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	_, err = tx.Exec(context.Background(),
		`INSERT INTO medisync.users (username, password_hash, display_name, role, ward_ids, card_token_hash, active)
		 VALUES ($1, '$2a$10$hash', 'Card User', 'NURSE', '{WARD-3A}', $2, true)`,
		username, hashedBytes)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}

	u, err := store.GetByCardToken(context.Background(), rawToken)
	if err != nil {
		t.Fatalf("GetByCardToken: %v", err)
	}
	if u == nil {
		t.Fatal("expected user, got nil")
	}
	if u.Username != username {
		t.Errorf("Username = %q, want %q", u.Username, username)
	}
}

func TestStoreGetByCardTokenNotFound_Integration(t *testing.T) {
	hasher := integrationHasher(t)
	store, _, cleanup := txStoreWithHasher(t, hasher)
	defer cleanup()

	u, err := store.GetByCardToken(context.Background(), "no-such-token")
	if err != nil {
		t.Fatalf("GetByCardToken: %v", err)
	}
	if u != nil {
		t.Errorf("expected nil for unknown token, got %+v", u)
	}
}

func TestStoreGetByCardTokenNilHasher_Integration(t *testing.T) {
	store, _, cleanup := txStore(t)
	defer cleanup()

	_, err := store.GetByCardToken(context.Background(), "any-token")
	if err == nil {
		t.Fatal("expected error for nil hasher, got nil")
	}
}

// --- SetCardToken integration tests ---

func TestStoreSetCardToken_Integration(t *testing.T) {
	hasher := integrationHasher(t)
	store, tx, cleanup := txStoreWithHasher(t, hasher)
	defer cleanup()

	username := uniqueUsername(t, "setcard")
	var userID string
	err := tx.QueryRow(context.Background(),
		`INSERT INTO medisync.users (username, password_hash, display_name, role, ward_ids, active)
		 VALUES ($1, '$2a$10$hash', 'Card Enrollee', 'NURSE', '{WARD-3A}', true)
		 RETURNING id`, username).Scan(&userID)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}

	rawToken := "enrolled-card-token"
	err = store.SetCardToken(context.Background(), userID, rawToken)
	if err != nil {
		t.Fatalf("SetCardToken: %v", err)
	}

	// Verify the hash is stored and can be looked up.
	u, err := store.GetByCardToken(context.Background(), rawToken)
	if err != nil {
		t.Fatalf("GetByCardToken after SetCardToken: %v", err)
	}
	if u == nil {
		t.Fatal("expected user after SetCardToken, got nil")
	}
	if u.Username != username {
		t.Errorf("Username = %q, want %q", u.Username, username)
	}
}

func TestStoreSetCardTokenUserNotFound_Integration(t *testing.T) {
	hasher := integrationHasher(t)
	store, _, cleanup := txStoreWithHasher(t, hasher)
	defer cleanup()

	err := store.SetCardToken(context.Background(), "00000000-0000-0000-0000-000000000000", "token")
	if err == nil {
		t.Fatal("expected error for unknown user, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention not found: %v", err)
	}
}

// --- Column removal verification ---

func TestCardTokenColumnGone_Integration(t *testing.T) {
	pool := integrationPool(t)
	defer pool.Close()

	var exists bool
	err := pool.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM information_schema.columns
		 WHERE table_schema = 'identity' AND table_name = 'users'
		 AND column_name = 'card_token')`).Scan(&exists)
	if err != nil {
		t.Fatalf("check card_token column: %v", err)
	}
	if exists {
		t.Fatal("card_token column should not exist (removed by 0004 migration)")
	}
}

func TestCardTokenHashColumnExists_Integration(t *testing.T) {
	pool := integrationPool(t)
	defer pool.Close()

	var exists bool
	err := pool.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM information_schema.columns
		 WHERE table_schema = 'identity' AND table_name = 'users'
		 AND column_name = 'card_token_hash')`).Scan(&exists)
	if err != nil {
		t.Fatalf("check card_token_hash column: %v", err)
	}
	if !exists {
		t.Fatal("card_token_hash column should exist (added by 0003 migration)")
	}
}

// --- SeedAdmin integration tests ---

func TestSeedAdminEmptyTable_Integration(t *testing.T) {
	store, tx, cleanup := txStore(t)
	defer cleanup()
	if _, err := tx.Exec(context.Background(), `DELETE FROM medisync.users`); err != nil {
		t.Fatalf("clear users in test transaction: %v", err)
	}

	created, err := store.SeedAdmin(context.Background(), "seed-hash")
	if err != nil {
		t.Fatalf("SeedAdmin: %v", err)
	}
	if !created {
		t.Error("expected created=true for empty table")
	}

	// Verify the admin was inserted.
	var username, role string
	err = tx.QueryRow(context.Background(),
		`SELECT username, role FROM medisync.users WHERE username = 'admin'`).Scan(&username, &role)
	if err != nil {
		t.Fatalf("verify admin: %v", err)
	}
	if username != "admin" {
		t.Errorf("username = %q, want admin", username)
	}
	if role != "ADMIN" {
		t.Errorf("role = %q, want ADMIN", role)
	}
}

func TestSeedAdminNonEmptyTable_Integration(t *testing.T) {
	store, tx, cleanup := txStore(t)
	defer cleanup()
	if _, err := tx.Exec(context.Background(), `DELETE FROM medisync.users`); err != nil {
		t.Fatalf("clear users in test transaction: %v", err)
	}

	// Pre-populate with a user.
	_, err := tx.Exec(context.Background(),
		`INSERT INTO medisync.users (username, password_hash, display_name, role, ward_ids, active)
		 VALUES ('existing-user', 'hash', 'Existing', 'NURSE', '{WARD-3A}', true)`)
	if err != nil {
		t.Fatalf("seed existing user: %v", err)
	}

	created, err := store.SeedAdmin(context.Background(), "seed-hash")
	if err != nil {
		t.Fatalf("SeedAdmin: %v", err)
	}
	if created {
		t.Error("expected created=false for non-empty table")
	}

	// Verify admin was NOT created.
	var count int
	err = tx.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM medisync.users WHERE username = 'admin'`).Scan(&count)
	if err != nil {
		t.Fatalf("count admin: %v", err)
	}
	if count != 0 {
		t.Errorf("admin should not have been created, got count=%d", count)
	}
}

// --- Schema verification ---

func TestIdentitySchemaExists_Integration(t *testing.T) {
	pool := integrationPool(t)
	defer pool.Close()

	var exists bool
	err := pool.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname = 'identity')`).Scan(&exists)
	if err != nil {
		t.Fatalf("check identity schema: %v", err)
	}
	if !exists {
		t.Fatal("identity schema should exist (created by 0001_init.sql)")
	}
}

func TestIdentityUsersTableExists_Integration(t *testing.T) {
	pool := integrationPool(t)
	defer pool.Close()

	var exists bool
	err := pool.QueryRow(context.Background(),
		`SELECT EXISTS(SELECT 1 FROM information_schema.tables
		 WHERE table_schema = 'identity' AND table_name = 'users')`).Scan(&exists)
	if err != nil {
		t.Fatalf("check medisync.users table: %v", err)
	}
	if !exists {
		t.Fatal("medisync.users table should exist (created by 0002_identity.sql)")
	}
}

func TestIdentityUsersConstraints_Integration(t *testing.T) {
	pool := integrationPool(t)
	defer pool.Close()

	// Check that role constraint rejects invalid values.
	_, err := pool.Exec(context.Background(),
		`INSERT INTO medisync.users (username, password_hash, role)
		 VALUES ('constraint-test', 'hash', 'INVALID_ROLE')`)
	if err == nil {
		// Clean up in case the constraint doesn't exist.
		pool.Exec(context.Background(), `DELETE FROM medisync.users WHERE username = 'constraint-test'`)
		t.Fatal("expected constraint violation for invalid role, got nil")
	}
}

func TestIdentityUsersUniqueUsername_Integration(t *testing.T) {
	pool := integrationPool(t)
	defer pool.Close()

	username := uniqueUsername(t, "uniq")
	_, err := pool.Exec(context.Background(),
		`INSERT INTO medisync.users (username, password_hash, role)
		 VALUES ($1, 'hash', 'NURSE')`, username)
	if err != nil {
		t.Fatalf("first insert: %v", err)
	}
	defer pool.Exec(context.Background(), `DELETE FROM medisync.users WHERE username = $1`, username)

	// Second insert with same username should fail.
	_, err = pool.Exec(context.Background(),
		`INSERT INTO medisync.users (username, password_hash, role)
		 VALUES ($1, 'hash', 'NURSE')`, username)
	if err == nil {
		t.Fatal("expected unique constraint violation for duplicate username, got nil")
	}
}
