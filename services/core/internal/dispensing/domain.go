// Package dispensing owns the prescription aggregate: intake from the
// hospital feed (M1), the dispense state machine and fulfillment
// coordination (M2+).
package dispensing

import (
	"fmt"
	"time"
)

// State represents a prescription lifecycle state as defined in the proto.
type State string

const (
	StateReceived   State = "RECEIVED"
	StateReady      State = "READY"
	StateDispensing State = "DISPENSING"
	StateDispensed  State = "DISPENSED"
	StateFailed     State = "FAILED"
	StateCancelled  State = "CANCELLED"
	StateExpired    State = "EXPIRED"
)

// ValidTransitions defines the prescription state machine.
// Terminal states (DISPENSED, FAILED, CANCELLED, EXPIRED) have no outgoing edges.
var ValidTransitions = map[State][]State{
	StateReceived:   {StateReady, StateCancelled, StateExpired},
	StateReady:      {StateDispensing, StateCancelled, StateExpired},
	StateDispensing: {StateDispensed, StateFailed},
}

// CanTransitionTo validates that a transition from current to target is legal.
// Returns nil when the transition is allowed, or an error describing why not.
func (s State) CanTransitionTo(target State) error {
	allowed, ok := ValidTransitions[s]
	if !ok {
		return fmt.Errorf("state %q is terminal; no transitions allowed", s)
	}
	for _, t := range allowed {
		if t == target {
			return nil
		}
	}
	return fmt.Errorf("cannot transition from %q to %q", s, target)
}

// Item represents a single line item within a prescription.
type Item struct {
	DrugCode   string `json:"drug_code"`
	DrugName   string `json:"drug_name"`
	Quantity   int32  `json:"quantity"`
	DosageText string `json:"dosage_text"`
}

// Prescription is the insert/consumer model — the subset of fields the
// hospital feeder provides. Used by the NATS consumer (consumer.go) and
// Store.Insert. The full row model (PrescriptionRow) is used for reads.
type Prescription struct {
	PrescriptionID string
	SourceSystem   string
	ProjectID      string
	HN             string
	PatientName    string
	WardID         string
	Items          []Item
	IssuedAt       *time.Time
}

// PrescriptionRow is the full database row model returned by read operations.
type PrescriptionRow struct {
	ID             string
	PrescriptionID string
	SourceSystem   string
	HN             string
	PatientName    string
	WardID         string
	Items          []Item
	State          State
	FailureReason  string
	IssuedAt       *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
