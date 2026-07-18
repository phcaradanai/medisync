package inventory

import (
	"sort"
	"time"
)

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
	ProjectID    string
	ExpiryDate   *time.Time
	Shelf        int32
	RowNum       int32
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// SlotBatch represents a single refill batch within a slot.
// FIFO dispensing consumes batches in expiry-date order.
type SlotBatch struct {
	ID         string
	SlotID     string
	SlotCode   string
	LotNumber  string
	ExpiryDate *time.Time
	Quantity   int32
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// FIFOAllocation describes how much to take from which batch.
type FIFOAllocation struct {
	SlotCode   string
	BatchID    string
	LotNumber  string
	ExpiryDate *time.Time
	TakeQty    int32
}

// AllocateFIFO allocates a requested quantity across multiple batches,
// consuming the earliest-expiring batches first.
// Returns allocations and any remaining unfulfilled quantity.
func AllocateFIFO(batches []SlotBatch, requested int32) ([]FIFOAllocation, int32) {
	sort.Slice(batches, func(i, j int) bool {
		ei, ej := batches[i].ExpiryDate, batches[j].ExpiryDate
		if ei == nil && ej == nil { return false }
		if ei == nil { return false }
		if ej == nil { return true }
		return ei.Before(*ej)
	})

	var allocs []FIFOAllocation
	remaining := requested
	for i := range batches {
		if remaining <= 0 { break }
		if batches[i].Quantity <= 0 { continue }
		take := batches[i].Quantity
		if take > remaining { take = remaining }
		allocs = append(allocs, FIFOAllocation{
			SlotCode:   batches[i].SlotCode,
			BatchID:    batches[i].ID,
			LotNumber:  batches[i].LotNumber,
			ExpiryDate: batches[i].ExpiryDate,
			TakeQty:    take,
		})
		remaining -= take
	}
	return allocs, remaining
}
