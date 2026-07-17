//go:build integration

package printing

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
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/config"
	"github.com/adm-chura3inter/medisync/services/core/internal/platform/natsx"
	"github.com/adm-chura3inter/medisync/services/core/internal/testutil"
)

const printOpsTestAPIKey = "printops-integration-key"

type observedPrintJob struct {
	request PrintJobRequest
	apiKey  string
}

func TestConsumerSubmitsPrintJob(t *testing.T) {
	requests := make(chan observedPrintJob, 1)
	url, cleanup := testutil.StartHTTPServer(map[string]http.HandlerFunc{
		"POST /api/v1/print-jobs": func(w http.ResponseWriter, r *http.Request) {
			var request PrintJobRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			requests <- observedPrintJob{request: request, apiKey: r.Header.Get("X-Api-Key")}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(PrintJobResponse{ID: "job-123", Status: "PENDING"})
		},
	})
	defer cleanup()

	js := &recordingPrintJetStream{}
	consumer := newHTTPPrintingConsumer(url, js)
	msg := newPrintRequestedMessage(t, "print-123", "RX-123")

	consumer.handle(msg)

	if msg.acks != 1 || msg.naks != 0 {
		t.Fatalf("ack/nak = %d/%d, want 1/0", msg.acks, msg.naks)
	}

	got := <-requests
	if got.apiKey != printOpsTestAPIKey {
		t.Errorf("X-Api-Key = %q, want %q", got.apiKey, printOpsTestAPIKey)
	}
	if got.request.RequestID != "print-123" {
		t.Errorf("request_id = %q, want print-123", got.request.RequestID)
	}
	if got.request.SourceReference != "RX-123" {
		t.Errorf("source_reference = %q, want RX-123", got.request.SourceReference)
	}
	if got.request.Payload["prescription_id"] != "RX-123" {
		t.Errorf("payload.prescription_id = %v, want RX-123", got.request.Payload["prescription_id"])
	}
	if got.request.Payload["generated_at"] == "" {
		t.Error("payload.generated_at must be populated")
	}

	if len(js.published) != 1 || js.published[0].subject != natsx.SubjectPrintCompleted {
		t.Fatalf("published subjects = %v, want [%s]", js.subjects(), natsx.SubjectPrintCompleted)
	}
	var completed eventsv1.PrintCompleted
	if err := protojson.Unmarshal(js.published[0].payload, &completed); err != nil {
		t.Fatalf("decode print.completed: %v", err)
	}
	if completed.GetPrintId() != "print-123" || completed.GetPrescriptionId() != "RX-123" {
		t.Errorf("print.completed = %+v", &completed)
	}
}

func TestConsumerNaksPrintOpsServerError(t *testing.T) {
	url, cleanup := testutil.StartHTTPServer(map[string]http.HandlerFunc{
		"POST /api/v1/print-jobs": func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "print queue unavailable", http.StatusInternalServerError)
		},
	})
	defer cleanup()

	consumer := newHTTPPrintingConsumer(url, &recordingPrintJetStream{})
	msg := newPrintRequestedMessage(t, "print-500", "RX-500")

	consumer.handle(msg)

	if msg.naks != 1 || msg.acks != 0 {
		t.Fatalf("ack/nak = %d/%d, want 0/1", msg.acks, msg.naks)
	}
}

func TestConsumerNaksPrintOpsNetworkError(t *testing.T) {
	url, cleanup := testutil.StartHTTPServer(nil)
	cleanup()

	consumer := newHTTPPrintingConsumer(url, &recordingPrintJetStream{})
	msg := newPrintRequestedMessage(t, "print-offline", "RX-offline")

	consumer.handle(msg)

	if msg.naks != 1 || msg.acks != 0 {
		t.Fatalf("ack/nak = %d/%d, want 0/1", msg.acks, msg.naks)
	}
}

func TestConsumerSucceedsAfterRedelivery(t *testing.T) {
	requests := 0
	url, cleanup := testutil.StartHTTPServer(map[string]http.HandlerFunc{
		"POST /api/v1/print-jobs": func(w http.ResponseWriter, _ *http.Request) {
			requests++
			if requests == 1 {
				http.Error(w, "temporary failure", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(PrintJobResponse{ID: "job-retry", Status: "PENDING"})
		},
	})
	defer cleanup()

	js := &recordingPrintJetStream{}
	consumer := newHTTPPrintingConsumer(url, js)
	firstDelivery := newPrintRequestedMessage(t, "print-retry", "RX-retry")
	redelivery := newPrintRequestedMessage(t, "print-retry", "RX-retry")

	consumer.handle(firstDelivery)
	consumer.handle(redelivery)

	if requests != 2 {
		t.Fatalf("printops requests = %d, want 2", requests)
	}
	if firstDelivery.naks != 1 || firstDelivery.acks != 0 {
		t.Fatalf("first delivery ack/nak = %d/%d, want 0/1", firstDelivery.acks, firstDelivery.naks)
	}
	if redelivery.acks != 1 || redelivery.naks != 0 {
		t.Fatalf("redelivery ack/nak = %d/%d, want 1/0", redelivery.acks, redelivery.naks)
	}
	if len(js.published) != 1 || js.published[0].subject != natsx.SubjectPrintCompleted {
		t.Fatalf("published subjects = %v, want one %s", js.subjects(), natsx.SubjectPrintCompleted)
	}
}

func newHTTPPrintingConsumer(url string, js jetstream.JetStream) *Consumer {
	client := NewClient(config.Config{
		PrintOpsURL:    url,
		PrintOpsAPIKey: printOpsTestAPIKey,
	})
	return &Consumer{
		js:     js,
		client: client,
		audit:  audit.NewWriterWithDB(&testutil.FakeExecer{}),
		log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func newPrintRequestedMessage(t *testing.T, printID, prescriptionID string) *printingTestMsg {
	t.Helper()
	payload, err := protojson.Marshal(&eventsv1.PrintRequested{
		PrintId:        printID,
		PrescriptionId: prescriptionID,
		TraceId:        "trace-123",
	})
	if err != nil {
		t.Fatalf("marshal print.requested: %v", err)
	}
	return &printingTestMsg{subject: natsx.SubjectPrintRequested, data: payload}
}

type publishedPrintEvent struct {
	subject string
	payload []byte
}

type recordingPrintJetStream struct {
	jetstream.JetStream
	published []publishedPrintEvent
}

func (j *recordingPrintJetStream) Publish(
	_ context.Context,
	subject string,
	payload []byte,
	_ ...jetstream.PublishOpt,
) (*jetstream.PubAck, error) {
	j.published = append(j.published, publishedPrintEvent{
		subject: subject,
		payload: append([]byte(nil), payload...),
	})
	return &jetstream.PubAck{}, nil
}

func (j *recordingPrintJetStream) subjects() []string {
	subjects := make([]string, len(j.published))
	for i, event := range j.published {
		subjects[i] = event.subject
	}
	return subjects
}

type printingTestMsg struct {
	subject string
	data    []byte
	acks    int
	naks    int
	terms   int
}

func (m *printingTestMsg) Data() []byte         { return m.data }
func (m *printingTestMsg) Subject() string      { return m.subject }
func (m *printingTestMsg) Headers() nats.Header { return nil }
func (m *printingTestMsg) Metadata() (*jetstream.MsgMetadata, error) {
	return &jetstream.MsgMetadata{}, nil
}
func (m *printingTestMsg) Reply() string                    { return "" }
func (m *printingTestMsg) Ack() error                       { m.acks++; return nil }
func (m *printingTestMsg) DoubleAck(context.Context) error  { m.acks++; return nil }
func (m *printingTestMsg) Nak() error                       { m.naks++; return nil }
func (m *printingTestMsg) NakWithDelay(time.Duration) error { m.naks++; return nil }
func (m *printingTestMsg) InProgress() error                { return nil }
func (m *printingTestMsg) Term() error                      { m.terms++; return nil }
func (m *printingTestMsg) TermWithReason(string) error      { m.terms++; return nil }
