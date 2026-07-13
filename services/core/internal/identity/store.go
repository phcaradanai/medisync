package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// dbConn is the narrow database interface for the identity store.
// *pgxpool.Pool satisfies this interface; tests inject a deterministic fake.
type dbConn interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Compile-time check that *pgxpool.Pool satisfies dbConn.
var _ dbConn = (*pgxpool.Pool)(nil)

// Store persists users to PostgreSQL. Pattern follows dispensing.Store.
type Store struct {
	db     dbConn
	hasher *CardTokenHasher
}

// NewStore creates a Store backed by a pgx connection pool.
// It does not enable card-token hashing; use NewStoreWithHasher
// to configure deterministic keyed-hash lookups.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{db: pool}
}

// NewStoreWithDB creates a Store backed by an arbitrary dbConn.
func NewStoreWithDB(db dbConn) *Store {
	return &Store{db: db}
}

// NewStoreWithHasher creates a Store backed by a pgx pool with a
// card-token hasher for deterministic keyed-hash lookups.
func NewStoreWithHasher(pool *pgxpool.Pool, hasher *CardTokenHasher) *Store {
	return &Store{db: pool, hasher: hasher}
}

// NewStoreWithDBAndHasher creates a Store backed by an arbitrary dbConn
// with a card-token hasher.
func NewStoreWithDBAndHasher(db dbConn, hasher *CardTokenHasher) *Store {
	return &Store{db: db, hasher: hasher}
}

// GetByUsername returns a User by username, or nil if not found.
// Normal reads do not load card_token_hash.
func (s *Store) GetByUsername(ctx context.Context, username string) (*User, error) {
	row := s.db.QueryRow(ctx,
		`SELECT id, username, password_hash, display_name, role, ward_ids,
		        active, created_at, updated_at
		   FROM identity.users WHERE username = $1`, username)
	return scanUser(row)
}

// GetByID returns a User by UUID, or nil if not found.
// Normal reads do not load card_token_hash.
func (s *Store) GetByID(ctx context.Context, id string) (*User, error) {
	row := s.db.QueryRow(ctx,
		`SELECT id, username, password_hash, display_name, role, ward_ids,
		        active, created_at, updated_at
		   FROM identity.users WHERE id = $1`, id)
	return scanUser(row)
}

// GetByCardToken returns a User whose card-token hash matches, or nil if
// not found. The input token is hashed before the database lookup. A nil
// hasher is a configuration error; there is no raw-fallback path because
// the plaintext card_token column no longer exists.
func (s *Store) GetByCardToken(ctx context.Context, token string) (*User, error) {
	if s.hasher == nil {
		return nil, ErrMissingHasher
	}
	hash, err := s.hasher.Hash(token)
	if err != nil {
		return nil, fmt.Errorf("get by card token: hash: %w", err)
	}
	row := s.db.QueryRow(ctx,
		`SELECT id, username, password_hash, display_name, role, ward_ids,
		        active, created_at, updated_at
		   FROM identity.users WHERE card_token_hash = $1`, hash)
	return scanUser(row)
}

// SetCardToken enrolls (or re-enrolls) a card token for the given user.
// The raw token is HMAC-hashed before storage; only the hash is written.
// The hash is stored as raw bytes in the BYTEA column.
// It returns ErrMissingHasher when the Store was created without a hasher.
func (s *Store) SetCardToken(ctx context.Context, userID, rawToken string) error {
	if s.hasher == nil {
		return ErrMissingHasher
	}
	if rawToken == "" {
		return ErrMissingCardToken
	}
	hash, err := s.hasher.Hash(rawToken)
	if err != nil {
		return fmt.Errorf("set card token: hash: %w", err)
	}
	tag, err := s.db.Exec(ctx,
		`UPDATE identity.users SET card_token_hash = $1 WHERE id = $2`,
		hash, userID)
	if err != nil {
		return fmt.Errorf("set card token: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("set card token: user %q not found", userID)
	}
	return nil
}

// SeedAdmin inserts the bootstrapping admin user if no users exist.
// It returns true when a new admin was created.
func (s *Store) SeedAdmin(ctx context.Context, passwordHash string) (bool, error) {
	var count int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM identity.users`).Scan(&count); err != nil {
		return false, fmt.Errorf("count users: %w", err)
	}
	if count > 0 {
		return false, nil
	}

	_, err := s.db.Exec(ctx,
		`INSERT INTO identity.users (username, password_hash, display_name, role, ward_ids)
		 VALUES ('admin', $1, 'Administrator', 'ADMIN', '{}')`, passwordHash)
	if err != nil {
		return false, fmt.Errorf("seed admin: %w", err)
	}
	return true, nil
}

// scanUser maps a pgx.Row to a User from a 9-column result (no card data).
// Returns nil when the row is empty (pgx.ErrNoRows).
func scanUser(row pgx.Row) (*User, error) {
	var u User
	var createdAt, updatedAt time.Time
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.DisplayName,
		(*roleScanner)(&u.Role), &u.WardIDs,
		&u.Active, &createdAt, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan user: %w", err)
	}
	u.CreatedAt = createdAt
	u.UpdatedAt = updatedAt
	return &u, nil
}

// roleScanner scans a text column into a Role, rejecting unknown values.
type roleScanner Role

func (r *roleScanner) Scan(src any) error {
	s, ok := src.(string)
	if !ok {
		return fmt.Errorf("role: expected string, got %T", src)
	}
	switch s {
	case "ADMIN", "PHARMACIST", "NURSE", "REFILLER":
		*r = roleScanner(s)
		return nil
	default:
		return fmt.Errorf("role: unknown role %q", s)
	}
}
