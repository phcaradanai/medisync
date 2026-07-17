package cabinet

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/adm-chura3inter/medisync/services/core/internal/platform/pagination"
)

// Compile-time check that *pgxpool.Pool satisfies dbConn.
var _ dbConn = (*pgxpool.Pool)(nil)

// dbConn is the narrow database interface.
type dbConn interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Store persists cabinets to PostgreSQL.
type Store struct {
	db dbConn
}

// NewStore creates a Store backed by a pgx pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{db: pool}
}

// NewStoreWithDB creates a Store backed by an arbitrary dbConn.
func NewStoreWithDB(db dbConn) *Store {
	return &Store{db: db}
}

// List returns cabinets, optionally scoped to a project, using a created_at
// cursor whose token is the last cabinet ID.
// When projectID is non-empty, only cabinets in that project are returned.
func (s *Store) List(ctx context.Context, projectID string, pageSize int32, pageToken string) ([]*Cabinet, string, int64, error) {
	pageSize = pagination.NormalizePageSize(pageSize)
	query := `SELECT id, code, name, display_name, active, project_id, created_at, updated_at
		   FROM cabinet.cabinet`
	args := []any{}
	whereSQL := ""
	argIdx := 1
	if projectID != "" {
		whereSQL = "WHERE project_id = $1"
		args = append(args, projectID)
		argIdx++
	}

	var totalCount int64
	countQuery := "SELECT COUNT(*) FROM cabinet.cabinet " + whereSQL
	if err := s.db.QueryRow(ctx, countQuery, args...).Scan(&totalCount); err != nil {
		return nil, "", 0, fmt.Errorf("count cabinets: %w", err)
	}

	if pageToken != "" {
		cursorClause := fmt.Sprintf(
			"created_at < (SELECT created_at FROM cabinet.cabinet WHERE id = $%d)",
			argIdx,
		)
		if whereSQL == "" {
			whereSQL = "WHERE " + cursorClause
		} else {
			whereSQL += " AND " + cursorClause
		}
		args = append(args, pageToken)
		argIdx++
	}
	query += " " + whereSQL + fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d", argIdx)
	args = append(args, pageSize+1)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, "", 0, fmt.Errorf("list cabinets: %w", err)
	}
	defer rows.Close()

	var cabinets []*Cabinet
	for rows.Next() {
		c, err := scanCabinet(rows)
		if err != nil {
			return nil, "", 0, err
		}
		cabinets = append(cabinets, c)
	}
	if err := rows.Err(); err != nil {
		return nil, "", 0, fmt.Errorf("iterate cabinet rows: %w", err)
	}

	var nextPageToken string
	if len(cabinets) > int(pageSize) {
		nextPageToken = cabinets[pageSize-1].ID
		cabinets = cabinets[:pageSize]
	}
	return cabinets, nextPageToken, totalCount, nil
}

// GetByID returns a Cabinet by UUID, or nil if not found.
func (s *Store) GetByID(ctx context.Context, id string) (*Cabinet, error) {
	row := s.db.QueryRow(ctx,
		`SELECT id, code, name, display_name, active, project_id, created_at, updated_at
		   FROM cabinet.cabinet WHERE id = $1`, id)
	return scanCabinet(row)
}

// Create inserts a new cabinet.
func (s *Store) Create(ctx context.Context, code, name, displayName, projectID string) (*Cabinet, error) {
	row := s.db.QueryRow(ctx,
		`INSERT INTO cabinet.cabinet (code, name, display_name, project_id)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (code) DO NOTHING
		 RETURNING id, code, name, display_name, active, project_id, created_at, updated_at`,
		code, name, displayName, projectID)
	c, err := scanCabinet(row)
	if err != nil {
		return nil, fmt.Errorf("create cabinet: %w", err)
	}
	if c == nil {
		return nil, ErrDuplicateCode
	}
	return c, nil
}

// Update modifies name and/or active flag.
func (s *Store) Update(ctx context.Context, id string, name *string, active *bool) (*Cabinet, error) {
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
		return s.GetByID(ctx, id)
	}

	setClauses = append(setClauses, "updated_at = now()")
	query := fmt.Sprintf(
		`UPDATE cabinet.cabinet SET %s WHERE id = $1
		 RETURNING id, code, name, display_name, active, project_id, created_at, updated_at`,
		joinClauses(setClauses))

	row := s.db.QueryRow(ctx, query, args...)
	return scanCabinet(row)
}

func scanCabinet(row pgx.Row) (*Cabinet, error) {
	var c Cabinet
	var createdAt, updatedAt time.Time
	err := row.Scan(&c.ID, &c.Code, &c.Name, &c.DisplayName, &c.Active, &c.ProjectID, &createdAt, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan cabinet: %w", err)
	}
	c.CreatedAt = createdAt
	c.UpdatedAt = updatedAt
	return &c, nil
}

func joinClauses(parts []string) string {
	s := ""
	for i, p := range parts {
		if i > 0 {
			s += ", "
		}
		s += p
	}
	return s
}

// Domain errors.
var (
	ErrDuplicateCode = errors.New("cabinet code already exists")
	ErrNotFound      = errors.New("cabinet not found")
)
