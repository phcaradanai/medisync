//go:build integration

package vending

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/encoding/protojson"

	eventsv1 "github.com/adm-chura3inter/medisync/services/core/internal/gen/medisync/events/v1"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/audit"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/natsx"
	"github.com/adm-chura3inter/medisync/services/core/internal/testutil"
)

const vendingTestToken = "vending-integration-token"

type observedDispenseRequest struct {
	request       DispenseRequest
	authorization string
}

func TestConsumerSendsDispenseRequest(t *testing.T) {
	requests := make(chan observedDispenseRequest, 1)
	url, cleanup := testutil.StartHTTPServer(map[string]http.HandlerFunc{
		"GET /api/v1/health": healthyVendingHandler,
		"POST /api/v1/vending/drugDispenser": func(w http.ResponseWriter, r *http.Request) {
			var request DispenseRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			requests <- observedDispenseRequest{
				request:       request,
				authorization: r.Header.Get("Authorization"),
			}
			writeSuccessfulDispense(w, request.Prescription)
		},
	})
	defer cleanup()

	js := &recordingVendingJetStream{}
	consumer := newHTTPVendingConsumer(url, js)
	msg := newFulfillmentRequestedMessage(t, "fulfill-123", "RX-123")

	consumer.handle(msg)

	if msg.acks != 1 || msg.naks != 0 {
		t.Fatalf("ack/nak = %d/%d, want 1/0", msg.acks, msg.naks)
	}

	got := <-requests
	if got.authorization != "Bearer "+vendingTestToken {
		t.Errorf("Authorization = %q, want Bearer token", got.authorization)
	}
	if got.request.Prescription != "RX-123" {
		t.Errorf("prescription = %q, want RX-123", got.request.Prescription)
	}
	if got.request.DoorNo != 1 {
		t.Errorf("doorNo = %d, want 1", got.request.DoorNo)
	}
	if len(got.request.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(got.request.Items))
	}
	item := got.request.Items[0]
	if item.Layer != 1 || item.ChannelStart != 1 || item.ChannelEnd != 1 || item.Quantity != 1 {
		t.Errorf("dispense item = %+v, want layer/channel 1 and quantity 1", item)
	}

	completed := decodeDispenseCompleted(t, js)
	if completed.GetDispenseId() != "fulfill-123" || completed.GetPrescriptionId() != "RX-123" {
		t.Errorf("dispense.completed = %+v", completed)
	}
	if completed.GetCompletedAt() == nil {
		t.Error("successful dispense must include completed_at")
	}
}

func TestConsumerPublishesFailureOnVendingServerError(t *testing.T) {
	url, cleanup := testutil.StartHTTPServer(map[string]http.HandlerFunc{
		"GET /api/v1/health": healthyVendingHandler,
		"POST /api/v1/vending/drugDispenser": func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "controller unavailable", http.StatusInternalServerError)
		},
	})
	defer cleanup()

	js := &recordingVendingJetStream{}
	consumer := newHTTPVendingConsumer(url, js)
	msg := newFulfillmentRequestedMessage(t, "fulfill-500", "RX-500")

	consumer.handle(msg)

	if msg.acks != 1 || msg.naks != 0 {
		t.Fatalf("ack/nak = %d/%d, want 1/0", msg.acks, msg.naks)
	}
	completed := decodeDispenseCompleted(t, js)
	if completed.GetCompletedAt() != nil {
		t.Error("failed dispense must not include completed_at")
	}
}

func TestConsumerTimesOutSlowVendingAgent(t *testing.T) {
	handlerStarted := make(chan struct{}, 1)
	releaseHandler := make(chan struct{})
	url, cleanup := testutil.StartHTTPServer(map[string]http.HandlerFunc{
		"GET /api/v1/health": healthyVendingHandler,
		"POST /api/v1/vending/drugDispenser": func(w http.ResponseWriter, r *http.Request) {
			handlerStarted <- struct{}{}
			select {
			case <-time.After(10 * time.Second):
				writeSuccessfulDispense(w, "RX-timeout")
			case <-releaseHandler:
				return
			case <-r.Context().Done():
				return
			}
		},
	})
	defer cleanup()
	defer close(releaseHandler)

	js := &recordingVendingJetStream{}
	consumer := newHTTPVendingConsumer(url, js)
	consumer.client.(*httpClient).http.Timeout = 100 * time.Millisecond
	msg := newFulfillmentRequestedMessage(t, "fulfill-timeout", "RX-timeout")

	startedAt := time.Now()
	consumer.handle(msg)
	elapsed := time.Since(startedAt)

	select {
	case <-handlerStarted:
	default:
		t.Fatal("slow dispense handler was not called")
	}
	if elapsed >= time.Second {
		t.Fatalf("client timeout took %s, want less than 1s", elapsed)
	}
	if msg.acks != 1 || msg.naks != 0 {
		t.Fatalf("ack/nak = %d/%d, want 1/0", msg.acks, msg.naks)
	}
	completed := decodeDispenseCompleted(t, js)
	if completed.GetCompletedAt() != nil {
		t.Error("timed-out dispense must not include completed_at")
	}
}

func healthyVendingHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func writeSuccessfulDispense(w http.ResponseWriter, prescription string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(DispenseResponse{
		OK: 1,
		Data: DispenseData{
			PrescriptionNo: prescription,
			Status:         "success",
			Door:           1,
			Steps: []DispenseStep{
				{Phase: "dispense", Layer: 1, Success: true},
			},
		},
	})
}

func newHTTPVendingConsumer(url string, js jetstream.JetStream) *Consumer {
	return &Consumer{
		js:     js,
		client: NewClient(url, vendingTestToken),
		audit:  audit.NewWriterWithDB(&testutil.FakeExecer{}),
		log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func newFulfillmentRequestedMessage(t *testing.T, fulfillmentID, prescriptionID string) *vendingIntegrationMsg {
	t.Helper()
	payload, err := protojson.Marshal(&eventsv1.FulfillmentRequested{
		FulfillmentId:  fulfillmentID,
		PrescriptionId: prescriptionID,
		TraceId:        "trace-123",
	})
	if err != nil {
		t.Fatalf("marshal fulfillment.requested: %v", err)
	}
	return &vendingIntegrationMsg{subject: natsx.SubjectFulfillmentRequested, data: payload}
}

func decodeDispenseCompleted(t *testing.T, js *recordingVendingJetStream) *eventsv1.DispenseCompleted {
	t.Helper()
	if len(js.published) != 1 || js.published[0].subject != natsx.SubjectDispenseCompleted {
		t.Fatalf("published subjects = %v, want [%s]", js.subjects(), natsx.SubjectDispenseCompleted)
	}
	var completed eventsv1.DispenseCompleted
	if err := protojson.Unmarshal(js.published[0].payload, &completed); err != nil {
		t.Fatalf("decode dispense.completed: %v", err)
	}
	return &completed
}

type publishedVendingEvent struct {
	subject string
	payload []byte
}

type recordingVendingJetStream struct {
	jetstream.JetStream
	published []publishedVendingEvent
}

func (j *recordingVendingJetStream) Publish(
	_ context.Context,
	subject string,
	payload []byte,
	_ ...jetstream.PublishOpt,
) (*jetstream.PubAck, error) {
	j.published = append(j.published, publishedVendingEvent{
		subject: subject,
		payload: append([]byte(nil), payload...),
	})
	return &jetstream.PubAck{}, nil
}

func (j *recordingVendingJetStream) subjects() []string {
	subjects := make([]string, len(j.published))
	for i, event := range j.published {
		subjects[i] = event.subject
	}
	return subjects
}

type vendingIntegrationMsg struct {
	subject string
	data    []byte
	acks    int
	naks    int
	terms   int
}

func (m *vendingIntegrationMsg) Data() []byte         { return m.data }
func (m *vendingIntegrationMsg) Subject() string      { return m.subject }
func (m *vendingIntegrationMsg) Headers() nats.Header { return nil }
func (m *vendingIntegrationMsg) Metadata() (*jetstream.MsgMetadata, error) {
	return &jetstream.MsgMetadata{}, nil
}
func (m *vendingIntegrationMsg) Reply() string                    { return "" }
func (m *vendingIntegrationMsg) Ack() error                       { m.acks++; return nil }
func (m *vendingIntegrationMsg) DoubleAck(context.Context) error  { m.acks++; return nil }
func (m *vendingIntegrationMsg) Nak() error                       { m.naks++; return nil }
func (m *vendingIntegrationMsg) NakWithDelay(time.Duration) error { m.naks++; return nil }
func (m *vendingIntegrationMsg) InProgress() error                { return nil }
func (m *vendingIntegrationMsg) Term() error                      { m.terms++; return nil }
func (m *vendingIntegrationMsg) TermWithReason(string) error      { m.terms++; return nil }
