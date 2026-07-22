package vending

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/encoding/protojson"

	eventsv1 "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/events/v1"
)

func TestConsumer_Handle_HealthFail(t *testing.T) {
	fakeClient := NewFailHealthClient("cabinet offline")
	c := &Consumer{
		router: singleClientRouter{client: fakeClient},
		log:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	event := &eventsv1.FulfillmentRequested{
		FulfillmentId:  "ful-1",
		PrescriptionId: "RX-001",
		KioskCode:      "00010001",
		Allocations:    testAllocations(),
		TraceId:        "trace-1",
	}
	payload, _ := protojson.Marshal(event)

	msg := &fakeJetStreamMsg{subject: "medisync.fulfillment.requested", data: payload}
	c.handle(msg)
	// No JS configured, so publishCompleted will fail silently in handle
	// but the key test is that health fail doesn't panic and doesn't call Dispense.
	if fakeClient.DispenseCount() != 0 {
		t.Fatal("expected no dispense calls when health fails")
	}
}

func TestConsumer_Handle_DispenseFail(t *testing.T) {
	fakeClient := NewFailDispenseClient("serial timeout")
	c := &Consumer{
		router: singleClientRouter{client: fakeClient},
		log:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	event := &eventsv1.FulfillmentRequested{
		FulfillmentId:  "ful-1",
		PrescriptionId: "RX-001",
		KioskCode:      "00010001",
		Allocations:    testAllocations(),
		TraceId:        "trace-1",
	}
	payload, _ := protojson.Marshal(event)

	msg := &fakeJetStreamMsg{subject: "medisync.fulfillment.requested", data: payload}
	// handle() should Ack after publishing (even if publish fails for no JS)
	c.handle(msg)
	if fakeClient.DispenseCount() != 1 {
		t.Fatalf("expected 1 dispense call, got %d", fakeClient.DispenseCount())
	}
}

func TestConsumer_Handle_TimeoutResponse(t *testing.T) {
	fakeClient := NewTimeoutClient()
	c := &Consumer{
		router: singleClientRouter{client: fakeClient},
		log:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	event := &eventsv1.FulfillmentRequested{
		FulfillmentId:  "ful-1",
		PrescriptionId: "RX-001",
		KioskCode:      "00010001",
		Allocations:    testAllocations(),
		TraceId:        "trace-1",
	}
	payload, _ := protojson.Marshal(event)

	msg := &fakeJetStreamMsg{subject: "medisync.fulfillment.requested", data: payload}
	c.handle(msg)
	if fakeClient.DispenseCount() != 1 {
		t.Fatalf("expected 1 dispense call, got %d", fakeClient.DispenseCount())
	}
}

func TestConsumer_Handle_Success(t *testing.T) {
	fakeClient := NewFakeClient()
	c := &Consumer{
		router: singleClientRouter{client: fakeClient},
		log:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	event := &eventsv1.FulfillmentRequested{
		FulfillmentId:  "ful-1",
		PrescriptionId: "RX-001",
		KioskCode:      "00010001",
		Allocations:    testAllocations(),
		TraceId:        "trace-1",
	}
	payload, _ := protojson.Marshal(event)

	msg := &fakeJetStreamMsg{subject: "medisync.fulfillment.requested", data: payload}
	c.handle(msg)
	if fakeClient.DispenseCount() != 1 {
		t.Fatalf("expected 1 dispense call, got %d", fakeClient.DispenseCount())
	}
}

func TestConsumer_Handle_BatchesAndOrdersShelvesDescending(t *testing.T) {
	fakeClient := NewFakeClient()
	c := &Consumer{
		router: singleClientRouter{client: fakeClient},
		log:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	event := &eventsv1.FulfillmentRequested{
		FulfillmentId:  "ful-batch",
		PrescriptionId: "RX-batch",
		KioskCode:      "00010001",
		Allocations: []*eventsv1.DispenseAllocation{
			{AllocationId: "alloc-layer-1", DoorNo: 1, HardwareLayer: 1, ChannelStart: 1, ChannelEnd: 1, Quantity: 1},
			{AllocationId: "alloc-layer-5", DoorNo: 1, HardwareLayer: 5, ChannelStart: 2, ChannelEnd: 2, Quantity: 1},
			{AllocationId: "alloc-layer-3", DoorNo: 1, HardwareLayer: 3, ChannelStart: 3, ChannelEnd: 3, Quantity: 1},
		},
	}
	payload, _ := protojson.Marshal(event)
	c.handle(&fakeJetStreamMsg{subject: "medisync.fulfillment.requested", data: payload})

	if fakeClient.DispenseCount() != 1 {
		t.Fatalf("expected one aggregate dispense call, got %d", fakeClient.DispenseCount())
	}
	request := fakeClient.LastDispense()
	if request.Prescription != "RX-batch" {
		t.Fatalf("prescription = %q, want RX-batch", request.Prescription)
	}
	if len(request.Items) != 3 {
		t.Fatalf("items = %d, want 3", len(request.Items))
	}
	wantLayers := []int{5, 3, 1}
	for index, want := range wantLayers {
		if request.Items[index].Layer != want {
			t.Errorf("items[%d].layer = %d, want %d", index, request.Items[index].Layer, want)
		}
	}
	if request.Items[0].AllocationID != "alloc-layer-5" {
		t.Errorf("items[0].allocationId = %q, want alloc-layer-5", request.Items[0].AllocationID)
	}
}

func TestAllocationResultsMapsPartialHardwareFailure(t *testing.T) {
	allocations := []*eventsv1.DispenseAllocation{
		{AllocationId: "a5", HardwareLayer: 5},
		{AllocationId: "a3", HardwareLayer: 3},
		{AllocationId: "a1", HardwareLayer: 1},
	}
	results, failed, reason, detail := allocationResults(allocations, &DispenseResponse{
		OK: 0,
		Data: DispenseData{
			Status: "failed",
			Steps: []DispenseStep{
				{Phase: "dispense", AllocationID: "a5", Layer: 5, Success: true},
				{Phase: "dispense", AllocationID: "a3", Layer: 3, Success: false},
			},
		},
	})

	if !failed || reason != "hardware_failed" || detail != "step dispense failed" {
		t.Fatalf("failure = %v/%q/%q", failed, reason, detail)
	}
	if !results["a5"].Success || results["a3"].Success || results["a1"].Success {
		t.Fatalf("unexpected allocation results: %+v", results)
	}
	if results["a1"].Detail != "not attempted after prior hardware failure" {
		t.Errorf("a1 detail = %q", results["a1"].Detail)
	}
}

func TestConsumer_Handle_MalformedMessage(t *testing.T) {
	c := &Consumer{
		router: singleClientRouter{client: NewFakeClient()},
		log:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	msg := &fakeJetStreamMsg{subject: "medisync.fulfillment.requested", data: []byte(`not valid json`)}
	// handle() should call reject which calls Term()
	c.handle(msg)
}

func TestConsumer_Handle_MissingIDs(t *testing.T) {
	c := &Consumer{
		router: singleClientRouter{client: NewFakeClient()},
		log:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	event := &eventsv1.FulfillmentRequested{
		FulfillmentId:  "", // missing
		PrescriptionId: "RX-001",
		TraceId:        "trace-1",
	}
	payload, _ := protojson.Marshal(event)

	msg := &fakeJetStreamMsg{subject: "medisync.fulfillment.requested", data: payload}
	c.handle(msg)
	// Should have been rejected via Term(), not dispensed.
}

func testAllocations() []*eventsv1.DispenseAllocation {
	return []*eventsv1.DispenseAllocation{{
		AllocationId: "alloc-1",
		DoorNo:       1, HardwareLayer: 1, ChannelStart: 1, ChannelEnd: 1, Quantity: 1,
	}}
}

type fakeJetStreamMsg struct {
	subject string
	data    []byte
}

func (m *fakeJetStreamMsg) Data() []byte         { return m.data }
func (m *fakeJetStreamMsg) Subject() string      { return m.subject }
func (m *fakeJetStreamMsg) Headers() nats.Header { return nil }
func (m *fakeJetStreamMsg) Metadata() (*jetstream.MsgMetadata, error) {
	return &jetstream.MsgMetadata{}, nil
}
func (m *fakeJetStreamMsg) Ack() error                             { return nil }
func (m *fakeJetStreamMsg) Nak() error                             { return nil }
func (m *fakeJetStreamMsg) Term() error                            { return nil }
func (m *fakeJetStreamMsg) InProgress() error                      { return nil }
func (m *fakeJetStreamMsg) AckAck() error                          { return nil }
func (m *fakeJetStreamMsg) NakWithDelay(delay time.Duration) error { return nil }
func (m *fakeJetStreamMsg) DoubleAck(ctx context.Context) error    { return nil }
func (m *fakeJetStreamMsg) Reply() string                          { return "" }
func (m *fakeJetStreamMsg) TermWithReason(reason string) error     { return nil }
