// Package dispensing owns the prescription aggregate: intake from the
// hospital feed (M1), the dispense state machine and fulfillment
// coordination (M2+).
package dispensing

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/adm-chura3inter/medisync/services/core/internal/testutil"
)

type Item struct {
	DrugCode   string `json:"drug_code"`
	DrugName   string `json:"drug_name"`
	Quantity   int32  `json:"quantity"`
	DosageText string `json:"dosage_text"`
}

type Prescription struct {
	PrescriptionID string
	SourceSystem   string
	HN             string
	PatientName    string
	WardID         string
	Items          []Item
	IssuedAt       *time.Time
}

// Store persists prescriptions to PostgreSQL. The exported NewStore
// constructor accepts *pgxpool.Pool for production wiring; unit tests
// inject a testutil.Execer fake through NewStoreWithDB.
type Store struct {
	db testutil.Execer
}

// NewStore creates a Store backed by a pgx connection pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{db: pool}
}

// NewStoreWithDB creates a Store backed by an arbitrary Execer.
// Exported for use by integration and unit tests.
func NewStoreWithDB(db testutil.Execer) *Store {
	return &Store{db: db}
}

// Insert stores a new prescription in READY state. Returns false when the
// (prescription_id, source_system) pair already exists — replayed events are
// silently deduplicated, per the intake idempotency rule.
func (s *Store) Insert(ctx context.Context, p Prescription) (bool, error) {
	items, err := json.Marshal(p.Items)
	if err != nil {
		return false, fmt.Errorf("marshal prescription items: %w", err)
	}

	tag, err := s.db.Exec(ctx,
		`INSERT INTO dispensing.prescription
		   (prescription_id, source_system, hn, patient_name, ward_id, items, state, issued_at)
		 VALUES ($1, $2, $3, $4, $5, $6, 'READY', $7)
		 ON CONFLICT ON CONSTRAINT prescription_external_key DO NOTHING`,
		p.PrescriptionID, p.SourceSystem, p.HN, p.PatientName, p.WardID, items, p.IssuedAt)
	if err != nil {
		return false, fmt.Errorf("insert prescription: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}
