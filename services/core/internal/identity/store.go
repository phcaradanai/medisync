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
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
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
		        project_id, active, created_at, updated_at
		   FROM identity.users WHERE username = $1`, username)
	return scanUser(row)
}

// GetByID returns a User by UUID, or nil if not found.
// Normal reads do not load card_token_hash.
func (s *Store) GetByID(ctx context.Context, id string) (*User, error) {
	row := s.db.QueryRow(ctx,
		`SELECT id, username, password_hash, display_name, role, ward_ids,
		        project_id, active, created_at, updated_at
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
		        project_id, active, created_at, updated_at
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
		`INSERT INTO identity.users (username, password_hash, display_name, role, ward_ids, project_id)
		 VALUES ('admin', $1, 'Administrator', 'ADMIN', '{}', NULL)`, passwordHash)
	if err != nil {
		return false, fmt.Errorf("seed admin: %w", err)
	}
	return true, nil
}

// ListUsers returns users, optionally filtered by search query and project.
// When projectID is non-empty, only users in that project are returned.
func (s *Store) ListUsers(ctx context.Context, query, projectID string) ([]*User, error) {
	var rows pgx.Rows
	var err error

	baseSQL := `SELECT id, username, password_hash, display_name, role, ward_ids,
	                   project_id, active, created_at, updated_at
	              FROM identity.users`
	var conditions []string
	var args []any
	argIdx := 1

	if query != "" {
		conditions = append(conditions, fmt.Sprintf("(username ILIKE $%d OR display_name ILIKE $%d)", argIdx, argIdx))
		args = append(args, "%"+query+"%")
		argIdx++
	}
	if projectID != "" {
		conditions = append(conditions, fmt.Sprintf("project_id = $%d", argIdx))
		args = append(args, projectID)
		argIdx++
	}

	sql := baseSQL
	if len(conditions) > 0 {
		sql += " WHERE " + conditions[0]
		for i := 1; i < len(conditions); i++ {
			sql += " AND " + conditions[i]
		}
	}
	sql += " ORDER BY username ASC"

	rows, err = s.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		var u User
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.DisplayName,
			(*roleScanner)(&u.Role), &u.WardIDs, &u.Active, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan user row: %w", err)
		}
		u.CreatedAt = createdAt
		u.UpdatedAt = updatedAt
		users = append(users, &u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user rows: %w", err)
	}
	return users, nil
}

// CreateUser inserts a new user. Returns ErrUsernameTaken when the
// username already exists.
func (s *Store) CreateUser(ctx context.Context, username, passwordHash, displayName string, role Role, wardIDs []string, projectID string) (*User, error) {
	if wardIDs == nil {
		wardIDs = []string{}
	}
	row := s.db.QueryRow(ctx,
		`INSERT INTO identity.users (username, password_hash, display_name, role, ward_ids, project_id, active)
		 VALUES ($1, $2, $3, $4, $5, $6, true)
		 ON CONFLICT (username) DO NOTHING
		 RETURNING id, username, password_hash, display_name, role, ward_ids, project_id, active, created_at, updated_at`,
		username, passwordHash, displayName, string(role), wardIDs, projectID)
	u, err := scanUser(row)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	if u == nil {
		return nil, ErrUsernameTaken
	}
	return u, nil
}

// UpdateUser modifies an existing user's mutable fields.
// Returns nil when the user is not found.
func (s *Store) UpdateUser(ctx context.Context, id string, displayName *string, role *Role, active *bool, wardIDs []string, projectID *string) (*User, error) {
	// Build dynamic UPDATE. Only set fields that are provided.
	setClauses := []string{}
	args := []any{id}
	argIdx := 2

	if displayName != nil {
		setClauses = append(setClauses, fmt.Sprintf("display_name = $%d", argIdx))
		args = append(args, *displayName)
		argIdx++
	}
	if role != nil {
		setClauses = append(setClauses, fmt.Sprintf("role = $%d", argIdx))
		args = append(args, string(*role))
		argIdx++
	}
	if active != nil {
		setClauses = append(setClauses, fmt.Sprintf("active = $%d", argIdx))
		args = append(args, *active)
		argIdx++
	}
	if wardIDs != nil {
		setClauses = append(setClauses, fmt.Sprintf("ward_ids = $%d", argIdx))
		args = append(args, wardIDs)
		argIdx++
	}
	if projectID != nil {
		setClauses = append(setClauses, fmt.Sprintf("project_id = $%d", argIdx))
		args = append(args, *projectID)
		argIdx++
	}
	if len(setClauses) == 0 {
		// Nothing to update — return the current user.
		return s.GetByID(ctx, id)
	}

	setClauses = append(setClauses, "updated_at = now()")

	querySQL := fmt.Sprintf(
		`UPDATE identity.users SET %s WHERE id = $1
		 RETURNING id, username, password_hash, display_name, role, ward_ids, project_id, active, created_at, updated_at`,
		joinWithCommas(setClauses))

	row := s.db.QueryRow(ctx, querySQL, args...)
	return scanUser(row)
}

func joinWithCommas(parts []string) string {
	s := ""
	for i, p := range parts {
		if i > 0 {
			s += ", "
		}
		s += p
	}
	return s
}

// scanUser maps a pgx.Row to a User from a 10-column result (no card data).
// Columns: id, username, password_hash, display_name, role, ward_ids,
//          project_id, active, created_at, updated_at.
// Returns nil when the row is empty (pgx.ErrNoRows).
func scanUser(row pgx.Row) (*User, error) {
	var u User
	var createdAt, updatedAt time.Time
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.DisplayName,
		(*roleScanner)(&u.Role), &u.WardIDs, &u.ProjectID,
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

// ── Project CRUD ──────────────────────────────────────────────────

// CreateProject inserts a new project.
func (s *Store) CreateProject(ctx context.Context, name, slug string) (*Project, error) {
	var p Project
	var createdAt, updatedAt time.Time
	err := s.db.QueryRow(ctx,
		`INSERT INTO identity.projects (name, slug) VALUES ($1, $2)
		 RETURNING id, name, slug, active, created_at, updated_at`,
		name, slug).Scan(&p.ID, &p.Name, &p.Slug, &p.Active, &createdAt, &updatedAt)
	if err != nil {
		return nil, fmt.Errorf("create project: %w", err)
	}
	p.CreatedAt = createdAt
	p.UpdatedAt = updatedAt
	return &p, nil
}

// GetProject returns a project by ID, or nil if not found.
func (s *Store) GetProject(ctx context.Context, id string) (*Project, error) {
	var p Project
	var createdAt, updatedAt time.Time
	err := s.db.QueryRow(ctx,
		`SELECT id, name, slug, active, created_at, updated_at
		   FROM identity.projects WHERE id = $1`, id).Scan(
		&p.ID, &p.Name, &p.Slug, &p.Active, &createdAt, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get project: %w", err)
	}
	p.CreatedAt = createdAt
	p.UpdatedAt = updatedAt
	return &p, nil
}

// ListProjects returns all projects.
func (s *Store) ListProjects(ctx context.Context) ([]*Project, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, name, slug, active, created_at, updated_at
		   FROM identity.projects ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()
	var projects []*Project
	for rows.Next() {
		var p Project
		var ca, ua time.Time
		if err := rows.Scan(&p.ID, &p.Name, &p.Slug, &p.Active, &ca, &ua); err != nil {
			return nil, fmt.Errorf("scan project: %w", err)
		}
		p.CreatedAt = ca
		p.UpdatedAt = ua
		projects = append(projects, &p)
	}
	return projects, rows.Err()
}

// UpdateProject modifies a project's name or active flag.
func (s *Store) UpdateProject(ctx context.Context, id string, name *string, active *bool) (*Project, error) {
	setClauses := []string{}
	args := []any{id}
	argIdx := 2
	if name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, *name)
		argIdx++
	}
	if active != nil {
		setClauses = append(setClauses, fmt.Sprintf("active = $%d", argIdx))
		args = append(args, *active)
		argIdx++
	}
	if len(setClauses) == 0 {
		return s.GetProject(ctx, id)
	}
	setClauses = append(setClauses, "updated_at = now()")
	querySQL := fmt.Sprintf(
		`UPDATE identity.projects SET %s WHERE id = $1
		 RETURNING id, name, slug, active, created_at, updated_at`,
		joinWithCommas(setClauses))
	var p Project
	var ca, ua time.Time
	err := s.db.QueryRow(ctx, querySQL, args...).Scan(&p.ID, &p.Name, &p.Slug, &p.Active, &ca, &ua)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("update project: %w", err)
	}
	p.CreatedAt = ca
	p.UpdatedAt = ua
	return &p, nil
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
