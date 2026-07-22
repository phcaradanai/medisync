// Package vending is the anti-corruption layer over vending-3d-ctl-agent.
// It owns:
//   - The HTTP client for POST /api/v1/vending/drugDispenser (Bearer auth, timeout)
//   - A fake client for testing
//   - Health check for hardware readiness
package vending

import "context"

import "net/http"

// DispenseItem describes a single slot to dispense from.
// layer, channelStart, channelEnd match the vending agent's internal addressing.
type DispenseItem struct {
	Layer        int `json:"layer"`
	ChannelStart int `json:"channelStart"`
	ChannelEnd   int `json:"channelEnd"`
	Quantity     int `json:"qty"`
}

// DispenseRequest is the payload sent to POST /api/v1/vending/drugDispenser.
type DispenseRequest struct {
	Prescription string         `json:"prescription"`
	DoorNo       int            `json:"doorNo"`
	Items        []DispenseItem `json:"items"`
}

// DispenseStep records one phase of the hardware dispense sequence.
type DispenseStep struct {
	Phase   string `json:"phase"`
	Layer   int    `json:"layer"`
	Success bool   `json:"success"`
}

// DispenseResponse is the high-level dispense result from the vending agent.
// The ok field is numeric (1 = success, 0 = failure) per the vending-3d-ctl-agent contract.
type DispenseResponse struct {
	OK   int          `json:"ok"`
	Data DispenseData `json:"data"`
}

// DispenseData contains the detailed dispense outcome.
type DispenseData struct {
	PrescriptionNo string         `json:"prescriptionNo"`
	Status         string         `json:"status"` // "success" or "failed"
	Door           int            `json:"door"`
	Steps          []DispenseStep `json:"steps"`
}

// Client is the interface for communicating with vending-3d-ctl-agent.
// Real: HTTP POST /api/v1/vending/drugDispenser with Bearer auth.
// Fake: always-succeed stub for tests.
type Client interface {
	// Health performs a quick health check against GET /api/v1/health.
	// Returns an error when the agent is unreachable or degraded.
	Health(ctx context.Context) error

	// Dispense instructs the vending hardware to dispense medication.
	// Returns the raw agent response. Callers interpret the Status field
	// to determine success or failure.
	Dispense(ctx context.Context, req DispenseRequest) (*DispenseResponse, error)
}

// ScannerEventSource is implemented by the real HTTP client. It is kept
// separate from Client so fake dispensing tests do not need a serial stream.
type ScannerEventSource interface {
	OpenScannerEvents(context.Context) (*http.Response, error)
}
