package cabinet

import "time"

// Cabinet is the domain model for a physical vending machine.
type Cabinet struct {
	ID        string
	Code      string
	Name      string
	Active    bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Role-based access: only admins can manage cabinets.
// Ward scoping does not apply to cabinet management.
