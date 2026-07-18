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

// SlotCapacityInput holds the dimensions used for capacity calculation.
type SlotCapacityInput struct {
	SlotWidth  float64 // cm
	SlotDepth  float64 // cm
	SlotHeight float64 // cm
	DrugWidth  float64 // cm per unit
	DrugDepth  float64 // cm per unit
	DrugHeight float64 // cm per unit
}

// CalculateSlotCapacity computes how many drug units fit in a slot
// based on physical dimensions (all in centimeters).
func CalculateSlotCapacity(in SlotCapacityInput) int32 {
	if in.SlotWidth <= 0 || in.SlotDepth <= 0 || in.SlotHeight <= 0 ||
		in.DrugWidth <= 0 || in.DrugDepth <= 0 || in.DrugHeight <= 0 {
		return 0
	}
	w := int32(in.SlotWidth / in.DrugWidth)
	d := int32(in.SlotDepth / in.DrugDepth)
	h := int32(in.SlotHeight / in.DrugHeight)
	if w <= 0 || d <= 0 || h <= 0 { return 0 }
	return w * d * h
}

// SlotGroupCapacity calculates total capacity across a group of slots.
func SlotGroupCapacity(slots []SlotCapacityInput, drug SlotCapacityInput) int32 {
	var total int32
	for _, s := range slots {
		total += CalculateSlotCapacity(SlotCapacityInput{
			SlotWidth: s.SlotWidth, SlotDepth: s.SlotDepth, SlotHeight: s.SlotHeight,
			DrugWidth: drug.DrugWidth, DrugDepth: drug.DrugDepth, DrugHeight: drug.DrugHeight,
		})
	}
	return total
}
