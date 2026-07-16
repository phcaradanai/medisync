package cabinet

import "time"

// Cabinet is the domain model for a physical vending machine.
type Cabinet struct {
	ID          string
	Code        string
	Name        string
	DisplayName string
	Active      bool
	ProjectID   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
