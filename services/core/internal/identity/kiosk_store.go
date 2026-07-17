package identity

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

// KioskStore is the narrow interface for kiosk persistence consumed by
// the kiosk handler. The concrete KioskPGStore satisfies this interface.
type KioskStore interface {
	List(ctx context.Context, projectID string, pageSize int32, pageToken string) ([]*Kiosk, string, int64, error)
	Create(ctx context.Context, k *Kiosk) error
	GetByCode(ctx context.Context, code string) (*Kiosk, error)
	GetByID(ctx context.Context, id string) (*Kiosk, error)
	Update(ctx context.Context, k *Kiosk) error
	UpdatePIN(ctx context.Context, id, pinHash string) error
}

// KioskPGStore persists kiosks to PostgreSQL.
type KioskPGStore struct {
	db dbConn
}

// NewKioskStore creates a KioskPGStore backed by a pgx connection pool.
func NewKioskStore(pool *pgxpool.Pool) *KioskPGStore {
	return &KioskPGStore{db: pool}
}

// NewKioskStoreWithDB creates a KioskPGStore backed by an arbitrary dbConn.
func NewKioskStoreWithDB(db dbConn) *KioskPGStore {
	return &KioskPGStore{db: db}
}

// List returns kiosks scoped to a project, using a created_at cursor whose
// token is the last kiosk ID. An empty projectID lists all kiosks.
func (s *KioskPGStore) List(ctx context.Context, projectID string, pageSize int32, pageToken string) ([]*Kiosk, string, int64, error) {
	pageSize = pagination.NormalizePageSize(pageSize)
	args := []any{}
	whereSQL := ""
	argIdx := 1
	if projectID != "" {
		whereSQL = "WHERE project_id = $1"
		args = append(args, projectID)
		argIdx++
	}

	var totalCount int64
	countQuery := "SELECT COUNT(*) FROM identity.kiosks " + whereSQL
	if err := s.db.QueryRow(ctx, countQuery, args...).Scan(&totalCount); err != nil {
		return nil, "", 0, fmt.Errorf("count kiosks: %w", err)
	}

	if pageToken != "" {
		cursorClause := fmt.Sprintf(
			"created_at < (SELECT created_at FROM identity.kiosks WHERE id = $%d)",
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
	query := fmt.Sprintf(
		`SELECT id, code, display_name, pin_hash, active, project_id, created_at, updated_at
		   FROM identity.kiosks %s ORDER BY created_at DESC, id DESC LIMIT $%d`,
		whereSQL, argIdx,
	)
	args = append(args, pageSize+1)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, "", 0, fmt.Errorf("list kiosks: %w", err)
	}
	defer rows.Close()
	kiosks, err := scanKiosks(rows)
	if err != nil {
		return nil, "", 0, err
	}

	var nextPageToken string
	if len(kiosks) > int(pageSize) {
		nextPageToken = kiosks[pageSize-1].ID
		kiosks = kiosks[:pageSize]
	}
	return kiosks, nextPageToken, totalCount, nil
}

// Create inserts a new kiosk. Returns ErrDuplicateKioskCode on conflict.
func (s *KioskPGStore) Create(ctx context.Context, k *Kiosk) error {
	tag, err := s.db.Exec(ctx,
		`INSERT INTO identity.kiosks (code, display_name, pin_hash, project_id)
		 VALUES ($1, $2, $3, $4)`,
		k.Code, k.DisplayName, k.PinHash, k.ProjectID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrDuplicateKioskCode
		}
		return fmt.Errorf("create kiosk: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errors.New("create kiosk: no rows affected")
	}
	return nil
}

// GetByCode returns a kiosk by its unique code, or nil if not found.
func (s *KioskPGStore) GetByCode(ctx context.Context, code string) (*Kiosk, error) {
	row := s.db.QueryRow(ctx,
		`SELECT id, code, display_name, pin_hash, active, project_id, created_at, updated_at
		   FROM identity.kiosks WHERE code = $1`, code)
	return scanKiosk(row)
}

// GetByID returns a kiosk by UUID, or nil if not found.
func (s *KioskPGStore) GetByID(ctx context.Context, id string) (*Kiosk, error) {
	row := s.db.QueryRow(ctx,
		`SELECT id, code, display_name, pin_hash, active, project_id, created_at, updated_at
		   FROM identity.kiosks WHERE id = $1`, id)
	return scanKiosk(row)
}

// Update modifies display_name and active flag.
func (s *KioskPGStore) Update(ctx context.Context, k *Kiosk) error {
	tag, err := s.db.Exec(ctx,
		`UPDATE identity.kiosks
		   SET display_name = $1, active = $2, updated_at = now()
		 WHERE id = $3`, k.DisplayName, k.Active, k.ID)
	if err != nil {
		return fmt.Errorf("update kiosk: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrKioskNotFound
	}
	return nil
}

// UpdatePIN replaces the PIN hash for a kiosk.
func (s *KioskPGStore) UpdatePIN(ctx context.Context, id, pinHash string) error {
	tag, err := s.db.Exec(ctx,
		`UPDATE identity.kiosks SET pin_hash = $1, updated_at = now() WHERE id = $2`,
		pinHash, id)
	if err != nil {
		return fmt.Errorf("update kiosk pin: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrKioskNotFound
	}
	return nil
}

func scanKiosks(rows pgx.Rows) ([]*Kiosk, error) {
	var kiosks []*Kiosk
	for rows.Next() {
		k, err := scanKiosk(rows)
		if err != nil {
			return nil, err
		}
		kiosks = append(kiosks, k)
	}
	return kiosks, rows.Err()
}

// scanKiosk maps a pgx.Row to a Kiosk. Returns nil for pgx.ErrNoRows.
func scanKiosk(row pgx.Row) (*Kiosk, error) {
	var k Kiosk
	var createdAt, updatedAt time.Time
	err := row.Scan(&k.ID, &k.Code, &k.DisplayName, &k.PinHash,
		&k.Active, &k.ProjectID, &createdAt, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan kiosk: %w", err)
	}
	k.CreatedAt = createdAt
	k.UpdatedAt = updatedAt
	return &k, nil
}

// Compile-time check: KioskPGStore satisfies KioskStore.
var _ KioskStore = (*KioskPGStore)(nil)
