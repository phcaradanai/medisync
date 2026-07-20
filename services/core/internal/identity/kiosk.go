package identity

import (
	"errors"
	"time"
)

// Kiosk is the domain model for a provisioned dispensing terminal.
type Kiosk struct {
	ID          string
	Code        string
	DisplayName string
	Name        string
	PinHash     string
	Active      bool
	ProjectID   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Kiosk error sentinels returned to callers. They must not distinguish
// between a wrong code and a wrong PIN.
var (
	ErrKioskCodeRequired  = errors.New("kiosk code is required")
	ErrKioskPinRequired   = errors.New("kiosk PIN is required")
	ErrInvalidKioskCode   = errors.New("invalid kiosk credentials")
	ErrInactiveKiosk      = errors.New("kiosk is inactive")
	ErrDuplicateKioskCode = errors.New("kiosk code already exists")
	ErrKioskNotFound      = errors.New("kiosk not found")
	ErrMissingKioskToken  = errors.New("kiosk access token is required")
)
