package dispensing

import (
	"sort"

	eventsv1 "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/events/v1"
)

// orderHardwareAllocations returns a stable copy ordered for the physical
// lift sequence: highest shelf first, then lower shelves. Keeping this order
// in the persisted dispense.requested payload makes the audit trail match the
// command sequence sent to the cabinet.
func orderHardwareAllocations(allocations []*eventsv1.DispenseAllocation) []*eventsv1.DispenseAllocation {
	ordered := append([]*eventsv1.DispenseAllocation(nil), allocations...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].GetHardwareLayer() > ordered[j].GetHardwareLayer()
	})
	return ordered
}
