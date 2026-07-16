// Package inventory owns slot inventory tracking (cabinet slots, stock
// levels, refill operations). Every state mutation is audited and
// publishes stock.changed events via NATS JetStream.
package inventory

import "time"

// Slot is the domain model for a cabinet slot. It mirrors the proto
// medisync.inventory.v1.Slot fields and decouples the store from
// proto types.
type Slot struct {
	ID           string
	CabinetID    string
	Code         string
	DisplayName  string
	DrugID       string
	DrugCode     string
	DrugName     string
	Capacity     int32
	Quantity     int32
	LowThreshold int32
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
