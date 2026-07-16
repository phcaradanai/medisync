package vending

import (
	"context"
	"testing"
	"time"

	"github.com/adm-chura3inter/medisync/services/core/internal/platform/config"
)

func TestFakeClient_Dispense_RecordsRequest(t *testing.T) {
	f := NewFakeClient()

	req := DispenseRequest{
		Prescription: "RX-001",
		DoorNo:       1,
		Items: []DispenseItem{
			{Layer: 1, ChannelStart: 1, ChannelEnd: 3, Quantity: 2},
		},
	}

	resp, err := f.Dispense(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.OK != 1 {
		t.Fatal("expected OK response from fake")
	}
	if resp.Data.Status != "success" {
		t.Fatalf("expected status 'success', got %q", resp.Data.Status)
	}
	if len(resp.Data.Steps) != 7 {
		t.Fatalf("expected 7 steps, got %d", len(resp.Data.Steps))
	}
	if f.DispenseCount() != 1 {
		t.Fatalf("expected 1 recorded dispense, got %d", f.DispenseCount())
	}

	last := f.LastDispense()
	if last.Prescription != "RX-001" {
		t.Fatalf("expected prescription 'RX-001', got %q", last.Prescription)
	}
}

func TestFakeClient_Dispense_MultipleRequests(t *testing.T) {
	f := NewFakeClient()

	for i := 0; i < 3; i++ {
		_, err := f.Dispense(context.Background(), DispenseRequest{Prescription: "RX"})
		if err != nil {
			t.Fatalf("unexpected error on dispense %d: %v", i, err)
		}
	}

	if f.DispenseCount() != 3 {
		t.Fatalf("expected 3 recorded dispenses, got %d", f.DispenseCount())
	}
}

func TestFakeClient_Health_DefaultHealthy(t *testing.T) {
	f := NewFakeClient()
	if err := f.Health(context.Background()); err != nil {
		t.Fatalf("fake health should be ok by default, got: %v", err)
	}
}

func TestFakeClient_Health_CustomFn(t *testing.T) {
	f := NewFakeClient()
	f.HealthFn = func(_ context.Context) error {
		return nil
	}
	if err := f.Health(context.Background()); err != nil {
		t.Fatalf("custom health should pass: %v", err)
	}
}

func TestFakeClient_Dispense_CustomFn(t *testing.T) {
	f := NewFakeClient()
	f.DispenseFn = func(_ context.Context, _ DispenseRequest) (*DispenseResponse, error) {
		return &DispenseResponse{
			OK: 1,
			Data: DispenseData{
				PrescriptionNo: "custom",
				Status:         "success",
			},
		}, nil
	}

	resp, err := f.Dispense(context.Background(), DispenseRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Data.PrescriptionNo != "custom" {
		t.Fatalf("expected 'custom', got %q", resp.Data.PrescriptionNo)
	}
}

func TestFakeClient_Reset(t *testing.T) {
	f := NewFakeClient()

	_, _ = f.Dispense(context.Background(), DispenseRequest{})
	_, _ = f.Dispense(context.Background(), DispenseRequest{})

	if f.DispenseCount() != 2 {
		t.Fatal("expected 2 before reset")
	}

	f.Reset()
	if f.DispenseCount() != 0 {
		t.Fatal("expected 0 after reset")
	}

	// Health should also work after reset (HealthFn is nil).
	if err := f.Health(context.Background()); err != nil {
		t.Fatalf("health failed after reset: %v", err)
	}
}

func TestFailHealthClient(t *testing.T) {
	f := NewFailHealthClient("cabinet offline")
	err := f.Health(context.Background())
	if err == nil {
		t.Fatal("expected health error")
	}
	if err.Error() != "vending fake health failure: cabinet offline" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFailDispenseClient(t *testing.T) {
	f := NewFailDispenseClient("serial timeout")
	_, err := f.Dispense(context.Background(), DispenseRequest{})
	if err == nil {
		t.Fatal("expected dispense error")
	}
	if err.Error() != "vending fake dispense failure: serial timeout" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTimeoutClient(t *testing.T) {
	f := NewTimeoutClient()
	resp, err := f.Dispense(context.Background(), DispenseRequest{Prescription: "RX"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.OK != 0 {
		t.Fatal("expected !OK for timeout client")
	}
	if resp.Data.Status != "failed" {
		t.Fatalf("expected status 'failed', got %q", resp.Data.Status)
	}
	if len(resp.Data.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(resp.Data.Steps))
	}
	// The second step should be marked as failed.
	if resp.Data.Steps[1].Success {
		t.Fatal("expected second step to be failed")
	}
}

func TestSuccessClient(t *testing.T) {
	f := NewSuccessClient("dispensed")
	resp, err := f.Dispense(context.Background(), DispenseRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.OK != 1 {
		t.Fatal("expected OK")
	}
	if resp.Data.Status != "dispensed" {
		t.Fatalf("expected status 'dispensed', got %q", resp.Data.Status)
	}
}

func TestTimeoutWait_Success(t *testing.T) {
	f := TimeoutWait(10 * time.Millisecond)
	ctx := context.Background()
	resp, err := f.Dispense(ctx, DispenseRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.OK != 1 {
		t.Fatal("expected OK")
	}
}

func TestTimeoutWait_ContextCanceled(t *testing.T) {
	f := TimeoutWait(10 * time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := f.Dispense(ctx, DispenseRequest{})
	if err == nil {
		t.Fatal("expected context canceled error")
	}
}

func TestNewClientFromConfig_Real(t *testing.T) {
	cfg := config.Config{
		VendingURL:            "http://localhost:4000",
		VendingAPIBearerToken: "secret",
		FulfillmentFake:       false,
	}
	c := NewClientFromConfig(cfg)
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	_, isFake := c.(*FakeClient)
	if isFake {
		t.Fatal("expected real client, got FakeClient")
	}
}

func TestNewClientFromConfig_Fake(t *testing.T) {
	cfg := config.Config{
		VendingURL:            "http://localhost:4000",
		VendingAPIBearerToken: "secret",
		FulfillmentFake:       true,
	}
	c := NewClientFromConfig(cfg)
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	_, isFake := c.(*FakeClient)
	if !isFake {
		t.Fatal("expected FakeClient, got real client")
	}
}

func TestCompileTimeInterfaceCheck(t *testing.T) {
	// Ensure both implementations satisfy Client.
	var _ Client = (*httpClient)(nil)
	var _ Client = (*FakeClient)(nil)
}
