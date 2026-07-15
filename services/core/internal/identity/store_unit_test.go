package identity

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

// --- Test fakes ---

// fakeDB implements dbConn for unit tests. It records Exec calls and
// returns a configured row from QueryRow.
type fakeDB struct {
	execCalls     []execCall
	execTag       pgconn.CommandTag
	execErr       error
	queryRowCalls []queryRowCall
	queryRow      pgx.Row
	queryCalls    []queryCall
	queryRows     pgx.Rows
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

func (f *fakeDB) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	f.queryRowCalls = append(f.queryRowCalls, queryRowCall{sql: sql, args: args})
	return f.queryRow
}

func (f *fakeDB) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	f.queryCalls = append(f.queryCalls, queryCall{sql: sql, args: args})
	return f.queryRows, f.queryErr
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

// fakeRow implements pgx.Row. It holds destination pointer pairs and an error.
type fakeRow struct {
	scanErr error
	// values are returned via the pointers passed to Scan.
	scanFn func(dest ...any) error
}

func (r *fakeRow) Scan(dest ...any) error {
	if r.scanFn != nil {
		return r.scanFn(dest...)
	}
	return r.scanErr
}

// rowWithUser returns a fakeRow that fills dest with a sample user.
// Matches the 9-column scan used by GetByUsername / GetByID.
func rowWithUser(u User) *fakeRow {
	return &fakeRow{
		scanFn: func(dest ...any) error {
			if len(dest) != 9 {
				return fmt.Errorf("expected 9 dests, got %d", len(dest))
			}
			*(dest[0].(*string)) = u.ID
			*(dest[1].(*string)) = u.Username
			*(dest[2].(*string)) = u.PasswordHash
			*(dest[3].(*string)) = u.DisplayName
			// dest[4] is *roleScanner (the custom Scan type).
			if rs, ok := dest[4].(*roleScanner); ok {
				*rs = roleScanner(u.Role)
			}
			wardIDs := dest[5].(*[]string)
			*wardIDs = append(*wardIDs, u.WardIDs...)
			*(dest[6].(*bool)) = u.Active
			if dt, ok := dest[7].(*time.Time); ok {
				*dt = u.CreatedAt
			}
			if dt, ok := dest[8].(*time.Time); ok {
				*dt = u.UpdatedAt
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

// bytesEqual compares two byte slices for equality.
func bytesEqual(a, b any) bool {
	aa, ok := a.([]byte)
	if !ok {
		return false
	}
	bb, ok := b.([]byte)
	if !ok {
		return false
	}
	if len(aa) != len(bb) {
		return false
	}
	for i := range aa {
		if aa[i] != bb[i] {
			return false
		}
	}
	return true
}

// --- Domain tests ---

func TestUserCanAdminAnyWard(t *testing.T) {
	admin := &User{Role: RoleAdmin, WardIDs: nil}
	if !admin.Can("WARD-3A") {
		t.Error("admin should be able to access any ward")
	}
	if !admin.Can("WARD-ICU") {
		t.Error("admin should be able to access any ward")
	}
	if !admin.Can("") {
		t.Error("admin should be able to access empty ward")
	}
}

func TestUserCanMatchingWard(t *testing.T) {
	u := &User{Role: RoleNurse, WardIDs: []string{"WARD-3A", "WARD-5B"}}
	if !u.Can("WARD-3A") {
		t.Error("nurse should access their own ward WARD-3A")
	}
	if !u.Can("WARD-5B") {
		t.Error("nurse should access their own ward WARD-5B")
	}
}

func TestUserCannotUnrelatedWard(t *testing.T) {
	u := &User{Role: RoleNurse, WardIDs: []string{"WARD-3A"}}
	if u.Can("WARD-ICU") {
		t.Error("nurse should not access unrelated ward")
	}
}

func TestUserCannotEmptyWardIDs(t *testing.T) {
	u := &User{Role: RoleNurse, WardIDs: []string{}}
	if u.Can("WARD-3A") {
		t.Error("nurse with no wards should not access any ward")
	}
}

func TestUserCanNilWardIDs(t *testing.T) {
	u := &User{Role: RolePharmacist, WardIDs: nil}
	if u.Can("WARD-3A") {
		t.Error("pharmacist with nil wards should not access any ward")
	}
}

// --- roleScanner tests ---

func TestRoleScannerValidRoles(t *testing.T) {
	tests := []struct {
		input    string
		expected Role
	}{
		{"ADMIN", RoleAdmin},
		{"PHARMACIST", RolePharmacist},
		{"NURSE", RoleNurse},
		{"REFILLER", RoleRefiller},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			var rs roleScanner
			err := rs.Scan(tt.input)
			if err != nil {
				t.Fatalf("unexpected error scanning %q: %v", tt.input, err)
			}
			if Role(rs) != tt.expected {
				t.Errorf("role = %q, want %q", Role(rs), tt.expected)
			}
		})
	}
}

func TestRoleScannerInvalidRole(t *testing.T) {
	var rs roleScanner
	err := rs.Scan("SUPERUSER")
	if err == nil {
		t.Fatal("expected error for invalid role, got nil")
	}
	if !strings.Contains(err.Error(), "unknown role") {
		t.Errorf("error should mention unknown role: %v", err)
	}
	if !strings.Contains(err.Error(), "SUPERUSER") {
		t.Errorf("error should include the bad value: %v", err)
	}
}

func TestRoleScannerNonString(t *testing.T) {
	var rs roleScanner
	err := rs.Scan(42)
	if err == nil {
		t.Fatal("expected error for non-string, got nil")
	}
	if !strings.Contains(err.Error(), "expected string") {
		t.Errorf("error should mention expected string: %v", err)
	}
}

func TestRoleScannerEmptyString(t *testing.T) {
	var rs roleScanner
	err := rs.Scan("")
	if err == nil {
		t.Fatal("expected error for empty string, got nil")
	}
	if !strings.Contains(err.Error(), "unknown role") {
		t.Errorf("error should mention unknown role: %v", err)
	}
}

// --- scanUser tests (9-column, normal reads) ---

func TestScanUserSuccess(t *testing.T) {
	expected := User{
		ID:           "550e8400-e29b-41d4-a716-446655440000",
		Username:     "nurse1",
		PasswordHash: "$2a$10$hash",
		DisplayName:  "Nurse One",
		Role:         RoleNurse,
		WardIDs:      []string{"WARD-3A", "WARD-5B"},
		Active:       true,
	}
	row := rowWithUser(expected)

	u, err := scanUser(row)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u == nil {
		t.Fatal("expected user, got nil")
	}
	if u.ID != expected.ID {
		t.Errorf("ID = %q, want %q", u.ID, expected.ID)
	}
	if u.Username != expected.Username {
		t.Errorf("Username = %q, want %q", u.Username, expected.Username)
	}
	if u.DisplayName != expected.DisplayName {
		t.Errorf("DisplayName = %q, want %q", u.DisplayName, expected.DisplayName)
	}
	if u.Role != expected.Role {
		t.Errorf("Role = %q, want %q", u.Role, expected.Role)
	}
	if len(u.WardIDs) != 2 || u.WardIDs[0] != "WARD-3A" || u.WardIDs[1] != "WARD-5B" {
		t.Errorf("WardIDs = %v, want [WARD-3A WARD-5B]", u.WardIDs)
	}
	if u.Active != expected.Active {
		t.Errorf("Active = %v, want %v", u.Active, expected.Active)
	}
}

func TestScanUserNoRows(t *testing.T) {
	u, err := scanUser(rowWithNoRows())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u != nil {
		t.Errorf("expected nil user for no rows, got %+v", u)
	}
}

func TestScanUserError(t *testing.T) {
	scanErr := errors.New("connection lost")
	u, err := scanUser(rowWithError(scanErr))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "scan user") {
		t.Errorf("error should wrap with 'scan user': %v", err)
	}
	if !strings.Contains(err.Error(), "connection lost") {
		t.Errorf("error should contain original: %v", err)
	}
	if u != nil {
		t.Errorf("expected nil user on error, got %+v", u)
	}
}

// --- Store query argument tests ---

func TestGetByUsernameQueryArgs(t *testing.T) {
	db := &fakeDB{queryRow: rowWithNoRows()}
	store := NewStoreWithDB(db)

	_, err := store.GetByUsername(context.Background(), "nurse1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	call := db.lastQueryRow()
	if !strings.Contains(call.sql, "FROM identity.users") {
		t.Error("SQL should reference identity.users")
	}
	if !strings.Contains(call.sql, "username = $1") {
		t.Error("SQL should filter by username")
	}
	if len(call.args) != 1 || call.args[0] != "nurse1" {
		t.Errorf("args = %v, want [nurse1]", call.args)
	}
	// Normal reads must not SELECT card_token_hash.
	if strings.Contains(call.sql, "card_token") {
		t.Error("GetByUsername must not SELECT card_token or card_token_hash")
	}
}

func TestGetByIDQueryArgs(t *testing.T) {
	db := &fakeDB{queryRow: rowWithNoRows()}
	store := NewStoreWithDB(db)

	_, err := store.GetByID(context.Background(), "uuid-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	call := db.lastQueryRow()
	if !strings.Contains(call.sql, "id = $1") {
		t.Error("SQL should filter by id")
	}
	if len(call.args) != 1 || call.args[0] != "uuid-123" {
		t.Errorf("args = %v, want [uuid-123]", call.args)
	}
	// Normal reads must not SELECT card_token_hash.
	if strings.Contains(call.sql, "card_token") {
		t.Error("GetByID must not SELECT card_token or card_token_hash")
	}
}

func TestGetByCardTokenNilHasherReturnsError(t *testing.T) {
	store := NewStoreWithDB(&fakeDB{queryRow: rowWithNoRows()})

	_, err := store.GetByCardToken(context.Background(), "card-xyz")
	if !errors.Is(err, ErrMissingHasher) {
		t.Errorf("expected ErrMissingHasher, got %v", err)
	}
}

// --- Store database error tests ---

func TestGetByUsernameDBError(t *testing.T) {
	db := &fakeDB{queryRow: rowWithError(errors.New("connection refused"))}
	store := NewStoreWithDB(db)

	_, err := store.GetByUsername(context.Background(), "nurse1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("error should contain original: %v", err)
	}
}

func TestGetByIDDBError(t *testing.T) {
	db := &fakeDB{queryRow: rowWithError(errors.New("timeout"))}
	store := NewStoreWithDB(db)

	_, err := store.GetByID(context.Background(), "id")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Errorf("error should contain original: %v", err)
	}
}

func TestGetByCardTokenDBError(t *testing.T) {
	hasher, err := NewCardTokenHasher("card-token-hmac-key-minimum-32!!")
	if err != nil {
		t.Fatalf("NewCardTokenHasher: %v", err)
	}
	db := &fakeDB{queryRow: rowWithError(errors.New("deadlock"))}
	store := NewStoreWithDBAndHasher(db, hasher)

	_, err = store.GetByCardToken(context.Background(), "token")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "deadlock") {
		t.Errorf("error should contain original: %v", err)
	}
}

// --- Store with CardTokenHasher tests ---

func TestGetByCardTokenWithHasherQueryArgs(t *testing.T) {
	hasher, err := NewCardTokenHasher("card-token-hmac-key-minimum-32!!")
	if err != nil {
		t.Fatalf("NewCardTokenHasher: %v", err)
	}

	expectedBytes, _ := hasher.Hash("card-xyz")
	db := &fakeDB{queryRow: rowWithNoRows()}
	store := NewStoreWithDBAndHasher(db, hasher)

	_, err = store.GetByCardToken(context.Background(), "card-xyz")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	call := db.lastQueryRow()
	if !strings.Contains(call.sql, "card_token_hash = $1") {
		t.Errorf("hashed query should filter by card_token_hash, got: %s", call.sql)
	}
	if len(call.args) != 1 || !bytesEqual(call.args[0], expectedBytes) {
		t.Errorf("args = %v, want [%x]", call.args, expectedBytes)
	}
	// Card-login scan must not SELECT card_token_hash (no 10th column).
	if strings.Contains(call.sql, "card_token_hash,") || strings.Contains(call.sql, "card_token_hash ,") {
		t.Error("GetByCardToken must not SELECT card_token_hash — hash is for lookup only")
	}
}

func TestGetByCardTokenWithHasherReturnsUser(t *testing.T) {
	hasher, err := NewCardTokenHasher("card-token-hmac-key-minimum-32!!")
	if err != nil {
		t.Fatalf("NewCardTokenHasher: %v", err)
	}

	u := User{
		ID:       "user-hashed",
		Username: "hasheduser",
		Role:     RoleNurse,
		Active:   true,
	}
	// Card-login now uses the same 9-column scan as normal reads.
	db := &fakeDB{queryRow: rowWithUser(u)}
	store := NewStoreWithDBAndHasher(db, hasher)

	user, err := store.GetByCardToken(context.Background(), "card-token-hashed")
	if err != nil {
		t.Fatalf("GetByCardToken: %v", err)
	}
	if user == nil {
		t.Fatal("expected user, got nil")
	}
	if user.Username != "hasheduser" {
		t.Errorf("Username = %q, want hasheduser", user.Username)
	}
}

func TestGetByCardTokenDifferentKeysProduceDifferentHashes(t *testing.T) {
	hasher1, _ := NewCardTokenHasher("key-one-minimum-32-bytes-key-one")
	hasher2, _ := NewCardTokenHasher("key-two-minimum-32-bytes-key-two")

	hash1, _ := hasher1.Hash("common-token")
	hash2, _ := hasher2.Hash("common-token")

	if bytesEqual(hash1, hash2) {
		t.Error("different keys must produce different hashes")
	}

	// Store with hasher1 should find the user; store with hasher2 should not.
	db := &fakeDB{queryRow: rowWithNoRows()}
	store1 := NewStoreWithDBAndHasher(db, hasher1)

	// Reset the calls for a fresh test.
	db.queryRowCalls = nil

	_, _ = store1.GetByCardToken(context.Background(), "common-token")
	call1 := db.queryRowCalls[0]

	// The query args must contain hash1 bytes, not hash2.
	if !bytesEqual(call1.args[0], hash1) {
		t.Errorf("store1 query used hash=%x, want %x", call1.args[0], hash1)
	}
}

// --- SetCardToken tests ---

func TestSetCardTokenSuccess(t *testing.T) {
	hasher, err := NewCardTokenHasher("card-token-hmac-key-minimum-32!!")
	if err != nil {
		t.Fatalf("NewCardTokenHasher: %v", err)
	}

	expectedBytes, _ := hasher.Hash("new-card-token")
	db := &fakeDB{
		execTag: pgconn.NewCommandTag("UPDATE 1"),
	}
	store := NewStoreWithDBAndHasher(db, hasher)

	err = store.SetCardToken(context.Background(), "user-123", "new-card-token")
	if err != nil {
		t.Fatalf("SetCardToken: %v", err)
	}

	call := db.lastExec()
	if !strings.Contains(call.sql, "SET card_token_hash = $1") {
		t.Errorf("SQL should SET card_token_hash, got: %s", call.sql)
	}
	if !strings.Contains(call.sql, "WHERE id = $2") {
		t.Errorf("SQL should filter by id, got: %s", call.sql)
	}
	if len(call.args) != 2 || !bytesEqual(call.args[0], expectedBytes) || call.args[1] != "user-123" {
		t.Errorf("args = %v, want [%x, user-123]", call.args, expectedBytes)
	}
}

func TestSetCardTokenNilHasherReturnsError(t *testing.T) {
	store := NewStoreWithDB(&fakeDB{})

	err := store.SetCardToken(context.Background(), "user-123", "token")
	if !errors.Is(err, ErrMissingHasher) {
		t.Errorf("expected ErrMissingHasher, got %v", err)
	}
}

func TestSetCardTokenEmptyTokenReturnsError(t *testing.T) {
	hasher, _ := NewCardTokenHasher("card-token-hmac-key-minimum-32!!")
	db := &fakeDB{}
	store := NewStoreWithDBAndHasher(db, hasher)

	err := store.SetCardToken(context.Background(), "user-123", "")
	if !errors.Is(err, ErrMissingCardToken) {
		t.Errorf("expected ErrMissingCardToken, got %v", err)
	}
	if len(db.execCalls) != 0 {
		t.Errorf("expected no database writes, got %d", len(db.execCalls))
	}
}

func TestSetCardTokenUserNotFound(t *testing.T) {
	hasher, _ := NewCardTokenHasher("card-token-hmac-key-minimum-32!!")
	db := &fakeDB{
		execTag: pgconn.NewCommandTag("UPDATE 0"),
	}
	store := NewStoreWithDBAndHasher(db, hasher)

	err := store.SetCardToken(context.Background(), "nonexistent", "token")
	if err == nil {
		t.Fatal("expected error for user not found, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention not found: %v", err)
	}
}

func TestSetCardTokenDBError(t *testing.T) {
	hasher, _ := NewCardTokenHasher("card-token-hmac-key-minimum-32!!")
	db := &fakeDB{
		execErr: errors.New("disk full"),
	}
	store := NewStoreWithDBAndHasher(db, hasher)

	err := store.SetCardToken(context.Background(), "user-123", "token")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "set card token") {
		t.Errorf("error should wrap with 'set card token': %v", err)
	}
}

// --- SeedAdmin tests ---

func TestSeedAdminEmptyTable(t *testing.T) {
	countRow := &fakeRow{
		scanFn: func(dest ...any) error {
			*(dest[0].(*int)) = 0
			return nil
		},
	}
	db := &fakeDB{
		queryRow: countRow,
		execTag:  pgconn.NewCommandTag("INSERT 0 1"),
	}
	store := NewStoreWithDB(db)

	created, err := store.SeedAdmin(context.Background(), "hashed-password")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !created {
		t.Error("expected created=true for empty table")
	}

	// Verify the count query was made.
	if len(db.queryRowCalls) < 1 {
		t.Fatal("expected at least one QueryRow call")
	}
	countCall := db.queryRowCalls[0]
	if !strings.Contains(countCall.sql, "COUNT(*)") {
		t.Error("count query should use COUNT(*)")
	}

	// Verify the insert was executed.
	execCall := db.lastExec()
	if !strings.Contains(execCall.sql, "INSERT INTO identity.users") {
		t.Error("insert SQL should reference identity.users")
	}
	if !strings.Contains(execCall.sql, "'admin'") {
		t.Error("should insert with username 'admin'")
	}
	if !strings.Contains(execCall.sql, "'ADMIN'") {
		t.Error("should insert with role 'ADMIN'")
	}
	// Args should include the password hash.
	foundHash := false
	for _, a := range execCall.args {
		if s, ok := a.(string); ok && s == "hashed-password" {
			foundHash = true
			break
		}
	}
	if !foundHash {
		t.Error("insert args should include the password hash")
	}
}

func TestSeedAdminNonEmptyTable(t *testing.T) {
	countRow := &fakeRow{
		scanFn: func(dest ...any) error {
			*(dest[0].(*int)) = 5
			return nil
		},
	}
	db := &fakeDB{
		queryRow: countRow,
	}
	store := NewStoreWithDB(db)

	created, err := store.SeedAdmin(context.Background(), "hashed-password")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created {
		t.Error("expected created=false for non-empty table")
	}

	// Verify no INSERT was attempted.
	if len(db.execCalls) != 0 {
		t.Errorf("expected 0 Exec calls when table non-empty, got %d", len(db.execCalls))
	}
}

func TestSeedAdminCountError(t *testing.T) {
	db := &fakeDB{
		queryRow: rowWithError(errors.New("count failed")),
	}
	store := NewStoreWithDB(db)

	_, err := store.SeedAdmin(context.Background(), "hashed-password")
	if err == nil {
		t.Fatal("expected error from count query, got nil")
	}
	if !strings.Contains(err.Error(), "count users") {
		t.Errorf("error should wrap with 'count users': %v", err)
	}
}

func TestSeedAdminInsertError(t *testing.T) {
	countRow := &fakeRow{
		scanFn: func(dest ...any) error {
			*(dest[0].(*int)) = 0
			return nil
		},
	}
	db := &fakeDB{
		queryRow: countRow,
		execErr:  errors.New("insert failed"),
	}
	store := NewStoreWithDB(db)

	_, err := store.SeedAdmin(context.Background(), "hashed-password")
	if err == nil {
		t.Fatal("expected error from insert, got nil")
	}
	if !strings.Contains(err.Error(), "seed admin") {
		t.Errorf("error should wrap with 'seed admin': %v", err)
	}
}
