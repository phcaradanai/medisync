package cursor

import (
	"database/sql"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Store tracks relayed jobs and poll cursor for idempotency.
// Uses SQLite for local durability. Not suitable for HA — see design doc.
type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL")
	if err != nil {
		return nil, err
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS relayed_jobs (
			printops_job_id   TEXT PRIMARY KEY,
			request_id        TEXT NOT NULL,
			project_id        TEXT NOT NULL DEFAULT '',
			status            TEXT NOT NULL,
			relayed_at        TEXT NOT NULL,
			prescription_id   TEXT NOT NULL DEFAULT ''
		);
		CREATE INDEX IF NOT EXISTS idx_relayed_request_id ON relayed_jobs(request_id);
		CREATE TABLE IF NOT EXISTS poll_cursor (
			id                INTEGER PRIMARY KEY CHECK (id = 1),
			last_completed_at TEXT,
			updated_at        TEXT NOT NULL
		);
		INSERT OR IGNORE INTO poll_cursor (id, last_completed_at, updated_at)
		VALUES (1, NULL, datetime('now'));
	`)
	return err
}

// AlreadyRelayed returns true if this job has been published to NATS.
func (s *Store) AlreadyRelayed(requestID string) (bool, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(1) FROM relayed_jobs WHERE request_id = ?", requestID).Scan(&count)
	return count > 0, err
}

// RecordRelay marks a job as successfully relayed.
func (s *Store) RecordRelay(jobID, requestID, projectID, status, prescriptionID string) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO relayed_jobs (printops_job_id, request_id, project_id, status, relayed_at, prescription_id)
		 VALUES (?, ?, ?, ?, datetime('now'), ?)`,
		jobID, requestID, projectID, status, prescriptionID)
	return err
}

// Cursor returns the timestamp to use in the PrintOps API `since` query param.
// On first run (NULL), returns the startup lookback window.
func (s *Store) Cursor(lookbackMinutes int) string {
	var last string
	s.db.QueryRow("SELECT last_completed_at FROM poll_cursor WHERE id = 1").Scan(&last)
	if last == "" {
		return time.Now().Add(-time.Duration(lookbackMinutes) * time.Minute).UTC().Format(time.RFC3339)
	}
	return last
}

// UpdateCursor advances the poll cursor to the given timestamp.
func (s *Store) UpdateCursor(completedAt string) error {
	_, err := s.db.Exec(
		`UPDATE poll_cursor SET last_completed_at = ?, updated_at = datetime('now') WHERE id = 1 AND (last_completed_at IS NULL OR ? > last_completed_at)`,
		completedAt, completedAt)
	return err
}
