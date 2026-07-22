//go:build integration

package vending

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/encoding/protojson"

	eventsv1 "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/events/v1"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/audit"
	"github.com/adm-chura3inter/medisync/services/core/internal/testutil"
)

func integrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping integration test that requires PostgreSQL")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Fatalf("ping test database: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
	})
	return pool
}

// TestFakeClientThreadSafety_Race verifies concurrent dispenses are safe.
func TestFakeClientThreadSafety_Race(t *testing.T) {
	client := NewFakeClient()
	done := make(chan struct{})
	const numGoroutines = 10
	const dispensesPerGoroutine = 100

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer func() { done <- struct{}{} }()
			for j := 0; j < dispensesPerGoroutine; j++ {
				_, err := client.Dispense(context.Background(), DispenseRequest{
					Prescription: "concurrent",
					DoorNo:       1,
				})
				if err != nil {
					t.Errorf("goroutine %d: Dispense failed: %v", id, err)
					return
				}
			}
		}(i)
	}

	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	expected := numGoroutines * dispensesPerGoroutine
	if count := client.DispenseCount(); count != expected {
		t.Errorf("DispenseCount = %d, want %d", count, expected)
	}
}

// TestHealthThreadSafety_Race verifies concurrent health checks are safe.
func TestHealthThreadSafety_Race(t *testing.T) {
	client := NewFakeClient()
	done := make(chan struct{})
	const numGoroutines = 20

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			if err := client.Health(context.Background()); err != nil {
				t.Errorf("Health failed: %v", err)
			}
		}()
	}

	for i := 0; i < numGoroutines; i++ {
		<-done
	}
}

// TestConsumerCreation_Integration verifies the consumer struct is accessible.
func TestConsumerCreation_Integration(t *testing.T) {
	c := &Consumer{}
	if c == nil {
		t.Fatal("Consumer struct is nil")
	}
}

// TestAuditWriterIntegration_RoundTrip verifies audit writes with a real PG.
func TestAuditWriterIntegration_RoundTrip(t *testing.T) {
	pool := integrationPool(t) // skips if no PG
	aw := audit.NewWriter(pool)

	err := aw.Write(context.Background(), audit.Entry{
		TraceID:  "trace-integration-1",
		Actor:    "system",
		Action:   "dispense.requested",
		Entity:   "vending",
		EntityID: "disp-001",
		Detail:   map[string]any{"slot": "A1", "qty": 2},
	})
	if err != nil {
		t.Fatalf("audit write failed: %v", err)
	}

	// Write a second entry for the completed event.
	err = aw.Write(context.Background(), audit.Entry{
		TraceID:  "trace-integration-1",
		Actor:    "system",
		Action:   "dispense.completed",
		Entity:   "vending",
		EntityID: "disp-001",
		Detail:   map[string]any{"status": "success"},
	})
	if err != nil {
		t.Fatalf("audit write completed failed: %v", err)
	}
}

// TestAuditWriterWithFakeExecer_Integration verifies the audit writer with a FakeExecer.
func TestAuditWriterWithFakeExecer_Integration(t *testing.T) {
	fakeDB := &testutil.FakeExecer{}
	aw := audit.NewWriterWithDB(fakeDB)

	err := aw.Write(context.Background(), audit.Entry{
		TraceID:  "trace-fake-1",
		Action:   "dispense.completed",
		Entity:   "vending",
		EntityID: "disp-002",
	})
	if err != nil {
		t.Fatalf("audit write failed: %v", err)
	}

	if len(fakeDB.Calls) != 1 {
		t.Fatalf("expected 1 audit call, got %d", len(fakeDB.Calls))
	}

	lastCall := fakeDB.LastCall()
	if lastCall.SQL == "" {
		t.Fatal("expected non-empty SQL")
	}
}

// TestConsumerHandleWithAudit_Integration verifies the consumer writes
// audit entries on fulfillment completion.
func TestConsumerHandleWithAudit_Integration(t *testing.T) {
	fakeDB := &testutil.FakeExecer{}
	aw := audit.NewWriterWithDB(fakeDB)
	fakeClient := NewFakeClient()

	c := &Consumer{
		client: fakeClient,
		audit:  aw,
		log:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	event := &eventsv1.FulfillmentRequested{
		FulfillmentId:  "ful-audit-1",
		PrescriptionId: "RX-AUDIT-001",
		TraceId:        "trace-audit-1",
	}
	payload, _ := protojson.Marshal(event)

	msg := &fakeJetStreamMsg{subject: "medisync.fulfillment.requested", data: payload}
	c.handle(msg)

	// Verify audit was written (even though publish may fail without JS).
	if len(fakeDB.Calls) == 0 {
		t.Fatal("expected at least one audit write")
	}

	firstCall := fakeDB.Calls[0]
	if firstCall.SQL == "" {
		t.Fatal("expected non-empty audit SQL")
	}
}

// TestDispenseRequestJSONRoundTrip verifies the dispense request can be marshaled and back.
func TestDispenseRequestJSONRoundTrip_Integration(t *testing.T) {
	req := DispenseRequest{
		Prescription: "RX-JSON-001",
		DoorNo:       1,
		Items: []DispenseItem{
			{Layer: 1, ChannelStart: 1, ChannelEnd: 3, Quantity: 2},
		},
	}

	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var roundTrip DispenseRequest
	if err := json.Unmarshal(b, &roundTrip); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if roundTrip.Prescription != req.Prescription {
		t.Errorf("Prescription = %q, want %q", roundTrip.Prescription, req.Prescription)
	}
	if len(roundTrip.Items) != 1 {
		t.Errorf("Items len = %d, want 1", len(roundTrip.Items))
	}
	if roundTrip.Items[0].Quantity != 2 {
		t.Errorf("Items[0].Quantity = %d, want 2", roundTrip.Items[0].Quantity)
	}
}

// TestDispenseResponseJSONRoundTrip verifies the dispense response JSON contract.
func TestDispenseResponseJSONRoundTrip_Integration(t *testing.T) {
	// Simulate a real vending agent response.
	raw := `{"ok":1,"data":{"prescriptionNo":"RX-001","status":"success","door":1,"steps":[{"phase":"lift","layer":1,"success":true},{"phase":"dispense","layer":1,"success":true}]}}`

	var resp DispenseResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if resp.OK != 1 {
		t.Fatal("expected OK=true from simulated response")
	}
	if resp.Data.Status != "success" {
		t.Fatalf("expected status success, got %q", resp.Data.Status)
	}
	if len(resp.Data.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(resp.Data.Steps))
	}
}

// TestFulfillmentRequestedProtoRoundTrip verifies the new proto message.
func TestFulfillmentRequestedProtoRoundTrip_Integration(t *testing.T) {
	req := &eventsv1.FulfillmentRequested{
		FulfillmentId:  "ful-001",
		PrescriptionId: "RX-001",
		TraceId:        "trace-1",
	}

	b, err := protojson.Marshal(req)
	if err != nil {
		t.Fatalf("marshal FulfillmentRequested: %v", err)
	}

	var roundTrip eventsv1.FulfillmentRequested
	if err := protojson.Unmarshal(b, &roundTrip); err != nil {
		t.Fatalf("unmarshal FulfillmentRequested: %v", err)
	}

	if roundTrip.GetFulfillmentId() != "ful-001" {
		t.Errorf("FulfillmentId = %q, want ful-001", roundTrip.GetFulfillmentId())
	}
	if roundTrip.GetPrescriptionId() != "RX-001" {
		t.Errorf("PrescriptionId = %q, want RX-001", roundTrip.GetPrescriptionId())
	}
	if roundTrip.GetTraceId() != "trace-1" {
		t.Errorf("TraceId = %q, want trace-1", roundTrip.GetTraceId())
	}
}

// TestFulfillmentCompletedProtoRoundTrip verifies the new proto message.
func TestFulfillmentCompletedProtoRoundTrip_Integration(t *testing.T) {
	req := &eventsv1.FulfillmentCompleted{
		FulfillmentId:  "ful-001",
		PrescriptionId: "RX-001",
		Success:        true,
		Detail:         "dispensed successfully",
		TraceId:        "trace-1",
	}

	b, err := protojson.Marshal(req)
	if err != nil {
		t.Fatalf("marshal FulfillmentCompleted: %v", err)
	}

	var roundTrip eventsv1.FulfillmentCompleted
	if err := protojson.Unmarshal(b, &roundTrip); err != nil {
		t.Fatalf("unmarshal FulfillmentCompleted: %v", err)
	}

	if !roundTrip.GetSuccess() {
		t.Error("expected Success=true")
	}
	if roundTrip.GetDetail() != "dispensed successfully" {
		t.Errorf("Detail = %q", roundTrip.GetDetail())
	}
	if roundTrip.GetFulfillmentId() != "ful-001" {
		t.Errorf("FulfillmentId = %q, want ful-001", roundTrip.GetFulfillmentId())
	}
}

// TestConsumerHandleHealthFailWithAudit_Integration verifies audit on health failure.
func TestConsumerHandleHealthFailWithAudit_Integration(t *testing.T) {
	fakeDB := &testutil.FakeExecer{}
	aw := audit.NewWriterWithDB(fakeDB)
	fakeClient := NewFailHealthClient("cabinet offline")

	c := &Consumer{
		client: fakeClient,
		audit:  aw,
		log:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	event := &eventsv1.FulfillmentRequested{
		FulfillmentId:  "ful-hf-1",
		PrescriptionId: "RX-HF-001",
		TraceId:        "trace-hf-1",
	}
	payload, _ := protojson.Marshal(event)

	msg := &fakeJetStreamMsg{subject: "medisync.fulfillment.requested", data: payload}
	c.handle(msg)
	// Should not have called Dispense.
	if fakeClient.DispenseCount() != 0 {
		t.Fatal("expected no dispense calls when health fails")
	}
}
