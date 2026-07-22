package inventory

import (
	"testing"
	"time"
)

func makeDate(day int) *time.Time {
	t := time.Date(2026, 7, day, 0, 0, 0, 0, time.UTC)
	return &t
}

func TestAllocateFIFO_SingleBatch(t *testing.T) {
	batches := []SlotBatch{
		{ID: "b1", SlotCode: "A01", Quantity: 50, ExpiryDate: makeDate(20)},
	}
	allocs, remaining := AllocateFIFO(batches, 10)

	if len(allocs) != 1 {
		t.Fatalf("expected 1 allocation, got %d", len(allocs))
	}
	if allocs[0].TakeQty != 10 {
		t.Errorf("TakeQty = %d, want 10", allocs[0].TakeQty)
	}
	if remaining != 0 {
		t.Errorf("remaining = %d, want 0", remaining)
	}
}

func TestAllocateFIFO_EarliestFirst(t *testing.T) {
	// Slot 2 has earlier expiry (July 14) than Slot 1 (July 20)
	batches := []SlotBatch{
		{ID: "b1", SlotCode: "A01", Quantity: 50, ExpiryDate: makeDate(20)}, // later
		{ID: "b2", SlotCode: "B03", Quantity: 30, ExpiryDate: makeDate(14)}, // earlier
	}
	allocs, _ := AllocateFIFO(batches, 10)

	// Should take from B03 first (earliest expiry)
	if allocs[0].SlotCode != "B03" {
		t.Errorf("first allocation SlotCode = %q, want B03 (earliest expiry)", allocs[0].SlotCode)
	}
}

func TestAllocateFIFO_SplitAcrossBatches(t *testing.T) {
	// Request 60, but batch 1 only has 20, batch 2 has 50
	batches := []SlotBatch{
		{ID: "b1", SlotCode: "A01", Quantity: 20, ExpiryDate: makeDate(12)}, // earliest
		{ID: "b2", SlotCode: "B03", Quantity: 50, ExpiryDate: makeDate(14)},
	}
	allocs, remaining := AllocateFIFO(batches, 60)

	if len(allocs) != 2 {
		t.Fatalf("expected 2 allocations, got %d", len(allocs))
	}
	if allocs[0].TakeQty != 20 {
		t.Errorf("first TakeQty = %d, want 20", allocs[0].TakeQty)
	}
	if allocs[1].TakeQty != 40 {
		t.Errorf("second TakeQty = %d, want 40", allocs[1].TakeQty)
	}
	if remaining != 0 {
		t.Errorf("remaining = %d, want 0", remaining)
	}
}

func TestAllocateFIFO_InsufficientStock(t *testing.T) {
	batches := []SlotBatch{
		{ID: "b1", SlotCode: "A01", Quantity: 10, ExpiryDate: makeDate(12)},
	}
	_, remaining := AllocateFIFO(batches, 50)

	if remaining != 40 {
		t.Errorf("remaining = %d, want 40", remaining)
	}
}

func TestAllocateFIFO_ComplexMultiSlot(t *testing.T) {
	// User's example: Slot1 batch1=12, batch2=14; Slot2 batch1=13
	// Order: S1-b1(12), S2-b1(13), S1-b2(14)
	batches := []SlotBatch{
		{ID: "s1b1", SlotCode: "S1", Quantity: 1, ExpiryDate: makeDate(12)},
		{ID: "s1b2", SlotCode: "S1", Quantity: 1, ExpiryDate: makeDate(14)},
		{ID: "s2b1", SlotCode: "S2", Quantity: 1, ExpiryDate: makeDate(13)},
	}
	allocs, _ := AllocateFIFO(batches, 3)

	if len(allocs) != 3 {
		t.Fatalf("expected 3 allocations, got %d", len(allocs))
	}
	// Order should be: S1 (12d), S2 (13d), S1 (14d)
	expected := []string{"S1", "S2", "S1"}
	for i, a := range allocs {
		if a.SlotCode != expected[i] {
			t.Errorf("allocation[%d] SlotCode = %q, want %q", i, a.SlotCode, expected[i])
		}
	}
}

func TestAllocateFIFO_NilExpiryGoesLast(t *testing.T) {
	batches := []SlotBatch{
		{ID: "b1", SlotCode: "A01", Quantity: 10, ExpiryDate: nil},
		{ID: "b2", SlotCode: "B03", Quantity: 10, ExpiryDate: makeDate(15)},
	}
	allocs, _ := AllocateFIFO(batches, 10)

	if allocs[0].SlotCode != "B03" {
		t.Errorf("nil expiry should go last, got first = %q, want B03", allocs[0].SlotCode)
	}
}

func TestAllocateFIFO_EmptyBatchSkipped(t *testing.T) {
	batches := []SlotBatch{
		{ID: "b1", SlotCode: "A01", Quantity: 0, ExpiryDate: makeDate(10)},
		{ID: "b2", SlotCode: "B03", Quantity: 5, ExpiryDate: makeDate(20)},
	}
	allocs, _ := AllocateFIFO(batches, 5)

	if len(allocs) != 1 || allocs[0].SlotCode != "B03" {
		t.Errorf("empty batch should be skipped")
	}
}
