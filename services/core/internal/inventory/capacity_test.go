package inventory

import "testing"

func TestCalculateSlotCapacity_Normal(t *testing.T) {
	// Slot 10x5x8 cm, Drug 2x1x0.5 cm
	// Width: 10/2=5, Depth: 5/1=5, Height: 8/0.5=16
	// Total: 5*5*16 = 400
	cap := CalculateSlotCapacity(SlotCapacityInput{
		SlotWidth: 10, SlotDepth: 5, SlotHeight: 8,
		DrugWidth: 2, DrugDepth: 1, DrugHeight: 0.5,
	})
	if cap != 400 {
		t.Errorf("capacity = %d, want 400", cap)
	}
}

func TestCalculateSlotCapacity_PartialFit(t *testing.T) {
	// Slot 5x5x5 cm, Drug 3x2x2 cm
	// Width: 5/3=1, Depth: 5/2=2, Height: 5/2=2
	// Total: 1*2*2 = 4
	cap := CalculateSlotCapacity(SlotCapacityInput{
		SlotWidth: 5, SlotDepth: 5, SlotHeight: 5,
		DrugWidth: 3, DrugDepth: 2, DrugHeight: 2,
	})
	if cap != 4 {
		t.Errorf("capacity = %d, want 4", cap)
	}
}

func TestCalculateSlotCapacity_ZeroDimensions(t *testing.T) {
	cap := CalculateSlotCapacity(SlotCapacityInput{})
	if cap != 0 {
		t.Errorf("capacity = %d, want 0 (zero dimensions)", cap)
	}
}

func TestCalculateSlotCapacity_DrugLargerThanSlot(t *testing.T) {
	// Drug 10cm wide, slot only 5cm — can't fit even one
	cap := CalculateSlotCapacity(SlotCapacityInput{
		SlotWidth: 5, SlotDepth: 5, SlotHeight: 5,
		DrugWidth: 10, DrugDepth: 10, DrugHeight: 10,
	})
	if cap != 0 {
		t.Errorf("capacity = %d, want 0 (drug too large)", cap)
	}
}

func TestSlotGroupCapacity_MultipleSlots(t *testing.T) {
	// Two slots: 10x5x8 and 8x6x5
	// Drug: 2x1x0.5
	// Slot 1: 5*5*16=400, Slot 2: 4*6*10=240
	// Total: 640
	total := SlotGroupCapacity(
		[]SlotCapacityInput{
			{SlotWidth: 10, SlotDepth: 5, SlotHeight: 8},
			{SlotWidth: 8, SlotDepth: 6, SlotHeight: 5},
		},
		SlotCapacityInput{DrugWidth: 2, DrugDepth: 1, DrugHeight: 0.5},
	)
	if total != 640 {
		t.Errorf("group capacity = %d, want 640", total)
	}
}
