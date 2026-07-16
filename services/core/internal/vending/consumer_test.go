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
		client: fakeClient,
		log:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	event := &eventsv1.FulfillmentRequested{
		FulfillmentId:  "ful-1",
		PrescriptionId: "RX-001",
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
		client: fakeClient,
		log:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	event := &eventsv1.FulfillmentRequested{
		FulfillmentId:  "ful-1",
		PrescriptionId: "RX-001",
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
		client: fakeClient,
		log:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	event := &eventsv1.FulfillmentRequested{
		FulfillmentId:  "ful-1",
		PrescriptionId: "RX-001",
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
		client: fakeClient,
		log:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	event := &eventsv1.FulfillmentRequested{
		FulfillmentId:  "ful-1",
		PrescriptionId: "RX-001",
		TraceId:        "trace-1",
	}
	payload, _ := protojson.Marshal(event)

	msg := &fakeJetStreamMsg{subject: "medisync.fulfillment.requested", data: payload}
	c.handle(msg)
	if fakeClient.DispenseCount() != 1 {
		t.Fatalf("expected 1 dispense call, got %d", fakeClient.DispenseCount())
	}
}

func TestConsumer_Handle_MalformedMessage(t *testing.T) {
	c := &Consumer{
		client: NewFakeClient(),
		log:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	msg := &fakeJetStreamMsg{subject: "medisync.fulfillment.requested", data: []byte(`not valid json`)}
	// handle() should call reject which calls Term()
	c.handle(msg)
}

func TestConsumer_Handle_MissingIDs(t *testing.T) {
	c := &Consumer{
		client: NewFakeClient(),
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

type fakeJetStreamMsg struct {
	subject string
	data    []byte
}

func (m *fakeJetStreamMsg) Data() []byte                           { return m.data }
func (m *fakeJetStreamMsg) Subject() string                        { return m.subject }
func (m *fakeJetStreamMsg) Headers() nats.Header                   { return nil }
func (m *fakeJetStreamMsg) Metadata() (*jetstream.MsgMetadata, error) { return &jetstream.MsgMetadata{}, nil }
func (m *fakeJetStreamMsg) Ack() error                             { return nil }
func (m *fakeJetStreamMsg) Nak() error                             { return nil }
func (m *fakeJetStreamMsg) Term() error                            { return nil }
func (m *fakeJetStreamMsg) InProgress() error                      { return nil }
func (m *fakeJetStreamMsg) AckAck() error                          { return nil }
func (m *fakeJetStreamMsg) NakWithDelay(delay time.Duration) error  { return nil }
func (m *fakeJetStreamMsg) DoubleAck(ctx context.Context) error     { return nil }
func (m *fakeJetStreamMsg) Reply() string                          { return "" }
func (m *fakeJetStreamMsg) TermWithReason(reason string) error     { return nil }
