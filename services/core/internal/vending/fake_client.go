package vending

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Ensure FakeClient satisfies Client.
var _ Client = (*FakeClient)(nil)

// FakeClient is an in-memory vending client that always succeeds by default.
// Use DispenseFn and HealthFn to inject custom behavior in tests.
type FakeClient struct {
	mu         sync.Mutex
	Dispenses  []DispenseRequest
	HealthFn   func(ctx context.Context) error
	DispenseFn func(ctx context.Context, req DispenseRequest) (*DispenseResponse, error)
}

// NewFakeClient returns a no-op fake vending client for dev/testing.
func NewFakeClient() *FakeClient {
	return &FakeClient{}
}

func (f *FakeClient) Health(ctx context.Context) error {
	f.mu.Lock()
	// Do not record health calls in Dispenses — health is a separate method.
	f.mu.Unlock()

	if f.HealthFn != nil {
		return f.HealthFn(ctx)
	}
	// Default: always healthy.
	return nil
}

func (f *FakeClient) Dispense(ctx context.Context, req DispenseRequest) (*DispenseResponse, error) {
	f.mu.Lock()
	f.Dispenses = append(f.Dispenses, req)
	f.mu.Unlock()

	if f.DispenseFn != nil {
		return f.DispenseFn(ctx, req)
	}

	return &DispenseResponse{
		OK: true,
		Data: DispenseData{
			PrescriptionNo: req.Prescription,
			Status:         "success",
			Door:           req.DoorNo,
			Steps: []DispenseStep{
				{Phase: "lift", Layer: 1, Success: true},
				{Phase: "dispense", Layer: 1, Success: true},
				{Phase: "lift-to-delivery", Layer: 0, Success: true},
				{Phase: "output-door-open", Layer: 0, Success: true},
				{Phase: "conveyor", Layer: 0, Success: true},
				{Phase: "pickup-door-open", Layer: 0, Success: true},
				{Phase: "output-door-close", Layer: 0, Success: true},
			},
		},
	}, nil
}

// LastDispense returns the most recently submitted dispense request, or empty.
func (f *FakeClient) LastDispense() DispenseRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.Dispenses) == 0 {
		return DispenseRequest{}
	}
	return f.Dispenses[len(f.Dispenses)-1]
}

// DispenseCount returns the number of submitted dispenses.
func (f *FakeClient) DispenseCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.Dispenses)
}

// Reset clears all recorded dispenses.
func (f *FakeClient) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Dispenses = nil
	f.DispenseFn = nil
	f.HealthFn = nil
}

// NewFailHealthClient returns a fake whose Health always fails.
func NewFailHealthClient(reason string) *FakeClient {
	f := NewFakeClient()
	f.HealthFn = func(_ context.Context) error {
		return fmt.Errorf("vending fake health failure: %s", reason)
	}
	return f
}

// NewFailDispenseClient returns a fake whose Dispense always fails.
func NewFailDispenseClient(reason string) *FakeClient {
	f := NewFakeClient()
	f.DispenseFn = func(_ context.Context, _ DispenseRequest) (*DispenseResponse, error) {
		return nil, fmt.Errorf("vending fake dispense failure: %s", reason)
	}
	return f
}

// NewTimeoutClient returns a fake whose Dispense simulates a hardware timeout.
// The returned response has status "failed" — callers treat this as FAILED.
func NewTimeoutClient() *FakeClient {
	f := NewFakeClient()
	f.DispenseFn = func(_ context.Context, req DispenseRequest) (*DispenseResponse, error) {
		return &DispenseResponse{
			OK: false,
			Data: DispenseData{
				PrescriptionNo: req.Prescription,
				Status:         "failed",
				Door:           req.DoorNo,
				Steps: []DispenseStep{
					{Phase: "lift", Layer: 1, Success: true},
					{Phase: "dispense", Layer: 1, Success: false},
				},
			},
		}, nil
	}
	return f
}

// NewSuccessClient returns a fake that succeeds with the given status.
func NewSuccessClient(status string) *FakeClient {
	f := NewFakeClient()
	f.DispenseFn = func(_ context.Context, req DispenseRequest) (*DispenseResponse, error) {
		return &DispenseResponse{
			OK: true,
			Data: DispenseData{
				PrescriptionNo: req.Prescription,
				Status:         status,
				Door:           req.DoorNo,
				Steps:          []DispenseStep{},
			},
		}, nil
	}
	return f
}

// TimeoutWait returns a fake that waits the given duration before responding.
func TimeoutWait(d time.Duration) *FakeClient {
	f := NewFakeClient()
	f.DispenseFn = func(ctx context.Context, req DispenseRequest) (*DispenseResponse, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(d):
		}
		return &DispenseResponse{
			OK: true,
			Data: DispenseData{
				PrescriptionNo: req.Prescription,
				Status:         "success",
				Steps:          []DispenseStep{},
			},
		}, nil
	}
	return f
}
