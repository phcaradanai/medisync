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

	event := &eventsv1.DispenseRequested{
		DispenseId:     "d1",
		PrescriptionId: "RX-001",
		TraceId:        "trace-1",
	}
	payload, _ := protojson.Marshal(event)

	msg := &fakeJetStreamMsg{subject: "medisync.dispense.requested", data: payload}
	err := c.handle(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error when vending agent is unhealthy")
	}
}

func TestConsumer_Handle_DispenseFail(t *testing.T) {
	fakeClient := NewFailDispenseClient("serial timeout")
	c := &Consumer{
		client: fakeClient,
		log:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	event := &eventsv1.DispenseRequested{
		DispenseId:     "d1",
		PrescriptionId: "RX-001",
		TraceId:        "trace-1",
	}
	payload, _ := protojson.Marshal(event)

	msg := &fakeJetStreamMsg{subject: "medisync.dispense.requested", data: payload}
	err := c.handle(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error when dispense fails")
	}
	if err.Error() != "publish dispense.failed: no jetstream context" {
		t.Logf("error (expected without JS): %v", err)
	}
}

func TestConsumer_Handle_TimeoutResponse(t *testing.T) {
	fakeClient := NewTimeoutClient()
	c := &Consumer{
		client: fakeClient,
		log:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	event := &eventsv1.DispenseRequested{
		DispenseId:     "d1",
		PrescriptionId: "RX-001",
		TraceId:        "trace-1",
	}
	payload, _ := protojson.Marshal(event)

	msg := &fakeJetStreamMsg{subject: "medisync.dispense.requested", data: payload}
	err := c.handle(context.Background(), msg)
	if err == nil {
		t.Fatal("expected error for timeout response")
	}
}

func TestConsumer_Handle_Success(t *testing.T) {
	fakeClient := NewFakeClient()
	c := &Consumer{
		client: fakeClient,
		log:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	event := &eventsv1.DispenseRequested{
		DispenseId:     "d1",
		PrescriptionId: "RX-001",
		TraceId:        "trace-1",
	}
	payload, _ := protojson.Marshal(event)

	msg := &fakeJetStreamMsg{subject: "medisync.dispense.requested", data: payload}
	err := c.handle(context.Background(), msg)
	if err == nil {
		t.Fatal("expected publish error (no JS in test)")
	}
	if err.Error() != "publish dispense.completed: no jetstream context" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConsumer_Handle_MalformedMessage(t *testing.T) {
	c := &Consumer{
		client: NewFakeClient(),
		log:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	}

	msg := &fakeJetStreamMsg{subject: "medisync.dispense.requested", data: []byte(`not valid json`)}
	err := c.handle(context.Background(), msg)
	if err != nil {
		t.Fatalf("malformed message should be term'd, not errored: %v", err)
	}
}

type fakeJetStreamMsg struct {
	subject string
	data    []byte
}

func (m *fakeJetStreamMsg) Data() []byte               { return m.data }
func (m *fakeJetStreamMsg) Subject() string              { return m.subject }
func (m *fakeJetStreamMsg) Headers() nats.Header          { return nil }
func (m *fakeJetStreamMsg) Metadata() (*jetstream.MsgMetadata, error) { return &jetstream.MsgMetadata{}, nil }
func (m *fakeJetStreamMsg) Ack() error                 { return nil }
func (m *fakeJetStreamMsg) Nak() error                 { return nil }
func (m *fakeJetStreamMsg) Term() error                { return nil }
func (m *fakeJetStreamMsg) InProgress() error          { return nil }
func (m *fakeJetStreamMsg) AckAck() error              { return nil }
func (m *fakeJetStreamMsg) NakWithDelay(delay time.Duration) error  { return nil }
func (m *fakeJetStreamMsg) DoubleAck(ctx context.Context) error           { return nil }
func (m *fakeJetStreamMsg) Reply() string                                { return "" }
func (m *fakeJetStreamMsg) TermWithReason(reason string) error            { return nil }
